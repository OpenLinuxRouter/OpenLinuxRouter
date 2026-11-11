package dhcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// The module's REST surface (design.md §3.2 rule 2, §6.2).
//
// It is deliberately thin. Every decision here was already made and tested in
// config.go, validate.go, plan.go and apply.go; this file translates between
// those and HTTP and does nothing else. If a rule appears in this file that is
// not in one of those, it is in the wrong place — the CLI would not get it.

// ModuleName is the path segment and event label for this module.
const ModuleName = "dhcp"

// HTTP serves the module. Its fields are what the module needs from core
// (design.md §3.1 — modules call core, not the reverse).
type HTTP struct {
	// Applier owns the read and write paths onto the system.
	Applier Applier

	// Lock is core's one global apply lock (§3.6). Writes take it; reads
	// never do.
	Lock *core.Lock

	// Events is where an applied change is announced so the UI can re-read.
	Events *core.Events
}

// Routes is the module's surface, declared as data so that it can be
// enumerated rather than only served (core.Route).
//
// Core mounts these with the /api/dhcp prefix stripped, so the patterns here do
// not repeat the module's own name.
func (h HTTP) Routes() []core.Route {
	return []core.Route{
		// Intent.
		{
			Method: "GET", Path: "/config", Tool: "show config",
			Summary: "Show the stored DHCP configuration: pools, reservations and options.",
			Handler: h.getConfig,
		},
		{
			Method: "PUT", Path: "/config",
			Summary:  "Replace the whole DHCP configuration.",
			Body:     core.BodyFull,
			Mutating: true,
			Query:    core.ApplyQuery(),
			Handler:  h.putConfig,
		},
		{
			Method: "PATCH", Path: "/config",
			Summary:  "Change named DHCP fields and leave the rest alone.",
			Body:     core.BodyRelaxed,
			Mutating: true,
			Query:    core.ApplyQuery(),
			Handler:  h.patchConfig,
		},

		// Dry run. A POST because it takes a body, not because it changes
		// anything — this is the HTTP spelling of `olr --dry-run` (§5.1), and
		// it is what lets an agent propose a change for a human to review
		// (§6.4). An empty body plans the stored intent, which is the drift
		// check (§5.4).
		{
			Method: "POST", Path: "/plan", Tool: "show plan",
			Summary: "Show what a DHCP change would do without doing it. " +
				"An empty body plans the stored configuration, which answers whether the box has drifted.",
			Body:    core.BodyRelaxed,
			Handler: h.postPlan,
		},

		// Re-apply stored intent without changing it — the repair path
		// design.md §5.3.2 asks for in place of rollback, and the same route
		// internal/dns and internal/ingress declare.
		//
		// A server that is enabled but not running drifts with nothing
		// rendered wrong: the plan is one service action and an empty change
		// list, and no edit to the configuration fixes it. Without this route
		// the only repair is to save an unrelated setting.
		{
			Method: "POST", Path: "/apply",
			Summary: "Re-render and restart the DHCP server from the stored configuration, changing no intent. " +
				"This is the repair path for a half-applied change, a hand-edited backend config, " +
				"or a backend that is enabled but not running.",
			Mutating: true,
			Query:    core.ApplyQuery(),
			Handler:  h.postApply,
		},

		// Clearing what is in the way, rather than only reporting it. The same
		// route internal/dns declares, and per-module for the same reason:
		// internal/cli cannot import a module, because the modules import it.
		{
			// No Tool: R3 in the conformance suite keeps every mutating route
			// off the agent surface until §6.2's disruptive gate exists.
			Method: "POST", Path: "/blockers/fix",
			Summary: "Clear what is standing in DHCP's way on this box: install a backend that is " +
				"missing, and stand down a distribution daemon holding UDP/67. " +
				"An empty body clears everything olr can clear; a body of " +
				`{"ids": ["..."]} clears only the named ones, using the ids from /status.`,
			Body:     core.BodyNone,
			Mutating: true,
			Query:    core.ApplyQuery(),
			Handler:  h.postFixBlockers,
		},

		// Observed. Never stored, never revisioned, always stamped (§4.5).
		{
			Method: "GET", Path: "/status", Tool: "status",
			Summary: "Show whether DHCP is running, and whether the box still matches the stored configuration.",
			Handler: h.getStatus,
		},
		{
			Method: "GET", Path: "/leases", Tool: "show leases",
			Summary: "List current DHCP leases: which address each client holds and until when.",
			Handler: h.getLeases,
		},
	}
}

// Handler returns the module's routes.
func (h HTTP) Handler() http.Handler { return core.RouteTable(h.Routes()) }

// --- intent ---------------------------------------------------------------

func (h HTTP) getConfig(w http.ResponseWriter, r *http.Request) {
	cfg, err := h.Applier.Load()
	if err != nil {
		core.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	core.WriteConfig(w, cfg)
}

// putConfig replaces the whole document. Validated against the full schema
// projection (§10).
func (h HTTP) putConfig(w http.ResponseWriter, r *http.Request) {
	data, err := core.ReadBody(w, r)
	if err != nil {
		core.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	// UnmarshalConfig rejects unknown fields. A mistyped key that silently did
	// nothing would be the worst outcome here: a 200, an operator who believes
	// the setting took, and a network that disagrees.
	cfg, err := UnmarshalConfig(data)
	if err != nil {
		core.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.mutate(w, r, func(Config) (Config, error) { return cfg, nil })
}

// patchConfig changes named fields and leaves the rest alone, which is what
// `olr set` and a UI toggle both need.
//
// Note the array semantics inherited from RFC 7386: a patch containing "pools"
// replaces the whole list rather than merging into it. That is why adding one
// reservation is a PUT of the new document rather than a PATCH.
func (h HTTP) patchConfig(w http.ResponseWriter, r *http.Request) {
	patch, err := core.ReadBody(w, r)
	if err != nil {
		core.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(bytes.TrimSpace(patch)) == 0 {
		core.WriteError(w, http.StatusBadRequest, "empty patch body")
		return
	}
	h.mutate(w, r, func(current Config) (Config, error) {
		return core.PatchConfig(current, patch, UnmarshalConfig)
	})
}

// applyResponse always carries the plan and the steps, successful or not.
//
// §5.3.2: there is no rollback, so a half-finished change stays half-finished
// and the honest thing is to say which steps landed. A bare error would leave
// the operator to guess, and re-running is the documented repair.
type applyResponse struct {
	Plan  planView        `json:"plan"`
	Steps []Step          `json:"steps,omitempty"`
	Error *core.ErrorBody `json:"error,omitempty"`
}

// mutate owns the entire read/merge/validate/apply transaction.
func (h HTTP) mutate(w http.ResponseWriter, r *http.Request, edit func(Config) (Config, error)) {
	core.MutateConfig(w, r, h.Lock, h.Applier.Load, edit, func(cfg Config) {
		h.apply(w, r, cfg)
	})
}

// apply is called with the global apply lock held.
func (h HTTP) apply(w http.ResponseWriter, r *http.Request, cfg Config) {
	gate, err := core.ParseApplyGate(r)
	if err != nil {
		core.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Cross-module inputs stay stable until the change is stored.
	if res := Validate(cfg, h.Applier.Networks); !res.OK() {
		core.WriteError(w, http.StatusUnprocessableEntity,
			"invalid dhcp configuration", problems(res.Errors)...)
		return
	}

	var (
		result   ApplyResult
		applyErr error
	)
	result, applyErr = h.Applier.ApplyChecked(r.Context(), cfg, func(plan Plan) error {
		return gate.CheckConnectivity(r.Context(), plan.Impact == ImpactDisruptive, r.URL.Path == "/apply")
	})

	// Published whenever anything was attempted, including a partial failure —
	// especially then. Something on the box changed and every client's idea of
	// it is now stale.
	if len(result.Steps) > 0 {
		h.Events.Publish(core.Event{Type: core.EventApplied, Module: ModuleName})
	}

	resp := applyResponse{Plan: viewPlan(result.Plan), Steps: result.Steps}
	if errors.Is(applyErr, core.ErrApplyHeld) {
		status := http.StatusConflict
		var failure *core.ErrorBody
		if gate.DryRun {
			status = http.StatusOK
		} else {
			failure = &core.ErrorBody{Message: core.ErrApplyHeld.Error()}
		}
		core.WriteJSON(w, status, applyResponse{Plan: viewPlan(result.Plan), Error: failure})
		return
	}

	if applyErr != nil {
		resp.Error = &core.ErrorBody{Message: applyErr.Error()}
		core.WriteJSON(w, http.StatusInternalServerError, resp)
		return
	}
	core.WriteJSON(w, http.StatusOK, resp)
}

// postApply re-applies stored intent, changing none of it.
//
// Any body is ignored rather than merged: this is the route for a box that is
// behind intent the operator already stored, and merging would make it a second
// spelling of PATCH.
func (h HTTP) postApply(w http.ResponseWriter, r *http.Request) {
	h.mutate(w, r, func(current Config) (Config, error) { return current, nil })
}

// --- dry run --------------------------------------------------------------

// postPlan answers "what would this do?" without doing it.
//
// An empty body plans the *stored* intent, which by §5.4 is exactly the drift
// check: plan unchanged intent against reality and see whether the diff is
// empty.
func (h HTTP) postPlan(w http.ResponseWriter, r *http.Request) {
	data, err := core.ReadBody(w, r)
	if err != nil {
		core.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	var cfg Config
	if len(bytes.TrimSpace(data)) == 0 {
		cfg, err = h.Applier.Load()
	} else {
		cfg, err = UnmarshalConfig(data)
	}
	if err != nil {
		core.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	plan, err := h.plan(r, cfg)
	if err != nil {
		// The message summarises and the problems carry the detail — never
		// both. A validation error renders every problem into its own text, so
		// passing it here as well as in problems() reported each one twice: to
		// the operator through `olr`, and to an agent through MCP, since both
		// render the envelope with core.ErrorBody.String. Everywhere else in
		// this module and in dns, clients and routing already passes a fixed
		// summary; this is the same sentence they use.
		message := err.Error()
		if len(plan.Validation.Errors) > 0 {
			message = "invalid dhcp configuration"
		}
		core.WriteError(w, http.StatusUnprocessableEntity, message,
			problems(plan.Validation.Errors)...)
		return
	}
	core.WriteJSON(w, http.StatusOK, viewPlan(plan))
}

// plan reads fresh system state and diffs cfg against it. No lock: §3.6 says
// reads never take one, and §5.4 requires this path to read reality rather than
// a cache for drift detection to mean anything.
func (h HTTP) plan(r *http.Request, cfg Config) (Plan, error) {
	obs, err := h.Applier.Observe(r.Context())
	if err != nil {
		return Plan{}, err
	}
	return BuildPlan(h.Applier.Backend, cfg, h.Applier.Networks, obs, time.Now())
}

// --- observed -------------------------------------------------------------

type statusResponse struct {
	Enabled bool `json:"enabled"`

	// Service is what systemd knows. Absent, with ServiceError set, when the
	// query itself failed — which is normal on a developer box with no D-Bus
	// and must not take the rest of the answer down with it.
	Service      *ServiceStatus `json:"service,omitempty"`
	ServiceError string         `json:"service_error,omitempty"`

	// Drifted is the §5.4 answer: does the stored intent still describe what is
	// actually on the box?
	Drifted    bool      `json:"drifted"`
	Drift      *planView `json:"drift,omitempty"`
	DriftError string    `json:"drift_error,omitempty"`

	// Blockers are things about the box, not the configuration, standing
	// between this module and its job — a distribution daemon holding UDP/67.
	//
	// The same list internal/dns publishes, asked about a different port.
	// dnsmasq.service takes both, and that is the case worth getting right: a
	// second dnsmasq is the likeliest incumbent here precisely because olr's own
	// documentation sends people to install the package it ships in.
	Blockers []core.Blocker `json:"blockers,omitempty"`

	// AsOf stamps the whole reply. Every observed object carries its freshness
	// so no surface can imply one it does not have (§4.5).
	AsOf time.Time `json:"as_of"`
}

func (h HTTP) getStatus(w http.ResponseWriter, r *http.Request) {
	resp := statusResponse{AsOf: time.Now()}

	cfg, err := h.Applier.Load()
	if err != nil {
		core.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	resp.Enabled = cfg.Enabled

	// Each half of "health" is reported independently and neither can suppress
	// the other (§5.4 — drift and backend liveness are two questions).
	if svc, err := h.Applier.Service.Status(r.Context()); err != nil {
		resp.ServiceError = err.Error()
	} else {
		resp.Service = &svc
	}

	resp.Blockers = h.blockers(r.Context())

	if plan, err := h.plan(r, cfg); err != nil {
		resp.DriftError = err.Error()
	} else {
		view := viewPlan(plan)
		resp.Drifted = !plan.Empty()
		resp.Drift = &view
	}

	core.WriteJSON(w, http.StatusOK, resp)
}

// blockers is what stands between this module and its job.
//
// Asked for unconditionally, including while the module is off — off is when the
// operator is about to turn it on, and the alternative is finding out from an
// apply that got halfway.
//
// Dependencies first: a missing dnsmasq is why nothing is serving, and a port
// conflict reported above it would read as the cause when it is not.
//
// One function rather than two copies so /status and /blockers/fix cannot
// disagree about what is wrong — the fix acts on what it re-derives here.
func (h HTTP) blockers(ctx context.Context) []core.Blocker {
	return append(core.DependencyBlockers(Dependencies()),
		core.DistroConflicts(ctx, dhcpServerPort)...)
}

// --- clearing what is in the way -------------------------------------------

// fixRequest names which blockers to clear. Absent or empty means all of them.
type fixRequest struct {
	IDs []string `json:"ids,omitempty"`
}

// fixResponse carries the steps whether or not they all landed (§5.3.2).
type fixResponse struct {
	Steps []core.Step     `json:"steps,omitempty"`
	Error *core.ErrorBody `json:"error,omitempty"`
}

func (h HTTP) postFixBlockers(w http.ResponseWriter, r *http.Request) {
	data, err := core.ReadBody(w, r)
	if err != nil {
		core.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	var req fixRequest
	if len(bytes.TrimSpace(data)) > 0 {
		if err := json.Unmarshal(data, &req); err != nil {
			core.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
	}

	// No apply lock; core.FixBlockers says why.
	steps, err := core.FixBlockers(r.Context(), h.blockers(r.Context()), req.IDs)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, core.ErrNoSuchFix) {
			status = http.StatusNotFound
		}
		core.WriteError(w, status, err.Error())
		return
	}

	if len(steps) > 0 {
		h.Events.Publish(core.Event{Type: core.EventApplied, Module: ModuleName})
	}

	resp := fixResponse{Steps: steps}
	if core.StepsFailed(steps) {
		resp.Error = &core.ErrorBody{Message: "some of that did not work; the steps say which"}
		core.WriteJSON(w, http.StatusInternalServerError, resp)
		return
	}
	core.WriteJSON(w, http.StatusOK, resp)
}

type leasesResponse struct {
	Leases []leaseView `json:"leases"`
	Usage  []usageView `json:"usage"`

	// Problems are unparseable lines in the lease database, reported rather
	// than dropped so a corrupt file is visible instead of silently shrinking
	// the list.
	Problems []core.Problem `json:"problems,omitempty"`

	AsOf time.Time `json:"as_of"`
}

func (h HTTP) getLeases(w http.ResponseWriter, r *http.Request) {
	now := time.Now()

	leases, bad, err := h.Applier.Leases()
	if err != nil {
		core.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	cfg, err := h.Applier.Load()
	if err != nil {
		core.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	resp := leasesResponse{
		Leases:   make([]leaseView, 0, len(leases)),
		Usage:    make([]usageView, 0, len(cfg.Pools)),
		Problems: problems(bad),
		AsOf:     now,
	}
	for _, l := range leases {
		resp.Leases = append(resp.Leases, viewLease(l, now))
	}
	for _, u := range h.Applier.Usage(cfg, leases) {
		resp.Usage = append(resp.Usage, viewUsage(u))
	}

	core.WriteJSON(w, http.StatusOK, resp)
}

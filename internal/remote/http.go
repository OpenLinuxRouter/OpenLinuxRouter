package remote

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// HTTP serves the module.
type HTTP struct {
	// Tunnel owns the read and write paths onto WireGuard.
	Tunnel TunnelApplier

	// Socks owns the read and write paths onto SOCKS5.
	Socks ProxyApplier

	// Lock is core's one global apply lock (§3.6). Writes take it; reads never
	// do.
	Lock *core.Lock

	// Events is where an applied change is announced so the UI can re-read.
	Events *core.Events

	// Conns reads the kernel's connection table for who is using the proxies.
	// Nil means the platform's own.
	Conns ConnTable

	// Places says where an address is. Nil answers every place as unknown.
	Places Locator
}

// gate is the pair of query parameters every mutating route carries (§6.2):
// ask first with dry_run, go ahead with a disruptive plan only with confirm.
//
// A function rather than a package variable because a Route holds the slice and
// a shared backing array would let one module's table be edited through
// another's.
func gate() []core.QueryParam {
	return []core.QueryParam{
		{
			Name: "dry_run", Type: "boolean",
			Summary: "Answer with the plan and change nothing.",
		},
		{
			Name: "confirm", Type: "boolean",
			Summary: "Go ahead even if the plan is disruptive. Without this a disruptive change is refused, and the plan is returned instead.",
		},
	}
}

// Routes is the module's surface, declared as data so it can be enumerated
// rather than only served. Core mounts these with the /api/remote prefix
// stripped.
func (h HTTP) Routes() []core.Route {
	out := []core.Route{
		{
			Method: "GET", Path: "/config", Tool: "show config",
			Summary: "Show the whole stored remote-access configuration: the address devices dial, and both ways in. " +
				"Credentials are never returned.",
			Handler: h.getConfig,
		},
		{
			Method: "PATCH", Path: "/config",
			Summary: "Change the address devices dial. It is shared by every way in, so changing it " +
				"invalidates every configuration and link already handed out.",
			Body:     core.BodyRelaxed,
			Query:    gate(),
			Mutating: true,
			Handler:  h.patchConfig,
		},
		{
			Method: "GET", Path: "/status", Tool: "status",
			Summary: "Show whether each way in is working: the tunnel's interface and its devices, the proxy's service, " +
				"and whether the box still matches the stored configuration.",
			Handler: h.getStatus,
		},
		{
			Method: "GET", Path: "/clients", Tool: "show clients",
			Summary: "Show who is connected right now, and from where: the addresses with connections open to each proxy, " +
				"and the country and network of each of those and of every tunnel device's last address. " +
				"An address is a place, not a device.",
			Handler: h.getClients,
		},
		{
			// No Tool, and the conformance suite's R3 is why: no mutating route
			// is published to an agent until §6.2's disruptive gate exists, and
			// this one installs software.
			Method: "POST", Path: "/blockers/fix",
			Summary: "Clear what is standing in remote access's way on this box — install wireguard-tools if it is missing. " +
				`An empty body clears everything olr can clear; a body of {"ids": ["..."]} clears only the named ones.`,
			Body:     core.BodyNone,
			Mutating: true,
			Handler:  h.postFixBlockers,
		},
	}
	out = append(out, h.wireguardRoutes()...)
	out = append(out, h.socksRoutes()...)
	return out
}

// Handler returns the module's routes.
func (h HTTP) Handler() http.Handler { return core.RouteTable(h.Routes()) }

// --- the whole document -----------------------------------------------------

func (h HTTP) getConfig(w http.ResponseWriter, r *http.Request) {
	cfg, err := h.Tunnel.Load()
	if err != nil {
		core.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	core.WriteConfig(w, cfg, cfg.Redacted())
}

// endpointPatch is the only thing the module-level PATCH accepts.
//
// Narrow on purpose. A merge patch over the whole document would let a caller
// change a cipher or a device through a route that plans neither, which is the
// one way the two-surface split could be defeated from outside.
type endpointPatch struct {
	Endpoint *string `json:"endpoint"`
}

// patchConfig changes the address devices dial.
//
// It has a gate and no plan, and that pair is worth explaining rather than
// looking like an oversight. The endpoint reaches no kernel state and no
// rendered file — it appears only in configurations and links that have
// **already been handed out**. So there is nothing to apply, and yet it is the
// most destructive field in the module: changing it makes every device's
// configuration point at the wrong place, with nothing failing on this box at
// all.
//
// A plan would have no changes to show. What the operator needs is the
// consequence, so that is what the refusal carries.
func (h HTTP) patchConfig(w http.ResponseWriter, r *http.Request) {
	data, err := core.ReadBody(w, r)
	if err != nil {
		core.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(bytes.TrimSpace(data)) == 0 {
		core.WriteError(w, http.StatusBadRequest, "empty patch body")
		return
	}

	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var patch endpointPatch
	if err := dec.Decode(&patch); err != nil {
		core.WriteError(w, http.StatusBadRequest,
			"this route changes the shared endpoint only; use /wireguard/config or "+
				"/socks5/config for the rest: "+err.Error())
		return
	}
	if patch.Endpoint == nil {
		core.WriteError(w, http.StatusBadRequest, "nothing to change")
		return
	}

	dryRun, confirm, err := gateParams(r)
	if err != nil {
		core.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	stale := false
	var (
		failed  error
		invalid Result
		impact  = ImpactNone
		reasons []string
		stored  Config
	)

	if lockErr := h.Lock.Do(r.Context(), func() error {
		cfg, err := h.Tunnel.Load()
		if err != nil {
			failed = err
			return nil
		}
		if !core.MatchConfig(w, r, cfg) {
			stale = true
			return nil
		}
		previous := cfg.Clone()
		cfg.Endpoint = *patch.Endpoint
		cfg.Normalize()

		var res Result
		validateEndpoint(&res, cfg)
		if !res.OK() {
			invalid = res
			return nil
		}

		impact, reasons = classifyEndpoint(previous, cfg)
		stored = previous
		if dryRun || (impact == ImpactDisruptive && !confirm) {
			return nil
		}
		if err := h.Tunnel.Save(cfg); err != nil {
			failed = err
			return nil
		}
		stored = cfg
		return nil
	}); lockErr != nil {
		core.WriteError(w, http.StatusServiceUnavailable,
			"timed out waiting for the apply lock: "+lockErr.Error())
		return
	}

	if stale {
		return
	}

	switch {
	case failed != nil:
		core.WriteError(w, http.StatusInternalServerError, failed.Error())
		return
	case !invalid.OK():
		core.WriteError(w, http.StatusUnprocessableEntity,
			"invalid remote-access configuration", problems(invalid.Errors)...)
		return
	}

	redacted := stored.Redacted()
	body := endpointResponse{Impact: impact, Reasons: reasons, Config: &redacted}

	switch {
	case dryRun:
		core.WriteJSON(w, http.StatusOK, body)
	case impact == ImpactDisruptive && !confirm:
		body.Error = &core.ErrorBody{Message: joinReasons(reasons) +
			"; repeat the request with confirm=true to go ahead"}
		core.WriteJSON(w, http.StatusConflict, body)
	default:
		h.Events.Publish(core.Event{Type: core.EventApplied, Module: ModuleName})
		core.WriteJSON(w, http.StatusOK, body)
	}
}

// endpointResponse is a plan-shaped answer for a change that has no plan.
type endpointResponse struct {
	Impact  Impact          `json:"impact"`
	Reasons []string        `json:"reasons,omitempty"`
	Config  *Config         `json:"config,omitempty"`
	Error   *core.ErrorBody `json:"error,omitempty"`
}

// classifyEndpoint answers the only question this field raises: is anybody
// holding a configuration that names the old value.
//
// A fact rather than a guess, the way §5.3.3 requires. A box being set up for
// the first time has handed nobody anything, and warning it about breaking
// configurations that do not exist is how an operator learns to click through
// the dialog that matters.
func classifyEndpoint(previous, next Config) (Impact, []string) {
	if previous.EndpointHost() == next.EndpointHost() || previous.EndpointHost() == "" {
		return ImpactNone, nil
	}

	var reasons []string
	if len(previous.WireGuard.Peers) > 0 {
		reasons = append(reasons, fmt.Sprintf(
			"%s already has a configuration naming %s, and it will stop connecting",
			core.Plural(len(previous.WireGuard.Peers), "peer"), previous.EndpointHost()))
	}
	if previous.Socks.Enabled && previous.Socks.Password != "" {
		reasons = append(reasons, "every SOCKS5 link already handed out names the old address and has to be replaced")
	}
	if len(reasons) == 0 {
		return ImpactNone, nil
	}
	return ImpactDisruptive, reasons
}

// --- status -----------------------------------------------------------------

type moduleStatus struct {
	// Endpoint is the address devices dial, shared by both ways in.
	Endpoint string `json:"endpoint,omitempty"`

	// Enabled, Drifted and Blockers are this module's answer to the three
	// questions every module's /status answers at the top level, and they are
	// here because this is the one module whose real answers are a level down.
	//
	// Remote access is two objects (docs/remote.md §1) — a tunnel and a proxy —
	// each with its own switch and its own drift, and nothing above them said
	// whether the module as a whole was on or matched. A generic client cannot
	// know that: internal/cli's fix.go decodes `enabled`, `drifted` and
	// `blockers` from whatever module it is looking at, precisely so it needs no
	// import of any of them, and against this module it decoded three zero
	// values and skipped it — silently, since a module that is off is the
	// ordinary case and looks identical.
	//
	// Either half being on makes the module on, and either half having drifted
	// makes it drifted, because the operator-facing sentence is "something you
	// switched on is not what is on the box" and one of two is enough for that.
	// `olr remote status` then says which.
	Enabled  bool           `json:"enabled"`
	Drifted  bool           `json:"drifted"`
	Blockers []core.Blocker `json:"blockers,omitempty"`

	Tunnel tunnelStatus `json:"tunnel"`
	Proxy  socksStatus  `json:"proxy"`

	AsOf time.Time `json:"as_of"`
}

func (h HTTP) getStatus(w http.ResponseWriter, r *http.Request) {
	cfg, err := h.Tunnel.Load()
	if err != nil {
		core.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	tunnel := h.tunnelStatusFor(r, cfg)
	proxy := h.socksStatusFor(r, cfg)

	core.WriteJSON(w, http.StatusOK, moduleStatus{
		Endpoint: cfg.Endpoint,
		Enabled:  tunnel.Enabled || proxy.Enabled,
		Drifted:  tunnel.Drifted || proxy.Drifted,

		// The tunnel's, which are the module's: Blockers() is what
		// POST /blockers/fix clears, and tunnelStatusFor reports the same list.
		// Naming it here rather than copying from tunnel keeps the two surfaces
		// reading from one source.
		Blockers: Blockers(),

		Tunnel: tunnel,
		Proxy:  proxy,
		AsOf:   stamp(),
	})
}

// --- clearing what is in the way ---------------------------------------------

// fixRequest names which blockers to clear. Absent or empty means all of them.
type fixRequest struct {
	IDs []string `json:"ids,omitempty"`
}

// fixResponse carries the steps whether or not they all landed, for the §5.3.2
// reason every apply response does: there is no rollback.
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

	steps, err := core.FixBlockers(r.Context(), Blockers(), req.IDs)
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

// --- helpers ----------------------------------------------------------------

// editError carries the status an edit failure should be reported with, so that
// "no such device" is a 404 and a malformed patch is a 400 without the write
// path having to know which edits can produce which.
type editError struct {
	status int
	err    error
}

func (e editError) Error() string { return e.err.Error() }
func (e editError) Unwrap() error { return e.err }

func notFound(err error) error   { return editError{status: http.StatusNotFound, err: err} }
func badRequest(err error) error { return editError{status: http.StatusBadRequest, err: err} }

// conflict is for a request refused by what is already stored rather than by
// its own shape — a name another device holds. Not a bad request: nothing here
// is malformed, and the answer is a different value, not a corrected one.
func conflict(err error) error { return editError{status: http.StatusConflict, err: err} }

func statusFor(err error) int {
	var e editError
	if errors.As(err, &e) {
		return e.status
	}
	return http.StatusUnprocessableEntity
}

// gateParams reads the two query parameters every mutating route carries.
func gateParams(r *http.Request) (dryRun, confirm bool, err error) {
	if dryRun, err = boolParam(r, "dry_run"); err != nil {
		return false, false, err
	}
	confirm, err = boolParam(r, "confirm")
	return dryRun, confirm, err
}

// boolParam reads a flag-style query parameter.
//
// Bare presence — `?confirm` — is true, because that is how a flag reads in a
// URL and a caller who wrote it meant it. A malformed value is refused rather
// than ignored: a client that meant to confirm a revocation and mistyped is
// much better off hearing about it than having the change quietly held or,
// worse, quietly made.
func boolParam(r *http.Request, name string) (bool, error) {
	q := r.URL.Query()
	if !q.Has(name) {
		return false, nil
	}
	raw := q.Get(name)
	if raw == "" {
		return true, nil
	}
	v, err := strconv.ParseBool(raw)
	if err != nil {
		return false, fmt.Errorf("%s must be true or false, not %q", name, raw)
	}
	return v, nil
}

// joinReasons renders a refusal's reasons as one sentence.
func joinReasons(reasons []string) string {
	if len(reasons) == 0 {
		return "this would invalidate something already handed out"
	}
	out := reasons[0]
	for _, r := range reasons[1:] {
		out += "; " + r
	}
	return out
}

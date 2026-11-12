package gateway

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/core"
	"github.com/open-linux-router/open-linux-router/internal/gateway/nat"
)

// The module's REST surface (design.md §3.2 rule 2, §6.2).
//
// Thin, like internal/dhcp's and internal/dns's: every decision was already
// made and tested in config.go, validate.go, render.go and plan.go, and this
// file only translates between those and HTTP. A rule that appears here and
// nowhere else is in the wrong place, because the CLI would not get it.

// HTTP serves the module. Its fields are what the module needs from core
// (design.md §3.1 — modules call core, not the reverse).
type HTTP struct {
	World    *WorldTraffic
	Activity *Activity
	Latency  *LatencyMonitor

	Applier Applier

	// Lock is core's one global apply lock (§3.6). Writes take it; reads never
	// do.
	Lock *core.Lock

	// Events is where a change is announced so other clients re-read.
	Events *core.Events

	// Watch is called after a successful apply. The daemon uses it to reconcile
	// probes and refresh the DNS relay's effective exit selection. Nil is legal.
	Watch func(Config)

	// NAT serves the other half of this module — the port forwards, and with
	// them the `olr_nat` table (internal/gateway/nat). Its routes are appended
	// to this module's, under the same prefix, so that one module has one
	// mount point however many packages implement it.
	//
	// The zero value serves nothing, which is what the conformance walk needs:
	// it calls Routes() on a zero HTTP and must not have to build a kernel to
	// do it.
	NAT *nat.HTTP
}

// Routes is the module's surface, declared as data so that it can be
// enumerated rather than only served (core.Route).
//
// Core mounts these with the /api/gateway prefix stripped.
func (h HTTP) Routes() []core.Route {
	// The gate every mutating route on this module carries (§6.2): ask first
	// with dry_run, go ahead with a disruptive plan only with confirm. This is
	// the module that already implements it; dhcp, dns and devices still owe
	// it.
	gate := []core.QueryParam{
		{
			Name: "dry_run", Type: "boolean",
			Summary: "Answer with the plan and change nothing.",
		},
		{
			Name: "confirm", Type: "boolean",
			Summary: "Go ahead even if the plan is disruptive. Without this a disruptive change is refused, and the plan is returned instead.",
		},
	}

	routes := []core.Route{
		{Method: "GET", Path: "/world-traffic", Tool: "show world traffic", Summary: "Top forty remote IPs by observed connection bytes over the last thirty seconds.", Handler: h.getWorldTraffic},
		{Method: "GET", Path: "/latency", Tool: "show latency", Summary: "Latest router HTTPS response measurements and selected websites.", Handler: h.getLatency},
		{Method: "POST", Path: "/latency/sites/{name}/measure", Summary: "Probe one monitored site now instead of waiting for the next round.", Handler: h.postLatencySiteMeasure},
		{Method: "POST", Path: "/latency/measure", Query: []core.QueryParam{{Name: "category", Type: "string", Summary: "Only a catalog category, custom, or empty for every site."}}, Summary: "Probe every monitored site now instead of waiting for the next round.", Handler: h.postLatencyMeasureAll},
		{Method: "PUT", Path: "/latency/sites/{name}/pin", Summary: "Pin or unpin a monitored site on the homepage.", Body: core.BodyFull, Mutating: true, Handler: h.putLatencySitePin},
		{Method: "PUT", Path: "/latency/target", Summary: "Choose an Internet latency target; empty selects the fastest once.", Body: core.BodyFull, Mutating: true, Handler: h.putLatencyTarget},
		{Method: "GET", Path: "/latency/sites/{name}/icon", Summary: "Discover a monitored site icon.", Handler: h.getLatencyIcon},
		{Method: "PUT", Path: "/latency/sites", Summary: "Replace custom HTTPS response targets.", Body: core.BodyFull, Mutating: true, Handler: h.putLatencySites},
		// Intent, whole document. Still the way to restore a backup or make
		// several changes at once; the routes below are additions, not
		// replacements.
		{
			Method: "GET", Path: "/config", Tool: "show config",
			Summary: "Show exits, per-network and per-client assignments, and defaults.",
			Handler: h.getConfig,
		},
		{
			Method: "PUT", Path: "/config",
			Summary:  "Replace the whole gateway configuration.",
			Body:     core.BodyFull,
			Query:    gate,
			Mutating: true,
			Handler:  h.putConfig,
		},
		{
			Method: "PATCH", Path: "/config",
			Summary: "Change named scalar gateway fields — enabled, default, stats — and leave the rest alone. " +
				"Cannot edit exits or assignments; use the item routes for those.",
			Body:     core.BodyRelaxed,
			Query:    gate,
			Mutating: true,
			Handler:  h.patchConfig,
		},

		{Method: "PUT", Path: "/destinations/{address}/{bits}", Summary: "Route a destination IP prefix through an exit.", Query: gate, Mutating: true, Handler: h.putDestination},
		{Method: "DELETE", Path: "/destinations/{address}/{bits}", Summary: "Remove a destination IP routing rule.", Query: gate, Mutating: true, Handler: h.deleteDestination},

		// Intent, one item at a time.
		//
		// These exist because of RFC 7386's one surprising rule: a merge patch
		// replaces an array wholesale rather than merging into it. So
		// `enabled`, `default` and `stats` are perfectly well served by PATCH
		// above and get no route here, while `exits` and `interfaces` cannot be
		// — a PATCH that meant to edit one exit would take the others' traffic
		// with it.
		//
		// The deeper reason is that without them the *edit* happens in the
		// client: every caller loads the document, splices the list itself, and
		// sends the whole thing back. That is two requests where the lock only
		// covers the second, and it is a rule — Config.Rename's cascade, Upsert
		// keeping an exit's slot — reimplemented once per client. Here it is one
		// request under one lock, calling the one implementation in config.go.
		{
			Method: "PUT", Path: "/exits/{name}",
			Summary:  "Create or replace one exit, leaving every other exit alone.",
			Query:    gate,
			Mutating: true,
			Handler:  h.putExit,
		},
		{
			Method: "DELETE", Path: "/exits/{name}",
			Summary:  "Remove one unused exit; network and client references are refused.",
			Query:    gate,
			Mutating: true,
			Handler:  h.deleteExit,
		},
		{
			Method: "PUT", Path: "/assignments/{interface}",
			Summary:  "Send one network's traffic out a named exit.",
			Query:    gate,
			Mutating: true,
			Handler:  h.putAssignment,
		},
		{
			Method: "DELETE", Path: "/assignments/{interface}",
			Summary:  "Stop routing one network out its own exit, returning it to the default.",
			Query:    gate,
			Mutating: true,
			Handler:  h.deleteAssignment,
		},
		{
			Method: "PUT", Path: "/clients/{mac}",
			Summary: "Override one client's way out; an empty exit explicitly uses the normal route.",
			Query:   gate, Mutating: true, Handler: h.putClient,
		},
		{
			Method: "DELETE", Path: "/clients/{mac}",
			Summary: "Return a client to its network's way out.",
			Query:   gate, Mutating: true, Handler: h.deleteClient,
		},

		// Dry run. A POST because it takes a body, not because it changes
		// anything.
		{
			Method: "POST", Path: "/plan", Tool: "show plan",
			Summary: "Show what a routing change would do without doing it. " +
				"An empty body plans the stored configuration, which answers whether the kernel has drifted.",
			Body:    core.BodyRelaxed,
			Handler: h.postPlan,
		},

		// Re-apply stored intent without changing it. This is the repair path
		// design.md §5.3.2 asks for in place of rollback: if an apply failed
		// halfway, or somebody ran `ip rule del` by hand, this finishes the
		// job.
		{
			Method: "POST", Path: "/apply",
			Summary: "Re-program the kernel from the stored gateway configuration, changing no intent. " +
				"This is the repair path for a half-applied change or rules removed by hand.",
			Mutating: true,
			Handler:  h.postApply,
		},

		// Observed. Never stored, always stamped with as_of (§4.5).
		{
			Method: "GET", Path: "/status", Tool: "status",
			Summary: "Show each exit's health, what the kernel is actually holding, " +
				"and whether it still matches the stored configuration.",
			Handler: h.getStatus,
		},

		// The routing table itself. Read only — there is no editor behind this
		// route and nothing here plans a change (§2.4: a static route is its own
		// object, and this page reports the tables everyone else's routes are
		// already in rather than becoming a second place to write them).
		{
			Method: "GET", Path: "/routes", Tool: "show routes",
			Summary: "Show every route in every table: the main table, each exit's own table, " +
				"and any table a rule outside olr points at.",
			Handler: h.getRoutes,
		},

		// Per-device and per-exit bytes, read from the kernel on every request.
		// Separate from /status because it is asked far more often and costs a
		// walk of every device on the network.
		{
			Method: "GET", Path: "/traffic", Tool: "show traffic",
			Summary: "Show bytes counted per client and per exit, read from the kernel. " +
				"Reports counting:false rather than failing when the counters cannot be read.",
			Handler: h.getTraffic,
		},
		{Method: "GET", Path: "/activity", Tool: "show activity", Summary: "Show retained half-hour traffic samples for one client, or for the requested addresses, over the last 90 days.", Handler: h.getActivity},
	}

	// The NAT half's routes, under the same prefix. Appended rather than
	// interleaved so that `olr routes` reads in the order the document does:
	// the routing half, then the translation half.
	if h.NAT != nil {
		routes = append(routes, h.NAT.Routes()...)
	}
	return routes
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

// putConfig replaces the whole document.
func (h HTTP) putConfig(w http.ResponseWriter, r *http.Request) {
	data, err := core.ReadBody(w, r)
	if err != nil {
		core.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	cfg, err := UnmarshalConfig(data)
	if err != nil {
		core.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	// The loaded document is discarded rather than consulted, which is what
	// "replace" means. It still goes through mutate so that every write on this
	// module takes the lock in the same place and in the same order.
	h.mutate(w, r, func(cur *Config) error { *cur = cfg; return nil })
}

// patchConfig changes named fields and leaves the rest alone.
//
// Same RFC 7386 array semantics as the other modules: a patch containing
// "exits" replaces the whole list rather than merging into it, which is why
// editing one exit goes to PUT /exits/{name} rather than through here. Worth
// knowing on this module in particular, because a patch that dropped the other
// exits would take their traffic with them.
//
// The merge happens inside mutate's lock, against the document as it is at that
// moment. Reading it out here first — which is what this did — left a window
// where another writer landed between the read and the apply, and the patch
// then wrote back a document built on what it had displaced.
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

	h.mutate(w, r, func(cur *Config) error {
		currentJSON, err := MarshalConfig(*cur)
		if err != nil {
			return err
		}
		merged, err := core.MergePatch(currentJSON, patch)
		if err != nil {
			return badRequest(err)
		}
		cfg, err := UnmarshalConfig(merged)
		if err != nil {
			return badRequest(err)
		}
		*cur = cfg
		return nil
	})
}

// mutate is every write on this module: load, edit, validate, plan, apply —
// all of it inside the one global apply lock (§3.6).
//
// Holding the lock across the *read* as well as the write is the point. The
// alternative, which is what a client doing GET-edit-PUT gets, leaves a window
// between the two halves in which another writer lands and is then silently
// overwritten. There is no revision to make the write conditional on, so the
// lock has to cover both halves or it covers nothing that matters.
//
// Validation runs inside for the same reason. It is pure (§5.3.1), so it could
// be hoisted out to keep a bad request from queueing behind a good one — but it
// has to see the document the edit actually produced, and that document does not
// exist until we hold the lock.
func (h HTTP) mutate(w http.ResponseWriter, r *http.Request, edit func(*Config) error) {
	h.mutateWith(w, r, false, edit)
}

// mutateWith is mutate with the disruptive gate optionally already satisfied.
//
// forceConfirm is set by POST /apply alone. That route re-programs intent the
// operator stored earlier — and confirmed then, if it needed confirming — so
// there is no new decision to put to them. Gating it would mean a box whose
// rules somebody flushed needs an extra flag to be repaired, and the drifted box
// is the one that most needs repairing.
func (h HTTP) mutateWith(w http.ResponseWriter, r *http.Request, forceConfirm bool, edit func(*Config) error) {
	dryRun, err := boolParam(r, "dry_run")
	if err != nil {
		core.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	confirm, err := boolParam(r, "confirm")
	if err != nil {
		core.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	confirm = confirm || forceConfirm

	admin := callerAddr(r)

	stale := false
	var (
		editErr  error
		invalid  Result
		failed   error
		obs      Observed
		plan     Plan
		desired  Desired
		result   ApplyResult
		stored   Config
		applyErr error
		held     bool
	)

	if err := h.Lock.Do(r.Context(), func() error {
		cfg, err := h.Applier.Load()
		if err != nil {
			failed = err
			return nil
		}
		if !core.MatchConfig(w, r, cfg) {
			stale = true
			return nil
		}

		if err := edit(&cfg); err != nil {
			editErr = err
			return nil
		}
		cfg.Normalize()

		if res := Validate(cfg, h.Applier.Links); !res.OK() {
			invalid = res
			return nil
		}

		if obs, err = h.Applier.Observe(r.Context()); err != nil {
			failed = err
			return nil
		}
		plan, desired, err = BuildPlan(cfg, h.Applier.Links, h.Applier.health(), obs, admin)
		if err != nil {
			failed = err
			return nil
		}

		// Two reasons to stop with the plan and write nothing. A dry run asked
		// the question without wanting the answer acted on; an unconfirmed
		// disruptive change is §5.3.3's one interruption — the operator is
		// about to move traffic that is flowing, possibly their own connection,
		// and that is the single case worth a second round trip.
		if dryRun || (plan.Impact == ImpactDisruptive && !confirm) {
			held = true
			// Re-read rather than keeping the value from before the edit. The
			// edit mutates Exits and Interfaces in place, and a Config copied
			// off the top shares those arrays — so the "this is what is still
			// stored" half of the answer would have quietly moved with it.
			stored, _ = h.Applier.Load()
			return nil
		}

		if err := core.ProtectConnectivity(r.Context(), plan.Impact == ImpactDisruptive); err != nil {
			failed = err
			return nil
		}
		result, stored, applyErr = h.Applier.ApplyPlanned(r.Context(), cfg, plan, desired)
		return nil
	}); err != nil {
		core.WriteError(w, http.StatusServiceUnavailable,
			"timed out waiting for the apply lock: "+err.Error())
		return
	}

	if stale {
		return
	}

	switch {
	case failed != nil:
		core.WriteError(w, http.StatusInternalServerError, failed.Error())
		return
	case editErr != nil:
		core.WriteError(w, statusFor(editErr), editErr.Error())
		return
	case !invalid.OK():
		core.WriteError(w, http.StatusUnprocessableEntity,
			"invalid gateway configuration", problems(invalid.Errors)...)
		return
	}

	view := viewPlan(plan, obs, desired)

	// A dry run answers with the plan alone, which is the same shape POST /plan
	// gives — a caller that asked "what would this do?" gets one answer to that
	// question regardless of which route it asked down.
	if dryRun {
		core.WriteJSON(w, http.StatusOK, view)
		return
	}

	if held {
		core.WriteJSON(w, http.StatusConflict, applyResponse{
			Plan:   view,
			Config: stored,
			Error: &core.ErrorBody{Message: "this would move traffic that is flowing; " +
				"repeat the request with confirm=true to go ahead"},
		})
		return
	}

	if applyErr != nil {
		// A refusal and a failure are different HTTP answers. §6's foreign-rule
		// refusal is a conflict the operator has to resolve elsewhere, not
		// something that went wrong here, and 409 is the word for that.
		//
		// It is the same status the unconfirmed case above returns, and the two
		// are told apart by the body rather than by a second code: `blocked` is
		// set when another program owns the routing table, and `impact` is
		// `disruptive` when we are only waiting to be told to go ahead. A client
		// has to read the plan either way to say anything useful.
		status := http.StatusInternalServerError
		if plan.Blocked != "" {
			status = http.StatusConflict
		}
		core.WriteJSON(w, status, applyResponse{
			Plan:   view,
			Steps:  result.Steps,
			Config: stored,
			Error:  &core.ErrorBody{Message: applyErr.Error()},
		})
		return
	}

	if h.Watch != nil {
		h.Watch(stored)
	}
	if !view.Empty {
		h.Events.Publish(core.Event{Type: core.EventApplied, Module: ModuleName})
	}

	core.WriteJSON(w, http.StatusOK, applyResponse{
		Plan: view, Steps: result.Steps, Config: stored,
	})
}

// --- dry run and repair ---------------------------------------------------

// postPlan answers "what would this do?" without doing it. An empty body plans
// the stored intent, which is the §5.4 drift check.
func (h HTTP) postPlan(w http.ResponseWriter, r *http.Request) {
	data, err := core.ReadBody(w, r)
	if err != nil {
		core.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	current, err := h.Applier.Load()
	if err != nil {
		core.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	desiredCfg := current
	if len(bytes.TrimSpace(data)) > 0 {
		desiredCfg, err = UnmarshalConfig(data)
		if err != nil {
			core.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
	}

	if res := Validate(desiredCfg, h.Applier.Links); !res.OK() {
		core.WriteError(w, http.StatusUnprocessableEntity,
			"invalid gateway configuration", problems(res.Errors)...)
		return
	}

	obs, err := h.Applier.Observe(r.Context())
	if err != nil {
		core.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	plan, desired, err := BuildPlan(desiredCfg, h.Applier.Links, h.Applier.health(), obs, callerAddr(r))
	if err != nil {
		core.WriteError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}

	core.WriteJSON(w, http.StatusOK, viewPlan(plan, obs, desired))
}

// postApply re-applies stored intent unchanged. The edit is the identity: what
// gets programmed is whatever is on disk.
func (h HTTP) postApply(w http.ResponseWriter, r *http.Request) {
	h.mutateWith(w, r, true, func(*Config) error { return nil })
}

// --- one item at a time ----------------------------------------------------

// putExit adds an exit, replaces one, or renames one.
//
// Renaming is the case worth spelling out: an exit's name is referenced by
// Config.Default and by every assignment, so changing it is a change in three
// places at once. Config.Rename does all three together and exists for exactly
// this — a name changed in one place and not the others is how a rename turns
// into an outage. Routing a mismatched body name here is what finally connects
// it to a caller; before this route, nothing but a test ever called it.
func (h HTTP) putExit(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")

	var exit Exit
	if err := core.DecodeJSON(w, r, &exit); err != nil {
		core.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	// An omitted name means "the one in the path", so the common case does not
	// have to say it twice.
	if exit.Name == "" {
		exit.Name = name
	}

	h.mutate(w, r, func(cfg *Config) error {
		if exit.Name != name {
			if _, ok := cfg.Find(name); !ok {
				return notFound(unknownExit(cfg, name))
			}
			if _, taken := cfg.Find(exit.Name); taken {
				return badRequest(fmt.Errorf(
					"cannot rename %q to %q: there is already an exit called %q",
					name, exit.Name, exit.Name))
			}
			// Rename first, so the references follow; Upsert then applies the
			// rest of the body. Upsert keeps the slot either way, which is what
			// stops an edit from moving the exit's route table out from under
			// every flow currently marked for it.
			cfg.Rename(name, exit.Name)
		}
		cfg.Upsert(exit)
		return nil
	})
}

// deleteExit removes an exit.
//
// It deliberately does not clean up references to the name, matching
// Config.Remove and `olr gateway rm exit`: a network still pointing at the
// deleted exit is a validation error naming both ends, which beats silently
// re-pointing somebody's phones at the modem because an exit was removed in
// another window.
func (h HTTP) deleteExit(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	h.mutate(w, r, func(cfg *Config) error {
		if !cfg.Remove(name) {
			return notFound(unknownExit(cfg, name))
		}
		return nil
	})
}

// assignmentBody is the body of PUT /assignments/{interface}.
//
// An empty exit is a real value and not a missing one: it records "this network
// explicitly follows the box-wide setting", which reads differently on screen
// from a network nobody has thought about. DELETE says the second thing.
type assignmentBody struct {
	Exit string `json:"exit"`
}

func (h HTTP) putAssignment(w http.ResponseWriter, r *http.Request) {
	iface := r.PathValue("interface")

	var body assignmentBody
	if err := core.DecodeJSON(w, r, &body); err != nil {
		core.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	h.mutate(w, r, func(cfg *Config) error {
		// Naming an exit that does not exist is caught by Validate with a
		// message listing the ones that do, so there is no check here that
		// would only duplicate it in a worse place.
		cfg.SetAssignment(iface, body.Exit)
		return nil
	})
}

func (h HTTP) deleteAssignment(w http.ResponseWriter, r *http.Request) {
	iface := r.PathValue("interface")
	h.mutate(w, r, func(cfg *Config) error {
		if !cfg.RemoveAssignment(iface) {
			return notFound(fmt.Errorf("%q has no exit of its own", iface))
		}
		return nil
	})
}

func (h HTTP) putClient(w http.ResponseWriter, r *http.Request) {
	mac, err := core.NormalizeMAC(r.PathValue("mac"))
	if err != nil {
		core.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	var body assignmentBody
	if err := core.DecodeJSON(w, r, &body); err != nil {
		core.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.mutate(w, r, func(cfg *Config) error {
		cfg.SetDevice(mac, body.Exit)
		return nil
	})
}

func (h HTTP) deleteClient(w http.ResponseWriter, r *http.Request) {
	mac, err := core.NormalizeMAC(r.PathValue("mac"))
	if err != nil {
		core.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.mutate(w, r, func(cfg *Config) error {
		if !cfg.RemoveClient(mac) {
			return notFound(fmt.Errorf("%s has no way out of its own", mac))
		}
		return nil
	})
}

// --- observed -------------------------------------------------------------

func (h HTTP) getStatus(w http.ResponseWriter, r *http.Request) {
	now := time.Now()

	st, err := h.Applier.GetStatus(r.Context())
	if err != nil {
		// Degraded rather than fatal, the way internal/dhcp's getStatus reports
		// each half of its answer independently: a kernel we could not read
		// still leaves the configured exits worth showing, and Known:false says
		// which half is missing.
		core.WriteJSON(w, http.StatusOK, viewStatus(st, nil, now))
		return
	}

	cfg, err := h.Applier.Load()
	var warnings []Problem
	if err == nil {
		warnings = Validate(cfg, h.Applier.Links).Warnings
	}

	core.WriteJSON(w, http.StatusOK, viewStatus(st, warnings, now))
}

func (h HTTP) getRoutes(w http.ResponseWriter, r *http.Request) {
	now := time.Now()

	cfg, err := h.Applier.Load()
	if err != nil {
		core.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	routes, err := h.Applier.Kernel.ReadRoutes(r.Context())
	if err != nil {
		// Degraded rather than fatal, the way getStatus reports a kernel it
		// could not read: the configured exits still name tables worth knowing,
		// and Known:false with the reason says which half is missing rather than
		// claiming the box has no routes.
		core.WriteJSON(w, http.StatusOK, routesView{
			Known:  false,
			Error:  err.Error(),
			Tables: []routeTableView{},
			AsOf:   now,
		})
		return
	}

	core.WriteJSON(w, http.StatusOK, viewRoutes(routes, cfg, now))
}

func (h HTTP) getTraffic(w http.ResponseWriter, r *http.Request) {
	now := time.Now()

	cfg, err := h.Applier.Load()
	if err != nil {
		core.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	usage, err := h.Applier.Traffic(r.Context())
	if err != nil {
		// Degraded rather than fatal, matching getStatus: a kernel we could not
		// read still leaves intent worth showing, and `counting: false` is what
		// says which half is missing. A machine that cannot do this at all —
		// the non-Linux build — is the same answer as one where nothing has
		// been applied yet, which is correct: neither is counting.
		core.WriteJSON(w, http.StatusOK, viewTraffic(cfg, nil, false, now))
		return
	}

	core.WriteJSON(w, http.StatusOK, viewTraffic(cfg, usage, true, now))
}

func (h HTTP) getActivity(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	if raw := query.Get("mac"); raw != "" {
		mac, err := core.NormalizeMAC(raw)
		if err != nil {
			core.WriteError(w, http.StatusBadRequest, "Invalid client hardware address")
			return
		}
		if h.Activity == nil {
			core.WriteError(w, http.StatusServiceUnavailable, "Activity unavailable")
			return
		}
		core.WriteJSON(w, http.StatusOK, h.Activity.view(mac, nil))
		return
	}
	values := query["address"]
	if len(values) > 32 {
		core.WriteError(w, http.StatusBadRequest, "Too many addresses")
		return
	}
	addresses := make([]netip.Addr, 0, len(values))
	seen := make(map[netip.Addr]bool)
	for _, value := range values {
		addr, err := netip.ParseAddr(value)
		if err != nil {
			core.WriteError(w, http.StatusBadRequest, "Invalid address")
			return
		}
		addr = addr.Unmap()
		if !seen[addr] {
			addresses = append(addresses, addr)
			seen[addr] = true
		}
	}
	if h.Activity == nil {
		core.WriteError(w, http.StatusServiceUnavailable, "Activity unavailable")
		return
	}
	core.WriteJSON(w, http.StatusOK, h.Activity.view("", addresses))
}

// --- plumbing --------------------------------------------------------------

// editError is a refusal from an edit that knows its own HTTP status.
//
// Without it every edit failure would come back as one status, and a DELETE
// naming an exit that is not there would read the same as a malformed patch
// body. Those are different mistakes and the caller fixes them differently.
type editError struct {
	status int
	err    error
}

func (e editError) Error() string { return e.err.Error() }
func (e editError) Unwrap() error { return e.err }

// notFound marks an edit that named something the configuration does not have.
func notFound(err error) error { return editError{http.StatusNotFound, err} }

// badRequest marks an edit refused because the request itself was malformed,
// rather than because the configuration it would produce is invalid.
func badRequest(err error) error { return editError{http.StatusBadRequest, err} }

// statusFor is the status an edit's refusal deserves. Anything that did not say
// is treated as a config the caller could have got right — 422, matching the
// answer a failed Validate gives.
func statusFor(err error) int {
	var e editError
	if errors.As(err, &e) {
		return e.status
	}
	return http.StatusUnprocessableEntity
}

// boolParam reads a flag-style query parameter.
//
// Bare presence — `?confirm` — is true, because that is how a flag reads in a
// URL and a caller who wrote it meant it. A malformed value is refused rather
// than ignored, for the reason internal/dns gives about ?limit=: a client that
// meant to confirm a disruptive change and mistyped is much better off hearing
// about it than having the change quietly held or, worse, quietly made.
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

// callerAddr is the address the request came from, for §5.1's lockout check.
//
// The zero value for a caller over the unix socket, which is the common case
// for `olr` and is genuinely not at risk: a local caller cannot be disconnected
// by a routing change. Getting this wrong in the other direction would be worse
// than not having it — an invented address could match a network and make every
// apply from the socket look disruptive.
func callerAddr(r *http.Request) netip.Addr {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return netip.Addr{}
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}
	}
	return addr.Unmap()
}

func (h HTTP) getLatency(w http.ResponseWriter, r *http.Request) {
	if h.Latency == nil {
		core.WriteJSON(w, http.StatusOK, LatencySnapshot{State: "unavailable", Sites: []LatencySite{}, Custom: []LatencySite{}})
		return
	}
	snapshot := h.Latency.Snapshot()
	if h.Latency.Custom != nil {
		snapshot.Custom = h.Latency.Custom.Snapshot()
	}
	core.WriteJSON(w, http.StatusOK, snapshot)
}

// postLatencySiteMeasure is one monitored site's "measure now". It is a POST
// because it starts work, not because it changes configuration: the site keeps
// its URL, exit and icon, and the result is ephemeral.
func (h HTTP) postLatencySiteMeasure(w http.ResponseWriter, r *http.Request) {
	if h.Latency == nil || h.Latency.Custom == nil {
		core.WriteError(w, http.StatusServiceUnavailable, "latency monitor unavailable")
		return
	}
	if err := h.Latency.Custom.Measure(r.PathValue("name")); err != nil {
		core.WriteError(w, http.StatusNotFound, err.Error())
		return
	}
	snapshot := h.Latency.Snapshot()
	snapshot.Custom = h.Latency.Custom.Snapshot()
	core.WriteJSON(w, http.StatusAccepted, snapshot)
}

// postLatencyMeasureAll is the page's Test all. Like the per-site route, it
// starts probes and leaves configuration alone.
func (h HTTP) postLatencyMeasureAll(w http.ResponseWriter, r *http.Request) {
	if h.Latency == nil || h.Latency.Custom == nil {
		core.WriteError(w, http.StatusServiceUnavailable, "latency monitor unavailable")
		return
	}
	category := r.URL.Query().Get("category")
	if !validLatencyCategory(category) {
		core.WriteError(w, http.StatusBadRequest, "unknown latency category")
		return
	}
	if err := h.Latency.Custom.MeasureAll(category); err != nil {
		core.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	snapshot := h.Latency.Snapshot()
	snapshot.Custom = h.Latency.Custom.Snapshot()
	core.WriteJSON(w, http.StatusAccepted, snapshot)
}

func (h HTTP) putLatencySitePin(w http.ResponseWriter, r *http.Request) {
	if h.Latency == nil || h.Latency.Custom == nil || h.Lock == nil {
		core.WriteError(w, http.StatusServiceUnavailable, "latency monitor unavailable")
		return
	}
	var body struct {
		Pinned *bool `json:"pinned"`
	}
	if err := core.DecodeJSON(w, r, &body); err != nil {
		core.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if body.Pinned == nil {
		core.WriteError(w, http.StatusBadRequest, "pinned must be a boolean")
		return
	}
	if err := h.Lock.Do(r.Context(), func() error {
		changed, err := h.Latency.Custom.SetPinned(r.PathValue("name"), *body.Pinned)
		if err != nil {
			return err
		}
		if changed && h.Events != nil {
			h.Events.Publish(core.Event{Type: core.EventApplied, Module: ModuleName})
		}
		snapshot := h.Latency.Snapshot()
		snapshot.Custom = h.Latency.Custom.Snapshot()
		core.WriteJSON(w, http.StatusOK, snapshot)
		return nil
	}); err != nil {
		status := http.StatusServiceUnavailable
		if errors.Is(err, errLatencySiteNotFound) {
			status = http.StatusNotFound
		}
		core.WriteError(w, status, err.Error())
	}
}

func (h HTTP) putLatencyTarget(w http.ResponseWriter, r *http.Request) {
	if h.Latency == nil {
		core.WriteError(w, http.StatusServiceUnavailable, "latency monitor unavailable")
		return
	}
	var body struct {
		Target *string `json:"target"`
	}
	if err := core.DecodeJSON(w, r, &body); err != nil {
		core.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if body.Target == nil || !validLatencyTarget(*body.Target) {
		core.WriteError(w, http.StatusBadRequest, "choose a listed Internet target")
		return
	}
	if err := h.Lock.Do(r.Context(), func() error { return h.Latency.SetTarget(*body.Target) }); err != nil {
		core.WriteError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	core.WriteJSON(w, http.StatusOK, h.Latency.Snapshot())
}

func (h HTTP) putLatencySites(w http.ResponseWriter, r *http.Request) {
	if h.Latency == nil || h.Latency.Custom == nil {
		core.WriteError(w, http.StatusServiceUnavailable, "latency monitor unavailable")
		return
	}
	var sites []CustomLatencySite
	if err := core.DecodeJSON(w, r, &sites); err != nil {
		core.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if sites == nil {
		core.WriteError(w, http.StatusBadRequest, "expected a list of sites")
		return
	}
	if err := h.Lock.Do(r.Context(), func() error {
		if err := h.Latency.Custom.Replace(sites); err != nil {
			core.WriteError(w, http.StatusBadRequest, err.Error())
			return nil
		}
		core.WriteJSON(w, http.StatusOK, h.Latency.Custom.Config())
		return nil
	}); err != nil {
		core.WriteError(w, http.StatusServiceUnavailable, err.Error())
	}
}

func (h HTTP) getLatencyIcon(w http.ResponseWriter, r *http.Request) {
	if h.Latency == nil || h.Latency.Custom == nil {
		http.NotFound(w, r)
		return
	}
	data, kind, err := h.Latency.Custom.Icon(r.Context(), r.PathValue("name"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", kind)
	w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	w.Header().Set("Cache-Control", "private, max-age=86400")
	w.Header().Set("Vary", "Authorization")
	w.Write(data)
}

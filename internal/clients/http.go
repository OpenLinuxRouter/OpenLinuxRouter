package clients

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// The module's REST surface (design.md §3.2 rule 2, §6.2).
//
// Thin, like internal/dhcp/http.go: every decision was already made and tested
// in config.go, validate.go, list.go and apply.go, and this file only translates
// between those and HTTP. A rule that appears here and nowhere else is in the
// wrong place, because the CLI would not get it.

// locationTimeout bounds the whole "where is this client" question. It is
// shorter than the device service's own thirty-second read budget on purpose: a
// switch that is unplugged is the common failure, and a page that waits thirty
// seconds to say "we could not ask" is worse than one that says it in twenty.
const locationTimeout = 20 * time.Second

// HTTP serves the module. Its fields are what the module needs from core
// (design.md §3.1 — modules call core, not the reverse).
type HTTP struct {
	Applier Applier

	// Lock is core's one global apply lock (§3.6). Writes take it; reads never
	// do.
	Lock *core.Lock

	// Events is where a change is announced so other clients re-read.
	Events *core.Events
}

// Routes is the module's surface, declared as data so that it can be
// enumerated rather than only served (core.Route).
//
// Core mounts these with the /api/clients prefix stripped.
func (h HTTP) Routes() []core.Route {
	return []core.Route{
		// Intent.
		{
			Method: "GET", Path: "/config", Tool: "show config",
			Summary: "Show the stored client inventory: names, categories and owners.",
			Handler: h.getConfig,
		},
		{
			Method: "PUT", Path: "/config",
			Summary:  "Replace the whole client inventory.",
			Body:     core.BodyFull,
			Mutating: true,
			Handler:  h.putConfig,
		},
		{
			Method: "PATCH", Path: "/config",
			Summary:  "Change named client fields and leave the rest alone.",
			Body:     core.BodyRelaxed,
			Mutating: true,
			Handler:  h.patchConfig,
		},

		// Intent, one item at a time, for the reason internal/gateway/http.go
		// gives: a merge patch replaces an array wholesale, so without these
		// every client would load the document, splice it and send it back —
		// two requests with the lock covering only the second, and the rename
		// cascade reimplemented once per client. Here each is one request under
		// the lock, calling config.go.
		{
			Method: "PUT", Path: "/groups/{name}",
			Summary:  "Create a client group; rename it by sending a different name, or move it inside another by sending a parent.",
			Mutating: true,
			Handler:  h.putGroup,
		},
		{
			Method: "DELETE", Path: "/groups/{name}",
			Summary:  "Remove a client group; its clients and subgroups move up to its parent.",
			Mutating: true,
			Handler:  h.deleteGroup,
		},
		{
			Method: "PUT", Path: "/{mac}",
			Summary:  "Create or replace one client's stored identity, preserving all other clients and groups.",
			Mutating: true, Handler: h.putDevice,
		},
		{
			Method: "DELETE", Path: "/{mac}",
			Summary:  "Forget one client's stored identity; observed presence and other modules' policies are kept.",
			Mutating: true, Handler: h.deleteDevice,
		},
		{
			Method: "PUT", Path: "/groups/{name}/members/{mac}",
			Summary:  "Put one client in a group, creating its entry if it has none.",
			Mutating: true,
			Handler:  h.putGroupMember,
		},
		{
			Method: "DELETE", Path: "/groups/{name}/members/{mac}",
			Summary:  "Take one client out of the group it is in.",
			Mutating: true,
			Handler:  h.deleteGroupMember,
		},

		// Dry run. A POST because it takes a body, not because it changes
		// anything.
		{
			Method: "POST", Path: "/plan", Tool: "show plan",
			Summary: "Show what a change to the client inventory would do without doing it.",
			Body:    core.BodyRelaxed,
			Handler: h.postPlan,
		},

		// The join. Observed half never stored, always stamped (§4.5).
		//
		// There is deliberately no /categories route: the vocabulary already
		// reaches every client through the published schema (schema.go), and a
		// second endpoint serving the same list would be the second source that
		// eventually disagrees with the first.
		{
			Method: "GET", Path: "/list", Tool: "show list",
			Summary: "List every client known on the network, joining stored identity to observed presence: " +
				"who is here now, what address they hold, and when they were last seen.",
			Handler: h.getList,
		},

		// Where, not what. Deliberately not a field on /list: answering it costs
		// a round trip to every switch that can report a forwarding table, and a
		// list of forty clients would pay that forty times on every repaint.
		// Asked one client at a time, it is one round trip per switch.
		{
			Method: "GET", Path: "/{mac}/location", Tool: "show location",
			Summary: "Ask the network equipment which port a client is attached to. " +
				"A switch that cannot report a forwarding table is reported as unable to answer, " +
				"not as having looked and found nothing.",
			Handler: h.getLocation,
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
	h.mutate(w, r, func(current *Config) error { *current = cfg; return nil })
}

// patchConfig changes named fields and leaves the rest alone.
//
// Same RFC 7386 array semantics as internal/dhcp: a patch containing "clients"
// replaces the whole list rather than merging into it, which is why naming one
// device is a PUT of the new document rather than a PATCH of one entry.
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
	h.mutate(w, r, func(current *Config) error {
		next, err := core.PatchConfig(*current, patch, UnmarshalConfig)
		if err != nil {
			return badRequest(err)
		}
		*current = next
		return nil
	})
}

// applyResponse carries the plan alongside the stored result.
//
// There are no Steps, unlike internal/dhcp: storing the document is a single
// atomic write, so there is no half-finished state to report. Saying that with
// an absent field rather than an empty array keeps the difference visible.
type applyResponse struct {
	Plan   planView        `json:"plan"`
	Config Config          `json:"config"`
	Error  *core.ErrorBody `json:"error,omitempty"`
}

// --- one item at a time ----------------------------------------------------

func (h HTTP) putDevice(w http.ResponseWriter, r *http.Request) {
	mac, err := core.NormalizeMAC(r.PathValue("mac"))
	if err != nil {
		core.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	var device Client
	if err := core.DecodeJSON(w, r, &device); err != nil {
		core.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if device.MAC != "" {
		bodyMAC, err := core.NormalizeMAC(device.MAC)
		if err != nil || bodyMAC != mac {
			core.WriteError(w, http.StatusBadRequest, "body MAC must match the device in the URL")
			return
		}
	}
	device.MAC = mac
	h.mutate(w, r, func(cfg *Config) error { cfg.Upsert(device); return nil })
}

func (h HTTP) deleteDevice(w http.ResponseWriter, r *http.Request) {
	mac, err := core.NormalizeMAC(r.PathValue("mac"))
	if err != nil {
		core.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.mutate(w, r, func(cfg *Config) error { cfg.Remove(mac); return nil })
}

// groupBody is the body of PUT /groups/{name}. An omitted name means the one in
// the path, so creating a group does not have to say it twice.
//
// Parent is a pointer because absent and empty mean different things: absent
// leaves the group where it is, and empty moves it to the top.
type groupBody struct {
	Name   string  `json:"name"`
	Parent *string `json:"parent"`
}

// putGroup creates a group, renames one when the body names another, and moves
// one when the body names a parent.
//
// Creating one that already exists is a no-op rather than a conflict: the
// request says "there should be a group called this", and there is. A move
// that would put a group inside itself is refused by Validate, with the path.
func (h HTTP) putGroup(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")

	var body groupBody
	if err := core.DecodeJSON(w, r, &body); err != nil {
		core.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	to := strings.TrimSpace(body.Name)
	if to == "" {
		to = name
	}

	h.mutate(w, r, func(cfg *Config) error {
		if _, ok := cfg.FindGroup(name); !ok {
			if to != name {
				return notFound(fmt.Errorf("there is no group called %q; %s", name, knownGroups(*cfg)))
			}
			cfg.UpsertGroup(Group{Name: name})
		}
		if to != name {
			// Changing only the case of a name is a rename too, so the clash
			// check is on the exact name: "iot" → "IoT" must not collide with
			// itself.
			if _, taken := cfg.FindGroup(to); taken {
				return badRequest(fmt.Errorf(
					"cannot rename %q to %q: there is already a group called %q", name, to, to))
			}
			cfg.RenameGroup(name, to)
		}
		if body.Parent != nil {
			g, _ := cfg.FindGroup(to)
			g.Parent = strings.TrimSpace(*body.Parent)
			cfg.UpsertGroup(g)
		}
		return nil
	})
}

func (h HTTP) deleteGroup(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	h.mutate(w, r, func(cfg *Config) error {
		if !cfg.RemoveGroup(name) {
			return notFound(fmt.Errorf("there is no group called %q; %s", name, knownGroups(*cfg)))
		}
		return nil
	})
}

// Group membership is one field on the client, so it is addressed through the
// group rather than as a sub-resource of the client: `/groups/{name}/members/
// {mac}` cannot collide with `/{mac}` the way `/{mac}/group` and `/groups/
// {name}` do, and it reads as what it is — a set and one of its members.
func (h HTTP) putGroupMember(w http.ResponseWriter, r *http.Request) {
	mac, err := core.NormalizeMAC(r.PathValue("mac"))
	if err != nil {
		core.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	name := strings.TrimSpace(r.PathValue("name"))
	h.mutate(w, r, func(cfg *Config) error {
		// Naming a group that does not exist is caught by Validate, with the
		// list of the ones that do.
		cfg.SetDeviceGroup(mac, name)
		return nil
	})
}

// deleteGroupMember is the inverse. It names the group the client is expected
// to be in, so a stale menu cannot silently empty a membership it never showed.
func (h HTTP) deleteGroupMember(w http.ResponseWriter, r *http.Request) {
	mac, err := core.NormalizeMAC(r.PathValue("mac"))
	if err != nil {
		core.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	name := strings.TrimSpace(r.PathValue("name"))
	h.mutate(w, r, func(cfg *Config) error {
		d, ok := cfg.Find(mac)
		if !ok || d.Group != name {
			return badRequest(fmt.Errorf("client %s is not in group %s", mac, name))
		}
		cfg.SetDeviceGroup(mac, "")
		return nil
	})
}

// mutate loads the stored document, edits it and stores the result, all under
// the apply lock, so that the edit is made against the document it is saved
// over rather than one a concurrent writer has since replaced.
func (h HTTP) mutate(w http.ResponseWriter, r *http.Request, edit func(*Config) error) {
	var (
		stored   Config
		plan     planView
		applyErr error
		refused  error
		invalid  Result
		stale    bool
	)
	if err := h.Lock.Do(r.Context(), func() error {
		current, err := h.Applier.Load()
		if err != nil {
			applyErr = err
			return nil
		}
		if !core.MatchConfig(w, r, current) {
			stale = true
			return nil
		}
		desired := current.Clone()
		if err := edit(&desired); err != nil {
			refused = err
			return nil
		}
		desired.Normalize()
		if res := Validate(desired); !res.OK() {
			invalid = res
			return nil
		}
		plan = buildPlan(current, desired)
		stored, applyErr = h.Applier.Save(desired)
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
	case refused != nil:
		core.WriteError(w, statusFor(refused), refused.Error())
		return
	case !invalid.OK():
		core.WriteError(w, http.StatusUnprocessableEntity,
			"invalid devices configuration", problems(invalid.Errors)...)
		return
	case applyErr != nil:
		core.WriteJSON(w, http.StatusInternalServerError, applyResponse{
			Plan:   plan,
			Config: stored,
			Error:  &core.ErrorBody{Message: applyErr.Error()},
		})
		return
	}

	if !plan.Empty {
		h.Events.Publish(core.Event{Type: core.EventApplied, Module: ModuleName})
	}
	core.WriteJSON(w, http.StatusOK, applyResponse{Plan: plan, Config: stored})
}

// editError carries the status an edit's refusal deserves, the way
// internal/gateway/http.go does.
type editError struct {
	status int
	err    error
}

func (e editError) Error() string { return e.err.Error() }
func (e editError) Unwrap() error { return e.err }

func notFound(err error) error   { return editError{http.StatusNotFound, err} }
func badRequest(err error) error { return editError{http.StatusBadRequest, err} }

func statusFor(err error) int {
	var e editError
	if errors.As(err, &e) {
		return e.status
	}
	return http.StatusUnprocessableEntity
}

// --- dry run --------------------------------------------------------------

// postPlan answers "what would this do?" without doing it. An empty body plans
// the stored intent, which is the §5.4 drift check — and for this module it is
// always empty, because the document is the only thing it owns.
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

	desired := current
	if len(bytes.TrimSpace(data)) > 0 {
		desired, err = UnmarshalConfig(data)
		if err != nil {
			core.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
	}

	if res := Validate(desired); !res.OK() {
		core.WriteError(w, http.StatusUnprocessableEntity,
			"invalid devices configuration", problems(res.Errors)...)
		return
	}

	core.WriteJSON(w, http.StatusOK, buildPlan(current, desired))
}

// --- the list -------------------------------------------------------------

// getLocation asks the equipment where one client is attached.
//
// It returns what each device said, not a single winner: the same MAC can be
// learnt on ports of two switches (a downstream switch between them), and
// picking one would be inventing a topology this router cannot see.
func (h HTTP) getLocation(w http.ResponseWriter, r *http.Request) {
	mac, err := core.NormalizeMAC(r.PathValue("mac"))
	if err != nil {
		core.WriteError(w, http.StatusBadRequest, "invalid MAC address")
		return
	}

	resp := locationResponse{MAC: mac, Locations: []locationView{}, AsOf: time.Now()}
	if h.Applier.Locator != nil {
		// Bounded: the device service allows one read thirty seconds, and a
		// switch that is off must not hold a page open for that long. The
		// devices are asked in parallel, so this bounds the slowest, not the sum.
		ctx, cancel := context.WithTimeout(r.Context(), locationTimeout)
		defer cancel()

		locations, problems, err := h.Applier.Locator.Locations(ctx, mac)
		if err != nil {
			core.WriteError(w, http.StatusInternalServerError, err.Error())
			return
		}
		resp.Problems = problems
		for _, l := range locations {
			resp.Locations = append(resp.Locations, viewLocation(l))
		}
	}

	core.WriteJSON(w, http.StatusOK, resp)
}

func (h HTTP) getList(w http.ResponseWriter, r *http.Request) {
	now := time.Now()

	list, probs, err := h.Applier.List(r.Context())
	if err != nil {
		core.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	resp := listResponse{
		Clients:  make([]deviceView, 0, len(list)),
		Problems: problems(probs),
		AsOf:     now,
	}
	for _, d := range list {
		resp.Clients = append(resp.Clients, viewDevice(d))
	}

	core.WriteJSON(w, http.StatusOK, resp)
}

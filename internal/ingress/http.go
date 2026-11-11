package ingress

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// The module's REST surface (design.md §3.2 rule 2, §6.2).
//
// Deliberately thin: every decision here was made and tested in config.go,
// validate.go, plan.go and apply.go. If a rule appears in this file that is not
// in one of those, it is in the wrong place — the CLI would not get it.

// HTTP serves the module.
type HTTP struct {
	// Applier owns the read and write paths onto the system.
	Applier Applier

	// Lock is core's one global apply lock (§3.6). Writes take it; reads never
	// do.
	Lock *core.Lock

	// Events is where an applied change is announced so the UI can re-read.
	Events *core.Events

	// IconClient is injectable for tests; production uses a bounded HTTPS client.
	IconClient *http.Client
}

// Routes is the module's surface, declared as data so it can be enumerated
// rather than only served. Core mounts these with the /api/ingress prefix
// stripped.
func (h HTTP) Routes() []core.Route {
	// The gate every mutating route carries (§6.2): ask first with dry_run, go
	// ahead with a disruptive plan only with confirm. Established by
	// internal/gateway and followed by its NAT half.
	//
	// It earns its keep here on one operation in particular. Removing a
	// published service takes a URL away from whoever has it open, and that is
	// not recoverable by retrying — so it is exactly §5.3.3's one interruption,
	// and a UI with a delete button needs the daemon to insist rather than
	// trusting that the button asked.
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

	return []core.Route{
		// Intent, whole document.
		{
			Method: "GET", Path: "/config", Tool: "show config",
			Summary: "Show the stored ingress configuration: which services are published, and how certificates are obtained. " +
				"The provider credential is never returned.",
			Handler: h.getConfig,
		},
		{
			Method: "PUT", Path: "/config",
			Summary:  "Replace the whole ingress configuration.",
			Body:     core.BodyFull,
			Query:    gate,
			Mutating: true,
			Handler:  h.putConfig,
		},
		{
			Method: "PATCH", Path: "/config",
			Summary: "Change the certificate settings, or turn ingress on and off, leaving the published services alone. " +
				"Cannot edit services; use the item routes for those.",
			Body:     core.BodyRelaxed,
			Query:    gate,
			Mutating: true,
			Handler:  h.patchConfig,
		},

		// Intent, one service at a time.
		//
		// These exist because of RFC 7386's one surprising rule: a merge patch
		// replaces an array wholesale rather than merging into it. So the
		// certificate settings are well served by PATCH above and get no route
		// here, while `services` cannot be — a PATCH meaning to edit one service
		// would take the others with it.
		//
		// The deeper reason is that without them the *edit* happens in the
		// client: every caller loads the document, splices the list, and sends
		// the whole thing back. That is two requests where the lock only covers
		// the second, and it is a rule — reducing `grafana` and
		// `grafana.home.example.com` to one entry — reimplemented once per
		// client.
		{
			Method: "PUT", Path: "/services/{name}",
			Summary:  "Publish one service, or replace it, leaving every other one alone.",
			Query:    gate,
			Mutating: true,
			Handler:  h.putService,
		},
		{
			Method: "DELETE", Path: "/services/{name}",
			Summary:  "Stop publishing one service. The name stops answering, so this is refused without confirm.",
			Query:    gate,
			Mutating: true,
			Handler:  h.deleteService,
		},

		{
			Method: "GET", Path: "/services/{name}/icon",
			Summary: "Show the best available website icon for an enabled published service.",
			Handler: h.getServiceIcon,
		},

		// Dry run. A POST because it takes a body, not because it changes
		// anything.
		{
			Method: "POST", Path: "/plan", Tool: "show plan",
			Summary: "Show what an ingress change would do without doing it. " +
				"An empty body plans the stored configuration, which answers whether the box has drifted.",
			Body:    core.BodyRelaxed,
			Handler: h.postPlan,
		},

		// Re-apply stored intent without changing it — the repair path
		// design.md §5.3.2 asks for in place of rollback.
		{
			Method: "POST", Path: "/apply",
			Summary: "Re-render and reload the proxy from the stored ingress configuration, changing no intent. " +
				"This is the repair path for a half-applied change or a hand-edited Caddyfile.",
			Mutating: true,
			Handler:  h.postApply,
		},

		// Observed. Never stored, always stamped with as_of (§4.5).
		{
			Method: "GET", Path: "/status", Tool: "status",
			Summary: "Show whether a proxy is installed and running, when its certificate expires and whether renewal is overdue, " +
				"and whether the box still matches the stored configuration.",
			Handler: h.getStatus,
		},
		{
			Method: "GET", Path: "/services", Tool: "show services",
			Summary: "List published services with their URLs and where each one currently points.",
			Handler: h.getServices,
		},
		{
			Method: "GET", Path: "/providers", Tool: "show providers",
			Summary: "List the DNS providers the installed proxy was built with.",
			Handler: h.getProviders,
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
	// Redacted on the way out, always. This is the response a UI renders into a
	// form and the CLI prints to a terminal, and no caller needs the real value
	// back — the only thing that reads it is the renderer, which loads the config
	// itself.
	core.WriteConfig(w, cfg, cfg.Redacted())
}

func (h HTTP) getServiceIcon(w http.ResponseWriter, r *http.Request) {
	cfg, err := h.Applier.Load()
	if err != nil {
		core.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !cfg.Enabled {
		http.NotFound(w, r)
		return
	}
	name := r.PathValue("name")
	var matched *Service
	for i := range cfg.Services {
		if cfg.Services[i].Name == name {
			matched = &cfg.Services[i]
			break
		}
	}
	if matched == nil {
		http.NotFound(w, r)
		return
	}
	if matched.Icon != "" {
		data, kind, err := CustomIcon(matched.Icon)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", kind)
		w.Header().Set("Cache-Control", "private, max-age=86400")
		w.Header().Set("Vary", "Authorization")
		w.Write(data)
		return
	}
	origin := (&url.URL{Scheme: "https", Host: qualify(name, cfg.Domain), Path: matched.LinkPath}).String()
	data, kind, err := DiscoverIcon(r.Context(), h.IconClient, origin)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", kind)
	// The icon is fetched from a published site, not generated by the API.
	// Bound staleness while letting browser cache survive daemon restarts.
	w.Header().Set("Cache-Control", "private, max-age=86400")
	w.Header().Set("Vary", "Authorization")
	w.Write(data)
}

func (h HTTP) putConfig(w http.ResponseWriter, r *http.Request) {
	data, err := core.ReadBody(w, r)
	if err != nil {
		core.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	next, err := UnmarshalConfig(data)
	if err != nil {
		core.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.mutate(w, r, func(cfg *Config) error {
		*cfg = preserveToken(next, *cfg)
		return nil
	})
}

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

	h.mutate(w, r, func(cfg *Config) error {
		currentJSON, err := MarshalConfig(*cfg)
		if err != nil {
			return err
		}
		merged, err := core.MergePatch(currentJSON, patch)
		if err != nil {
			return badRequest(err)
		}
		// Strict again on the merged result, so an unknown key in the patch is
		// still caught rather than smuggled in by the merge.
		next, err := UnmarshalConfig(merged)
		if err != nil {
			return badRequest(err)
		}
		*cfg = preserveToken(next, *cfg)
		return nil
	})
}

func (h HTTP) putService(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")

	var s Service
	if err := core.DecodeJSON(w, r, &s); err != nil {
		core.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	// The path wins over the body, and an omitted name means "the one in the
	// path" so the common case does not have to say it twice. Honouring the URL
	// is the reading that keeps PUT idempotent on the address it was sent to.
	s.Name = name

	h.mutate(w, r, func(cfg *Config) error {
		cfg.SetService(s)
		return nil
	})
}

func (h HTTP) deleteService(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	h.mutate(w, r, func(cfg *Config) error {
		if !cfg.RemoveService(name) {
			// 404, not 422. "There is no service by that name" is a statement
			// about the address, and a UI retrying a delete it already completed
			// should be able to tell that from a refusal.
			return notFound(fmt.Errorf("no published service named %q", name))
		}
		return nil
	})
}

// preserveToken closes the round trip redaction creates.
//
// GET /config returns the credential as `********`. A UI that renders that into
// a form and sends the form back would otherwise *store the mask* — the proxy
// would restart, authenticate with eight asterisks, and renewals would fail from
// then on. Nothing about that is visible at the moment it happens, which is the
// same shape of failure as everything else in this module worth guarding
// against.
//
// So the mask means "unchanged", which is the only thing it can honestly mean.
// The real value is reachable by sending a different one.
func preserveToken(next, current Config) Config {
	if next.Certificate.Token == RedactedToken {
		next.Certificate.Token = current.Certificate.Token
	}
	return next
}

// applyResponse always carries the plan and the steps, successful or not
// (§5.3.2: there is no rollback, so a half-finished change stays half-finished
// and the honest thing is to say which steps landed).
type applyResponse struct {
	Pending  bool            `json:"pending,omitempty"`
	Problems []core.Problem  `json:"problems,omitempty"`
	Plan     planView        `json:"plan"`
	Steps    []Step          `json:"steps,omitempty"`
	Error    *core.ErrorBody `json:"error,omitempty"`

	// Config is what is stored now, redacted. Returned on the refusal path
	// especially: a client that has just been told "no" needs to know the
	// document did not move, and asking again would be a second request against
	// a box whose lock it no longer holds.
	Config *Config `json:"config,omitempty"`
}

// mutate is the one write path every mutating route goes through.
//
// Load, edit, validate, observe, plan, and only then write — all inside the one
// global apply lock (§3.6). The lock has to cover both halves or it covers
// nothing that matters: two clients publishing a service at once would otherwise
// each splice their own copy of the list.
func (h HTTP) mutate(w http.ResponseWriter, r *http.Request, edit func(*Config) error) {
	h.mutateWith(w, r, false, edit)
}

// mutateWith is mutate with the disruptive gate optionally already satisfied.
//
// forceConfirm is set by POST /apply alone. That route re-applies intent the
// operator stored earlier — and confirmed then, if it needed confirming — so
// there is no new decision to put to them. Gating it would mean a box whose
// Caddyfile somebody edited needs an extra flag to be repaired, and the drifted
// box is the one that most needs repairing.
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

	stale := false
	var (
		editErr  error
		invalid  Result
		failed   error
		plan     Plan
		result   ApplyResult
		stored   Config
		applyErr error
		held     bool
		pending  bool
	)

	if lockErr := h.Lock.Do(r.Context(), func() error {
		cfg, err := h.Applier.Load()
		if err != nil {
			failed = err
			return nil
		}
		if !core.MatchConfig(w, r, cfg) {
			stale = true
			return nil
		}
		before := cfg.Clone()
		if err := edit(&cfg); err != nil {
			editErr = err
			return nil
		}
		cfg.Normalize()

		if res := Validate(cfg, h.Applier.DNS, h.Applier.Clients); !res.OK() {
			invalid = res
			if r.Method == http.MethodPut && r.PathValue("name") != "" &&
				canSaveRepair(before, cfg, r.PathValue("name"), h.Applier.DNS, h.Applier.Clients) {
				pending = true
				stored = before
				if !dryRun {
					doc, err := h.Applier.Store.Load()
					if err != nil {
						failed = err
						return nil
					}
					cfg.ResolveClientReferences(h.Applier.Clients)
					data, err := MarshalConfig(cfg)
					if err != nil {
						failed = err
						return nil
					}
					doc.Set(ModuleName, data)
					failed = h.Applier.Store.Save(doc)
					stored = cfg
				}
			}
			return nil
		}

		obs, err := h.Applier.Observe(r.Context())
		if err != nil {
			failed = err
			return nil
		}
		plan, err = BuildPlan(h.Applier.Backend, cfg, h.Applier.DNS, h.Applier.Clients, obs)
		if err != nil {
			failed = err
			return nil
		}

		// Two reasons to stop with the plan and write nothing. A dry run asked
		// the question without wanting it acted on; an unconfirmed disruptive
		// change is §5.3.3's one interruption — a name somebody is using is
		// about to stop answering, and that is the single case worth a second
		// round trip.
		if dryRun || (plan.Impact == ImpactDisruptive && !confirm) {
			held = true
			// Re-read rather than keeping the value from before the edit: the
			// edit mutates Services in place, and a Config copied off the top
			// shares that array — so the "this is what is still stored" half of
			// the answer would have quietly moved with it.
			stored, _ = h.Applier.Load()
			return nil
		}

		result, applyErr = h.Applier.ApplyPlanned(r.Context(), cfg, plan)
		stored, _ = h.Applier.Load()
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
	case editErr != nil:
		core.WriteError(w, statusFor(editErr), editErr.Error())
		return
	case !invalid.OK() && !pending:
		core.WriteError(w, http.StatusUnprocessableEntity,
			"invalid ingress configuration", problems(invalid.Errors)...)
		return
	}

	view := viewPlan(plan)
	if pending {
		if !dryRun {
			h.Events.Publish(core.Event{Type: core.EventApplied, Module: ModuleName})
		}
		redacted := stored.Redacted()
		core.WriteJSON(w, http.StatusOK, applyResponse{Plan: view, Config: &redacted, Pending: true, Problems: problems(invalid.Errors)})
		return
	}

	// A dry run answers with the plan alone, the same shape POST /plan gives, so
	// a caller that asked "what would this do?" gets one answer regardless of
	// which route it asked down.
	if dryRun {
		core.WriteJSON(w, http.StatusOK, view)
		return
	}

	redacted := stored.Redacted()

	if held {
		core.WriteJSON(w, http.StatusConflict, applyResponse{
			Plan:   view,
			Config: &redacted,
			Error: &core.ErrorBody{Message: disruptionMessage(plan) +
				"; repeat the request with confirm=true to go ahead"},
		})
		return
	}

	// Published whenever anything was attempted, including a partial failure —
	// especially then. Something on the box changed and every client's idea of it
	// is now stale.
	if len(result.Steps) > 0 {
		h.Events.Publish(core.Event{Type: core.EventApplied, Module: ModuleName})
	}

	resp := applyResponse{Plan: view, Steps: result.Steps, Config: &redacted}
	if applyErr != nil {
		resp.Error = &core.ErrorBody{Message: applyErr.Error()}
		core.WriteJSON(w, http.StatusInternalServerError, resp)
		return
	}
	core.WriteJSON(w, http.StatusOK, resp)
}

// disruptionMessage says what is about to be lost, in the operator's terms.
//
// The plan already worked this out (plan.go's classify), so repeating its reasons
// rather than inventing a sentence keeps the refusal and the preview saying the
// same thing — a dialog that warns about one name while the plan beneath it
// lists another is worse than no warning.
func disruptionMessage(plan Plan) string {
	if len(plan.Reasons) > 0 {
		return strings.Join(plan.Reasons, "; ")
	}
	return "this would take a published name away"
}

// --- re-apply -------------------------------------------------------------

func (h HTTP) postApply(w http.ResponseWriter, r *http.Request) {
	h.mutateWith(w, r, true, func(*Config) error { return nil })
}

// --- dry run --------------------------------------------------------------

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
		if err == nil {
			// Same round trip as preserveToken, and it matters here too:
			// planning with the mask would report a credential change that a PUT
			// of the same body would not make.
			var current Config
			if current, err = h.Applier.Load(); err == nil {
				cfg = preserveToken(cfg, current)
			}
		}
	}
	if err != nil {
		core.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	plan, err := h.plan(r, cfg)
	if err != nil {
		message := err.Error()
		if len(plan.Validation.Errors) > 0 {
			message = "invalid ingress configuration"
		}
		core.WriteError(w, http.StatusUnprocessableEntity, message,
			problems(plan.Validation.Errors)...)
		return
	}
	core.WriteJSON(w, http.StatusOK, viewPlan(plan))
}

// plan reads fresh system state and diffs cfg against it. No lock: §3.6 says
// reads never take one, and §5.4 requires reading reality rather than a cache
// for drift detection to mean anything.
func (h HTTP) plan(r *http.Request, cfg Config) (Plan, error) {
	obs, err := h.Applier.Observe(r.Context())
	if err != nil {
		return Plan{}, err
	}
	return BuildPlan(h.Applier.Backend, cfg, h.Applier.DNS, h.Applier.Clients, obs)
}

// --- observed -------------------------------------------------------------

type statusResponse struct {
	Problems []core.Problem `json:"problems,omitempty"`
	Enabled  bool           `json:"enabled"`

	// Domain is the suffix published names live under — this module's own
	// Config.Domain, repeated here so an observed read does not have to load
	// the config to find out what the URLs look like.
	Domain string `json:"domain,omitempty"`

	// Service is what systemd knows. Absent, with ServiceError set, when the
	// query itself failed — normal on a developer box with no D-Bus, and it must
	// not take the rest of the answer down with it.
	Service      *ProxyStatus `json:"service,omitempty"`
	ServiceError string       `json:"service_error,omitempty"`

	// Binary is the proxy olr found, and BinaryError says why it found none. olr
	// ships no proxy (binary.go), so "is one present" is a first-class part of
	// this module's health rather than something to discover at apply time.
	Binary      string `json:"binary,omitempty"`
	BinaryError string `json:"binary_error,omitempty"`

	// Certificate is the clock-dependent half of health (docs/ingress.md §8).
	// Reported beside the service state and never folded into it: a proxy that
	// is running perfectly while its renewals fail is exactly the situation this
	// module has to be able to describe.
	Certificate  CertState `json:"certificate"`
	CertError    string    `json:"certificate_error,omitempty"`
	CertWarnings []string  `json:"certificate_warnings,omitempty"`

	Published  int       `json:"published"`
	Drifted    bool      `json:"drifted"`
	Drift      *planView `json:"drift,omitempty"`
	DriftError string    `json:"drift_error,omitempty"`
	AsOf       time.Time `json:"as_of"`
}

func (h HTTP) getStatus(w http.ResponseWriter, r *http.Request) {
	resp := statusResponse{AsOf: stamp()}

	cfg, err := h.Applier.Load()
	if err != nil {
		core.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	resp.Enabled = cfg.Enabled
	resp.Problems = problems(Validate(cfg, h.Applier.DNS, h.Applier.Clients).Errors)
	resp.Published = len(cfg.Services)
	resp.Domain = cfg.Domain

	// Both halves: is there a proxy, and can it do the job. The second is the one
	// that is usually wrong (ErrNoProviders), and a status that only answered the
	// first would report a healthy box that can never get a certificate.
	if binary, err := FindBinary(); err != nil {
		resp.BinaryError = ErrBinaryMissing().Error()
	} else {
		resp.Binary = binary
		if providers, err := ListProviders(r.Context(), binary); err == nil && !HasProviders(providers) {
			resp.BinaryError = ErrNoProviders(binary).Error()
		}
	}

	// Each half of health is reported independently and neither can suppress the
	// other (§5.4).
	if svc, err := h.Applier.Proxy.Status(r.Context()); err != nil {
		resp.ServiceError = err.Error()
	} else {
		resp.Service = &svc
	}

	if certs, err := ReadCertificates(h.Applier.Paths.Data); err != nil {
		resp.CertError = err.Error()
	} else {
		resp.Certificate = CertificateState(certs, resp.Domain, resp.AsOf)
		resp.CertWarnings = certWarnings(cfg, resp.Certificate)
	}

	if plan, err := h.plan(r, cfg); err != nil {
		resp.DriftError = err.Error()
	} else {
		view := viewPlan(plan)
		resp.Drifted = !plan.Empty()
		resp.Drift = &view
	}

	core.WriteJSON(w, http.StatusOK, resp)
}

// certWarnings turns the certificate state into sentences.
//
// An active check, per docs/ingress.md §8: the whole failure mode here is that
// nothing looks wrong for weeks. A number in a JSON field satisfies nobody who
// is not already looking for it, so the condition is also stated in words a
// status line can print without interpreting.
func certWarnings(cfg Config, state CertState) []string {
	if !cfg.Enabled {
		return nil
	}
	switch {
	case !state.Found:
		if len(cfg.Services) == 0 {
			return nil
		}
		return []string{
			"no certificate has been issued yet — this is normal for a few minutes after enabling, " +
				"and permanent if the DNS provider credential is wrong",
		}
	case state.ExpiresIn <= 0:
		return []string{"the certificate has expired, so every published name is refused by browsers now"}
	case state.RenewalOverdue:
		return []string{
			"renewal is overdue: the certificate should already have been replaced and was not, " +
				"so something has been failing quietly. It stops working entirely in " +
				core.Plural(state.ExpiresInDays, "day"),
		}
	}
	return nil
}

type servicesResponse struct {
	Services []serviceView `json:"services"`
	Domain   string        `json:"domain,omitempty"`
	AsOf     time.Time     `json:"as_of"`
}

func (h HTTP) getServices(w http.ResponseWriter, r *http.Request) {
	cfg, err := h.Applier.Load()
	if err != nil {
		core.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	domain := cfg.Domain

	resp := servicesResponse{
		Services: make([]serviceView, 0, len(cfg.Services)),
		Domain:   domain,
		AsOf:     stamp(),
	}
	for _, s := range cfg.Services {
		resp.Services = append(resp.Services, viewService(s, domain, h.Applier.Clients))
	}
	core.WriteJSON(w, http.StatusOK, resp)
}

type providersResponse struct {
	// Binary is which proxy answered. Reported because a box can have more than
	// one Caddy on it — the distro's and the operator's — and "which providers
	// are available" is meaningless without saying which binary was asked.
	Binary    string   `json:"binary,omitempty"`
	Providers []string `json:"providers"`
}

// getProviders asks the operator's binary what it was built with.
//
// There is no list of our own to fall back on, on purpose (providers.go). A
// missing binary is a 503 rather than an empty list: an empty list reads as
// "your proxy supports nothing", which is a different and much more alarming
// statement than "there is no proxy here yet", and only one of them is true.
func (h HTTP) getProviders(w http.ResponseWriter, r *http.Request) {
	binary, err := FindBinary()
	if err != nil {
		core.WriteError(w, http.StatusServiceUnavailable, ErrBinaryMissing().Error())
		return
	}
	providers, err := ListProviders(r.Context(), binary)
	if err != nil {
		core.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// A binary with no providers is reported the same way as no binary, because
	// it leaves the operator in the same place — unable to get a certificate —
	// and a 200 with an empty list would render as an empty dropdown explaining
	// nothing. The message is the one that differs, and it is the message that
	// matters here.
	if !HasProviders(providers) {
		core.WriteError(w, http.StatusServiceUnavailable, ErrNoProviders(binary).Error())
		return
	}
	core.WriteJSON(w, http.StatusOK, providersResponse{Binary: binary, Providers: providers})
}

// --- helpers --------------------------------------------------------------

// editError carries the status an edit failure should be reported with, so that
// "no such service" is a 404 and a malformed patch is a 400 without mutate
// having to know which edits can produce which.
type editError struct {
	status int
	err    error
}

func (e editError) Error() string { return e.err.Error() }
func (e editError) Unwrap() error { return e.err }

func notFound(err error) error   { return editError{status: http.StatusNotFound, err: err} }
func badRequest(err error) error { return editError{status: http.StatusBadRequest, err: err} }

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
// than ignored: a client that meant to confirm a disruptive change and mistyped
// is much better off hearing about it than having the change quietly held or,
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

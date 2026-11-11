package dns

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// Applier turns intent into a running resolver.
//
// There is no rollback here on purpose (design.md §5.2/§5.3.2). If a multi-step
// change fails halfway, the steps that landed stay landed and are reported;
// re-running finishes the job. That is more honest and more debuggable than a
// silent revert, and it is why Steps records outcomes rather than being
// discarded on error.
type Applier struct {
	// Backend renders both daemons' files.
	Backend Backend

	// Links is the window onto the link module.
	Links LinkView

	// Reservations is the window onto the dhcp module's fixed addresses. Nil is
	// legitimate and costs one validation warning — see dhcp.go, which is also
	// where the reason it never reaches the renderer is written down.
	Reservations ReservationView

	// DialIn is the window onto the tunnel's DNS promise (dialin.go): the
	// address every dial-in client configuration names, which this module has
	// to answer on whether or not the operator pinned it. Nil means no tunnel,
	// and adds nothing.
	DialIn DialInView

	// Resolver supervises unbound; Relay supervises our own olr-dnsd.
	//
	// Two, because this module drives two daemons and they fail differently. A
	// blocklist edit reloads the relay and must not touch the resolver; a
	// forwarder change restarts the resolver and must not interrupt the relay's
	// socket.
	Resolver Service
	Relay    Service

	// Paths is the on-disk layout.
	Paths Paths

	// Store is core's configuration document, which owns this module's intent
	// alongside every other module's.
	Store *core.Store

	// Observer reads what the relay saw. Nil means observations are
	// unavailable, which is a legitimate state — a stopped relay is exactly
	// that — and never an error on its own.
	Observer ObserveView

	// PortCheck reports whether something already holds :53. Nil means
	// PortConflict. Injectable so the refusal path is testable without
	// arranging for a real conflict on the build machine.
	PortCheck func() (bool, error)

	// Settle is how long apply watches a started backend before believing it.
	// Zero means DefaultSettle; negative means check once and return.
	Settle time.Duration
}

// DefaultSettle is the post-apply observation window (design.md §11.5).
//
// It is not a guess at how long a daemon takes to start — it is how long we
// watch it *after* systemd says the job is done. unbound rejects a bad config
// at runtime and exits; the relay refuses to bind a port somebody else holds
// and exits. Both look identical to a healthy start for the first fraction of a
// second, and on this module the difference is the whole network's name
// resolution.
const DefaultSettle = 1500 * time.Millisecond

// settleInterval is how often a unit is sampled during that window.
const settleInterval = 250 * time.Millisecond

func (a Applier) settleWindow() time.Duration {
	if a.Settle == 0 {
		return DefaultSettle
	}
	if a.Settle < 0 {
		return 0
	}
	return a.Settle
}

func (a Applier) portCheck() func() (bool, error) {
	if a.PortCheck != nil {
		return a.PortCheck
	}
	return PortConflict
}

// service returns the Service driving a unit.
func (a Applier) service(unit string) (Service, error) {
	switch unit {
	case a.Backend.ResolverUnit():
		return a.Resolver, nil
	case a.Backend.RelayUnit():
		return a.Relay, nil
	}
	return nil, fmt.Errorf("internal: no service for unit %q", unit)
}

// NewApplierAt wires up both backends against a link view, with every rendered
// path relocated under root.
//
// An empty root is the real system. See RootedPaths for why a non-empty one
// exists — it is the development escape hatch, not a supported deployment
// layout. The store is passed in already rooted, because core owns that path.
func NewApplierAt(store *core.Store, links LinkView, reservations ReservationView, root string) (Applier, error) {
	paths := RootedPaths(root)
	backend := NewBackend(paths).WithSource(store.Path())
	backend.Intent = store

	resolver, err := NewService(backend.ResolverUnit())
	if err != nil {
		return Applier{}, err
	}
	relay, err := NewService(backend.RelayUnit())
	if err != nil {
		return Applier{}, err
	}

	return Applier{
		Backend:      backend,
		Links:        links,
		Reservations: reservations,
		Resolver:     resolver,
		Relay:        relay,
		Paths:        paths,
		Store:        store,
		Observer:     NewObserver(paths.ObserveSocket),
	}, nil
}

// Step is one unit of work and how it went.
type Step struct {
	Description string `json:"description"`
	Done        bool   `json:"done"`
	Error       string `json:"error,omitempty"`
}

// ApplyResult is what an apply actually did.
type ApplyResult struct {
	Plan  Plan   `json:"plan"`
	Steps []Step `json:"steps"`
}

// Load reads stored intent out of the configuration document.
func (a Applier) Load() (Config, error) {
	doc, err := a.Store.Load()
	if err != nil {
		return Config{}, err
	}
	return FromDocument(doc)
}

// Observe reads the actual state of the system: what is on disk, whether the
// daemons are running, and who has been resolving through us.
//
// Every field is read fresh. Planning against observation rather than against a
// cached copy of what we last wrote is what makes drift detection free
// (design.md §5.4).
func (a Applier) Observe(ctx context.Context) (Observed, error) {
	obs := Observed{Files: map[string][]byte{}, Units: map[string]UnitState{}}

	// Everything under the directories we own, walked rather than listed. A
	// file we did not render must still be seen, or a policy removed from the
	// config would go on being enforced — the relay reads the directory, not
	// our intentions about it.
	paths := map[string]bool{}
	for _, root := range a.observedRoots() {
		err := filepath.WalkDir(root, func(path string, e fs.DirEntry, err error) error {
			switch {
			case os.IsNotExist(err):
				return nil
			case err != nil:
				return err
			case e.IsDir():
				return nil
			case strings.HasPrefix(e.Name(), "."):
				// core.WriteFileAtomic's temporary files. Skipping them keeps a
				// crashed apply's leftovers out of the plan rather than
				// scheduling a delete for a name that will never recur.
				return nil
			}
			paths[path] = true
			return nil
		})
		if err != nil {
			return Observed{}, fmt.Errorf("reading %s: %w", root, err)
		}
	}
	for p := range paths {
		data, err := os.ReadFile(p)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return Observed{}, fmt.Errorf("reading %s: %w", p, err)
		}
		obs.Files[p] = data
	}

	// A missing service manager is reported as "not known" rather than failing
	// the whole observation: the file half of the answer is still useful, and
	// `olr dns status` surfaces the service error on its own line.
	for _, unit := range a.Backend.Units() {
		svc, err := a.service(unit)
		if err != nil {
			return Observed{}, err
		}
		state := UnitState{}
		if status, err := svc.Status(ctx); err == nil {
			state = UnitState{
				Known:         true,
				Running:       status.Active,
				Starting:      status.State == "activating",
				EnabledAtBoot: status.Enabled,
				Installed:     status.Installed,
			}
		}
		obs.Units[unit] = state
	}

	return obs, nil
}

// backend is the renderer with this applier's facts attached.
//
// Backend stays a value with no methods that reach outside itself, which is what
// lets a test render without a daemon; the applier is where the views live, so
// the join happens here rather than in every BuildPlan caller.
func (a Applier) backend() Backend {
	b := a.Backend
	b.DialIn = a.DialIn
	return b
}

// Plan reports what applying desired would do, without doing it.
func (a Applier) Plan(ctx context.Context, desired Config) (Plan, error) {
	obs, err := a.Observe(ctx)
	if err != nil {
		return Plan{}, err
	}
	return BuildPlan(a.backend(), desired, a.Links, a.Reservations, obs, time.Now())
}

// Drift reports what has changed underneath stored intent.
//
// This is §5.4 in one line: plan the intent we already have against the system
// as it actually is. A non-empty plan is drift.
func (a Applier) Drift(ctx context.Context) (Plan, error) {
	current, err := a.Load()
	if err != nil {
		return Plan{}, err
	}
	return a.Plan(ctx, current)
}

// Apply writes the config and brings both daemons into line with it.
//
// It applies immediately and is in effect on return (design.md §5.1) — there is
// no staged commit to follow up with.
func (a Applier) Apply(ctx context.Context, desired Config) (ApplyResult, error) {
	return a.ApplyChecked(ctx, desired, nil)
}

// ApplyChecked checks the execution plan before storing or programming anything.
// The caller holds the global apply lock.
func (a Applier) ApplyChecked(ctx context.Context, desired Config, check func(Plan) error) (ApplyResult, error) {
	obs, err := a.Observe(ctx)
	if err != nil {
		return ApplyResult{}, err
	}
	plan, err := BuildPlan(a.backend(), desired, a.Links, a.Reservations, obs, time.Now())
	if err != nil {
		return ApplyResult{Plan: plan}, err
	}

	if check != nil {
		if err := check(plan); err != nil {
			return ApplyResult{Plan: plan}, err
		}
	}

	result := ApplyResult{Plan: plan}
	run := func(description string, fn func() error) error {
		err := fn()
		step := Step{Description: description, Done: err == nil}
		if err != nil {
			step.Error = err.Error()
		}
		result.Steps = append(result.Steps, step)
		return err
	}

	// Intent is stored first, before anything it describes. If a later step
	// fails, a re-run has something to finish the job from — which is the whole
	// premise of idempotent re-apply instead of rollback (§5.3.2).
	//
	// Read-modify-write on the shared document, so a save here cannot drop
	// another module's configuration. It is safe without further locking
	// because every config write in the process already holds the one global
	// apply lock (§3.6).
	if err := run("store configuration in "+a.Store.Path(), func() error {
		doc, err := a.Store.Load()
		if err != nil {
			return err
		}
		data, err := MarshalConfig(desired)
		if err != nil {
			return err
		}
		doc.Set(ModuleName, data)
		return a.Store.Save(doc)
	}); err != nil {
		return result, err
	}

	// nothingToDo, not Empty: a change that is only cosmetic is not drift, but
	// it is still a file to bring up to date.
	if plan.nothingToDo() {
		return result, nil
	}

	// Deduplicated, so that two entries naming one directory cannot make the
	// step log an operator reads after a failure repeat a line verbatim.
	// Harmless to run twice, but it makes the reader wonder what they missed.
	//
	// Under the Paths olr ships, these four are now distinct and this collapses
	// nothing: the pair that used to collide was the resolver's config and the
	// relay's, and the resolver's moved to where its confinement lets it read
	// it. The filter stays because Paths is a struct a caller can build — tests
	// do, and RootedPaths is the shipped example rather than the only one — and
	// nothing in its type says the four stay distinct. observedRoots below does
	// still collapse a genuine duplicate, since the relay's config and the
	// hijack ruleset share a directory.
	seen := map[string]bool{}
	for _, dir := range []string{
		filepath.Dir(a.Paths.UnboundConf),
		filepath.Dir(a.Paths.RelayConf),
		a.Paths.PolicyDir,
		filepath.Dir(a.Paths.TrustAnchor),
	} {
		dir = filepath.Clean(dir)
		if seen[dir] {
			continue
		}
		seen[dir] = true
		if err := run("create "+dir, func() error { return os.MkdirAll(dir, 0o755) }); err != nil {
			return result, err
		}
	}

	rendered, err := a.Backend.Render(desired, a.Links)
	if err != nil {
		return result, err
	}
	for _, change := range plan.Changes {
		switch change.Kind {
		case ChangeDelete:
			path := change.Path
			if err := run("remove "+path, func() error {
				err := os.Remove(path)
				if os.IsNotExist(err) {
					return nil
				}
				return err
			}); err != nil {
				return result, err
			}
		default:
			file, ok := rendered.Get(change.Path)
			if !ok {
				return result, fmt.Errorf("internal: planned %s but rendered nothing for it", change.Path)
			}
			if err := run("write "+file.Path, func() error {
				return core.WriteFileAtomic(file.Path, file.Data, file.Mode)
			}); err != nil {
				return result, err
			}
		}
	}

	// Checked before anything is asked of systemd, because the error it would
	// otherwise produce — a bare "Unit olr-dnsd.service not found" from D-Bus —
	// names neither the cause nor the fix. A missing unit file is a packaging
	// problem and no amount of re-applying will resolve it.
	for _, s := range plan.Services {
		if s.Action == ActionNone && s.Enable == nil {
			continue
		}
		unit := s.Unit
		if err := run("check "+unit+" is installed", func() error {
			return a.checkInstalled(ctx, unit)
		}); err != nil {
			return result, err
		}
	}

	// Only checked when the relay is about to bind: a reload or restart of a
	// process that already holds the socket would trip over itself. Gated on
	// the port actually being 53, which is the only port this check speaks to.
	if a.startingRelay(plan) && ListensOnDefaultPort(desired) {
		if err := run(fmt.Sprintf("check nothing else holds port %d", DNSPort), func() error {
			inUse, err := a.portCheck()()
			if err != nil {
				return err
			}
			if inUse {
				return ErrPortInUse()
			}
			return nil
		}); err != nil {
			return result, err
		}
	}

	// Boot-time state before the service actions, so that a backend which then
	// fails to start is at least configured the way the operator asked. The
	// reverse order would leave a box that came up, failed, and would also not
	// have come back after a reboot.
	for _, s := range plan.Services {
		if s.Enable == nil {
			continue
		}
		unit, enable := s.Unit, *s.Enable
		svc, err := a.service(unit)
		if err != nil {
			return result, err
		}
		verb := map[bool]string{true: "enable", false: "disable"}[enable]
		if err := run(verb+" "+unit+" at boot", func() error {
			if enable {
				return svc.Enable(ctx)
			}
			return svc.Disable(ctx)
		}); err != nil {
			return result, err
		}
	}

	for _, s := range a.serviceOrder(plan) {
		if s.Action == ActionNone {
			continue
		}
		unit, action := s.Unit, s.Action
		svc, err := a.service(unit)
		if err != nil {
			return result, err
		}
		if err := run(string(action)+" "+unit, func() error {
			switch action {
			case ActionStart:
				return svc.Start(ctx)
			case ActionStop:
				return svc.Stop(ctx)
			case ActionReload:
				return svc.Reload(ctx)
			case ActionRestart:
				return svc.Restart(ctx)
			}
			return nil
		}); err != nil {
			return result, err
		}

		// Post-apply verification (design.md §11.5). §3.6 permits it
		// explicitly: "did the unit come up and stay up" is bounded, where "did
		// a client resolve a name" would be convergence and would freeze every
		// other module.
		//
		// It is worth the wall-clock here more than anywhere. A resolver that
		// died takes the whole building offline at once, and unlike a dead DHCP
		// server it does so immediately rather than as leases expire.
		if desired.Enabled && action != ActionStop {
			if err := run("verify "+unit+" stayed up", func() error {
				return a.verifyServing(ctx, unit)
			}); err != nil {
				return result, err
			}
		}
	}

	return result, nil
}

// serviceOrder sequences the two units.
//
// Coming up, the resolver goes first: a relay whose upstream is not answering
// serves SERVFAIL to every client on the network, and a SERVFAIL is cached.
// Going down, the order reverses for the mirror of the same reason — the relay
// stops accepting queries before the thing behind it disappears, so nobody is
// handed a failure that looks like a broken name rather than a stopped service.
func (a Applier) serviceOrder(plan Plan) []ServicePlan {
	out := slices.Clone(plan.Services)
	stopping := slices.ContainsFunc(out, func(s ServicePlan) bool { return s.Action == ActionStop })
	if stopping {
		slices.Reverse(out)
	}
	return out
}

// startingRelay reports whether the plan brings the relay's socket up from
// nothing, which is the only case the port check applies to.
func (a Applier) startingRelay(plan Plan) bool {
	for _, s := range plan.Services {
		if s.Unit == a.Backend.RelayUnit() && s.Action == ActionStart {
			return true
		}
	}
	return false
}

// checkInstalled refuses to drive a unit whose file is not on the box.
//
// Not an error where the service manager itself is missing: that is a developer
// machine, and "we cannot tell" must not be reported as "it is broken" — the
// same distinction verifyServing makes (§3.4).
func (a Applier) checkInstalled(ctx context.Context, unit string) error {
	svc, err := a.service(unit)
	if err != nil {
		return err
	}
	status, err := svc.Status(ctx)
	switch {
	case errors.Is(err, ErrNoServiceManager):
		return nil
	case err != nil:
		return err
	case !status.Installed:
		return fmt.Errorf(
			"%s is not installed, so there is no DNS server for olr to drive.\n"+
				"The unit ships inside olr and is written out by `olr enable`; a missing "+
				"one means the binary was copied into place without it.\n"+
				"Run `sudo olr enable` to write the units, or reinstall the package",
			status.Unit)
	}
	return nil
}

// verifyServing watches a unit for the settle window and fails if it does not
// stay active.
//
// Staying up is the whole check, and it is a stronger signal than it looks.
// unbound exits on a config it cannot parse or a socket it cannot bind, and so
// does the relay — neither sits there alive and idle. So a process still
// running after the window has read its configuration and bound its sockets.
//
// There is a third way to exit, which this used to read as one of those two: a
// file it is not allowed to open at all. A distribution that confines the
// resolver by path refuses that before the configuration is parsed, and the
// config check the unit runs first — as an ordinary, unconfined process —
// calls the same file valid. Two explanations that do not fit, and a dead
// resolver. Paths holds the answer to where those files have to live; the
// message below names the case rather than leaving the operator to guess.
func (a Applier) verifyServing(ctx context.Context, unit string) error {
	svc, err := a.service(unit)
	if err != nil {
		return err
	}
	deadline := time.Now().Add(a.settleWindow())
	for {
		status, err := svc.Status(ctx)
		switch {
		case errors.Is(err, ErrNoServiceManager):
			// Nothing to verify against. Observe already reports this as "not
			// known" and `olr dns status` surfaces it on its own line; failing
			// the apply here would turn "we cannot tell" into "it broke", which
			// are different answers (§3.4).
			return nil
		case err != nil:
			return err
		case !status.Active:
			return fmt.Errorf(
				"%s did not stay up after starting (%s).\n"+
					"The backend accepted the job and then exited, which usually means it "+
					"rejected the rendered configuration or could not bind a socket. A "+
					"third thing does it and looks like neither: it was not allowed to "+
					"open the file. Debian's AppArmor profile confines /usr/sbin/unbound "+
					"to /etc/unbound and /var/lib/unbound — which is why this module's two "+
					"resolver files are rendered inside those trees — and a denial there "+
					"reads as \"Permission denied\" for a config unbound-checkconf just "+
					"passed.\n"+
					"See `journalctl -u %s`",
				status.Unit, describeState(status), status.Unit)
		}

		remaining := time.Until(deadline)
		if remaining <= 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(min(settleInterval, remaining)):
		}
	}
}

func describeState(s ServiceStatus) string {
	parts := make([]string, 0, 2)
	if s.State != "" {
		parts = append(parts, "state "+s.State)
	}
	if s.SubState != "" {
		parts = append(parts, "sub-state "+s.SubState)
	}
	if len(parts) == 0 {
		return "no state reported"
	}
	return strings.Join(parts, ", ")
}

// observedRoots is the set of directories Observe walks.
//
// The policy directory normally sits under the rendered config's own directory
// and is subsumed by the first root; it is listed anyway because Paths is
// free-form, and a nested one is dropped rather than walked twice.
func (a Applier) observedRoots() []string {
	seen := map[string]bool{}
	var candidates []string
	for _, p := range []string{
		filepath.Dir(a.Paths.UnboundConf),
		filepath.Dir(a.Paths.RelayConf),
		a.Paths.PolicyDir,
		filepath.Dir(a.Paths.HijackNFT),
	} {
		if p == "" || p == "." || p == string(filepath.Separator) {
			continue
		}
		p = filepath.Clean(p)
		if !seen[p] {
			seen[p] = true
			candidates = append(candidates, p)
		}
	}

	var roots []string
	for _, c := range candidates {
		nested := false
		for _, other := range candidates {
			if other != c && strings.HasPrefix(c, other+string(filepath.Separator)) {
				nested = true
				break
			}
		}
		if !nested {
			roots = append(roots, c)
		}
	}
	sort.Strings(roots)
	return roots
}

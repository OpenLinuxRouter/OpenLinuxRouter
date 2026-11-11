package dhcp

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// Applier turns intent into a running DHCP server.
//
// There is no rollback here on purpose (design.md §5.2/§5.3.2). If a multi-step
// change fails halfway, the steps that landed stay landed and are reported;
// re-running finishes the job. That is more honest and more debuggable than a
// silent revert, and it is why Steps below records outcomes rather than being
// discarded on error.
type Applier struct {
	// Backend renders the daemon's files.
	Backend Dnsmasq
	// Networks is the window onto the link module's networks.
	Networks NetworkView
	// Service supervises the daemon.
	Service Service
	// Paths is the on-disk layout.
	Paths Paths
	// Store is core's configuration document, which owns this module's intent
	// alongside every other module's. Revision history, if it is ever built,
	// attaches here rather than in the module — as numbered JSON snapshots on
	// disk, since design.md §10 #5 closed as no database.
	Store *core.Store

	// PortCheck reports whether something already holds the DHCP server port.
	// Nil means PortConflict. Injectable so the refusal path is testable
	// without arranging for a real port conflict on the build machine.
	PortCheck func() (bool, error)

	// Settle is how long apply watches a started backend before believing it.
	// Zero means DefaultSettle; negative means check once and return.
	Settle time.Duration
}

// DefaultSettle is the post-apply observation window (design.md §11.5).
//
// It is not a guess at how long dnsmasq takes to start — it is how long we
// watch it *after* systemd says the job is done. The unit is Type=simple, so
// systemd reports success the moment the process is forked, which is before
// dnsmasq has read a config file or bound a socket. A config it rejects at
// runtime therefore looks identical to a healthy start for the first fraction
// of a second.
const DefaultSettle = 1500 * time.Millisecond

// settleInterval is how often the unit is sampled during that window.
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

// NewApplierAt wires up the dnsmasq backend against a link view, with every
// rendered path relocated under root.
//
// An empty root is the real system. See RootedPaths for why a non-empty one
// exists — it is the development escape hatch, not a supported deployment
// layout, and nothing but the daemon's own flags should ever set it. The store
// is passed in already rooted, because core owns that path.
func NewApplierAt(store *core.Store, networks NetworkView, root string) (Applier, error) {
	paths := RootedPaths(root)
	backend := NewDnsmasq(paths).WithSource(store.Path())
	service, err := NewService(backend.Unit())
	if err != nil {
		return Applier{}, err
	}
	return Applier{
		Backend:  backend,
		Networks: networks,
		Service:  service,
		Paths:    paths,
		Store:    store,
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
// daemon is running, and which leases are held.
//
// Every field is read fresh. Planning against observation rather than against a
// cached copy of what we last wrote is what makes drift detection free
// (design.md §5.4).
func (a Applier) Observe(ctx context.Context) (Observed, error) {
	obs := Observed{Files: map[string][]byte{}}

	// Everything under the directories we own, walked rather than listed. A
	// file we did not render must still be seen or a stale reservation would
	// keep being served — and that argument does not stop at the two
	// directories we happen to write today: a file left behind anywhere in the
	// rendered tree by an older olr, or by a hand-edit, is exactly the drift
	// §5.4 exists to surface.
	paths := map[string]bool{a.Paths.Conf: true}
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

	leases, _, err := LoadLeases(a.Paths.LeaseFile)
	if err != nil {
		return Observed{}, err
	}
	obs.Leases = leases

	// A missing service manager is reported as "not running" rather than
	// failing the whole observation: the file half of the answer is still
	// useful, and `olr dhcp status` surfaces the service error separately.
	if status, err := a.Service.Status(ctx); err == nil {
		obs.ServiceKnown = true
		obs.Running = status.Active
		obs.Starting = status.State == "activating"
		obs.EnabledAtBoot = status.Enabled
		obs.Installed = status.Installed
	}

	return obs, nil
}

// Plan reports what applying desired would do, without doing it.
func (a Applier) Plan(ctx context.Context, desired Config) (Plan, error) {
	obs, err := a.Observe(ctx)
	if err != nil {
		return Plan{}, err
	}
	return BuildPlan(a.Backend, desired, a.Networks, obs, time.Now())
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

// Apply writes the config and brings the daemon into line with it.
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
	plan, err := BuildPlan(a.Backend, desired, a.Networks, obs, time.Now())
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
	// apply lock (§3.6) — that is precisely the TOCTOU the single lock exists
	// to close.
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

	for _, dir := range []string{
		filepath.Dir(a.Paths.Conf), a.Paths.HostsDir, a.Paths.OptsDir,
		filepath.Dir(a.Paths.LeaseFile),
	} {
		if err := run("create "+dir, func() error { return os.MkdirAll(dir, 0o755) }); err != nil {
			return result, err
		}
	}

	rendered, err := a.Backend.Render(desired, a.Networks)
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
	// otherwise produce — a bare "Unit olr-dhcp.service not found" from D-Bus —
	// names neither the cause nor the fix. A missing unit file is a packaging
	// problem and no amount of re-applying will resolve it.
	if plan.Action != ActionNone || plan.Enable != nil {
		if err := run("check "+a.Backend.Unit()+" is installed", func() error {
			return a.checkInstalled(ctx)
		}); err != nil {
			return result, err
		}
	}

	// Only checked when we are about to start: a reload or restart of a daemon
	// that is already bound would trip over its own socket.
	if plan.Action == ActionStart {
		if err := run("check nothing else holds UDP/67", func() error {
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

	// Boot-time state before the service action, so that a backend which then
	// fails to start is at least configured the way the operator asked. The
	// reverse order would leave a box that came up, failed, and would also not
	// have come back after a reboot.
	if plan.Enable != nil {
		enable := *plan.Enable
		verb := map[bool]string{true: "enable", false: "disable"}[enable]
		if err := run(verb+" "+a.Backend.Unit()+" at boot", func() error {
			if enable {
				return a.Service.Enable(ctx)
			}
			return a.Service.Disable(ctx)
		}); err != nil {
			return result, err
		}
	}

	if action := plan.Action; action != ActionNone {
		err := run(string(action)+" "+a.Backend.Unit(), func() error {
			switch action {
			case ActionStart:
				return a.Service.Start(ctx)
			case ActionStop:
				return a.Service.Stop(ctx)
			case ActionReload:
				return a.Service.Reload(ctx)
			case ActionRestart:
				return a.Service.Restart(ctx)
			}
			return nil
		})
		if err != nil {
			return result, err
		}

		// Post-apply verification (design.md §11.5). §3.6 permits it explicitly:
		// "did the unit come up and stay up" is bounded, where "did a client get
		// a lease" would be convergence and would freeze every other module.
		//
		// It is worth the wall-clock. A DHCP server that died is invisible for
		// hours and then breaks every client at once as leases expire, and it is
		// the one failure the lockout guard (§5.5) does not cover — the operator
		// keeps their own session throughout.
		if desired.Enabled && action != ActionStop {
			if err := run("verify "+a.Backend.Unit()+" stayed up", func() error {
				return a.verifyServing(ctx)
			}); err != nil {
				return result, err
			}
		}
	}

	return result, nil
}

// checkInstalled refuses to drive a unit whose file is not on the box.
//
// Not an error where the service manager itself is missing: that is a developer
// machine, and "we cannot tell" must not be reported as "it is broken" — the
// same distinction verifyServing makes (§3.4).
func (a Applier) checkInstalled(ctx context.Context) error {
	status, err := a.Service.Status(ctx)
	switch {
	case errors.Is(err, ErrNoServiceManager):
		return nil
	case err != nil:
		return err
	case !status.Installed:
		return fmt.Errorf(
			"%s is not installed, so there is no DHCP server for olr to drive.\n"+
				"The unit ships inside olr and is written out by `olr enable`; a missing "+
				"one means the binary was copied into place without it.\n"+
				"Run `sudo olr enable` to write the units, or reinstall the package",
			status.Unit)
	}
	return nil
}

// verifyServing watches the unit for the settle window and fails if it does not
// stay active.
//
// Staying up is the whole check, and it is a stronger signal than it looks:
// dnsmasq exits on a bad config or a socket it cannot bind rather than sitting
// there alive and idle. So a process still running after the window has parsed
// its configuration and bound its sockets. Asserting on UDP/67 directly was
// considered and rejected — a network serving only RA never binds it, so the
// check would fail exactly where IPv6 is configured correctly.
func (a Applier) verifyServing(ctx context.Context) error {
	deadline := time.Now().Add(a.settleWindow())
	for {
		status, err := a.Service.Status(ctx)
		switch {
		case errors.Is(err, ErrNoServiceManager):
			// Nothing to verify against. Observe already reports this as "not
			// running" and `olr dhcp status` surfaces it on its own line;
			// failing the apply here would turn "we cannot tell" into "it
			// broke", which are different answers (§3.4).
			return nil
		case err != nil:
			return err
		case !status.Active:
			return fmt.Errorf(
				"%s did not stay up after starting (%s).\n"+
					"The backend accepted the job and then exited, which usually means it "+
					"rejected the rendered configuration or could not bind a socket.\n"+
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
// HostsDir and OptsDir normally sit under the rendered config's own directory
// and are subsumed by the first root; they are listed anyway because Paths is
// free-form, and a nested one is dropped rather than walked twice.
func (a Applier) observedRoots() []string {
	seen := map[string]bool{}
	var candidates []string
	for _, p := range []string{filepath.Dir(a.Paths.Conf), a.Paths.HostsDir, a.Paths.OptsDir} {
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

// Leases reads the current lease database.
func (a Applier) Leases() ([]Lease, []Problem, error) { return LoadLeases(a.Paths.LeaseFile) }

// Usage summarises each pool's occupancy.
//
// A pool whose network has gone away is skipped rather than reported as empty.
// "0 of 0 in use" is indistinguishable from a working network nobody is on,
// and the missing network is already a validation error with a better message.
func (a Applier) Usage(c Config, leases []Lease) []Usage {
	out := make([]Usage, 0, len(c.Pools))
	for _, p := range c.Pools {
		info, err := a.Networks.Network(p.Network)
		if err != nil {
			continue
		}
		out = append(out, UsageOf(p, info, leases, time.Now()))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Network < out[j].Network })
	return out
}

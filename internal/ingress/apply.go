package ingress

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// Applier turns intent into a running proxy.
//
// There is no rollback (design.md §5.2/§5.3.2). If a multi-step change fails
// halfway, the steps that landed stay landed and are reported; re-running
// finishes the job. That is why Steps records outcomes rather than being
// discarded on error.
type Applier struct {
	// Backend renders the proxy's files.
	Backend Caddy
	// DNS is the window onto the dns module — only the suffix it answers
	// authoritatively, so that a published domain inside it is refused rather
	// than silently unresolvable (validate.go).
	DNS DNSView
	// Clients is the window onto the clients module.
	Clients ClientView
	// Proxy supervises the backend. Named for what it is rather than
	// "Service", which in this module is the operator's object — see service.go.
	Proxy ProxyUnit
	// Paths is the on-disk layout.
	Paths Paths
	// Store is core's configuration document.
	Store *core.Store

	// PortCheck reports which of the proxy's ports are already held. Nil means
	// PortConflict. Injectable so the refusal path is testable without
	// arranging a real conflict on the build machine.
	PortCheck func() ([]uint64, error)

	// CheckConfig validates a rendered Caddyfile by running the proxy binary
	// against it. Nil means RunConfigCheck.
	CheckConfig ConfigChecker

	// Locate finds the proxy binary. Nil means FindBinary.
	//
	// Injectable for the reason PortCheck is: left to the real thing, every
	// test in this package would pass or fail depending on whether the build
	// machine happens to have a `caddy` on its PATH.
	Locate func() (string, error)

	// Settle is how long apply watches a started backend before believing it.
	// Zero means DefaultSettle; negative means check once and return.
	Settle time.Duration
}

// ConfigChecker validates a Caddyfile on disk.
//
// The environment is passed rather than read from the process, because the file
// references the provider credential as `{env.…}` and olrd does not itself hold
// that variable. Handing it to the subprocess keeps the credential out of a file
// the check would have had to create.
type ConfigChecker func(ctx context.Context, confPath string, env []string) error

// ErrNoChecker reports that no proxy binary was available, so a rendered config
// could not be checked.
//
// Tolerated in the same way and for the same reason as ErrNoServiceManager: on a
// machine with no backend, "we could not tell" must not be reported as "your
// configuration is broken". Where it matters — a box about to actually start the
// proxy — the missing binary is refused separately, by ErrBinaryMissing, which
// says how to get one.
var ErrNoChecker = errors.New("no proxy binary to check the configuration with")

// DefaultSettle is the post-apply observation window.
//
// Not a guess at how long Caddy takes to start: it is how long we watch the unit
// *after* systemd says the job is done. A configuration Caddy rejects at runtime
// looks identical to a healthy start for the first fraction of a second.
const DefaultSettle = 1500 * time.Millisecond

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

func (a Applier) portCheck() func() ([]uint64, error) {
	if a.PortCheck != nil {
		return a.PortCheck
	}
	return PortConflict
}

func (a Applier) locate() func() (string, error) {
	if a.Locate != nil {
		return a.Locate
	}
	return FindBinary
}

func (a Applier) configChecker() ConfigChecker {
	if a.CheckConfig != nil {
		return a.CheckConfig
	}
	return RunConfigCheck
}

// RunConfigCheck asks the bundled proxy whether a Caddyfile is valid.
//
// This is docs/ingress.md §7.1's rule in one function, and the stakes are what
// make it worth a subprocess on every apply: a Caddyfile the proxy rejects takes
// down **every** published service at once, not the one being edited. Validating
// before the file reaches its final path turns that into a refused change.
func RunConfigCheck(ctx context.Context, confPath string, env []string) error {
	binary, err := FindBinary()
	if err != nil {
		return ErrNoChecker
	}
	cmd := exec.CommandContext(ctx, binary, "validate", "--adapter", "caddyfile", "--config", confPath)
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	if err == nil {
		return nil
	}
	// Caddy's own words, filtered but not reworded. It names the directive and
	// the line, and we have nothing better to say about a file we generated than
	// what the thing that has to read it said about it.
	msg := caddyComplaint(out)
	if msg == "" {
		return fmt.Errorf("the rendered Caddyfile was rejected: %w", err)
	}
	// A provider the binary was not built with is the rejection worth
	// anticipating in words: Caddy reports it as an unregistered module, which is
	// accurate and does not mention the thing that would actually help. The
	// phrase matched here was taken from a real caddy's output, not guessed —
	// `module not registered: dns.providers.<name>`.
	hint := ""
	if strings.Contains(msg, providerPrefix) {
		hint = fmt.Sprintf("\n\n%s was not built with that DNS provider. "+
			"`olr ingress show providers` lists what it has", binary)
	}
	return fmt.Errorf("the rendered Caddyfile was rejected:\n%s%s", msg, hint)
}

// caddyComplaint reduces `caddy validate` output to the part about the config.
//
// Caddy logs to stderr in JSON and then prints its actual complaint in plain
// text, so the raw output opens with a line like
// `{"level":"info","msg":"using config from file",…}` that tells an operator
// nothing. Dropping anything shaped like a log record leaves the sentence naming
// the directive and the line, which is the whole of what is useful.
//
// Found by running it rather than reasoned about: the noise was the first thing
// visible the first time a real Caddy rejected a real rendered file.
func caddyComplaint(out []byte) string {
	var kept []string
	for _, line := range strings.Split(string(out), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "{") {
			continue
		}
		kept = append(kept, trimmed)
	}
	return strings.Join(kept, "\n")
}

// NewApplierAt wires up the Caddy backend, with every rendered path relocated
// under root. An empty root is the real system.
func NewApplierAt(store *core.Store, dns DNSView, clients ClientView, root string) (Applier, error) {
	paths := RootedPaths(root)
	backend := NewCaddy(paths).WithSource(store.Path())
	proxy, err := NewProxyUnit(backend.Unit())
	if err != nil {
		return Applier{}, err
	}
	return Applier{
		Backend: backend,
		DNS:     dns,
		Clients: clients,
		Proxy:   proxy,
		Paths:   paths,
		Store:   store,
	}, nil
}

// Step is one unit of work and how it went.
type Step struct {
	Description string `json:"description"`
	Done        bool   `json:"done"`
	Error       string `json:"error,omitempty"`

	// Skipped marks a step that could not be attempted rather than one that
	// failed — a config check with no binary to run it. Done stays true,
	// because nothing is outstanding, but an operator reading the list should
	// not believe a check happened that did not.
	Skipped bool `json:"skipped,omitempty"`
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
	cfg, err := FromDocument(doc)
	if err == nil {
		cfg.ResolveClientReferences(a.Clients)
	}
	return cfg, err
}

// Observe reads the actual state of the system, every field fresh. Planning
// against observation rather than a cached copy of what we last wrote is what
// makes drift detection free (design.md §5.4).
func (a Applier) Observe(ctx context.Context) (Observed, error) {
	obs := Observed{Files: map[string][]byte{}}

	root := filepath.Dir(a.Paths.Conf)
	err := filepath.WalkDir(root, func(path string, e fs.DirEntry, err error) error {
		switch {
		case os.IsNotExist(err):
			return nil
		case err != nil:
			return err
		case e.IsDir():
			return nil
		case strings.HasPrefix(e.Name(), "."):
			// core.WriteFileAtomic's temporaries. Skipping them keeps a crashed
			// apply's leftovers out of the plan.
			return nil
		}
		data, readErr := os.ReadFile(path)
		if os.IsNotExist(readErr) {
			return nil
		}
		if readErr != nil {
			return fmt.Errorf("reading %s: %w", path, readErr)
		}
		obs.Files[path] = data
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		return Observed{}, fmt.Errorf("reading %s: %w", root, err)
	}

	// A missing service manager is reported as "not running" rather than
	// failing the observation: the file half is still useful, and `olr ingress
	// status` surfaces the service error on its own line.
	if status, err := a.Proxy.Status(ctx); err == nil {
		obs.ServiceKnown = true
		obs.Running = status.Active
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
	return BuildPlan(a.Backend, desired, a.DNS, a.Clients, obs)
}

// Drift reports what has changed underneath stored intent — design.md §5.4 in
// one line. A non-empty plan is drift.
func (a Applier) Drift(ctx context.Context) (Plan, error) {
	current, err := a.Load()
	if err != nil {
		return Plan{}, err
	}
	return a.Plan(ctx, current)
}

// Apply writes the config and brings the proxy into line with it. It applies
// immediately and is in effect on return (design.md §5.1).
func (a Applier) Apply(ctx context.Context, desired Config) (ApplyResult, error) {
	obs, err := a.Observe(ctx)
	if err != nil {
		return ApplyResult{}, err
	}
	plan, err := BuildPlan(a.Backend, desired, a.DNS, a.Clients, obs)
	if err != nil {
		return ApplyResult{Plan: plan}, err
	}
	return a.ApplyPlanned(ctx, desired, plan)
}

// ApplyPlanned applies a plan that has already been built and looked at.
//
// Split out of Apply for the HTTP surface, which has to do something between the
// two halves: a disruptive plan is *held* and returned for confirmation rather
// than applied (http.go). Re-planning after that decision would be a second
// observation, so the plan the operator agreed to would not be the plan that
// lands — and "a published name stops answering" is exactly the kind of thing
// that must not change shape between being shown and being done.
func (a Applier) ApplyPlanned(ctx context.Context, desired Config, plan Plan) (ApplyResult, error) {
	desired.ResolveClientReferences(a.Clients)
	result := ApplyResult{Plan: plan}
	run := func(description string, fn func() error) error {
		err := fn()
		step := Step{Description: description, Done: err == nil}
		switch {
		case errors.Is(err, ErrNoChecker):
			step.Done, step.Skipped, err = true, true, nil
		case err != nil:
			step.Error = err.Error()
		}
		result.Steps = append(result.Steps, step)
		return err
	}

	// Intent is stored first, before anything it describes, so that a failed
	// later step leaves a re-run something to finish from (§5.3.2).
	//
	// Read-modify-write on the shared document, safe without further locking
	// because every config write in this process holds the one global apply
	// lock (§3.6).
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

	// nothingToDo, not Empty: a cosmetic rewrite is not drift, but it is still
	// a file to bring up to date.
	if plan.nothingToDo() {
		return result, nil
	}

	rendered, err := a.Backend.Render(desired, a.Clients)
	if err != nil {
		return result, err
	}

	for _, dir := range []string{filepath.Dir(a.Paths.Conf), a.Paths.Data} {
		if err := run("create "+dir, func() error { return os.MkdirAll(dir, 0o755) }); err != nil {
			return result, err
		}
	}

	// Checked before a byte reaches its final path. A Caddyfile the proxy
	// rejects takes down every published service at once, so the difference
	// between checking here and discovering it at reload is the difference
	// between a refused change and an outage (docs/ingress.md §7.1).
	if err := run("check the rendered Caddyfile is valid", func() error {
		return a.checkRendered(ctx, rendered, desired)
	}); err != nil {
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

	// Checked before anything is asked of systemd, because a bare "Unit
	// olr-caddy.service not found" from D-Bus names neither the cause nor the
	// fix, and no amount of re-applying resolves a packaging problem.
	if plan.Action != ActionNone || plan.Enable != nil {
		// The binary before the unit, because it is the more likely of the two
		// to be missing and the one whose absence olr can do something about
		// explaining. A unit that exists with no binary behind it fails at
		// start with an exec error naming a path and nothing else.
		if desired.Enabled {
			if err := run("check a proxy binary is available", func() error {
				if _, err := a.locate()(); err != nil {
					return ErrBinaryMissing()
				}
				return nil
			}); err != nil {
				return result, err
			}
		}
		if err := run("check "+a.Backend.Unit()+" is installed", func() error {
			return a.checkInstalled(ctx)
		}); err != nil {
			return result, err
		}
	}

	// Only when we are about to start: a reload of a proxy that is already
	// bound would trip over its own listeners.
	if plan.Action == ActionStart {
		if err := run("check nothing else holds TCP/80 or TCP/443", func() error {
			held, err := a.portCheck()()
			if err != nil {
				return err
			}
			if len(held) > 0 {
				return ErrPortInUse(held)
			}
			return nil
		}); err != nil {
			return result, err
		}
	}

	// Boot-time state before the service action, so a backend that then fails
	// to start is at least configured the way the operator asked. The reverse
	// order leaves a box that came up, failed, and would also not have come
	// back after a reboot.
	if plan.Enable != nil {
		enable := *plan.Enable
		verb := map[bool]string{true: "enable", false: "disable"}[enable]
		if err := run(verb+" "+a.Backend.Unit()+" at boot", func() error {
			if enable {
				return a.Proxy.Enable(ctx)
			}
			return a.Proxy.Disable(ctx)
		}); err != nil {
			return result, err
		}
	}

	if action := plan.Action; action != ActionNone {
		if err := run(string(action)+" "+a.Backend.Unit(), func() error {
			switch action {
			case ActionStart:
				return a.Proxy.Start(ctx)
			case ActionStop:
				return a.Proxy.Stop(ctx)
			case ActionReload:
				return a.Proxy.Reload(ctx)
			case ActionRestart:
				return a.Proxy.Restart(ctx)
			}
			return nil
		}); err != nil {
			return result, err
		}

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

// checkRendered runs the config check against a copy of the Caddyfile written
// somewhere harmless.
//
// A copy, because the check needs a file and the point of checking is that the
// real path is not touched until the answer is yes. It is written beside the
// real one so that any relative path inside the config resolves the same way,
// and with a dot prefix so Observe skips it — a check that crashed halfway must
// not leave behind a file the next plan schedules a delete for.
func (a Applier) checkRendered(ctx context.Context, rendered Rendered, desired Config) error {
	file, ok := rendered.Get(a.Paths.Conf)
	if !ok {
		return fmt.Errorf("internal: nothing rendered for %s", a.Paths.Conf)
	}

	dir := filepath.Dir(a.Paths.Conf)
	tmp, err := os.CreateTemp(dir, ".check-*.caddyfile")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(file.Data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}

	// The credential reaches the check as an environment variable rather than
	// through the rendered env file, which may not be on disk yet and whose
	// contents we would rather not have in a second place.
	env := []string{TokenEnv + "=" + desired.Certificate.Token}
	err = a.configChecker()(ctx, tmp.Name(), env)

	// Caddy names the file it was given, and the file it was given is a
	// temporary the operator cannot open — while the *line number* beside it is
	// the most useful thing in the message. Rewriting the path keeps the number
	// pointing somewhere real.
	if err != nil && strings.Contains(err.Error(), tmp.Name()) {
		return errors.New(strings.ReplaceAll(err.Error(), tmp.Name(), a.Paths.Conf))
	}
	return err
}

// checkInstalled refuses to drive a unit whose file is not on the box.
//
// Not an error where the service manager itself is missing: that is a developer
// machine, and "we cannot tell" must not be reported as "it is broken".
func (a Applier) checkInstalled(ctx context.Context) error {
	status, err := a.Proxy.Status(ctx)
	switch {
	case errors.Is(err, ErrNoServiceManager):
		return nil
	case err != nil:
		return err
	case !status.Installed:
		return fmt.Errorf(
			"%s is not installed, so there is no proxy for olr to drive.\n"+
				"The unit ships inside olr and is written out by `olr enable`; a missing one\n"+
				"means the binary was copied into place without it. Run `sudo olr enable`",
			status.Unit)
	}
	return nil
}

// verifyServing watches the unit for the settle window and fails if it does not
// stay active.
//
// Staying up is a stronger signal than it looks: Caddy exits on a configuration
// it cannot load or a listener it cannot bind, rather than sitting there alive
// and idle. Asserting on :443 directly was considered and rejected — the socket
// is bound before the first certificate exists, so it would report success at
// exactly the moment issuance is still the open question.
func (a Applier) verifyServing(ctx context.Context) error {
	deadline := time.Now().Add(a.settleWindow())
	for {
		status, err := a.Proxy.Status(ctx)
		switch {
		case errors.Is(err, ErrNoServiceManager):
			// Nothing to verify against. Observe already reports this as "not
			// running"; failing here would turn "we cannot tell" into "it is
			// broken".
			return nil
		case err != nil:
			return err
		case !status.Active:
			return fmt.Errorf(
				"%s did not stay running. The proxy exits rather than serving a "+
					"configuration it cannot load, so check `olr ingress logs`",
				status.Unit)
		}
		if !time.Now().Before(deadline) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(settleInterval):
		}
	}
}

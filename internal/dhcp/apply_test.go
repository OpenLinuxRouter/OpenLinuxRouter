package dhcp

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// fakeService records what was asked of the daemon without needing one.
type fakeService struct {
	active bool
	calls  []string
	fail   error

	// enabled is the boot-time state, tracked separately from active because
	// that is exactly the distinction under test: a unit can be running now and
	// still not come back after a reboot.
	enabled bool

	// notInstalled models a box where the unit file was never laid down, which
	// is what an incomplete install looks like.
	notInstalled bool

	// statusErr is returned by Status instead of a state.
	statusErr error

	// diesAfter makes the unit go inactive after this many Status calls,
	// modelling a backend that accepts the start job and then exits. That is
	// what a Type=simple unit looks like when dnsmasq parses the rendered
	// config, dislikes it, and quits — systemd has already reported the job
	// "done" by then.
	diesAfter int
	statusN   int
}

func (f *fakeService) Status(context.Context) (ServiceStatus, error) {
	if f.statusErr != nil {
		return ServiceStatus{Unit: "olr-dhcp.service"}, f.statusErr
	}
	f.statusN++
	if f.diesAfter > 0 && f.statusN > f.diesAfter {
		f.active = false
		return ServiceStatus{
			Unit: "olr-dhcp.service", Active: false, State: "failed", SubState: "failed",
			Enabled: f.enabled, Installed: !f.notInstalled,
		}, nil
	}
	return ServiceStatus{
		Unit: "olr-dhcp.service", Active: f.active, State: "active",
		Enabled: f.enabled, Installed: !f.notInstalled,
	}, nil
}

func (f *fakeService) do(verb string) error {
	f.calls = append(f.calls, verb)
	if f.fail != nil {
		return f.fail
	}
	switch verb {
	case "start", "restart":
		f.active = true
	case "stop":
		f.active = false
	case "enable":
		f.enabled = true
	case "disable":
		f.enabled = false
	}
	return nil
}

func (f *fakeService) Start(context.Context) error   { return f.do("start") }
func (f *fakeService) Stop(context.Context) error    { return f.do("stop") }
func (f *fakeService) Restart(context.Context) error { return f.do("restart") }
func (f *fakeService) Reload(context.Context) error  { return f.do("reload") }
func (f *fakeService) Enable(context.Context) error  { return f.do("enable") }
func (f *fakeService) Disable(context.Context) error { return f.do("disable") }

// testApplier builds an Applier rooted entirely inside a temp directory, which
// is what makes the whole apply path testable without root, systemd or /etc.
func testApplier(t *testing.T) (Applier, *fakeService) {
	t.Helper()
	root := t.TempDir()
	paths := Paths{
		Conf:      filepath.Join(root, "rendered", "dnsmasq.conf"),
		HostsDir:  filepath.Join(root, "rendered", "hosts.d"),
		OptsDir:   filepath.Join(root, "rendered", "opts.d"),
		LeaseFile: filepath.Join(root, "state", "leases"),
		PIDFile:   filepath.Join(root, "run", "dhcp.pid"),
	}
	svc := &fakeService{}
	return Applier{
		Backend:  NewDnsmasq(paths),
		Networks: testNetworks(),
		Service:  svc,
		Paths:    paths,
		Store:    core.NewStore(filepath.Join(root, "olr.json"), ModuleName),
		// Pinned rather than left to PortConflict, which would read the build
		// machine's /proc and make these tests depend on whether anything
		// happens to be serving DHCP there.
		PortCheck: func() (bool, error) { return false, nil },
		// Check the unit once and return, rather than watching it for the
		// default window. The fake service is decided the instant it is asked,
		// so waiting would only add DefaultSettle to every apply in the file.
		// The window itself is exercised by TestVerifyServing below.
		Settle: -1,
	}, svc
}

func TestApplyOnAFreshSystem(t *testing.T) {
	a, svc := testApplier(t)
	c := validConfig(t)

	result, err := a.Apply(context.Background(), c)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	if _, err := os.Stat(a.Store.Path()); err != nil {
		t.Errorf("intent was not stored: %v", err)
	}
	if _, err := os.Stat(a.Paths.Conf); err != nil {
		t.Errorf("dnsmasq.conf was not written: %v", err)
	}
	// Enabled as well as started. A router that serves DHCP until its next
	// power cut and then does not is the failure this ordering exists to
	// prevent, so both calls are asserted rather than just the visible one.
	if want := []string{"enable", "start"}; !equalStrings(svc.calls, want) {
		t.Errorf("service calls = %v, want %v", svc.calls, want)
	}
	if result.Plan.Action != ActionStart {
		t.Errorf("Action = %q, want start", result.Plan.Action)
	}
	for _, step := range result.Steps {
		if !step.Done {
			t.Errorf("step %q did not complete: %s", step.Description, step.Error)
		}
	}
}

// A unit that is running but not enabled is the shape of the bug this phase
// exists to close: everything looks healthy, and the box comes back from a
// power cut with no DHCP at all. It has to read as drift.
func TestAnEnabledConfigOnADisabledUnitIsDrift(t *testing.T) {
	a, svc := testApplier(t)
	c := validConfig(t)

	if _, err := a.Apply(context.Background(), c); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if !svc.enabled {
		t.Fatal("the first apply did not enable the unit")
	}

	// Somebody ran `systemctl disable olr-dhcp` behind our back.
	svc.enabled = false
	svc.calls = nil

	plan, err := a.Drift(context.Background())
	if err != nil {
		t.Fatalf("Drift: %v", err)
	}
	if plan.Empty() {
		t.Fatal("a disabled unit was not reported as drift")
	}
	if plan.Enable == nil || !*plan.Enable {
		t.Fatalf("Enable = %v, want a pending enable", plan.Enable)
	}
	// Said in the operator's terms, because the consequence is invisible until
	// the next reboot.
	if !strings.Contains(strings.Join(plan.Reasons, " "), "reboot") {
		t.Errorf("reasons do not explain the consequence: %v", plan.Reasons)
	}
	// Nothing a client holds changes when a unit is enabled.
	if plan.Impact != ImpactNone {
		t.Errorf("Impact = %s, want none", plan.Impact)
	}

	if _, err := a.Apply(context.Background(), c); err != nil {
		t.Fatalf("second Apply: %v", err)
	}
	if !equalStrings(svc.calls, []string{"enable"}) {
		t.Errorf("service calls = %v, want [enable] and nothing else", svc.calls)
	}
}

// Disabling means both halves too, or the box would come back serving DHCP the
// operator had switched off.
func TestDisablingStopsAndDisables(t *testing.T) {
	a, svc := testApplier(t)
	c := validConfig(t)

	if _, err := a.Apply(context.Background(), c); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	svc.calls = nil

	c.Enabled = false
	if _, err := a.Apply(context.Background(), c); err != nil {
		t.Fatalf("Apply disabling: %v", err)
	}

	if !equalStrings(svc.calls, []string{"disable", "stop"}) {
		t.Errorf("service calls = %v, want [disable stop]", svc.calls)
	}
	if svc.enabled {
		t.Error("the unit still starts at boot after being disabled")
	}
}

// D-Bus answers a missing unit with a bare "Unit ... not found", which names
// neither the cause nor the fix. A missing unit file is a packaging problem.
func TestApplyRefusesWhenTheUnitIsNotInstalled(t *testing.T) {
	a, svc := testApplier(t)
	svc.notInstalled = true

	result, err := a.Apply(context.Background(), validConfig(t))
	if err == nil {
		t.Fatal("Apply drove a unit that is not installed")
	}
	if !strings.Contains(err.Error(), "not installed") {
		t.Errorf("error does not name the cause: %v", err)
	}
	if !strings.Contains(err.Error(), "olr enable") {
		t.Errorf("error does not name the fix: %v", err)
	}
	if len(svc.calls) != 0 {
		t.Errorf("the service was driven anyway: %v", svc.calls)
	}
	// Intent is still stored: the config is fine, the box is not, and a re-run
	// after fixing the package should have something to finish from (§5.3.2).
	if _, err := os.Stat(a.Store.Path()); err != nil {
		t.Errorf("intent was not stored before the refusal: %v", err)
	}
	if len(result.Steps) == 0 {
		t.Error("no steps reported, so the operator cannot tell what landed")
	}
}

// A developer box has no system bus. "We cannot tell" must not be reported as
// "it is broken" (§3.4) — the same rule verifyServing follows.
func TestApplyDoesNotRefuseWhenThereIsNoServiceManager(t *testing.T) {
	a, svc := testApplier(t)
	svc.statusErr = ErrNoServiceManager

	if _, err := a.Apply(context.Background(), validConfig(t)); err != nil {
		t.Fatalf("Apply refused on a box with no service manager: %v", err)
	}
}

// Applying the same config twice must be a no-op. This is the property the
// whole drift story rests on: if a clean re-apply were not empty, `olr status`
// would report drift forever.
func TestApplyIsIdempotent(t *testing.T) {
	a, svc := testApplier(t)
	c := validConfig(t)
	ctx := context.Background()

	if _, err := a.Apply(ctx, c); err != nil {
		t.Fatalf("first Apply: %v", err)
	}
	before := len(svc.calls)

	result, err := a.Apply(ctx, c)
	if err != nil {
		t.Fatalf("second Apply: %v", err)
	}
	if !result.Plan.Empty() {
		t.Errorf("re-applying an unchanged config planned work: %v", result.Plan.Changes)
	}
	if len(svc.calls) != before {
		t.Errorf("re-apply touched the service: %v", svc.calls[before:])
	}
}

func TestDriftIsDetectedWhenAFileIsEditedOutOfBand(t *testing.T) {
	a, _ := testApplier(t)
	ctx := context.Background()
	if _, err := a.Apply(ctx, validConfig(t)); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	clean, err := a.Drift(ctx)
	if err != nil {
		t.Fatalf("Drift: %v", err)
	}
	if !clean.Empty() {
		t.Fatalf("drift reported immediately after apply: %v", clean.Changes)
	}

	// Somebody edits the generated file the header told them not to edit.
	if err := os.WriteFile(a.Paths.Conf, []byte("port=53\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	drifted, err := a.Drift(ctx)
	if err != nil {
		t.Fatalf("Drift: %v", err)
	}
	if drifted.Empty() {
		t.Fatal("hand-edited config was not reported as drift")
	}
	if drifted.Action != ActionRestart {
		t.Errorf("Action = %q, want restart", drifted.Action)
	}
}

// A reservation left behind by an earlier config keeps being served, because
// dnsmasq reads the directory rather than our intent. Observe has to see it.
func TestObserveSeesFilesWeDidNotRender(t *testing.T) {
	a, _ := testApplier(t)
	ctx := context.Background()
	if _, err := a.Apply(ctx, validConfig(t)); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	stray := filepath.Join(a.Paths.HostsDir, "deadbeef0000.conf")
	if err := os.WriteFile(stray, []byte("de:ad:be:ef:00:00,192.168.1.77\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	obs, err := a.Observe(ctx)
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	if _, ok := obs.Files[stray]; !ok {
		t.Fatalf("Observe missed %s; saw %v", stray, keys(obs.Files))
	}

	// And applying must clean it up.
	result, err := a.Apply(ctx, validConfig(t))
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if _, err := os.Stat(stray); !os.IsNotExist(err) {
		t.Errorf("stray reservation survived apply (err=%v), steps: %v", err, result.Steps)
	}
}

// design.md §5.3.2: no rollback. A failure leaves what landed in place and
// reports exactly which steps those were, so a re-run can finish the job.
func TestApplyReportsPartialProgressOnFailure(t *testing.T) {
	a, svc := testApplier(t)
	svc.fail = errors.New("unit olr-dhcp.service failed to start")

	result, err := a.Apply(context.Background(), validConfig(t))
	if err == nil {
		t.Fatal("Apply succeeded despite the service failing")
	}

	// The files that were written before the failure stay written.
	if _, statErr := os.Stat(a.Paths.Conf); statErr != nil {
		t.Errorf("config was rolled back rather than left in place: %v", statErr)
	}

	var failed, done int
	for _, s := range result.Steps {
		if s.Done {
			done++
		} else {
			failed++
			if !strings.Contains(s.Error, "failed to start") {
				t.Errorf("failed step does not carry the cause: %+v", s)
			}
		}
	}
	if done == 0 {
		t.Error("no completed steps reported; a re-run could not tell what landed")
	}
	if failed != 1 {
		t.Errorf("got %d failed steps, want 1", failed)
	}
}

// Intent is stored before the things it describes, so a re-run has something to
// finish the job from.
func TestApplyStoresIntentEvenWhenTheServiceFails(t *testing.T) {
	a, svc := testApplier(t)
	svc.fail = errors.New("boom")

	if _, err := a.Apply(context.Background(), validConfig(t)); err == nil {
		t.Fatal("expected an error")
	}

	stored, err := a.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(stored.Pools) != 1 || stored.Pools[0].Network != "lan" {
		t.Errorf("intent was not stored before the failure: %+v", stored)
	}
}

func TestApplyRejectsAnInvalidConfigBeforeWritingAnything(t *testing.T) {
	a, svc := testApplier(t)
	c := validConfig(t)
	c.Pools[0].IPv4.Start = addr(t, "10.0.0.5") // outside the lan network's subnet

	if _, err := a.Apply(context.Background(), c); err == nil {
		t.Fatal("Apply accepted an invalid config")
	}
	if _, err := os.Stat(a.Store.Path()); !os.IsNotExist(err) {
		t.Error("an invalid config was still stored")
	}
	if _, err := os.Stat(a.Paths.Conf); !os.IsNotExist(err) {
		t.Error("an invalid config was still rendered to disk")
	}
	if len(svc.calls) != 0 {
		t.Errorf("the service was touched for an invalid config: %v", svc.calls)
	}
}

func TestApplyReloadsForAReservation(t *testing.T) {
	a, svc := testApplier(t)
	ctx := context.Background()
	c := validConfig(t)
	if _, err := a.Apply(ctx, c); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	svc.calls = nil

	c.SetReservation(Reservation{MAC: "aa:bb:cc:dd:ee:ff", IP: addr(t, "192.168.1.50"), Hostname: "nas"})
	if _, err := a.Apply(ctx, c); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	if want := []string{"reload"}; !equalStrings(svc.calls, want) {
		t.Errorf("service calls = %v, want %v — a reservation must not restart the daemon", svc.calls, want)
	}
}

// We run our own dnsmasq instance rather than taking over the distro's, so two
// DHCP servers can end up racing for UDP/67. olr refuses rather than stopping
// somebody else's daemon (design.md §3.4).
func TestApplyRefusesToStartWhenSomethingElseHoldsThePort(t *testing.T) {
	a, svc := testApplier(t)
	a.PortCheck = func() (bool, error) { return true, nil }

	result, err := a.Apply(context.Background(), validConfig(t))
	if err == nil {
		t.Fatal("Apply started a second DHCP server on an occupied port")
	}
	if !strings.Contains(err.Error(), "UDP/67") {
		t.Errorf("error does not name the conflict: %v", err)
	}
	if !strings.Contains(err.Error(), "ss -lunp") {
		t.Errorf("error does not tell the operator how to find the holder: %v", err)
	}
	if len(svc.calls) != 0 {
		t.Errorf("the service was started anyway: %v", svc.calls)
	}
	// The check must come after the files are written, so that fixing the
	// conflict and re-running is a pure start with nothing left to render.
	if _, statErr := os.Stat(a.Paths.Conf); statErr != nil {
		t.Errorf("config was not written before the port check: %v", statErr)
	}
	if len(result.Steps) == 0 {
		t.Error("no steps recorded")
	}
}

// A reload must not be gated on the port being free — the daemon we are about
// to signal is the very thing holding it.
func TestPortCheckDoesNotBlockAReload(t *testing.T) {
	a, svc := testApplier(t)
	ctx := context.Background()
	c := validConfig(t)
	if _, err := a.Apply(ctx, c); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	a.PortCheck = func() (bool, error) { return true, nil } // our own daemon
	svc.calls = nil

	c.SetReservation(Reservation{MAC: "aa:bb:cc:dd:ee:ff", IP: addr(t, "192.168.1.50")})
	if _, err := a.Apply(ctx, c); err != nil {
		t.Fatalf("reload was blocked by our own daemon holding the port: %v", err)
	}
	if want := []string{"reload"}; !equalStrings(svc.calls, want) {
		t.Errorf("service calls = %v, want %v", svc.calls, want)
	}
}

// design.md §11.5: a dead DHCP server is invisible for hours and then breaks
// every client at once. The unit is Type=simple, so systemd reports the start
// job done the moment the process is forked — before dnsmasq has read a config
// file. Without the settle window this apply reports success.
func TestApplyFailsWhenTheBackendExitsAfterStarting(t *testing.T) {
	a, svc := testApplier(t)
	a.Settle = 300 * time.Millisecond
	svc.diesAfter = 1 // survives Observe's look, gone by the time we verify

	result, err := a.Apply(context.Background(), validConfig(t))
	if err == nil {
		t.Fatal("Apply reported success for a backend that exited straight after starting")
	}
	if !strings.Contains(err.Error(), "did not stay up") {
		t.Errorf("error does not say what happened: %v", err)
	}
	if !strings.Contains(err.Error(), "journalctl") {
		t.Errorf("error does not point at the daemon's own logs: %v", err)
	}

	// The start still happened, so the operator has to be told the difference
	// between "we never tried" and "we tried and it died".
	if !equalStrings(svc.calls, []string{"enable", "start"}) {
		t.Errorf("service calls = %v, want [enable start]", svc.calls)
	}
	var verify *Step
	for i, s := range result.Steps {
		if strings.HasPrefix(s.Description, "verify ") {
			verify = &result.Steps[i]
		}
	}
	if verify == nil {
		t.Fatalf("no verification step recorded: %+v", result.Steps)
	}
	if verify.Done {
		t.Error("the verification step is recorded as having succeeded")
	}
}

func TestApplyVerifiesTheBackendStayedUp(t *testing.T) {
	a, svc := testApplier(t)
	a.Settle = 200 * time.Millisecond

	result, err := a.Apply(context.Background(), validConfig(t))
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// Sampling once would pass on a backend that dies a moment later, which is
	// the whole failure this exists to catch. Observe accounts for one call, so
	// anything above two means the window was actually watched.
	if svc.statusN < 3 {
		t.Errorf("the unit was sampled %d times; the settle window was not watched", svc.statusN)
	}
	if !hasStep(result.Steps, "verify ") {
		t.Errorf("no verification step recorded: %+v", result.Steps)
	}
}

// "We could not tell" and "it broke" are different answers, and only one of
// them is honest (design.md §3.4). A box with no systemd must still apply.
func TestVerificationIsSkippedWithoutAServiceManager(t *testing.T) {
	a, svc := testApplier(t)
	svc.statusErr = ErrNoServiceManager

	if _, err := a.Apply(context.Background(), validConfig(t)); err != nil {
		t.Fatalf("Apply failed on a box with no service manager: %v", err)
	}
}

// Disabling stops the backend, so there is nothing to verify stayed up.
func TestVerificationIsSkippedWhenDisabling(t *testing.T) {
	a, _ := testApplier(t)
	ctx := context.Background()
	c := validConfig(t)
	if _, err := a.Apply(ctx, c); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	c.Enabled = false
	result, err := a.Apply(ctx, c)
	if err != nil {
		t.Fatalf("disabling failed: %v", err)
	}
	if hasStep(result.Steps, "verify ") {
		t.Errorf("a stopped backend was checked for still being up: %+v", result.Steps)
	}
}

// The delete pass has to see the whole rendered tree, not just the two
// directories we happen to write today. A file left anywhere under it by an
// older olr keeps being read by dnsmasq.
func TestObserveSeesFilesOutsideTheKnownDirectories(t *testing.T) {
	a, _ := testApplier(t)
	ctx := context.Background()
	if _, err := a.Apply(ctx, validConfig(t)); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	root := filepath.Dir(a.Paths.Conf)
	stale := filepath.Join(root, "legacy", "dnsmasq.conf.old")
	if err := os.MkdirAll(filepath.Dir(stale), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stale, []byte("dhcp-range=10.0.0.5,10.0.0.9\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// A temporary file from a crashed apply, on the other hand, is not intent
	// that leaked — scheduling a delete for a name that will never recur would
	// be noise in every plan.
	temp := filepath.Join(root, ".dnsmasq.conf.123456")
	if err := os.WriteFile(temp, []byte("half written"), 0o644); err != nil {
		t.Fatal(err)
	}

	obs, err := a.Observe(ctx)
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	if _, ok := obs.Files[stale]; !ok {
		t.Errorf("Observe missed %s; saw %v", stale, keys(obs.Files))
	}
	if _, ok := obs.Files[temp]; ok {
		t.Errorf("Observe picked up the temporary file %s", temp)
	}
}

func hasStep(steps []Step, prefix string) bool {
	for _, s := range steps {
		if strings.HasPrefix(s.Description, prefix) {
			return true
		}
	}
	return false
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func keys(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

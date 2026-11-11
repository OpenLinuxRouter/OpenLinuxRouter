package ingress

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// fakeProxy stands in for systemd.
type fakeProxy struct {
	calls   []string
	active  bool
	enabled bool

	notInstalled bool
	statusErr    error
	fail         error

	// diesAfter makes the unit go inactive after this many Status calls,
	// modelling a proxy that accepts the start job and then exits — what a
	// Caddy that dislikes its configuration looks like, since systemd has
	// already reported the job done by then.
	diesAfter int
	statusN   int
}

func (f *fakeProxy) Status(context.Context) (ProxyStatus, error) {
	if f.statusErr != nil {
		return ProxyStatus{Unit: UnitName}, f.statusErr
	}
	f.statusN++
	if f.diesAfter > 0 && f.statusN > f.diesAfter {
		f.active = false
		return ProxyStatus{Unit: UnitName, Active: false, State: "failed",
			Enabled: f.enabled, Installed: !f.notInstalled}, nil
	}
	return ProxyStatus{Unit: UnitName, Active: f.active, State: "active",
		Enabled: f.enabled, Installed: !f.notInstalled}, nil
}

func (f *fakeProxy) do(verb string) error {
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

func (f *fakeProxy) Start(context.Context) error   { return f.do("start") }
func (f *fakeProxy) Stop(context.Context) error    { return f.do("stop") }
func (f *fakeProxy) Restart(context.Context) error { return f.do("restart") }
func (f *fakeProxy) Reload(context.Context) error  { return f.do("reload") }
func (f *fakeProxy) Enable(context.Context) error  { return f.do("enable") }
func (f *fakeProxy) Disable(context.Context) error { return f.do("disable") }

// testApplier builds an Applier rooted entirely inside a temp directory, which
// is what makes the whole apply path testable without root, systemd, a Caddy
// binary, or /etc.
func testApplier(t *testing.T) (Applier, *fakeProxy) {
	t.Helper()
	root := t.TempDir()
	paths := RootedPaths(root)
	proxy := &fakeProxy{}
	return Applier{
		Backend: NewCaddy(paths),
		DNS:     goodDNS(),
		Clients: goodDevices(),
		Proxy:   proxy,
		Paths:   paths,
		Store:   core.NewStore(filepath.Join(root, "olr.json"), ModuleName),
		// Pinned rather than left to PortConflict, which would read the build
		// machine's /proc and make these tests depend on whether anything
		// happens to be serving HTTP there.
		PortCheck: func() ([]uint64, error) { return nil, nil },
		// A checker that approves, so these tests exercise the apply path
		// rather than the absence of a proxy binary. The refusal path has its
		// own test below.
		CheckConfig: func(context.Context, string, []string) error { return nil },
		// Pinned for the same reason PortCheck is: otherwise every test here
		// would depend on whether the build machine has a caddy on its PATH.
		Locate: func() (string, error) { return "/fake/caddy", nil },
		// Check the unit once rather than watching it for the default window.
		Settle: -1,
	}, proxy
}

func steps(r ApplyResult) string {
	var b strings.Builder
	for _, s := range r.Steps {
		b.WriteString(s.Description)
		if s.Skipped {
			b.WriteString(" [skipped]")
		}
		if s.Error != "" {
			b.WriteString(" !" + s.Error)
		}
		b.WriteString("\n")
	}
	return b.String()
}

func TestApplyOnAFreshSystem(t *testing.T) {
	a, proxy := testApplier(t)

	result, err := a.Apply(context.Background(), good())
	if err != nil {
		t.Fatalf("apply: %v\n%s", err, steps(result))
	}

	conf, err := os.ReadFile(a.Paths.Conf)
	if err != nil {
		t.Fatalf("reading the Caddyfile: %v", err)
	}
	if !strings.Contains(string(conf), "grafana.svc.example.com") {
		t.Errorf("the published name is not in the rendered file:\n%s", conf)
	}

	info, err := os.Stat(a.Paths.Env)
	if err != nil {
		t.Fatalf("reading the env file: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("env file mode = %v, want 0600 — this is the credential", info.Mode().Perm())
	}

	if want := []string{"enable", "start"}; strings.Join(proxy.calls, ",") != strings.Join(want, ",") {
		t.Errorf("service calls = %v, want %v", proxy.calls, want)
	}
}

// Re-applying the same intent is a no-op, which is what makes §5.3.2's
// "re-run to finish the job" safe to recommend.
func TestApplyIsIdempotent(t *testing.T) {
	a, proxy := testApplier(t)
	ctx := context.Background()

	if _, err := a.Apply(ctx, good()); err != nil {
		t.Fatalf("first apply: %v", err)
	}
	before := len(proxy.calls)

	result, err := a.Apply(ctx, good())
	if err != nil {
		t.Fatalf("second apply: %v\n%s", err, steps(result))
	}
	if !result.Plan.Empty() {
		t.Errorf("second apply saw drift: %v", result.Plan.Changes)
	}
	if len(proxy.calls) != before {
		t.Errorf("second apply touched the service: %v", proxy.calls[before:])
	}
}

// The whole point of checking before writing: a rejected render must leave the
// live configuration alone rather than take every published service down.
func TestApplyRefusesToWriteAConfigTheProxyRejects(t *testing.T) {
	a, proxy := testApplier(t)
	ctx := context.Background()

	if _, err := a.Apply(ctx, good()); err != nil {
		t.Fatalf("first apply: %v", err)
	}
	live, err := os.ReadFile(a.Paths.Conf)
	if err != nil {
		t.Fatal(err)
	}
	callsBefore := len(proxy.calls)

	a.CheckConfig = func(context.Context, string, []string) error {
		return errors.New("Caddyfile:12 - unrecognized directive: reverse_prox")
	}
	desired := good()
	desired.Services[0].Upstream.Port = 9999

	result, err := a.Apply(ctx, desired)
	if err == nil {
		t.Fatalf("expected the apply to be refused\n%s", steps(result))
	}
	if !strings.Contains(err.Error(), "unrecognized directive") {
		t.Errorf("the proxy's own message is the useful one, got %v", err)
	}

	after, err := os.ReadFile(a.Paths.Conf)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(live) {
		t.Error("a rejected render must not reach the live Caddyfile")
	}
	if len(proxy.calls) != callsBefore {
		t.Errorf("a rejected render must not signal the proxy: %v", proxy.calls[callsBefore:])
	}
}

// The check leaves nothing behind that the next plan would schedule a delete for.
func TestApplyLeavesNoCheckArtefacts(t *testing.T) {
	a, _ := testApplier(t)
	ctx := context.Background()
	if _, err := a.Apply(ctx, good()); err != nil {
		t.Fatalf("apply: %v", err)
	}

	entries, err := os.ReadDir(filepath.Dir(a.Paths.Conf))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if len(names) != 2 {
		t.Fatalf("expected only the Caddyfile and the env file, got %v", names)
	}

	drift, err := a.Drift(ctx)
	if err != nil {
		t.Fatalf("drift: %v", err)
	}
	if !drift.Empty() {
		t.Errorf("a completed apply must leave no drift, got %v", drift.Changes)
	}
}

// "We could not tell" is not "your configuration is broken".
func TestApplySkipsTheCheckWhenTheProxyIsNotInstalled(t *testing.T) {
	a, _ := testApplier(t)
	a.CheckConfig = func(context.Context, string, []string) error { return ErrNoChecker }

	result, err := a.Apply(context.Background(), good())
	if err != nil {
		t.Fatalf("a missing binary must not fail the apply: %v\n%s", err, steps(result))
	}
	var found bool
	for _, s := range result.Steps {
		if strings.Contains(s.Description, "Caddyfile is valid") {
			found = true
			if !s.Skipped {
				t.Error("the step must be reported as skipped, not as passed")
			}
			if !s.Done {
				t.Error("nothing is outstanding, so Done stays true")
			}
		}
	}
	if !found {
		t.Errorf("expected the check step to be recorded:\n%s", steps(result))
	}
}

func TestApplyRefusesToStartWhenSomethingElseHoldsAPort(t *testing.T) {
	a, _ := testApplier(t)
	a.PortCheck = func() ([]uint64, error) { return []uint64{80}, nil }

	result, err := a.Apply(context.Background(), good())
	if err == nil {
		t.Fatalf("expected a refusal\n%s", steps(result))
	}
	if !strings.Contains(err.Error(), "TCP/80") {
		t.Errorf("the error must name the port, got %v", err)
	}
	if !strings.Contains(err.Error(), "ss -ltnp") {
		t.Errorf("the error must tell the operator how to find the holder, got %v", err)
	}
}

// A reload of a proxy that is already bound does not trip over its own
// listeners, so the port check must not run for one.
func TestApplyDoesNotPortCheckAReload(t *testing.T) {
	a, _ := testApplier(t)
	ctx := context.Background()
	if _, err := a.Apply(ctx, good()); err != nil {
		t.Fatalf("first apply: %v", err)
	}

	a.PortCheck = func() ([]uint64, error) {
		// Which it now is — by us.
		return []uint64{80, 443}, nil
	}
	desired := good()
	desired.Services = append(desired.Services, Service{
		Name: "nas", Upstream: Upstream{Host: "10.0.0.9", Port: 5000},
	})
	if _, err := a.Apply(ctx, desired); err != nil {
		t.Fatalf("a reload must not be refused by our own listeners: %v", err)
	}
}

func TestApplyReloadsForACaddyfileChangeAndRestartsForACredential(t *testing.T) {
	a, proxy := testApplier(t)
	ctx := context.Background()
	if _, err := a.Apply(ctx, good()); err != nil {
		t.Fatalf("first apply: %v", err)
	}

	desired := good()
	desired.Services[0].Upstream.Port = 4000
	if _, err := a.Apply(ctx, desired); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if last := proxy.calls[len(proxy.calls)-1]; last != "reload" {
		t.Errorf("a Caddyfile edit should reload, got %q", last)
	}

	desired.Certificate.Token = "rotated"
	if _, err := a.Apply(ctx, desired); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if last := proxy.calls[len(proxy.calls)-1]; last != "restart" {
		t.Errorf("a new credential only takes effect on a restart, got %q", last)
	}
}

// Intent is stored before anything it describes, so a failure leaves a re-run
// something to finish from.
func TestApplyStoresIntentEvenWhenALaterStepFails(t *testing.T) {
	a, proxy := testApplier(t)
	proxy.fail = errors.New("systemd said no")

	result, err := a.Apply(context.Background(), good())
	if err == nil {
		t.Fatalf("expected the failure to surface\n%s", steps(result))
	}
	if result.Steps[0].Description != "store configuration in "+a.Store.Path() {
		t.Fatalf("intent must be stored first, got %q", result.Steps[0].Description)
	}
	if !result.Steps[0].Done {
		t.Error("the store step should have succeeded")
	}

	stored, err := a.Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(stored.Services) != 1 {
		t.Errorf("stored intent = %v, want the config we tried to apply", stored)
	}
}

func TestApplyRefusesAnUninstalledUnit(t *testing.T) {
	a, proxy := testApplier(t)
	proxy.notInstalled = true

	result, err := a.Apply(context.Background(), good())
	if err == nil {
		t.Fatalf("expected a refusal\n%s", steps(result))
	}
	if !strings.Contains(err.Error(), "olr enable") {
		t.Errorf("the error must name the command that writes the unit, got %v", err)
	}
}

// A proxy that accepts the start job and then exits is the failure this exists
// to catch: systemd reports success before Caddy has loaded a configuration.
func TestVerifyServingCatchesAProxyThatDies(t *testing.T) {
	a, proxy := testApplier(t)
	proxy.diesAfter = 2
	a.Settle = 0 // the real window

	result, err := a.Apply(context.Background(), good())
	if err == nil {
		t.Fatalf("expected the apply to notice\n%s", steps(result))
	}
	if !strings.Contains(err.Error(), "did not stay running") {
		t.Errorf("got %v", err)
	}
	if !strings.Contains(err.Error(), "olr ingress logs") {
		t.Errorf("the error should say where to look, got %v", err)
	}
}

// On a box with no system bus, "we could not tell" must not fail the apply.
func TestVerifyServingToleratesNoServiceManager(t *testing.T) {
	a, proxy := testApplier(t)
	proxy.statusErr = ErrNoServiceManager

	if err := a.verifyServing(context.Background()); err != nil {
		t.Errorf("expected tolerance, got %v", err)
	}
}

func TestApplyStopsWhenDisabled(t *testing.T) {
	a, proxy := testApplier(t)
	ctx := context.Background()
	if _, err := a.Apply(ctx, good()); err != nil {
		t.Fatalf("first apply: %v", err)
	}

	desired := good()
	desired.Enabled = false
	result, err := a.Apply(ctx, desired)
	if err != nil {
		t.Fatalf("apply: %v\n%s", err, steps(result))
	}
	if result.Plan.Impact != ImpactDisruptive {
		t.Errorf("impact = %s, want disruptive", result.Plan.Impact)
	}
	joined := strings.Join(proxy.calls, ",")
	if !strings.Contains(joined, "disable") || !strings.Contains(joined, "stop") {
		t.Errorf("disabling must both stop now and stay stopped after a reboot: %v", proxy.calls)
	}
}

// The config is stored, so the intent survives a disable and can be turned back
// on without retyping it.
func TestDisablingKeepsTheConfiguration(t *testing.T) {
	a, _ := testApplier(t)
	ctx := context.Background()
	if _, err := a.Apply(ctx, good()); err != nil {
		t.Fatalf("first apply: %v", err)
	}

	desired := good()
	desired.Enabled = false
	if _, err := a.Apply(ctx, desired); err != nil {
		t.Fatalf("apply: %v", err)
	}

	stored, err := a.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(stored.Services) != 1 || stored.Services[0].Name != "grafana" {
		t.Errorf("disabling must not discard published services, got %v", stored.Services)
	}
}

func TestUnmarshalConfigRejectsUnknownFields(t *testing.T) {
	if _, err := UnmarshalConfig([]byte(`{"enabled":true,"provider_tokn":"x"}`)); err == nil {
		t.Fatal("a misspelled credential key must not be silently ignored")
	}
}

// olr ships no proxy, so "there isn't one" is a first-class refusal with the
// instructions attached rather than an exec error at start time.
func TestApplyRefusesWhenThereIsNoProxyBinary(t *testing.T) {
	a, proxy := testApplier(t)
	a.Locate = func() (string, error) { return "", ErrNoBinary }

	result, err := a.Apply(context.Background(), good())
	if err == nil {
		t.Fatalf("expected a refusal\n%s", steps(result))
	}
	for _, want := range []string{"caddyserver.com/download", "xcaddy", SearchPath[0]} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal must mention %q, got:\n%v", want, err)
		}
	}
	if len(proxy.calls) != 0 {
		t.Errorf("nothing should have been asked of systemd: %v", proxy.calls)
	}
}

// Disabling must not need a proxy: an operator whose binary is gone still has to
// be able to turn the module off.
func TestApplyWithoutABinaryCanStillDisable(t *testing.T) {
	a, _ := testApplier(t)
	ctx := context.Background()
	if _, err := a.Apply(ctx, good()); err != nil {
		t.Fatalf("first apply: %v", err)
	}

	a.Locate = func() (string, error) { return "", ErrNoBinary }
	desired := good()
	desired.Enabled = false
	if result, err := a.Apply(ctx, desired); err != nil {
		t.Fatalf("disabling must not require a proxy: %v\n%s", err, steps(result))
	}
}

func TestParseProvidersReadsListModules(t *testing.T) {
	out := `
Standard modules: 118

dns.providers.cloudflare
dns.providers.route53
http.handlers.reverse_proxy
tls.issuance.acme

  Non-standard modules: 2
`
	got := parseProviders(out)
	if len(got) != 2 || got[0] != "cloudflare" || got[1] != "route53" {
		t.Fatalf("got %v, want [cloudflare route53]", got)
	}
}

// A format change must cost an empty list, never a wrong one.
func TestParseProvidersIgnoresEverythingElse(t *testing.T) {
	if got := parseProviders("total nonsense\nhttp.handlers.file_server\n"); len(got) != 0 {
		t.Fatalf("got %v, want nothing", got)
	}
}

// The state a stock Caddy is actually in, and the one this path has to explain
// rather than render as an empty dropdown. Verified against a real caddy v2.9.1:
// 127 modules, no DNS providers.
func TestAStockBuildHasNoProviders(t *testing.T) {
	stock := parseProviders("http.handlers.reverse_proxy\ntls.issuance.acme\nhttp.encoders.gzip\n")
	if HasProviders(stock) {
		t.Fatalf("got %v, want none", stock)
	}
	// An empty slice rather than nil, so the API answers [] and not null.
	if stock == nil {
		t.Error("parseProviders must not return nil; null reads as a missing field")
	}

	err := ErrNoProviders("/usr/bin/caddy")
	for _, want := range []string{"/usr/bin/caddy", "compiled in", "caddyserver.com/download", "xcaddy"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the explanation must mention %q, got:\n%v", want, err)
		}
	}
	// It must not read as "no proxy found" to somebody looking at one.
	if strings.Contains(err.Error(), "no proxy binary found") {
		t.Error("a wrong build is not a missing binary; the remedies differ")
	}
}

package ingress

import (
	"strings"
	"testing"
)

// renderInto produces the files a config would write, as an Observed — the
// "system already in this state" fixture every drift test needs.
func renderInto(t *testing.T, b Caddy, c Config) Observed {
	t.Helper()
	out, err := b.Render(c, goodDevices())
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	obs := Observed{Files: map[string][]byte{}, Running: true, ServiceKnown: true, Installed: true}
	obs.EnabledAtBoot = c.Enabled
	for _, f := range out.Files {
		obs.Files[f.Path] = f.Data
	}
	return obs
}

func testBackend(t *testing.T) Caddy {
	t.Helper()
	return NewCaddy(RootedPaths(t.TempDir()))
}

func plan(t *testing.T, b Caddy, desired Config, obs Observed) Plan {
	t.Helper()
	p, err := BuildPlan(b, desired, goodDNS(), goodDevices(), obs)
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}
	return p
}

// The drift answer: intent already applied means no plan.
func TestPlanIsEmptyWhenNothingChanged(t *testing.T) {
	b := testBackend(t)
	c := good()
	p := plan(t, b, c, renderInto(t, b, c))

	if !p.Empty() {
		t.Fatalf("expected no drift, got %d changes, action %s, reasons %v",
			len(p.Changes), p.Action, p.Reasons)
	}
	if p.Impact != ImpactNone {
		t.Errorf("impact = %s, want none", p.Impact)
	}
}

// Rewording an explanation in the renderer must not bounce every deployed proxy.
func TestPlanTreatsACommentOnlyChangeAsNoDrift(t *testing.T) {
	b := testBackend(t)
	c := good()
	obs := renderInto(t, b, c)

	// Stand in for a future release that reworded a comment: same directives,
	// different prose.
	obs.Files[b.Paths.Conf] = append([]byte("# an older olr said something else\n"),
		obs.Files[b.Paths.Conf]...)

	p := plan(t, b, c, obs)
	if !p.Empty() {
		t.Fatalf("a comment is not drift; got %v", p.Changes)
	}
	if p.Action != ActionNone {
		t.Errorf("action = %s, want none — a comment must not signal the proxy", p.Action)
	}
	// The file is still rewritten, because stale explanations help nobody.
	if len(p.Changes) != 1 || p.Changes[0].Impact != ImpactNone {
		t.Errorf("expected one cosmetic change, got %v", p.Changes)
	}
	if p.nothingToDo() {
		t.Error("cosmetic is not drift, but it is still a file to write")
	}
}

// The ordinary case: publishing a service is a graceful reload.
func TestPlanAddingAServiceReloads(t *testing.T) {
	b := testBackend(t)
	before := good()
	obs := renderInto(t, b, before)

	desired := before.Clone()
	desired.Services = append(desired.Services, Service{
		Name: "nas", Upstream: Upstream{Client: "nuc", Port: 5000},
	})

	p := plan(t, b, desired, obs)
	if p.Action != ActionReload {
		t.Errorf("action = %s, want reload", p.Action)
	}
	if p.Impact != ImpactReload {
		t.Errorf("impact = %s, want reload — nobody should notice a new name", p.Impact)
	}
	if len(p.Reasons) != 0 {
		t.Errorf("a reload needs no warning, got %v", p.Reasons)
	}
}

// The non-obvious one: the credential arrives through EnvironmentFile=, which
// systemd applies at process start, so a reload would leave the old token in use
// with nothing reporting a problem.
func TestPlanChangingTheTokenRestarts(t *testing.T) {
	b := testBackend(t)
	before := good()
	obs := renderInto(t, b, before)

	desired := before.Clone()
	desired.Certificate.Token = "rotated"

	p := plan(t, b, desired, obs)
	if p.Action != ActionRestart {
		t.Fatalf("action = %s, want restart: a reload would not pick up a new credential", p.Action)
	}
	if p.Impact != ImpactRestart {
		t.Errorf("impact = %s, want restart", p.Impact)
	}
	if len(p.Changes) != 1 || p.Changes[0].Path != b.Paths.Env {
		t.Fatalf("expected only the env file to change, got %v", p.Changes)
	}
	if !p.Changes[0].Secret {
		t.Error("the env file's change must be marked secret")
	}
	joined := strings.Join(p.Reasons, " ")
	if !strings.Contains(joined, "drops connections") {
		t.Errorf("an operator has to be told a restart costs connections, got %v", p.Reasons)
	}
}

// A box upgraded from an olr that rendered `admin off` has a proxy with no admin
// endpoint, and a reload is delivered through that endpoint. The first apply
// after the upgrade has to restart, or it fails at the reload and so does every
// one after it.
func TestPlanMovingTheAdminEndpointRestarts(t *testing.T) {
	b := testBackend(t)
	c := good()
	obs := renderInto(t, b, c)

	current := string(obs.Files[b.Paths.Conf])
	directive := "admin " + adminAddress(b.Paths.Admin)
	if !strings.Contains(current, directive) {
		t.Fatalf("fixture: rendered Caddyfile has no %q:\n%s", directive, current)
	}
	obs.Files[b.Paths.Conf] = []byte(strings.Replace(current, directive, "admin off", 1))

	p := plan(t, b, c, obs)
	if p.Action != ActionRestart {
		t.Fatalf("action = %s, want restart: a reload cannot reach a proxy whose admin "+
			"endpoint is elsewhere", p.Action)
	}
	if !strings.Contains(strings.Join(p.Reasons, " "), "admin socket") {
		t.Errorf("the restart should say why, got %v", p.Reasons)
	}
}

// And the ordinary edit after that is a reload again: the rule is about the
// admin line, not about the Caddyfile.
func TestPlanAddingAServiceAfterTheMoveStillReloads(t *testing.T) {
	b := testBackend(t)
	before := good()
	obs := renderInto(t, b, before)

	desired := before.Clone()
	desired.Services = append(desired.Services, Service{
		Name: "wiki", Upstream: Upstream{Host: "192.168.1.50", Port: 3001},
	})
	if p := plan(t, b, desired, obs); p.Action != ActionReload {
		t.Fatalf("action = %s, want reload", p.Action)
	}
}

// Removing a published name takes a URL away from whoever was using it, which is
// not recoverable by retrying.
func TestPlanRemovingAServiceIsDisruptive(t *testing.T) {
	b := testBackend(t)
	before := good()
	obs := renderInto(t, b, before)

	desired := before.Clone()
	desired.Services = nil

	p := plan(t, b, desired, obs)
	if p.Impact != ImpactDisruptive {
		t.Fatalf("impact = %s, want disruptive", p.Impact)
	}
	joined := strings.Join(p.Reasons, " ")
	if !strings.Contains(joined, "grafana.svc.example.com") {
		t.Errorf("the reason must name what stops answering, got %v", p.Reasons)
	}
}

func TestPlanDisablingIsDisruptive(t *testing.T) {
	b := testBackend(t)
	before := good()
	obs := renderInto(t, b, before)

	desired := before.Clone()
	desired.Enabled = false

	p := plan(t, b, desired, obs)
	if p.Action != ActionStop {
		t.Errorf("action = %s, want stop", p.Action)
	}
	if p.Impact != ImpactDisruptive {
		t.Errorf("impact = %s, want disruptive", p.Impact)
	}
	if p.Enable == nil || *p.Enable {
		t.Errorf("disabling must also take the unit out of boot, got %v", p.Enable)
	}
}

func TestPlanStartsWhenNotRunning(t *testing.T) {
	b := testBackend(t)
	c := good()
	obs := Observed{Files: map[string][]byte{}, ServiceKnown: true, Installed: true}

	p := plan(t, b, c, obs)
	if p.Action != ActionStart {
		t.Errorf("action = %s, want start", p.Action)
	}
	if p.Enable == nil || !*p.Enable {
		t.Errorf("an enabled module must also come back after a reboot, got %v", p.Enable)
	}
}

// A box with no system bus must not read as "not enabled" forever.
func TestPlanDoesNotScheduleEnableWhenServiceIsUnknown(t *testing.T) {
	b := testBackend(t)
	c := good()
	obs := Observed{Files: map[string][]byte{}} // ServiceKnown false

	if p := plan(t, b, c, obs); p.Enable != nil {
		t.Errorf("expected no enable step where systemd did not answer, got %v", *p.Enable)
	}
}

func TestPlanSchedulesDeletesForLeftoverFiles(t *testing.T) {
	b := testBackend(t)
	c := good()
	obs := renderInto(t, b, c)
	stale := b.Paths.Conf + ".old"
	obs.Files[stale] = []byte("left behind by an older olr\n")

	p := plan(t, b, c, obs)
	var found bool
	for _, ch := range p.Changes {
		if ch.Path == stale && ch.Kind == ChangeDelete {
			found = true
		}
	}
	if !found {
		t.Fatalf("a leftover file is drift, got %v", p.Changes)
	}
}

func TestPlanRefusesAnInvalidConfig(t *testing.T) {
	b := testBackend(t)
	c := good()
	c.Certificate.Provider = ""

	_, err := BuildPlan(b, c, goodDNS(), goodDevices(), Observed{Files: map[string][]byte{}})
	if err == nil {
		t.Fatal("planning an unappliable config must fail before anything is written")
	}
}

func TestServedNamesReadsOurOwnMatchers(t *testing.T) {
	b := testBackend(t)
	c := good()
	c.Services = append(c.Services, Service{Name: "nas", Upstream: Upstream{Host: "10.0.0.9", Port: 5000}})
	obs := renderInto(t, b, c)

	got := obs.Served(b.Paths.Conf)
	want := []string{"grafana.svc.example.com", "nas.svc.example.com"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

// It parses our output and nothing else. Anything unrecognised is simply not
// reported as served, which costs a warning and never invents one.
func TestServedNamesIgnoresAnythingElse(t *testing.T) {
	if got := servedNames([]byte("host not.a.matcher\n@svc_x path /foo\nreverse_proxy a:1\n")); got != nil {
		t.Fatalf("got %v, want nothing", got)
	}
}

func TestImpactRoundTripsThroughText(t *testing.T) {
	for _, want := range []Impact{ImpactNone, ImpactReload, ImpactRestart, ImpactDisruptive} {
		text, err := want.MarshalText()
		if err != nil {
			t.Fatalf("marshal %v: %v", want, err)
		}
		var got Impact
		if err := got.UnmarshalText(text); err != nil {
			t.Fatalf("unmarshal %q: %v", text, err)
		}
		if got != want {
			t.Errorf("round trip of %v gave %v", want, got)
		}
	}
	var i Impact
	if err := i.UnmarshalText([]byte("sideways")); err == nil {
		t.Error("an unknown impact must not decode silently")
	}
}

// The plan diff is printed in a terminal and pasted into issues.
func TestDiffWithholdsSecretContents(t *testing.T) {
	c := Change{
		Path: "/etc/open-linux-router/rendered/ingress/caddy.env",
		Kind: ChangeUpdate, Impact: ImpactRestart, Secret: true,
		Before: []byte("OLR_INGRESS_DNS_TOKEN=old-secret\n"),
		After:  []byte("OLR_INGRESS_DNS_TOKEN=new-secret\n"),
	}
	got := c.Diff()
	for _, leak := range []string{"old-secret", "new-secret"} {
		if strings.Contains(got, leak) {
			t.Fatalf("the diff leaked %q:\n%s", leak, got)
		}
	}
	if !strings.Contains(got, "contents withheld") {
		t.Errorf("the operator still has to be told the file changed:\n%s", got)
	}
	if !strings.Contains(got, "caddy.env") {
		t.Errorf("the path is not the secret:\n%s", got)
	}
}

func TestDiffShowsOrdinaryFiles(t *testing.T) {
	c := Change{
		Path: "/x/Caddyfile", Kind: ChangeUpdate, Impact: ImpactReload,
		Before: []byte("reverse_proxy 10.0.0.1:80\n"),
		After:  []byte("reverse_proxy 10.0.0.2:80\n"),
	}
	got := c.Diff()
	if !strings.Contains(got, "10.0.0.2") {
		t.Fatalf("a non-secret file must show its diff:\n%s", got)
	}
}

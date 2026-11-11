package dns

import (
	"net/netip"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// observedFor renders a config and presents it back as what is on disk, so a
// test can start from "already applied" and change one thing.
func observedFor(t *testing.T, b Backend, c Config, running bool) Observed {
	t.Helper()
	rendered, err := b.Render(c, testLinks())
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	obs := Observed{Files: map[string][]byte{}, Units: map[string]UnitState{}}
	for _, f := range rendered.Files {
		obs.Files[f.Path] = f.Data
	}
	for _, unit := range b.Units() {
		obs.Units[unit] = UnitState{
			Known: true, Running: running, EnabledAtBoot: running, Installed: true,
		}
	}
	return obs
}

func planFor(t *testing.T, b Backend, cfg Config, obs Observed) Plan {
	t.Helper()
	plan, err := BuildPlan(b, cfg, testLinks(), nil, obs, time.Now())
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}
	return plan
}

func actionFor(plan Plan, unit string) ServiceAction {
	for _, s := range plan.Services {
		if s.Unit == unit {
			return s.Action
		}
	}
	return ActionNone
}

// A backend that is enabled and not running is work, and naming it separately
// from the file half is what lets olrd converge one without the other.
func TestPlanStartsABackendThatIsNotRunning(t *testing.T) {
	b := testBackend(t)
	cfg := validConfig()

	// Every file already right, nothing running.
	obs := observedFor(t, b, cfg, false)

	plan := planFor(t, b, cfg, obs)
	if len(plan.Changes) != 0 {
		t.Fatalf("files were reported as changed: %+v", plan.Changes)
	}
	if !plan.StartsABackend() {
		t.Errorf("a stopped backend did not plan as a start: %+v", plan.Services)
	}
	if plan.RewritesFiles() {
		t.Error("the file half claimed work it does not have")
	}
	if plan.Empty() {
		t.Error("a box that is not answering DNS reported no drift")
	}
}

// The race the service half used to be excluded over. systemd brings the
// enabled units up at boot on its own schedule, and an olrd that read
// "activating" as "stopped" would start a unit that was already starting —
// which is olr and systemd racing for the socket at every boot.
func TestPlanLeavesAUnitSystemdIsAlreadyStarting(t *testing.T) {
	b := testBackend(t)
	cfg := validConfig()

	// Enabled at boot, which is why systemd is starting them, and not up yet.
	obs := observedFor(t, b, cfg, false)
	for unit, state := range obs.Units {
		state.Starting, state.EnabledAtBoot = true, true
		obs.Units[unit] = state
	}

	plan := planFor(t, b, cfg, obs)
	if plan.StartsABackend() {
		t.Errorf("olr planned to start a unit systemd is already starting: %+v", plan.Services)
	}
	if !plan.Empty() {
		t.Errorf("a box mid-boot reported drift: %+v", plan.Services)
	}
}

// The drift check (design.md §5.4): plan unchanged intent against reality and
// see whether the diff is empty.
func TestPlanOfAnAppliedConfigIsEmpty(t *testing.T) {
	b := testBackend(t)
	cfg := validConfig()

	plan := planFor(t, b, cfg, observedFor(t, b, cfg, true))
	if !plan.Empty() {
		t.Errorf("an applied config planned work: %+v %v", plan.Services, plan.Changes)
	}
	if plan.Impact != ImpactNone {
		t.Errorf("impact = %s, want none", plan.Impact)
	}
}

// The whole reason policies live in a directory: the relay re-reads it on
// SIGHUP, so the commonest edit costs nobody an interrupted query.
func TestAddingABlockedNameIsAReloadOfTheRelayAlone(t *testing.T) {
	b := testBackend(t)
	before := validConfig()
	obs := observedFor(t, b, before, true)

	after := validConfig()
	after.Policies = []Policy{{Name: "kids", Block: []string{"example.com"}}}
	after.Normalize()

	plan := planFor(t, b, after, obs)

	if got := actionFor(plan, b.RelayUnit()); got != ActionReload {
		t.Errorf("relay action = %s, want reload", got)
	}
	if got := actionFor(plan, b.ResolverUnit()); got != ActionNone {
		t.Errorf("resolver action = %s, want none — a blocklist is nothing to do with unbound", got)
	}
	if plan.Impact != ImpactReload {
		t.Errorf("impact = %s, want reload", plan.Impact)
	}
}

// A forwarder change is unbound's business and must not disturb the socket the
// whole network is talking to.
func TestChangingTheUpstreamRestartsOnlyTheResolver(t *testing.T) {
	b := testBackend(t)
	before := validConfig()
	obs := observedFor(t, b, before, true)

	after := validConfig()
	after.Upstream = Upstream{
		Mode:    ModeForward,
		Servers: []netip.AddrPort{netip.MustParseAddrPort("1.1.1.1:53")},
	}

	plan := planFor(t, b, after, obs)

	if got := actionFor(plan, b.ResolverUnit()); got != ActionRestart {
		t.Errorf("resolver action = %s, want restart", got)
	}
	if got := actionFor(plan, b.RelayUnit()); got != ActionNone {
		t.Errorf("relay action = %s, want none", got)
	}
}

// A policy removed from the config has to be removed from the directory too:
// the relay reads whatever is in it, not whatever we meant to put there.
func TestRemovingAPolicyDeletesItsFile(t *testing.T) {
	b := testBackend(t)
	before := validConfig()
	before.Policies = []Policy{{Name: "kids", Block: []string{"example.com"}}}
	before.Normalize()
	obs := observedFor(t, b, before, true)

	plan := planFor(t, b, validConfig(), obs)

	stale := filepath.Join(b.Paths.PolicyDir, "kids.json")
	var found bool
	for _, c := range plan.Changes {
		if c.Path == stale {
			found = true
			if c.Kind != ChangeDelete {
				t.Errorf("stale policy planned as %s, want delete", c.Kind)
			}
			if c.Impact != ImpactReload {
				t.Errorf("deleting a policy planned %s, want reload", c.Impact)
			}
			if c.Unit != b.RelayUnit() {
				t.Errorf("stale policy attributed to %q, want %q", c.Unit, b.RelayUnit())
			}
		}
	}
	if !found {
		t.Errorf("the stale policy file was not scheduled for deletion; changes: %v", plan.Changes)
	}
	// The delete is only half of it — without the signal the relay goes on
	// enforcing a rule the operator removed.
	if got := actionFor(plan, b.RelayUnit()); got != ActionReload {
		t.Errorf("relay action = %s, want reload", got)
	}
}

// A resolver going away costs everybody everything immediately, and does not
// present as "DNS is down" — it presents as the internet being broken.
func TestDisablingIsDisruptive(t *testing.T) {
	b := testBackend(t)
	cfg := validConfig()
	obs := observedFor(t, b, cfg, true)

	off := validConfig()
	off.Enabled = false

	plan := planFor(t, b, off, obs)

	if plan.Impact != ImpactDisruptive {
		t.Errorf("impact = %s, want disruptive", plan.Impact)
	}
	if got := actionFor(plan, b.RelayUnit()); got != ActionStop {
		t.Errorf("relay action = %s, want stop", got)
	}
	if len(plan.Reasons) == 0 || !strings.Contains(plan.Reasons[0], "every client") {
		t.Errorf("the reason does not say who is affected: %v", plan.Reasons)
	}
}

// A unit that is running now and not enabled costs nothing until the box
// reboots, and then costs the whole network its name resolution.
func TestBootStateIsDrift(t *testing.T) {
	b := testBackend(t)
	cfg := validConfig()
	obs := observedFor(t, b, cfg, true)
	for unit, state := range obs.Units {
		state.EnabledAtBoot = false
		obs.Units[unit] = state
	}

	plan := planFor(t, b, cfg, obs)

	if plan.Empty() {
		t.Fatal("a unit that would not come back after a reboot did not read as drift")
	}
	for _, s := range plan.Services {
		if s.Enable == nil || !*s.Enable {
			t.Errorf("%s: enable = %v, want true", s.Unit, s.Enable)
		}
	}
	if !strings.Contains(strings.Join(plan.Reasons, " "), "reboot") {
		t.Errorf("the reason does not mention the reboot: %v", plan.Reasons)
	}
}

// "We could not tell" and "it is off" are different answers. Treating the first
// as the second would make every box without a system bus report permanent
// drift and carry an enable step that can never be satisfied.
func TestUnknownServiceStateDoesNotManufactureAnEnableStep(t *testing.T) {
	b := testBackend(t)
	cfg := validConfig()
	obs := observedFor(t, b, cfg, true)
	for unit := range obs.Units {
		obs.Units[unit] = UnitState{} // nothing known
	}

	plan := planFor(t, b, cfg, obs)
	for _, s := range plan.Services {
		if s.Enable != nil {
			t.Errorf("%s carries an enable step on a box with no service manager", s.Unit)
		}
	}
}

// A reworded comment in a new release rewrites the file and must not restart
// the resolver, or `olr status` cries wolf on every upgrade.
func TestCosmeticChangesAreNotDrift(t *testing.T) {
	b := testBackend(t)
	cfg := validConfig()
	obs := observedFor(t, b, cfg, true)

	// Simulate the previous release's wording.
	old := strings.Replace(string(obs.Files[b.Paths.UnboundConf]),
		"# Generated by open-linux-router from", "# Written by open-linux-router from", 1)
	obs.Files[b.Paths.UnboundConf] = []byte(old)

	plan := planFor(t, b, cfg, obs)

	if plan.Empty() != true {
		t.Errorf("a comment change read as drift: %v", plan.Changes)
	}
	// It is still a file to write — Apply's early exit is a different predicate.
	if plan.nothingToDo() {
		t.Error("the file was not scheduled for rewriting")
	}
	for _, c := range plan.Changes {
		if c.Path == b.Paths.UnboundConf && c.Impact != ImpactNone {
			t.Errorf("comment-only change classified %s", c.Impact)
		}
	}
}

func TestBuildPlanRefusesAnInvalidConfig(t *testing.T) {
	b := testBackend(t)
	bad := validConfig()
	// An address on no interface this box has. Not an empty listen list, which
	// is now the ordinary default and means the wildcard.
	bad.Listen = []netip.AddrPort{netip.MustParseAddrPort("10.9.9.9:53")}

	if _, err := BuildPlan(b, bad, testLinks(), nil, Observed{}, time.Now()); err == nil {
		t.Fatal("planning succeeded against a config that cannot be applied")
	}
}

// Impact is an int with a text encoding, so without the pair a plan can be sent
// and never read — and `olr dns` is a client of its own module's API.
func TestImpactRoundTrips(t *testing.T) {
	for _, i := range []Impact{ImpactNone, ImpactReload, ImpactRestart, ImpactDisruptive} {
		text, err := i.MarshalText()
		if err != nil {
			t.Fatal(err)
		}
		var back Impact
		if err := back.UnmarshalText(text); err != nil {
			t.Fatalf("UnmarshalText(%q): %v", text, err)
		}
		if back != i {
			t.Errorf("%s round-tripped to %s", i, back)
		}
	}
	var i Impact
	if err := i.UnmarshalText([]byte("nonsense")); err == nil {
		t.Error("an unknown impact was accepted")
	}
}

// Network renumbering changes link facts, not DNS intent or rendered files.
func TestNetworkRenumberingDoesNotDriftDNS(t *testing.T) {
	b := testBackend(t)
	cfg := validConfig()
	cfg.Listen = nil
	obs := observedFor(t, b, cfg, true)
	links := testLinks()
	lan := links["lan0"]
	lan.Prefixes = []netip.Prefix{netip.MustParsePrefix("10.7.0.1/16"), netip.MustParsePrefix("2001:db8:1::1/64")}
	links["lan0"] = lan
	plan, err := BuildPlan(b, cfg, links, nil, obs, time.Now())
	if err != nil || !plan.Empty() {
		t.Fatalf("renumbering caused DNS drift: %+v, %v", plan, err)
	}
}

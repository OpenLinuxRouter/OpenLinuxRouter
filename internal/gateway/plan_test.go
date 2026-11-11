package gateway

import (
	"context"
	"net/netip"
	"strings"
	"testing"
)

// programmed returns a kernel already holding exactly what c asks for, which is
// the starting point for every "does an edit produce the right plan?" test.
func programmed(t *testing.T, c Config) *StaticKernel {
	t.Helper()
	c.Normalize()
	d := Render(c, testLinks(), nil)
	k := &StaticKernel{State: d.objectLines(), Sysctls: map[string]string{}}
	for _, s := range d.Sysctls {
		k.Sysctls[s.Key] = s.Value
	}
	return k
}

func planFor(t *testing.T, c Config, k *StaticKernel, admin netip.Addr) Plan {
	t.Helper()
	c.Normalize()
	obs, err := k.Observe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	plan, _, err := BuildPlan(c, testLinks(), nil, obs, admin)
	if err != nil {
		t.Fatalf("building the plan: %v", err)
	}
	return plan
}

func changeLines(p Plan) []string {
	out := make([]string, 0, len(p.Changes))
	for _, c := range p.Changes {
		out = append(out, string(c.Kind)+" "+c.Line)
	}
	return out
}

// design.md §5.4: drift is not separate machinery, it is the plan against
// unchanged intent coming back empty.
func TestAnAlreadyProgrammedKernelPlansEmpty(t *testing.T) {
	c := testConfig()
	plan := planFor(t, c, programmed(t, c), netip.Addr{})

	if !plan.Empty() {
		t.Fatalf("expected no drift, got %v", changeLines(plan))
	}
	if plan.Impact != ImpactNone {
		t.Errorf("impact = %s, want none", plan.Impact)
	}
}

func TestAHandRemovedRuleShowsAsDrift(t *testing.T) {
	c := testConfig()
	k := programmed(t, c)

	// Somebody ran `ip rule del`. On the other modules that would be a
	// hand-edited file; here the kernel is the file.
	var kept []string
	for _, l := range k.State {
		if !strings.HasPrefix(l, "rule ip priority 8100 ") {
			kept = append(kept, l)
		}
	}
	k.State = kept

	plan := planFor(t, c, k, netip.Addr{})
	if plan.Empty() {
		t.Fatal("a missing ip rule should be drift")
	}
	if got := changeLines(plan); len(got) != 1 || !strings.HasPrefix(got[0], "add rule ip priority 8100") {
		t.Errorf("expected the local guard to be re-added, got %v", got)
	}
}

// §3.4: an assignment edit only changes classify rules, and `ct mark` restore
// means flows already running keep the exit they started on. That is the whole
// reason the save/restore pair is worth two rules.
func TestChangingAnAssignmentIsAReloadNotADisruption(t *testing.T) {
	before := testConfig()
	k := programmed(t, before)

	after := testConfig()
	after.Interfaces[0].Exit = "Blocked"

	plan := planFor(t, after, k, netip.Addr{})
	if plan.Empty() {
		t.Fatal("the classify rules should have changed")
	}
	if plan.Impact != ImpactReload {
		t.Errorf("impact = %s, want reload; changes were %v", plan.Impact, changeLines(plan))
	}
}

func TestAddingADeviceOverrideIsAReload(t *testing.T) {
	before := testConfig()
	k := programmed(t, before)
	after := testConfig()
	after.SetDevice("aa:bb:cc:dd:ee:ff", "")
	plan := planFor(t, after, k, netip.Addr{})
	if plan.Empty() || plan.Impact != ImpactReload {
		t.Fatalf("client override should keep established flows: %+v", plan)
	}
}

// §5.3.3: `disruptive` is a fact, so it needs somebody actually using the path
// that moves.
func TestMovingAPathNobodyIsUsingIsNotDisruptive(t *testing.T) {
	before := testConfig()
	k := programmed(t, before)
	k.Active = nil

	after := testConfig()
	setExit(&after, "Proxy", func(e *Exit) { e.Via.NextHop = hop("192.168.1.51") })

	plan := planFor(t, after, k, netip.Addr{})
	if plan.Impact == ImpactDisruptive {
		t.Errorf("nothing is on the network, so nothing can be disconnected: %v", plan.Reasons)
	}
}

func TestMovingAPathSomebodyIsUsingIsDisruptive(t *testing.T) {
	before := testConfig()
	k := programmed(t, before)
	k.Active = []string{"192.168.1.23"}

	after := testConfig()
	setExit(&after, "Proxy", func(e *Exit) { e.Via.NextHop = hop("192.168.1.51") })

	plan := planFor(t, after, k, netip.Addr{})
	if plan.Impact != ImpactDisruptive {
		t.Fatalf("impact = %s, want disruptive", plan.Impact)
	}
	if !reasonsContain(plan, "192.168.1.23") {
		t.Errorf("the reason should name who is affected, got %v", plan.Reasons)
	}
}

// §5.1's last row: the one mistake this module can cause that the operator
// cannot undo by clicking again.
func TestRoutingTheAdminsOwnAddressIsDisruptive(t *testing.T) {
	before := Config{Enabled: true}
	k := programmed(t, before)

	after := testConfig()
	admin := netip.MustParseAddr("192.168.1.99")

	plan := planFor(t, after, k, admin)
	if plan.Impact != ImpactDisruptive {
		t.Fatalf("impact = %s, want disruptive", plan.Impact)
	}
	if !reasonsContain(plan, "disconnect you") {
		t.Errorf("the reason should say so plainly, got %v", plan.Reasons)
	}
}

func TestAnAdminOnAnUnaffectedNetworkIsNotWarned(t *testing.T) {
	before := Config{Enabled: true}
	k := programmed(t, before)

	plan := planFor(t, testConfig(), k, netip.MustParseAddr("10.9.9.9"))
	if reasonsContain(plan, "disconnect you") {
		t.Errorf("an address on no assigned network is not at risk: %v", plan.Reasons)
	}
}

// §6: structural detection, and refusal rather than racing another owner of the
// routing table.
func TestForeignRulesBlockThePlan(t *testing.T) {
	c := testConfig()
	k := programmed(t, c)
	k.Foreign = []ForeignRule{
		{Priority: 9000, Family: "ip", Table: 2022, Selector: "from all lookup 2022", HasDefault: true},
	}

	plan := planFor(t, c, k, netip.Addr{})
	if plan.Blocked == "" {
		t.Fatal("a foreign rule owning a default route should block the plan")
	}
	for _, want := range []string{"9000", "2022", "auto-route"} {
		if !strings.Contains(plan.Blocked, want) {
			t.Errorf("the refusal should mention %q, got %q", want, plan.Blocked)
		}
	}
}

// Tearing our own state down never conflicts with anybody, and refusing to do
// so would leave an operator unable to back out of the situation the refusal is
// about.
func TestForeignRulesDoNotBlockATeardown(t *testing.T) {
	c := testConfig()
	k := programmed(t, c)
	k.Foreign = []ForeignRule{{Priority: 9000, Table: 2022, HasDefault: true}}

	off := testConfig()
	off.Enabled = false

	plan := planFor(t, off, k, netip.Addr{})
	if plan.Blocked != "" {
		t.Fatalf("disabling should always be possible, got %q", plan.Blocked)
	}
}

func TestDisablingRemovesEverything(t *testing.T) {
	c := testConfig()
	k := programmed(t, c)

	off := testConfig()
	off.Enabled = false

	plan := planFor(t, off, k, netip.Addr{})
	for _, ch := range plan.Changes {
		if ch.Kind != ChangeRemove {
			t.Errorf("disabling should only remove, got %s %q", ch.Kind, ch.Line)
		}
	}
	if !reasonsContain(plan, "box's normal path") {
		t.Errorf("the operator should be told what disabling means, got %v", plan.Reasons)
	}
}

// A kernel we could not read is a different answer from a kernel with nothing
// in it, and conflating them would make every developer machine report drift.
func TestAnUnreadableKernelPlansNothing(t *testing.T) {
	c := testConfig()
	k := &StaticKernel{Unknown: true}

	plan := planFor(t, c, k, netip.Addr{})
	if !plan.Empty() {
		t.Errorf("nothing can be planned against an unknown kernel, got %v", changeLines(plan))
	}
}

// The sysctls are the one namespace we edit rather than own, so they move in
// one direction only.
func TestASysctlAlreadyAtZeroIsNotAChange(t *testing.T) {
	c := testConfig()
	c.Normalize()
	desired := Render(c, testLinks(), nil)

	k := &StaticKernel{State: desired.objectLines()}
	obs, _ := k.Observe(context.Background())
	obs.Sysctls = map[string]string{}
	for _, s := range desired.Sysctls {
		obs.Sysctls[s.Key] = s.Value
	}

	plan, _, err := BuildPlan(c, testLinks(), nil, obs, netip.Addr{})
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Empty() {
		t.Errorf("sysctls already correct should produce no work, got %v", changeLines(plan))
	}
}

func TestASysctlSomebodyElseSetIsNeverRemoved(t *testing.T) {
	// No exits at all, so we want no sysctls — but the box has some at 0
	// already. Those belong to whoever set them.
	c := Config{Enabled: true}
	c.Normalize()

	obs := Observed{
		Known: true,
		Sysctls: map[string]string{
			"net.ipv4.conf.eth9.send_redirects": "0",
		},
	}
	plan, _, err := BuildPlan(c, testLinks(), nil, obs, netip.Addr{})
	if err != nil {
		t.Fatal(err)
	}
	for _, ch := range plan.Changes {
		if strings.Contains(ch.Line, "eth9") {
			t.Errorf("we must not touch a sysctl we did not ask for: %s %q", ch.Kind, ch.Line)
		}
	}
}

func TestAllSendRedirectsIsReportedNotWritten(t *testing.T) {
	c := testConfig()
	c.Normalize()
	k := &StaticKernel{}
	on := true
	k.AllSendRedirects = &on

	plan := planFor(t, c, k, netip.Addr{})
	if !reasonsContain(plan, "net.ipv4.conf.all.send_redirects") {
		t.Fatalf("the machine-wide sysctl should be reported, got %v", plan.Reasons)
	}
	for _, ch := range plan.Changes {
		if strings.Contains(ch.Line, "conf.all.") {
			t.Errorf("olr must not write machine-wide sysctls: %q", ch.Line)
		}
	}
}

func withIPv6Forwarding(on bool) Config {
	c := testConfig()
	v := IPv6Forwarding(on)
	c.IPv6Forwarding = &v
	return c
}

// Turning v6 forwarding on purges every RA-learned default route on an
// interface whose accept_ra is not 2. That is this box losing its own IPv6 the
// moment the plan applies, so it is disruptive and names the interface — and
// the fix is reported, not written, because accept_ra there is not ours.
func TestTurningOnIPv6ForwardingNamesTheInterfacesItCosts(t *testing.T) {
	k := programmed(t, testConfig())
	k.RAHosts = []string{"enp1s0"}

	plan := planFor(t, withIPv6Forwarding(true), k, netip.Addr{})
	if plan.Impact != ImpactDisruptive {
		t.Errorf("impact = %s, want disruptive", plan.Impact)
	}
	if !reasonsContain(plan, "enp1s0") || !reasonsContain(plan, "accept_ra") {
		t.Errorf("the plan should name enp1s0 and the fix, got %v", plan.Reasons)
	}
	for _, ch := range plan.Changes {
		if strings.Contains(ch.Line, "accept_ra") {
			t.Errorf("accept_ra is the interface owner's to write, not ours: %q", ch.Line)
		}
	}
}

// Nothing learned from upstream, nothing to lose: a plain reload.
func TestTurningOnIPv6ForwardingWithNoRAHostsIsNotDisruptive(t *testing.T) {
	plan := planFor(t, withIPv6Forwarding(true), programmed(t, testConfig()), netip.Addr{})
	if plan.Impact == ImpactDisruptive {
		t.Errorf("nothing to lose, but impact = %s: %v", plan.Impact, plan.Reasons)
	}
	if !slicesContain(changeLines(plan), string(ChangeAdd)+" sysctl "+IPv6ForwardingSysctl+" = 1") {
		t.Errorf("want the sysctl written, got %v", changeLines(plan))
	}
}

// Already forwarding means the routes are already gone; saying so on every
// later plan would be the warning nobody reads.
func TestIPv6ForwardingAlreadyOnDoesNotWarnAgain(t *testing.T) {
	c := withIPv6Forwarding(true)
	k := programmed(t, c)
	k.RAHosts = []string{"enp1s0"}

	plan := planFor(t, c, k, netip.Addr{})
	if reasonsContain(plan, "enp1s0") {
		t.Errorf("already on, nothing new to report: %v", plan.Reasons)
	}
}

func slicesContain(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

func TestImpactRoundTripsThroughJSON(t *testing.T) {
	for _, want := range []Impact{ImpactNone, ImpactReload, ImpactRestart, ImpactDisruptive} {
		text, err := want.MarshalText()
		if err != nil {
			t.Fatal(err)
		}
		var got Impact
		if err := got.UnmarshalText(text); err != nil {
			t.Fatalf("%q: %v", text, err)
		}
		if got != want {
			t.Errorf("%q decoded as %s", text, got)
		}
	}
}

func TestApplyStoresIntentEvenWhenTheKernelRefuses(t *testing.T) {
	// A config that cannot be applied right now is still the config the
	// operator asked for; losing it on the way to reporting the problem would
	// make the problem worse.
	c := testConfig()
	c.Normalize()

	store := newTestStore(t)
	a := Applier{
		Kernel: &StaticKernel{ApplyErr: errBoom, FailAfter: 1},
		Links:  testLinks(),
		Store:  store,
	}

	result, stored, err := a.Apply(context.Background(), c, netip.Addr{})
	if err == nil {
		t.Fatal("expected the kernel failure to surface")
	}
	if len(stored.Exits) != len(c.Exits) {
		t.Errorf("intent was not stored: %+v", stored)
	}
	// §5.3.2: no rollback, so which steps landed is the operator's starting
	// point and has to be reported.
	if len(result.Steps) == 0 {
		t.Error("the steps that landed should be reported")
	}
	reloaded, err := a.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.Exits) != len(c.Exits) {
		t.Errorf("intent did not reach the document: %+v", reloaded)
	}
}

func reasonsContain(p Plan, substr string) bool {
	for _, r := range p.Reasons {
		if strings.Contains(r, substr) {
			return true
		}
	}
	return strings.Contains(p.Blocked, substr)
}

// The upgrade this version has to survive: a box whose accounting table was
// built by a version that keyed the wrong address. The sets are named the same,
// so only the rules' canonical lines say the table is stale — and if the plan
// cannot see that, applying will not fix the box.
//
// The old line is spelled out rather than derived, because deriving it from
// today's code would make the test agree with whatever the code does.
func TestAStaleAccountingTableIsPlannedForReplacement(t *testing.T) {
	c := testConfig()
	c.Normalize()

	// Verbatim what the previous version wrote, set name included. Using a
	// made-up name here would pass whatever the code did, because any observed
	// line missing from the desired set reads as a removal — the whole point is
	// that this is the line a real box actually has.
	const oldLine = "nft stat set up4 ipv4_addr . mark from ip saddr"
	obs := Observed{
		Known: true,
		Lines: []string{
			"nft table inet " + StatTableName,
			"nft chain " + AccountChain + " forward filter",
			oldLine,
		},
	}
	plan, _, err := BuildPlan(c, testLinks(), nil, obs, netip.Addr{})
	if err != nil {
		t.Fatal(err)
	}

	var removesOld, addsNew bool
	for _, ch := range plan.Changes {
		if ch.Line == oldLine {
			removesOld = true
		}
		if ch.Line == StatSets()[0].Line() {
			addsNew = true
		}
	}
	if !removesOld || !addsNew {
		t.Errorf("a table keyed the old way should plan as replaced "+
			"(removes old %v, adds new %v): %+v", removesOld, addsNew, plan.Changes)
	}

	// And it must not read as disruptive. Rebuilding the counters costs the
	// running totals, which §7.5 already says do not survive a reboot; it does
	// not move a single packet, and a dialog that says otherwise here is the
	// one that trains people to click through the dialog that matters.
	if plan.Impact == ImpactDisruptive {
		t.Errorf("replacing the accounting table moves no traffic: %v", plan.Reasons)
	}
}

// §3.8, and the reason forwarding is read back rather than written once and
// trusted: Docker, libvirt and k8s all write the global key on startup, and the
// kernel's write path resets every per-client value with it. A box olr left
// forwarding can stop forwarding because something else was installed, and the
// plan is where that has to surface.
func TestForwardingTurnedOffBehindUsIsDrift(t *testing.T) {
	c := testConfig()
	c.Normalize()
	k := programmed(t, c)
	k.Sysctls[ForwardingSysctl] = "0"

	plan := planFor(t, c, k, netip.Addr{})
	if !containsLine(plan, "add sysctl net.ipv4.ip_forward = 1") {
		t.Fatalf("a box that stopped forwarding has drifted, got %v", changeLines(plan))
	}
}

// The other direction of the same read: a box already forwarding is not work.
// Without this the plan would report a change on every apply of every config,
// and design.md §5.4's "an empty plan means the box agrees with intent" stops
// being true for every router olr has ever configured.
func TestForwardingAlreadyOnIsNotAChange(t *testing.T) {
	c := testConfig()
	c.Normalize()
	k := programmed(t, c)

	plan := planFor(t, c, k, netip.Addr{})
	for _, l := range changeLines(plan) {
		if strings.Contains(l, "ip_forward") {
			t.Errorf("a box already forwarding needs no work: %q", l)
		}
	}
}

func containsLine(p Plan, want string) bool {
	for _, l := range changeLines(p) {
		if l == want {
			return true
		}
	}
	return false
}

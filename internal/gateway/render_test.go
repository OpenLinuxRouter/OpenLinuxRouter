package gateway

import (
	"fmt"
	"net/netip"
	"strings"
	"testing"
)

func lines(t *testing.T, c Config, health Health) []string {
	t.Helper()
	c.Normalize()
	if res := Validate(c, testLinks()); !res.OK() {
		t.Fatalf("config under test does not validate: %v", res.Errors)
	}
	return Render(c, testLinks(), health).Lines()
}

func contains(lines []string, want string) bool {
	for _, l := range lines {
		if l == want {
			return true
		}
	}
	return false
}

func containsPrefix(lines []string, prefix string) bool {
	for _, l := range lines {
		if strings.HasPrefix(l, prefix) {
			return true
		}
	}
	return false
}

func dump(lines []string) string { return "\n" + strings.Join(lines, "\n") }

func sprintf(format string, args ...any) string { return fmt.Sprintf(format, args...) }

// setExit edits one exit by name, because Normalize sorts the list and an index
// written today is a different exit tomorrow.
func setExit(c *Config, name string, edit func(*Exit)) {
	for i := range c.Exits {
		if c.Exits[i].Name == name {
			edit(&c.Exits[i])
			return
		}
	}
	panic("no exit named " + name)
}

// The rule §3.3 calls load-bearing and the most common way a hand-rolled setup
// breaks: without it an exit table's default route swallows traffic to your own
// LAN, and the router stops being able to reach the devices behind it.
func TestTheLocalGuardIsAlwaysPresentInBothFamilies(t *testing.T) {
	got := lines(t, testConfig(), nil)
	for _, fam := range []string{"ip", "ip6"} {
		want := "rule " + fam + " priority 8100 from all lookup main suppress_prefixlength 0"
		if !contains(got, want) {
			t.Errorf("missing %q in %s", want, dump(got))
		}
	}
}

func TestEachExitGetsOneRuleAndOneRoutePerFamily(t *testing.T) {
	c := testConfig()
	c.Normalize()
	proxy, _ := c.Find("Proxy")

	got := lines(t, c, nil)

	// Derived from the slot rather than hardcoded, because which slot an exit
	// lands in is an allocation detail and asserting on it would make this test
	// fail every time an unrelated exit was renamed.
	wantRule := sprintf("rule ip priority %d fwmark %#08x/%#08x lookup %d",
		proxy.Priority(), proxy.Mark(), MarkMask, proxy.Table())
	if !contains(got, wantRule) {
		t.Errorf("missing %q in %s", wantRule, dump(got))
	}
	wantRoute := sprintf("route ip table %d default via 192.168.1.50 dev br-lan", proxy.Table())
	if !contains(got, wantRoute) {
		t.Errorf("missing %q in %s", wantRoute, dump(got))
	}
}

// §5.4: a v4-only exit with working IPv6 on the clients sends everything with
// an AAAA record out the default path at full speed, unnoticed.
func TestAV4OnlyNextHopBlocksIPv6RatherThanLeakingIt(t *testing.T) {
	c := testConfig()
	c.Normalize()
	proxy, _ := c.Find("Proxy")

	got := lines(t, c, nil)
	want := sprintf("route ip6 table %d unreachable default", proxy.Table())
	if !contains(got, want) {
		t.Errorf("IPv6 should be refused, not left to the default path: %s", dump(got))
	}
}

func TestIPv6DirectInstallsNoV6RuleAtAll(t *testing.T) {
	c := testConfig()
	c.Normalize()
	setExit(&c, "Proxy", func(e *Exit) { e.IPv6 = IPv6Direct })
	proxy, _ := c.Find("Proxy")
	got := lines(t, c, nil)

	if containsPrefix(got, sprintf("route ip6 table %d", proxy.Table())) {
		t.Errorf("direct means no v6 route, so the mark selects nothing: %s", dump(got))
	}
	if containsPrefix(got, sprintf("rule ip6 priority %d", proxy.Priority())) {
		t.Errorf("a v6 rule with no table behind it would fall through to main: %s", dump(got))
	}
}

func TestAnInterfaceExitCarriesBothFamilies(t *testing.T) {
	c := Config{
		Enabled:    true,
		Exits:      []Exit{{Name: "Office", Via: Via{Kind: ViaInterface, Interface: "wg0"}}},
		Interfaces: []Assignment{{Interface: "br-lan", Exit: "Office"}},
	}
	got := lines(t, c, nil)

	if !contains(got, "route ip table 8101 default dev wg0") {
		t.Errorf("missing the v4 route: %s", dump(got))
	}
	if !contains(got, "route ip6 table 8101 default dev wg0") {
		t.Errorf("an interface carries v6 too: %s", dump(got))
	}
	_ = c
}

// §3.7: unreachable, never blackhole. Blackhole drops silently and every
// connection hangs for thirty seconds.
func TestBlockedIsUnreachable(t *testing.T) {
	c := testConfig()
	c.Normalize()
	blocked, _ := c.Find("Blocked")

	got := lines(t, c, nil)
	want := "route ip table " + itoa(blocked.Table()) + " unreachable default"
	if !contains(got, want) {
		t.Errorf("missing %q in %s", want, dump(got))
	}
}

func TestClassifyRulesMatchTheNetworksOwnPrefixes(t *testing.T) {
	c := testConfig()
	c.Normalize()
	proxy, _ := c.Find("Proxy")

	got := lines(t, c, nil)

	if !contains(got, sprintf("nft source ip 192.168.1.0/24 mark %#08x from br-lan via Proxy unless dnat", proxy.Mark())) {
		t.Errorf("missing the classify rule for br-lan: %s", dump(got))
	}
	// The prefix is masked, so a link reporting 192.168.1.1/24 classifies the
	// whole subnet rather than one address.
	if containsPrefix(got, "nft source ip 192.168.1.1/24") {
		t.Errorf("prefix was not masked: %s", dump(got))
	}
}

// §3.5's DNAT guard, and the reason it is asserted on the *text* rather than on
// the expression list: the canonical line is what a box's rules are compared
// against, so a kernel still holding the older, unguarded rules has to read back
// as drift. If this string stopped being part of the line, the fix would live in
// the binary and never reach a box that had already applied once.
//
// What it prevents is in docs/port-forwarding.md §6: without it, the reply leg of a
// port-forwarded connection matches the source rule — conntrack has not restored
// the original destination yet — and is sent out the exit, so port forwarding
// silently fails on every box that has one.
func TestClassifyRulesExcludeDNATedConnections(t *testing.T) {
	c := testConfig()
	c.Normalize()

	for _, line := range lines(t, c, nil) {
		if !strings.HasPrefix(line, "nft source ") {
			continue
		}
		if !strings.HasSuffix(line, " unless dnat") {
			t.Errorf("classify rule does not exclude DNATed connections: %q", line)
		}
	}
}

// §3.4's flow stickiness, which is what keeps an assignment edit `reload`
// rather than `disruptive`.
func TestRestoreAndAccountRulesExistPerExitInUse(t *testing.T) {
	c := testConfig()
	c.Normalize()
	proxy, _ := c.Find("Proxy")

	got := lines(t, c, nil)

	if !contains(got, sprintf("nft restore mark %#08x for Proxy", proxy.Mark())) {
		t.Errorf("no ct-mark restore for Proxy: %s", dump(got))
	}
	if !contains(got, sprintf("nft account mark %#08x counter exit%d for Proxy", proxy.Mark(), proxy.Slot)) {
		t.Errorf("no accounting rule for Proxy: %s", dump(got))
	}
	// §7.3: show what you cannot account for.
	if !contains(got, "nft count-unpoliced") {
		t.Errorf("the residual counter is missing: %s", dump(got))
	}
	if !contains(got, "nft counter "+UnpolicedCounter) {
		t.Errorf("the residual counter object is missing: %s", dump(got))
	}
}

func TestRulesAreInEvaluationOrder(t *testing.T) {
	got := lines(t, testConfig(), nil)
	index := func(prefix string) int {
		for i, l := range got {
			if strings.HasPrefix(l, prefix) {
				return i
			}
		}
		return -1
	}

	restore, source := index("nft restore"), index("nft source")
	account, unpoliced := index("nft account"), index("nft count-unpoliced")
	if !(restore < source && source < account && account < unpoliced) {
		t.Errorf("rules out of evaluation order (restore %d, source %d, account %d, unpoliced %d): %s",
			restore, source, account, unpoliced, dump(got))
	}
}

func TestDeviceOverridesPrecedeNetworkRules(t *testing.T) {
	c := testConfig()
	c.SetDevice("aa:bb:cc:dd:ee:01", "")
	c.SetDevice("aa:bb:cc:dd:ee:02", "Blocked")
	got := lines(t, c, nil)
	direct := sprintf("nft device aa:bb:cc:dd:ee:01 on br-lan source 192.168.1.0/24 direct mark %#08x unless dnat", DirectMark)
	blocked, _ := c.Find("Blocked")
	proxy, _ := c.Find("Proxy")
	via := sprintf("nft device aa:bb:cc:dd:ee:02 on br-lan source 192.168.1.0/24 mark %#08x via Blocked unless dnat", blocked.Mark())
	network := sprintf("nft source ip 192.168.1.0/24 mark %#08x from br-lan via Proxy unless dnat", proxy.Mark())
	index := func(s string) int {
		for i, line := range got {
			if line == s {
				return i
			}
		}
		return -1
	}
	if index(direct) < 0 || index(via) < 0 || index(network) < 0 ||
		index(direct) >= index(network) || index(via) >= index(network) {
		t.Fatalf("client override must precede network classification: %s", dump(got))
	}
	if !contains(got, sprintf("nft restore mark %#08x for direct", DirectMark)) ||
		!contains(got, sprintf("nft account mark %#08x counter direct for direct", DirectMark)) {
		t.Fatalf("direct decision must survive flow changes: %s", dump(got))
	}
}

func TestDeviceRulesAreLimitedToAdoptedInterfacesAndPrefixes(t *testing.T) {
	c := testConfig()
	c.SetDevice("aa:bb:cc:dd:ee:01", "Proxy")
	links := testLinks()
	links["br-foreign"] = LinkInfo{Prefixes: []netip.Prefix{netip.MustParsePrefix("10.9.0.1/24")}}
	links["br-empty"] = LinkInfo{Adopted: true}
	c.Normalize()
	got := Render(c, links, nil).Lines()
	for _, line := range got {
		if strings.HasPrefix(line, "nft device ") &&
			(strings.Contains(line, "br-foreign") || strings.Contains(line, "br-empty")) {
			t.Errorf("client override escaped its owned network: %s", line)
		}
	}
}

// §5.3, and §11 open decision 2 resolved as the doc leans.
func TestANextHopSNATsByDefault(t *testing.T) {
	c := testConfig()
	c.Normalize()
	proxy, _ := c.Find("Proxy")

	got := lines(t, c, nil)
	want := sprintf("nft snat mark %#08x dev br-lan for Proxy", proxy.Mark())
	if !contains(got, want) {
		t.Errorf("missing %q in %s", want, dump(got))
	}
}

func TestSNATCanBeTurnedOff(t *testing.T) {
	c := testConfig()
	c.Normalize()
	off := false
	setExit(&c, "Proxy", func(e *Exit) { e.SNAT = &off })
	got := lines(t, c, nil)

	if containsPrefix(got, "nft snat") {
		t.Errorf("snat:false should install nothing: %s", dump(got))
	}
}

// §5.2, in both directions: the client is told to bypass us, and the proxy box
// tells us to add routes we did not choose.
func TestRedirectSysctlsAreSetOnTheNextHopsInterface(t *testing.T) {
	got := lines(t, testConfig(), nil)
	for _, key := range []string{
		"sysctl net.ipv4.conf.br-lan.send_redirects = 0",
		"sysctl net.ipv4.conf.br-lan.accept_redirects = 0",
		"sysctl net.ipv6.conf.br-lan.accept_redirects = 0",
	} {
		if !contains(got, key) {
			t.Errorf("missing %q in %s", key, dump(got))
		}
	}
	// Never machine-wide: those interfaces were not handed to us.
	if containsPrefix(got, "sysctl net.ipv4.conf.all") {
		t.Errorf("we must not write all.*: %s", dump(got))
	}
}

// §5.5: block is the default, and the UI can then say "no internet — Proxy is
// down", which is a sentence somebody can act on.
func TestADownExitBlocksItsTraffic(t *testing.T) {
	c := testConfig()
	c.Normalize()
	proxy, _ := c.Find("Proxy")

	got := lines(t, c, Health{"Proxy": false})
	want := sprintf("route ip table %d unreachable default", proxy.Table())
	if !contains(got, want) {
		t.Errorf("a down exit should refuse rather than leak: %s", dump(got))
	}
}

func TestADownExitSetToDirectInstallsNothing(t *testing.T) {
	c := testConfig()
	c.Normalize()
	setExit(&c, "Proxy", func(e *Exit) { e.OnFailure = FailDirect })
	proxy, _ := c.Find("Proxy")

	got := lines(t, c, Health{"Proxy": false})

	if containsPrefix(got, sprintf("route ip table %d ", proxy.Table())) {
		t.Errorf("failing open means no table entry at all: %s", dump(got))
	}
	if containsPrefix(got, sprintf("rule ip priority %d ", proxy.Priority())) {
		t.Errorf("failing open means no policy rule, so traffic uses main: %s", dump(got))
	}
}

func TestAnExitNobodyUsesIsNotProgrammed(t *testing.T) {
	c := testConfig()
	c.Exits = append(c.Exits, Exit{Name: "Spare", Via: Via{Kind: ViaInterface, Interface: "wg0"}})
	c.Normalize()
	spare, _ := c.Find("Spare")

	got := lines(t, c, nil)
	if containsPrefix(got, "route ip table "+itoa(spare.Table())) {
		t.Errorf("a configured but unassigned exit should not reach the kernel: %s", dump(got))
	}
}

func TestDisabledRendersNoRoutingPolicy(t *testing.T) {
	c := testConfig()
	c.Enabled = false
	got := lines(t, c, nil)

	for _, prefix := range []string{"rule ", "route ", "nft source", "nft snat", "sysctl net.ip"} {
		if containsPrefix(got, prefix) {
			t.Errorf("disabling should leave the box routing normally, but found %q: %s",
				prefix, dump(got))
		}
	}
}

// §7.1: the accounting table does not depend on the routing one. With no policy
// installed every packet carries mark 0 and the same structure degrades to
// plain per-client totals — so a box that only wants to know who is using the
// bandwidth gets that without routing anything.
func TestAccountingSurvivesRoutingBeingDisabled(t *testing.T) {
	c := testConfig()
	c.Enabled = false
	got := lines(t, c, nil)

	if !contains(got, "nft table inet "+StatTableName) {
		t.Errorf("the accounting table should still be installed: %s", dump(got))
	}
	for _, s := range StatSets() {
		if !contains(got, s.Line()) {
			t.Errorf("missing accounting set %q: %s", s.Name, dump(got))
		}
	}
}

// §7.1: three chains, because forwarding is not the only way bytes reach a
// device. A name Ingress publishes is terminated here, and before the local
// chains existed every byte of it was invisible: the client page read 0 B/s
// while a video streamed at 12 MB/s, because caddy on the box was the other end
// of the connection and no packet of it was ever forwarded.
func TestAccountingCountsTheLocalHalf(t *testing.T) {
	got := lines(t, testConfig(), nil)

	for _, c := range StatChains() {
		if !contains(got, c.Line()) {
			t.Errorf("missing accounting chain: %q", c.Line())
		}
		for _, r := range c.Rules {
			if !contains(got, r.Line()) {
				t.Errorf("chain %s is missing %q", c.Name, r.Line())
			}
		}
	}

	// The local chains carry the ordinary rule only: a translated connection's
	// remote end is this box, and this box's own address is no device's.
	for _, c := range StatChains() {
		if c.Name == AccountChain {
			continue
		}
		for _, r := range c.Rules {
			if r.DNAT || strings.Contains(r.Line(), "status dnat") {
				t.Errorf("%s carries %q, which would key this box's own address", c.Name, r.Line())
			}
			switch {
			case c.Hook == "input" && r.Set.Down:
				t.Errorf("%s on input carries %s: input counts what a device sent here", c.Name, r.Set.Name)
			case c.Hook == "output" && !r.Set.Down:
				t.Errorf("%s on output carries %s: output counts what it was sent back", c.Name, r.Set.Name)
			}
		}
	}

	// And every set is still reachable from the forward chain, so a device on
	// the same network as the router keeps being counted there.
	fwd := StatChains()[0]
	if fwd.Name != AccountChain || fwd.Hook != "forward" {
		t.Fatalf("the first chain must be the forward hook: %+v", fwd)
	}
	for _, s := range StatSets() {
		if !contains(got, s.Line()) || !contains(got, s.DNATLine()) {
			t.Errorf("the forward chain no longer carries both halves of %s", s.Name)
		}
	}
}

func TestAccountingCanBeTurnedOff(t *testing.T) {
	// §7.5 asks for a visible off switch, and this is it.
	c := testConfig()
	off := false
	c.Stats = &off
	got := lines(t, c, nil)

	if containsPrefix(got, "nft table inet "+StatTableName) || containsPrefix(got, "nft stat set") {
		t.Errorf("stats:false should install no accounting at all: %s", dump(got))
	}
	// And it must not have taken the routing half with it.
	if !containsPrefix(got, "route ip table ") {
		t.Errorf("turning accounting off must not disturb routing: %s", dump(got))
	}
}

func TestAccountingIsOnByDefault(t *testing.T) {
	// A stored document from before this field existed reads as nil, which has
	// to mean on — otherwise upgrading silently stops counting.
	var c Config
	if !c.StatsOrDefault() {
		t.Error("accounting should default on")
	}
	on := true
	c.Stats = &on
	if !c.StatsOrDefault() {
		t.Error("an explicit true should be honoured")
	}
}

// Every packet must enter exactly one accounting set: ordinary connections
// use the opener, while DNATed connections use the translated local endpoint.
func TestAccountingKeysLocalEndpointForDNAT(t *testing.T) {
	for _, spec := range StatSets() {
		normal, dnat := spec.Line(), spec.DNATLine()
		if !strings.Contains(normal, "status not dnat") || !strings.Contains(dnat, "status dnat") {
			t.Errorf("%s must partition DNAT status: %q / %q", spec.Name, normal, dnat)
		}
		wantNormal, wantDNAT := "original", "reply"
		if spec.Down {
			wantNormal, wantDNAT = "reply", "original"
		}
		if !strings.Contains(normal, "ct direction "+wantNormal) || !strings.Contains(dnat, "ct direction "+wantDNAT) {
			t.Errorf("%s has wrong direction: %q / %q", spec.Name, normal, dnat)
		}
		for _, line := range []string{normal, dnat} {
			if !contains(lines(t, testConfig(), nil), line) {
				t.Errorf("missing %q", line)
			}
		}
	}
}

// An older box has a table whose rules are shaped differently but whose sets
// have the same names. The set names alone therefore cannot decide whether it
// needs rebuilding, and the canonical line is what does — so every one of the
// four has to be distinct from every other.
func TestAccountingLineDistinguishesTheDirections(t *testing.T) {
	seen := map[string]string{}
	for _, s := range StatSets() {
		if prev, dup := seen[s.Line()]; dup {
			t.Errorf("%s and %s render the same line %q, so drift between them "+
				"would be invisible", prev, s.Name, s.Line())
		}
		seen[s.Line()] = s.Name
		if prev, dup := seen[s.DNATLine()]; dup {
			t.Errorf("%s and %s share %q", prev, s.Name, s.DNATLine())
		}
		seen[s.DNATLine()] = s.Name
	}
}

func TestAccountingKeyWidthsMatchTheKernelPadding(t *testing.T) {
	// Each concatenated component is padded to the four-byte register the
	// kernel builds the key in, which is why a v4 key is eight bytes and not
	// five. decodeStatKey splits on exactly these offsets.
	for _, s := range StatSets() {
		want := 8
		if s.V6 {
			want = 20
		}
		if got := s.KeyBytes(); got != want {
			t.Errorf("%s key is %d bytes, want %d", s.Name, got, want)
		}
	}
}

func TestInheritingTheBoxDefaultMarksNothing(t *testing.T) {
	// Nothing to mark: unmarked traffic uses `main`, which is exactly what "use
	// the box's own connection" asks for.
	c := Config{
		Enabled:    true,
		Exits:      []Exit{{Name: "Proxy", Via: Via{Kind: ViaBlocked}}},
		Interfaces: []Assignment{{Interface: "br-lan"}},
	}
	got := lines(t, c, nil)

	if containsPrefix(got, "nft source") {
		t.Errorf("no classify rule should be emitted: %s", dump(got))
	}
}

func TestCanonicalLinesFitTheKernelsUserdataLimit(t *testing.T) {
	// Each nft line is stored in that rule's netlink userdata, which the kernel
	// caps at 256 bytes. A name at the limit must still fit.
	c := Config{
		Enabled: true,
		Exits: []Exit{{
			Name: strings.Repeat("x", MaxNameLen),
			Via:  Via{Kind: ViaNextHop, NextHop: hop("192.168.1.50")},
		}},
		Interfaces: []Assignment{{Interface: "br-lan", Exit: strings.Repeat("x", MaxNameLen)}},
	}
	for _, l := range lines(t, c, nil) {
		if strings.HasPrefix(l, "nft ") && len(l) > 200 {
			t.Errorf("line is %d bytes, too close to the 256-byte userdata limit: %q", len(l), l)
		}
	}
}

// §3.8. Every rule, route and SNAT entry in this module describes what happens
// to a packet addressed to somewhere else, and the kernel drops that packet
// before consulting any of them while this is 0. A module that programs the
// whole forwarding path and leaves the box not forwarding is broken.
func TestForwardingIsTurnedOn(t *testing.T) {
	got := lines(t, testConfig(), nil)
	if !contains(got, "sysctl net.ipv4.ip_forward = 1") {
		t.Errorf("a router has to forward: %s", dump(got))
	}
}

// The case that motivated writing it at all, and the one an exit-keyed
// condition would have missed: `default` unset means "everything uses this
// box's own connection", which is precisely a box that has to forward. It is
// also the shape every olr install has before its first exit is added.
func TestForwardingIsTurnedOnWithNoExitsAtAll(t *testing.T) {
	c := Config{Enabled: true}
	c.Normalize()
	got := Render(c, testLinks(), nil).Lines()

	if !contains(got, "sysctl net.ipv4.ip_forward = 1") {
		t.Errorf("a box with no exits still forwards for its own LAN: %s", dump(got))
	}
}

// Switched off means "leave the box routing exactly as it did before olr was
// installed" (Desired.Enabled), and a box we taught to forward is not that.
func TestADisabledModuleWritesNoSysctls(t *testing.T) {
	c := Config{Enabled: false}
	c.Normalize()
	got := Render(c, testLinks(), nil).Lines()

	if containsPrefix(got, "sysctl net.ip") {
		t.Errorf("disabled routing must not change IP sysctls: %s", dump(got))
	}
}

// Unset means olr does not touch it. IPv6 forwarding is not free the way IPv4's
// is — it costs the box any IPv6 it learned from upstream — so it is never
// inferred, not even on an otherwise complete gateway config.
func TestIPv6ForwardingIsNotWrittenUnlessAskedFor(t *testing.T) {
	got := lines(t, testConfig(), nil)
	for _, l := range got {
		if strings.Contains(l, IPv6ForwardingSysctl) {
			t.Errorf("nobody asked for v6 forwarding, so it is not ours to write: %q", l)
		}
	}
}

func TestIPv6ForwardingIsWrittenWhenTurnedOn(t *testing.T) {
	c := testConfig()
	on := IPv6Forwarding(true)
	c.IPv6Forwarding = &on
	got := lines(t, c, nil)
	if !contains(got, "sysctl "+IPv6ForwardingSysctl+" = 1") {
		t.Errorf("turned on, so it has to be written: %s", dump(got))
	}
}

// Off is a value, not an absence: a switch the operator turned off has to take
// the kernel with it, or the page would say "off" over a box still forwarding.
func TestIPv6ForwardingTurnedOffWritesZero(t *testing.T) {
	c := testConfig()
	off := IPv6Forwarding(false)
	c.IPv6Forwarding = &off
	got := lines(t, c, nil)
	if !contains(got, "sysctl "+IPv6ForwardingSysctl+" = 0") {
		t.Errorf("turned off, so it has to be written as 0: %s", dump(got))
	}
}

func TestManagedSocksTunFailsClosedIfInterfaceDisappears(t *testing.T) {
	c := testConfig()
	c.Exits = append(c.Exits, Exit{Name: "SOCKS", Via: Via{Kind: ViaInterface, Interface: "olrsocks0"}})
	c.Default = "SOCKS"
	c.Normalize()
	links := testLinks()
	links["olrsocks0"] = LinkInfo{Name: "olrsocks0", Up: true}
	got := Render(c, links, nil).Lines()
	e, _ := c.Find("SOCKS")
	for _, want := range []string{
		sprintf("route ip table %d default dev olrsocks0", e.Table()),
		sprintf("route ip table %d unreachable default metric 100", e.Table()),
		sprintf("route ip6 table %d unreachable default", e.Table()),
	} {
		if !contains(got, want) {
			t.Errorf("missing %q in %s", want, dump(got))
		}
	}
	absent := Render(c, testLinks(), nil).Lines()
	if contains(absent, sprintf("route ip table %d default dev olrsocks0", e.Table())) ||
		!contains(absent, sprintf("route ip table %d unreachable default", e.Table())) {
		t.Errorf("missing backend must fail closed: %s", dump(absent))
	}
	links["olrsocks0"] = LinkInfo{Name: "olrsocks0", Up: false}
	down := Render(c, links, nil).Lines()
	if !contains(down, sprintf("route ip table %d unreachable default", e.Table())) {
		t.Errorf("down backend must fail closed: %s", dump(down))
	}
	e.OnFailure = FailDirect
	c.Upsert(e)
	if Validate(c, testLinks()).OK() {
		t.Fatal("managed SOCKS5 exit must not fail open")
	}
}

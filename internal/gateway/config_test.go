package gateway

import (
	"net/netip"
	"testing"

	"github.com/open-linux-router/open-linux-router/internal/core"
	"github.com/open-linux-router/open-linux-router/internal/gateway/nat"
)

func hop(s string) *netip.Addr {
	a := netip.MustParseAddr(s)
	return &a
}

// testConfig is the reference topology of docs/dns.md §1: a modem and a proxy
// box, both next hops, neither of them something a device is ever told about.
func testConfig() Config {
	return Config{
		Enabled: true,
		Exits: []Exit{
			{Name: "Proxy", Via: Via{Kind: ViaNextHop, NextHop: hop("192.168.1.50")}},
			{Name: "Blocked", Via: Via{Kind: ViaBlocked}},
		},
		Interfaces: []Assignment{
			{Interface: "br-lan", Exit: "Proxy"},
			{Interface: "br-iot", Exit: "Blocked"},
		},
	}
}

func testLinks() StaticLinks {
	return StaticLinks{
		"br-lan": {Adopted: true, Up: true, Prefixes: []netip.Prefix{
			netip.MustParsePrefix("192.168.1.1/24"),
		}},
		"br-iot": {Adopted: true, Up: true, Prefixes: []netip.Prefix{
			netip.MustParsePrefix("192.168.30.1/24"),
		}},
		"wg0": {Adopted: true, Up: true},
	}
}

func TestDeviceAssignmentRenamesWithExit(t *testing.T) {
	c := testConfig()
	c.SetDevice("AA-BB-CC-DD-EE-FF", "Proxy")
	if len(c.Clients) != 1 || c.Clients[0].MAC != "aa:bb:cc:dd:ee:ff" {
		t.Fatalf("device not canonicalized: %+v", c.Clients)
	}
	if !c.Rename("Proxy", "Clash") || c.Clients[0].Exit != "Clash" {
		t.Fatalf("rename lost client override: %+v", c.Clients)
	}
	if !c.RemoveClient("AA-BB-CC-DD-EE-FF") || len(c.Clients) != 0 {
		t.Fatalf("could not restore inheritance: %+v", c.Clients)
	}
}

func TestDeviceOverrideAloneUsesAnExit(t *testing.T) {
	c := Config{Enabled: true, Exits: []Exit{{Name: "Blocked", Via: Via{Kind: ViaBlocked}}}}
	c.SetDevice("aa:bb:cc:dd:ee:ff", "Blocked")
	if !c.InUse("Blocked") {
		t.Fatal("client override did not activate its exit")
	}
	got := lines(t, c, nil)
	if !containsPrefix(got, "nft device aa:bb:cc:dd:ee:ff ") ||
		!containsPrefix(got, "route ip table ") {
		t.Fatalf("device-only exit was not programmed: %s", dump(got))
	}
}

func TestDeviceOverrideIsVisibleAsAnExitUser(t *testing.T) {
	c := testConfig()
	c.SetDevice("aa:bb:cc:dd:ee:ff", "Proxy")
	if got := c.UsedBy("Proxy"); len(got) != 2 || got[0] != "aa:bb:cc:dd:ee:ff" {
		t.Fatalf("device should be listed alongside network: %v", got)
	}
}

func TestNormalizeSortsAndAllocatesSlots(t *testing.T) {
	c := Config{Exits: []Exit{
		{Name: "  Zeta  "},
		{Name: "Alpha"},
	}}
	c.Normalize()

	if c.Exits[0].Name != "Alpha" || c.Exits[1].Name != "Zeta" {
		t.Fatalf("exits not sorted or trimmed: %+v", c.Exits)
	}
	if c.Exits[0].Slot != 1 || c.Exits[1].Slot != 2 {
		t.Fatalf("slots not allocated: %d, %d", c.Exits[0].Slot, c.Exits[1].Slot)
	}
}

// The property the whole slot design exists for: adding an exit must not move
// an existing one, because moving it would move its route table out from under
// every flow already marked for it.
func TestAddingAnExitKeepsEveryOtherSlot(t *testing.T) {
	c := Config{Exits: []Exit{{Name: "Proxy"}, {Name: "Modem"}}}
	c.Normalize()
	before := map[string]int{}
	for _, e := range c.Exits {
		before[e.Name] = e.Slot
	}

	// Sorts to the front, which is exactly the case a position-derived slot
	// would get wrong.
	c.Upsert(Exit{Name: "Backup", Via: Via{Kind: ViaBlocked}})

	for _, e := range c.Exits {
		if was, existed := before[e.Name]; existed && e.Slot != was {
			t.Errorf("%s moved from slot %d to %d", e.Name, was, e.Slot)
		}
	}
	if _, ok := c.Find("Backup"); !ok {
		t.Fatal("Backup was not added")
	}
}

func TestUpsertKeepsTheSlotWhenReplacing(t *testing.T) {
	c := Config{Exits: []Exit{{Name: "Proxy"}, {Name: "Modem"}}}
	c.Normalize()
	proxy, _ := c.Find("Proxy")

	c.Upsert(Exit{Name: "Proxy", Via: Via{Kind: ViaInterface, Interface: "wg0"}})

	got, _ := c.Find("Proxy")
	if got.Slot != proxy.Slot {
		t.Fatalf("slot changed on replace: was %d, now %d", proxy.Slot, got.Slot)
	}
	if got.Via.Interface != "wg0" {
		t.Fatalf("via not replaced: %+v", got.Via)
	}
}

func TestAllocateSlotsClearsDuplicatesFromAHandEditedFile(t *testing.T) {
	// Two exits sharing a slot means two exits sharing a route table, and one
	// silently carrying the other's traffic.
	c := Config{Exits: []Exit{
		{Name: "A", Slot: 4},
		{Name: "B", Slot: 4},
	}}
	c.Normalize()

	if c.Exits[0].Slot == c.Exits[1].Slot {
		t.Fatalf("duplicate slot survived: %+v", c.Exits)
	}
	if c.Exits[0].Slot != 4 {
		t.Errorf("the first claimant should keep slot 4, got %d", c.Exits[0].Slot)
	}
}

func TestAllocateSlotsRejectsOutOfRange(t *testing.T) {
	c := Config{Exits: []Exit{{Name: "A", Slot: MaxSlot + 1}, {Name: "B", Slot: -3}}}
	c.Normalize()
	for _, e := range c.Exits {
		if !Slot(e.Slot).Valid() {
			t.Errorf("%s kept an out-of-range slot %d", e.Name, e.Slot)
		}
	}
}

func TestSlotResources(t *testing.T) {
	// The property that makes a slot legible on sight: one number, three uses.
	s := Slot(3)
	if got, want := s.Mark(), uint32(0x00030000); got != want {
		t.Errorf("mark = %#08x, want %#08x", got, want)
	}
	if got, want := s.Table(), 8103; got != want {
		t.Errorf("table = %d, want %d", got, want)
	}
	if s.Priority() != s.Table() {
		t.Errorf("priority %d and table %d should read alike", s.Priority(), s.Table())
	}
	if s.Mark()&^MarkMask != 0 {
		t.Errorf("mark %#08x escapes the documented mask %#08x", s.Mark(), MarkMask)
	}
}

func TestOwnsPriorityAndTable(t *testing.T) {
	if !OwnsPriority(LocalPriority) {
		t.Error("the local guard's own priority should be ours")
	}
	if OwnsTable(Base) {
		t.Error("Base is the local guard's priority, not a table of ours")
	}
	if OwnsPriority(Base + MaxSlot + 1) {
		t.Error("the range should stop at Base+MaxSlot")
	}
	for _, prio := range []int{0, 100, 9000, 32766} {
		if OwnsPriority(prio) {
			t.Errorf("priority %d is not in our documented range", prio)
		}
	}
}

func TestAssignedReportsItsSource(t *testing.T) {
	c := testConfig()
	c.Default = "Proxy"
	c.Normalize()

	exit, source := c.Assigned("br-iot")
	if exit != "Blocked" || source != SourceInterface {
		t.Errorf("br-iot = %q from %q, want Blocked from interface", exit, source)
	}

	// A network nobody has configured follows the box-wide setting, and says so.
	exit, source = c.Assigned("br-guest")
	if exit != "Proxy" || source != SourceDefault {
		t.Errorf("br-guest = %q from %q, want Proxy from default", exit, source)
	}
}

// §2.1's motivating case: everything through Proxy except one network, which is
// a default plus one override rather than a negation or a rule at position 1.
func TestEverythingThroughOneExitExceptOne(t *testing.T) {
	c := Config{
		Enabled: true,
		Default: "Proxy",
		Exits:   []Exit{{Name: "Proxy", Via: Via{Kind: ViaBlocked}}},
		Interfaces: []Assignment{
			{Interface: "br-lan"},
			{Interface: "br-nas", Exit: ""},
		},
	}
	c.Normalize()

	// An explicitly-empty assignment inherits, and reports the default as the
	// source so that changing the default makes the consequence visible.
	exit, source := c.Assigned("br-nas")
	if exit != "Proxy" || source != SourceDefault {
		t.Errorf("explicit inherit = %q from %q, want Proxy from default", exit, source)
	}
}

func TestRenameMovesEveryReference(t *testing.T) {
	c := testConfig()
	c.Default = "Proxy"
	c.Normalize()

	if !c.Rename("Proxy", "Relay") {
		t.Fatal("rename reported no such exit")
	}
	if c.Default != "Relay" {
		t.Errorf("default still names the old exit: %q", c.Default)
	}
	for _, a := range c.Interfaces {
		if a.Exit == "Proxy" {
			t.Errorf("%s still points at the old name", a.Interface)
		}
	}
	if _, ok := c.Find("Relay"); !ok {
		t.Error("the renamed exit is missing")
	}
}

func TestRenameKeepsTheSlot(t *testing.T) {
	c := testConfig()
	c.Normalize()
	before, _ := c.Find("Proxy")

	c.Rename("Proxy", "Relay")

	after, _ := c.Find("Relay")
	if after.Slot != before.Slot {
		t.Errorf("renaming moved the route table: slot %d became %d", before.Slot, after.Slot)
	}
}

func TestRemoveLeavesDanglingReferencesForValidateToReport(t *testing.T) {
	c := testConfig()
	c.Normalize()
	c.Remove("Proxy")

	// Deliberate: silently re-pointing br-lan at the modem because an exit was
	// deleted elsewhere is worse than refusing.
	res := Validate(c, testLinks())
	if res.OK() {
		t.Fatal("a dangling assignment should be an error")
	}
}

func TestSNATDefaultsOnForANextHopAndOffOtherwise(t *testing.T) {
	next := Exit{Via: Via{Kind: ViaNextHop, NextHop: hop("192.168.1.50")}}
	if !next.SNATOrDefault() {
		t.Error("a next hop should SNAT by default (§5.3)")
	}
	off := false
	next.SNAT = &off
	if next.SNATOrDefault() {
		t.Error("an explicit false should be honoured")
	}
	iface := Exit{Via: Via{Kind: ViaInterface, Interface: "wg0"}}
	if iface.SNATOrDefault() {
		t.Error("an interface exit has no next hop to SNAT toward")
	}
}

func TestIPv6DefaultsToBlock(t *testing.T) {
	// The conservative answer, because an exit whose v6 capability we cannot
	// see is one where carrying it might work and might leak silently.
	if got := (Exit{}).IPv6OrDefault(); got != IPv6Block {
		t.Errorf("default IPv6 handling = %q, want %q", got, IPv6Block)
	}
}

func TestRoundTripThroughJSON(t *testing.T) {
	c := testConfig()
	c.Default = "Proxy"
	c.Normalize()

	data, err := MarshalConfig(c)
	if err != nil {
		t.Fatal(err)
	}
	back, err := UnmarshalConfig(data)
	if err != nil {
		t.Fatalf("re-reading what we wrote: %v", err)
	}
	if len(back.Exits) != len(c.Exits) || back.Default != c.Default {
		t.Fatalf("round trip lost data: %+v", back)
	}
	got, _ := back.Find("Proxy")
	if got.Via.NextHop == nil || got.Via.NextHop.String() != "192.168.1.50" {
		t.Errorf("next hop did not survive: %+v", got.Via)
	}
	if got.Slot == 0 {
		t.Error("the slot did not survive, which would renumber every exit on reload")
	}
}

func TestUnmarshalRejectsUnknownFields(t *testing.T) {
	_, err := UnmarshalConfig([]byte(`{"enabled":true,"exitz":[]}`))
	if err == nil {
		t.Fatal("a mistyped key should be an error, not a silent no-op")
	}
}

func TestDurationRoundTripsAsAString(t *testing.T) {
	var d Duration
	if err := d.UnmarshalText([]byte("30s")); err != nil {
		t.Fatal(err)
	}
	text, err := d.MarshalText()
	if err != nil {
		t.Fatal(err)
	}
	if string(text) != "30s" {
		t.Errorf("duration serialised as %q, want 30s", text)
	}
}

// --- the routing → gateway rename -----------------------------------------

// docKeyed builds a configuration document with this module's subtree stored
// under an arbitrary key, so the legacy name can be exercised directly.
func docKeyed(t *testing.T, key, raw string) core.Document {
	t.Helper()
	var d core.Document
	d.Set(key, []byte(raw))
	return d
}

// A v0.1.0 box stored this module under "routing". The key moved with the
// rename, and the failure mode if it were simply dropped is the quiet one:
// FromDocument treats an absent section as "nobody configured an exit", so the
// upgrade would produce an empty gateway screen, no error, and no programmed
// policy — indistinguishable from a fresh install.
func TestFromDocumentReadsTheLegacyRoutingKey(t *testing.T) {
	doc := docKeyed(t, LegacyModuleName, `{"enabled":true,"exits":[{"name":"Office","via":{"interface":"wg0"}}]}`)

	cfg, err := FromDocument(doc)
	if err != nil {
		t.Fatalf("FromDocument on a document with the legacy key: %v", err)
	}
	if !cfg.Enabled {
		t.Error("enabled = false; the legacy section was not read")
	}
	if len(cfg.Exits) != 1 || cfg.Exits[0].Name != "Office" {
		t.Fatalf("exits = %+v, want the one stored under %q", cfg.Exits, LegacyModuleName)
	}
}

// Both keys present means a box that upgraded and then applied: the new key is
// authoritative and the stale one is inert, never merged.
func TestCurrentKeyWinsOverTheLegacyOne(t *testing.T) {
	var d core.Document
	d.Set(LegacyModuleName, []byte(`{"enabled":true,"exits":[{"name":"Stale","via":{"interface":"wg0"}}]}`))
	d.Set(ModuleName, []byte(`{"enabled":true,"exits":[{"name":"Current","via":{"interface":"wg1"}}]}`))

	cfg, err := FromDocument(d)
	if err != nil {
		t.Fatalf("FromDocument: %v", err)
	}
	if len(cfg.Exits) != 1 || cfg.Exits[0].Name != "Current" {
		t.Fatalf("exits = %+v, want only the one under %q", cfg.Exits, ModuleName)
	}
}

// Neither key is still the empty state, not an error.
func TestFromDocumentTreatsBothKeysAbsentAsUnconfigured(t *testing.T) {
	cfg, err := FromDocument(docKeyed(t, "dhcp", `{"enabled":false}`))
	if err != nil {
		t.Fatalf("FromDocument on a document with no gateway section: %v", err)
	}
	if cfg.Enabled || len(cfg.Exits) != 0 {
		t.Fatalf("config = %+v, want the zero value", cfg)
	}
}

// docs/remote-access.md §6.2: the tunnel is a network behind this box like any
// other, so it reaches the table twice — once as a source the egress masquerade
// covers, and once as the set of sources the inside rules rewrite.
func TestNATProjectsTheDialInNetwork(t *testing.T) {
	c := Config{Enabled: true}
	networks := []netip.Prefix{netip.MustParsePrefix("192.168.1.0/24")}
	dialIn := &nat.DialIn{
		Prefix: netip.MustParsePrefix("10.6.0.0/24"),
		Tunnel: "wg0",
	}

	got := c.NAT("pppoe0", networks, dialIn)
	if got.DialIn == nil {
		t.Fatal("the dial-in network was dropped")
	}
	if got.DialIn.Uplink != "pppoe0" {
		t.Errorf("dial-in uplink = %q, want the one the caller read from dial", got.DialIn.Uplink)
	}
	if got.Egress == nil {
		t.Fatal("no egress rule")
	}
	if want := netip.MustParsePrefix("10.6.0.0/24"); !hasPrefix(got.Egress.Sources, want) {
		t.Errorf("egress sources %v do not cover the tunnel", got.Egress.Sources)
	}
	if want := netip.MustParsePrefix("192.168.1.0/24"); !hasPrefix(got.Egress.Sources, want) {
		t.Errorf("egress sources %v dropped a served network", got.Egress.Sources)
	}
	if len(got.Egress.Sources) != 2 {
		t.Errorf("egress sources = %v, want the served network and the tunnel", got.Egress.Sources)
	}
}

func TestNATWithoutATunnelWritesNoDialInRule(t *testing.T) {
	c := Config{Enabled: true}
	got := c.NAT("pppoe0", []netip.Prefix{netip.MustParsePrefix("192.168.1.0/24")}, nil)
	if got.DialIn != nil {
		t.Errorf("a box with no tunnel got a dial-in network: %+v", got.DialIn)
	}
}

func hasPrefix(prefixes []netip.Prefix, want netip.Prefix) bool {
	for _, p := range prefixes {
		if p == want {
			return true
		}
	}
	return false
}

// An interval set cannot carry overlapping elements, and the egress rule must
// not pretend an overlapping tunnel is one of the networks it serves.
func TestNATLeavesAnOverlappingTunnelOutOfTheEgressSources(t *testing.T) {
	c := Config{Enabled: true}
	served := netip.MustParsePrefix("10.6.0.0/24")
	got := c.NAT("pppoe0", []netip.Prefix{served}, &nat.DialIn{Prefix: served})

	if got.Egress == nil {
		t.Fatal("no egress rule")
	}
	if len(got.Egress.Sources) != 1 || got.Egress.Sources[0] != served {
		t.Errorf("egress sources = %v, want just the served network", got.Egress.Sources)
	}
}

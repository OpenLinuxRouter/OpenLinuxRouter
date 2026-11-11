package gateway

import (
	"net/netip"
	"strings"
	"testing"
)

func errorAt(t *testing.T, res Result, path string) Problem {
	t.Helper()
	for _, p := range res.Errors {
		if p.Path == path {
			return p
		}
	}
	t.Fatalf("no error at %q; errors were %v", path, res.Errors)
	return Problem{}
}

func hasWarningContaining(res Result, substr string) bool {
	for _, p := range res.Warnings {
		if strings.Contains(p.Message, substr) {
			return true
		}
	}
	return false
}

func TestValidateAcceptsTheReferenceTopology(t *testing.T) {
	c := testConfig()
	c.Normalize()
	if res := Validate(c, testLinks()); !res.OK() {
		t.Fatalf("the reference topology should validate: %v", res.Errors)
	}
}

func TestExitDNSModesValidateTheirTransport(t *testing.T) {
	for _, tc := range []struct {
		dns  *ExitDNS
		path string
	}{
		{&ExitDNS{Mode: "exit"}, ""},
		{&ExitDNS{Mode: "custom", Server: netip.MustParseAddrPort("192.168.1.10:53")}, ""},
		{&ExitDNS{Mode: "custom"}, "exits[1].dns.server"},
		{&ExitDNS{Mode: "custom", Server: netip.MustParseAddrPort("127.0.0.1:53")}, "exits[1].dns.server"},
		{&ExitDNS{Mode: "custom", Server: netip.MustParseAddrPort("192.168.1.1:53")}, "exits[1].dns.server"},
		{&ExitDNS{Mode: "custom", Server: netip.MustParseAddrPort("[2001:db8::1]:53")}, "exits[1].dns.server"},
		{&ExitDNS{Mode: "invalid"}, "exits[1].dns.mode"},
	} {
		c := testConfig()
		for i := range c.Exits {
			if c.Exits[i].Name == "Proxy" {
				c.Exits[i].DNS = tc.dns
			}
		}
		c.Normalize()
		res := Validate(c, testLinks())
		if tc.path == "" && !res.OK() {
			t.Errorf("%+v: %v", tc.dns, res.Errors)
		}
		if tc.path != "" {
			errorAt(t, res, tc.path)
		}
	}
}

func TestDeviceOverridesRejectInvalidDuplicateAndMissingExit(t *testing.T) {
	c := testConfig()
	c.Clients = []ClientAssignment{
		{MAC: "bad", Exit: "Proxy"},
		{MAC: "aa:bb:cc:dd:ee:ff", Exit: "Proxy"},
		{MAC: "AA-BB-CC-DD-EE-FF", Exit: "Missing"},
	}
	r := Validate(c, testLinks())
	errorAt(t, r, "devices[0].mac")
	errorAt(t, r, "devices[2].mac")
	errorAt(t, r, "devices[2].exit")
}

func TestDeviceOverrideWithoutAnAdoptedNetworkWarns(t *testing.T) {
	c := Config{Clients: []ClientAssignment{{MAC: "aa:bb:cc:dd:ee:ff"}}}
	r := Validate(c, StaticLinks{})
	if !hasWarningContaining(r, "will not match traffic") {
		t.Fatalf("no warning for an override with no network: %+v", r)
	}
}

// §5.1's second row, and the mistake it exists to catch: entering the proxy's
// public address instead of its address on your own network.
func TestNextHopMustBeDirectlyReachable(t *testing.T) {
	c := Config{
		Enabled:    true,
		Exits:      []Exit{{Name: "Proxy", Via: Via{Kind: ViaNextHop, NextHop: hop("203.0.113.9")}}},
		Interfaces: []Assignment{{Interface: "br-lan", Exit: "Proxy"}},
	}
	c.Normalize()

	res := Validate(c, testLinks())
	p := errorAt(t, res, "exits[0].via.next_hop")
	if !strings.Contains(p.Message, "public") {
		t.Errorf("the message should name the likely cause, got %q", p.Message)
	}
}

func TestNextHopIsCheckedAgainstTheNamedDevice(t *testing.T) {
	c := Config{
		Enabled: true,
		Exits: []Exit{{Name: "Proxy", Via: Via{
			Kind: ViaNextHop, NextHop: hop("192.168.1.50"), Dev: "br-iot",
		}}},
		Interfaces: []Assignment{{Interface: "br-lan", Exit: "Proxy"}},
	}
	c.Normalize()

	// The address is reachable, but not on the interface that was named.
	res := Validate(c, testLinks())
	errorAt(t, res, "exits[0].via.next_hop")
}

func TestAnExitTakesOnlyItsOwnFormsFields(t *testing.T) {
	c := Config{
		Enabled: true,
		Exits: []Exit{{Name: "Mixed", Via: Via{
			Kind: ViaInterface, Interface: "wg0", NextHop: hop("192.168.1.50"),
		}}},
	}
	c.Normalize()

	res := Validate(c, testLinks())
	errorAt(t, res, "exits[0].via.next_hop")
}

func TestAnUnknownInterfaceIsAWarningNotAnError(t *testing.T) {
	// WireGuard, PPPoE and proxy TUN devices are created by something other
	// than us and routinely do not exist when the exit is configured. Refusing
	// would make "start the tunnel first, then tell olr" the required order.
	c := Config{
		Enabled: true,
		Exits:   []Exit{{Name: "Office", Via: Via{Kind: ViaInterface, Interface: "utun9"}}},
	}
	c.Normalize()

	res := Validate(c, testLinks())
	if !res.OK() {
		t.Fatalf("an absent tunnel should not block configuration: %v", res.Errors)
	}
	if !hasWarningContaining(res, "will not carry traffic") {
		t.Errorf("the operator should be told, got %v", res.Warnings)
	}
}

func TestAssignmentNeedsAnAdoptedInterfaceWithAddresses(t *testing.T) {
	links := StaticLinks{
		"br-wan":  {Adopted: false, Up: true, Prefixes: []netip.Prefix{netip.MustParsePrefix("10.0.0.2/24")}},
		"br-bare": {Adopted: true, Up: true},
	}
	c := Config{
		Enabled: true,
		Exits:   []Exit{{Name: "Blocked", Via: Via{Kind: ViaBlocked}}},
		Interfaces: []Assignment{
			{Interface: "br-wan", Exit: "Blocked"},
			{Interface: "br-bare", Exit: "Blocked"},
		},
	}
	c.Normalize()

	// Normalize sorts by interface name, so br-bare is index 0 and br-wan is 1.
	res := Validate(c, links)
	if p := errorAt(t, res, "interfaces[0].interface"); !strings.Contains(p.Message, "no addresses") {
		t.Errorf("an addressless interface has no source range, got %q", p.Message)
	}
	if p := errorAt(t, res, "interfaces[1].interface"); !strings.Contains(p.Message, "adopt") {
		t.Errorf("an unadopted interface should say so, got %q", p.Message)
	}
}

// §2.3: a source with two answers is genuinely ambiguous, and inventing a
// tie-break would produce behaviour nobody can predict from the screen.
func TestTwoAssignmentsForOneNetworkAreRefused(t *testing.T) {
	c := Config{
		Enabled: true,
		Exits:   []Exit{{Name: "Blocked", Via: Via{Kind: ViaBlocked}}},
		Interfaces: []Assignment{
			{Interface: "br-lan", Exit: "Blocked"},
			{Interface: "br-lan", Exit: ""},
		},
	}
	c.Normalize()

	res := Validate(c, testLinks())
	errorAt(t, res, "interfaces[1].interface")
}

func TestUnknownExitNamesTheOnesThatExist(t *testing.T) {
	c := testConfig()
	c.Default = "Typo"
	c.Normalize()

	res := Validate(c, testLinks())
	p := errorAt(t, res, "default")
	if !strings.Contains(p.Message, `"Proxy"`) {
		t.Errorf("the message should list the real exits, got %q", p.Message)
	}
}

func TestProbeTargetMustBeOnTheFarSide(t *testing.T) {
	// A probe that never leaves the box passes while the exit is dead, which
	// is worse than no probe: it reports health it did not measure.
	c := testConfig()
	c.Exits[0].Probe = &Probe{Target: netip.MustParseAddrPort("127.0.0.1:443")}
	c.Normalize()

	res := Validate(c, testLinks())
	i := indexOfExit(t, c, "Proxy")
	errorAt(t, res, exitPath(i)+".probe.target")
}

func TestProbeTimeoutMustBeShorterThanItsInterval(t *testing.T) {
	c := testConfig()
	c.Exits[0].Probe = &Probe{
		Target:   netip.MustParseAddrPort("1.1.1.1:443"),
		Interval: Duration(5e9),
		Timeout:  Duration(10e9),
	}
	c.Normalize()

	res := Validate(c, testLinks())
	i := indexOfExit(t, c, "Proxy")
	errorAt(t, res, exitPath(i)+".probe.timeout")
}

func TestAnUnprobedExitInUseIsWarnedAbout(t *testing.T) {
	// docs/dns.md §1.2 rests the whole topology rule on olr being able to
	// re-point a dead exit, so an exit carrying traffic with no health check is
	// a single point of failure with no detection.
	c := testConfig()
	c.Normalize()

	res := Validate(c, testLinks())
	if !hasWarningContaining(res, "never health-checked") {
		t.Errorf("expected a warning about the unprobed exit, got %v", res.Warnings)
	}
}

func TestIPv6DirectIsWarnedAbout(t *testing.T) {
	c := testConfig()
	c.Exits[0].IPv6 = IPv6Direct
	c.Normalize()

	res := Validate(c, testLinks())
	if !hasWarningContaining(res, "AAAA") {
		t.Errorf("§5.4's leak should be called out by name, got %v", res.Warnings)
	}
}

func TestFailDirectIsWarnedAbout(t *testing.T) {
	c := testConfig()
	c.Exits[0].OnFailure = FailDirect
	c.Normalize()

	res := Validate(c, testLinks())
	if !hasWarningContaining(res, "unrouted") {
		t.Errorf("failing open should never be silent, got %v", res.Warnings)
	}
}

func TestDuplicateExitNamesAreRefused(t *testing.T) {
	c := Config{Exits: []Exit{
		{Name: "Proxy", Via: Via{Kind: ViaBlocked}},
		{Name: "Proxy", Via: Via{Kind: ViaBlocked}},
	}}
	c.Normalize()

	res := Validate(c, testLinks())
	errorAt(t, res, "exits[1].name")
}

func TestAnExitNeedsAForm(t *testing.T) {
	c := Config{Exits: []Exit{{Name: "Nameless"}}}
	c.Normalize()

	res := Validate(c, testLinks())
	p := errorAt(t, res, "exits[0].via.kind")
	if !strings.Contains(p.Message, string(ViaNextHop)) {
		t.Errorf("the message should list the forms, got %q", p.Message)
	}
}

func exitPath(i int) string { return "exits[" + itoa(i) + "]" }

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}

func indexOfExit(t *testing.T, c Config, name string) int {
	t.Helper()
	for i, e := range c.Exits {
		if e.Name == name {
			return i
		}
	}
	t.Fatalf("no exit named %q", name)
	return -1
}

package dhcp

import (
	"strings"
	"testing"
)

// The two things stage two set out to make true: a range is derived from the
// network rather than typed, and IPv4 and IPv6 are independent.

// design.md §11.2 makes the derived range a requirement, and the reason is a
// safety property rather than convenience: the collision DHCP cannot defend
// against is a statically configured client inside the dynamic range, and
// dnsmasq has no exclusion primitive.
func TestAPoolWithNoRangeDerivesOneFromItsNetwork(t *testing.T) {
	c := Config{Enabled: true, Pools: []Pool{{Network: "lan", IPv4: &PoolIPv4{}}}}

	if res := Validate(c, testNetworks()); !res.OK() {
		t.Fatalf("refused a pool with no explicit range: %v", res.Errors)
	}

	start, end, ok := c.Pools[0].Range(mustNetwork(t, "lan"))
	if !ok {
		t.Fatal("no range was derived")
	}
	if start.String() != "192.168.1.100" || end.String() != "192.168.1.254" {
		t.Errorf("derived %s-%s, want 192.168.1.100-192.168.1.254", start, end)
	}
}

// The derived range must leave the low block free, or the whole reason for
// deriving it is gone.
func TestTheDerivedRangeLeavesTheStaticBlockFree(t *testing.T) {
	start, _, ok := Pool{Network: "lan", IPv4: &PoolIPv4{}}.Range(mustNetwork(t, "lan"))
	if !ok {
		t.Fatal("no range was derived")
	}
	if start.String() == "192.168.1.2" {
		t.Error("the derived range starts immediately after the router, leaving nothing for statics")
	}
}

// An explicit range wins. Somebody who typed one is not asking for our opinion,
// and silently replacing it would renumber a working network.
func TestAnExplicitRangeIsNotOverridden(t *testing.T) {
	p := Pool{Network: "lan", IPv4: &PoolIPv4{
		Start: addr(t, "192.168.1.10"),
		End:   addr(t, "192.168.1.20"),
	}}
	start, end, _ := p.Range(mustNetwork(t, "lan"))
	if start.String() != "192.168.1.10" || end.String() != "192.168.1.20" {
		t.Errorf("range = %s-%s, want the typed 192.168.1.10-192.168.1.20", start, end)
	}
}

// The configuration that could not be written down before: a network that
// serves no IPv4 at all and only advertises a prefix. The old Pool required
// Start and End, so this shape did not exist.
func TestANetworkCanServeIPv6Only(t *testing.T) {
	c := Config{Enabled: true, Pools: []Pool{{
		Network: "v6only",
		IPv6:    &PoolIPv6{Mode: RASLAAC},
	}}}

	if res := Validate(c, testNetworks()); !res.OK() {
		t.Fatalf("refused an IPv6-only pool: %v", res.Errors)
	}

	rendered, err := NewDnsmasq(DefaultPaths()).Render(c, testNetworks())
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	main, ok := rendered.Get(DefaultPaths().Conf)
	if !ok {
		t.Fatal("no main config was rendered")
	}
	text := string(main.Data)
	switch {
	case !strings.Contains(text, "constructor:br-v6,ra-stateless"):
		t.Errorf("no IPv6 range was rendered:\n%s", text)
	case strings.Contains(text, "255.255.255.0"):
		t.Errorf("an IPv4 range was rendered for a pool that has none:\n%s", text)
	case !strings.Contains(text, "enable-ra"):
		t.Error("enable-ra is missing, so nothing would actually be advertised")
	}
}

// The mirror image: IPv4 with no IPv6 must render no `dhcp-range=::` line and
// no enable-ra, or every box would advertise itself as an IPv6 router.
func TestANetworkCanServeIPv4Only(t *testing.T) {
	rendered, err := NewDnsmasq(DefaultPaths()).Render(validConfig(t), testNetworks())
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	main, _ := rendered.Get(DefaultPaths().Conf)
	text := string(main.Data)
	if strings.Contains(text, "enable-ra") || strings.Contains(text, "constructor:") {
		t.Errorf("IPv6 was advertised for a pool that did not ask for it:\n%s", text)
	}
}

// A pool that does neither is the one shape that is genuinely broken, and it is
// newly expressible — so it needs a rule of its own rather than falling through
// as valid.
func TestAPoolMustServeSomething(t *testing.T) {
	c := Config{Enabled: true, Pools: []Pool{{Network: "lan"}}}

	res := Validate(c, testNetworks())
	if res.OK() {
		t.Fatal("accepted a pool that serves neither IPv4 nor IPv6")
	}
	if !hasProblem(res.Errors, "pools[0]", "serves neither") {
		t.Errorf("errors = %s", problemStrings(res.Errors))
	}
}

// design.md §4.3: Android has never implemented DHCPv6 and the request was
// closed "Won't Fix (Intended Behavior)". A network relying on it silently
// loses every Android client, which for this audience is most of the handsets —
// so it warns rather than passing quietly.
func TestStatefulDHCPv6WarnsAboutAndroid(t *testing.T) {
	c := validConfig(t)
	c.Pools[0].IPv6 = &PoolIPv6{Mode: RAStateful}

	res := Validate(c, testNetworks())
	if !res.OK() {
		t.Fatalf("refused stateful DHCPv6, which is a supported advanced mode: %v", res.Errors)
	}
	if !hasProblem(res.Warnings, "pools[0].ipv6.mode", "Android") {
		t.Errorf("warnings = %s", problemStrings(res.Warnings))
	}
}

// Validation reaches the kernel for exactly one thing now — whether the
// members are up — and that is a warning. Everything else is answerable from
// two stored documents, which is what the whole change was for.
func TestValidationNeedsNoObservedState(t *testing.T) {
	// A fixture with no `Up` anywhere: every network is down as far as this
	// view is concerned, and nothing that decides validity may depend on it.
	down := StaticNetworks{}
	for name, info := range testNetworks() {
		info.Up = false
		down[name] = info
	}

	if res := Validate(validConfig(t), down); !res.OK() {
		t.Errorf("a config's validity depends on observed state: %v", res.Errors)
	}
}

// Renaming a network is a delete and a create, not an edit, and the pool that
// pointed at the old name has to say so against its own field — the asymmetry
// `link` relies on when it refuses to check other modules' references.
func TestAPoolOnAMissingNetworkNamesTheNetwork(t *testing.T) {
	c := Config{Enabled: true, Pools: []Pool{{Network: "renamed", IPv4: &PoolIPv4{}}}}

	res := Validate(c, testNetworks())
	if res.OK() {
		t.Fatal("accepted a pool pointing at a network that does not exist")
	}
	if !hasProblem(res.Errors, "pools[0].network", "olr net show") {
		t.Errorf("the error does not say where to look: %s", problemStrings(res.Errors))
	}
}

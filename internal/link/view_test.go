package link

import (
	"net/netip"
	"strings"
	"testing"
)

// The subnet arithmetic these used to cover now lives in internal/core, where
// internal/dhcp can reach it too; see internal/core/subnet_test.go. What is left
// here is what this module actually decides.

func TestViewInterfaceCarriesTheSubnetHalves(t *testing.T) {
	observed := testInterfaces(t)
	cfg := Config{Adopted: []string{"lan0"}}
	info := find(t, Join(cfg, observed), "lan0")

	v := viewInterface(info, observedByName(observed), cfg)
	switch {
	case v.Address != "192.168.1.2":
		t.Errorf("Address = %q, want 192.168.1.2", v.Address)
	case v.Subnet != "192.168.1.0/24":
		t.Errorf("Subnet = %q, want 192.168.1.0/24", v.Subnet)
	case v.MAC != "aa:bb:cc:dd:ee:ff":
		t.Errorf("MAC = %q", v.MAC)
	case !v.Running:
		t.Error("Running is false for an interface with a carrier")
	}
}

// The interface row names the network it carries, which is what lets it answer
// "what is this NIC for" with something other than the address it happens to
// hold at the moment.
func TestViewInterfaceNamesItsNetwork(t *testing.T) {
	observed := testInterfaces(t)
	cfg := Config{
		Adopted:  []string{"lan0"},
		Networks: []Network{{Name: "lan", Members: []string{"lan0"}}},
	}

	withNetwork := viewInterface(find(t, Join(cfg, observed), "lan0"), observedByName(observed), cfg)
	if withNetwork.Network != "lan" {
		t.Errorf("Network = %q, want lan", withNetwork.Network)
	}
	without := viewInterface(find(t, Join(cfg, observed), "lan1"), observedByName(observed), cfg)
	if without.Network != "" {
		t.Errorf("Network = %q for an interface in no network, want empty", without.Network)
	}
}

// The derived range is published with the network rather than left to each
// client, so that `dhcp`, the form and the CLI cannot end up with three
// opinions about where the static block ends.
func TestViewNetworkPublishesTheDerivedRange(t *testing.T) {
	observed := testInterfaces(t)
	n := Network{
		Name:    "lan",
		Members: []string{"lan0"},
		IPv4:    &NetworkIPv4{Subnet: netip.MustParsePrefix("172.16.1.0/24")},
	}

	v := viewNetwork(n, observedByName(observed), netip.Prefix{})
	switch {
	case v.Subnet != "172.16.1.0/24":
		t.Errorf("Subnet = %q", v.Subnet)
	case v.Router != "172.16.1.1":
		t.Errorf("Router = %q, want the derived 172.16.1.1", v.Router)
	case v.RouterExplicit:
		t.Error("RouterExplicit is true for a derived address")
	case v.SuggestedStart != "172.16.1.100" || v.SuggestedEnd != "172.16.1.254":
		t.Errorf("derived range %s-%s, want 172.16.1.100-172.16.1.254", v.SuggestedStart, v.SuggestedEnd)
	}
}

// A network whose member is not on this machine is a row that still has to be
// shown — it is the typo case and the not-plugged-in-yet case — but it must not
// look like one that is working.
func TestViewNetworkReportsAnAbsentMember(t *testing.T) {
	observed := testInterfaces(t)
	n := Network{Name: "lan", Members: []string{"nosuch0"}}

	if v := viewNetwork(n, observedByName(observed), netip.Prefix{}); v.Present {
		t.Error("Present is true for a network whose member does not exist")
	}
}

func TestBuildPlanNamesAdoptAndRelease(t *testing.T) {
	stored := Config{Adopted: []string{"lan0"}}
	desired := Config{Adopted: []string{"lan1"}}

	plan := buildPlan(stored, desired, testInterfaces(t), Options{}, netip.Prefix{})
	if plan.Empty {
		t.Fatal("plan is empty for a change of adopted interface")
	}
	if plan.Impact != impactNone {
		t.Errorf("Impact = %q, want %q; adoption touches nothing on the box", plan.Impact, impactNone)
	}

	kinds := map[string]string{}
	for _, c := range plan.Changes {
		kinds[nameOf(c.Path)] = c.Kind
	}
	if kinds["lan1"] != kindCreate {
		t.Errorf("lan1 = %q, want %q", kinds["lan1"], kindCreate)
	}
	if kinds["lan0"] != kindDelete {
		t.Errorf("lan0 = %q, want %q", kinds["lan0"], kindDelete)
	}
}

func TestBuildPlanIsEmptyForNoChange(t *testing.T) {
	cfg := Config{Adopted: []string{"lan0"}}
	if plan := buildPlan(cfg, cfg, testInterfaces(t), Options{}, netip.Prefix{}); !plan.Empty {
		t.Errorf("plan = %+v, want empty", plan)
	}
}

// Creating a network on an interface that already holds a different address has
// to say, in the plan, that the old address is going away. This is the change
// that can take the operator's own session with it, and §5.5's guard is not
// built — the plan is the only warning there is.
func TestBuildPlanShowsTheAddressBeingRemoved(t *testing.T) {
	stored := Config{Adopted: []string{"lan0"}}
	desired := Config{
		Adopted: []string{"lan0"},
		Networks: []Network{{
			Name:    "lan",
			Members: []string{"lan0"},
			IPv4:    &NetworkIPv4{Subnet: netip.MustParsePrefix("172.16.1.0/24")},
		}},
	}

	plan := buildPlan(stored, desired, testInterfaces(t), Options{}, netip.Prefix{})
	if plan.Impact != impactDisruptive {
		t.Errorf("Impact = %q, want %q; lan0's 192.168.1.2/24 is being removed", plan.Impact, impactDisruptive)
	}

	var kernel string
	for _, c := range plan.Changes {
		if strings.HasPrefix(c.Path, "interfaces[") {
			kernel = c.Diff
		}
	}
	switch {
	case kernel == "":
		t.Fatalf("no kernel change in %+v", plan.Changes)
	case !strings.Contains(kernel, "- ip addr del 192.168.1.2/24 dev lan0"):
		t.Errorf("diff does not name the address being removed:\n%s", kernel)
	case !strings.Contains(kernel, "+ ip addr add 172.16.1.1/24 dev lan0"):
		t.Errorf("diff does not name the address being added:\n%s", kernel)
	}
}

// Adding a network to an interface that has no address takes nothing away, so
// it must not be classified the same as a renumber — an operator who is warned
// about everything stops reading the warnings.
func TestBuildPlanDoesNotCallAFirstAddressDisruptive(t *testing.T) {
	cfg := Config{
		Adopted: []string{"lan1"},
		Networks: []Network{{
			Name:    "lan",
			Members: []string{"lan1"},
			IPv4:    &NetworkIPv4{Subnet: netip.MustParsePrefix("172.16.1.0/24")},
		}},
	}

	plan := buildPlan(Config{Adopted: []string{"lan1"}}, cfg, testInterfaces(t), Options{}, netip.Prefix{})
	if plan.Impact != impactRestart {
		t.Errorf("Impact = %q, want %q; lan1 has no address to lose", plan.Impact, impactRestart)
	}
}

// Re-applying an unchanged config is not a no-op once the kernel is involved.
// Somebody who ran `ip addr del` by hand has to show up here as work to do,
// which is what §5.4 means by drift being free.
func TestBuildPlanSeesDriftAgainstAnUnchangedConfig(t *testing.T) {
	cfg := Config{
		Adopted: []string{"lan1"},
		Networks: []Network{{
			Name:    "lan",
			Members: []string{"lan1"},
			IPv4:    &NetworkIPv4{Subnet: netip.MustParsePrefix("172.16.1.0/24")},
		}},
	}

	// lan1 carries no address in the fixture, so intent and reality disagree.
	plan := buildPlan(cfg, cfg, testInterfaces(t), Options{}, netip.Prefix{})
	if plan.Empty {
		t.Error("plan is empty although the kernel does not have the network's address")
	}
}

func TestPhysicalInterfaceView(t *testing.T) {
	cfg := Config{}
	observed := []Interface{
		{Name: "enp1s0", Physical: true},
		{Name: "wg0"},
		{Name: "lo", Loopback: true},
	}
	for _, tc := range []struct {
		name     string
		physical bool
	}{
		{"enp1s0", true}, {"wg0", false}, {"lo", false},
	} {
		v := viewInterface(find(t, Join(cfg, observed), tc.name), observedByName(observed), cfg)
		if v.Physical != tc.physical {
			t.Errorf("%s physical = %v, want %v", tc.name, v.Physical, tc.physical)
		}
	}
}

// The MTU is what `firewall` derives its MSS clamp from, and what an operator
// comparing a LAN against a PPPoE uplink needs to see in one place.
func TestViewInterfaceCarriesTheMTU(t *testing.T) {
	observed := []Interface{{Name: "lan0", Up: true, Running: true, MTU: 1500}}
	cfg := Config{Adopted: []string{"lan0"}}
	v := viewInterface(find(t, Join(cfg, observed), "lan0"), observedByName(observed), cfg)
	if v.MTU != 1500 {
		t.Errorf("MTU = %d, want 1500", v.MTU)
	}

	// An adopted interface the kernel does not have has no MTU to report; zero
	// is the honest answer and `omitempty` keeps it out of the API.
	withAbsent := Config{Adopted: []string{"lan0", "nosuch0"}}
	absent := viewInterface(find(t, Join(withAbsent, observed), "nosuch0"), observedByName(observed), withAbsent)
	if absent.MTU != 0 {
		t.Errorf("MTU = %d for an absent interface, want 0", absent.MTU)
	}
}

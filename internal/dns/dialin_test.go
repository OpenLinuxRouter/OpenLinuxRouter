package dns

import (
	"encoding/json"
	"net/netip"
	"slices"
	"testing"

	"github.com/open-linux-router/open-linux-router/internal/dnsrelay"
)

// staticDialIn is a DialInView backed by a literal value.
func staticDialIn(info DialInInfo) DialInView { return staticDialInValue{info} }

type staticDialInValue struct{ info DialInInfo }

func (s staticDialInValue) DialIn() (DialInInfo, error) { return s.info, nil }

func tunnel() DialInView {
	return staticDialIn(DialInInfo{Enabled: true, Addr: netip.MustParseAddr("10.6.0.1")})
}

func TestServedListenAddsTheDialInAddressToPins(t *testing.T) {
	cfg := validConfig() // pins 192.168.1.1:53
	got, err := ServedListen(cfg, tunnel())
	if err != nil {
		t.Fatal(err)
	}
	want := []netip.AddrPort{
		netip.MustParseAddrPort("192.168.1.1:53"),
		netip.MustParseAddrPort("10.6.0.1:53"),
	}
	slices.SortStableFunc(want, compareAddrPort)
	if !slices.Equal(got, want) {
		t.Errorf("ServedListen() = %v, want %v", got, want)
	}
}

// Empty still means the wildcard. The wildcard already covers every address
// this box holds, including one that is not up yet, so pinning a second copy of
// the dial-in address would only be a copy of a kernel fact.
func TestServedListenStaysTheWildcardWithNothingPinned(t *testing.T) {
	cfg := validConfig()
	cfg.Listen = nil
	got, err := ServedListen(cfg, tunnel())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("ServedListen() = %v, want the wildcard (empty)", got)
	}
}

// The service address follows the pinned port: the relay answers on one port,
// and a second port would be a socket the hijack rule cannot name.
func TestServiceListenUsesThePinnedPort(t *testing.T) {
	cfg := validConfig()
	cfg.Listen = []netip.AddrPort{netip.MustParseAddrPort("192.168.1.1:5300")}
	got, err := ServiceListen(cfg, tunnel())
	if err != nil {
		t.Fatal(err)
	}
	want := []netip.AddrPort{netip.MustParseAddrPort("10.6.0.1:5300")}
	if !slices.Equal(got, want) {
		t.Errorf("ServiceListen() = %v, want %v", got, want)
	}
}

// No tunnel, no address. A box with remote access off is not told to answer
// anywhere on a subnet that does not exist.
func TestServiceListenIsEmptyWithoutATunnel(t *testing.T) {
	cfg := validConfig()
	for name, view := range map[string]DialInView{
		"nil":     nil,
		"off":     staticDialIn(DialInInfo{Enabled: false, Addr: netip.MustParseAddr("10.6.0.1")}),
		"no addr": staticDialIn(DialInInfo{Enabled: true}),
	} {
		got, err := ServiceListen(cfg, view)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(got) != 0 {
			t.Errorf("%s: ServiceListen() = %v, want nothing", name, got)
		}
	}
}

// The incident this whole path exists for.
//
// A box pins the resolver to its LAN address. A client dials in and is told
// `DNS = 10.6.0.1`. Before this, nothing answered on 10.6.0.1 and the client
// reported "connected" while every name lookup failed — the tunnel was healthy
// and the phone was offline. The promise and the listener are now derived from
// the same fact, so the rendered relay config cannot omit it.
func TestPeersResolveThroughAnAddressWeServe(t *testing.T) {
	b := testBackend(t)
	b.DialIn = tunnel()

	out, err := b.Render(validConfig(), testLinks())
	if err != nil {
		t.Fatal(err)
	}
	file, ok := out.Get(b.Paths.RelayConf)
	if !ok {
		t.Fatalf("nothing rendered at %s", b.Paths.RelayConf)
	}
	var rendered dnsrelay.Config
	if err := json.Unmarshal(file.Data, &rendered); err != nil {
		t.Fatalf("relay.json: %v", err)
	}

	dialIn := netip.MustParseAddrPort("10.6.0.1:53")
	if !slices.Contains(rendered.AlsoListen, dialIn) {
		t.Errorf("relay.json serves %v, missing the address every client configuration names (%v)",
			rendered.AlsoListen, dialIn)
	}
	// And it is the tolerant half, not a pin: the tunnel may come up after the
	// relay does, and that must not cost the box its resolver.
	if slices.Contains(rendered.Listen, dialIn) {
		t.Errorf("%v was pinned, so an absent tunnel would refuse to start the relay", dialIn)
	}
}

// A pin that names the tunnel address is redundant, not a second socket.
func TestRenderRelayDoesNotRepeatAPinnedDialInAddress(t *testing.T) {
	b := testBackend(t)
	b.DialIn = tunnel()
	cfg := validConfig()
	cfg.Listen = append(cfg.Listen, netip.MustParseAddrPort("10.6.0.1:53"))

	out, err := b.Render(cfg, testLinks())
	if err != nil {
		t.Fatal(err)
	}
	file, _ := out.Get(b.Paths.RelayConf)
	var rendered dnsrelay.Config
	if err := json.Unmarshal(file.Data, &rendered); err != nil {
		t.Fatal(err)
	}
	if len(rendered.AlsoListen) != 0 {
		t.Errorf("AlsoListen = %v, want nothing: the address is already pinned", rendered.AlsoListen)
	}
}

func TestDerivedNotesNameTheDialInAddress(t *testing.T) {
	notes := DerivedNotes(validConfig(), testLinks(), tunnel())
	if !slices.Contains(notes, "answering DNS on 10.6.0.1:53 for clients that dial in") {
		t.Errorf("DerivedNotes() = %v, want the dial-in line", notes)
	}

	// Switched off is the one state where the module promises nothing, so it
	// must not claim to answer anywhere.
	off := validConfig()
	off.Enabled = false
	if notes := DerivedNotes(off, testLinks(), tunnel()); len(notes) != 0 {
		t.Errorf("DerivedNotes(disabled) = %v, want nothing", notes)
	}
}

// The status surface has to name the source of each address, not just the set:
// "10.6.0.1 is answered on because clients dial in" is the sentence that tells
// an operator why an address they never configured is there.
func TestServeViewNamesWhereEachAddressCameFrom(t *testing.T) {
	got := serveView(validConfig(), tunnel())
	want := []servedView{
		{Addr: netip.MustParseAddrPort("192.168.1.1:53"), Source: servedPinned},
		{Addr: netip.MustParseAddrPort("10.6.0.1:53"), Source: servedDialIn},
	}
	if !slices.Equal(got, want) {
		t.Errorf("serveView() = %v, want %v", got, want)
	}

	cfg := validConfig()
	cfg.Listen = nil
	if got := serveView(cfg, tunnel()); !slices.Equal(got, []servedView{{Source: servedWildcard}}) {
		t.Errorf("serveView() under the wildcard = %v, want one wildcard entry", got)
	}

	// Switched off answers nowhere, whatever the pins say.
	off := validConfig()
	off.Enabled = false
	if got := serveView(off, tunnel()); len(got) != 0 {
		t.Errorf("serveView(disabled) = %v, want nothing", got)
	}
}

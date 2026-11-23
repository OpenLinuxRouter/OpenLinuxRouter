package firewall

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

var (
	wg  = Opening{For: "remote access (WireGuard)", Protocol: UDP, Port: 51820}
	web = Opening{For: "ingress", Protocol: TCP, Port: 443}
)

func TestOffRendersNothing(t *testing.T) {
	d := Render(Config{}, []string{"br-lan"}, []Opening{wg})
	if d.Enabled || len(d.Lines()) != 0 {
		t.Fatalf("a disabled firewall rendered %v", d.Lines())
	}
}

func TestRuleset(t *testing.T) {
	d := Render(Config{Enabled: true}, []string{"wg0", "br-lan", "br-lan", " "}, []Opening{web, wg})
	want := []string{
		"nft table inet olr_filter",
		"nft counter blocked_forward",
		"nft counter blocked_input",
		"nft chain input filter",
		"nft input established,related accept",
		"nft input from lo accept",
		"nft input from br-lan,wg0 accept",
		"nft input icmp accept",
		"nft input icmpv6 accept",
		"nft input dhcp client accept",
		"nft input dhcpv6 client accept",
		"nft allow tcp port 443 for ingress",
		"nft allow udp port 51820 for remote access (WireGuard)",
		"nft input drop counter blocked_input",
		"nft chain forward filter",
		"nft forward established,related accept",
		"nft forward from br-lan,wg0 accept",
		"nft forward to not br-lan,wg0 accept",
		"nft forward port forwards accept",
		"nft forward icmp accept",
		"nft forward icmpv6 accept",
		"nft forward drop counter blocked_forward",
	}
	// Collapse source partitions to check the original filtering order and
	// permissions remain intact. Source coverage is tested separately.
	var got []string
	for _, line := range d.Lines() {
		base, _, _ := strings.Cut(line, " source ")
		if len(got) == 0 || got[len(got)-1] != base {
			got = append(got, base)
		}
	}
	if !slices.Equal(got, want) {
		t.Fatalf("ruleset:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}

	// Both chains end in their drop; nothing may follow it.
	for _, chain := range [][]Rule{d.Input, d.Forward} {
		if chain[len(chain)-1].Kind != RuleDrop {
			t.Errorf("chain does not end in a drop: %v", chain[len(chain)-1].Line)
		}
	}
	// Every line fits in the kernel's comment limit.
	for _, l := range d.Lines() {
		if len(l) > 200 {
			t.Errorf("line too long for userdata: %q", l)
		}
	}
}

func TestOpeningsAreDeduplicatedByPort(t *testing.T) {
	d := Render(Config{Enabled: true}, []string{"br-lan"}, []Opening{
		{For: "b", Protocol: TCP, Port: 80},
		{For: "a", Protocol: TCP, Port: 80},
		{For: "a", Protocol: UDP, Port: 80},
	})
	if len(d.Openings) != 2 || d.Openings[0].For != "a" {
		t.Fatalf("openings = %+v", d.Openings)
	}
}

func TestStatusListsAreNeverNull(t *testing.T) {
	// The UI maps over both; JSON null would crash the page.
	if normalizeInside(nil) == nil || normalizeOpenings(nil) == nil {
		t.Fatal("an empty list normalised to nil")
	}
}

func TestStatusIncludesActiveIPTVPath(t *testing.T) {
	a := Applier{
		Store:    core.NewStore(filepath.Join(t.TempDir(), "olr.json"), ModuleName),
		Kernel:   &StaticKernel{},
		Boundary: StaticBoundary{Interfaces: []string{"lan0"}, Multicast: IPTV{Upstream: "iptv0", Downstream: []string{"lan0"}}},
	}
	for _, enabled := range []bool{true, false} {
		if err := a.save(Config{Enabled: enabled}); err != nil {
			t.Fatal(err)
		}
		st, err := a.GetStatus(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if (st.IPTV.Upstream != "") != enabled {
			t.Fatalf("enabled=%v: IPTV=%+v", enabled, st.IPTV)
		}
		raw, err := json.Marshal(st)
		if err != nil {
			t.Fatal(err)
		}
		if enabled && !strings.Contains(string(raw), `"iptv":{"upstream":"iptv0","downstream":["lan0"]}`) {
			t.Fatalf("IPTV API fields missing: %s", raw)
		}
	}
}

func TestEnablingWithNoNetworksIsRefused(t *testing.T) {
	_, _, err := BuildPlan(Config{Enabled: true}, StaticBoundary{}, Observed{Known: true}, "")
	if !errors.Is(err, ErrNoInside) {
		t.Fatalf("err = %v, want ErrNoInside", err)
	}
	// Off is always fine, networks or not.
	if _, _, err := BuildPlan(Config{}, StaticBoundary{}, Observed{Known: true}, ""); err != nil {
		t.Fatal(err)
	}
}

func TestPlanAgainstKernel(t *testing.T) {
	b := StaticBoundary{Interfaces: []string{"br-lan"}}
	k := &StaticKernel{}

	plan, d, err := BuildPlan(Config{Enabled: true}, b, mustObserve(t, k), "")
	if err != nil {
		t.Fatal(err)
	}
	if plan.Empty || plan.Impact != ImpactReload {
		t.Fatalf("enabling on an empty kernel: %+v", plan)
	}
	if err := k.Apply(context.Background(), d); err != nil {
		t.Fatal(err)
	}

	plan, _, _ = BuildPlan(Config{Enabled: true}, b, mustObserve(t, k), "")
	if !plan.Empty {
		t.Fatalf("re-planning applied state is not a no-op: %+v", plan.Changes)
	}

	// A new opening adds one counted rule for each source partition.
	b.Open = []Opening{wg}
	plan, _, _ = BuildPlan(Config{Enabled: true}, b, mustObserve(t, k), "")
	if len(plan.Changes) != 3 {
		t.Fatalf("adding an opening: %+v", plan.Changes)
	}
	for _, change := range plan.Changes {
		if change.Kind != "add" || !strings.HasPrefix(change.Line, wg.Line()+" source ") {
			t.Fatalf("unexpected opening change: %+v", change)
		}
	}

	// Disabling removes everything.
	plan, _, _ = BuildPlan(Config{}, b, mustObserve(t, k), "")
	for _, c := range plan.Changes {
		if c.Kind != "remove" {
			t.Fatalf("disabling adds %q", c.Line)
		}
	}
}

func TestLockoutWarning(t *testing.T) {
	b := StaticBoundary{Interfaces: []string{"br-lan"}}
	k := &StaticKernel{}

	for _, tc := range []struct {
		via  string
		warn bool
	}{
		{"", false},       // the CLI over the socket
		{"lo", false},     // a tunnel from the box itself
		{"br-lan", false}, // from inside
		{"eth0", true},    // from the upstream network
	} {
		plan, _, err := BuildPlan(Config{Enabled: true}, b, mustObserve(t, k), tc.via)
		if err != nil {
			t.Fatal(err)
		}
		warned := len(plan.Warnings) > 0
		if warned != tc.warn || (tc.warn && plan.Impact != ImpactDisruptive) {
			t.Errorf("via %q: warnings %v impact %s", tc.via, plan.Warnings, plan.Impact)
		}
	}

	// Switching it off from outside is how you get back in; never warn.
	plan, _, _ := BuildPlan(Config{}, b, mustObserve(t, k), "eth0")
	if len(plan.Warnings) > 0 {
		t.Errorf("disabling from outside warned: %v", plan.Warnings)
	}
}

func TestHTTPHoldsALockoutUntilConfirmed(t *testing.T) {
	store := core.NewStore(filepath.Join(t.TempDir(), "olr.json"), ModuleName)
	k := &StaticKernel{}
	h := HTTP{
		Applier: Applier{
			Store:  store,
			Kernel: k,
			Boundary: StaticBoundary{
				Interfaces: []string{"br-lan"},
				Addresses:  map[netip.Addr]string{netip.MustParseAddr("203.0.113.7"): "eth0"},
			},
		},
		Lock:   core.NewLock(),
		Events: core.NewEvents(),
	}
	handler := h.Handler()

	send := func(query string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("PATCH", "/config"+query, strings.NewReader(`{"enabled":true}`))
		local := &net.TCPAddr{IP: net.ParseIP("203.0.113.7"), Port: 8080}
		req = req.WithContext(context.WithValue(req.Context(), http.LocalAddrContextKey, net.Addr(local)))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}

	if rec := send(""); rec.Code != http.StatusConflict {
		t.Fatalf("unconfirmed lockout: status %d, body %s", rec.Code, rec.Body)
	}
	if cfg, _ := h.Applier.Load(); cfg.Enabled || len(k.State) != 0 {
		t.Fatal("an unconfirmed lockout was stored or applied")
	}

	if rec := send("?confirm=true"); rec.Code != http.StatusOK {
		t.Fatalf("confirmed: status %d, body %s", rec.Code, rec.Body)
	}
	if cfg, _ := h.Applier.Load(); !cfg.Enabled || len(k.State) == 0 {
		t.Fatal("a confirmed change was not stored and applied")
	}
}

func mustObserve(t *testing.T, k *StaticKernel) Observed {
	t.Helper()
	obs, err := k.Observe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return obs
}

// The tunnel's opening has no port and names its one source; two tunnels to
// different brokers are two openings, not one deduplicated by port zero.
func TestASixInFourOpeningNamesItsSource(t *testing.T) {
	he := Opening{For: "IPv6 tunnel", Protocol: SixInFour, From: netip.MustParseAddr("216.66.80.26")}
	if got := he.Line(); got != "nft allow 6in4 from 216.66.80.26 for IPv6 tunnel" {
		t.Errorf("line = %q", got)
	}
	other := he
	other.From = netip.MustParseAddr("216.66.80.30")
	if got := normalizeOpenings([]Opening{he, other, he}); len(got) != 2 {
		t.Errorf("want the two sources kept and the duplicate dropped, got %+v", got)
	}
}

func TestIPTVRulesAreNarrowAndRemovedWhenDisabled(t *testing.T) {
	iptv := IPTV{Upstream: "iptv0", Downstream: []string{"lan0"}}
	desired := RenderIPTV(Config{Enabled: true}, []string{"lan0"}, nil, iptv)
	var igmp, multicast int
	for _, r := range desired.Input {
		if r.Kind == RuleIPTVIGMP {
			igmp++
		}
	}
	for _, r := range desired.Forward {
		if r.Kind == RuleIPTVStream {
			multicast++
			if r.Upstream != "iptv0" || r.Downstream != "lan0" {
				t.Fatalf("broad multicast rule: %+v", r)
			}
		}
	}
	if igmp != 2 || multicast != 1 {
		t.Fatalf("IGMP rules = %d, streams = %d", igmp, multicast)
	}
	if desired.Input[len(desired.Input)-1].Kind != RuleDrop || desired.Forward[len(desired.Forward)-1].Kind != RuleDrop {
		t.Fatal("IPTV bypassed final drop")
	}
	off := RenderIPTV(Config{}, []string{"lan0"}, nil, iptv)
	if len(off.Lines()) != 0 {
		t.Fatalf("firewall off retained IPTV rules: %v", off.Lines())
	}
	cleared := RenderIPTV(Config{Enabled: true}, []string{"lan0"}, nil, IPTV{})
	for _, r := range append(cleared.Input, cleared.Forward...) {
		if r.Kind == RuleIPTVIGMP || r.Kind == RuleIPTVStream {
			t.Fatalf("IPTV disabled but rule remains: %+v", r)
		}
	}
}

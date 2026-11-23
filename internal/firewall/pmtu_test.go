package firewall

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// The clamp value is the whole fix, so the arithmetic is asserted directly
// rather than only through a rendered line: 40 is the IPv4 and TCP header the
// MSS option does not include.
func TestMSSArithmetic(t *testing.T) {
	for _, tc := range []struct {
		mtu  int
		want int
	}{
		{1500, 1460},
		{1492, 1452}, // PPPoE on the way out, the case this exists for
		{1480, 1440}, // the 6in4 device
		{1400, 1360},
		{576, 536},
	} {
		if got := mssFor(tc.mtu); got != tc.want {
			t.Errorf("mssFor(%d) = %d, want %d", tc.mtu, got, tc.want)
		}
	}
}

func clampTest() MSSClamp {
	return MSSClamp{
		PathMTU: PathMTU{MTU: 1492, Method: MethodProbe, Interface: "ens18"},
		Inside:  map[string]int{"br-lan": 1500},
	}
}

// A path narrower than the inside interface is clamped in both directions:
// outbound so the server sees the client's real limit, and inbound so the
// client does not send segments the way out cannot carry.
func TestClampRendersOnBothSides(t *testing.T) {
	d := RenderMSS(Config{Enabled: true}, []string{"br-lan"}, nil, IPTV{}, clampTest())
	got := forwardLines(d)
	want := []string{
		"nft forward oifname ens18 tcp flags syn tcp option maxseg size set 1452",
		"nft forward oifname br-lan tcp flags syn tcp option maxseg size set 1452",
	}
	if !slices.Equal(got[:len(want)], want) {
		t.Fatalf("clamp lines = %v, want %v", got[:len(want)], want)
	}
	// They come before every verdict rule: after an accept they are dead text.
	for _, line := range got[:len(want)] {
		if strings.Contains(line, " accept") || strings.Contains(line, " drop") {
			t.Errorf("clamp rule carries a verdict: %q", line)
		}
	}

	// The rewrite is the only new rule kind, and it has no counter.
	for _, r := range d.Forward[:len(want)] {
		if r.Kind != RuleMSSClamp || r.MSS != 1452 {
			t.Errorf("clamp rule = %+v", r)
		}
	}
}

// Nothing is rendered when the path is as wide as the network: the advertised
// MSS is already true.
func TestNoClampWhenThePathMatchesTheInside(t *testing.T) {
	clamp := clampTest()
	clamp.PathMTU.MTU = 1500
	d := RenderMSS(Config{Enabled: true}, []string{"br-lan"}, nil, IPTV{}, clamp)
	if lines := forwardLines(d); slices.ContainsFunc(lines, func(l string) bool {
		return strings.Contains(l, "maxseg")
	}) {
		t.Fatalf("equal MTUs rendered a clamp: %v", lines)
	}
}

// Nothing is rendered when every inside interface is already narrower than the
// way out; clamping there would shrink a limit nobody exceeded.
func TestNoClampWhenInsideIsNarrower(t *testing.T) {
	clamp := clampTest()
	clamp.Inside = map[string]int{"br-lan": 1400}
	d := RenderMSS(Config{Enabled: true}, []string{"br-lan"}, nil, IPTV{}, clamp)
	if lines := forwardLines(d); slices.ContainsFunc(lines, func(l string) bool {
		return strings.Contains(l, "maxseg")
	}) {
		t.Fatalf("a narrower LAN rendered a clamp: %v", lines)
	}
}

// The tunnel is a second path. Traffic into it and traffic back into the
// networks are both clamped to the narrower of the two ways out.
func TestClampCoversTheSixInFourTunnel(t *testing.T) {
	clamp := clampTest()
	clamp.Tunnel, clamp.TunnelMTU = "olr-6in4", 1480
	d := RenderMSS(Config{Enabled: true}, []string{"br-lan"}, nil, IPTV{}, clamp)
	want := []string{
		"nft forward oifname ens18 tcp flags syn tcp option maxseg size set 1452",
		"nft forward oifname olr-6in4 tcp flags syn tcp option maxseg size set 1440",
		"nft forward oifname br-lan tcp flags syn tcp option maxseg size set 1440",
	}
	if got := forwardLines(d); !slices.Equal(got[:len(want)], want) {
		t.Fatalf("tunnel clamp lines = %v, want %v", got[:len(want)], want)
	}
}

// The forward chain is where a forwarded IPv4 path-MTU error has to survive.
// Without this rule only conntrack's `related` keeps it alive, and that does not
// cover an error for a flow it never saw.
func TestForwardChainAcceptsIPv4ICMP(t *testing.T) {
	d := Render(Config{Enabled: true}, []string{"br-lan"}, nil)
	if !slices.ContainsFunc(d.Forward, func(r Rule) bool {
		base, _, _ := strings.Cut(r.Line, " source ")
		return r.Kind == RuleForwardICMP && base == "nft forward icmp accept"
	}) {
		t.Fatalf("forward chain has no IPv4 ICMP rule: %v", forwardLines(d))
	}
}

// Detection is cached: the probe sends a packet and waits for an ICMP error, so
// running it per API call would be both slow and rude.
func TestMTUProberCachesItsFinding(t *testing.T) {
	calls := 0
	p := &MTUProber{
		TTL: time.Hour,
		Probe: func(context.Context) (PathMTU, error) {
			calls++
			return PathMTU{MTU: 1492, Method: MethodProbe, Interface: "ens18"}, nil
		},
	}
	for i := 0; i < 3; i++ {
		if got := p.Observe(); got.MTU != 1492 || got.Method != MethodProbe {
			t.Fatalf("Observe = %+v", got)
		}
	}
	if calls != 1 {
		t.Fatalf("probe ran %d times, want once", calls)
	}
}

// A failed detection is cached briefly, then retried: the usual cause is a line
// that has not come up, and saying "not detected" forever would hide its
// recovery.
func TestMTUProberRetriesAfterFailure(t *testing.T) {
	calls := 0
	p := &MTUProber{
		FailedTTL: time.Millisecond,
		Probe: func(context.Context) (PathMTU, error) {
			calls++
			return PathMTU{}, context.DeadlineExceeded
		},
	}
	first := p.Observe()
	if first.Method != MethodUnavailable || first.Error == "" {
		t.Fatalf("failed detection = %+v", first)
	}
	time.Sleep(2 * time.Millisecond)
	p.Observe()
	if calls != 2 {
		t.Fatalf("probe ran %d times, want a retry", calls)
	}
}

func TestClampProblem(t *testing.T) {
	needed := clampTest()

	t.Run("nothing to say when the clamp is rendered", func(t *testing.T) {
		if _, ok := clampProblem(Config{Enabled: true}, needed); ok {
			t.Fatal("reported a problem for a ruleset that clamps")
		}
	})
	t.Run("nothing to say when no clamp is needed", func(t *testing.T) {
		clamp := clampTest()
		clamp.PathMTU.MTU = 1500
		if _, ok := clampProblem(Config{Enabled: true}, clamp); ok {
			t.Fatal("reported a problem with no narrower path")
		}
	})
	t.Run("firewall off", func(t *testing.T) {
		p, ok := clampProblem(Config{}, needed)
		if !ok || p.Path != "mss_clamp" {
			t.Fatalf("problem = %+v, %v", p, ok)
		}
		for _, want := range []string{"1492", "1500", "first full-size packet", "hang"} {
			if !strings.Contains(p.Message, want) {
				t.Errorf("message %q does not mention %q", p.Message, want)
			}
		}
	})
	t.Run("native rules", func(t *testing.T) {
		p, ok := clampProblem(Config{Enabled: true, Ruleset: []byte(`{"nftables":[]}`)}, needed)
		if !ok || !strings.Contains(p.Message, "native") {
			t.Fatalf("problem = %+v, %v", p, ok)
		}
	})
}

// forwardLines is the forward chain's text without the per-source suffixes, so
// a test can assert on the canonical order.
func forwardLines(d Desired) []string {
	out := make([]string, 0, len(d.Forward))
	for _, r := range d.Forward {
		out = append(out, r.Line)
	}
	return out
}

// A clamp rewrites a packet rather than deciding one, so it must not appear in
// the traffic ledger or make the totals look incomplete.
func TestClampIsNotTraffic(t *testing.T) {
	d := RenderMSS(Config{Enabled: true}, []string{"br-lan"}, nil, IPTV{}, clampTest())
	traffic, _ := trafficFor(d, map[string]TrafficCount{})
	for _, entry := range traffic {
		if strings.Contains(entry.ID, "maxseg") {
			t.Fatalf("clamp rule counted as traffic: %+v", entry)
		}
	}
}

// The observation reaches the plan, the plan reaches the kernel, and reading it
// back is a no-op — the round trip every rendered line has to survive.
func TestPlanAppliesTheClamp(t *testing.T) {
	clamp := clampTest()
	b := StaticBoundary{Interfaces: []string{"br-lan"}, Clamp: &clamp}
	k := &StaticKernel{}

	_, d, err := BuildPlan(Config{Enabled: true}, b, mustObserve(t, k), "")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(d.Forward, func(r Rule) bool { return r.Kind == RuleMSSClamp }) {
		t.Fatalf("plan has no clamp: %v", forwardLines(d))
	}
	if err := k.Apply(context.Background(), d); err != nil {
		t.Fatal(err)
	}
	replan, _, err := BuildPlan(Config{Enabled: true}, b, mustObserve(t, k), "")
	if err != nil {
		t.Fatal(err)
	}
	if !replan.Empty {
		t.Fatalf("re-planning the applied clamp is not a no-op: %+v", replan.Changes)
	}
}

// The status problem is what tells an operator that the clamp they are not
// getting is costing them something.
func TestStatusReportsTheMissingClamp(t *testing.T) {
	clamp := clampTest()
	a := Applier{
		Store:    core.NewStore(filepath.Join(t.TempDir(), "olr.json"), ModuleName),
		Kernel:   &StaticKernel{},
		Boundary: StaticBoundary{Interfaces: []string{"br-lan"}, Clamp: &clamp},
	}
	if err := a.save(Config{}); err != nil {
		t.Fatal(err)
	}
	st, err := a.GetStatus(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if st.PathMTU.MTU != 1492 || st.PathMTU.Method != MethodProbe {
		t.Fatalf("status path MTU = %+v", st.PathMTU)
	}
	if len(st.Problems) != 1 || st.Problems[0].Path != "mss_clamp" {
		t.Fatalf("status problems = %+v", st.Problems)
	}
}

package firewall

import (
	"context"
	"fmt"
	"net/netip"
	"slices"
	"strings"
	"sync"
	"time"

	probing "github.com/prometheus-community/pro-bing"
	"github.com/vishvananda/netlink"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// Path MTU discovery and TCP MSS clamping.
//
// A client on a 1500-byte network believes every packet up to 1500 bytes can
// leave it, and advertises that belief in every SYN as MSS 1460. When the way
// out is a PPPoE session at 1492, the first full-size packet of every new
// connection is discarded by the first router on the way and has to wait for an
// ICMP "fragmentation needed" before the endpoints retry smaller. That is the
// ordinary case: a visible delay on the first packet of every connection. When
// ICMP is filtered anywhere along the path it is worse — the connection stalls
// instead of retrying.
//
// The fix is to clamp the advertised MSS to what the path can carry, on both
// directions of the forward chain: the client's SYN on its way out, and the
// server's SYN-ACK on its way in. Changing the interface MTU is not the fix:
// it would fragment or reset LAN traffic for a limit that exists only on the
// way out, and it would claim a smaller link than the LAN actually is.
//
// Detection is deliberately not configuration. The way out is a property of the
// box, not a setting, and an operator should not have to re-type 1492 into
// three places (here, the interface, and PPPoE) for it to stay true.

// PathMTU is what was learned about the way out, and how.
type PathMTU struct {
	// MTU is the path's maximum transmission unit in bytes, zero when it could
	// not be learned.
	MTU int `json:"mtu"`

	// Method is how it was found, one of the Method constants below.
	Method string `json:"method,omitempty"`

	// Interface is the egress interface the measurement belongs to.
	Interface string `json:"interface,omitempty"`

	// At is when the observation was made.
	At time.Time `json:"at,omitzero"`

	// Error is why detection failed, for MethodUnavailable only.
	Error string `json:"error,omitempty"`
}

// The ways a path MTU is found. Their names are the evidence: an operator
// deciding whether to trust 1492 should be able to see whether it was measured
// or merely believed by the kernel.
const (
	// MethodProbe means a DF-set echo at the interface MTU just taught the
	// kernel a smaller path MTU, which the route lookup then returned.
	MethodProbe = "probe"

	// MethodRoute means the kernel already held a smaller cached path MTU, so
	// no probe was needed to discover it.
	MethodRoute = "route"

	// MethodInterface means the probe found nothing smaller than the interface
	// itself: the path is as wide as the link, or something is filtering the
	// ICMP that would say otherwise.
	MethodInterface = "interface"

	// MethodUnavailable means no path MTU could be learned at all.
	MethodUnavailable = "unavailable"
)

// String is the evidence in one line, for `olr firewall status`.
func (p PathMTU) String() string {
	switch p.Method {
	case MethodProbe:
		return fmt.Sprintf("%d via %s (probed)", p.MTU, p.Interface)
	case MethodRoute:
		return fmt.Sprintf("%d via %s (cached route)", p.MTU, p.Interface)
	case MethodInterface:
		return fmt.Sprintf("%d via %s (no smaller path found)", p.MTU, p.Interface)
	case MethodUnavailable:
		return "not detected: " + p.Error
	default:
		if p.MTU > 0 {
			return fmt.Sprintf("%d", p.MTU)
		}
		return "not detected"
	}
}

// MTUProber detects the way out's path MTU and keeps the answer for a while.
//
// The cache is the whole point: the probe sends a packet and waits for an ICMP
// error, which is not something to do on every API call. A finding lives for
// minutes rather than seconds because a path MTU changes when a line does, not
// when the UI is refreshed.
type MTUProber struct {
	// Probe performs one detection. Nil means probePathMTU, the real one.
	// Injectable so the cache can be tested without a network.
	Probe func(ctx context.Context) (PathMTU, error)

	// TTL is how long a successful finding is kept. Zero means five minutes.
	TTL time.Duration

	// FailedTTL is how long a failed detection is kept. Zero means thirty
	// seconds: the common cause is a line that has not come up yet, and that is
	// worth retrying sooner than a path that has already been measured.
	FailedTTL time.Duration

	mu     sync.Mutex
	cached PathMTU
	known  bool
	until  time.Time
}

// NewMTUProber returns a prober with the default TTLs.
func NewMTUProber() *MTUProber { return &MTUProber{} }

// Observe returns what is known about the path MTU, probing only when the
// cached answer has expired.
//
// The lock is held across the probe so that a burst of readers produces one
// measurement rather than one each. The probe is bounded (roughly a second) and
// this is called from apply and status paths, not from a hot loop.
func (p *MTUProber) Observe() PathMTU {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.known && time.Now().Before(p.until) {
		return p.cached
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	mtu, err := p.probe()(ctx)
	if err != nil {
		mtu = PathMTU{Method: MethodUnavailable, At: time.Now(), Error: err.Error()}
	}
	p.cached, p.known = mtu, true
	p.until = time.Now().Add(p.ttl(mtu.Method == MethodUnavailable))
	return mtu
}

func (p *MTUProber) probe() func(context.Context) (PathMTU, error) {
	if p.Probe != nil {
		return p.Probe
	}
	return probePathMTU
}

func (p *MTUProber) ttl(failed bool) time.Duration {
	if failed {
		if p.FailedTTL > 0 {
			return p.FailedTTL
		}
		return 30 * time.Second
	}
	if p.TTL > 0 {
		return p.TTL
	}
	return 5 * time.Minute
}

// probeDest is where the active probe is aimed. It does not need to answer:
// what the probe wants is the ICMP "fragmentation needed" that the first router
// with a smaller link sends back. An anycast resolver is used because it is
// routed from everywhere and is not an olr-invented dependency.
var probeDest = netip.MustParseAddr("1.1.1.1")

// probePathMTU measures the way out.
//
// The route lookup is what makes this self-contained: it names the egress
// interface, its MTU, and the MTU the kernel has cached for the destination.
// When the cache already holds a smaller number, that is the finding and no
// packet is sent. Otherwise one DF-set echo at the interface MTU is sent and
// the kernel is asked again — the router's ICMP error is what fills the cache.
func probePathMTU(ctx context.Context) (PathMTU, error) {
	now := time.Now()
	name, ifaceMTU, cached, err := routeTo(probeDest)
	if err != nil {
		return PathMTU{}, err
	}
	if cached > 0 && ifaceMTU > 0 && cached < ifaceMTU {
		return PathMTU{MTU: cached, Method: MethodRoute, Interface: name, At: now}, nil
	}
	if ifaceMTU <= 0 {
		return PathMTU{}, fmt.Errorf("interface %s reports no MTU", name)
	}

	// A lost echo is not an error here: no reply is the expected outcome, and
	// the ICMP error we came for is not an echo reply at all. Only a failure to
	// send is worth reporting, and even then the route may already have learned
	// something, so try the lookup before giving up.
	sendErr := dfEcho(ctx, probeDest, name, ifaceMTU)

	for attempt := 0; attempt < 4; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return PathMTU{}, ctx.Err()
			case <-time.After(150 * time.Millisecond):
			}
		}
		if _, _, mtu, err := routeTo(probeDest); err == nil && mtu > 0 && mtu < ifaceMTU {
			return PathMTU{MTU: mtu, Method: MethodProbe, Interface: name, At: now}, nil
		}
	}
	if sendErr != nil {
		return PathMTU{}, fmt.Errorf("probing %s via %s: %w", probeDest, name, sendErr)
	}
	return PathMTU{MTU: ifaceMTU, Method: MethodInterface, Interface: name, At: now}, nil
}

// routeTo resolves the egress interface for a destination, its MTU, and the
// bytes the kernel has cached for that destination (zero when it has none).
func routeTo(dst netip.Addr) (name string, ifaceMTU, cached int, err error) {
	routes, err := netlink.RouteGet(dst.AsSlice())
	if err != nil {
		return "", 0, 0, fmt.Errorf("looking up the route to %s: %w", dst, err)
	}
	if len(routes) == 0 {
		return "", 0, 0, fmt.Errorf("no route to %s", dst)
	}
	route := routes[0]
	link, err := netlink.LinkByIndex(route.LinkIndex)
	if err != nil {
		return "", 0, 0, fmt.Errorf("reading interface %d: %w", route.LinkIndex, err)
	}
	return link.Attrs().Name, link.Attrs().MTU, route.MTU, nil
}

// dfEcho sends one DF-set echo request sized to fill the interface MTU, so that
// any smaller link on the way has to answer with "fragmentation needed".
func dfEcho(ctx context.Context, dst netip.Addr, iface string, ifaceMTU int) error {
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()

	pinger := probing.New(dst.String())
	pinger.InterfaceName = iface
	pinger.SetDoNotFragment(true)
	// 20 bytes of IPv4 header and 8 of ICMP header come out of the MTU.
	pinger.Size = ifaceMTU - 28
	pinger.Count = 1
	pinger.Interval = time.Second
	pinger.Timeout = 900 * time.Millisecond
	pinger.SetLogger(probing.NoopLogger{})
	return pinger.RunWithContext(ctx)
}

// MSSClamp is the observation MSS rules are rendered from: what the way out can
// carry, and how wide each trusted interface is.
type MSSClamp struct {
	// PathMTU is the detected way out.
	PathMTU PathMTU

	// Inside is each trusted interface's MTU, keyed by name.
	Inside map[string]int

	// Tunnel is the 6in4 device and its MTU, when the uplink has one.
	Tunnel    string
	TunnelMTU int
}

// maxInside is the widest trusted interface, which is the one clamping has to
// satisfy.
func (c MSSClamp) maxInside() int {
	widest := 0
	for _, mtu := range c.Inside {
		if mtu > widest {
			widest = mtu
		}
	}
	return widest
}

// under reports whether any trusted interface is wider than mtu.
func (c MSSClamp) under(mtu int) bool { return mtu > 0 && mtu < c.maxInside() }

// Needed reports whether any path is narrower than a trusted interface, and
// the narrowest such path. This is the condition for rendering a clamp at all:
// with no narrower path the advertised MSS is already true.
func (c MSSClamp) Needed() (int, bool) {
	narrowest := 0
	for _, mtu := range []int{c.PathMTU.MTU, c.TunnelMTU} {
		if c.under(mtu) && (narrowest == 0 || mtu < narrowest) {
			narrowest = mtu
		}
	}
	return narrowest, narrowest > 0
}

// mssFor is the segment size a path of mtu can carry: the MTU less 20 bytes of
// IPv4 and 20 of TCP header. The value is what goes into the MSS option, and 40
// is also the right subtraction for the 6in4 device because the option is
// clamped by the smallest path the traffic can take.
func mssFor(mtu int) int { return mtu - 40 }

// clampRules are the forward-chain rules that rewrite the MSS option.
//
// They come before every verdict rule in the chain: they have no verdict of
// their own, so they rewrite a SYN where they see it and let evaluation
// continue. A rule after `from <inside> accept` would never see an outgoing
// SYN at all.
//
// Toward the uplink is the client's SYN; toward each trusted interface is the
// server's SYN-ACK. Both advertise a segment the path cannot carry, and both
// are rewritten to the narrowest path that interface's traffic may use.
func clampRules(c MSSClamp) []Rule {
	var rules []Rule
	if c.PathMTU.MTU > 0 && c.PathMTU.Interface != "" && c.under(c.PathMTU.MTU) {
		rules = append(rules, clampRule(c.PathMTU.Interface, c.PathMTU.MTU))
	}
	if c.Tunnel != "" && c.under(c.TunnelMTU) {
		rules = append(rules, clampRule(c.Tunnel, c.TunnelMTU))
	}

	names := make([]string, 0, len(c.Inside))
	for name := range c.Inside {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		mtu := c.Inside[name]
		narrowest := 0
		for _, path := range []int{c.PathMTU.MTU, c.TunnelMTU} {
			if path > 0 && mtu > path && (narrowest == 0 || path < narrowest) {
				narrowest = path
			}
		}
		if narrowest > 0 {
			rules = append(rules, clampRule(name, narrowest))
		}
	}
	return rules
}

func clampRule(iface string, pathMTU int) Rule {
	mss := mssFor(pathMTU)
	return Rule{
		Kind:      RuleMSSClamp,
		Interface: iface,
		MSS:       mss,
		Line: fmt.Sprintf("nft forward oifname %s tcp flags syn tcp option maxseg size set %d",
			iface, mss),
	}
}

// clampProblem is the finding shown when a clamp is needed but nothing renders
// one: the firewall is off, or it is running a native ruleset that was saved
// before this rule existed.
func clampProblem(c Config, clamp MSSClamp) (core.Problem, bool) {
	if c.Enabled && len(c.Ruleset) == 0 {
		return core.Problem{}, false
	}
	pathMTU, needed := clamp.Needed()
	if !needed {
		return core.Problem{}, false
	}

	var b strings.Builder
	fmt.Fprintf(&b, "the way out carries at most %d-byte packets, but your networks are %d, and no MSS clamp is in force. ",
		pathMTU, clamp.maxInside())
	b.WriteString("Every new connection's first full-size packet is discarded and retried after an ICMP " +
		"\"fragmentation needed\"; where that ICMP is filtered, the connection can hang instead. ")
	if !c.Enabled {
		b.WriteString("Turning the firewall on adds the clamp.")
	} else {
		b.WriteString("Saved native rules replace the generated ones; add a rule that clamps the TCP MSS " +
			"option to " + fmt.Sprint(mssFor(pathMTU)) + " on the way out and back.")
	}
	return core.Problem{Path: "mss_clamp", Message: b.String()}, true
}

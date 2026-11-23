package firewall

import (
	"fmt"
	"strings"
)

// Rendering the ruleset a config asks for.
//
// Pure, for the reason internal/gateway/nat's render.go gives: Render returns a
// value, the planner diffs it against what the kernel holds, the tests assert
// on it, and only kernel_linux.go turns it into netlink. The ruleset is
// docs/firewall.md §3, and it is short enough to read in one go:
//
//	input    (to this router)          forward  (through it)
//	  established, related  accept       mss clamp             (no verdict)
//	  loopback              accept       established, related  accept
//	  from inside           accept       from inside           accept
//	  icmp, icmpv6          accept       not toward inside     accept
//	  dhcp, dhcpv6 client   accept       port forwards (dnat)  accept
//	  each opening          accept       icmp, icmpv6          accept
//	  drop                               drop
//
// Each rule is partitioned by incoming source and counted without changing
// its order or verdict. Both chains end in explicit counted drops under an
// accept policy; named counters also retain the aggregate blocked totals.

// TableName is the table this module owns, whole.
const TableName = "olr_filter"

// Chain names. Named for their hooks, so `nft list table inet olr_filter`
// needs no legend.
const (
	InputChain   = "input"
	ForwardChain = "forward"
)

// Counter names, one per chain's final drop.
const (
	InputCounter   = "blocked_input"
	ForwardCounter = "blocked_forward"
)

// RuleKind is which of the fixed shapes a rule has. The kernel layer maps each
// to its expressions; everything above it only needs the line.
type RuleKind int

const (
	RuleEstablished RuleKind = iota
	RuleLoopback
	RuleFromInside
	RuleICMP
	RuleICMPv6
	RuleDHCPClient
	RuleDHCPv6Client
	RuleOpening
	RuleNotTowardInside
	RulePortForward
	RuleForwardICMP
	RuleForwardICMPv6
	RuleDrop
	RuleIPTVIGMP
	RuleIPTVStream

	// RuleMSSClamp rewrites the TCP MSS option and has no verdict of its own:
	// it narrows what a connection advertises, then evaluation continues into
	// the accept or drop that would have matched anyway. It is the one rule
	// that must sit before every verdict, and the one rule that is not
	// partitioned by traffic source.
	RuleMSSClamp
)

// Rule is one rule, in the order it is evaluated.
type Rule struct {
	Kind   RuleKind
	Source TrafficSource

	// Opening is set for RuleOpening.
	Opening Opening

	Upstream   string
	Downstream string

	// Counter is the legacy aggregate counter for RuleDrop.
	Counter string

	// Interface is the egress interface RuleMSSClamp applies to.
	Interface string

	// MSS is the value RuleMSSClamp writes into the TCP MSS option.
	MSS int

	// Line is the canonical text: the diff basis, what the UI prints, and the
	// comment stored on the rule so reading the kernel back reproduces it.
	Line string
}

// Desired is the kernel state a config asks for.
type Desired struct {
	Ruleset []byte
	// Enabled is false when the module is off, and then there is no table at
	// all: applying removes ours and the box filters exactly as it did before.
	Enabled bool

	// Inside are the trusted interfaces, sorted.
	Inside []string

	// Openings are the ports served to the outside, sorted.
	Openings []Opening
	IPTV     IPTV

	Input   []Rule
	Forward []Rule
}

// Render turns intent and what the rest of olr says into a ruleset.
//
// It assumes Validate has passed. In particular it does not guard against an
// empty inside list, which would render a table that drops the operator's own
// connection; Validate refuses that before anything reaches here.
func Render(c Config, inside []string, openings []Opening) Desired {
	return RenderMSS(c, inside, openings, IPTV{}, MSSClamp{})
}

func RenderIPTV(c Config, inside []string, openings []Opening, iptv IPTV) Desired {
	return RenderMSS(c, inside, openings, iptv, MSSClamp{})
}

// RenderMSS is Render with the path-MTU observation that decides whether MSS
// clamping is needed. The two renderers without it exist for callers that have
// no observation — tests, and a box whose kernel cannot tell — and render
// exactly what those callers can be sure of.
func RenderMSS(c Config, inside []string, openings []Opening, iptv IPTV, clamp MSSClamp) Desired {
	d := Desired{Enabled: c.Enabled}
	if !c.Enabled {
		return d
	}
	d.Inside = normalizeInside(inside)
	d.Openings = normalizeOpenings(openings)
	d.IPTV = iptv
	names := strings.Join(d.Inside, ",")

	rule := func(chain string, kind RuleKind, what string) Rule {
		return Rule{Kind: kind, Line: fmt.Sprintf("nft %s %s", chain, what)}
	}

	d.Input = []Rule{
		// First, because it is what nearly every packet is, and because every
		// rule after it only has to think about a connection's first packet.
		rule(InputChain, RuleEstablished, "established,related accept"),
		rule(InputChain, RuleLoopback, "from lo accept"),
		rule(InputChain, RuleFromInside, "from "+names+" accept"),
		// ICMP is not optional on IPv6: neighbour discovery and router
		// advertisements are ICMPv6, and blocking "packet too big" breaks path
		// MTU discovery in the way that looks like "ping works, pages hang".
		// The types are RFC 4890's for a host; the v4 set is the errors and
		// echo, so the uplink still answers a ping.
		rule(InputChain, RuleICMP, "icmp accept"),
		rule(InputChain, RuleICMPv6, "icmpv6 accept"),
		// The uplink's own address may come from DHCP, and a lease offer does
		// not belong to any connection conntrack has seen.
		rule(InputChain, RuleDHCPClient, "dhcp client accept"),
		rule(InputChain, RuleDHCPv6Client, "dhcpv6 client accept"),
	}
	if iptv.Upstream != "" {
		for _, down := range iptv.Downstream {
			d.Input = append(d.Input, Rule{Kind: RuleIPTVIGMP, Downstream: down, Line: fmt.Sprintf("nft input igmp from %s for IPTV", down)})
		}
		d.Input = append(d.Input, Rule{Kind: RuleIPTVIGMP, Downstream: iptv.Upstream, Line: fmt.Sprintf("nft input igmp from %s for IPTV", iptv.Upstream)})
	}
	for _, o := range d.Openings {
		d.Input = append(d.Input, Rule{Kind: RuleOpening, Opening: o, Line: o.Line()})
	}
	d.Input = append(d.Input, Rule{
		Kind: RuleDrop, Counter: InputCounter,
		Line: fmt.Sprintf("nft %s drop counter %s", InputChain, InputCounter),
	})

	d.Forward = []Rule{
		rule(ForwardChain, RuleEstablished, "established,related accept"),
		rule(ForwardChain, RuleFromInside, "from "+names+" accept"),
		// Traffic that is not headed into one of our networks is not this
		// module's to judge. That is Docker, libvirt and anything else
		// forwarding between interfaces olr does not own — and it is why a box
		// running containers keeps working with this switched on.
		rule(ForwardChain, RuleNotTowardInside, "to not "+names+" accept"),
		// What a port forward translated is let through: the forward *is* the
		// operator's permission, so there is nothing to write twice.
		rule(ForwardChain, RulePortForward, "port forwards accept"),
		// Echo and errors toward inside hosts, per RFC 4890. There is no NAT
		// hiding a v4 path MTU here either: without the errors, a forwarded
		// connection that meets a smaller link waits for a message this box
		// drops. `related` covers most of them because conntrack ties the error
		// to the connection it belongs to, but ICMP about a flow conntrack has
		// already forgotten — or never saw — does not survive that.
		rule(ForwardChain, RuleForwardICMP, "icmp accept"),
		rule(ForwardChain, RuleForwardICMPv6, "icmpv6 accept"),
		{
			Kind: RuleDrop, Counter: ForwardCounter,
			Line: fmt.Sprintf("nft %s drop counter %s", ForwardChain, ForwardCounter),
		},
	}
	// First, because they have no verdict: a clamp rule after any accept is
	// dead text. See clampRules for why both directions are clamped.
	if clamps := clampRules(clamp); len(clamps) > 0 {
		d.Forward = append(clamps, d.Forward...)
	}
	if iptv.Upstream != "" {
		for _, down := range iptv.Downstream {
			rule := Rule{Kind: RuleIPTVStream, Upstream: iptv.Upstream, Downstream: down, Line: fmt.Sprintf("nft forward IPv4 multicast %s to %s for IPTV", iptv.Upstream, down)}
			d.Forward = append(d.Forward[:len(d.Forward)-1], rule, d.Forward[len(d.Forward)-1])
		}
	}
	d.Input = splitSources(d.Input, d.Inside)
	d.Forward = splitSources(d.Forward, d.Inside)
	return d
}

// Lines is the whole desired state as canonical text, in evaluation order.
func (d Desired) Lines() []string {
	if !d.Enabled {
		return nil
	}
	out := []string{
		"nft table inet " + TableName,
		"nft counter " + ForwardCounter,
		"nft counter " + InputCounter,
		"nft chain " + InputChain + " filter",
	}
	for _, r := range d.Input {
		out = append(out, r.Line)
	}
	out = append(out, "nft chain "+ForwardChain+" filter")
	for _, r := range d.Forward {
		out = append(out, r.Line)
	}
	return out
}

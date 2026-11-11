package firewall

import (
	"fmt"
	"slices"
	"strings"
)

// TrafficSource partitions incoming interfaces, including interfaces appearing
// after apply. Outside is every interface other than lo or a trusted interface.
type TrafficSource string

const (
	SourceOutside TrafficSource = "outside"
	SourceInside  TrafficSource = "inside"
	SourceLocal   TrafficSource = "local"
)

type TrafficCount struct {
	Packets uint64 `json:"packets"`
	Bytes   uint64 `json:"bytes"`
}

// Traffic records the first matching decision, not every overlapping condition.
// Input and forward are kept distinct; a packet traverses only one of them.
type Traffic struct {
	ID          string        `json:"id"`
	Source      TrafficSource `json:"source"`
	Category    string        `json:"category"`
	Description string        `json:"description"`
	Destination string        `json:"destination"`
	Verdict     string        `json:"verdict"`
	TrafficCount
}

func splitSources(rules []Rule, inside []string) []Rule {
	var out []Rule
	for _, rule := range rules {
		sources := []TrafficSource{SourceOutside, SourceInside, SourceLocal}
		switch rule.Kind {
		case RuleFromInside:
			sources = []TrafficSource{SourceInside}
			if slices.Contains(inside, "lo") {
				sources = append(sources, SourceLocal)
			}
		case RuleLoopback:
			sources = []TrafficSource{SourceLocal}
		case RuleIPTVIGMP, RuleIPTVStream:
			iface := rule.Downstream
			if rule.Kind == RuleIPTVStream {
				iface = rule.Upstream
			}
			source := SourceOutside
			if iface == "lo" {
				source = SourceLocal
			} else if slices.Contains(inside, iface) {
				source = SourceInside
			}
			sources = []TrafficSource{source}
		}
		for _, source := range sources {
			r := rule
			r.Source = source
			r.Line += " source " + string(source) + " counted"
			out = append(out, r)
		}
	}
	return out
}

func trafficFor(d Desired, counts map[string]TrafficCount) ([]Traffic, bool) {
	traffic := []Traffic{}
	complete := d.Enabled
	for _, chain := range []struct {
		rules []Rule
		dest  string
	}{{d.Input, "router"}, {d.Forward, "forwarded"}} {
		for _, r := range chain.rules {
			count, ok := counts[r.Line]
			complete = complete && ok
			category, description := semanticCategory(r)
			verdict := "allowed"
			if r.Kind == RuleDrop {
				verdict = "blocked"
			}
			traffic = append(traffic, Traffic{ID: r.Line, Source: r.Source,
				Category: category, Description: description, Destination: chain.dest,
				Verdict: verdict, TrafficCount: count})
		}
	}
	return traffic, complete
}

func semanticCategory(r Rule) (string, string) {
	switch r.Kind {
	case RuleEstablished:
		return "Replies and related traffic", "Established connections and related traffic."
	case RuleLoopback:
		return "Router-local traffic", "Loopback traffic to services on this router."
	case RuleFromInside:
		return "Inside-initiated access", "Connections from trusted inside interfaces, including enabled remote-access WireGuard."
	case RuleICMP:
		return "ICMP", "Permitted IPv4 errors and ping to the router."
	case RuleICMPv6:
		return "ICMPv6", "Permitted IPv6 errors, ping and control messages to the router."
	case RuleForwardICMPv6:
		return "ICMPv6", "Permitted IPv6 errors and ping to inside clients."
	case RuleDHCPClient:
		return "DHCP", "IPv4 lease replies for the router’s DHCP client."
	case RuleDHCPv6Client:
		return "DHCPv6", "IPv6 lease replies for the router’s DHCPv6 client."
	case RuleOpening:
		o := r.Opening
		if o.Protocol == SixInFour {
			return o.For + " · IP protocol 41", fmt.Sprintf("IP protocol 41 to the router, only from tunnel broker %s.", o.From)
		}
		return fmt.Sprintf("%s · %s %d", o.For, strings.ToUpper(string(o.Protocol)), o.Port), "Port opened by this service on the router."
	case RuleNotTowardInside:
		return "Other forwarded traffic", "Destination interface is outside the trusted inside set."
	case RulePortForward:
		return "Port-forwarded connections", "Connections translated by a configured port forward (DNAT)."
	case RuleIPTVIGMP:
		return "IPTV · IGMP", "Membership messages on " + r.Downstream + "."
	case RuleIPTVStream:
		return "IPTV · IPv4 multicast", "Streams from " + r.Upstream + " to " + r.Downstream + "."
	case RuleDrop:
		if r.Counter == InputCounter {
			return "Unsolicited access to the router", "Traffic to the router that matches none of the preceding permissions."
		}
		return "Unsolicited access to inside clients", "Traffic headed into trusted networks that matches none of the preceding permissions."
	}
	return "Unknown", ""
}

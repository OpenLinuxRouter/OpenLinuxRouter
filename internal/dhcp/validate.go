package dhcp

import (
	"errors"
	"fmt"
	"net/netip"
	"strings"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// Validation is the highest-value mechanism in the whole apply story
// (design.md §5.3.1). There is no cross-module transaction, so nothing unwinds
// a bad change — but atomic *validation* is cheap where atomic *apply* is not,
// because validation is pure reads and needs no coordination.
//
// It became markedly more pure when pools moved from interfaces to networks.
// Every rule about a range used to be checked against an interface's observed
// prefixes, so the rules needed the kernel and a stored config could be made
// invalid by something that happened outside olr entirely. Now a range is
// checked against the network's stored subnet, and the only fact still read
// from the machine is whether the members are up — which is a warning.

// Problem is one validation finding, addressed by a JSON-ish path so a UI can
// attach it to the field that caused it.
type Problem struct {
	Path    string
	Message string
}

func (p Problem) String() string {
	if p.Path == "" {
		return p.Message
	}
	return p.Path + ": " + p.Message
}

// Result separates the fatal from the merely suspect.
//
// The distinction earns its keep: a reservation inside a dynamic range is a
// real hazard but dnsmasq permits it and some people rely on it, so refusing
// would be us overruling the operator on their own network. Warnings say so
// and get out of the way.
type Result struct {
	Errors   []Problem
	Warnings []Problem
}

func (r *Result) errorf(path, format string, args ...any) {
	r.Errors = append(r.Errors, Problem{Path: path, Message: fmt.Sprintf(format, args...)})
}

func (r *Result) warnf(path, format string, args ...any) {
	r.Warnings = append(r.Warnings, Problem{Path: path, Message: fmt.Sprintf(format, args...)})
}

// OK reports whether the config can be applied.
func (r Result) OK() bool { return len(r.Errors) == 0 }

// Err collapses the errors into one, or nil.
func (r Result) Err() error {
	if r.OK() {
		return nil
	}
	msgs := make([]error, len(r.Errors))
	for i, p := range r.Errors {
		msgs[i] = errors.New(p.String())
	}
	return fmt.Errorf("invalid dhcp configuration:\n  %w", errors.Join(msgs...))
}

// ownedDirectives are the dnsmasq directives this module renders itself.
//
// The escape hatch (Config.ExtraConf) is additive by design. Letting it set one
// of these would let the rendered file contradict the config that produced it,
// which defeats the single-source rule the escape hatch exists to preserve —
// the whole point is that unusual settings stay revisioned rather than being
// hand-edited into the daemon's file.
var ownedDirectives = map[string]string{
	"port":             "DNS is disabled unconditionally; it belongs to the dns module (design.md §4.2)",
	"interface":        "set by the pool's interface field",
	"except-interface": "managed with interface",
	"bind-interfaces":  "managed with interface",
	"dhcp-range":       "set by the pool's start, end, lease_time and ra fields",
	"dhcp-authoritative": "always set; olr refuses to start when another DHCP server holds the port, " +
		"so this module is authoritative by construction",
	"dhcp-lease-max": "sized from the configured pools, so that a large pool is not silently capped",
	"dhcp-host":      "use reservations",
	"dhcp-hostsfile": "reservations are rendered into the module's hosts directory",
	"dhcp-hostsdir":  "reservations are rendered into the module's hosts directory",
	"dhcp-optsfile":  "use the pool's options field",
	"dhcp-optsdir":   "use the pool's options field",
	"dhcp-leasefile": "the lease database location is fixed",
	"dhcp-script":    "reserved for publishing leases to the dns module",
	"conf-file":      "the module owns this file; including another would hide configuration from olr",
	"conf-dir":       "the module owns this file; including another would hide configuration from olr",
	"pid-file":       "managed with the service unit",
	"user":           "managed with the service unit",
	"group":          "managed with the service unit",
}

// renderedOptions are the DHCP options with a dedicated config field. Setting
// one through Pool.Options as well would emit two dhcp-option lines for the
// same tag, and which one dnsmasq honours is not something we want to depend on.
var renderedOptions = map[string]string{
	"3": "gateway", "router": "gateway",
	"6": "dns", "dns-server": "dns",
	"15": "domain", "domain-name": "domain",
	"42": "ntp", "ntp-server": "ntp",
}

// Validate checks a config against itself and against the link module's view of
// the interfaces it names.
//
// It is pure: no files, no netlink, no root. That is what lets the entire rule
// set be table-tested and lets `olr dhcp` check a config on a laptop.
func Validate(c Config, networks NetworkView) Result {
	var r Result

	if c.Enabled && len(c.Pools) == 0 {
		r.warnf("pools", "DHCP is enabled but no pool is configured, so nothing will be served")
	}

	// subnets maps a network to the prefix its pool sits in, and ranges to the
	// resolved range, for the checks below. Both are resolved once: a derived
	// range recomputed per rule is a derived range that eventually differs
	// between two of them.
	subnets := map[string]netip.Prefix{}
	ranges := map[string][2]netip.Addr{}
	seenNetwork := map[string]int{}

	for i, p := range c.Pools {
		path := fmt.Sprintf("pools[%d]", i)

		if p.Network == "" {
			r.errorf(path+".network", "required")
			continue
		}
		if first, dup := seenNetwork[p.Network]; dup {
			r.errorf(path+".network", "network %q already has a pool at pools[%d]; one pool per network",
				p.Network, first)
			continue
		}
		seenNetwork[p.Network] = i

		info, err := networks.Network(p.Network)
		if err != nil {
			r.errorf(path+".network", "%v; `olr net show` lists the networks there are", err)
			continue
		}
		if !info.Up {
			r.warnf(path+".network", "%q is down; the pool is configured but will not serve until it comes up",
				p.Network)
		}
		if p.IPv4 == nil && p.RA() == RAOff {
			r.errorf(path, "%q serves neither IPv4 nor IPv6, so it does nothing; "+
				"give it an ipv4 range or set ipv6.mode", p.Network)
		}

		if start, end, ok := validatePoolIPv4(&r, path, p, info); ok {
			subnets[p.Network] = info.Subnet
			ranges[p.Network] = [2]netip.Addr{start, end}
		}
		validatePoolIPv6(&r, path, p, info)

		if lt := p.LeaseTime; lt != 0 && lt < MinLeaseTime {
			r.errorf(path+".lease_time", "%s is below dnsmasq's two minute minimum", lt)
		}
		validateOptions(&r, path, p.Options)
	}

	validateOverlaps(&r, c, ranges)
	validateReservations(&r, c, subnets, ranges)
	validateCapacity(&r, c, ranges)
	validateExtraConf(&r, c.ExtraConf)

	return r
}

// LargePoolWarning is the total pool capacity above which a config is worth
// remarking on. Not a limit — dnsmasq will serve it and so will we.
const LargePoolWarning = 10000

// validateCapacity remarks on a pool large enough to be a liability.
//
// dnsmasq's own 1000-lease default exists to stop a hostile client on the LAN
// inventing leases until the daemon runs out of memory, and we raise that
// ceiling to whatever the pools imply (see leaseMax). Raising it is right — a
// pool that cannot fill is worse — but a /16 handed out to an untrusted network
// is a decision worth making on purpose rather than by leaving a prefix at its
// default. A warning says so and gets out of the way.
func validateCapacity(r *Result, c Config, ranges map[string][2]netip.Addr) {
	total := 0
	for _, p := range c.Pools {
		if rng, ok := ranges[p.Network]; ok {
			total += core.RangeSize(rng[0], rng[1])
		}
	}
	if total > LargePoolWarning {
		r.warnf("pools",
			"the pools total %d addresses; dnsmasq holds a record per lease, so a range this large is "+
				"a memory cost and something for a hostile client on the network to exhaust",
			total)
	}
}

// validatePoolIPv4 checks a pool's range against the network it is served on,
// and returns the resolved range.
//
// Every rule below reads the network's *stored* subnet. That is the change this
// whole thing exists for: a range is now wrong because it contradicts the
// network it is on, not because an interface somewhere happens to hold a
// different address — a complaint the operator had no way to act on, because
// nothing in olr could change that address.
func validatePoolIPv4(r *Result, path string, p Pool, n NetworkInfo) (netip.Addr, netip.Addr, bool) {
	if p.IPv4 == nil {
		return netip.Addr{}, netip.Addr{}, false
	}
	v4 := *p.IPv4

	if !n.HasIPv4() {
		r.errorf(path+".ipv4", "%q has no IPv4 subnet, so there is nothing to hand out from; "+
			"give it one with `olr net set %s --subnet <cidr>`", n.Name, n.Name)
		return netip.Addr{}, netip.Addr{}, false
	}

	switch {
	case v4.Explicit() && !v4.Start.IsValid():
		r.errorf(path+".ipv4.start", "required when an end is given")
		return netip.Addr{}, netip.Addr{}, false
	case v4.Explicit() && !v4.End.IsValid():
		r.errorf(path+".ipv4.end", "required when a start is given")
		return netip.Addr{}, netip.Addr{}, false
	case v4.Explicit() && (!v4.Start.Is4() || !v4.End.Is4()):
		// An IPv6 literal here is almost always somebody looking for the ipv6
		// block, so the message points at it rather than just refusing.
		r.errorf(path+".ipv4", "an IPv4 range takes IPv4 addresses; configure IPv6 under ipv6.mode")
		return netip.Addr{}, netip.Addr{}, false
	case v4.Explicit() && v4.Start.Compare(v4.End) > 0:
		r.errorf(path+".ipv4", "start %s is above end %s", v4.Start, v4.End)
		return netip.Addr{}, netip.Addr{}, false
	}

	start, end, ok := p.Range(n)
	if !ok {
		r.errorf(path+".ipv4", "%s has no addresses to hand out", n.Subnet)
		return netip.Addr{}, netip.Addr{}, false
	}

	prefix := n.Subnet
	if !prefix.Contains(start) {
		r.errorf(path+".ipv4.start", "%s is outside %s, the subnet of network %q", start, prefix, n.Name)
		return netip.Addr{}, netip.Addr{}, false
	}
	if !prefix.Contains(end) {
		r.errorf(path+".ipv4.end", "%s is outside %s, the subnet of network %q", end, prefix, n.Name)
		return netip.Addr{}, netip.Addr{}, false
	}

	// The three addresses that cannot be handed to a client.
	if core.InRange(start, end, n.Router) {
		r.errorf(path+".ipv4", "the range contains %s, which is this router's own address on %q",
			n.Router, n.Name)
	}
	if network := prefix.Masked().Addr(); core.InRange(start, end, network) {
		r.errorf(path+".ipv4", "the range contains the network address %s", network)
	}
	if bcast, ok := core.Broadcast(prefix); ok && core.InRange(start, end, bcast) {
		r.errorf(path+".ipv4", "the range contains the broadcast address %s", bcast)
	}

	if p.Gateway != nil {
		switch {
		case !p.Gateway.IsValid():
			r.errorf(path+".gateway", "invalid address")
		case !prefix.Contains(*p.Gateway):
			r.errorf(path+".gateway", "%s is outside %s, so clients could not reach it", *p.Gateway, prefix)
		}
	}
	for j, dns := range p.DNS {
		if !dns.IsValid() {
			r.errorf(fmt.Sprintf("%s.dns[%d]", path, j), "invalid address")
		}
	}
	for j, ntp := range p.NTP {
		if !ntp.IsValid() {
			r.errorf(fmt.Sprintf("%s.ntp[%d]", path, j), "invalid address")
		}
	}

	return start, end, true
}

// validatePoolIPv6 checks the IPv6 half, which is a mode and nothing else.
//
// There is no range to check because there is no range: dnsmasq's
// `constructor:` derives the prefix from the member interface, so what would be
// validated here is a fact about the uplink's delegation that neither module
// stores.
func validatePoolIPv6(r *Result, path string, p Pool, n NetworkInfo) {
	if p.IPv6 == nil {
		return
	}
	if !p.IPv6.Mode.Valid() {
		r.errorf(path+".ipv6.mode", "unknown mode %q (want %v)", p.IPv6.Mode, RAModes())
		return
	}
	if p.IPv6.Mode.OrDefault() == RAStateful {
		// design.md §4.3: Android has never implemented DHCPv6 and Google closed
		// the request as "Won't Fix (Intended Behavior)". A network relying on
		// stateful DHCPv6 silently loses every Android client on it, which for
		// this audience is most of the handsets.
		r.warnf(path+".ipv6.mode",
			"stateful DHCPv6 hands out addresses that Android clients will not ask for — "+
				"they get no DHCPv6 address at all. They still work via the advertised prefix, "+
				"but anything depending on a DHCPv6 lease will not see them")
	}
	if m := p.IPv6.MTU; m != 0 {
		switch {
		case p.IPv6.Mode.OrDefault() == RAOff:
			r.errorf(path+".ipv6.mtu", "an MTU is advertised in router advertisements, and this "+
				"network sends none; set ipv6.mode, or drop the MTU")
		case m < 1280 || m > 65535:
			// 1280 is IPv6's floor (RFC 8200 §5); dnsmasq refuses less.
			r.errorf(path+".ipv6.mtu", "%d is outside 1280–65535", m)
		}
	}
	if len(n.Members) == 0 {
		r.warnf(path+".ipv6", "%q has no interface, so nothing can be advertised on it", n.Name)
	}
}

// validateOverlaps rejects two pools handing out the same address. Ranges on
// different networks cannot legitimately overlap either — if they do, the
// subnets themselves collide, and `link` refuses that separately.
func validateOverlaps(r *Result, c Config, ranges map[string][2]netip.Addr) {
	for i := range c.Pools {
		a, aok := ranges[c.Pools[i].Network]
		if !aok {
			continue
		}
		for j := i + 1; j < len(c.Pools); j++ {
			b, bok := ranges[c.Pools[j].Network]
			if !bok {
				continue
			}
			if a[0].Compare(b[1]) <= 0 && b[0].Compare(a[1]) <= 0 {
				r.errorf(fmt.Sprintf("pools[%d]", j),
					"range %s-%s overlaps pools[%d] (%s) which serves %s-%s",
					b[0], b[1], i, c.Pools[i].Network, a[0], a[1])
			}
		}
	}
}

func validateReservations(r *Result, c Config, subnets map[string]netip.Prefix, ranges map[string][2]netip.Addr) {
	seenMAC := map[string]int{}
	seenIP := map[netip.Addr]int{}

	for i, res := range c.Reservations {
		path := fmt.Sprintf("reservations[%d]", i)

		mac, err := NormalizeMAC(res.MAC)
		if err != nil {
			r.errorf(path+".mac", "%v", err)
		} else if first, dup := seenMAC[mac]; dup {
			r.errorf(path+".mac", "%s is already reserved at reservations[%d]", mac, first)
		} else {
			seenMAC[mac] = i
		}

		if !res.IP.IsValid() {
			r.errorf(path+".ip", "required")
		} else if !res.IP.Is4() {
			r.errorf(path+".ip", "reservations are IPv4; IPv6 clients are addressed by the ipv6 block")
		} else {
			if first, dup := seenIP[res.IP]; dup {
				r.errorf(path+".ip", "%s is already reserved at reservations[%d]", res.IP, first)
			} else {
				seenIP[res.IP] = i
			}
			validateReservationSubnet(r, path, res, subnets, ranges)
		}

		if res.Hostname != "" {
			if err := validateHostname(res.Hostname); err != nil {
				r.errorf(path+".hostname", "%v", err)
			}
		}
		if lt := res.LeaseTime; lt != 0 && lt < MinLeaseTime {
			r.errorf(path+".lease_time", "%s is below dnsmasq's two minute minimum", lt)
		}
	}
}

// validateReservationSubnet enforces dnsmasq's own rule — a dhcp-host address
// must share a subnet with some dhcp-range, though it need not be inside the
// range itself.
func validateReservationSubnet(r *Result, path string, res Reservation, subnets map[string]netip.Prefix, ranges map[string][2]netip.Addr) {
	for network, prefix := range subnets {
		if !prefix.Contains(res.IP) {
			continue
		}
		if rng, ok := ranges[network]; ok && core.InRange(rng[0], rng[1], res.IP) {
			// Permitted by dnsmasq, and it does honour the reservation. But the
			// address is also in the pool it hands out from, so the margin for
			// error is one dnsmasq bug wide. Say so; do not refuse.
			r.warnf(path+".ip",
				"%s is inside %s's dynamic range (%s-%s); reserving an address outside the range removes any chance of a collision",
				res.IP, network, rng[0], rng[1])
		}
		return
	}
	r.errorf(path+".ip", "%s is not in the subnet of any configured pool, so dnsmasq would never offer it", res.IP)
}

func validateOptions(r *Result, path string, options []Option) {
	for j, o := range options {
		p := fmt.Sprintf("%s.options[%d]", path, j)
		name := strings.TrimSpace(strings.ToLower(o.Option))
		switch {
		case name == "":
			r.errorf(p+".option", "required")
		case strings.ContainsAny(name, ",="):
			r.errorf(p+".option", "%q may not contain a comma or an equals sign", o.Option)
		default:
			if field, owned := renderedOptions[strings.TrimPrefix(name, "option:")]; owned {
				r.errorf(p+".option", "option %q is rendered from the pool's %s field; set that instead", o.Option, field)
			}
		}
		if o.Value == "" {
			r.errorf(p+".value", "required")
		}
		if strings.ContainsAny(o.Value, "\n\r") {
			r.errorf(p+".value", "may not contain a newline")
		}
	}
}

func validateExtraConf(r *Result, extra string) {
	if extra == "" {
		return
	}
	for i, line := range strings.Split(extra, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		directive := strings.ToLower(strings.TrimSpace(trimmed))
		if eq := strings.IndexByte(directive, '='); eq >= 0 {
			directive = strings.TrimSpace(directive[:eq])
		}
		directive = strings.TrimPrefix(directive, "--")
		if why, owned := ownedDirectives[directive]; owned {
			r.errorf(fmt.Sprintf("extra_dnsmasq_conf line %d", i+1),
				"%q is set by the dhcp module: %s", directive, why)
		}
	}
}

// validateHostname applies the RFC 1123 label rules dnsmasq will apply anyway,
// so the failure surfaces here with a field path rather than in the daemon's
// startup log.
func validateHostname(h string) error {
	if len(h) > 63 {
		return fmt.Errorf("%q is longer than 63 characters", h)
	}
	if strings.HasPrefix(h, "-") || strings.HasSuffix(h, "-") {
		return fmt.Errorf("%q may not start or end with a hyphen", h)
	}
	for _, c := range h {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '-':
		default:
			return fmt.Errorf("%q contains %q; hostnames may use only letters, digits and hyphens", h, string(c))
		}
	}
	return nil
}

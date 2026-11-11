package gateway

import (
	"fmt"
	"net/netip"
	"sort"
	"strings"

	"github.com/open-linux-router/open-linux-router/internal/socksout"
)

// Rendering the kernel state a config asks for.
//
// This file is pure, and that is the load-bearing property rather than a
// stylistic one. `Render` produces a **value**, not a side effect: the planner
// diffs that value against what the kernel actually has, the differ prints it,
// the tests assert on it, and only kernel_linux.go turns it into netlink calls.
// So the whole decision layer — which prefix gets which mark, which table holds
// which route, what happens to IPv6 — is testable on a laptop with no
// privileges, which is what design.md §10 asks of every module.
//
// The rules being rendered are docs/gateway.md §3.3. The distinction that
// decides the shape is §3.1's: **nftables classifies, the RPDB routes.** There
// is no nft expression that picks a next hop — `dup` copies, `fwd` is L2-only,
// and DNAT would rewrite the destination and break the connection — so an exit
// costs one nft rule, one `ip rule`, and one route, created once and then left
// alone.

// Desired is the kernel state a config asks for, as values.
//
// Nothing here is text. Lines() renders it for the differ and for `olr diff`;
// the kernel layer walks the same fields to program them. One source, two
// readers, so a rule that is displayed is the rule that is installed.
type Desired struct {
	// Enabled is false when the module is switched off. Everything below is
	// then empty, and applying means tearing our state down and leaving the box
	// routing exactly as it did before olr was installed.
	Enabled bool

	// Table is the nftables table we own.
	Table NFTable

	// Rules are RPDB entries, in our documented priority range.
	Rules []RuleSpec

	// Routes are the per-exit default routes, in our documented table range.
	Routes []RouteSpec

	// Sysctls are the kernel settings this module owns: IP forwarding (§3.8)
	// and the per-interface redirect settings (§5.2).
	Sysctls []SysctlSpec

	// Stat is the accounting table (§7.1), and it is deliberately outside
	// Enabled: it is installed whether or not any routing policy is. With no
	// policy every packet carries mark 0 and the same two rules degrade to
	// plain per-client totals.
	Stat StatTable
}

// StatTable is `inet olr_stat` — per-client *and* per-exit bytes from one
// structure, in three base chains (§7.1).
//
// The concatenated key is what makes per-client and per-exit one measurement:
// the mark is already on the packet from the classify chain, so keying on
// `address . mark` costs nothing over keying on the address alone and answers
// "Living Room TV: 40 GB, of which 38 via Proxy" without a second table.
//
// Three chains rather than one because forwarding is not the only way bytes
// reach a device. A name published by ingress, the resolver on port 53, the web
// UI: the box is the *end* of those conversations, not a way through them, so
// not one of their packets ever reaches the forward hook. The first report of
// this table showing 0 B/s came from exactly that — a video streamed at
// 12 MB/s from an ingress name, the topology drawing the stream, and the device
// page insisting the device was idle, because caddy on the box was the other
// end of the connection. The join on `address . mark` is unchanged; the local
// chains only move where the counting happens.
type StatTable struct {
	Enabled bool

	// Chains are the base chains, in a stable order.
	Chains []StatChain
}

// StatChain is one base chain of the accounting table: the hook it counts on
// and the rules it carries.
//
// The forward chain counts both halves of a conversation that crosses the box,
// so it carries the DNAT pair as well (§7.1 — a port forward is attributed to
// the local endpoint it was translated to). The local chains count the one half
// a client has with the box itself, and only the ordinary rule exists there: a
// packet addressed to a local socket was never forwarded, and a translated
// connection's *remote* end is this box, whose own address is no device's.
type StatChain struct {
	// Name is the chain's name in the table.
	Name string

	// Hook is the nft hook keyword: forward, input or output.
	Hook string

	// Rules are the rules, in evaluation order.
	Rules []StatRule
}

// Line is the chain's canonical form, the same one the plan diffs.
func (c StatChain) Line() string {
	return fmt.Sprintf("nft chain %s %s filter", c.Name, c.Hook)
}

// StatRule is one accounting rule: the set it feeds, and which side of the
// conversation it keys on.
type StatRule struct {
	Set StatSet

	// DNAT says this rule takes the connections conntrack has translated. It is
	// one half of a pair: exactly one of the two matches any packet.
	DNAT bool
}

// Line is the rule's canonical form, which is also the comment it carries in
// the kernel, so a rule that changed shape reads back differently (§7.5).
func (r StatRule) Line() string { return r.Set.ruleLine(r.DNAT) }

// StatSetSize bounds each accounting set.
//
// A dynamic set on a router keyed by address is a memory-growth vector, so it
// is capped. When a set is full the `update` fails and the packet is still
// forwarded — the chain's policy is accept — so the failure costs accounting
// for new devices and never connectivity.
//
// The number is what it is because of what the key can now be: one element per
// (device, exit), which is a few hundred in a large house. It was 65536 when
// the sets could also accumulate one element per *remote* address, and that
// ceiling was doing real work — see StatSet.Down for why it no longer has to.
// Traffic reports how full the set is rather than leaving the cap to be
// discovered.
const StatSetSize = 8192

// StatSet is one direction of one family. Ordinary connections are keyed on
// their opener; DNATed connections are keyed on the translated local endpoint.
type StatSet struct {
	Name string
	V6   bool
	Down bool
}

// StatChains is the fixed shape of the accounting table. A function rather than
// a var so a caller cannot mutate the list everything else derives from.
//
// The local chains deliberately reuse the four sets rather than bringing their
// own. A device's upload is its upload whether the bytes left for the internet
// or ended in this box, and splitting it would leave every reader summing two
// rows per device — the same reason the mark is part of the key rather than a
// second table.
func StatChains() []StatChain {
	up4 := StatSet{Name: StatUp4, V6: false, Down: false}
	down4 := StatSet{Name: StatDown4, V6: false, Down: true}
	up6 := StatSet{Name: StatUp6, V6: true, Down: false}
	down6 := StatSet{Name: StatDown6, V6: true, Down: true}

	return []StatChain{
		{Name: AccountChain, Hook: "forward", Rules: []StatRule{
			{Set: up4}, {Set: up4, DNAT: true},
			{Set: down4}, {Set: down4, DNAT: true},
			{Set: up6}, {Set: up6, DNAT: true},
			{Set: down6}, {Set: down6, DNAT: true},
		}},
		// Input is what a device sent here; output is what it was sent back.
		// The two hooks are split this way round rather than each holding both
		// directions because the opposite halves belong to this box's own
		// conversations — a proxy dialling the NAS on a client's behalf — and
		// counting those would double every stream the box relays and hang it
		// all on an address that is nobody's device.
		{Name: LocalInChain, Hook: "input", Rules: []StatRule{{Set: up4}, {Set: up6}}},
		{Name: LocalOutChain, Hook: "output", Rules: []StatRule{{Set: down4}, {Set: down6}}},
	}
}

// StatSets is the fixed set of four, derived from the chains so that a set
// cannot be handed to a chain and left out of the readers.
func StatSets() []StatSet {
	var out []StatSet
	seen := map[string]bool{}
	for _, c := range StatChains() {
		for _, r := range c.Rules {
			if seen[r.Set.Name] {
				continue
			}
			seen[r.Set.Name] = true
			out = append(out, r.Set)
		}
	}
	return out
}

// KeyBytes is the width of one element's key: the address, then the mark, each
// padded to the 4-byte register the kernel concatenates in.
func (s StatSet) KeyBytes() int {
	if s.V6 {
		return 16 + 4
	}
	return 4 + 4
}

// Line is the ordinary accounting rule's canonical form. Its status guard
// also makes an old table visibly drift so an upgrade rebuilds it.
func (s StatSet) Line() string { return s.ruleLine(false) }

// DNATLine records the translated endpoint rather than the remote opener.
func (s StatSet) DNATLine() string { return s.ruleLine(true) }

func (s StatSet) ruleLine(dnat bool) string {
	key, addr, direction, status := "ipv4_addr", "ip saddr", "original", "not dnat"
	if s.Down {
		addr, direction = "ip daddr", "reply"
	}
	if dnat {
		status = "dnat"
		if s.Down {
			addr, direction = "ip daddr", "original"
		} else {
			addr, direction = "ip saddr", "reply"
		}
	}
	if s.V6 {
		key = "ipv6_addr"
		addr = "ip6 " + strings.TrimPrefix(addr, "ip ")
	}
	return fmt.Sprintf("nft stat set %s %s . mark from %s where ct direction %s status %s", s.Name, key, addr, direction, status)
}

// NFTable is the `inet olr_route` table (§3.3).
//
// The classify chain has six kinds of rule and they run in this order:
//
//	restore    an established flow gets back the exit it started on
//	destination explicit destination prefixes, longest prefix first
//	device     a MAC override takes precedence over its network
//	source     a new flow is classified by where it came from
//	account    every packet is counted against its exit, and the decision saved
//	unpoliced  whatever matched nothing is counted too (§7.3)
//
// The restore/account pair is §3.4's `ct mark` save and restore, and it buys
// three things: an in-flight session survives a policy edit, per-flow
// classification replaces a per-packet set lookup, and editing an assignment
// stays `reload` rather than `disruptive`.
//
// Both are rendered **per exit** rather than as one generic pair, and that is
// forced rather than chosen: nftables' bitwise expression combines a register
// with a *constant* mask and xor, not with another register, so
// "set my byte of the mark to whatever the connection's byte says" is not one
// expression. Splitting it per exit makes the value a constant again. It costs
// two rules per exit in use, which is nothing, and it keeps the mask discipline
// §3.2 demands — we never write a bit outside 0x00ff0000.
type NFTable struct {
	// Destinations precede device and source assignments, longest prefix first.
	Destinations []DestinationRule
	// Exits are the in-use exits, for the restore and accounting rules.
	Exits []ExitRule
	// Clients precede network source rules; the explicit-direct mark prevents
	// the network classifier from assigning a different exit.
	Clients []ClientRule

	// Sources are the classify rules: one per (assignment, family), matching a
	// source prefix and setting the exit's mark.
	Sources []SourceRule

	// SNAT are the postrouting rules of §5.3, one per next-hop exit that wants
	// its source addresses rewritten.
	SNAT []SNATRule

	// Counters are the named counter objects to create, sorted.
	//
	// Named, not anonymous inline ones, and §3.3 is emphatic about why:
	// re-rendering a chain zeroes anonymous counters, so editing one rule would
	// silently reset every other rule's numbers. Named objects survive rule
	// replacement.
	Counters []string
}

// ClientRule classifies an Ethernet source by its stable MAC, independent of
// its current DHCP address. Exit is empty for an explicit direct override.
type ClientRule struct {
	MAC       string
	Exit      string
	Mark      uint32
	Interface string
	Prefix    netip.Prefix
}

func (r ClientRule) Line() string {
	if r.Exit == "" {
		return fmt.Sprintf("nft device %s on %s source %s direct mark %#08x unless dnat",
			r.MAC, r.Interface, r.Prefix, r.Mark)
	}
	return fmt.Sprintf("nft device %s on %s source %s mark %#08x via %s unless dnat",
		r.MAC, r.Interface, r.Prefix, r.Mark, r.Exit)
}

// ExitRule is the restore-and-account pair for one exit in use.
type ExitRule struct {
	Exit    string
	Mark    uint32
	Counter string
}

// RestoreLine and AccountLine are the two rules' canonical forms, each stored
// in its own rule's userdata.
func (e ExitRule) RestoreLine() string {
	return fmt.Sprintf("nft restore mark %#08x for %s", e.Mark, e.Exit)
}

func (e ExitRule) AccountLine() string {
	return fmt.Sprintf("nft account mark %#08x counter %s for %s", e.Mark, e.Counter, e.Exit)
}

// SourceRule marks traffic from one prefix for one exit.
type SourceRule struct {
	// Interface and Exit are the assignment this came from, carried for the
	// rule comment and for error messages. The classifier matches the prefix.
	Interface string
	Exit      string

	// Prefix is the source range matched.
	Prefix netip.Prefix

	// Mark is the exit's fwmark, already shifted into our byte.
	//
	// There is deliberately no counter here. Counting happens once per exit in
	// the accounting rule, which sees every packet of every flow; counting in
	// the classify rule would count only the first packet of each connection,
	// because the restore rule handles the rest.
	Mark uint32
}

// SNATRule rewrites the source address of traffic leaving through a next hop.
type SNATRule struct {
	Exit string
	Mark uint32

	// Dev is the interface the next hop is reached through. Matching on it as
	// well as on the mark is belt and braces, and cheap.
	Dev string
}

// RuleKind distinguishes the two RPDB entries we install.
type RuleKind string

const (
	// RuleLocal is the priority-8100 entry that keeps LAN and connected
	// traffic local. One per family, always present when enabled.
	RuleLocal RuleKind = "local"

	// RuleMark selects an exit's table by its fwmark.
	RuleMark RuleKind = "mark"
)

// RuleSpec is one RPDB entry.
type RuleSpec struct {
	Kind     RuleKind
	Priority int

	// V6 selects which of the two RPDBs this belongs to. They are separate
	// kernel structures, so every rule is stated for one family explicitly
	// rather than implied for both.
	V6 bool

	// Mark and Table apply to RuleMark.
	Mark  uint32
	Table int

	// Exit is carried for messages.
	Exit string
}

// RouteType is what a route does with a packet.
type RouteType string

const (
	// RouteVia forwards it.
	RouteVia RouteType = "via"

	// RouteUnreachable refuses it with ICMP admin-prohibited, which is §3.7's
	// whole point: blackhole drops silently and every connection hangs for
	// thirty seconds, while unreachable makes applications fail immediately.
	// Same feature, completely different experience for whoever is holding the
	// tablet.
	RouteUnreachable RouteType = "unreachable"
)

// RouteSpec is one exit's default route, in that exit's own table.
type RouteSpec struct {
	Table int
	V6    bool
	Type  RouteType

	// Gateway is the next hop, nil for an interface route.
	Gateway *netip.Addr

	// Dev is the outgoing interface, empty when the kernel resolves it.
	Dev string

	// Priority is a route metric. A persistent unreachable fallback stays
	// in place if the TUN disappears and its interface route is deleted.
	Priority int

	// Exit is carried for messages, and Why explains an unreachable route in
	// the operator's terms — "Proxy does not carry IPv6" reads very differently
	// from an unexplained refusal.
	Exit string
	Why  string
}

// SysctlSpec is one kernel setting this module owns (§3.8, §5.2).
//
// Per-interface, with exactly one exception: ForwardingSysctl, which is
// machine-wide because the concern is. design.md §3.4 says shared state is
// touched only when a module explicitly owns that concern — "does this box
// forward" is owned here and nowhere else, and renderSysctls argues it.
//
// Everything else stays on the interfaces we route through, never
// `net.ipv4.conf.all.*`, which would change behaviour on interfaces nobody
// handed us. The one place that bites is `send_redirects`, where the kernel ORs
// the `all` value with the per-interface one; when `all` is 1 the plan reports
// it rather than quietly writing it.
type SysctlSpec struct {
	Key   string
	Value string
	Why   string
}

// ForwardingSysctl is the machine-wide IPv4 forwarding switch.
//
// Named rather than spelled inline because three places have to agree on it:
// renderSysctls writes it, LinuxKernel.readSysctls reads it back so drift means
// something, and the planner reports it as the reason a box with a complete
// routing policy still forwards nothing.
const ForwardingSysctl = "net.ipv4.ip_forward"

// IPv6ForwardingSysctl is the machine-wide IPv6 forwarding switch, written only
// when Config.IPv6Forwarding says so.
//
// `all` rather than per-interface for the reason ForwardingSysctl gives, plus
// one of IPv6's own: writing `all` also sets `default`, so an interface created
// later — a tunnel, a WireGuard device — comes up in router mode too, instead of
// answering neighbour solicitations as a host and being dropped from its
// clients' router lists.
const IPv6ForwardingSysctl = "net.ipv6.conf.all.forwarding"

// Line is this rule's canonical form, and also the bytes stored in its netlink
// userdata so that reading the kernel back reproduces it exactly.
//
// Kept comfortably under the kernel's 256-byte userdata limit: an interface
// name is bounded at 15 characters, a prefix at 43, and MaxNameLen caps the
// exit. Validate rejects control characters in a name, so this is always one
// line.
//
// **`unless dnat` is part of the text on purpose.** It names §3.5's guard —
// this rule does not apply to the reply leg of a port-forwarded connection —
// and it is in the canonical form rather than only in the expression list so
// that a box still holding the older rules reads back as drift and gets them
// replaced. Without it the fix would land in the binary and never reach a
// kernel that had already been programmed.
func (s SourceRule) Line() string {
	return fmt.Sprintf("nft source %s %s mark %#08x from %s via %s unless dnat",
		family(s.Prefix.Addr().Is6()), s.Prefix, s.Mark, s.Interface, s.Exit)
}

// Line is this rule's canonical form, stored in its userdata for the same
// reason.
func (s SNATRule) Line() string {
	dev := s.Dev
	if dev == "" {
		dev = "-"
	}
	return fmt.Sprintf("nft snat mark %#08x dev %s for %s", s.Mark, dev, s.Exit)
}

// Health is what the prober currently believes about each exit, keyed by exit
// name. An exit absent from the map is treated as up.
//
// Passed *into* rendering rather than acted on separately, and that is what
// keeps design.md §5.4 true. If the prober re-pointed a dead exit by writing to
// the kernel behind the planner's back, the next drift check would compare
// intent against a kernel that disagrees with it and report drift — for a box
// that is behaving exactly as configured. Making health an input means the
// tripped state *is* the expected state, and drift keeps meaning what it says.
type Health map[string]bool

// Down reports whether the prober has marked this exit down.
func (h Health) Down(name string) bool {
	up, known := h[name]
	return known && !up
}

// Render turns intent into the kernel state that implements it.
//
// It assumes the config has been validated; an unvalidated one produces
// nonsense rather than an error, which is why every caller validates first
// (design.md §5.3.1 — the whole value of validation is that it happens before
// anything is written).
func Render(c Config, links LinkView, health Health) Desired {
	d := Desired{Enabled: c.Enabled}

	// Before the early return, because accounting outlives routing policy
	// (§7.1). A box with `enabled: false` and stats on still counts bytes; it
	// just counts them all against mark 0.
	if c.StatsOrDefault() {
		d.Stat = StatTable{Enabled: true, Chains: StatChains()}
		// Enables byte counters for new conntrack entries. Do not force this
		// shared kernel facility off: remote access also consumes its counters.
		d.Sysctls = append(d.Sysctls, SysctlSpec{Key: "net.netfilter.nf_conntrack_acct", Value: "1"})
	}

	if !c.Enabled {
		return d
	}

	// The local guard, one per family, and the most load-bearing four lines in
	// the module. Without it an exit table's default route swallows traffic
	// addressed to your own LAN, and the router stops being able to reach the
	// devices behind it (§3.3).
	d.Rules = append(d.Rules,
		RuleSpec{Kind: RuleLocal, Priority: LocalPriority, V6: false},
		RuleSpec{Kind: RuleLocal, Priority: LocalPriority, V6: true},
	)

	counters := map[string]bool{UnpolicedCounter: true}

	// Only exits something actually uses are programmed. An exit nobody routes
	// through is a saved setting, not a live route table: installing it would
	// mean a configured-but-unassigned exit still shows up in `ip rule` output,
	// which makes the kernel disagree with the screen for no gain.
	for _, e := range c.Exits {
		if !c.InUse(e.Name) {
			continue
		}
		counter := counterName(e)
		counters[counter] = true
		d.Table.Exits = append(d.Table.Exits, ExitRule{
			Exit: e.Name, Mark: e.Mark(), Counter: counter,
		})

		renderExit(&d, c, e, links, health)
	}
	if infos, err := links.Interfaces(); err == nil {
		// Stable output even if the observed interface order changes. The
		// kernel evaluates these rules in the order we render them.
		sort.Slice(infos, func(i, j int) bool { return infos[i].Name < infos[j].Name })
		for _, a := range c.Clients {
			for _, info := range infos {
				if !info.Adopted {
					continue
				}
				for _, prefix := range info.Prefixes {
					if !prefix.IsValid() {
						continue
					}
					if a.Exit == "" && !counters[DirectCounter] {
						counters[DirectCounter] = true
						d.Table.Exits = append(d.Table.Exits, ExitRule{
							Exit: "direct", Mark: DirectMark, Counter: DirectCounter,
						})
					}
					rule := ClientRule{MAC: a.MAC, Exit: a.Exit, Mark: DirectMark,
						Interface: info.Name, Prefix: prefix.Masked()}
					if e, ok := c.Find(a.Exit); ok {
						rule.Mark = e.Mark()
					}
					d.Table.Clients = append(d.Table.Clients, rule)
				}
			}
		}
	}

	renderSources(&d, c, links)
	renderDestinations(&d, c, links)

	d.Table.Counters = sortedKeys(counters)
	renderSysctls(&d, c, links)

	return d
}

// counterName is the named counter object for one exit.
//
// Keyed by slot rather than by name, so renaming an exit does not reset its
// byte count — the number an operator is watching survives them fixing a typo.
func counterName(e Exit) string { return fmt.Sprintf("exit%d", e.Slot) }

// renderExit installs one exit's RPDB rule and route, per family.
func renderExit(d *Desired, c Config, e Exit, links LinkView, health Health) {
	down := health.Down(e.Name)

	// §5.5's fail-open mode is the one case where an exit in use installs
	// nothing at all: no rule means the mark selects no table, so the traffic
	// falls through to `main` and takes the box's normal path. That it is
	// achieved by absence rather than by a route is worth knowing when reading
	// `ip rule` output on a box whose exit is down.
	if down && e.OnFailure.OrDefault() == FailDirect {
		return
	}

	for _, v6 := range []bool{false, true} {
		route, ok := exitRoute(e, links, v6, down)
		if e.Via.Kind == ViaInterface && e.Via.Interface == socksout.Interface && !v6 && !down {
			if info, err := links.Interface(socksout.Interface); err != nil || !info.Up {
				// At boot the backend may be disabled or not yet ready. Keep the
				// rule's table populated so marked packets cannot fall to main.
				route = RouteSpec{Type: RouteUnreachable}
			}
		}
		if !ok {
			// IPv6Direct: no rule, no route, so v6 takes the normal path.
			continue
		}
		route.Table = e.Table()
		route.V6 = v6
		route.Exit = e.Name

		d.Routes = append(d.Routes, route)
		if e.Via.Kind == ViaInterface && e.Via.Interface == socksout.Interface && route.Type == RouteVia {
			// This route survives removal of the TUN when its process dies.
			d.Routes = append(d.Routes, RouteSpec{Table: e.Table(), V6: v6,
				Type: RouteUnreachable, Priority: 100, Exit: e.Name})
		}
		d.Rules = append(d.Rules, RuleSpec{
			Kind:     RuleMark,
			Priority: e.Priority(),
			V6:       v6,
			Mark:     e.Mark(),
			Table:    e.Table(),
			Exit:     e.Name,
		})
	}

	if e.Via.Kind == ViaNextHop && e.SNATOrDefault() && !down {
		d.Table.SNAT = append(d.Table.SNAT, SNATRule{
			Exit: e.Name,
			Mark: e.Mark(),
			Dev:  nextHopDev(e, links),
		})
	}
}

// exitRoute decides the one route in an exit's table for one family.
//
// The three-way answer here is §5.4 made mechanical. An exit carries a family
// or it does not; when it does not, the traffic is refused, sent direct, or —
// never — quietly allowed to leak.
func exitRoute(e Exit, links LinkView, v6, down bool) (RouteSpec, bool) {
	if down {
		// The exit is up as far as configuration goes but the probe says it is
		// not forwarding. OnFailure is block (direct returned earlier), so
		// everything assigned to it stops here, visibly.
		return RouteSpec{
			Type: RouteUnreachable,
			Why:  fmt.Sprintf("%s is not responding", e.Name),
		}, true
	}

	if e.Via.Kind == ViaBlocked {
		return RouteSpec{
			Type: RouteUnreachable,
			Why:  fmt.Sprintf("%s blocks traffic", e.Name),
		}, true
	}

	if e.Carries(v6) {
		switch e.Via.Kind {
		case ViaInterface:
			return RouteSpec{Type: RouteVia, Dev: e.Via.Interface}, true
		case ViaNextHop:
			hop := *e.Via.NextHop
			return RouteSpec{Type: RouteVia, Gateway: &hop, Dev: nextHopDev(e, links)}, true
		}
	}

	// The exit does not carry this family. For IPv6 the operator's answer
	// decides; for IPv4 there is no field, because a v6-only next hop is exotic
	// enough that inventing one would be modelling for its own sake — it fails
	// closed, and Validate warns.
	if !v6 {
		return RouteSpec{
			Type: RouteUnreachable,
			Why:  fmt.Sprintf("%s carries IPv6 only", e.Name),
		}, true
	}

	switch e.IPv6OrDefault() {
	case IPv6Direct:
		return RouteSpec{}, false
	default:
		return RouteSpec{
			Type: RouteUnreachable,
			Why:  fmt.Sprintf("%s does not carry IPv6", e.Name),
		}, true
	}
}

// Carries reports whether this exit natively handles a family.
//
// An interface carries both — a tunnel or a TUN device is not v4-only just
// because nobody gave it a v6 address, and if it has none the route simply has
// nothing to carry. A next hop carries exactly the family of its own address,
// which is the honest answer: there is no such thing as reaching a v6
// destination through a v4 gateway.
func (e Exit) Carries(v6 bool) bool {
	switch e.Via.Kind {
	case ViaInterface:
		return !v6 || e.Via.Interface != socksout.Interface
	case ViaNextHop:
		if e.Via.NextHop == nil {
			return false
		}
		// Is4In6 is excluded deliberately: ::ffff:192.0.2.1 is an IPv4 address
		// wearing a v6 spelling, and treating it as a v6 next hop would install
		// a v6 default route that cannot carry anything.
		hopIsV6 := e.Via.NextHop.Is6() && !e.Via.NextHop.Is4In6()
		return hopIsV6 == v6
	case ViaBlocked:
		return true
	}
	return false
}

// nextHopDev resolves the interface a next hop is reached through, preferring
// what the operator wrote and falling back to the one whose subnet holds it.
//
// An empty answer is legal and means "let the kernel work it out from the main
// table", which is right whenever the hop is on a network we already have an
// address on — the case Validate has already insisted on.
func nextHopDev(e Exit, links LinkView) string {
	if e.Via.Dev != "" {
		return e.Via.Dev
	}
	if e.Via.NextHop == nil {
		return ""
	}
	if info, ok := InterfaceWithPrefixContaining(links, *e.Via.NextHop); ok {
		return info.Name
	}
	return ""
}

// renderSources builds the classify chain: one rule per assigned network per
// family, matching the network's own prefixes.
//
// The prefixes come from link rather than from our own config, which is what
// makes it impossible for the range we classify on to drift away from the
// range that is actually configured on the interface (design.md §4.1).
func renderSources(d *Desired, c Config, links LinkView) {
	for _, a := range c.Interfaces {
		exitName, _ := c.Assigned(a.Interface)
		if exitName == "" {
			// Inheriting a box default of "the box's own route". Nothing to
			// mark: unmarked traffic uses `main`, which is precisely what this
			// asks for, and a rule that set mark 0 would be a no-op that
			// somebody would later have to explain.
			continue
		}
		e, ok := c.Find(exitName)
		if !ok {
			continue // Validate has already reported this.
		}
		info, err := links.Interface(a.Interface)
		if err != nil {
			continue
		}

		for _, v6 := range []bool{false, true} {
			for _, p := range info.PrefixesFor(v6) {
				d.Table.Sources = append(d.Table.Sources, SourceRule{
					Interface: a.Interface,
					Exit:      e.Name,
					Prefix:    p.Masked(),
					Mark:      e.Mark(),
				})
			}
		}
	}

	sort.SliceStable(d.Table.Sources, func(i, j int) bool {
		return d.Table.Sources[i].Prefix.String() < d.Table.Sources[j].Prefix.String()
	})
}

// renderSysctls is §3.8 and §5.2: the switch that makes this box a router at
// all, and the two redirect settings that keep traffic coming through it.
//
// **IP forwarding first, because nothing else in this module means anything
// without it.** Every rule, route and SNAT entry here describes what happens to
// a packet addressed to somewhere else, and with `net.ipv4.ip_forward` at 0 the
// kernel drops that packet before any of it is consulted. A module that
// programs the whole forwarding path and then leaves the box not forwarding is
// not being a good citizen, it is being broken — so this is written, and §3.8
// records the argument in full.
//
// It is the one machine-wide key this module writes, against design.md §3.4's
// *don't squat shared state*. That rule's own wording is what permits it:
// sysctls are touched "only when a module explicitly owns that concern", and
// the concern here is whether this box forwards. Per-interface
// `conf.<dev>.forwarding` would be the narrower write and IPv4 honours it — the
// forwarding decision reads the *ingress* device's flag — but it needs setting
// on both the arrival and the departure interface to pass a packet and its
// reply, and any third party writing the global key resets every per-client
// value underneath us. Narrower, and less likely to still be true in an hour.
// §3.8 takes the plain one while the module is young.
//
// IPv6 is written only when the operator says so (Config.IPv6Forwarding).
// `net.ipv6.conf.all.forwarding` switches every interface out of host mode, and
// an interface in router mode stops accepting the RAs this box may be getting
// its own address and default route from — so unlike IPv4 it is not free, and
// it is not inferred. The plan names the interfaces it would cost
// (Observed.RAHosts); setting their accept_ra to 2 is not this module's write.
//
// Then §5.2, in both directions. When the exit's next hop shares a segment with
// the clients — the normal case, and the one a WAN gateway never produces — two
// things happen that have to be turned off explicitly:
//
//   - We forward a packet back out the interface it arrived on, and the kernel
//     helpfully tells the client to talk to the proxy box directly. Some clients
//     obey, and then their traffic stops passing through us at all.
//   - When the proxy box's own rules say DIRECT it forwards back out the same
//     interface and sends *us* a redirect, and our table quietly acquires routes
//     we did not choose.
func renderSysctls(d *Desired, c Config, links LinkView) {
	// Unconditional within an enabled module, and not tied to an exit existing.
	// The configuration that needs this most is the one with no exits at all:
	// `default` unset means "everything uses this box's own connection", which
	// is precisely a box that has to forward. Keying it on an exit would leave
	// the commonest working setup as the one that does not work.
	d.Sysctls = append(d.Sysctls, SysctlSpec{
		Key:   ForwardingSysctl,
		Value: "1",
		Why: "without it the kernel drops every packet addressed to somewhere " +
			"else, so nothing behind this router reaches anything",
	})

	if v := c.IPv6Forwarding; v != nil {
		spec := SysctlSpec{
			Key:   IPv6ForwardingSysctl,
			Value: "0",
			Why:   "IPv6 forwarding was turned off",
		}
		if *v {
			spec.Value = "1"
			spec.Why = "lets IPv6 pass through this router between its networks and the internet"
		}
		d.Sysctls = append(d.Sysctls, spec)
	}

	devs := map[string]bool{}
	for _, e := range c.Exits {
		if e.Via.Kind != ViaNextHop || !c.InUse(e.Name) {
			continue
		}
		if dev := nextHopDev(e, links); dev != "" {
			devs[dev] = true
		}
	}

	for _, dev := range sortedKeys(devs) {
		d.Sysctls = append(d.Sysctls,
			SysctlSpec{
				Key:   fmt.Sprintf("net.ipv4.conf.%s.send_redirects", dev),
				Value: "0",
				Why: "stops the kernel telling clients on " + dev +
					" to bypass this router and talk to the next hop directly",
			},
			SysctlSpec{
				Key:   fmt.Sprintf("net.ipv4.conf.%s.accept_redirects", dev),
				Value: "0",
				Why: "stops the next hop adding routes to this box's table that " +
					"nobody configured",
			},
			SysctlSpec{
				Key:   fmt.Sprintf("net.ipv6.conf.%s.accept_redirects", dev),
				Value: "0",
				Why:   "the same, for IPv6",
			},
		)
	}
}

// --- canonical text -------------------------------------------------------

// Lines renders the desired state as canonical text.
//
// One representation does three jobs, and that is deliberate rather than
// thrifty. It is the diff basis, so `olr diff` shows a routing change as lines
// rather than as "3 things will change". It is what the UI prints. And each nft
// line is stored verbatim in that rule's netlink userdata, so reading the
// kernel back yields exactly these strings and drift is a string comparison —
// no re-deriving a rule from its expression list, and no second format that
// could disagree with the first.
//
// That last job is why these are compact semantic lines rather than nft syntax.
// Rendering `ip saddr … meta mark set meta mark and 0xff00ffff or 0x00020000`
// here would mean writing the ruleset twice, once for humans and once for the
// kernel, with nothing keeping the two honest — and it would not fit in the 256
// bytes the kernel allows for userdata. The nft syntax lives in exactly one
// place, kernel_linux.go, where it is executed rather than described.
func (d Desired) Lines() []string {
	out := d.objectLines()
	for _, s := range d.Sysctls {
		out = append(out, fmt.Sprintf("sysctl %s = %s", s.Key, s.Value))
	}
	return out
}

// objectLines is the subset the planner compares against the kernel: the things
// we create and would delete.
//
// Sysctls are excluded and handled separately, because they are the one thing
// here we edit rather than own. Every other line describes an object that
// exists because we made it, so its absence from the desired set means "remove
// it"; a sysctl somebody else had already set to 0 is not ours to put back.
func (d Desired) objectLines() []string {
	var out []string

	// First, and outside the Enabled guard below, because the accounting table
	// has its own lifetime (§7.1).
	if d.Stat.Enabled {
		out = append(out, fmt.Sprintf("nft table inet %s", StatTableName))
		for _, c := range d.Stat.Chains {
			out = append(out, c.Line())
			for _, r := range c.Rules {
				out = append(out, r.Line())
			}
		}
	}

	if !d.Enabled {
		return out
	}

	out = append(out, fmt.Sprintf("nft table inet %s", TableName))
	out = append(out, fmt.Sprintf("nft chain %s prerouting mangle", ClassifyChain))
	for _, name := range d.Table.Counters {
		out = append(out, fmt.Sprintf("nft counter %s", name))
	}

	// In evaluation order, which is also the order they are installed, because
	// a reader comparing this against `nft list ruleset` should be able to
	// follow a packet down the list.
	for _, e := range d.Table.Exits {
		out = append(out, e.RestoreLine())
	}
	for _, r := range d.Table.Destinations {
		out = append(out, r.Line())
	}
	for _, r := range d.Table.Clients {
		out = append(out, r.Line())
	}
	for _, s := range d.Table.Sources {
		out = append(out, s.Line())
	}
	for _, e := range d.Table.Exits {
		out = append(out, e.AccountLine())
	}
	out = append(out, "nft count-unpoliced")

	if len(d.Table.SNAT) > 0 {
		out = append(out, fmt.Sprintf("nft chain %s postrouting srcnat", PostroutingChain))
		for _, s := range d.Table.SNAT {
			out = append(out, s.Line())
		}
	}

	for _, r := range d.Rules {
		out = append(out, r.Line())
	}
	for _, r := range d.Routes {
		out = append(out, r.Line())
	}

	return out
}

// Line is this RPDB entry's canonical form.
//
// A method rather than a local format string because the kernel layer keys its
// add/remove sets on exactly this text: if the two spellings drifted apart,
// every apply would remove a rule and immediately add back an identical one,
// and every plan would report work that had already been done.
func (r RuleSpec) Line() string {
	if r.Kind == RuleLocal {
		return fmt.Sprintf("rule %s priority %d from all lookup main suppress_prefixlength 0",
			family(r.V6), r.Priority)
	}
	return fmt.Sprintf("rule %s priority %d fwmark %#08x/%#08x lookup %d",
		family(r.V6), r.Priority, r.Mark, MarkMask, r.Table)
}

// Line is this route's canonical form, for the same reason.
func (r RouteSpec) Line() string {
	if r.Type == RouteUnreachable {
		line := fmt.Sprintf("route %s table %d unreachable default", family(r.V6), r.Table)
		if r.Priority > 0 {
			line += fmt.Sprintf(" metric %d", r.Priority)
		}
		return line
	}
	line := fmt.Sprintf("route %s table %d default", family(r.V6), r.Table)
	if r.Gateway != nil {
		line += " via " + r.Gateway.String()
	}
	if r.Dev != "" {
		line += " dev " + r.Dev
	}
	if r.Priority > 0 {
		line += fmt.Sprintf(" metric %d", r.Priority)
	}
	return line
}

// Text renders Lines as a document, which is what the differ compares.
func (d Desired) Text() string {
	lines := d.Lines()
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n") + "\n"
}

func family(v6 bool) string {
	if v6 {
		return "ip6"
	}
	return "ip"
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

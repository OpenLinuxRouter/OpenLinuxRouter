// Package gateway owns the boundary between the networks this box serves and
// everything else, in both directions (docs/gateway.md §0).
//
// Three concerns, three nftables tables, three lifetimes:
//
//   - **Exits, and which network uses which** — `olr_route`, this package.
//   - **NAT** — `olr_nat`, internal/gateway/nat: port forwards inward, one
//     egress masquerade outward. It arrived here from a module called
//     `firewall` that did no filtering; docs/port-forwarding.md §0 has that
//     argument, and the nat package's own comment has why it is a package
//     rather than a flat merge.
//   - **Byte accounting** — `olr_stat`, and the one concern Enabled does not
//     govern (§7.1).
//
// The object this package itself owns is an **exit**: anything that accepts
// traffic addressed somewhere else and takes responsibility for delivering it
// (docs/gateway.md §1.1). That is a membership test rather than a label, and it is what settles
// the question the naming kept failing at — a WireGuard interface, a proxy box
// on the LAN and `unreachable` are all exits, while a SOCKS5 port is not,
// because the client has to *ask* in its own protocol.
//
// The operator never meets the word. They meet one sentence with a preposition
// doing the work — *Internet via [ Proxy ]* — attached to the thing they are
// looking at, inherited most-specific-wins (§2). An ordered first-match rule
// list is what the kernel gets; it is deliberately not what the screen shows,
// because answering "where does my phone actually go?" against one requires
// simulating evaluation.
package gateway

import (
	"encoding/json"
	"fmt"
	"net/netip"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/core"
	"github.com/open-linux-router/open-linux-router/internal/gateway/nat"
)

// ModuleName is the path segment, config section and event label for this
// module.
const ModuleName = "gateway"

// Config is the gateway module's intent — what the operator asked for, not what
// is running. It is the single source for the CLI flags, the REST body, the UI
// form, and the MCP tool definition (design.md §3.2 rule 3), so a field added
// here appears on every surface without further work.
//
// Fields without `omitempty` are reflected as schema-required (design.md §10,
// config format), so the tags are load-bearing.
type Config struct {
	// Legacy records that a stored document still called its per-client
	// overrides `devices`, the word this module used before the two inventories
	// were told apart. Never serialised, so one save writes the current shape.
	Legacy bool `json:"-"`

	LatencyTarget string              `json:"latency_target,omitempty"`
	LatencySites  []CustomLatencySite `json:"latency_sites"`
	// LatencyCommon holds only the operator's exit overrides for the catalog
	// sites; the sites themselves are always present. Keeping them out of
	// latency_sites is what makes "cannot be deleted" true rather than a UI rule.
	LatencyCommon []CommonLatency `json:"latency_common,omitempty"`
	// LatencyPins selects monitored sites for the homepage, independently of
	// probe targets and exits. An absent list means no sites are pinned.
	LatencyPins []string `json:"latency_pins,omitempty"`
	// Enabled controls whether any of this is programmed into the kernel at
	// all. Disabling tears down our tables, our rules and our route tables and
	// leaves the box routing and translating exactly as it did before olr was
	// installed — which is also what makes the module safe to try (design.md
	// §7).
	//
	// **It governs `olr_nat` as well as `olr_route`**, and that is the sharpest
	// edge on this config: turning the module off stops policy routing *and
	// closes every forwarded port*, in one request. docs/gateway.md §0 states
	// it, the plan says it in those words, and the impact is disruptive —
	// a switch that silently shuts a port somebody is reaching a service
	// through is the one thing this must not do quietly.
	//
	// Stats is the single exception, for the reason §7.1 gives: a different
	// table with a different lifetime. Nothing else gets its own lifecycle
	// switch; a concern's condition is its own configuration existing.
	//
	// Keeping the configuration while off means it can be turned back on
	// without retyping it, matching internal/dhcp and internal/dns.
	Enabled bool `json:"enabled"`

	// Exits are the ways out. Empty means everything uses the box's own default
	// route, which is what an unconfigured box already does.
	Exits []Exit `json:"exits,omitempty"`

	// Forwards are the port forwards — what arrives from outside, and where it
	// goes (internal/gateway/nat, docs/port-forwarding.md).
	//
	// A field of this config rather than a section of its own, because the
	// module that used to own them was deleted. The stored shape is the nat
	// package's Forward; the projection into what that package works on is
	// Config.NAT.
	Forwards []nat.Forward `json:"forwards,omitempty"`

	// SNAT rewrites the source address of traffic from this box's networks as
	// it leaves `dial`'s uplink. Nil means on.
	//
	// The same word and the same pointer reasoning as Exit.SNAT, because it is
	// the same question asked about the default path instead of about an exit:
	// a bool could not tell "the operator turned it off" from "the field was
	// never written", which is what decides whether a document stored by an
	// older olr keeps behaving the way it did.
	//
	// It is **not** a second Enabled. docs/gateway.md §3.9 has the argument;
	// the short form is that turning it off is a real configuration — an
	// operator who added a static route for their LAN on the modem wants the
	// client's own address to survive the trip — rather than a lifecycle
	// switch. Without an uplink it writes nothing either way.
	SNAT *EgressSNAT `json:"snat,omitempty"`

	// Default names the exit that everything uses unless something more
	// specific says otherwise — the top rung of §2.1's ladder.
	//
	// Empty means the box's own default route, and that is deliberately not
	// spelled as a reserved exit name. §1.2 says the box's default gateway *is*
	// an exit; what it is not is an exit *this module* owns. It is whatever sits
	// in the main table, and the control over it lives one module away, in
	// `dial`: an operator who wants to choose it sets `dial`'s uplink, which
	// owns the interface, its address and the default route, and olr puts all
	// three back after a reboot. A box with no uplink configured still reaches
	// this line, and the main table is then whatever the distribution, a DHCP
	// client or somebody's hand put there.
	//
	// So an editable row here would be the wrong place rather than an
	// impossible one — the same value with a second owner, and §4.1 allows one
	// owner per fact. Empty stays empty, and the mechanism is unchanged; what
	// changed is that there is now somewhere in olr to go and set it.
	Default string `json:"default,omitempty"`

	// Stats controls per-client and per-exit byte accounting (§7.1).
	//
	// Nil means on. It is deliberately **not** governed by Enabled: §7.1 is
	// explicit that the accounting table does not depend on the routing one —
	// with no policy installed every packet carries mark 0 and the same
	// structure degrades to plain per-client totals. So a box that only wants
	// to know who is using the bandwidth gets that without routing anything.
	//
	// A switch exists because §7.5 asks for one. What it turns off is modest
	// compared to the query log — byte totals in kernel memory, no history and
	// no names — but "a visible off switch" was decided now rather than after
	// somebody asks, and this is it.
	Stats *bool `json:"stats,omitempty"`

	// IPv6Forwarding is whether this box forwards IPv6 (§3.8). Nil means olr
	// leaves net.ipv6.conf.all.forwarding exactly as the box has it.
	//
	// Its own switch rather than following ip_forward, because turning it on
	// costs something IPv4 forwarding does not: an interface in router mode
	// stops accepting router advertisements, so a box getting its own IPv6 from
	// upstream loses it. That is the operator's decision, made here — not
	// something this module infers from a network having a v6 prefix. The plan
	// names the interfaces it would cost, and fixing them (accept_ra=2) is left
	// to whoever owns them.
	//
	// A pointer so that false means "write 0" rather than "leave it": an
	// operator who turns this off expects the box to stop forwarding, and a
	// switch that only ever moved the kernel one way would show off while the
	// kernel stayed on.
	IPv6Forwarding *IPv6Forwarding `json:"ipv6_forwarding,omitempty"`

	// Interfaces assigns an exit per network — §2.5's first tier of the ladder,
	// refined by client overrides below.
	//
	// Keyed by kernel interface name rather than by a network, matching
	// internal/dhcp's pools and for the reason internal/dns states in as many
	// words: networks do not exist until the link module lands (design.md
	// milestone 1), and inventing a second spelling now would be one more thing
	// to unpick then. The name of this field changes when link does; the shape
	// does not.
	Interfaces []Assignment `json:"interfaces,omitempty"`

	// Clients override the network choice by source MAC. An empty exit
	// explicitly takes the box's normal route; absence inherits the network.
	Clients []ClientAssignment `json:"clients,omitempty"`

	// Destinations override source assignments; longest prefix wins.
	Destinations []DestinationAssignment `json:"destinations,omitempty"`
}

// EgressSNAT is Config.SNAT's type.
//
// A named bool so schema.go can describe it — core reflects a bare `bool` as
// `boolean` with nothing else to say, and the one thing worth saying about this
// field is that leaving it out means *on*.
type EgressSNAT bool

// IPv6Forwarding is Config.IPv6Forwarding's type, named for the same reason as
// EgressSNAT: what leaving it out means is the thing worth describing.
type IPv6Forwarding bool

// Assignment is one rung of the ladder: this source uses that exit.
//
// Clients now refine this field through Config.Clients; groups are later.
type Assignment struct {
	// Interface is the source network, named by its kernel interface.
	Interface string `json:"interface"`

	// Exit names an exit, or is empty to inherit Config.Default.
	//
	// Empty is a real value and not a missing one: *IoT devices go direct* is
	// the case §2.1 opens with, and the answer is that the IoT network keeps
	// the default — nothing to configure, because it is already right.
	Exit string `json:"exit,omitempty"`
}

// ClientAssignment is a client-specific override, keyed by canonical MAC.
type ClientAssignment struct {
	MAC  string `json:"mac"`
	Exit string `json:"exit,omitempty"`
}

// Exit is one way out of the box.
type Exit struct {
	// Name is what the operator calls it — "Proxy", "Modem", "Blocked". It is
	// also the reference used by Default and by every Assignment, so renaming
	// one is a rename everywhere; see Config.Rename.
	Name string `json:"name"`

	// Via is how traffic actually leaves. §1.2's forms, minus TPROXY.
	Via Via `json:"via"`

	// Slot is the only field an operator never sets: it is allocated on save
	// and preserved thereafter. It is written down rather than derived because
	// every kernel resource this module owns is computed from it — see
	// resources.go, which is also where the argument for storing it lives.
	Slot int `json:"slot"`

	// IPv6 is the exit's answer to §5.4, and there is no useful default that
	// works for every form, so Resolve fills it in per form rather than a
	// constant doing it.
	//
	// The failure it exists to prevent is the quietest one in the document: if
	// the exit carries v4 only and clients have working IPv6, everything with
	// an AAAA record goes out the default path at full speed and nothing looks
	// wrong. That is the difference between working and appearing to work.
	IPv6 IPv6Mode `json:"ipv6,omitempty"`

	// OnFailure is what happens to assigned traffic when the probe says this
	// exit is down. Empty means FailBlock.
	OnFailure FailureMode `json:"on_failure,omitempty"`

	// SNAT rewrites the source address of traffic sent to a next-hop exit, so
	// the far box replies to us rather than straight to the client over the
	// shared L2 (§5.3). Nil means on for ViaNextHop and is ignored otherwise.
	//
	// A pointer because the default is *on* and a plain bool could not tell
	// "the operator turned it off" from "the field was never written" — which
	// is exactly the distinction that decides whether a stored document from an
	// older olr keeps behaving the way it did.
	//
	// What it costs is real and worth stating on the screen: the proxy box
	// stops seeing which client a connection came from, so its own per-source
	// rules stop working. That is acceptable here only because source selection
	// is a thing we took over. An operator who wants the far box to keep doing
	// its own per-source work turns this off and gives that box a network of
	// its own — §11 open decision 2's other answer.
	SNAT *bool `json:"snat,omitempty"`

	// Probe is the through-path health check (§5.5). Nil means the exit is
	// never probed and so is never considered down.
	//
	// Not probing is a legitimate choice for `blocked` and for an exit whose
	// liveness is already visible some other way, but it is the wrong default
	// for a next hop: docs/dns.md §1.2 rests the whole "DHCP hands out olr, and
	// only olr" topology rule on olr being able to re-point a dead exit,
	// because IPv4 has no gateway failover at the device layer. Validate warns
	// when a routing exit has no probe rather than silently accepting it.
	Probe *Probe `json:"probe,omitempty"`

	// DNS chooses the resolver for clients using this exit. Nil keeps OLR's
	// local resolver; it never changes what DNS address DHCP hands to clients.
	DNS *ExitDNS `json:"dns,omitempty"`
}

// ExitDNS selects where OLR's relay forwards public names for this exit.
type ExitDNS struct {
	// Mode is olr, exit (the next hop on port 53), or custom.
	Mode string `json:"mode"`
	// Server is required only for custom mode. The address is reached by OLR.
	Server netip.AddrPort `json:"server,omitempty"`
}

// Via is how an exit delivers traffic. §1.2's forms.
//
// The doc writes these as four shapes discriminated by which key is present.
// This carries an explicit Kind instead, for three reasons that all point the
// same way: the discriminator publishes through core.Reflect as an enum a UI
// form can switch on, "exactly the fields for this kind" becomes one flat
// validation rule instead of a count of set fields, and a document that names
// its own shape survives a field being added later without the meaning of the
// old documents changing.
type Via struct {
	// Kind selects the form. See ViaKinds for the vocabulary.
	Kind ViaKind `json:"kind"`

	// Interface names the device traffic leaves through: a WireGuard or
	// Tailscale interface, a PPPoE session, a proxy's TUN. Only for
	// ViaInterface.
	//
	// There is deliberately no separate `tun` or `vpn` form (§1.2). A TUN
	// device is an instance of this, and giving it its own kind would force the
	// same question for every other interface-shaped thing until the schema had
	// a kind per vendor.
	Interface string `json:"interface,omitempty"`

	// NextHop is the address of a box that will take the traffic on — the
	// modem, or a machine on the LAN running a proxy. Only for ViaNextHop.
	NextHop *netip.Addr `json:"next_hop,omitempty"`

	// Dev is the interface the next hop is reachable through. Only for
	// ViaNextHop, and optional: empty means the kernel resolves it from the
	// main table, which is right whenever the next hop is on a network we
	// already have an address on. Validate checks that it is.
	Dev string `json:"dev,omitempty"`
}

// ViaKind names one of §1.2's forms.
type ViaKind string

const (
	// ViaInterface sends traffic out a device. wg0, tailscale0, ppp0, a proxy's
	// TUN — all the same thing to us.
	ViaInterface ViaKind = "interface"

	// ViaNextHop hands traffic to another box, which is the form the reference
	// topology in docs/dns.md §1 is built from: the modem is a next hop and so
	// is the proxy box, and neither is something a device is ever told about.
	ViaNextHop ViaKind = "next_hop"

	// ViaBlocked refuses traffic, explicitly. It is an exit by §1.1's test
	// because refusing *is* taking responsibility, and it is the answer to one
	// of the two most common requests that turn out not to involve a proxy at
	// all — "IoT gets no internet" (§2.4).
	ViaBlocked ViaKind = "blocked"

	// ViaLocalSocket — TPROXY — is v2 (§9) and deliberately absent from the
	// vocabulary rather than declared and refused. An enum value that every
	// generated surface offers and validation then rejects is a lie in the
	// schema, and the schema is the contract three other surfaces are built
	// from.
)

// ViaKinds lists the vocabulary, for flag help and the schema enum.
func ViaKinds() []ViaKind { return []ViaKind{ViaInterface, ViaNextHop, ViaBlocked} }

// Valid reports whether k is a known form. The empty string is not: unlike the
// enums that have a sensible default, there is no form to fall back to, and
// guessing one would silently route traffic somewhere nobody asked for.
func (k ViaKind) Valid() bool { return slices.Contains(ViaKinds(), k) }

// Routing reports whether this form leaves through a route table.
//
// ViaInterface and ViaNextHop are both routing and differ by one field;
// ViaBlocked is routing too, with an unreachable route rather than a real one.
// The split that matters is against ViaLocalSocket, where the packet is never
// routed at all — and that form does not exist yet, which is why this reads as
// a tautology today and will not once TPROXY lands.
func (k ViaKind) Routing() bool { return k == ViaInterface || k == ViaNextHop || k == ViaBlocked }

// IPv6Mode is an exit's answer to §5.4.
type IPv6Mode string

const (
	// IPv6Via carries IPv6 through the exit alongside IPv4. Correct when the
	// exit actually has IPv6 — a WireGuard tunnel with a v6 address, a next hop
	// that routes v6.
	IPv6Via IPv6Mode = "via"

	// IPv6Block sends IPv6 from assigned sources to `unreachable` instead of
	// letting it take the default path. This is the right answer for a v4-only
	// exit, and it fails loudly: a browser tries AAAA, gets an immediate
	// refusal, and falls straight back to IPv4.
	IPv6Block IPv6Mode = "block"

	// IPv6Direct lets IPv6 from assigned sources take the box's default path.
	// Available, never a default: it is the exact leak §5.4 is about, and the
	// operator has to have typed it.
	IPv6Direct IPv6Mode = "direct"
)

// IPv6Modes lists the vocabulary.
func IPv6Modes() []IPv6Mode { return []IPv6Mode{IPv6Via, IPv6Block, IPv6Direct} }

// Valid reports whether m is known. Empty is valid and resolves per form.
func (m IPv6Mode) Valid() bool { return m == "" || slices.Contains(IPv6Modes(), m) }

// FailureMode is what happens to assigned traffic when the exit's probe fails.
type FailureMode string

const (
	// FailBlock stops the traffic, which is the default. The UI can then say
	// "Living Room TV: no internet — Proxy is down", and that is a sentence
	// somebody can act on.
	FailBlock FailureMode = "block"

	// FailDirect sends the traffic out the box's default path instead. It
	// silently leaks exactly what the operator asked to route, so it is
	// available and never the default.
	FailDirect FailureMode = "direct"
)

// FailureModes lists the vocabulary.
func FailureModes() []FailureMode { return []FailureMode{FailBlock, FailDirect} }

// Valid reports whether f is known. Empty is valid and means FailBlock.
func (f FailureMode) Valid() bool { return f == "" || slices.Contains(FailureModes(), f) }

// OrDefault resolves the empty string.
func (f FailureMode) OrDefault() FailureMode {
	if f == "" {
		return FailBlock
	}
	return f
}

// Probe is a through-path health check for one exit (§5.5).
//
// Through-path, not a ping, and the distinction is the whole point: a crashed
// proxy daemon on a live box answers ARP and ICMP indefinitely while
// forwarding nothing — and worse, loops our traffic back at us, because its own
// default gateway is us. Only an end-to-end connection through the exit can
// tell that apart from a healthy one.
type Probe struct {
	// Target is what we connect to, as address:port. An address rather than a
	// name on purpose: resolving it would make the probe depend on DNS, so a
	// resolver problem would present as every exit being down at once.
	Target netip.AddrPort `json:"target"`

	// Interval is how often to check. Zero means DefaultProbeInterval.
	Interval Duration `json:"interval,omitempty"`

	// Timeout bounds one attempt. Zero means DefaultProbeTimeout. It must be
	// shorter than Interval or attempts overlap.
	Timeout Duration `json:"timeout,omitempty"`

	// Failures is how many consecutive failures mark the exit down, and
	// Successes how many consecutive successes bring it back. Zero means the
	// defaults below.
	//
	// Hysteresis in both directions, per §5.5, and it is not symmetric by
	// accident: coming back should be slower than going down is, because a
	// half-recovered proxy that flaps is worse for the house than one that
	// stays down until somebody looks at it.
	Failures  int `json:"failures,omitempty"`
	Successes int `json:"successes,omitempty"`
}

// Probe defaults. Thirty seconds is slow enough to be invisible on the wire and
// fast enough that a dead exit is caught before anyone files a complaint.
const (
	DefaultProbeInterval  = Duration(30 * time.Second)
	DefaultProbeTimeout   = Duration(5 * time.Second)
	DefaultProbeFailures  = 3
	DefaultProbeSuccesses = 2
)

// MinProbeInterval is a floor, so that a typo cannot turn the probe into a
// connection flood against somebody else's server.
const MinProbeInterval = Duration(time.Second)

// Resolved fills in every zero value, so the prober never has to.
func (p Probe) Resolved() Probe {
	if p.Interval <= 0 {
		p.Interval = DefaultProbeInterval
	}
	if p.Timeout <= 0 {
		p.Timeout = DefaultProbeTimeout
	}
	if p.Failures <= 0 {
		p.Failures = DefaultProbeFailures
	}
	if p.Successes <= 0 {
		p.Successes = DefaultProbeSuccesses
	}
	return p
}

// MaxNameLen bounds an exit name so a UI can lay out a row without defending
// against a megabyte in a field. Generous enough that no real name hits it.
const MaxNameLen = 64

// SNATOrDefault resolves the nil pointer: on for a next hop, off for everything
// else, per §5.3's recommendation for v1.
func (e Exit) SNATOrDefault() bool {
	if e.Via.Kind != ViaNextHop {
		return false
	}
	if e.SNAT == nil {
		return true
	}
	return *e.SNAT
}

// IPv6OrDefault resolves the empty IPv6 mode per form.
//
// The default is IPv6Block for every form, and that is the conservative answer
// rather than the convenient one. An exit whose v6 capability we cannot see
// from here is one where "carry it" might work and might leak; blocking fails
// immediately and visibly, which is the failure an operator can diagnose. An
// exit that really does carry v6 says so in one field.
func (e Exit) IPv6OrDefault() IPv6Mode {
	if e.IPv6 == "" {
		return IPv6Block
	}
	return e.IPv6
}

// StatsOrDefault resolves the nil pointer: accounting is on unless it was
// deliberately turned off.
func (c Config) StatsOrDefault() bool {
	if c.Stats == nil {
		return true
	}
	return *c.Stats
}

// Find returns the exit with this name, or false.
func (c Config) Find(name string) (Exit, bool) {
	for _, e := range c.Exits {
		if e.Name == name {
			return e, true
		}
	}
	return Exit{}, false
}

// Assigned reports the exit name in force for an interface, and where it came
// from — §2.2's *effective value, with its source*.
//
// Inheritance is unusable if the answer is not visible, so this is the function
// every surface asks rather than each one reimplementing the ladder. It returns
// the empty exit name for "the box's own default route", which is a real answer
// and not an absent one.
func (c Config) Assigned(iface string) (exit string, source Source) {
	for _, a := range c.Interfaces {
		if a.Interface != iface {
			continue
		}
		if a.Exit == "" {
			// Explicitly set to inherit. Reported as coming from the default
			// rather than from the network, because that is what an operator
			// changing the default needs to know: this row will follow.
			return c.Default, SourceDefault
		}
		return a.Exit, SourceInterface
	}
	return c.Default, SourceDefault
}

// Source says which rung of §2.1's ladder produced an effective value.
type Source string

const (
	// SourceDefault is the box-wide setting.
	SourceDefault Source = "default"
	// SourceInterface is a per-network assignment.
	SourceInterface Source = "interface"
)

// InUse reports whether any assignment, or the default, names this exit.
//
// Used by the impact classifier: changing an exit nobody routes through cannot
// disturb anybody, and saying so is what keeps `disruptive` from crying wolf.
func (c Config) InUse(name string) bool {
	for _, a := range c.Destinations {
		if a.Exit == name && name != "" {
			return true
		}
	}
	if name == "" {
		return false
	}
	if c.Default == name {
		return true
	}
	for _, a := range c.Interfaces {
		if a.Exit == name {
			return true
		}
	}
	for _, a := range c.Clients {
		if a.Exit == name {
			return true
		}
	}
	return false
}

// UsedBy lists the interfaces and client MACs assigned to this exit, sorted.
func (c Config) UsedBy(name string) []string {
	if name == "" {
		return nil
	}
	var out []string
	for _, a := range c.Destinations {
		if a.Exit == name {
			out = append(out, "destination "+a.Prefix)
		}
	}
	for _, a := range c.Interfaces {
		if got, _ := c.Assigned(a.Interface); got == name {
			out = append(out, a.Interface)
		}
	}
	for _, a := range c.Clients {
		if a.Exit == name {
			out = append(out, a.MAC)
		}
	}
	sort.Strings(out)
	return out
}

// UnmarshalConfig parses a document strictly.
//
// Unknown fields are rejected for the reason internal/dhcp gives on the PUT
// path: a mistyped key that silently did nothing would be the worst outcome — a
// 200, an operator who believes the setting took, and a screen that disagrees.
func UnmarshalConfig(data []byte) (Config, error) {
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()

	var l legacyConfig
	if err := dec.Decode(&l); err != nil {
		return Config{}, fmt.Errorf("parsing gateway config: %w", err)
	}
	c := l.Config
	if l.Devices != nil {
		if c.Clients != nil {
			return Config{}, fmt.Errorf("parsing gateway config: both clients and devices are present")
		}
		c.Clients = l.Devices
		c.Legacy = true
	}
	c.Normalize()
	return c, nil
}

// legacyConfig is this module's subtree as an older olr wrote it, when the
// per-client overrides were called `devices`.
type legacyConfig struct {
	Config
	Devices []ClientAssignment `json:"devices,omitempty"`
}

// HasLegacyKeys reports whether a stored subtree still calls its per-client
// overrides `devices`, the word this module used before the two inventories
// were told apart.
func HasLegacyKeys(raw []byte) bool {
	var probe struct {
		Clients json.RawMessage `json:"clients"`
		Devices json.RawMessage `json:"devices"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return false
	}
	return probe.Devices != nil && probe.Clients == nil
}

// MarshalConfig renders intent for the store.
func MarshalConfig(c Config) ([]byte, error) {
	c.Normalize()
	return json.MarshalIndent(c, "", "  ")
}

// LegacyModuleName is what this module's key was called before v0.1.1.
//
// The module was renamed from `routing` to `gateway`: the box as a whole is the
// router, and the part of it that forwards traffic is the gateway, alongside
// `dhcp` and `dns`. ModuleName is the document key as well as the CLI name and
// the API path segment, so the rename moved the on-disk key with it.
const LegacyModuleName = "routing"

// FromDocument reads this module's subtree out of the configuration document.
//
// A document without a "gateway" key is not an error — it means nobody has
// configured an exit, which is exactly what a fresh install looks like and what
// the screen must render as an empty state rather than a failure.
//
// Which is exactly what makes the rename dangerous, and why the fallback below
// exists. A v0.1.0 box has its exits under "routing"; without this it would
// come back from the upgrade with an empty gateway screen, no error anywhere,
// and its policy silently not programmed. "Nobody configured this" and "the key
// moved" are indistinguishable to the caller, so they must not be to us.
//
// The read is one-way on purpose. Nothing rewrites the document here: the next
// apply marshals under ModuleName and the stale key falls away with it, so an
// operator who upgrades and changes nothing keeps a file that a downgrade can
// still read.
func FromDocument(d core.Document) (Config, error) {
	raw, ok := d.Raw(ModuleName)
	if !ok {
		if raw, ok = d.Raw(LegacyModuleName); !ok {
			return Config{}, nil
		}
	}
	c, err := UnmarshalConfig(raw)
	if err != nil {
		return Config{}, fmt.Errorf("%s configuration: %w", ModuleName, err)
	}
	return c, nil
}

// Normalize puts the config in canonical form: names and interfaces trimmed,
// exits and assignments sorted, slots allocated.
//
// Sorting is not cosmetic. The lists are rendered in stored order, so an
// append-on-edit would make a row jump to the bottom the moment it was renamed
// — the bug internal/devices called out in the dhcp tables. Sorting on the way
// in means position is a function of identity, not of edit history.
//
// It deliberately does not validate: a malformed exit is left as-is for
// Validate to report against a proper path.
func (c *Config) Normalize() {
	// Pins name monitored sites, not probe identities. Drop removed custom
	// names here so any config write also prevents their later resurrection.
	pins := make([]string, 0, len(c.LatencyPins))
	for _, name := range c.LatencyPins {
		known := false
		if _, ok := commonLatencySite(name); ok {
			known = true
		}
		for _, site := range c.LatencySites {
			if site.Name == name {
				known = true
				break
			}
		}
		if known {
			pins = append(pins, name)
		}
	}
	slices.Sort(pins)
	c.LatencyPins = slices.Compact(pins)

	for i := range c.Destinations {
		a := &c.Destinations[i]
		a.Prefix = strings.TrimSpace(a.Prefix)
		a.Exit = strings.TrimSpace(a.Exit)
		if p, err := destinationPrefix(a.Prefix); err == nil {
			a.Prefix = p.String()
		}
	}
	slices.SortStableFunc(c.Destinations, func(a, b DestinationAssignment) int { return strings.Compare(a.Prefix, b.Prefix) })
	for i := range c.Exits {
		c.Exits[i].Name = strings.TrimSpace(c.Exits[i].Name)
		c.Exits[i].Via.Interface = strings.TrimSpace(c.Exits[i].Via.Interface)
		c.Exits[i].Via.Dev = strings.TrimSpace(c.Exits[i].Via.Dev)
		if c.Exits[i].DNS != nil && c.Exits[i].DNS.Mode == "olr" && !c.Exits[i].DNS.Server.IsValid() {
			c.Exits[i].DNS = nil
		}
	}
	c.Default = strings.TrimSpace(c.Default)
	for i := range c.Interfaces {
		c.Interfaces[i].Interface = strings.TrimSpace(c.Interfaces[i].Interface)
		c.Interfaces[i].Exit = strings.TrimSpace(c.Interfaces[i].Exit)
	}
	for i := range c.Clients {
		c.Clients[i].MAC = strings.TrimSpace(c.Clients[i].MAC)
		if mac, err := core.NormalizeMAC(c.Clients[i].MAC); err == nil {
			c.Clients[i].MAC = mac
		}
		c.Clients[i].Exit = strings.TrimSpace(c.Clients[i].Exit)
	}

	slices.SortStableFunc(c.Exits, func(a, b Exit) int { return strings.Compare(a.Name, b.Name) })
	slices.SortStableFunc(c.Interfaces, func(a, b Assignment) int {
		return strings.Compare(a.Interface, b.Interface)
	})
	slices.SortStableFunc(c.Clients, func(a, b ClientAssignment) int {
		return strings.Compare(a.MAC, b.MAC)
	})

	// The forwards normalize themselves — names and interfaces trimmed, list
	// sorted, slots allocated — in the package that owns them, so that the one
	// rule about a forward's slot lives beside the counter it names.
	nc := nat.Config{Forwards: c.Forwards}
	nc.Normalize()
	c.Forwards = nc.Forwards

	// Last, because it depends on the names being settled and it is the one
	// step here that invents a value rather than tidying one.
	c.allocateSlots()
}

// Upsert adds an exit or replaces the one with the same name, keeping its slot.
//
// Keeping the slot is the point of the method existing at all: an edit that
// silently reallocated it would move the exit's route table out from under
// every flow currently marked for it.
func (c *Config) Upsert(e Exit) {
	for i := range c.Exits {
		if c.Exits[i].Name == e.Name {
			if e.Slot == 0 {
				e.Slot = c.Exits[i].Slot
			}
			c.Exits[i] = e
			c.Normalize()
			return
		}
	}
	c.Exits = append(c.Exits, e)
	c.Normalize()
}

// SetAssignment points one network at an exit, replacing any it already had.
//
// An empty exit name is meaningful and is kept rather than dropped: it records
// "this network explicitly follows the box-wide setting", which reads
// differently on screen from a network nobody has thought about. Use
// RemoveAssignment to say the second thing.
func (c *Config) SetAssignment(iface, exit string) {
	for i := range c.Interfaces {
		if c.Interfaces[i].Interface == iface {
			c.Interfaces[i].Exit = exit
			c.Normalize()
			return
		}
	}
	c.Interfaces = append(c.Interfaces, Assignment{Interface: iface, Exit: exit})
	c.Normalize()
}

// RemoveAssignment drops a network's override, reporting whether it had one.
func (c *Config) RemoveAssignment(iface string) bool {
	for i, a := range c.Interfaces {
		if a.Interface == iface {
			c.Interfaces = append(c.Interfaces[:i], c.Interfaces[i+1:]...)
			return true
		}
	}
	return false
}

// SetDevice overrides the network choice for one MAC; empty means direct.
func (c *Config) SetDevice(mac, exit string) {
	if norm, err := core.NormalizeMAC(mac); err == nil {
		mac = norm
	}
	for i := range c.Clients {
		if c.Clients[i].MAC == mac {
			c.Clients[i].Exit = exit
			c.Normalize()
			return
		}
	}
	c.Clients = append(c.Clients, ClientAssignment{MAC: mac, Exit: exit})
	c.Normalize()
}

// RemoveClient returns the device to its network's setting.
func (c *Config) RemoveClient(mac string) bool {
	if norm, err := core.NormalizeMAC(mac); err == nil {
		mac = norm
	}
	for i, a := range c.Clients {
		if a.MAC == mac {
			c.Clients = append(c.Clients[:i], c.Clients[i+1:]...)
			return true
		}
	}
	return false
}

// Remove drops an exit, reporting whether it was there.
//
// It deliberately does not clean up references to the name. A dangling Default
// or Assignment is a validation error with a message naming both ends, which is
// a better outcome than silently re-pointing somebody's phones at the modem
// because an exit was deleted in another tab.
func (c *Config) Remove(name string) bool {
	for i, e := range c.Exits {
		if e.Name == name {
			c.Exits = append(c.Exits[:i], c.Exits[i+1:]...)
			return true
		}
	}
	return false
}

// Rename changes an exit's name and every reference to it, together.
//
// Together is the whole reason this is a method rather than two edits: an exit
// is referenced by name from Default and from every Assignment, so renaming in
// one place and not the others is how a rename turns into an outage.
func (c *Config) Rename(from, to string) bool {
	e, ok := c.Find(from)
	if !ok {
		return false
	}
	e.Name = to
	for i := range c.Destinations {
		if c.Destinations[i].Exit == from {
			c.Destinations[i].Exit = to
		}
	}
	c.Remove(from)
	c.Exits = append(c.Exits, e)
	if c.Default == from {
		c.Default = to
	}
	for i := range c.Interfaces {
		if c.Interfaces[i].Exit == from {
			c.Interfaces[i].Exit = to
		}
	}
	for i := range c.Clients {
		if c.Clients[i].Exit == from {
			c.Clients[i].Exit = to
		}
	}
	for i := range c.LatencySites {
		if c.LatencySites[i].Exit == from {
			c.LatencySites[i].Exit = to
		}
	}
	for i := range c.LatencyCommon {
		if c.LatencyCommon[i].Exit == from {
			c.LatencyCommon[i].Exit = to
		}
	}
	c.Normalize()
	return true
}

// Empty reports whether anything has been configured at all.
func (c Config) Empty() bool {
	return len(c.Destinations) == 0 && len(c.Exits) == 0 && len(c.Interfaces) == 0 && len(c.Clients) == 0 && c.Default == "" &&
		len(c.Forwards) == 0
}

// SNATOrDefault resolves the nil pointer: egress masquerade is on unless the
// operator turned it off.
func (c Config) SNATOrDefault() bool { return c.SNAT == nil || bool(*c.SNAT) }

// NAT projects this config into what internal/gateway/nat works on.
//
// The projection rather than a shared struct, and it is the whole seam between
// the two halves: this package owns the stored document and decides what the
// other one is asked to render, while that one owns the table and how. Note
// what it passes for Enabled — this module's single switch, which is why
// turning the module off closes every forwarded port.
//
// `uplink`, `networks` and `dialIn` come from outside: the uplink is `dial`'s
// (read through UplinkView), the networks are `link`'s, and the dial-in network
// is `remote`'s. Any being absent means the rule built from it is not written,
// which is the correct answer for the reference topology — olr beside the
// modem, with no interface of its own facing outward and no business guessing
// which one does — and for a box with no tunnel.
func (c Config) NAT(uplink string, networks []netip.Prefix, dialIn *nat.DialIn) nat.Config {
	out := nat.Config{
		Enabled:  c.Enabled,
		Forwards: c.Forwards,
	}
	if dialIn != nil && dialIn.Prefix.IsValid() {
		d := *dialIn
		d.Uplink = uplink
		out.DialIn = &d
	}
	if c.SNATOrDefault() && uplink != "" && len(networks) > 0 {
		sources := slices.Clone(networks)
		// The tunnel is a network behind this box like any other, so its peers
		// join the egress source set: a peer that sends everything must leave
		// with an address the internet can reply to, exactly as a laptop on the
		// LAN does (docs/remote-access.md §6.2). Not when it overlaps one of
		// those networks: the set is an interval set, and overlapping elements
		// are either rejected by the kernel or meant something nobody asked
		// for. nat.OverlapsServed skips the rule for the same reason.
		if out.DialIn != nil && !overlapsAny(out.DialIn.Prefix, networks) {
			sources = append(sources, out.DialIn.Prefix.Masked())
		}
		slices.SortFunc(sources, func(a, b netip.Prefix) int {
			return strings.Compare(a.String(), b.String())
		})
		out.Egress = &nat.Egress{Interface: uplink, Sources: sources}
	}
	return out
}

// overlapsAny reports whether prefix shares addresses with any of networks.
func overlapsAny(prefix netip.Prefix, networks []netip.Prefix) bool {
	for _, n := range networks {
		if n.Overlaps(prefix) {
			return true
		}
	}
	return false
}

package link

import (
	"fmt"
	"net/netip"
	"slices"
	"strings"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// The API's own shapes, for the reason internal/dhcp/view.go gives: what the
// HTTP API returns should be a deliberate decision rather than a side effect of
// which fields happened to be exported.

// interfaceView is one row of the interface list.
type interfaceView struct {
	Name string `json:"name"`

	// Adopted is the stored half: the operator handed this interface to olr.
	Adopted bool `json:"adopted"`

	// Present reports whether the kernel currently has it. False with Adopted
	// true is the typo case, and the UI has to say so rather than showing a row
	// that looks like any other.
	Present bool `json:"present"`

	// Up is administrative state, Running is carrier. Both, because "up with no
	// cable" is the most common reason a freshly configured DHCP server appears
	// to do nothing, and one boolean cannot say it.
	Up      bool `json:"up"`
	Running bool `json:"running"`

	Loopback bool   `json:"loopback"`
	Physical bool   `json:"physical"`
	MAC      string `json:"mac,omitempty"`
	Index    int    `json:"index,omitempty"`

	// MTU is the kernel's maximum transmission unit, absent when the interface
	// is not present or the kernel could not be asked. It is shown because it
	// is the number the MSS clamp is derived from, and an operator looking at a
	// path-MTU complaint should be able to see both halves on one screen.
	MTU       int     `json:"mtu,omitempty"`
	SpeedMbps *uint64 `json:"speed_mbps,omitempty"`
	RXBytes   *uint64 `json:"rx_bytes,omitempty"`
	TXBytes   *uint64 `json:"tx_bytes,omitempty"`

	// The packet, drop and error counters, cumulative for the same reason the
	// byte counters are: a page that draws a rate differences them against
	// its own previous read. Absent means the kernel did not answer, never
	// zero, so a source that cannot report drops is not drawn as one that
	// reports none.
	//
	// Only the drops reach the printed table. Everything else here is for the
	// API and the performance page, where there is room to say what it means;
	// a terminal row of seven cumulative numbers is a row nobody reads.
	RXPackets    *uint64 `json:"rx_packets,omitempty"`
	TXPackets    *uint64 `json:"tx_packets,omitempty"`
	RXDropped    *uint64 `json:"rx_dropped,omitempty"`
	TXDropped    *uint64 `json:"tx_dropped,omitempty"`
	RXErrors     *uint64 `json:"rx_errors,omitempty"`
	TXErrors     *uint64 `json:"tx_errors,omitempty"`
	RXFIFOErrors *uint64 `json:"rx_fifo_errors,omitempty"`

	// RXMissedErrors is the device's buffer-exhaustion counter, published
	// apart from RXDropped so a reader can tell "nothing wanted the frame"
	// from "the device had no room for it". RFC 2863 puts both under
	// ifInDiscards; the kernel keeps them separate and only the split is
	// actionable.
	RXMissedErrors *uint64 `json:"rx_missed_errors,omitempty"`

	// Prefixes are every address on the interface, link-local excluded.
	Prefixes []string `json:"prefixes,omitempty"`

	// Address and Subnet are the interface's first IPv4 prefix, split into the
	// two halves an operator reads separately: "this router is 192.168.1.2" and
	// "the network is 192.168.1.0/24". Empty when the interface has no IPv4
	// address.
	//
	// These are *observed*. What the interface is supposed to have is its
	// network's subnet, and the two disagreeing is drift rather than a
	// contradiction — which is why both are published instead of one being
	// derived from the other.
	Address string `json:"address,omitempty"`
	Subnet  string `json:"subnet,omitempty"`

	// Network is the network this interface carries, empty if it carries none.
	//
	// The reverse of a network's member list, and what lets an interface row say
	// what it is *for*. Before networks existed the only available answer to
	// "what is ens18 for" was whatever address it happened to be holding.
	Network string `json:"network,omitempty"`
}

func viewInterface(info Info, observed map[string]Interface, cfg Config) interfaceView {
	v := interfaceView{
		Name:     info.Name,
		Adopted:  info.Adopted,
		Present:  info.Present,
		Up:       info.Up,
		Loopback: false,
		Prefixes: make([]string, 0, len(info.Prefixes)),
	}
	for _, p := range info.Prefixes {
		v.Prefixes = append(v.Prefixes, p.String())
	}
	if iface, ok := observed[info.Name]; ok {
		v.Running = iface.Running
		v.Loopback = iface.Loopback
		v.Physical = iface.Physical
		v.MAC = iface.HardwareAddr
		v.Index = iface.Index
		v.MTU = iface.MTU
		v.SpeedMbps = iface.SpeedMbps
		v.RXBytes = iface.RXBytes
		v.TXBytes = iface.TXBytes
		v.RXPackets = iface.RXPackets
		v.TXPackets = iface.TXPackets
		v.RXDropped = iface.RXDropped
		v.TXDropped = iface.TXDropped
		v.RXErrors = iface.RXErrors
		v.TXErrors = iface.TXErrors
		v.RXFIFOErrors = iface.RXFIFOErrors
		v.RXMissedErrors = iface.RXMissedErrors
	}
	if prefix, ok := core.FirstIPv4(info.Prefixes); ok {
		v.Address = prefix.Addr().String()
		v.Subnet = prefix.Masked().String()
	}
	if n, ok := cfg.NetworkFor(info.Name); ok {
		v.Network = n.Name
	}
	return v
}

// networkView is one network as the API publishes it.
//
// It carries the derived range alongside the subnet because every caller wants
// it and none of them should compute it: `dhcp` derives a pool from it and the
// form prefills from it, and two implementations of §11.2's "leave a static
// block free" rule would eventually disagree about where the block ends.
type networkView struct {
	Name    string   `json:"name"`
	Members []string `json:"members"`

	// Subnet and Router are the network's IPv4 intent, empty for a network that
	// serves no IPv4.
	Subnet string `json:"subnet,omitempty"`
	Router string `json:"router,omitempty"`

	// RouterExplicit distinguishes "the operator pinned this address" from "we
	// derived the first host address", which is what a form needs to decide
	// between showing the field filled in and showing it as a placeholder.
	RouterExplicit bool `json:"router_explicit,omitempty"`

	// Subnet6 and Router6 are the network's static IPv6 intent, empty for a
	// network with no ipv6 block. Router6Explicit is RouterExplicit's twin.
	Subnet6         string `json:"subnet6,omitempty"`
	Router6         string `json:"router6,omitempty"`
	Router6Explicit bool   `json:"router6_explicit,omitempty"`

	// Delegated is the number of the /64 the network takes out of the
	// uplink's delegated prefix, absent for a static one. With it set and
	// Subnet6 empty, the prefix has not been delegated yet — or is too small
	// to have this many /64s.
	Delegated *int `json:"delegated,omitempty"`

	// SuggestedStart and SuggestedEnd are the range `dhcp` derives when nobody
	// types one. A hint, never a second opinion: `dhcp` validates whatever range
	// it is given against its own rules regardless of where the numbers came
	// from.
	SuggestedStart string `json:"suggested_start,omitempty"`
	SuggestedEnd   string `json:"suggested_end,omitempty"`

	// Present reports whether every member exists on this machine. False is the
	// typo case and the not-plugged-in-yet case.
	Present bool `json:"present"`
}

func viewNetwork(n Network, observed map[string]Interface, delegated netip.Prefix) networkView {
	v := networkView{
		Name:    n.Name,
		Members: slices.Clone(n.Members),
		Present: len(n.Members) > 0,
	}
	if v.Members == nil {
		v.Members = []string{}
	}
	for _, m := range n.Members {
		if _, ok := observed[m]; !ok {
			v.Present = false
		}
	}
	if n.IPv4 != nil && n.IPv4.Subnet.IsValid() {
		router := n.IPv4.RouterAddr()
		v.Subnet = n.IPv4.Subnet.String()
		v.Router = router.String()
		v.RouterExplicit = n.IPv4.Router != nil
		if start, end, ok := core.SuggestRange(n.IPv4.Subnet, router); ok {
			v.SuggestedStart, v.SuggestedEnd = start.String(), end.String()
		}
	}
	if n.IPv6 != nil {
		v.Delegated = n.IPv6.Delegated
		if subnet, router, ok := n.IPv6.Resolve(delegated); ok {
			v.Subnet6 = subnet.String()
			v.Router6 = router.Addr().String()
			v.Router6Explicit = n.IPv6.Router != nil
		}
	}
	return v
}

type listResponse struct {
	Interfaces []interfaceView `json:"interfaces"`

	// Networks are the networks configured on this box, alongside the interfaces
	// rather than behind a second request: every surface that renders one wants
	// the other in the same breath, and two round trips would let them be read
	// at two different instants.
	Networks []networkView `json:"networks"`

	// Problems are the findings against the stored set — an adopted name with
	// no interface behind it, a network whose member does not exist. Reported
	// alongside the list rather than as an error, because every one of them
	// describes a row that is still worth showing.
	Problems []core.Problem `json:"problems,omitempty"`

	// AsOf stamps the whole reply: the observed half is read per request and
	// never cached, so it carries its freshness (§4.5).
	AsOf time.Time `json:"as_of"`
}

// --- plan -------------------------------------------------------------------

// planView mirrors internal/dhcp's and internal/devices' plan shape on purpose.
//
// The field names are identical so that a client's Plan type and its plan
// preview render any module's answer without a second implementation.
//
// The impact stopped being a constant `none` when networks landed. Two kinds of
// change now share one plan and they could not be further apart: adopting an
// interface still does nothing at all to the box, while renumbering a network
// takes every client's address away and may take the operator's own session
// with it. One number for both would be useless exactly where it matters.
type planView struct {
	Backend  string         `json:"backend"`
	Changes  []changeView   `json:"changes"`
	Action   string         `json:"action"`
	Impact   string         `json:"impact"`
	Empty    bool           `json:"empty"`
	Warnings []core.Problem `json:"warnings,omitempty"`
}

type changeView struct {
	Path   string `json:"path"`
	Kind   string `json:"kind"`
	Impact string `json:"impact"`
	Diff   string `json:"diff"`
}

const (
	impactNone       = "none"
	impactRestart    = "restart"
	impactDisruptive = "disruptive"

	actionNone      = "none"
	actionConfigure = "configure"

	kindCreate = "create"
	kindUpdate = "update"
	kindDelete = "delete"
)

// impactRank orders the vocabulary so a plan can report the worst of its parts.
var impactRank = map[string]int{impactNone: 0, impactRestart: 1, impactDisruptive: 2}

// buildPlan diffs stored intent against a proposal, and both against the kernel.
//
// Three sources, which is one more than the other modules need. The document
// diff says what the operator changed; the kernel diff says what has to happen
// on the box for the result to be true. They are not the same list — re-applying
// an unchanged config still has work to do if somebody ran `ip addr del` by
// hand, and that is drift (§5.4) rather than a no-op.
func buildPlan(stored, desired Config, observed []Interface, opts Options, delegated netip.Prefix) planView {
	stored.Normalize()
	desired.Normalize()

	res := Validate(desired, observed)
	plan := planView{
		Backend:  "",
		Changes:  []changeView{},
		Action:   actionNone,
		Impact:   impactNone,
		Warnings: problems(res.Warnings),
	}

	// --- adoption: bare names, and nothing reaches the box ---
	//
	// No update kind here, only create and delete: the entries are bare names,
	// so changing one is removing a name and adding another. Rendering that as
	// an update would invent a relationship between two interfaces that have
	// nothing to do with each other.
	for _, name := range desired.Adopted {
		if !stored.IsAdopted(name) {
			plan.Changes = append(plan.Changes, changeView{
				Path: itemPath(name), Kind: kindCreate, Impact: impactNone,
				Diff: fmt.Sprintf("+ %s\n", name),
			})
		}
	}
	for _, name := range stored.Adopted {
		if !desired.IsAdopted(name) {
			plan.Changes = append(plan.Changes, changeView{
				Path: itemPath(name), Kind: kindDelete, Impact: impactNone,
				Diff: fmt.Sprintf("- %s\n", name),
			})
		}
	}

	// --- networks: keyed objects, and these do reach the box ---
	names := make([]string, 0, len(stored.IPv6Auto)+len(desired.IPv6Auto))
	for name := range stored.IPv6Auto {
		names = append(names, name)
	}
	for name := range desired.IPv6Auto {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range slices.Compact(names) {
		before, was := stored.IPv6Auto[name]
		after, now := desired.IPv6Auto[name]
		if was == now && before == after {
			continue
		}
		value := "host defaults"
		if now {
			value = fmt.Sprintf("%t", after)
		}
		plan.Changes = append(plan.Changes, changeView{
			Path: "interfaces[" + name + "].ipv6_auto", Kind: kindUpdate,
			Impact: impactDisruptive,
			Diff:   "+ Automatic IPv6 addressing: " + value + "\n",
		})
	}

	for _, n := range desired.Networks {
		before, existed := stored.Network(n.Name)
		switch {
		case !existed:
			plan.Changes = append(plan.Changes, changeView{
				Path: networkPath(n.Name), Kind: kindCreate, Impact: impactRestart,
				Diff: describeNetwork("+ ", n),
			})
		case !sameNetwork(before, n):
			plan.Changes = append(plan.Changes, changeView{
				Path: networkPath(n.Name), Kind: kindUpdate, Impact: networkImpact(before, n),
				Diff: describeNetwork("- ", before) + describeNetwork("+ ", n),
			})
		}
	}
	for _, n := range stored.Networks {
		if _, kept := desired.Network(n.Name); !kept {
			// Removing a network takes the router's address off the interface,
			// which is every bit as disruptive as renumbering it — unless the
			// address is being kept for the uplink to take over, when what goes
			// is only what this box serves there.
			plan.Changes = append(plan.Changes, changeView{
				Path: networkPath(n.Name), Kind: kindDelete,
				Impact: pick(opts.KeepAddresses, impactRestart, impactDisruptive),
				Diff:   describeNetwork("- ", n),
			})
		}
	}

	// --- the kernel: what has to happen for any of the above to be true ---
	for _, ap := range PlanAddrs(desired, observed, delegated) {
		lines := DescribeAddrPlan(ap)
		if len(lines) == 0 {
			continue
		}
		plan.Changes = append(plan.Changes, changeView{
			Path: "interfaces[" + ap.Interface + "]",
			Kind: kindUpdate,
			// Taking an address away drops whatever was using it. Adding one to
			// an interface that had none takes nothing away from anybody.
			Impact: pick(len(ap.Remove) > 0, impactDisruptive, impactRestart),
			Diff:   strings.Join(lines, "\n") + "\n",
		})
	}

	// The addresses networks leave behind, on interfaces no network claims any
	// more — the half of a removal PlanAddrs cannot see, because it walks the
	// networks that still exist.
	if !opts.KeepAddresses {
		for _, ap := range PlanRetire(stored, desired, observed, delegated, delegated) {
			plan.Changes = append(plan.Changes, changeView{
				Path:   "interfaces[" + ap.Interface + "]",
				Kind:   kindUpdate,
				Impact: impactDisruptive,
				Diff:   strings.Join(DescribeAddrPlan(ap), "\n") + "\n",
			})
		}
	}

	for _, c := range plan.Changes {
		if impactRank[c.Impact] > impactRank[plan.Impact] {
			plan.Impact = c.Impact
		}
	}
	if plan.Impact != impactNone {
		plan.Action = actionConfigure
	}
	plan.Empty = len(plan.Changes) == 0
	return plan
}

// sameNetwork reports whether two networks are configured identically.
func sameNetwork(a, b Network) bool {
	if a.Name != b.Name || !slices.Equal(a.Members, b.Members) {
		return false
	}
	return sameIPv4(a.IPv4, b.IPv4) && sameIPv6(a.IPv6, b.IPv6)
}

func sameIPv4(a, b *NetworkIPv4) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Subnet == b.Subnet && a.RouterAddr() == b.RouterAddr()
}

func sameIPv6(a, b *NetworkIPv6) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Subnet == b.Subnet && a.RouterAddr() == b.RouterAddr() && sameInt(a.Delegated, b.Delegated)
}

func sameInt(a, b *int) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// networkImpact classifies a change to an existing network in client terms.
//
// The question is only ever "does anything on this network lose the address it
// is holding". Renumbering the subnet does; adding an IPv4 block to a network
// that had none does not, because there was nothing there to lose.
func networkImpact(before, after Network) string {
	if !slices.Equal(before.Members, after.Members) {
		return impactDisruptive
	}
	v4, v6 := ipv4Impact(before.IPv4, after.IPv4), ipv6Impact(before.IPv6, after.IPv6)
	if impactRank[v6] > impactRank[v4] {
		return v6
	}
	return v4
}

func ipv4Impact(before, after *NetworkIPv4) string {
	switch {
	case before == nil && after == nil:
		return impactNone
	case before == nil:
		return impactRestart
	case after == nil, before.Subnet != after.Subnet:
		return impactDisruptive
	case before.RouterAddr() != after.RouterAddr():
		// The subnet is unchanged, so clients keep valid addresses — but their
		// default gateway just moved and they will not learn that until they
		// renew, which is a network that looks fine and does not work.
		return impactDisruptive
	}
	return impactNone
}

// ipv6Impact is the same question for the v6 block. Losing or renumbering the
// prefix strands every address clients formed from it until its advertised
// lifetime runs out, which is the same "looks fine and does not work" as a
// moved IPv4 gateway. Gaining one takes nothing away.
func ipv6Impact(before, after *NetworkIPv6) string {
	switch {
	case before == nil && after == nil:
		return impactNone
	case before == nil:
		return impactRestart
	case after == nil, before.Subnet != after.Subnet, before.RouterAddr() != after.RouterAddr(),
		!sameInt(before.Delegated, after.Delegated):
		return impactDisruptive
	}
	return impactNone
}

// describeNetwork renders a network as diff lines.
func describeNetwork(sign string, n Network) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%snetwork %s\n", sign, n.Name)
	if len(n.Members) > 0 {
		fmt.Fprintf(&b, "%s  on %s\n", sign, strings.Join(n.Members, ", "))
	}
	if n.IPv4 != nil && n.IPv4.Subnet.IsValid() {
		fmt.Fprintf(&b, "%s  ipv4 %s, this router at %s\n", sign, n.IPv4.Subnet, n.IPv4.RouterAddr())
	} else {
		fmt.Fprintf(&b, "%s  no ipv4\n", sign)
	}
	switch {
	case n.IPv6 != nil && n.IPv6.Delegated != nil:
		fmt.Fprintf(&b, "%s  ipv6 /64 number %d of the delegated prefix\n", sign, *n.IPv6.Delegated)
	case n.IPv6 != nil && n.IPv6.Subnet.IsValid():
		fmt.Fprintf(&b, "%s  ipv6 %s, this router at %s\n", sign, n.IPv6.Subnet, n.IPv6.RouterAddr())
	}
	return b.String()
}

func itemPath(name string) string    { return "adopted[" + name + "]" }
func networkPath(name string) string { return "networks[" + name + "]" }

// pick is a conditional expression, for the places where a three-line if would
// only separate a value from the condition that chooses it.
func pick[T any](cond bool, yes, no T) T {
	if cond {
		return yes
	}
	return no
}

// observedByName indexes the kernel's list, for the view's second half.
func observedByName(in []Interface) map[string]Interface {
	out := make(map[string]Interface, len(in))
	for _, iface := range in {
		out[iface.Name] = iface
	}
	return out
}

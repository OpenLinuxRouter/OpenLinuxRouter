package firewall

import (
	"fmt"
	"net/netip"
	"slices"
	"strings"
)

// Boundary is everything this module reads from the rest of olr.
//
// Declared here and answered in internal/daemon, like every other module's
// view, so that `firewall` imports none of the modules it reads and design.md
// §4.1's arrows keep pointing one way.
type Boundary interface {
	// Inside lists the interfaces whose traffic is trusted: every member of a
	// `link` network, and the dial-in tunnel when `remote` has one.
	Inside() ([]string, error)

	// Openings lists what other modules are serving to the outside, and so
	// what must stay reachable from there.
	Openings() ([]Opening, error)

	// IPTV reads the enabled multicast path from its owning module.
	IPTV() (IPTV, error)

	// InterfaceOf names the interface holding a local address, for the
	// lockout warning. False when no interface holds it.
	InterfaceOf(addr netip.Addr) (string, bool)
}

// mssClamper is the optional half of a Boundary: the MTUs around the box that
// decide whether a TCP MSS clamp is needed.
//
// Optional rather than a fourth method on Boundary because it is observed
// state, not something every implementation can answer — a test Boundary and a
// non-Linux box have no opinion, and the zero observation renders no clamp. The
// same shape internal/gateway uses for an optional health source.
type mssClamper interface {
	MSSClamp() (MSSClamp, bool)
}

// mssClampOf asks a Boundary for its observation, or reports that it has none.
func mssClampOf(b Boundary) (MSSClamp, bool) {
	if b == nil {
		return MSSClamp{}, false
	}
	c, ok := b.(mssClamper)
	if !ok {
		return MSSClamp{}, false
	}
	return c.MSSClamp()
}

// IPTV is the selected IPv4 multicast path, empty when disabled.
type IPTV struct {
	Upstream   string   `json:"upstream"`
	Downstream []string `json:"downstream"`
}

// Protocol is a transport.
type Protocol string

const (
	TCP Protocol = "tcp"
	UDP Protocol = "udp"

	// SixInFour is IPv6 carried in IPv4, IP protocol 41. Not a transport and
	// it has no port, so an opening for it names who it comes from instead —
	// a tunnel broker, and nobody else.
	SixInFour Protocol = "6in4"
)

// Opening is one port this router serves to the outside, and who asked for it.
//
// Nobody types these. Each is derived from an object that already exists — the
// WireGuard server listens on a port, so that port is open — which is why
// there is no way to leave one behind: remove the object and the next apply
// has one opening fewer.
type Opening struct {
	// For is who asked, in the operator's words: "remote access (WireGuard)".
	For string `json:"for"`

	Protocol Protocol `json:"protocol"`

	// Port is the destination port, zero for SixInFour.
	Port uint16 `json:"port,omitempty"`

	// From limits the opening to one source address. Set for SixInFour, where
	// letting protocol 41 in from anywhere would let anybody inject IPv6 into
	// the tunnel.
	From netip.Addr `json:"from,omitzero"`
}

// Line is the opening's canonical form. It is also the text of the rule that
// implements it, so it names the owner — `nft list table inet olr_filter`
// should say why a port is open, not just that it is.
func (o Opening) Line() string {
	if o.Protocol == SixInFour {
		return fmt.Sprintf("nft allow %s from %s for %s", o.Protocol, o.From, o.For)
	}
	return fmt.Sprintf("nft allow %s port %d for %s", o.Protocol, o.Port, o.For)
}

// Describe is the opening as a status line reads it: "udp 51820", or
// "6in4 from 216.66.80.26".
func (o Opening) Describe() string {
	if o.Protocol == SixInFour {
		return fmt.Sprintf("%s from %s", o.Protocol, o.From)
	}
	return fmt.Sprintf("%s %d", o.Protocol, o.Port)
}

// StaticBoundary is a Boundary backed by values. What the tests use.
type StaticBoundary struct {
	Interfaces []string
	Open       []Opening
	Addresses  map[netip.Addr]string
	Err        error
	Multicast  IPTV

	// Clamp is the optional path-MTU observation. Nil means this Boundary
	// cannot see the kernel, which renders no clamp.
	Clamp *MSSClamp
}

// Inside implements Boundary.
func (b StaticBoundary) IPTV() (IPTV, error) { return b.Multicast, b.Err }

func (b StaticBoundary) Inside() ([]string, error) { return b.Interfaces, b.Err }

// Openings implements Boundary.
func (b StaticBoundary) Openings() ([]Opening, error) { return b.Open, b.Err }

// MSSClamp implements the optional observation half of Boundary.
func (b StaticBoundary) MSSClamp() (MSSClamp, bool) {
	if b.Clamp == nil {
		return MSSClamp{}, false
	}
	return *b.Clamp, true
}

// InterfaceOf implements Boundary.
func (b StaticBoundary) InterfaceOf(addr netip.Addr) (string, bool) {
	name, ok := b.Addresses[addr.Unmap()]
	return name, ok
}

// normalizeInside sorts and de-duplicates, dropping blanks.
func normalizeInside(in []string) []string {
	out := make([]string, 0, len(in))
	for _, name := range in {
		if name = strings.TrimSpace(name); name != "" {
			out = append(out, name)
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// normalizeOpenings sorts and de-duplicates by (protocol, port). Two owners of
// one port is a conflict the owners' own validation reports; here the first
// name in sort order is kept, so the rule text is stable.
func normalizeOpenings(in []Opening) []Opening {
	out := append([]Opening{}, in...)
	slices.SortFunc(out, func(a, b Opening) int {
		if c := strings.Compare(string(a.Protocol), string(b.Protocol)); c != 0 {
			return c
		}
		if a.Port != b.Port {
			return int(a.Port) - int(b.Port)
		}
		if c := a.From.Compare(b.From); c != 0 {
			return c
		}
		return strings.Compare(a.For, b.For)
	})
	return slices.CompactFunc(out, func(a, b Opening) bool {
		return a.Protocol == b.Protocol && a.Port == b.Port && a.From == b.From
	})
}

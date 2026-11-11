package dns

import (
	"errors"
	"fmt"
	"net/netip"
	"sort"
)

// LinkView is this module's read-only window onto the link module.
//
// design.md §4.1 fixes the direction: dns depends on link, reads link's facts
// through link, and never keeps its own copy. The interface is declared here,
// by the consumer, for the reason internal/dhcp/link.go gives — it states
// exactly the facts dns needs rather than exposing all of link's surface, and
// dns imports nothing to get them. internal/daemon adapts link's neutral Info into it.
//
// It is a near-twin of dhcp's, with one method more, and the extra method is
// why they are not one type. dhcp names an interface and asks about it; dns
// names an *address* and has to find which interface, if any, owns it.
type LinkView interface {
	// Interface returns what is known about an interface, or
	// ErrNoSuchInterface.
	Interface(name string) (LinkInfo, error)

	// Interfaces lists everything link knows about, in a stable order.
	Interfaces() ([]LinkInfo, error)
}

// LinkInfo is the subset of an interface's state that DNS decisions depend on.
type LinkInfo struct {
	// Name is the kernel interface name.
	Name string `json:"name,omitempty"`

	// Adopted reports whether the operator handed this interface to olr
	// (design.md §7). Explicit listen addresses must belong to adopted
	// interfaces, so the operator cannot accidentally pin to an unknown one.
	Adopted bool `json:"adopted"`

	// Up reports the operational state. Not an error for configuration
	// purposes: an interface can be legitimately configured while down, so this
	// only ever produces a warning.
	Up bool `json:"up"`

	// Prefixes are the addresses configured on the interface, with masks.
	Prefixes []netip.Prefix `json:"prefixes"`

	// Network names the `link` network this interface carries, if any.
	Network string `json:"network,omitempty"`
}

// ErrNoSuchInterface is returned by LinkView.Interface for an unknown name.
var ErrNoSuchInterface = errors.New("no such interface")

// HasAddress reports whether addr is one of the interface's own addresses.
func (l LinkInfo) HasAddress(addr netip.Addr) bool {
	for _, p := range l.Prefixes {
		if p.Addr() == addr {
			return true
		}
	}
	return false
}

// FindPrefix returns the interface prefix containing addr.
func (l LinkInfo) FindPrefix(addr netip.Addr) (netip.Prefix, bool) {
	for _, p := range l.Prefixes {
		if p.Contains(addr) {
			return p, true
		}
	}
	return netip.Prefix{}, false
}

// InterfaceWithAddress finds the interface that owns an address.
//
// This is the question a listen address asks, and it is why LinkView carries
// Interfaces(): "is 192.168.1.1 ours, and is it adopted" cannot be answered by
// naming an interface, because the operator did not name one.
func InterfaceWithAddress(links LinkView, addr netip.Addr) (LinkInfo, bool) {
	all, err := links.Interfaces()
	if err != nil {
		return LinkInfo{}, false
	}
	for _, info := range all {
		if info.HasAddress(addr) {
			return info, true
		}
	}
	return LinkInfo{}, false
}

// anyAdopted reports whether the operator has handed this router anything at
// all. It separates "DNS has nothing to serve" from "DNS is misconfigured",
// which are different problems with different fixes.
func anyAdopted(links LinkView) bool {
	infos, err := links.Interfaces()
	if err != nil {
		// Unreadable is not the same as empty, and guessing "empty" here would
		// tell an operator with a working box to go adopt an interface they
		// already adopted.
		return true
	}
	for _, info := range infos {
		if info.Adopted {
			return true
		}
	}
	return false
}

// StaticLinks is a LinkView backed by a map, for tests.
//
// It used to be a production path too: olrd read a hand-written JSON file into
// one of these because there was no link module to ask. There is now, and it
// reads the kernel — so this is the fixture and nothing else.
type StaticLinks map[string]LinkInfo

// Interface implements LinkView.
func (s StaticLinks) Interface(name string) (LinkInfo, error) {
	info, ok := s[name]
	if !ok {
		return LinkInfo{}, fmt.Errorf("%q: %w", name, ErrNoSuchInterface)
	}
	if info.Name == "" {
		info.Name = name
	}
	return info, nil
}

// Interfaces implements LinkView, sorted by name so callers are deterministic.
func (s StaticLinks) Interfaces() ([]LinkInfo, error) {
	names := make([]string, 0, len(s))
	for name := range s {
		names = append(names, name)
	}
	sort.Strings(names)

	out := make([]LinkInfo, 0, len(s))
	for _, name := range names {
		info := s[name]
		if info.Name == "" {
			info.Name = name
		}
		out = append(out, info)
	}
	return out, nil
}

// DerivedNotes describes what the module filled in for a caller who did not say.
//
// Two things, both of which have to be said out loud rather than done quietly
// (design.md §5.6): the wildcard default, and the address clients that dial in
// are told to use. The second is not a default at all — it is the other half of
// a promise `remote` made when it printed a client configuration (dialin.go) —
// but it is still an address this module answers on without the operator naming
// it, and an operator reading `olr dns status` should not have to infer that
// from the absence of a pin.
func DerivedNotes(c Config, _ LinkView, dialIn DialInView) []string {
	if !c.Enabled {
		return nil
	}
	var out []string
	if len(c.Listen) == 0 {
		out = append(out, fmt.Sprintf("answering DNS on every address this router holds, port %d", DNSPort))
	}
	// An unreadable store is the renderer's problem to report, not this
	// function's: it builds sentences for a preview, and a plan that cannot be
	// rendered has already failed by the time anybody reads them.
	if service, err := ServiceListen(c, dialIn); err == nil && len(service) > 0 {
		out = append(out, fmt.Sprintf("answering DNS on %s for clients that dial in", joinAddrPorts(service)))
	}
	return out
}

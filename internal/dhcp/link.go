package dhcp

import (
	"errors"
	"fmt"
	"net/netip"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// NetworkView is this module's read-only window onto the link module.
//
// design.md §4.1 fixes the direction: dhcp depends on link, reads link's facts
// through link, and never keeps its own copy. The interface is declared here,
// by the consumer, so dhcp imports nothing — internal/daemon adapts link's
// neutral shape into this one.
//
// # Why this is keyed by network and not by interface
//
// It used to hand back an interface's *observed* prefixes, and every rule in
// validate.go was written against them. That made a stored range depend on an
// address configured outside olr: a range in a subnet the interface did not
// happen to hold was refused, with no way to change the subnet from anywhere in
// the product. §4.4 always said modules key off the network — `vlan30` is an
// implementation detail, `guest` is the thing somebody named — and `link` now
// owns one, so the subnet arrives as intent rather than as an observation.
//
// What that buys is visible in validate.go: nearly every rule became pure. The
// only facts still read from the machine are whether the members exist and
// whether they are up, and both of those are warnings.
type NetworkView interface {
	// Network returns what is known about a network, or ErrNoSuchNetwork.
	Network(name string) (NetworkInfo, error)

	// Networks lists every configured network, for the surfaces that offer a
	// choice rather than resolving one.
	Networks() ([]NetworkInfo, error)
}

// NetworkInfo is the subset of a network that DHCP decisions depend on.
type NetworkInfo struct {
	// Name is the network's name, and the pool's foreign key.
	Name string `json:"name"`

	// Members are the kernel interfaces it lives on. dnsmasq needs them for its
	// `interface=` and `constructor:` directives, which are the two places a
	// kernel name still legitimately appears in this module.
	Members []string `json:"members,omitempty"`

	// Subnet is the network's IPv4 prefix — stored intent, not an observation.
	// Invalid for a network that serves no IPv4 at all, which is a real
	// configuration: RA needs no v4 subnet.
	Subnet netip.Prefix `json:"subnet,omitempty"`

	// Router is this box's address on the network, and the default gateway a
	// client is handed when a pool does not override it.
	Router netip.Addr `json:"router,omitempty"`

	// Up reports whether every member is up. Not an error for configuration
	// purposes: a network can legitimately be configured while its cable is
	// out, so this only ever produces a warning.
	Up bool `json:"up"`
}

// ErrNoSuchNetwork is returned by NetworkView.Network for an unknown name.
var ErrNoSuchNetwork = errors.New("no such network")

// HasIPv4 reports whether the network has a subnet to serve addresses from.
func (n NetworkInfo) HasIPv4() bool { return n.Subnet.IsValid() && n.Subnet.Addr().Is4() }

// DerivedRange is the range a pool gets when nobody types one (design.md
// §11.2 — "range: derived from the network prefix; explicit overrides").
//
// Deriving matters for more than convenience. The collision DHCP cannot defend
// against is a statically configured client inside the dynamic range, and
// dnsmasq has no exclusion primitive — so the only defence is a range that
// deliberately leaves a low block free. Deriving it gives that for free;
// asking the operator to type it does not.
func (n NetworkInfo) DerivedRange() (netip.Addr, netip.Addr, bool) {
	if !n.HasIPv4() {
		return netip.Addr{}, netip.Addr{}, false
	}
	return core.SuggestRange(n.Subnet, n.Router)
}

// StaticNetworks is a NetworkView backed by a map, for tests.
//
// Validation rules are the largest thing in this module and the whole point of
// §5.3.1 is that they can be exercised without a network. That was already true
// and is more true now: with the subnet arriving as intent, a fixture is three
// fields rather than a simulated kernel.
type StaticNetworks map[string]NetworkInfo

// Network implements NetworkView.
func (s StaticNetworks) Network(name string) (NetworkInfo, error) {
	info, ok := s[name]
	if !ok {
		return NetworkInfo{}, fmt.Errorf("%q: %w", name, ErrNoSuchNetwork)
	}
	if info.Name == "" {
		info.Name = name
	}
	return info, nil
}

// Networks implements NetworkView.
func (s StaticNetworks) Networks() ([]NetworkInfo, error) {
	out := make([]NetworkInfo, 0, len(s))
	for name := range s {
		info, err := s.Network(name)
		if err != nil {
			return nil, err
		}
		out = append(out, info)
	}
	return out, nil
}

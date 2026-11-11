package link

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"slices"
	"strings"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// The join: stored consent against observed facts.
//
// This is the same shape internal/clients/list.go has for a device — an
// operator's half that is ours and stored, an observed half that is read
// through whatever observes it — and it is the only place the two meet, so
// "adoption is intent, addresses are facts" is a property of the system rather
// than something every caller has to remember.

// ErrNoSuchInterface is returned by Facts.Interface for a name the kernel does
// not have and nobody has adopted.
//
// The three consumer modules each declare their own copy of this error
// (internal/dhcp/link.go and friends). That is not duplication to remove: they
// declare the *facts they need* rather than importing ours, which is what keeps
// design.md §4.1's arrow pointing one way. The adapters in internal/daemon translate.
var ErrNoSuchInterface = errors.New("no such interface")

// Info is one interface as the rest of olr sees it.
//
// Deliberately the union of what dhcp, dns and routing each declare they need,
// and nothing more. Those three LinkInfo structs happen to be identical today;
// they are still three types, because the day one of them needs an MTU is the
// day the other two should not grow a field they do not use.
type Info struct {
	Name     string
	Adopted  bool
	Up       bool
	Prefixes []netip.Prefix

	// Present reports whether the kernel currently has this interface.
	//
	// False means it was adopted and is not there: a typo, a USB adapter that
	// is unplugged, a VLAN that has not been created yet. Distinguished from
	// "not up" because they need different answers from the operator, and
	// because flattening them would make `olr adopt eth9` look like it worked.
	Present bool

	// Network names the network this interface carries, empty when none. It
	// is how a consumer tells an interface this router *serves* from one it
	// was merely handed — the uplink is adopted too.
	Network string
}

// Facts joins the kernel's interfaces with the adopted set.
type Facts struct {
	// Source reads the observed half. Nil means Kernel.
	Source Source

	// Store is the configuration document holding the adopted set.
	Store *core.Store

	// Delegated reads the uplink's delegated prefix, for the networks
	// numbered out of it. Nil means none.
	Delegated func() netip.Prefix
}

// source resolves the nil default in one place.
func (f Facts) source() Source {
	if f.Source == nil {
		return Kernel
	}
	return f.Source
}

// Config reads the stored half.
func (f Facts) Config() (Config, error) {
	if f.Store == nil {
		return Config{}, nil
	}
	doc, err := f.Store.Load()
	if err != nil {
		return Config{}, err
	}
	return FromDocument(doc)
}

// Interfaces lists everything link knows about, in a stable order.
//
// The union of two key sets, not just the kernel's: an interface is on the list
// if it exists *or* somebody adopted it. Dropping the second would make a typo
// in `olr adopt` invisible — the name would be stored, every module would keep
// refusing to use it, and the one screen that could explain why would show
// nothing at all.
func (f Facts) Interfaces() ([]Info, error) {
	observed, err := f.source()()
	if err != nil {
		return nil, err
	}
	cfg, err := f.Config()
	if err != nil {
		return nil, err
	}
	return Join(cfg, observed), nil
}

// Join is the join itself, as a function of its two inputs.
//
// Pure, and separate from Interfaces, so that a caller which has already read
// both halves — the HTTP list handler, which needs the same `observed` to
// validate against — does not read them a second time. Reading twice would not
// merely be wasteful: the two reads could disagree, and the list would then be
// validated against a machine state it is not describing.
func Join(cfg Config, observed []Interface) []Info {
	out := make([]Info, 0, len(observed)+len(cfg.Adopted))
	seen := make(map[string]bool, len(observed))
	for _, iface := range observed {
		seen[iface.Name] = true
		info := Info{
			Name:     iface.Name,
			Adopted:  cfg.IsAdopted(iface.Name),
			Up:       iface.Up,
			Prefixes: iface.Prefixes,
			Present:  true,
		}
		if n, ok := cfg.NetworkFor(iface.Name); ok {
			info.Network = n.Name
		}
		out = append(out, info)
	}
	for _, name := range cfg.Adopted {
		if !seen[name] {
			info := Info{Name: name, Adopted: true}
			if n, ok := cfg.NetworkFor(name); ok {
				info.Network = n.Name
			}
			out = append(out, info)
		}
	}

	// Absent interfaces sort to the end, after loopback: they are the least
	// useful rows and the ones most likely to be a mistake, so they read as a
	// footnote rather than as part of the machine's inventory.
	slices.SortStableFunc(out, func(a, b Info) int {
		if a.Present != b.Present {
			if a.Present {
				return -1
			}
			return 1
		}
		return 0
	})
	return out
}

// Interface returns what is known about one interface, or ErrNoSuchInterface.
func (f Facts) Interface(name string) (Info, error) {
	all, err := f.Interfaces()
	if err != nil {
		return Info{}, err
	}
	for _, info := range all {
		if info.Name == name {
			return info, nil
		}
	}
	return Info{}, fmt.Errorf("%q: %w", name, ErrNoSuchInterface)
}

// --- the development override ----------------------------------------------

// fileInterface is one entry of the `--links` file.
type fileInterface struct {
	Name     string   `json:"name,omitempty"`
	Up       bool     `json:"up"`
	Running  bool     `json:"running,omitempty"`
	Loopback bool     `json:"loopback,omitempty"`
	MAC      string   `json:"mac,omitempty"`
	Prefixes []string `json:"prefixes,omitempty"`

	// Adopted is accepted and ignored.
	//
	// The file used to carry adoption, because there was nowhere else to put it.
	// There is now — the configuration document — and having two sources for one
	// fact is what §4.1 forbids, so this field is parsed only so that an
	// existing file does not fail to load. Adopt through the UI or `olr adopt`.
	Adopted bool `json:"adopted,omitempty"`
}

// LoadFile reads interface facts from a JSON file keyed by interface name:
//
//	{"lan0": {"up": true, "prefixes": ["192.168.1.1/24"]}}
//
// This is the `--links` flag, and it is a development affordance rather than a
// deployment one: it lets olrd run against a plausible network on a machine that
// has none, which is what `make dev` needs. A packaged install reads the kernel.
func LoadFile(path string) (Source, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading interface facts: %w", err)
	}

	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var raw map[string]fileInterface
	if err := dec.Decode(&raw); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}

	out := make([]Interface, 0, len(raw))
	for name, entry := range raw {
		if entry.Name != "" {
			name = entry.Name
		}
		iface := Interface{
			Name:         name,
			Up:           entry.Up,
			Running:      entry.Running || entry.Up,
			Loopback:     entry.Loopback,
			HardwareAddr: entry.MAC,
		}
		for _, p := range entry.Prefixes {
			prefix, err := netip.ParsePrefix(strings.TrimSpace(p))
			if err != nil {
				return nil, fmt.Errorf("parsing %s: %s: %w", path, name, err)
			}
			iface.Prefixes = append(iface.Prefixes, prefix)
		}
		out = append(out, iface)
	}
	sortInterfaces(out)

	// Read once and closed over. The file is a fixture, not a system to observe,
	// so re-reading it per request would only add a way for `make dev` to break
	// halfway through a session.
	return func() ([]Interface, error) { return out, nil }, nil
}

// NetworkInfo is one network joined to the state of its members.
//
// The stored half is the network — name, members, subnet, router address. The
// observed half is exactly one boolean: whether every member is up. That
// asymmetry is the point of the whole object. A consumer asking "what subnet is
// this network" gets an answer that does not depend on the machine, which is
// what lets `dhcp` validate a range without a kernel; a consumer asking "is it
// working" still gets the truth, read fresh.
type NetworkInfo struct {
	Name    string
	Members []string

	// Subnet and Router are intent. Invalid when the network serves no IPv4.
	Subnet netip.Prefix
	Router netip.Addr

	// Subnet6 and Router6 are the network's IPv6 intent, resolved against the
	// delegated prefix for a delegated network. Invalid when the network has no
	// ipv6 block, or its prefix has not been delegated yet — which is not "no IPv6": a delegated
	// prefix on the member is still there, and still served by `dhcp`'s RA.
	Subnet6 netip.Prefix
	Router6 netip.Addr

	// Up reports that every member exists and is administratively up. A network
	// with no members is not up: there is nothing for it to be up on.
	Up bool
}

// Networks returns every configured network, joined to its members' state.
func (f Facts) Networks() ([]NetworkInfo, error) {
	observed, err := f.source()()
	if err != nil {
		return nil, err
	}
	cfg, err := f.Config()
	if err != nil {
		return nil, err
	}

	byName := make(map[string]Interface, len(observed))
	for _, iface := range observed {
		byName[iface.Name] = iface
	}

	out := make([]NetworkInfo, 0, len(cfg.Networks))
	for _, n := range cfg.Networks {
		info := NetworkInfo{
			Name:    n.Name,
			Members: slices.Clone(n.Members),
			Up:      len(n.Members) > 0,
		}
		for _, m := range n.Members {
			if iface, ok := byName[m]; !ok || !iface.Up {
				info.Up = false
			}
		}
		if n.IPv4 != nil && n.IPv4.Subnet.IsValid() {
			info.Subnet = n.IPv4.Subnet
			info.Router = n.IPv4.RouterAddr()
		}
		if n.IPv6 != nil {
			var delegated netip.Prefix
			if f.Delegated != nil {
				delegated = f.Delegated()
			}
			if subnet, router, ok := n.IPv6.Resolve(delegated); ok {
				info.Subnet6, info.Router6 = subnet, router.Addr()
			}
		}
		out = append(out, info)
	}
	return out, nil
}

// ErrNoSuchNetwork is returned by Network for an unknown name.
var ErrNoSuchNetwork = errors.New("no such network")

// Network returns one network, or ErrNoSuchNetwork.
func (f Facts) Network(name string) (NetworkInfo, error) {
	all, err := f.Networks()
	if err != nil {
		return NetworkInfo{}, err
	}
	for _, n := range all {
		if n.Name == name {
			return n, nil
		}
	}
	return NetworkInfo{}, fmt.Errorf("%q: %w", name, ErrNoSuchNetwork)
}

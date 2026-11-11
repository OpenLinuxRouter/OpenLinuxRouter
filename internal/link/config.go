// Package link owns which interfaces the operator has handed to olr, and the
// networks those interfaces carry.
//
// # What this is, and what it is not
//
// design.md §9 milestone 1 owes a `link` module that brings up interfaces,
// carries addressing, and lands the network object (§4.4) every module after it
// keys off. This is now most of that module:
//
//   - **Adoption** — the list of interface names the operator handed over. Pure
//     consent: storing it changes nothing on the box. It exists because the
//     adopt-only rule (§3.4, §7) was being enforced by three modules against a
//     fact that had nowhere to live, and a safety rule nobody can satisfy is
//     not a safety rule.
//   - **Networks** (§4.4). A network names a subnet and this box's address on
//     it, and applying one *does* reach the kernel. This is the half
//     that was missing, and its absence had a specific cost: `dhcp` could only
//     validate a range against whatever address an interface had been given
//     from outside olr, so an operator who wanted a different subnet met an
//     error that no page in olr could fix. olrd puts the address back when it
//     starts (Applier.Restore), because the kernel forgets it on a reboot and
//     nothing else on the box knows it.
//
// Still owed, and deliberately not invented here: bridges, VLANs, and taking an
// interface away from NetworkManager, systemd-networkd or ifupdown. The last of
// those has a cost design.md §7 states: until it lands, a member interface has
// two managers that do not know about each other, and only the operator can
// make them agree — docs/install.md says how. `Network.Members` is a list so that
// the day bridging lands the schema is already right, but Validate requires
// exactly one member until something exists that can create a bridge device.
//
// The observed half of an interface's facts — its current addresses, whether it
// is up — is still read from the kernel per request and never stored. There is
// no copy to drift (§4.5). What a network stores is *intent*: the subnet we want,
// not the subnet that is there. Apply is what closes the gap between them, and
// the difference between the two is drift rather than a second opinion.
package link

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/netip"
	"slices"
	"strings"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// ModuleName is the path segment, config section and event label for this
// module.
const ModuleName = "link"

// MaxInterfaceNameLen is the kernel's IFNAMSIZ minus the terminator. A longer
// name cannot name an interface that exists, so accepting one would only defer
// the complaint to a place with less context.
const MaxInterfaceNameLen = 15

// MaxNetworkNameLen bounds a network name.
//
// dnsmasq tags are built from it (`dhcp-range=set:<name>,...`), it appears in a
// URL path, and it is typed on a command line. 32 is comfortably more than any
// of those need and short enough that a list of them lines up in a terminal.
const MaxNetworkNameLen = 32

// Config is the link module's intent.
//
// One field, and it is a list of names rather than a list of objects. That is a
// bet worth stating: the day this grows an address or a network it becomes a list
// of structs, and the migration is a shim in UnmarshalConfig. Making it a struct
// today to avoid that would mean inventing the field set §10 decision 7 says is
// still open, in the module least able to change it later.
type Config struct {
	// Adopted names the interfaces the operator has handed to olr.
	//
	// Adoption is consent, not configuration: it changes nothing on the box by
	// itself. What it does is let the other modules act on an interface — a
	// pool, a resolver, an exit — which is why removing a name here can make
	// another module's stored config invalid. That is reported by that module,
	// against its own field, rather than guarded here; see Validate.
	Adopted []string `json:"adopted,omitempty"`

	// IPv6Auto controls automatic IPv6 addressing per adopted interface.
	// Absent leaves the host's setting alone. False stops SLAAC and the host
	// DHCPv6 client; true enables them. Static addresses are not removed.
	IPv6Auto map[string]bool `json:"ipv6_auto,omitempty"`

	// Networks are the LANs this box serves (design.md §4.4).
	//
	// Unlike Adopted, a network *is* configuration: it says what the subnet is
	// and what address this router holds on it, and applying one writes that
	// address to the kernel. That is the whole point — before networks existed
	// there was nowhere in olr to say "this LAN is 172.16.1.0/24", so `dhcp`
	// had to validate a range against whatever address the interface happened
	// to have been given from outside olr, and an operator who wanted a
	// different subnet met an error with no page behind it.
	Networks []Network `json:"networks,omitempty"`
}

// Network is one network: the object `dhcp`, `dns` and later `firewall` and
// `wifi` all key off (design.md §4.4).
//
// One word at every layer — the schema, the API, `olr net add iot` and the page
// all say *network*. It used to be `group` below the UI, and a word the
// operator never sees is a word every reader of the schema has to translate;
// "group" is now kept for sets of clients, which is what it sounds like. A
// document still carrying the old keys is read by UnmarshalConfig and
// rewritten once at startup.
type Network struct {
	// Name is the primary key, and it is operator-chosen on purpose. `vlan30`
	// is an implementation detail; `guest` is the thing somebody named and the
	// thing that survives being moved to a different bridge.
	Name string `json:"name"`

	// Members are the adopted interfaces this network lives on.
	//
	// A list because §4.4 says a network has bridge members, and the day bridging
	// lands this field is already the right shape. Until then Validate requires
	// exactly one: more than one member needs a bridge device that something
	// has to create, and inventing the schema for that without building it
	// would be the retrofit §9 warns about in miniature.
	Members []string `json:"members"`

	// IPv4 is nil for a network that serves no IPv4 at all — which is a real
	// configuration now that `dhcp` can serve RA on its own, and was not
	// expressible before.
	IPv4 *NetworkIPv4 `json:"ipv4,omitempty"`

	// IPv6 is nil for a network whose IPv6, if any, comes from somewhere else
	// — an address the distribution put on the member, which dnsmasq's
	// `constructor:` follows on its own. Set, it is a /64 that olr writes to
	// every member, the same way IPv4 is: either static (a tunnel broker's
	// routed prefix, a ULA), or numbered out of the prefix the ISP delegated to
	// the uplink, which olr follows when the ISP renumbers.
	//
	// The claim it makes is narrower than IPv4's. olr owns *its own* v6 address
	// on a member — adds it, and takes it off when the network stops asking for
	// it — and leaves every other v6 address alone. A member routinely holds
	// SLAAC and delegated addresses olr never configured, and taking those off
	// because a network did not name them would cut the box off in exactly the
	// setups that already work.
	IPv6 *NetworkIPv6 `json:"ipv6,omitempty"`
}

// NetworkIPv4 is a network's IPv4 addressing.
type NetworkIPv4 struct {
	// Subnet is the network, as the operator thinks of it: 172.16.1.0/24.
	//
	// This is the field whose absence started all of this. It is stored intent,
	// not an observation, which is what lets `dhcp` validate a range against it
	// without consulting the kernel at all.
	Subnet netip.Prefix `json:"subnet"`

	// Router is this box's own address on the network. Nil derives the first
	// host address, which is `.1` on every ordinary prefix — design.md §11.2
	// makes that a requirement rather than a nicety, since `olr net add iot`
	// has to work with no flags at all.
	Router *netip.Addr `json:"router,omitempty"`
}

// NetworkIPv6 is a network's IPv6 addressing: a static Subnet, or the
// Delegated'th /64 of the uplink's delegated prefix. Exactly one of the two.
type NetworkIPv6 struct {
	// Subnet is the network's /64: 2001:db8:1:1::/64. Exactly /64, because
	// that is what SLAAC works on and a network whose clients cannot address
	// themselves is not one `dhcp` can serve.
	Subnet netip.Prefix `json:"subnet,omitzero"`

	// Delegated numbers the network's /64 out of the prefix the ISP delegated
	// to the uplink (dial's prefix delegation): 0 is the first /64, 1 the
	// second. A /60 has sixteen, a /56 two hundred and fifty-six.
	//
	// A number rather than a prefix because the prefix is the ISP's and
	// changes; the number is the operator's and does not. When the ISP
	// renumbers, every delegated network moves with it and keeps its place.
	Delegated *int `json:"delegated,omitempty"`

	// Router is this box's own address on the network. Nil derives `::1`,
	// the same convention IPv4's `.1` follows. Static subnets only: a pinned
	// address inside a prefix the ISP can change would be wrong the first time
	// it did.
	Router *netip.Addr `json:"router,omitempty"`
}

// Resolve is the network's /64 and this router's address in it, against the
// uplink's delegated prefix. False when there is nothing to write: a delegated
// network with no prefix delegated yet, or a number past its end.
func (n NetworkIPv6) Resolve(delegated netip.Prefix) (subnet, router netip.Prefix, ok bool) {
	if n.Delegated == nil {
		if !n.Subnet.IsValid() {
			return netip.Prefix{}, netip.Prefix{}, false
		}
		return n.Subnet, n.RouterPrefix(), true
	}
	subnet, ok = NthSubnet64(delegated, *n.Delegated)
	if !ok {
		return netip.Prefix{}, netip.Prefix{}, false
	}
	return subnet, netip.PrefixFrom(FirstHost6(subnet), 64), true
}

// NthSubnet64 is the n'th /64 of a prefix /64 or shorter.
func NthSubnet64(p netip.Prefix, n int) (netip.Prefix, bool) {
	if !p.IsValid() || !p.Addr().Is6() || p.Bits() > 64 || n < 0 {
		return netip.Prefix{}, false
	}
	if room := 64 - p.Bits(); room < 63 && uint64(n) >= uint64(1)<<room {
		return netip.Prefix{}, false
	}
	a := p.Masked().Addr().As16()
	hi := binary.BigEndian.Uint64(a[:8]) | uint64(n)
	binary.BigEndian.PutUint64(a[:8], hi)
	return netip.PrefixFrom(netip.AddrFrom16(a), 64), true
}

// RouterAddr resolves the stored or derived router address.
func (n NetworkIPv6) RouterAddr() netip.Addr {
	if n.Router != nil && n.Router.IsValid() {
		return *n.Router
	}
	return FirstHost6(n.Subnet)
}

// RouterPrefix is the router address with the network's mask.
func (n NetworkIPv6) RouterPrefix() netip.Prefix {
	return netip.PrefixFrom(n.RouterAddr(), n.Subnet.Bits())
}

// FirstHost6 is `::1` in a prefix. The all-zeros address is the subnet-router
// anycast address (RFC 4291 §2.6.1), so it is not a host to hand the router.
func FirstHost6(p netip.Prefix) netip.Addr {
	if !p.IsValid() || !p.Addr().Is6() {
		return netip.Addr{}
	}
	return p.Masked().Addr().Next()
}

// RouterAddr resolves the stored or derived router address.
func (n NetworkIPv4) RouterAddr() netip.Addr {
	if n.Router != nil && n.Router.IsValid() {
		return *n.Router
	}
	addr, _ := core.FirstHost(n.Subnet)
	return addr
}

// RouterPrefix is the router address with the network's mask — the form
// netlink wants when adding an address to an interface.
func (n NetworkIPv4) RouterPrefix() netip.Prefix {
	return netip.PrefixFrom(n.RouterAddr(), n.Subnet.Bits())
}

// Network returns a network by name.
func (c Config) Network(name string) (Network, bool) {
	i := slices.IndexFunc(c.Networks, func(n Network) bool { return n.Name == name })
	if i < 0 {
		return Network{}, false
	}
	return c.Networks[i], true
}

// SetNetwork adds or replaces a network, keyed by name.
func (c *Config) SetNetwork(n Network) {
	if i := slices.IndexFunc(c.Networks, func(e Network) bool { return e.Name == n.Name }); i >= 0 {
		c.Networks[i] = n
		return
	}
	c.Networks = append(c.Networks, n)
	c.Normalize()
}

// RemoveNetwork drops a network, reporting whether there was one.
//
// It does not check whether `dhcp` still references it. That is the same
// asymmetry Adopted has and it is deliberate: the module that stored the
// reference is the one that can explain what breaks, and it reports the
// problem against its own field. Guarding here would mean `link` importing
// every module that reads it, which is exactly the arrow §4.1 forbids.
func (c *Config) RemoveNetwork(name string) bool {
	i := slices.IndexFunc(c.Networks, func(n Network) bool { return n.Name == name })
	if i < 0 {
		return false
	}
	c.Networks = slices.Delete(c.Networks, i, i+1)
	return true
}

// NetworkFor returns the network an interface belongs to.
//
// The reverse lookup, for the surfaces that start from an interface row — the
// interface list has to say "ens18 carries lan", and without this it would have
// to walk every network itself.
func (c Config) NetworkFor(iface string) (Network, bool) {
	for _, n := range c.Networks {
		if slices.Contains(n.Members, iface) {
			return n, true
		}
	}
	return Network{}, false
}

// Normalize puts the config in canonical form: trimmed, deduplicated, sorted.
//
// Not lowercased. Interface names are case-sensitive on Linux, so `eth0` and
// `ETH0` are two different interfaces and folding them would silently adopt one
// the operator did not name.
//
// Sorting matters for the same reason it does in every other module: the
// document is compared as bytes downstream, so a reordered array would read as
// a change nobody made.
func (c *Config) Normalize() {
	out := make([]string, 0, len(c.Adopted))
	seen := map[string]bool{}
	for _, name := range c.Adopted {
		name = strings.TrimSpace(name)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	slices.Sort(out)

	// nil rather than an empty slice, so that a config with nothing adopted
	// marshals as an absent key rather than `"adopted": []`. The two mean the
	// same thing and only one of them is worth writing to a file.
	if len(out) == 0 {
		c.Adopted = nil
	} else {
		c.Adopted = out
	}

	c.normalizeNetworks()
}

// normalizeNetworks canonicalises the network list: names and members trimmed,
// members deduplicated, both sorted.
//
// The subnet is masked as well, so that `172.16.1.5/24` typed into a form is
// stored as `172.16.1.0/24`. Without that the same network could be written two
// ways, and every downstream byte comparison would call the difference drift.
func (c *Config) normalizeNetworks() {
	out := make([]Network, 0, len(c.Networks))
	seen := map[string]bool{}
	for _, n := range c.Networks {
		n.Name = strings.TrimSpace(n.Name)
		if n.Name == "" || seen[n.Name] {
			continue
		}
		seen[n.Name] = true

		members := make([]string, 0, len(n.Members))
		member := map[string]bool{}
		for _, m := range n.Members {
			m = strings.TrimSpace(m)
			if m == "" || member[m] {
				continue
			}
			member[m] = true
			members = append(members, m)
		}
		slices.Sort(members)
		if len(members) == 0 {
			members = nil
		}
		n.Members = members

		if n.IPv4 != nil && n.IPv4.Subnet.IsValid() {
			v4 := *n.IPv4
			v4.Subnet = v4.Subnet.Masked()
			// An explicit router that equals what would be derived is dropped,
			// so the stored file says `.1` exactly once — in the subnet — and a
			// form that helpfully fills the field in does not turn a default
			// into a pin.
			if v4.Router != nil {
				if derived, ok := core.FirstHost(v4.Subnet); ok && *v4.Router == derived {
					v4.Router = nil
				}
			}
			n.IPv4 = &v4
		}

		if n.IPv6 != nil {
			v6 := *n.IPv6
			if v6.Subnet.IsValid() {
				v6.Subnet = v6.Subnet.Masked()
			}
			if v6.Router != nil && *v6.Router == FirstHost6(v6.Subnet) {
				v6.Router = nil
			}
			n.IPv6 = &v6
		}

		out = append(out, n)
	}
	slices.SortStableFunc(out, func(a, b Network) int { return strings.Compare(a.Name, b.Name) })

	if len(out) == 0 {
		c.Networks = nil
		return
	}
	c.Networks = out
}

// Clone deep-copies the config, so a proposal can be diffed against the stored
// one without either sharing a backing array.
func (c Config) Clone() Config {
	out := Config{Adopted: slices.Clone(c.Adopted)}
	if c.IPv6Auto != nil {
		out.IPv6Auto = make(map[string]bool, len(c.IPv6Auto))
		for name, enabled := range c.IPv6Auto {
			out.IPv6Auto[name] = enabled
		}
	}
	if c.Networks != nil {
		out.Networks = make([]Network, len(c.Networks))
		for i, n := range c.Networks {
			n.Members = slices.Clone(n.Members)
			if n.IPv4 != nil {
				v4 := *n.IPv4
				if v4.Router != nil {
					addr := *v4.Router
					v4.Router = &addr
				}
				n.IPv4 = &v4
			}
			out.Networks[i] = n
		}
	}
	return out
}

// IsAdopted reports whether the operator handed us this interface.
func (c Config) IsAdopted(name string) bool {
	return slices.Contains(c.Adopted, strings.TrimSpace(name))
}

// Adopt adds an interface, reporting whether anything changed.
//
// Idempotent, and that is load-bearing rather than tidy: `olr adopt eth0` run
// twice should be a no-op with a calm message, not an error the operator has to
// read carefully to discover was harmless.
func (c *Config) Adopt(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" || c.IsAdopted(name) {
		return false
	}
	c.Adopted = append(c.Adopted, name)
	c.Normalize()
	return true
}

// Release drops an interface, reporting whether it was there.
func (c *Config) Release(name string) bool {
	name = strings.TrimSpace(name)
	i := slices.Index(c.Adopted, name)
	if i < 0 {
		return false
	}
	c.Adopted = slices.Delete(c.Adopted, i, i+1)
	delete(c.IPv6Auto, name)
	c.Normalize()
	return true
}

// Empty reports whether the module has been configured at all.
func (c Config) Empty() bool { return len(c.Adopted) == 0 && len(c.Networks) == 0 }

// UnmarshalConfig parses a config, rejecting unknown fields.
//
// Strict for the reason internal/dhcp/config.go gives: a typo'd key that is
// silently ignored produces a box that is quietly not doing what its config
// says, and here it would produce one that quietly adopted nothing.
//
// The one key it translates rather than refuses is `groups`, which is what
// Networks was called until the word was freed for sets of devices. A key we
// used to write is not a typo, and refusing it would be a box that stops
// loading its own file on upgrade. See HasLegacyKeys for the other half.
func UnmarshalConfig(data []byte) (Config, error) {
	data, _, err := renameLegacyKeys(data)
	if err != nil {
		return Config{}, fmt.Errorf("parsing link config: %w", err)
	}

	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()

	var c Config
	if err := dec.Decode(&c); err != nil {
		return Config{}, fmt.Errorf("parsing link config: %w", err)
	}
	c.Normalize()
	return c, nil
}

// legacyNetworksKey is what Networks was stored as before the rename.
const legacyNetworksKey = "groups"

// renameLegacyKeys rewrites the old spelling of Networks to the current one.
func renameLegacyKeys(data []byte) ([]byte, bool, error) {
	return core.RenameKey(data, legacyNetworksKey, "networks")
}

// HasLegacyKeys reports whether a stored subtree still spells Networks the old
// way.
//
// UnmarshalConfig reads such a subtree without complaint, which is exactly why
// something else has to ask: internal/daemon checks this at startup and
// rewrites the document once, so that `olr.json` — the file design.md §10 says
// you SSH in and read — does not keep a key no current page or command uses.
func HasLegacyKeys(data []byte) bool {
	_, renamed, _ := renameLegacyKeys(data)
	return renamed
}

// MarshalConfig renders this module's subtree of the document.
//
// No "$schema" key: the document has exactly one, written by the store, and a
// second nested inside a module's subtree would be a claim this module is not
// in a position to make. Indentation is likewise the store's — it re-indents
// the whole document in one pass.
func MarshalConfig(c Config) ([]byte, error) {
	c.Normalize()
	return json.Marshal(c)
}

// FromDocument reads this module's subtree out of the configuration document.
//
// A document without a "link" key is not an error — it means nothing has been
// adopted yet, which is exactly what a fresh install looks like and what the
// interface list must render as an empty state rather than a failure.
func FromDocument(d core.Document) (Config, error) {
	raw, ok := d.Raw(ModuleName)
	if !ok {
		return Config{}, nil
	}
	c, err := UnmarshalConfig(raw)
	if err != nil {
		return Config{}, fmt.Errorf("%s configuration: %w", ModuleName, err)
	}
	return c, nil
}

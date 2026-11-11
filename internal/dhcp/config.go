package dhcp

import (
	"encoding/json"
	"fmt"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// DefaultLeaseTime applies to any pool that does not set one. dnsmasq's own
// default is one hour, which is short enough that a busy LAN renews constantly.
const DefaultLeaseTime = Duration(12 * time.Hour)

// MinLeaseTime is dnsmasq's floor, not ours: it rejects anything shorter.
const MinLeaseTime = Duration(2 * time.Minute)

// Config is the dhcp module's intent — what the operator asked for, not what is
// running. It is the single source for the CLI flags, the REST body, the UI
// form, and the MCP tool definition (design.md §3.2 rule 3), so a field added
// here appears on every surface without further work.
//
// Fields without `omitempty` are reflected as schema-required (design.md §10,
// config format), so the tags are load-bearing.
type Config struct {
	// Enabled controls whether the DHCP server runs at all. Disabling stops the
	// service but keeps the configuration, so it can be turned back on without
	// retyping it.
	Enabled bool `json:"enabled"`

	// Pools holds at most one pool per network (design.md §11.4 — multiple
	// pools per network is permanently the escape hatch's job).
	Pools []Pool `json:"pools,omitempty"`

	// Reservations are deliberately global rather than nested under a pool.
	// dnsmasq matches a dhcp-host by MAC across every range at once, so
	// modelling them per-pool would invent an ownership that the daemon does
	// not actually have.
	Reservations []Reservation `json:"reservations,omitempty"`

	// ExtraConf is the module's declared escape hatch (design.md §3.2 rule 5):
	// appended verbatim to the rendered dnsmasq config. It is revisioned with
	// everything else, which is the whole point — editing the daemon's file out
	// of band is what this exists to make unnecessary.
	//
	// It may not set directives the module renders itself; see validateExtra.
	ExtraConf string `json:"extra_dnsmasq_conf,omitempty"`

	// Legacy carries the pre-0.3 pools when the document held them, so that the
	// daemon can create the networks they imply and finish keying them.
	//
	// Not serialised: it exists between parsing an old document and rewriting a
	// current one, and a `-` tag is what keeps it out of both the stored file
	// and the published schema. See unmarshalLegacy.
	Legacy []LegacyPool `json:"-"`
}

// Pool is what one network serves.
//
// # Why IPv4 and IPv6 are separate objects
//
// They were one: an IPv4 range with an `ra` field bolted onto it. Two arguments
// against that, pointing the same way.
//
// The operator's is that these are two decisions. "Hand out 172.16.1.100–200"
// and "advertise the prefix so clients configure themselves" have nothing in
// common except the network they happen on, and a form that puts the second in
// a dropdown beside the lease time is claiming otherwise.
//
// The backend's is that dnsmasq already models them apart: an IPv6 range is its
// own `dhcp-range` line (`dhcp-range=::,constructor:ens18,ra-stateless`). The
// old shape collapsed two things into one and made render.go split them back
// out, leaving us a layer of indirection further from the daemon than the
// daemon is from itself.
//
// It also makes a configuration expressible that was not: a network that serves
// only RA. The old Pool required IPv4 Start and End, so "no v4 here, just
// advertise the prefix" could not be written down at all.
type Pool struct {
	// Network is the pool's primary key: the network it serves (design.md §4.4).
	//
	// It used to be the kernel interface name — the thing §4.4 calls an
	// implementation detail. Keying on it meant a range was checked against
	// whatever address the interface had been given from outside olr.
	Network string `json:"network"`

	// IPv4 is nil for a network that hands out no IPv4 addresses.
	IPv4 *PoolIPv4 `json:"ipv4,omitempty"`

	// IPv6 is nil for RAOff.
	IPv6 *PoolIPv6 `json:"ipv6,omitempty"`

	// LeaseTime is zero for DefaultLeaseTime.
	//
	// Shared rather than per-family, like everything below it. dnsmasq writes
	// these onto each range separately, but they are one decision to an operator
	// — "how long a lease lasts here" is not a question anybody answers twice.
	LeaseTime Duration `json:"lease_time,omitempty"`

	// Gateway is nil for "the network's router address", which is what almost
	// every deployment wants. Set it only to hand clients a different next hop.
	//
	// IPv4 only, and not for want of symmetry: DHCPv6 has no default-gateway
	// option and never had one (design.md §4.3). IPv6 hosts learn their next hop
	// from router advertisement and nowhere else.
	Gateway *netip.Addr `json:"gateway,omitempty"`

	// DNS is nil for "the router itself". Note that the router answering DNS is
	// the dns module's job, not ours (design.md §4.2) — we only advertise the
	// address. Both families may appear here; render.go sorts them onto the
	// right range.
	DNS []netip.Addr `json:"dns,omitempty"`

	// Domain is DHCP option 15, the client's search domain.
	Domain string `json:"domain,omitempty"`

	// NTP is DHCP option 42.
	NTP []netip.Addr `json:"ntp,omitempty"`

	// Options are additional DHCP options, the per-pool escape hatch. Rendered
	// into the reloadable options directory, so changing one does not restart
	// the daemon.
	Options []Option `json:"options,omitempty"`
}

// PoolIPv4 is the dynamic IPv4 range a network hands out.
type PoolIPv4 struct {
	// Start and End bound the range, inclusive. Both absent means "derive it
	// from the network's subnet" (design.md §11.2) — see NetworkInfo.DerivedRange
	// for why deriving is a safety property rather than a convenience.
	//
	// They are checked against the network's *stored* subnet. Nothing here
	// consults the kernel, which is the point: a range is wrong because it
	// contradicts the network it is on, not because an interface somewhere is
	// holding a different address.
	Start netip.Addr `json:"start,omitempty"`
	End   netip.Addr `json:"end,omitempty"`
}

// Explicit reports whether the operator typed a range rather than leaving it
// derived.
func (p PoolIPv4) Explicit() bool { return p.Start.IsValid() || p.End.IsValid() }

// PoolIPv6 is what a network advertises over IPv6.
type PoolIPv6 struct {
	// Mode selects the behaviour. Empty means RAOff.
	//
	// There is no range here, and that is not an omission. dnsmasq's
	// `constructor:` derives the prefix from the member interface's own address,
	// so a delegated prefix that changes is followed by the daemon rather than
	// re-rendered by us. A literal IPv6 range would have to be retyped every
	// time the uplink's delegation moved.
	Mode RAMode `json:"mode,omitempty"`

	// MTU is advertised in the router advertisement, so clients size their
	// packets to it rather than to their own link. Zero advertises none, and
	// clients use the interface's own MTU.
	//
	// Typed, not read off the uplink. It matters when IPv6 leaves by a tunnel
	// narrower than the LAN — 1480 for a 6in4 tunnel under 1500 — and that is
	// `dial`'s fact; this module does not go and find it (design.md §4.3). The
	// tunnel still works without it, through Packet Too Big, until something
	// on the path drops ICMPv6 and large transfers stall.
	MTU int `json:"mtu,omitempty"`
}

// LeaseTimeOrDefault resolves the zero value.
func (p Pool) LeaseTimeOrDefault() Duration {
	if p.LeaseTime <= 0 {
		return DefaultLeaseTime
	}
	return p.LeaseTime
}

// RA resolves the IPv6 block to a mode, RAOff when there is none.
func (p Pool) RA() RAMode {
	if p.IPv6 == nil {
		return RAOff
	}
	return p.IPv6.Mode.OrDefault()
}

// ServesIPv4 reports whether the pool hands out IPv4 addresses at all.
func (p Pool) ServesIPv4() bool { return p.IPv4 != nil }

// Range resolves the pool's IPv4 range against its network, deriving one when
// the operator did not type it.
//
// One place rather than three. The renderer, the validator and the lease
// accounting all need the resolved range, and a derived value computed
// separately in each is a derived value that eventually differs in one.
func (p Pool) Range(n NetworkInfo) (netip.Addr, netip.Addr, bool) {
	if p.IPv4 == nil {
		return netip.Addr{}, netip.Addr{}, false
	}
	if p.IPv4.Explicit() {
		return p.IPv4.Start, p.IPv4.End, p.IPv4.Start.IsValid() && p.IPv4.End.IsValid()
	}
	return n.DerivedRange()
}

// Reservation pins one client to one address.
type Reservation struct {
	// MAC is the client's hardware address. Normalize lowercases it so that
	// diffs do not churn on capitalisation.
	MAC string `json:"mac"`

	// IP must sit in the same subnet as some pool, though dnsmasq explicitly
	// allows it to fall outside that pool's dynamic range — and outside is the
	// better habit, since it cannot then collide with a dynamic allocation.
	IP netip.Addr `json:"ip"`

	// Hostname is optional. When set it is also the name dnsmasq will use for
	// the client, which is what the dns module will later publish.
	Hostname string `json:"hostname,omitempty"`

	// LeaseTime is zero for the enclosing pool's lease time.
	LeaseTime Duration `json:"lease_time,omitempty"`
}

// Option is one raw DHCP option.
type Option struct {
	// Option is either a number ("42") or one of dnsmasq's option names
	// ("ntp-server"). We pass it through rather than maintaining our own table
	// of the ~100 DHCP options, which would go stale and add nothing.
	Option string `json:"option"`

	// Value is passed through verbatim, commas and all, so multi-value options
	// work without special-casing.
	Value string `json:"value"`
}

// RAMode selects IPv6 behaviour on a pool's interface.
//
// IPv6 is a dimension, not a module (design.md §4.3), so it lives here as a
// field rather than anywhere more ceremonious.
type RAMode string

// Each mode names a documented dnsmasq behaviour rather than a synonym for one,
// so there is no gap between what the field promises and what the daemon does.
const (
	// RAOff serves no IPv6 at all.
	RAOff RAMode = "off"

	// RASLAAC advertises the prefix, lets clients self-assign an address from
	// it, and answers DHCPv6 information requests so they still get DNS and
	// NTP from us. dnsmasq's ra-stateless: the O and A bits.
	//
	// This is the mode most networks want, and it needs nothing from the dial
	// module — dnsmasq's constructor: syntax derives the prefix from the
	// interface's own address, so a delegated prefix that changes is followed
	// automatically instead of having to be re-rendered.
	RASLAAC RAMode = "slaac"

	// RAStateful additionally hands out addresses over DHCPv6, for networks
	// that want the same address-to-client record IPv4 has. Clients end up with
	// both a DHCPv6 and a SLAAC address.
	RAStateful RAMode = "stateful"
)

// RAModes lists the vocabulary, for flag help and schema enums.
func RAModes() []RAMode { return []RAMode{RAOff, RASLAAC, RAStateful} }

// Valid reports whether m is a known mode. The empty string is valid and means
// RAOff.
func (m RAMode) Valid() bool { return m == "" || slices.Contains(RAModes(), m) }

// OrDefault resolves the empty string.
func (m RAMode) OrDefault() RAMode {
	if m == "" {
		return RAOff
	}
	return m
}

// Duration is a time.Duration that round-trips through JSON as a string.
//
// Without the wrapper a 12 hour lease serialises as 43200000000000, which is
// unreadable in a config file and hostile in an API response (design.md §10,
// config format).
type Duration time.Duration

// Duration converts back to the stdlib type.
func (d Duration) Duration() time.Duration { return time.Duration(d) }

// durationUnits covers the units dnsmasq itself accepts for a lease time.
// time.ParseDuration knows nothing of days or weeks, and a two-day lease is an
// entirely ordinary thing to want.
var durationUnits = map[byte]time.Duration{
	's': time.Second,
	'm': time.Minute,
	'h': time.Hour,
	'd': 24 * time.Hour,
	'w': 7 * 24 * time.Hour,
}

// ParseDuration reads "12h", "45m", "2d", "1w30m", or a bare number of seconds.
func ParseDuration(s string) (Duration, error) {
	rest := strings.TrimSpace(s)
	if rest == "" {
		return 0, fmt.Errorf("empty duration")
	}
	var total time.Duration
	for rest != "" {
		i := 0
		for i < len(rest) && rest[i] >= '0' && rest[i] <= '9' {
			i++
		}
		if i == 0 {
			return 0, fmt.Errorf("invalid duration %q: expected a number at %q", s, rest)
		}
		n, err := strconv.ParseInt(rest[:i], 10, 63)
		if err != nil {
			return 0, fmt.Errorf("invalid duration %q: %w", s, err)
		}
		rest = rest[i:]
		if rest == "" {
			// A unitless lease time means seconds to dnsmasq, so it means
			// seconds here too rather than being an error an operator has to
			// learn about.
			total += time.Duration(n) * time.Second
			break
		}
		unit, ok := durationUnits[rest[0]]
		if !ok {
			return 0, fmt.Errorf("invalid duration %q: unknown unit %q (want s, m, h, d or w)", s, rest[:1])
		}
		total += time.Duration(n) * unit
		rest = rest[1:]
	}
	return Duration(total), nil
}

// String renders the duration the way an operator would write it — "12h", not
// time.Duration's "12h0m0s".
func (d Duration) String() string {
	td := time.Duration(d)
	if td <= 0 {
		return "0s"
	}
	var b strings.Builder
	for _, u := range []struct {
		suffix string
		size   time.Duration
	}{{"w", 7 * 24 * time.Hour}, {"d", 24 * time.Hour}, {"h", time.Hour}, {"m", time.Minute}, {"s", time.Second}} {
		if n := td / u.size; n > 0 {
			fmt.Fprintf(&b, "%d%s", n, u.suffix)
			td -= n * u.size
		}
	}
	if td > 0 {
		// Sub-second precision is meaningless for a lease but we would rather
		// print something odd than silently round it away.
		b.WriteString(td.String())
	}
	return b.String()
}

// Seconds renders the duration as dnsmasq wants it in a config file.
func (d Duration) Seconds() string {
	return strconv.FormatInt(int64(time.Duration(d)/time.Second), 10)
}

// MarshalText makes Duration a JSON string.
func (d Duration) MarshalText() ([]byte, error) { return []byte(d.String()), nil }

// UnmarshalText parses that string back.
func (d *Duration) UnmarshalText(text []byte) error {
	parsed, err := ParseDuration(string(text))
	if err != nil {
		return err
	}
	*d = parsed
	return nil
}

// NormalizeMAC lowercases and canonicalises a hardware address, so that
// "AA-BB-CC-DD-EE-FF" and "aa:bb:cc:dd:ee:ff" are one reservation rather than
// two. It returns an error for anything net.ParseMAC rejects.
//
// The implementation is core's, because the `clients` module keys identity by
// the same string and the client list joins the two on it (design.md §4.4). It
// stays exported here so that this module's own call sites read in its own
// vocabulary rather than reaching across for a spelling rule.
func NormalizeMAC(s string) (string, error) {
	return core.NormalizeMAC(s)
}

// Normalize puts the config in canonical form: MACs lowercased, pools sorted by
// interface, reservations sorted by MAC.
//
// Everything downstream depends on this. Rendering is deterministic only if the
// input order is, and drift detection compares rendered bytes — so without a
// canonical order, reordering a JSON array would read as drift.
//
// It deliberately does not validate; a malformed MAC is left as-is for
// Validate to report with a proper path.
func (c *Config) Normalize() {
	for i, r := range c.Reservations {
		if mac, err := NormalizeMAC(r.MAC); err == nil {
			c.Reservations[i].MAC = mac
		}
	}
	slices.SortStableFunc(c.Pools, func(a, b Pool) int {
		return strings.Compare(a.Network, b.Network)
	})
	slices.SortStableFunc(c.Reservations, func(a, b Reservation) int {
		if n := strings.Compare(a.MAC, b.MAC); n != 0 {
			return n
		}
		return a.IP.Compare(b.IP)
	})
}

// Clone deep-copies the config, so that a proposed change can be diffed against
// the current one without either sharing backing arrays.
func (c Config) Clone() Config {
	out := c
	out.Pools = make([]Pool, len(c.Pools))
	for i, p := range c.Pools {
		p.DNS = slices.Clone(p.DNS)
		p.NTP = slices.Clone(p.NTP)
		p.Options = slices.Clone(p.Options)
		if p.Gateway != nil {
			gw := *p.Gateway
			p.Gateway = &gw
		}
		if p.IPv4 != nil {
			v4 := *p.IPv4
			p.IPv4 = &v4
		}
		if p.IPv6 != nil {
			v6 := *p.IPv6
			p.IPv6 = &v6
		}
		out.Pools[i] = p
	}
	out.Reservations = slices.Clone(c.Reservations)
	return out
}

// Pool returns the pool for a network.
func (c Config) Pool(network string) (Pool, bool) {
	i := slices.IndexFunc(c.Pools, func(p Pool) bool { return p.Network == network })
	if i < 0 {
		return Pool{}, false
	}
	return c.Pools[i], true
}

// SetPool adds or replaces the pool for p.Network.
func (c *Config) SetPool(p Pool) {
	if i := slices.IndexFunc(c.Pools, func(e Pool) bool { return e.Network == p.Network }); i >= 0 {
		c.Pools[i] = p
		return
	}
	c.Pools = append(c.Pools, p)
	c.Normalize()
}

// RemovePool drops a network's pool, reporting whether there was one.
func (c *Config) RemovePool(network string) bool {
	i := slices.IndexFunc(c.Pools, func(p Pool) bool { return p.Network == network })
	if i < 0 {
		return false
	}
	c.Pools = slices.Delete(c.Pools, i, i+1)
	return true
}

// Reservation returns the reservation for a MAC, which need not be normalized.
func (c Config) Reservation(mac string) (Reservation, bool) {
	norm, err := NormalizeMAC(mac)
	if err != nil {
		return Reservation{}, false
	}
	i := slices.IndexFunc(c.Reservations, func(r Reservation) bool { return r.MAC == norm })
	if i < 0 {
		return Reservation{}, false
	}
	return c.Reservations[i], true
}

// SetReservation adds or replaces a reservation, keyed by MAC.
func (c *Config) SetReservation(r Reservation) {
	if mac, err := NormalizeMAC(r.MAC); err == nil {
		r.MAC = mac
	}
	if i := slices.IndexFunc(c.Reservations, func(e Reservation) bool { return e.MAC == r.MAC }); i >= 0 {
		c.Reservations[i] = r
		return
	}
	c.Reservations = append(c.Reservations, r)
	c.Normalize()
}

// RemoveReservation drops a reservation by MAC, reporting whether there was one.
func (c *Config) RemoveReservation(mac string) bool {
	norm, err := NormalizeMAC(mac)
	if err != nil {
		return false
	}
	i := slices.IndexFunc(c.Reservations, func(r Reservation) bool { return r.MAC == norm })
	if i < 0 {
		return false
	}
	c.Reservations = slices.Delete(c.Reservations, i, i+1)
	return true
}

// MarshalConfig renders the config as the module's subtree of the document.
//
// It does not stamp a "$schema" key: the document has exactly one, written by
// the store (core.SchemaURL), and a second one nested inside a module's subtree
// would be a claim this module is not in a position to make. Indentation is
// likewise not ours — the store re-indents the whole document in one pass, so
// what matters here is only the values and their canonical order.
func MarshalConfig(c Config) ([]byte, error) {
	c.Normalize()
	return json.Marshal(c)
}

// UnmarshalConfig parses a config, rejecting unknown fields.
//
// Strictness is deliberate: a typo'd key that is silently ignored produces a
// router that is quietly not doing what its config says, which is the worst
// failure mode this module has.
//
// The one exception is the pre-0.3 pool shape, which it reads and converts —
// see LegacyPool. Strictness and migration are not in tension here: a key we
// used to write is not a typo, and the alternative is a box that stops serving
// DHCP on upgrade with a parse error naming a field the operator never typed.
//
// The other exception is a pool's `group`, which is what Network was called
// until the word was freed for sets of clients. It is renamed before the strict
// decode, for the same reason and with the same second half as the link
// module's `groups`: see HasLegacyKeys.
func UnmarshalConfig(data []byte) (Config, error) {
	data, _, err := renameLegacyKeys(data)
	if err != nil {
		return Config{}, fmt.Errorf("parsing dhcp config: %w", err)
	}

	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	var c Config
	if err := dec.Decode(&c); err != nil {
		if migrated, legacy, ok := unmarshalLegacy(data); ok {
			migrated.Normalize()
			migrated.Legacy = legacy
			return migrated, nil
		}
		return Config{}, fmt.Errorf("parsing dhcp config: %w", err)
	}
	c.Normalize()
	return c, nil
}

// legacyNetworkKey is what Pool.Network was stored as before the rename.
const legacyNetworkKey = "group"

// renameLegacyKeys rewrites every pool's old spelling of Network to the
// current one, reporting whether any pool had it.
//
// Per pool rather than all or nothing, so that a document where one pool was
// added after the upgrade and one was not still reads. A pool carrying both
// spellings is refused and named by its index, which is what the operator
// needs to find it.
func renameLegacyKeys(data []byte) ([]byte, bool, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(data, &obj); err != nil || obj == nil {
		return data, false, nil
	}
	raw, ok := obj["pools"]
	if !ok {
		return data, false, nil
	}
	var pools []json.RawMessage
	if err := json.Unmarshal(raw, &pools); err != nil {
		return data, false, nil
	}

	renamed := false
	for i, p := range pools {
		out, ok, err := core.RenameKey(p, legacyNetworkKey, "network")
		if err != nil {
			return data, false, fmt.Errorf("pools[%d]: %w", i, err)
		}
		if ok {
			pools[i] = out
			renamed = true
		}
	}
	if !renamed {
		return data, false, nil
	}

	encoded, err := json.Marshal(pools)
	if err != nil {
		return data, false, err
	}
	obj["pools"] = encoded
	out, err := json.Marshal(obj)
	if err != nil {
		return data, false, err
	}
	return out, true, nil
}

// HasLegacyKeys reports whether a stored subtree still has a pool spelling its
// network the old way. internal/daemon asks at startup and rewrites the
// document once; see the link module's HasLegacyKeys for why it has to ask.
func HasLegacyKeys(data []byte) bool {
	_, renamed, _ := renameLegacyKeys(data)
	return renamed
}

// LegacyPool is a pool as written before networks existed: keyed by kernel
// interface, with the IPv4 range at the top level and IPv6 as a sibling `ra`
// field.
//
// Kept readable rather than dropped because 0.2.0 shipped and there are
// installs in the field. It is only ever read — nothing writes this shape
// again.
type LegacyPool struct {
	Interface string       `json:"interface"`
	Start     netip.Addr   `json:"start"`
	End       netip.Addr   `json:"end"`
	LeaseTime Duration     `json:"lease_time,omitempty"`
	Gateway   *netip.Addr  `json:"gateway,omitempty"`
	DNS       []netip.Addr `json:"dns,omitempty"`
	Domain    string       `json:"domain,omitempty"`
	NTP       []netip.Addr `json:"ntp,omitempty"`
	RA        RAMode       `json:"ra,omitempty"`
	Options   []Option     `json:"options,omitempty"`
}

// legacyConfig is the 0.2.0 document shape.
type legacyConfig struct {
	Enabled      bool          `json:"enabled"`
	Pools        []LegacyPool  `json:"pools,omitempty"`
	Reservations []Reservation `json:"reservations,omitempty"`
	ExtraConf    string        `json:"extra_dnsmasq_conf,omitempty"`
}

// unmarshalLegacy reads the old shape and converts it, reporting the pools it
// converted so that the caller can finish the job.
//
// The conversion is deliberately incomplete, and that is the honest shape for
// it. A pool's `interface` names a kernel interface; a pool's `network` names a
// network, and this module does not own networks and cannot invent one. So the
// range, lease, options and IPv6 mode are carried across here, the interface
// name is parked in Legacy, and internal/daemon — the one place that sees both
// modules — creates the matching network and fills the key in. See
// daemon.migratePools.
func unmarshalLegacy(data []byte) (Config, []LegacyPool, bool) {
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()

	var old legacyConfig
	if err := dec.Decode(&old); err != nil {
		return Config{}, nil, false
	}
	if len(old.Pools) == 0 {
		// Nothing distinguishes an empty legacy document from a current one, so
		// there is nothing to migrate and no reason to claim there was.
		return Config{}, nil, false
	}

	out := Config{
		Enabled:      old.Enabled,
		Reservations: old.Reservations,
		ExtraConf:    old.ExtraConf,
		Pools:        make([]Pool, 0, len(old.Pools)),
	}
	for _, p := range old.Pools {
		out.Pools = append(out.Pools, p.Convert())
	}
	return out, old.Pools, true
}

// Convert turns a legacy pool into the current shape, minus its key.
func (p LegacyPool) Convert() Pool {
	out := Pool{
		// Network is left empty on purpose; only the daemon can supply it.
		LeaseTime: p.LeaseTime,
		Gateway:   p.Gateway,
		DNS:       p.DNS,
		Domain:    p.Domain,
		NTP:       p.NTP,
		Options:   p.Options,
	}
	if p.Start.IsValid() || p.End.IsValid() {
		// Carried across explicitly rather than left to be derived. The operator
		// typed this range, clients are holding addresses from it right now, and
		// re-deriving would silently renumber a working network on upgrade.
		out.IPv4 = &PoolIPv4{Start: p.Start, End: p.End}
	}
	if mode := p.RA.OrDefault(); mode != RAOff {
		out.IPv6 = &PoolIPv6{Mode: mode}
	}
	return out
}

// FromDocument reads this module's subtree out of the configuration document.
//
// A document without a "dhcp" key is not an error — it means the module has
// never been configured, which is a legitimate state and exactly what a fresh
// install looks like.
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

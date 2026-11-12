package daemon

import (
	"context"
	"net/netip"
	"strings"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/clients"
	"github.com/open-linux-router/open-linux-router/internal/core"
	"github.com/open-linux-router/open-linux-router/internal/dhcp"
	"github.com/open-linux-router/open-linux-router/internal/dns"
)

// Adapters joining the dhcp module to the devices module.
//
// They live here, in the binary that mounts both, rather than inside either
// module. That is what keeps design.md §4.1's arrow pointing one way: `devices`
// declares the interfaces it needs (PresenceSource, FixedAddressView,
// NetworkView, NameSource) and never imports `dhcp`, while `dhcp` stays unaware
// that an inventory exists. The two are introduced at the one place that
// already knows the whole module list.

// clientOwners is the address → client join the traffic sampler keys history by,
// read from the same presence sources the client list joins. Problems are
// dropped here rather than reported: a source that cannot be read leaves its
// addresses unattributed, which history already knows how to store, and the
// client list is where an operator is told why.
func clientOwners(sources []clients.PresenceSource) func(context.Context) map[netip.Addr]string {
	return func(ctx context.Context) map[netip.Addr]string {
		sightings, _ := clients.Gather(ctx, sources...)
		return clients.Owners(sightings)
	}
}

// dhcpPresence reads the lease database as sightings.
type dhcpPresence struct {
	applier dhcp.Applier
}

func (d dhcpPresence) Name() clients.Source { return clients.SourceDHCPLease }

func (d dhcpPresence) Presence(_ context.Context) ([]clients.Sighting, []clients.Problem, error) {
	leases, bad, err := d.applier.Leases()
	if err != nil {
		return nil, nil, err
	}

	// Unparseable lease lines are carried across rather than dropped, so a
	// corrupt lease file is visible on the device list instead of quietly
	// shortening it.
	problems := make([]clients.Problem, 0, len(bad))
	for _, p := range bad {
		problems = append(problems, clients.Problem{
			Path:    string(clients.SourceDHCPLease),
			Message: p.Message,
		})
	}

	now := time.Now()
	out := make([]clients.Sighting, 0, len(leases))
	for _, l := range leases {
		// A DHCPv6 lease frequently has no hardware address at all — it is
		// keyed by IAID and DUID instead. A device is keyed by MAC (§4.4), so
		// there is nothing here to join on and the honest thing is to skip it
		// rather than invent a key. The consequence is that a v6-only client is
		// invisible to this source, which is the same gap ARP has and the same
		// one a netlink ND reader would close.
		if l.MAC == "" {
			continue
		}

		s := clients.Sighting{
			MAC:      l.MAC,
			Hostname: l.Hostname,
			Source:   clients.SourceDHCPLease,
			Active:   l.Active(now),
		}
		if l.IP.IsValid() {
			s.IP = l.IP.String()
		}
		if !l.Expires.IsZero() {
			expires := l.Expires
			s.Expires = &expires
		}
		out = append(out, s)
	}

	return out, problems, nil
}

// dhcpNames reads what each client calls itself off the lease database, for the
// record of names.
//
// The lease is the only source of a name: the neighbour table carries none, and
// DNS records would be our own answer rather than the client's claim. Reading it
// here rather than reusing dhcpPresence keeps the two questions apart — a lease's
// Active flag is about presence and its hostname is about identity, and the
// background sampler wants only the second.
//
// A lease with no hostname is skipped rather than reported as an empty name:
// silence is not a client withdrawing the name it gave last week.
type dhcpNames struct {
	applier dhcp.Applier
}

func (d dhcpNames) Hostnames(context.Context) (map[string]string, error) {
	leases, _, err := d.applier.Leases()
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(leases))
	for _, l := range leases {
		if l.MAC == "" {
			continue
		}
		name := strings.TrimSpace(l.Hostname)
		if name == "" {
			continue
		}
		mac, err := core.NormalizeMAC(l.MAC)
		if err != nil {
			// Validation rejects these on the way in, so one here means a
			// hand-edited file. Skipped for the reason dhcpPresence skips a
			// lease with no MAC: there is nothing to key a name on.
			continue
		}
		out[mac] = name
	}
	return out, nil
}

// dhcpFixedAddresses reads reservations as MAC → address.
//
// Read through dhcp's own Load rather than from a copy: `dhcp` remains the
// single owner of a fixed address, and the device list joins against it per
// request. §11.1 asked that the fixed-address *workflow* start from the device;
// it did not ask for the fact to move, and moving it would be the private copy
// §4.1 forbids.
type dhcpFixedAddresses struct {
	applier dhcp.Applier
}

// dhcpNetworks reads the pools as the ranges served on each interface.
//
// This is what lets the device list place a device the neighbour table has
// forgotten — an away phone still holds a lease, and its address still falls
// inside the pool it came from. Read per request through dhcp's own Load, for
// the same reason dhcpFixedAddresses is: `dhcp` stays the single owner of the
// range and no second copy exists to drift.
//
// Reservations are deliberately not consulted here. A reserved address normally
// sits *outside* the pool it belongs beside, so treating one as a range of its
// own would place a device on evidence no stronger than the operator having
// typed an address — and ARP already places that device properly whenever it is
// awake.
type dhcpNetworks struct {
	applier dhcp.Applier
}

func (d dhcpNetworks) Networks(_ context.Context) ([]clients.Network, error) {
	cfg, err := d.applier.Load()
	if err != nil {
		return nil, err
	}

	out := make([]clients.Network, 0, len(cfg.Pools))
	for _, p := range cfg.Pools {
		info, err := d.applier.Networks.Network(p.Network)
		if err != nil {
			continue
		}
		// The resolved range, not the stored fields: a pool whose range is
		// derived has addresses, and reading the stored fields would report it
		// as having none.
		start, end, ok := p.Range(info)
		if !ok {
			continue
		}
		out = append(out, clients.Network{
			Name:    p.Network,
			Members: info.Members,
			Start:   start,
			End:     end,
		})
	}
	return out, nil
}

func (d dhcpFixedAddresses) FixedAddresses(_ context.Context) (map[string]string, error) {
	cfg, err := d.applier.Load()
	if err != nil {
		return nil, err
	}

	out := make(map[string]string, len(cfg.Reservations))
	for _, r := range cfg.Reservations {
		mac, err := core.NormalizeMAC(r.MAC)
		if err != nil {
			// Validation rejects these on the way in, so one here means a
			// hand-edited file. Skipped rather than failing the whole list: the
			// dhcp module's own surface is where that complaint belongs.
			continue
		}
		if r.IP.IsValid() {
			out[mac] = r.IP.String()
		}
	}
	return out, nil
}

// dnsReservations reads reservations as the dns module's cross-check.
//
// The same one-way arrow and the same per-request read as dhcpFixedAddresses
// above, joining the other pair design.md §4.1 names: `dhcp` publishes, `dns`
// subscribes. What dns does with it is narrower than what devices does —
// internal/dns/dhcp.go is emphatic that this feeds validation and never the
// renderer, so that a reservation edit cannot leave the resolver looking
// drifted.
type dnsReservations struct {
	applier dhcp.Applier
}

func (d dnsReservations) Reservations() ([]dns.Reservation, error) {
	cfg, err := d.applier.Load()
	if err != nil {
		return nil, err
	}

	out := make([]dns.Reservation, 0, len(cfg.Reservations))
	for _, r := range cfg.Reservations {
		mac, err := core.NormalizeMAC(r.MAC)
		if err != nil {
			// A hand-edited file, since validation rejects these on the way in.
			// Skipped rather than failing the list, for the reason
			// dhcpFixedAddresses gives: the complaint belongs on dhcp's own
			// surface, not attached to somebody else's config.
			continue
		}
		out = append(out, dns.Reservation{MAC: mac, IP: r.IP, Hostname: r.Hostname})
	}
	return out, nil
}

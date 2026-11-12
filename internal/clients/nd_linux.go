//go:build linux

package clients

import (
	"context"
	"fmt"
	"net"
	"net/netip"

	"github.com/vishvananda/netlink"
)

// ND reads the kernel's neighbour table for IPv6.
//
// ARP is the IPv4 half of the same question and /proc/net/arp is the whole of
// that answer. IPv6 has no such file, so this is the one presence source that
// costs the netlink library the last-heard sampler already links against. It is
// not a nicety: without it every IPv6 address a client uses is traffic the
// router can count and cannot name, which is how a client list showing one IPv4
// address sat beside an accounting table holding two thousand IPv6 ones.
type ND struct{}

// Name identifies the source in problem messages.
func (ND) Name() Source { return SourceND }

// Presence lists the IPv6 neighbours the kernel holds a hardware address for.
//
// A REACHABLE entry is presence — the kernel confirmed it answered within the
// last half minute — and anything else the kernel still has a hardware address
// for is reported as a sighting but not as presence. The difference matters to
// the device list, not to history: an address that stopped answering an hour ago
// still belongs to the client that held it, and that is the question the traffic
// sampler needs answered when the accounting sets are read.
func (ND) Presence(_ context.Context) ([]Sighting, []Problem, error) {
	list, err := netlink.NeighList(0, netlink.FAMILY_V6)
	if err != nil {
		return nil, nil, fmt.Errorf("reading the IPv6 neighbour table: %w", err)
	}
	names := map[int]string{}
	out := make([]Sighting, 0, len(list))
	for _, n := range list {
		if n.Family != netlink.FAMILY_V6 || len(n.HardwareAddr) < 6 || len(n.IP) == 0 {
			continue
		}
		// INCOMPLETE and FAILED entries are a question the kernel is still
		// asking, not an answer: the hardware address beside them is a
		// placeholder, and treating one as an owner would hand a client another
		// client's address.
		if n.State&netlink.NUD_INCOMPLETE != 0 || n.State&netlink.NUD_FAILED != 0 {
			continue
		}
		addr, ok := netip.AddrFromSlice(n.IP)
		if !ok {
			continue
		}
		addr = addr.Unmap()
		// A link-local address never reaches an accounting set that matters:
		// internet traffic leaves through a global or unique-local source.
		// Listing them would put fe80:: on the card beside the address the client
		// actually uses.
		if !addr.Is6() || addr.IsLinkLocalUnicast() || addr.IsMulticast() {
			continue
		}
		iface := ""
		if n.LinkIndex > 0 {
			name, ok := names[n.LinkIndex]
			if !ok {
				if link, err := net.InterfaceByIndex(n.LinkIndex); err == nil {
					name = link.Name
				}
				names[n.LinkIndex] = name
			}
			iface = name
		}
		out = append(out, Sighting{
			MAC:       net.HardwareAddr(n.HardwareAddr).String(),
			IP:        addr.String(),
			Interface: iface,
			Source:    SourceND,
			Active:    n.State&netlink.NUD_REACHABLE != 0,
			// ND has no expiry of its own. The kernel forgets an entry when it
			// is garbage collected, and an entry that is here now was confirmed
			// at some point in the last few minutes.
		})
	}
	return out, nil, nil
}

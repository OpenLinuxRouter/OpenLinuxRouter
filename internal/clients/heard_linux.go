//go:build linux

package clients

import (
	"context"

	"github.com/vishvananda/netlink"
)

// Neighbours hears a client when the kernel's neighbour table holds it as
// REACHABLE: confirmed to answer within the last half minute or so. Both
// families, because a phone that only talks IPv6 is still a phone.
//
// Netlink, where the ARP presence source reads /proc/net/arp: that file
// prints a STALE entry and a fresh one alike, and the difference is the whole
// of what this is for.
type Neighbours struct{}

// Heard lists the hardware addresses the kernel has just confirmed.
func (Neighbours) Heard(_ context.Context) ([]string, error) {
	list, err := netlink.NeighList(0, netlink.FAMILY_ALL)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(list))
	for _, n := range list {
		if n.State&netlink.NUD_REACHABLE == 0 || len(n.HardwareAddr) == 0 {
			continue
		}
		out = append(out, n.HardwareAddr.String())
	}
	return out, nil
}

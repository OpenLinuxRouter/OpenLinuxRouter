//go:build linux

package inspection

import (
	"context"
	"fmt"
	"net/netip"

	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

// readFlows lists conntrack, in both families, as the sampler's inputs.
//
// The same table the world map differences, read for a different question: that
// one asks "where", this one asks "which client, under which name". Reading it
// twice is cheaper than threading one sampler through two owners.
func readFlows(ctx context.Context) ([]Flow, error) {
	var out []Flow
	for _, family := range []netlink.InetFamily{unix.AF_INET, unix.AF_INET6} {
		if err := ctx.Err(); err != nil {
			return out, err
		}
		flows, err := netlink.ConntrackTableList(netlink.ConntrackTable, family)
		if err != nil {
			return out, fmt.Errorf("listing conntrack: %w", err)
		}
		for _, f := range flows {
			src, ok := netip.AddrFromSlice(f.Forward.SrcIP)
			if !ok {
				continue
			}
			dst, ok := netip.AddrFromSlice(f.Forward.DstIP)
			if !ok {
				continue
			}
			proto := ""
			if f.ProtoInfo != nil {
				proto = f.ProtoInfo.Protocol()
			}
			out = append(out, Flow{
				Src: src.Unmap(), Dst: dst.Unmap(),
				Sport: f.Forward.SrcPort, Dport: f.Forward.DstPort, Proto: proto,
				BytesOut: f.Forward.Bytes, BytesIn: f.Reverse.Bytes,
			})
		}
	}
	return out, nil
}

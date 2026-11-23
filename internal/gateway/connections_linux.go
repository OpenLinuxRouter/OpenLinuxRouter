//go:build linux

package gateway

import (
	"fmt"
	"net/netip"
	"os"
	"strings"

	"github.com/vishvananda/netlink"
)

// The kernel's connection table, and the switch that makes its counters worth
// reading.
//
// /proc/net/nf_conntrack rather than a netlink dump, for the reason
// internal/clients/arp.go gives about /proc/net/arp: the kernel already formats
// the table as text, and one read of it is cheaper and simpler than a netlink
// conversation. The text is also the interface this box's operator can inspect
// by hand, so the parser reads exactly what they would.
const (
	conntrackPath = "/proc/net/nf_conntrack"
	conntrackAcct = "/proc/sys/net/netfilter/nf_conntrack_acct"
)

// readConntrack samples the table once.
//
// The accounting switch is read first. It is one small file against a table of
// thousands of lines, and with it off every entry parses with zero counters —
// a census of connections that all look idle, which is actually a census of a
// kernel nobody asked to count. That case is a state the endpoint reports, not
// a failure it hides.
//
// The box's own addresses are read in the same sample because classification
// needs them: without that set the router's own connections would be listed as
// an unnamed client's, and the resolver's traffic would look like a device.
func readConntrack() (conntrackSample, error) {
	acct, err := os.ReadFile(conntrackAcct)
	if err != nil {
		return conntrackSample{}, fmt.Errorf("reading %s: %w", conntrackAcct, err)
	}
	if strings.TrimSpace(string(acct)) != "1" {
		return conntrackSample{}, errConntrackNotCounting
	}
	data, err := os.ReadFile(conntrackPath)
	if err != nil {
		return conntrackSample{}, fmt.Errorf("reading %s: %w", conntrackPath, err)
	}
	local, err := localAddresses()
	if err != nil {
		return conntrackSample{}, err
	}
	return conntrackSample{entries: parseConntrack(data), local: local}, nil
}

// localAddresses is every address this box holds, on every interface.
//
// The addresses, not the prefixes: 172.16.1.1/24 on the LAN interface is the
// router's address, while 172.16.1.32 in the same prefix is a client. A set
// built from prefixes would call every client local and empty the census.
func localAddresses() (map[netip.Addr]bool, error) {
	addresses, err := netlink.AddrList(nil, netlink.FAMILY_ALL)
	if err != nil {
		return nil, fmt.Errorf("listing this box's addresses: %w", err)
	}
	out := make(map[netip.Addr]bool, len(addresses))
	for _, a := range addresses {
		if ip, ok := netip.AddrFromSlice(a.IP); ok {
			out[ip.Unmap()] = true
		}
	}
	return out, nil
}

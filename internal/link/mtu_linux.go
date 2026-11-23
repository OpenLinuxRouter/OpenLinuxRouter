//go:build linux

package link

import "github.com/vishvananda/netlink"

// linkMTUs reads every interface's MTU in one netlink dump.
//
// The stdlib net.Interface has no MTU, and this is the one fact about an
// interface the firewall has to have: it decides whether MSS clamping is
// needed. A second netlink call rather than a wider dependency: LinkList is one
// dump, and the writer already uses the package for the interfaces it changes.
//
// An unreadable list is not an error here. An interface list without MTUs is
// still a useful interface list, and the zero value says "unknown" rather than
// pretending to be 1500.
func linkMTUs() map[string]int {
	links, err := netlink.LinkList()
	if err != nil {
		return nil
	}
	out := make(map[string]int, len(links))
	for _, l := range links {
		attrs := l.Attrs()
		out[attrs.Name] = attrs.MTU
	}
	return out
}

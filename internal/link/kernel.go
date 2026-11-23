package link

import (
	"fmt"
	"net"
	"net/netip"
	"os"
	"slices"
	"strings"
)

// The observed half of an interface: what the kernel says right now.
//
// Never stored. design.md §4.5 is the rule — own the model, never the state —
// and this is the half of §7 that was previously kept in a hand-written
// `--links` file. That file was a second copy of a fact the kernel already
// held, with nothing to keep the two in step: an operator who renumbered an
// interface would get pools validated against the address it used to have. A
// syscall per request is a small price for that class of bug not existing.

// Interface is one kernel interface, as read.
type Interface struct {
	// Name is the kernel name, and the key every other module uses.
	Name string

	// Index is the kernel's ifindex. Published because it is the one stable
	// handle when a name is ambiguous in a log, not because anything keys on it.
	Index int

	// Up is the administrative state — the operator has brought the interface
	// up. It is what "configured" means and is the flag the other modules test.
	Up bool

	// Running is the operational state: a carrier is present. Reported
	// separately rather than folded into Up because the two disagree in ways
	// worth seeing — an up interface with no cable in it is the single most
	// common reason a new DHCP server appears to do nothing — and because some
	// drivers never set it, so treating a false here as "down" would hide
	// working interfaces on those.
	Running bool

	// Loopback marks lo. Kept so that callers can exclude it by what it is
	// rather than by matching its name.
	Loopback bool

	// Physical is backed by a device in sysfs, not merely a kernel interface.
	Physical bool

	// HardwareAddr is the MAC, empty for interfaces that have none.
	HardwareAddr string

	// MTU is the interface's maximum transmission unit in bytes, zero when it
	// could not be read. Published because it is the number MSS clamping is
	// decided from: a client on a 1500-byte LAN cannot learn that the way out
	// is smaller, so the router has to do the arithmetic for it.
	MTU int

	// Prefixes are the addresses configured on the interface, with masks.
	//
	// Link-local addresses are excluded: fe80::/10 and 169.254.0.0/16 are
	// present on essentially every interface and are never the subnet an
	// operator means. Including them would put an identical fe80::/64 on every
	// row of the interface list, and would hand `gateway` a source prefix that
	// matches every network on the box at once.
	Prefixes []netip.Prefix

	// Optional live counters and negotiated speed. Unsupported drivers leave
	// them absent; an observed zero differs from an unavailable reading.
	//
	// The packet, drop and error readings are the other half of the traffic
	// story: bytes alone cannot separate a link that is full from a router
	// dropping what it could not afford to forward. They are cumulative since
	// the interface came up, like the byte counters, and their consumers
	// difference them rather than asking this package for a rate — the kernel
	// already publishes the number, and a second clock here would only be a
	// second thing to get wrong.
	SpeedMbps    *uint64
	RXBytes      *uint64
	TXBytes      *uint64
	RXPackets    *uint64
	TXPackets    *uint64
	RXDropped    *uint64
	TXDropped    *uint64
	RXErrors     *uint64
	TXErrors     *uint64
	RXFIFOErrors *uint64
}

// Source is where interface facts come from.
//
// A function rather than an interface with one method, because that is all it
// is, and because it lets a test supply three interfaces without a fixture
// type. Tests need this: a machine's real NICs are not something to assert
// against, and CI has none worth serving DHCP on.
type Source func() ([]Interface, error)

// Kernel reads the interfaces this machine actually has.
//
// Enumeration is stdlib `net`, with one netlink call beside it for MTUs
// (mtu_linux.go). `net.Interface` carries no MTU, and it is the one fact in
// this list that cannot be derived from addresses and flags — so it is worth
// the second call, and nothing else here needs more than enumeration.
// `net.Interfaces` speaks the same netlink underneath.
func Kernel() ([]Interface, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, fmt.Errorf("reading the kernel's interface list: %w", err)
	}

	mtus := linkMTUs()

	out := make([]Interface, 0, len(ifaces))
	for _, iface := range ifaces {
		info := Interface{
			Name:     iface.Name,
			Index:    iface.Index,
			Up:       iface.Flags&net.FlagUp != 0,
			Running:  iface.Flags&net.FlagRunning != 0,
			Loopback: iface.Flags&net.FlagLoopback != 0,
			Physical: hasDevice(iface.Name),
		}
		if len(iface.HardwareAddr) > 0 {
			info.HardwareAddr = iface.HardwareAddr.String()
		}
		info.MTU = mtus[iface.Name]
		readInterfaceMetrics(&info)

		// An interface whose addresses cannot be read still belongs on the list.
		// Dropping it would make an interface disappear from the UI for a reason
		// that has nothing to do with whether it can serve DHCP, and the empty
		// prefix list already says everything a caller needs: no pool can
		// validate against it.
		addrs, err := iface.Addrs()
		if err == nil {
			info.Prefixes = prefixesOf(addrs)
		}

		out = append(out, info)
	}

	sortInterfaces(out)
	return out, nil
}

// prefixesOf converts the kernel's addresses, dropping what is not a subnet.
func prefixesOf(addrs []net.Addr) []netip.Prefix {
	var out []netip.Prefix
	for _, a := range addrs {
		ipnet, ok := a.(*net.IPNet)
		if !ok {
			continue
		}
		prefix, ok := toPrefix(ipnet)
		if !ok || prefix.Addr().IsLinkLocalUnicast() {
			continue
		}
		out = append(out, prefix)
	}
	slices.SortFunc(out, func(a, b netip.Prefix) int {
		// IPv4 first, then by address: the v4 subnet is what an operator is
		// looking for on a home network, and a stable order keeps the UI's rows
		// and the rendered config from churning.
		if a.Addr().Is4() != b.Addr().Is4() {
			if a.Addr().Is4() {
				return -1
			}
			return 1
		}
		return a.Addr().Compare(b.Addr())
	})
	return out
}

// toPrefix converts a net.IPNet, which carries its mask as bytes, into a
// netip.Prefix, which carries it as a length.
//
// The v4-in-v6 case is the whole reason this is not a one-liner. The kernel can
// hand back a 4-byte or a 16-byte representation of the same IPv4 address, and
// a 16-byte one unmaps to an Is4 address while its mask still reports 128 bits
// — which would produce a /120 for a /24 and silently place every pool outside
// its own subnet.
func toPrefix(n *net.IPNet) (netip.Prefix, bool) {
	addr, ok := netip.AddrFromSlice(n.IP)
	if !ok {
		return netip.Prefix{}, false
	}
	addr = addr.Unmap()

	ones, bits := n.Mask.Size()
	if bits == 0 {
		// Size reports 0, 0 for a non-contiguous mask. There is no prefix length
		// that describes one, so there is nothing honest to publish.
		return netip.Prefix{}, false
	}
	if addr.Is4() && bits == 128 {
		ones, bits = ones-96, 32
	}
	if ones < 0 || ones > bits {
		return netip.Prefix{}, false
	}
	return netip.PrefixFrom(addr, ones), true
}

// sortInterfaces orders the list the way an operator reads it: real interfaces
// first, loopback last, alphabetically within each.
//
// Loopback is pushed to the bottom rather than hidden. It cannot be adopted
// (see Validate), but an interface list that silently omits an interface is a
// list you cannot trust to be complete — and `lo` missing is exactly the kind
// of absence that reads as a bug in the reader rather than a decision.
func sortInterfaces(in []Interface) {
	slices.SortStableFunc(in, func(a, b Interface) int {
		if a.Loopback != b.Loopback {
			if a.Loopback {
				return 1
			}
			return -1
		}
		return strings.Compare(a.Name, b.Name)
	})
}

// Virtual devices live in /sys/class/net too, but lack a hardware device link.
func hasDevice(name string) bool {
	_, err := os.Stat("/sys/class/net/" + name + "/device")
	return err == nil
}

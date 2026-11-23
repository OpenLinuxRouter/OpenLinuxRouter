package link

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// readMetricsAt reads the sysfs counter files under dir into iface.
//
// Platform-free on purpose even though only the Linux half calls it with a
// real path: the parser is the part with a decision in it — an absent or
// unparsable file is unknown, not zero — and keeping it here lets that
// decision be tested on a machine that has no sysfs at all.
//
// Missing files, unsupported speed (-1), and a device removed during the read
// all leave gaps, without hiding the interface or reporting a made-up zero.
func readMetricsAt(iface *Interface, dir string) {
	read := func(name string) *uint64 {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return nil
		}
		value, err := strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64)
		if err != nil {
			return nil
		}
		return &value
	}
	iface.SpeedMbps = read("speed")
	if iface.SpeedMbps != nil && *iface.SpeedMbps == 0 {
		iface.SpeedMbps = nil
	}
	iface.RXBytes = read("statistics/rx_bytes")
	iface.TXBytes = read("statistics/tx_bytes")
	iface.RXPackets = read("statistics/rx_packets")
	iface.TXPackets = read("statistics/tx_packets")
	iface.RXDropped = read("statistics/rx_dropped")
	iface.TXDropped = read("statistics/tx_dropped")
	iface.RXErrors = read("statistics/rx_errors")
	iface.TXErrors = read("statistics/tx_errors")
	// rx_fifo_errors is the one queue-specific counter worth reading on its
	// own: a full receive ring is the router doing too much work, not a link
	// losing packets in transit, and the two are fixed differently. The
	// sibling counters (missed, overrun, frame, carrier) are either folded
	// into rx_errors by the driver or say the same thing as one of these; a
	// page that needs them can ask the kernel directly.
	iface.RXFIFOErrors = read("statistics/rx_fifo_errors")
}

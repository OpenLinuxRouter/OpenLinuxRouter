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
	// Two counters beside rx_dropped say what a drop was not.
	//
	// rx_missed_errors is the device's buffer-exhaustion counter — the kernel
	// documents rx_dropped as excluding exactly these, because "nothing wanted
	// the frame" and "the device had no room for it" are different faults. A
	// page that reports only the sum cannot say which one it is looking at.
	//
	// rx_fifo_errors is a receive-FIFO error: a link or driver fault, not a
	// queue decision.
	iface.RXMissedErrors = read("statistics/rx_missed_errors")
	iface.RXFIFOErrors = read("statistics/rx_fifo_errors")
}

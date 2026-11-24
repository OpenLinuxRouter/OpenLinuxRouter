package link

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// A counter the kernel did not answer for is absent, never zero. The
// difference matters twice over: zero is a real reading a page can plot, and a
// fabricated one would be indistinguishable from a healthy interface with no
// drops.
func TestEachCounterIsAbsentWhenItCannotBeRead(t *testing.T) {
	fields := []struct {
		name string
		path string
		get  func(Interface) *uint64
	}{
		{"rx_bytes", "statistics/rx_bytes", func(i Interface) *uint64 { return i.RXBytes }},
		{"tx_bytes", "statistics/tx_bytes", func(i Interface) *uint64 { return i.TXBytes }},
		{"rx_packets", "statistics/rx_packets", func(i Interface) *uint64 { return i.RXPackets }},
		{"tx_packets", "statistics/tx_packets", func(i Interface) *uint64 { return i.TXPackets }},
		{"rx_dropped", "statistics/rx_dropped", func(i Interface) *uint64 { return i.RXDropped }},
		{"tx_dropped", "statistics/tx_dropped", func(i Interface) *uint64 { return i.TXDropped }},
		{"rx_errors", "statistics/rx_errors", func(i Interface) *uint64 { return i.RXErrors }},
		{"tx_errors", "statistics/tx_errors", func(i Interface) *uint64 { return i.TXErrors }},
		{"rx_fifo_errors", "statistics/rx_fifo_errors", func(i Interface) *uint64 { return i.RXFIFOErrors }},
		{"rx_missed_errors", "statistics/rx_missed_errors", func(i Interface) *uint64 { return i.RXMissedErrors }},
	}

	for _, tc := range fields {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.Mkdir(filepath.Join(dir, "statistics"), 0755); err != nil {
				t.Fatal(err)
			}
			write := func(value string) {
				t.Helper()
				if err := os.WriteFile(filepath.Join(dir, tc.path), []byte(value), 0644); err != nil {
					t.Fatal(err)
				}
			}
			iface := Interface{Name: "lan0"}

			readMetricsAt(&iface, dir)
			if got := tc.get(iface); got != nil {
				t.Fatalf("an absent %s read as %d", tc.name, *got)
			}

			write("not a number\n")
			readMetricsAt(&iface, dir)
			if got := tc.get(iface); got != nil {
				t.Fatalf("an unparsable %s read as %d", tc.name, *got)
			}

			write("0\n")
			readMetricsAt(&iface, dir)
			if got := tc.get(iface); got == nil || *got != 0 {
				t.Fatalf("an observed zero %s did not survive: %v", tc.name, got)
			}

			write("17\n")
			readMetricsAt(&iface, dir)
			if got := tc.get(iface); got == nil || *got != 17 {
				t.Fatalf("a valid %s read as %v", tc.name, got)
			}
		})
	}
}

func TestInterfaceMetricsMissingUnsupportedAndZero(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "statistics"), 0755); err != nil {
		t.Fatal(err)
	}
	write := func(name, value string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(value), 0644); err != nil {
			t.Fatal(err)
		}
	}
	iface := Interface{Name: "lan0", Index: 7}
	readMetricsAt(&iface, dir)
	if iface.SpeedMbps != nil || iface.RXBytes != nil || iface.TXBytes != nil {
		t.Fatal("missing readings must remain absent")
	}
	write("speed", "-1\n")
	write("statistics/rx_bytes", "0\n")
	write("statistics/tx_bytes", "invalid\n")
	write("statistics/rx_dropped", "0\n")
	write("statistics/tx_dropped", "3\n")
	write("statistics/rx_packets", "81\n")
	write("statistics/tx_packets", "82\n")
	write("statistics/rx_errors", "0\n")
	write("statistics/tx_errors", "4\n")
	write("statistics/rx_fifo_errors", "5\n")
	write("statistics/rx_missed_errors", "6\n")
	readMetricsAt(&iface, dir)
	if iface.SpeedMbps != nil || iface.RXBytes == nil || *iface.RXBytes != 0 || iface.TXBytes != nil {
		t.Fatalf("unsupported speed, observed zero, invalid counter: %+v", iface)
	}
	write("speed", "2500\n")
	write("statistics/tx_bytes", "123456\n")
	readMetricsAt(&iface, dir)
	if iface.SpeedMbps == nil || *iface.SpeedMbps != 2500 || iface.TXBytes == nil || *iface.TXBytes != 123456 {
		t.Fatalf("valid readings: %+v", iface)
	}
	v := viewInterface(Info{Name: "lan0", Present: true}, map[string]Interface{"lan0": iface}, Config{})
	encoded, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(encoded, &fields); err != nil {
		t.Fatal(err)
	}
	want := map[string]float64{
		"rx_bytes":         0,
		"tx_bytes":         123456,
		"rx_packets":       81,
		"tx_packets":       82,
		"rx_dropped":       0,
		"tx_dropped":       3,
		"rx_errors":        0,
		"tx_errors":        4,
		"rx_fifo_errors":   5,
		"rx_missed_errors": 6,
		"speed_mbps":       2500,
		"index":            7,
	}
	for name, value := range want {
		if fields[name] != value {
			t.Fatalf("API dropped %s or its observed zero: %s", name, encoded)
		}
	}
	write("speed", "0\n")
	readMetricsAt(&iface, dir)
	if iface.SpeedMbps != nil {
		t.Fatal("zero speed is unavailable, not a 0 Mbps link")
	}
}

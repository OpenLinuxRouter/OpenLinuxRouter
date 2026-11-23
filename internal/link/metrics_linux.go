//go:build linux

package link

import "path/filepath"

func readInterfaceMetrics(iface *Interface) {
	readMetricsAt(iface, filepath.Join("/sys/class/net", iface.Name))
}

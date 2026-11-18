package daemon

import (
	"context"
	"net"
	"net/url"

	"github.com/open-linux-router/open-linux-router/internal/clients"
	"github.com/open-linux-router/open-linux-router/internal/devices"
)

// surveyAddresses is what a switch is asked about when the topology survey
// places addresses on its ports: this router's own interfaces first (the cable
// back here), then the managed boxes (the cables between them), then every
// client online now. An offline client has aged out of any forwarding table,
// and asking about it is a lookup that cannot answer.
type surveyAddresses struct {
	clients clients.Applier
	devices *devices.Service
}

func (s surveyAddresses) Addresses(ctx context.Context) ([]string, error) {
	var out []string
	if ifaces, err := net.Interfaces(); err == nil {
		for _, iface := range ifaces {
			if iface.Flags&net.FlagLoopback == 0 && len(iface.HardwareAddr) == 6 {
				out = append(out, iface.HardwareAddr.String())
			}
		}
	}
	hosts := map[string]bool{}
	if cfg, err := devices.Load(s.devices.Store); err == nil {
		for _, d := range cfg.Devices {
			if u, err := url.Parse(d.Address); err == nil && u.Hostname() != "" {
				hosts[u.Hostname()] = true
			}
		}
	}
	list, _, err := s.clients.List(ctx)
	if err != nil {
		return out, err
	}
	var boxes, online []string
	for _, r := range list {
		if r.Presence != nil {
			for _, ip := range r.Presence.IPs {
				if hosts[ip] {
					boxes = append(boxes, r.MAC)
					break
				}
			}
		}
		if r.Online() {
			online = append(online, r.MAC)
		}
	}
	return append(append(out, boxes...), online...), nil
}

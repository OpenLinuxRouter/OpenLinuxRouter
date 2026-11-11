package dnsrelay

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/netip"
	"os"
	"slices"
	"strings"
)

// Routes is gateway's effective DNS selection, rendered independently of the
// resolver's own configuration so gateway edits only require a relay reload.
type Routes struct {
	LocalDomain string         `json:"local_domain,omitempty"`
	Default     string         `json:"default,omitempty"`
	Networks    []RouteNetwork `json:"networks,omitempty"`
	Clients     []RouteClient  `json:"clients,omitempty"`
	Exits       []RouteExit    `json:"exits,omitempty"`
}

type RouteNetwork struct {
	Prefix netip.Prefix `json:"prefix"`
	Exit   string       `json:"exit,omitempty"`
}

type RouteClient struct {
	MAC  string `json:"mac"`
	Exit string `json:"exit,omitempty"`

	// Prefixes are the adopted network ranges on which gateway can see this
	// device's MAC. An unknown IP within one of them cannot be safely guessed.
	Prefixes []netip.Prefix `json:"prefixes,omitempty"`
}

type RouteExit struct {
	Name string         `json:"name"`
	DNS  netip.AddrPort `json:"dns"`
}

func MarshalRoutes(r Routes) ([]byte, error) { return json.MarshalIndent(r, "", "  ") }

// renameLegacyClients rewrites the pre-rename `devices` key into `clients`.
func renameLegacyClients(data []byte) ([]byte, error) {
	var probe struct {
		Clients json.RawMessage `json:"clients"`
		Devices json.RawMessage `json:"devices"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return data, nil
	}
	if probe.Devices == nil || probe.Clients != nil {
		return data, nil
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(data, &doc); err != nil {
		return data, err
	}
	doc["clients"] = probe.Devices
	delete(doc, "devices")
	return json.Marshal(doc)
}

func LoadRoutes(path string) (Routes, error) {
	if path == "" {
		return Routes{}, nil
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return Routes{}, fmt.Errorf("DNS routes are not installed at %s", path)
	}
	if err != nil {
		return Routes{}, err
	}
	// The relay and the daemon are one package, so an upgrade replaces both at
	// once — but dnsd may read the file olrd wrote before the upgrade, where
	// per-client routes were called `devices`. Translate that one key before
	// strict decoding, so a restart between the two applies is not an outage.
	data, err = renameLegacyClients(data)
	if err != nil {
		return Routes{}, err
	}
	var routes Routes
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&routes); err != nil {
		return Routes{}, fmt.Errorf("reading DNS routes: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return Routes{}, fmt.Errorf("DNS routes contain trailing data")
	}
	seenExit := map[string]bool{}
	for _, e := range routes.Exits {
		if e.Name == "" || seenExit[e.Name] {
			return Routes{}, fmt.Errorf("duplicate or unnamed DNS exit %q", e.Name)
		}
		if !e.DNS.IsValid() || e.DNS.Port() == 0 || e.DNS.Addr().IsUnspecified() ||
			e.DNS.Addr().IsMulticast() {
			return Routes{}, fmt.Errorf("exit %q has no usable DNS upstream", e.Name)
		}
		seenExit[e.Name] = true
	}
	seenMAC := map[string]bool{}
	for _, d := range routes.Clients {
		mac, err := net.ParseMAC(d.MAC)
		if err != nil || len(mac) != 6 {
			return Routes{}, fmt.Errorf("DNS route has an invalid device MAC %q", d.MAC)
		}
		key := strings.ToLower(mac.String())
		if seenMAC[key] {
			return Routes{}, fmt.Errorf("duplicate DNS route for %s", key)
		}
		seenMAC[key] = true
		for _, p := range d.Prefixes {
			if !p.IsValid() || !p.Addr().Is4() {
				return Routes{}, fmt.Errorf("DNS route for %s has an invalid client prefix", d.MAC)
			}
		}
	}
	for _, n := range routes.Networks {
		if !n.Prefix.IsValid() || !n.Prefix.Addr().Is4() {
			return Routes{}, fmt.Errorf("DNS route has an invalid network prefix")
		}
	}
	routes.LocalDomain = strings.ToLower(strings.TrimSuffix(routes.LocalDomain, "."))
	if routes.LocalDomain == "" {
		routes.LocalDomain = "home.arpa"
	}
	slices.SortFunc(routes.Networks, func(a, b RouteNetwork) int {
		if a.Prefix.Bits() != b.Prefix.Bits() {
			return b.Prefix.Bits() - a.Prefix.Bits()
		}
		return strings.Compare(a.Prefix.String(), b.Prefix.String())
	})
	return routes, nil
}

// Select chooses the upstream for one observed client. The last result is
// uncertainty: a MAC-dependent route exists in this subnet but the client
// could not be identified, so the relay must not guess an incompatible answer.
func (r Routes) Select(ip netip.Addr, mac string) (netip.AddrPort, bool, bool) {
	if !ip.IsValid() {
		return netip.AddrPort{}, false, false
	}
	name := r.Default
	ip = ip.Unmap()
	for _, n := range r.Networks {
		if n.Prefix.Contains(ip) {
			name = n.Exit
			break
		}
	}
	if mac != "" {
		for _, d := range r.Clients {
			if strings.EqualFold(d.MAC, mac) && containsAny(d.Prefixes, ip) {
				name = d.Exit
				break
			}
		}
	}
	if mac == "" {
		for _, d := range r.Clients {
			if containsAny(d.Prefixes, ip) && d.Exit != name && r.dnsFor(d.Exit) != r.dnsFor(name) {
				return netip.AddrPort{}, false, true
			}
		}
	}
	if dns := r.dnsFor(name); dns.IsValid() {
		return dns, true, false
	}
	return netip.AddrPort{}, false, false
}

func containsAny(prefixes []netip.Prefix, addr netip.Addr) bool {
	for _, p := range prefixes {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}

func (r Routes) dnsFor(name string) netip.AddrPort {
	for _, e := range r.Exits {
		if e.Name == name {
			return e.DNS
		}
	}
	return netip.AddrPort{}
}

// DeviceDNSMayDiffer is the conservative answer for an unidentified client:
// if any MAC override on this subnet would select another DNS server, an
// upstream answer might not match the route the kernel gives that client.
func (r Routes) DeviceDNSMayDiffer(ip netip.Addr) bool {
	if !ip.IsValid() {
		return false
	}
	name := r.Default
	for _, n := range r.Networks {
		if n.Prefix.Contains(ip.Unmap()) {
			name = n.Exit
			break
		}
	}
	for _, d := range r.Clients {
		if containsAny(d.Prefixes, ip.Unmap()) && r.dnsFor(d.Exit) != r.dnsFor(name) {
			return true
		}
	}
	return false
}

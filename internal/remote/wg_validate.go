package remote

import (
	"fmt"
	"net/netip"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/open-linux-router/open-linux-router/internal/clients"
	"github.com/open-linux-router/open-linux-router/internal/core"
)

// Validation is the highest-value mechanism in the apply story (design.md
// §5.3.1), and this module leans on it for a reason particular to what it
// produces: **a client configuration leaves the box.** Every other module's
// mistakes stay here and are fixed by re-applying. A peer's file is copied to a
// phone, and a subnet or an endpoint that was wrong when it was written is
// wrong on somebody's device until they notice and come back.
//
// It is pure — no files, no network, no root — so the whole rule set is
// table-tested and `olr remote` can check a config on a laptop.

// MaxInterfaceNameLen is the kernel's IFNAMSIZ minus the terminator.
//
// Declared here rather than imported from `link`, which has the same constant.
// That is not duplication to remove: this module states the facts it depends on
// and imports no other module (design.md §4.1), and a constant the kernel fixes
// is the cheapest possible thing to state twice.
const MaxInterfaceNameLen = 15

// MaxPeerNameLen is a DNS label's limit. A peer's name becomes a local name
// when the dial-in segment is registered as a network
// (docs/remote-access.md §3.1), so a name that could not be one is refused now
// rather than migrated later.
const MaxPeerNameLen = 63

// MaxPeerDisplayNameLen bounds the label, generously. Long enough for "Li's
// Macbook Pro (work)" and short enough that a heading can be laid out without
// defending against a megabyte.
const MaxPeerDisplayNameLen = 64

// ValidateWireGuard checks the tunnel's half of a config, and the networks it
// has to coexist with.
func ValidateWireGuard(c Config, networks NetworkView) Result {
	var r Result
	w := c.WireGuard
	known := networksOf(networks)

	validateInterface(&r, w)
	validateSubnet(&r, w, known)
	validateKey(&r, w)
	validatePeers(&r, w, known)
	validateExtraConf(&r, w.ExtraConf)

	return r
}

func validateInterface(r *Result, w WireGuard) {
	name := w.InterfaceOrDefault()
	switch {
	case len(name) > MaxInterfaceNameLen:
		r.errorf("wireguard.interface",
			"%q is %d characters; the kernel allows %d", name, len(name), MaxInterfaceNameLen)
	case strings.ContainsAny(name, "/ \t"):
		r.errorf("wireguard.interface", "%q contains a space or a slash, which cannot name an interface", name)
	case name == "lo":
		r.errorf("wireguard.interface", "`lo` is the loopback interface")
	}

	if w.ListenPort != 0 && w.ListenPort < 1024 {
		// Not refused: olr runs as root and the kernel will allow it. Worth a
		// remark because a low port is far more likely to be a typo than a
		// choice, and because some middleboxes treat the range differently.
		r.warnf("wireguard.listen_port",
			"%d is a privileged port; WireGuard's own default is %d and nothing here needs a low one",
			w.ListenPort, DefaultListenPort)
	}
}

// validateSubnet is where the dial-in network is held to being a network.
//
// The overlap check is the one that matters. A dial-in subnet that overlaps a
// home network produces a tunnel that connects perfectly and reaches nothing,
// because the device now has two routes for one prefix and picks the wrong one
// — a failure with no error anywhere and no component to inspect.
func validateSubnet(r *Result, w WireGuard, networks []NetworkInfo) {
	subnet := w.SubnetOrDefault()
	switch {
	case !subnet.IsValid():
		r.errorf("wireguard.subnet", "required: the network dial-in devices get their addresses from")
		return
	case !subnet.Addr().Is4():
		r.errorf("wireguard.subnet",
			"%s is IPv6; olr addresses the dial-in network in IPv4 only for now", subnet)
		return
	case subnet.Addr().IsLoopback() || subnet.Addr().IsMulticast():
		r.errorf("wireguard.subnet", "%s is not a network addresses can be handed out from", subnet)
		return
	case subnet.Bits() > 30:
		r.errorf("wireguard.subnet",
			"%s leaves no room for this box and a peer; use /30 or wider", subnet)
		return
	}

	for _, n := range networks {
		if !n.Subnet.IsValid() || !n.Subnet.Overlaps(subnet) {
			continue
		}
		r.errorf("wireguard.subnet",
			"%s overlaps the %q network (%s), so a dial-in device would have two routes for one "+
				"prefix and reach neither reliably. Pick a range nothing at home uses",
			subnet, n.Name, n.Subnet)
	}

	if w.Address == nil {
		return
	}
	addr := *w.Address
	switch {
	case !addr.IsValid():
		r.errorf("wireguard.address", "is not an address")
	case !subnet.Contains(addr):
		r.errorf("wireguard.address", "%s is not inside %s", addr, subnet)
	default:
		if start, end, ok := core.HostRange(subnet); ok && !core.InRange(start, end, addr) {
			r.errorf("wireguard.address",
				"%s is the network or broadcast address of %s, which no host may hold", addr, subnet)
		}
	}
}

func validateKey(r *Result, w WireGuard) {
	if w.PrivateKey == "" {
		if w.Enabled {
			// Reached only by a config edited by hand or restored from a backup
			// with the key stripped: every path that enables the tunnel through
			// olr generates one first (http.go).
			r.errorf("wireguard.private_key",
				"this box has no key yet. `olr remote enable` generates one; a config restored "+
					"without it needs the peers re-issued, because their files name the old public key")
		}
		return
	}
	if err := ValidKey(w.PrivateKey); err != nil {
		r.errorf("wireguard.private_key", "%v", err)
		return
	}
	if _, err := PublicKeyFor(w.PrivateKey); err != nil {
		r.errorf("wireguard.private_key", "%v", err)
	}
}

func validatePeers(r *Result, w WireGuard, networks []NetworkInfo) {
	subnet := w.SubnetOrDefault()
	router := w.RouterAddr()

	byName := map[string]int{}
	byKey := map[string]int{}
	byAddr := map[netip.Addr]int{}

	for i, p := range w.Peers {
		path := fmt.Sprintf("wireguard.peers[%d]", i)

		if p.Name == "" {
			r.errorf(path+".name", "required")
			continue
		}
		if first, dup := byName[p.Name]; dup {
			r.errorf(path+".name", "%q is already a peer at wireguard.peers[%d]", p.Name, first)
			continue
		}
		byName[p.Name] = i
		validatePeerName(r, path, p.Name)
		validatePeerDisplayName(r, path, p.DisplayName)
		validatePeerPicture(r, path, p)

		switch {
		case p.PublicKey == "":
			r.errorf(path+".public_key", "required")
		default:
			if err := ValidKey(p.PublicKey); err != nil {
				r.errorf(path+".public_key", "%v", err)
			} else if first, dup := byKey[p.PublicKey]; dup {
				// Two names for one key is the shape of docs/remote-access.md
				// §2.1's warning made concrete, and unlike two devices sharing
				// one file it *is* detectable: only one of them can be
				// connected at a time, and which one is decided by whichever
				// handshaked last.
				r.errorf(path+".public_key",
					"%q already uses this key; one key is one device, and two devices sharing one "+
						"take turns being connected", w.Peers[first].Name)
			} else {
				byKey[p.PublicKey] = i
			}
		}

		validatePeerAddress(r, path, i, p, subnet, router, byAddr, w)

		if !p.Routes.Valid() {
			r.errorf(path+".routes", "unknown value %q (want %v)", p.Routes, RouteScopes())
		}
		validatePeerRoutes(r, path, p, networks)
	}

	if w.Enabled && len(w.Peers) == 0 {
		r.warnf("wireguard.peers",
			"remote access is on and no device can dial in yet; add one with `olr remote add peer <name>`")
	}
}

// validatePeerDisplayName checks the one field that is free text.
//
// Loose on purpose: a label is the operator's words. What it is not allowed to
// carry is a control character, because every rendering of it — a table cell, a
// page heading, a log line quoting it — is single-line, and a newline in a name
// breaks all of them at once (the same rule `devices` applies to its names).
func validatePeerDisplayName(r *Result, path, label string) {
	if label == "" {
		return
	}
	if n := utf8.RuneCountInString(label); n > MaxPeerDisplayNameLen {
		r.errorf(path+".display_name", "the label is %d characters; the limit is %d", n, MaxPeerDisplayNameLen)
		return
	}
	if i := strings.IndexFunc(label, unicode.IsControl); i >= 0 {
		r.errorf(path+".display_name", "the label contains a control character (%q)", label[i])
	}
}

func validatePeerName(r *Result, path, name string) {
	if len(name) > MaxPeerNameLen {
		r.errorf(path+".name", "%q is %d characters; a name may not exceed %d", name, len(name), MaxPeerNameLen)
		return
	}
	for _, c := range name {
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' {
			continue
		}
		r.errorf(path+".name", "%q contains %q; a name may use letters, digits and hyphens", name, string(c))
		return
	}
	if strings.HasPrefix(name, "-") || strings.HasSuffix(name, "-") {
		r.errorf(path+".name", "%q may not begin or end with a hyphen", name)
	}
}

func validatePeerAddress(r *Result, path string, i int, p Peer, subnet netip.Prefix, router netip.Addr, seen map[netip.Addr]int, w WireGuard) {
	switch {
	case !p.Address.IsValid():
		r.errorf(path+".address", "required: a peer's address is fixed when it is created, never served")
	case !subnet.Contains(p.Address):
		// The one that appears after somebody renumbers the dial-in network.
		// Refused rather than reallocated, because reallocating would silently
		// change an address that is written into a file on somebody's phone.
		r.errorf(path+".address",
			"%s is outside %s. The dial-in network moved and this peer did not; remove it and add it "+
				"again to get an address on the new range — its client configuration has to be replaced either way",
			p.Address, subnet)
	case p.Address == router:
		r.errorf(path+".address", "%s is this box's own address on the dial-in network", p.Address)
	default:
		if first, dup := seen[p.Address]; dup {
			r.errorf(path+".address", "%s is already held by %q", p.Address, w.Peers[first].Name)
			return
		}
		seen[p.Address] = i
	}
}

// validatePeerPicture checks the two fields that only decide how a device is
// drawn.
//
// Checked here rather than left to the UI, because a stored picture is stored
// intent and the document has to mean something to every reader — `devices`
// closes its vocabulary for the same reason: a value the renderer cannot
// resolve is a device that silently loses its picture after a reload. Both
// halves are the other module's rules, called rather than copied (design.md
// §4.1: one owner).
func validatePeerPicture(r *Result, path string, p Peer) {
	if !p.Category.Valid() {
		r.errorf(path+".category", "unknown category %q; valid values are %s",
			p.Category, categoryVocabulary())
	}
	if err := p.Icon.Validate(); err != nil {
		r.errorf(path+".icon", "%v", err)
	}
}

// categoryVocabulary renders the categories for a message, the way the module
// that owns them does.
func categoryVocabulary() string {
	all := clients.Categories()
	out := make([]string, 0, len(all))
	for _, c := range all {
		out = append(out, string(c))
	}
	return strings.Join(out, ", ")
}

// validatePeerRoutes reports the two things that make a client configuration
// technically valid and practically useless.
//
// Both are warnings rather than refusals, and deliberately: neither is wrong,
// and both describe something outside this module's control. docs/remote-access.md
// §8 is the longer version.
func validatePeerRoutes(r *Result, path string, p Peer, networks []NetworkInfo) {
	switch p.Routes.OrDefault() {
	case RouteHome:
		if hasIPv4Network(networks) {
			return
		}
		r.warnf(path+".routes",
			"there are no networks configured yet, so %q will reach this box and nothing behind it. "+
				"`olr net add <name>` is what gives it somewhere to go", p.Name)
	case RouteEverything:
		r.warnf(path+".routes",
			"%q will send all of its traffic here, and olr does not yet write the address translation "+
				"that lets it out again — port forwarding has its own (docs/port-forwarding.md) and nothing "+
				"covers ordinary egress. Unless something else on this box masquerades the dial-in "+
				"network, expect a tunnel that connects and reaches no internet", p.Name)
	}
}

func hasIPv4Network(networks []NetworkInfo) bool {
	for _, n := range networks {
		if n.Subnet.IsValid() && n.Subnet.Addr().Is4() {
			return true
		}
	}
	return false
}

// ownedKeys are the settings this module renders itself.
//
// The escape hatch is additive by design (design.md §3.2 rule 5). Letting it
// restate one of these would let the file `wg setconf` reads contradict the
// config that produced it, which defeats the single-source rule the hatch
// exists to preserve.
//
// Only the two `[Interface]` keys are owned. `PublicKey` and `AllowedIPs` are
// deliberately absent: a hand-written `[Peer]` block is the main thing this
// hatch is for, and both belong in one. The check is textual and therefore
// approximate — the authoritative one is `wg setconf` rejecting the file before
// anything else happens.
var ownedWireGuardKeys = map[string]string{
	"privatekey": "the box's key is generated and stored by olr",
	"listenport": "set from wireguard.listen_port",
}

func validateExtraConf(r *Result, extra string) {
	if strings.TrimSpace(extra) == "" {
		return
	}
	for i, line := range strings.Split(extra, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		key, _, ok := strings.Cut(trimmed, "=")
		if !ok {
			continue
		}
		if why, owned := ownedWireGuardKeys[strings.ToLower(strings.TrimSpace(key))]; owned {
			r.errorf("wireguard.raw_wireguard_conf",
				"line %d sets %q, which this module renders: %s", i+1, strings.TrimSpace(key), why)
		}
	}
}

package clients

import (
	"context"
	"net/netip"
	"slices"
	"strings"
	"time"
)

// The client list is the join design.md §4.4 describes: identity, which is
// ours and stored, against presence, which is observed and never stored as
// truth. This file is the only place that join happens, which is what makes
// "an operator's answer beats detection" a property of the system rather than a
// convention every caller has to remember.

// FixedAddressView is this module's read-only window onto whoever owns fixed
// addresses — today `dhcp`, through its reservations.
//
// The fixed address is *not* copied into this module's config, and that is the
// point of §4.1: `dhcp` remains the single owner of the fact, and this module
// reads it through an interface rather than keeping a second copy that could
// disagree. What §11.1 actually asked for is that the *workflow* start from the
// device rather than from a MAC typed into a form, and joining it here gives
// every client — UI, CLI, agent — that view without any of them reimplementing
// the join.
type FixedAddressView interface {
	// FixedAddresses maps canonical MAC to reserved address.
	FixedAddresses(ctx context.Context) (map[string]string, error)
}

// NetworkView is this module's read-only window onto whoever owns the address
// ranges — today `dhcp`, through its pools. The same one-way arrow as
// FixedAddressView: this module declares what it needs and never imports dhcp.
//
// It exists because the neighbour table is the only source that reports an
// interface, and it only knows clients it has heard from lately. A client that
// is merely asleep still holds a lease, and placing it by which range its
// address falls in is the difference between a map of the household and a map
// of whatever happens to be awake.
type NetworkView interface {
	// Networks returns this router's networks and the range served on each.
	Networks(ctx context.Context) ([]Network, error)
}

// Network is one of this router's networks: the name an operator gave it, the
// interfaces it lives on, and the range served there.
//
// Keyed by name rather than by interface since design.md §4.4's network landed.
// The distinction is not cosmetic here — this is the value that reaches the
// client list and the topology map as "which network is this on", so naming it
// by kernel interface meant the operator called a network `lan` everywhere
// except the one screen where they look for their clients.
type Network struct {
	// Name is the network's name, and what a placed device reports.
	Name string

	// Members are the kernel interfaces it lives on, for placing a client that
	// a source saw on an interface rather than by address.
	Members []string

	Start, End netip.Addr
}

// Holds reports whether an address falls inside this network's range.
func (n Network) Holds(addr netip.Addr) bool {
	if !addr.IsValid() || !n.Start.IsValid() || !n.End.IsValid() {
		return false
	}
	// Compare orders v4 before v6, so without this a v4 address would test as
	// being inside every v6 range — an answer that is wrong rather than absent.
	if addr.Is4() != n.Start.Is4() {
		return false
	}
	return addr.Compare(n.Start) >= 0 && addr.Compare(n.End) <= 0
}

// place returns the name of the first network holding the address.
func place(addr netip.Addr, networks []Network) (string, bool) {
	for _, n := range networks {
		if n.Holds(addr) {
			return n.Name, true
		}
	}
	return "", false
}

// placeInterface returns the name of the network an interface carries.
//
// A client seen on an interface that carries no network is left unplaced rather
// than reported as being on an interface. That is the same rule the range
// fallback follows and the one the field's own contract states: filing a client
// under a plausible-looking guess is the failure mode to avoid, and "bridge0"
// is not one of this router's networks just because a packet arrived on it.
func placeInterface(iface string, networks []Network) (string, bool) {
	for _, n := range networks {
		if slices.Contains(n.Members, iface) {
			return n.Name, true
		}
	}
	return "", false
}

// Origin says where a resolved value came from, so a UI can distinguish what it
// was told from what it inferred. A guess presented as a fact is the failure
// mode icon-style-spec.md is written to avoid.
type Origin string

const (
	// OriginOperator is stored intent: a human said so.
	OriginOperator Origin = "operator"

	// OriginDetected is inference from a MAC or a hostname.
	OriginDetected Origin = "detected"

	// OriginObserved is a fact read off the network, such as the hostname a
	// client announced.
	OriginObserved Origin = "observed"

	// OriginRemembered is a name or a time the router kept from when the client
	// was last here. No source can be asked now, so it is history rather than an
	// observation — see seen.go — and it is never intent.
	OriginRemembered Origin = "remembered"

	// OriginNone means nothing could supply a value.
	OriginNone Origin = ""
)

// History is what the router remembers about clients that are not here now:
// when it last heard them, and the name they last announced.
//
// It is an interface rather than *Seen so that the join can be exercised
// without a store, and it lives here for the reason FixedAddressView does: this
// module states the facts it needs and the composition root supplies them.
type History interface {
	LastSeen(mac string) (time.Time, bool)
	Hostname(mac string) (name string, at time.Time, ok bool)
}

// Resolved is one device with both halves joined.
type Resolved struct {
	MAC string

	// Name and NameOrigin: a stored name, else the hostname the client
	// announced, else the name it last announced while it was here, else empty —
	// in which case a UI falls back to the MAC. Neither observed name is ever
	// promoted into stored intent, and a remembered one is stamped with
	// NameAsOf, so the operator's own naming stays distinguishable from the
	// client's and the client's stays distinguishable from history.
	Name       string
	NameOrigin Origin

	// NameAsOf is when a remembered name was heard, set only for
	// OriginRemembered. Nil for a name something can confirm right now.
	NameAsOf *time.Time

	// Category and CategoryOrigin, resolved in the spec's order: operator, then
	// detection, then nothing.
	Category       Category
	CategoryOrigin Origin

	// Detected is what inference produced regardless of whether it won. Kept so
	// the UI can offer "we think this is a printer — accept?" on a client the
	// operator has never touched, and so an override is visibly an override.
	Detected Detected

	Model string
	Icon  Icon
	Notes string

	// Group is the operator's group for this client, empty for none. Never
	// inferred: a client is in a group only because somebody put it there.
	Group string

	// Stored reports whether there is a config entry for this client. False
	// means it is on the list purely because it was seen.
	Stored bool

	// Presence is nil for a client that is stored but has not been seen — the
	// statically-addressed printer between reboots. Nil is meaningfully
	// different from "seen but inactive" and the UI says so differently.
	Presence *Presence

	// FixedIP is the reserved address, owned by dhcp, empty if none.
	FixedIP string

	// Network is which of this router's networks the client is on, and
	// NetworkOrigin says how we know: OriginObserved when a source saw it there,
	// OriginDetected when it was placed by which range its address falls in.
	//
	// The distinction is the same one Name and Category make, and it matters for
	// the same reason. Observed is a fact from the kernel; detected is an
	// inference that a hand-set address outside every pool will quietly fail, so
	// a map drawn from this must be able to say "we could not place it" rather
	// than filing it under a plausible network.
	Network       string
	NetworkOrigin Origin

	// LastSeen is when the client was last heard from — see seen.go. Nil when
	// it has not been heard since the record began, which is not the same as
	// never seen: Presence can list it from a lease all the same.
	LastSeen *time.Time
}

// Online reports whether any source considers the client current.
func (r Resolved) Online() bool {
	return r.Presence != nil && r.Presence.Active
}

// DisplayName is what to show, falling back to the MAC so a client is never
// nameless on screen.
func (r Resolved) DisplayName() string {
	if r.Name != "" {
		return r.Name
	}
	return r.MAC
}

// Build joins stored identity, observed sightings, fixed addresses and what the
// router remembers into the list, and returns any problems collected along the
// way.
//
// The union of three key sets, not just the sightings: a client is on the list
// if a human described it, *or* something saw it, *or* it holds a reservation.
// Dropping any of those three would lose a real case — the printer named last
// year that is currently powered off, the guest phone nobody has named, and the
// reservation made for a client that has not connected yet.
//
// History adds names and times to those clients, never clients to the list: a
// MAC nobody described, saw or reserved is not a client the operator has here,
// and remembering every randomised address a phone ever used would be a list of
// everything rather than a map of the network.
func Build(cfg Config, sightings []Sighting, fixed map[string]string, networks []Network, history History) ([]Resolved, []Problem) {
	presence, problems := Merge(sightings)

	macs := map[string]bool{}
	for _, d := range cfg.Clients {
		macs[d.MAC] = true
	}
	for mac := range presence {
		macs[mac] = true
	}
	for mac := range fixed {
		macs[mac] = true
	}

	out := make([]Resolved, 0, len(macs))
	for mac := range macs {
		out = append(out, resolve(mac, cfg, presence, fixed, networks, history))
	}

	// Sorted by the name the operator reads, not by presence. Sorting online
	// clients first would reorder the whole list every time a phone slept,
	// which is the row-jumping that makes a table impossible to click in.
	slices.SortStableFunc(out, func(a, b Resolved) int {
		an, bn := strings.ToLower(a.DisplayName()), strings.ToLower(b.DisplayName())
		if n := strings.Compare(an, bn); n != 0 {
			return n
		}
		return strings.Compare(a.MAC, b.MAC)
	})

	return out, problems
}

// resolve applies the resolution order for one device.
func resolve(mac string, cfg Config, presence map[string]Presence, fixed map[string]string, networks []Network, history History) Resolved {
	r := Resolved{MAC: mac}

	if p, ok := presence[mac]; ok {
		copied := p
		r.Presence = &copied
	}

	if history != nil {
		if at, ok := history.LastSeen(mac); ok {
			r.LastSeen = &at
		}
	}

	// Which network the client is on, preferring what a source actually saw.
	// The neighbour table only knows clients it has heard from lately, so the
	// range fallback is what keeps a sleeping phone on the map — and the two are
	// complementary rather than redundant: ARP is the only thing that can place
	// a statically-addressed device, whose address sits outside every pool.
	if r.Presence != nil {
		// What a source saw is resolved through the interface's network rather
		// than reported raw. The two branches now answer the same question in
		// the same vocabulary — before, one said `lan` and the other said
		// `bridge0`, and which one a client got depended on whether it happened
		// to be awake.
		if len(r.Presence.Interfaces) > 0 {
			if name, ok := placeInterface(r.Presence.Interfaces[0], networks); ok {
				r.Network, r.NetworkOrigin = name, OriginObserved
			}
		}
		if r.Network == "" {
			for _, ip := range r.Presence.IPs {
				addr, err := netip.ParseAddr(ip)
				if err != nil {
					continue
				}
				if name, ok := place(addr, networks); ok {
					r.Network, r.NetworkOrigin = name, OriginDetected
					break
				}
			}
		}
	}

	hostname := ""
	if r.Presence != nil {
		hostname = r.Presence.Hostname
	}

	// The name the client last announced while it was here, when nothing can be
	// asked now. It is evidence about the same device, only older — which is
	// exactly why it is a fallback and not a replacement.
	remembered, rememberedAt := "", time.Time{}
	if hostname == "" && history != nil {
		if name, at, ok := history.Hostname(mac); ok {
			remembered, rememberedAt = name, at
		}
	}

	// Detection runs for every device, whether or not it wins, so that the UI
	// can always show what we would have guessed. A remembered hostname is
	// evidence too: a phone away for a week is still the phone its own name
	// says it is, and the picture should not fall back to the vendor's generic
	// one merely because the lease lapsed.
	evidence := hostname
	if evidence == "" {
		evidence = remembered
	}
	r.Detected = Detect(mac, evidence)

	stored, ok := cfg.Find(mac)
	if ok {
		r.Stored = true
		r.Model = stored.Model
		r.Icon = stored.Icon
		r.Notes = stored.Notes
		r.Group = stored.Group
	}

	switch {
	case stored.Name != "":
		r.Name, r.NameOrigin = stored.Name, OriginOperator
	case hostname != "":
		r.Name, r.NameOrigin = hostname, OriginObserved
	case remembered != "":
		r.Name, r.NameOrigin = remembered, OriginRemembered
		if !rememberedAt.IsZero() {
			at := rememberedAt
			r.NameAsOf = &at
		}
	default:
		r.Name, r.NameOrigin = "", OriginNone
	}

	// The resolution order from icon-style-spec.md, in one place. An operator
	// override is unconditional: a client whose picture was corrected must not
	// have it silently changed back by the next fingerprint update.
	switch {
	case stored.Category != "":
		r.Category, r.CategoryOrigin = stored.Category, OriginOperator
	case r.Detected.Category != "":
		r.Category, r.CategoryOrigin = r.Detected.Category, OriginDetected
	default:
		r.Category, r.CategoryOrigin = CategoryUnknown, OriginNone
	}

	r.FixedIP = fixed[mac]

	return r
}

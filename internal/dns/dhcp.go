package dns

import "net/netip"

// This module's read-only window onto whoever owns fixed addresses — today
// `dhcp`, through its reservations.
//
// design.md §4.1 names this exact pair as the cycle that has to be broken by
// inverting one side, and fixes which side: `dhcp` publishes, `dns` subscribes.
// The interface is declared here, by the consumer, for the reason
// internal/dns/link.go gives — it states the three facts dns has an opinion
// about instead of exposing all of dhcp's surface, and dns imports nothing to
// get them. internal/daemon adapts dhcp's own Reservation into it.
//
// **It feeds Validate and nothing else, which is the whole design of it.**
// Rendering deliberately does not read this, and the temptation to make it is
// the trap worth naming: a local name that came from a reservation would be
// correct exactly once. Rendered bytes are what drift is measured against
// (design.md §5.4), so a reservation edit would leave this module *drifted*
// until somebody applied it — and drift is supposed to mean a human edited the
// box, not that a sibling module moved on. The alternative, re-applying dns
// whenever dhcp applies, breaks the promise underneath every mutating route in
// olr: one request, and the plan decides whether it lands (§5.3.3). A dhcp plan
// that silently restarted the resolver is precisely the surprise that promise
// exists to prevent.
//
// So the subscription buys a *warning*: it cannot change a byte, which means it
// cannot drift and needs no cross-module apply, and it still catches the one
// failure that is otherwise silent — the name and the reservation disagreeing
// about where a client is.
type ReservationView interface {
	// Reservations lists every fixed address, in dhcp's canonical order.
	Reservations() ([]Reservation, error)
}

// Reservation is the subset of a dhcp reservation dns has an opinion about.
type Reservation struct {
	// MAC is canonical, as core.NormalizeMAC leaves it.
	MAC string

	// IP is the address dhcp hands that client.
	IP netip.Addr

	// Hostname is what dhcp calls it, and may be empty — most reservations
	// have no name at all.
	Hostname string
}

// reservationsFor reads the view, tolerating both a nil view and a failure.
//
// Neither is an error here. A nil view is what the tests and any future caller
// without a dhcp module hand us; a read that fails means the document is
// unreadable, which every other rule in this file is already reporting. Both
// cost the cross-check and nothing else.
func reservationsFor(view ReservationView) []Reservation {
	if view == nil {
		return nil
	}
	out, err := view.Reservations()
	if err != nil {
		return nil
	}
	return out
}

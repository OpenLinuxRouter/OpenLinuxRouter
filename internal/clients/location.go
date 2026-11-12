package clients

import (
	"context"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// Where a client is attached is not a fact this router owns.
//
// The router routes; a switch in the middle is what learns which of its ports a
// frame arrived on. So "which port is the TV on" is a question to the equipment,
// and the honest answers are "port 5", "that switch cannot say", and "we have no
// equipment that could". This file states the question; internal/daemon answers
// it over the equipment inventory, for the reason FixedAddressView gives: this
// module asks for the fact it needs and never imports the module that owns it.

// Location is one piece of equipment's answer about one client.
type Location struct {
	// DeviceID is the inventory entry that answered, and Device is the name the
	// operator gave it. Both, because the ID is what another call takes and the
	// name is what a person reads.
	DeviceID string
	Device   string

	// Port is the port identifier verbatim: an SE109 Pro says "3", an OpenWrt
	// box says "lan2". Not renumbered or prettified — a number the operator
	// cannot match to a label on the box is worse than one they have to
	// translate once.
	Port string

	// VLAN is the VLAN the entry was learnt in, zero when the device does not
	// say. It is the device's answer, not this router's configuration.
	VLAN int

	// ObservedAt is when the device reported it, not when we asked.
	ObservedAt time.Time
}

// Locator answers where a client is attached, from whichever equipment can say.
//
// A device that cannot report a forwarding table contributes a problem and no
// location rather than an error: "this switch cannot say" and "this switch says
// it is not here" are different answers, and collapsing them would let a switch
// with no such feature look like a switch that looked and found nothing.
type Locator interface {
	Locations(ctx context.Context, mac string) ([]Location, []core.Problem, error)
}

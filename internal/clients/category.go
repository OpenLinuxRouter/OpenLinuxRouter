package clients

// Category is what kind of thing a client is: the word the operator sees, and
// the thing that picks its picture.
//
// The vocabulary is deliberately closed and small. An open string would mean
// every deployment invented its own spelling of "printer", no icon would ever
// match twice, and the enum could never be published to the UI as a drop-down
// (design.md §3.2 rule 3). A closed list also makes the honest answer
// available: a client we cannot place is CategoryUnknown, not a guess.
//
// Adding a value here is a one-line change and needs no image — resolution
// falls back to the unknown icon while still showing the correct *label*, which
// is what lets the icon set be filled in gradually.
type Category string

const (
	// CategoryUnknown is the zero value in spirit but not in encoding: an
	// unset category means "nobody has said, so detection may answer", while
	// an explicit "unknown" means "the operator looked and could not place it".
	// Keeping those distinct is what stops a detection pass from overwriting a
	// deliberate answer.
	CategoryUnknown Category = "unknown"

	// Personal.
	CategoryPhone   Category = "phone"
	CategoryTablet  Category = "tablet"
	CategoryLaptop  Category = "laptop"
	CategoryDesktop Category = "desktop"
	CategoryWatch   Category = "watch"
	CategoryEreader Category = "ereader"

	// Media.
	CategoryTV      Category = "tv"
	CategorySpeaker Category = "speaker"
	CategoryConsole Category = "console"

	// Home automation.
	CategoryCamera     Category = "camera"
	CategoryDoorbell   Category = "doorbell"
	CategoryThermostat Category = "thermostat"
	CategorySensor     Category = "sensor"
	CategoryPlug       Category = "plug"
	CategoryLight      Category = "light"
	CategoryVacuum     Category = "vacuum"

	// Appliances. The connected white goods a home network fills up with, each
	// its own word because "sensor" or "plug" would put the wrong picture on a
	// fridge.
	CategoryAirPurifier   Category = "airpurifier"
	CategoryWaterPurifier Category = "waterpurifier"
	CategoryHumidifier    Category = "humidifier"
	CategoryAirCon        Category = "aircon"
	CategoryAirer         Category = "airer"
	CategoryFridge        Category = "fridge"
	CategoryWasher        Category = "washer"

	// Computing and peripherals.
	CategoryPrinter Category = "printer"
	CategoryNAS     Category = "nas"
	CategoryServer  Category = "server"
	CategorySBC     Category = "sbc"

	// Network infrastructure.
	//
	// The modem is the provider's box, upstream of everything an operator
	// owns: a fibre ONT — the 光猫 on the wall — or a cable or DSL modem. It
	// earns a category rather than a variant of router because it is a
	// different *thing*, not a different shape of the same thing: it is
	// usually not ours, it usually cannot be configured from here, and naming
	// it is how someone says "the internet comes in through that one".
	CategoryModem       Category = "modem"
	CategoryRouter      Category = "router"
	CategoryAccessPoint Category = "accesspoint"
	CategorySwitch      Category = "switch"
	CategoryHub         Category = "hub"
)

// categories is the vocabulary in display order, which is grouped by how an
// operator thinks about their network rather than alphabetically. The picker
// renders it in this order.
//
// This slice is the single source: Categories, the JSON Schema enum, and
// validation all read it, so a value added above and here cannot be legal in
// one place and rejected in another.
var categories = []Category{
	CategoryUnknown,

	CategoryPhone, CategoryTablet, CategoryLaptop, CategoryDesktop,
	CategoryWatch, CategoryEreader,

	CategoryTV, CategorySpeaker, CategoryConsole,

	CategoryCamera, CategoryDoorbell, CategoryThermostat, CategorySensor,
	CategoryPlug, CategoryLight, CategoryVacuum,

	CategoryAirPurifier, CategoryWaterPurifier, CategoryHumidifier,
	CategoryAirCon, CategoryAirer, CategoryFridge, CategoryWasher,

	CategoryPrinter, CategoryNAS, CategoryServer, CategorySBC,

	CategoryModem, CategoryRouter, CategoryAccessPoint, CategorySwitch, CategoryHub,
}

// Categories returns the vocabulary in display order.
func Categories() []Category {
	return append([]Category(nil), categories...)
}

// Valid reports whether c is a member of the vocabulary.
//
// The empty string is valid and means "unset, detection may answer" — see
// CategoryUnknown. A schema that rejected "" would refuse a document this
// module itself accepts, which is the trap RAMode's schema comment in
// internal/dhcp/schema.go already calls out.
func (c Category) Valid() bool {
	if c == "" {
		return true
	}
	for _, known := range categories {
		if c == known {
			return true
		}
	}
	return false
}

// Or returns c, or fallback when c is unset. It is the one-line spelling of the
// resolution order in the icon style spec: an operator's answer beats a
// detected one, always.
func (c Category) Or(fallback Category) Category {
	if c == "" {
		return fallback
	}
	return c
}

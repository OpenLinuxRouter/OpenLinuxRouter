package clients

import (
	"slices"
	"strings"
)

// VendorKey is a stable slug for a vendor we might one day draw a picture of.
//
// It exists because Vendor is a *label* and an icon needs a *key*. "TP-Link"
// is what goes under a client; `tp-link` is what names a file. Deriving one
// from the other in the UI would work right up until someone improved a name
// in the generator's alias list — the label would get better and every picture
// keyed on it would silently stop resolving. Sending both means the two cannot
// drift, because the same table decides them.
//
// The vocabulary is closed and small, for the reason Category's is (see
// category.go): an open slug space would mean artwork keyed on whatever
// spelling the registry happened to use that year, and no way to check a key
// against anything. Closed also makes the guarantee testable — every value
// below is asserted to be reachable from a real hardware address, so a key can
// never be added for a vendor no device can produce.
//
// Absence is the normal case and is not a failure. Most of the 29,838 vendors
// in the registry have no key, and a client made by one of them still shows its
// vendor *name*; it just falls through to its category for a picture. Adding a
// key is one line here and one in the UI's VendorKey union.
type VendorKey string

// vendorKeyByName maps a resolved vendor name to its key.
//
// Keyed on the name *after* cleanup and aliasing — the string that actually
// reaches Detected.Vendor, whether it came from ouiTable or from the registry.
// Lower-cased on both sides at lookup so an alias that changes case does not
// break the join.
//
// The set is deliberately a starter: vendors common on a home network whose
// hardware looks like something in particular. Adding to it costs nothing and
// changes nothing until artwork exists, which is the property that lets the
// list grow by whoever happens to be drawing that week.
var vendorKeyByName = map[string]VendorKey{
	// Personal computing.
	"apple":   "apple",
	"samsung": "samsung",
	"google":  "google",
	"dell":    "dell",
	"lenovo":  "lenovo",
	"hp":      "hp",
	"hpe":     "hpe",
	"asus":    "asus",
	"acer":    "acer",
	"intel":   "intel",
	"huawei":  "huawei",
	"xiaomi":  "xiaomi",

	// Media and the living room.
	"amazon":    "amazon",
	"microsoft": "microsoft",
	"sony":      "sony",
	"lg":        "lg",
	"nintendo":  "nintendo",
	"sonos":     "sonos",
	"roku":      "roku",

	// Infrastructure, which is what a router's own screen shows most of.
	"tp-link":  "tp-link",
	"netgear":  "netgear",
	"ubiquiti": "ubiquiti",
	"mikrotik": "mikrotik",
	"cisco":    "cisco",
	"zyxel":    "zyxel",

	// Things that sit in a cupboard.
	"synology":     "synology",
	"qnap":         "qnap",
	"raspberry pi": "raspberry-pi",
	"espressif":    "espressif",
	"philips hue":  "philips-hue",
	"brother":      "brother",
	"canon":        "canon",
	"epson":        "epson",
}

// VendorKeyFor returns the key for a resolved vendor name, or empty when the
// vendor has none — which is most of them, and is not a problem to solve.
func VendorKeyFor(vendor string) VendorKey {
	if vendor == "" {
		return ""
	}
	return vendorKeyByName[strings.ToLower(strings.TrimSpace(vendor))]
}

// VendorKeys returns every key, sorted, for tests and for anyone filling in
// artwork who wants to know what is drawable.
func VendorKeys() []VendorKey {
	keys := make([]VendorKey, 0, len(vendorKeyByName))
	for _, key := range vendorKeyByName {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

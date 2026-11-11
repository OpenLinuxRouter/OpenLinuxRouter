package clients

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/invopop/jsonschema"
)

// Icon is a picture an operator chose for a client, overriding the one its
// category and vendor would pick. Five shapes are legal:
//
//	<vendor>/<category>             a vendor's take on a kind of device: "apple/laptop"
//	<category>/<variant>            one form of a kind of device: "nas/4bay"
//	<vendor>/<category>/<variant>   one form of a vendor's take: "apple/desktop/mini"
//	os/<system>                     an operating system's mark: "os/debian"
//	thesvg:<slug>                   a logo from theSVG library
//
// The first is the answer to randomised MACs. A modern phone or laptop hides
// its maker from the registry, so the Apple pictures a detected vendor would
// select are unreachable for exactly the devices most likely to be Apple ones;
// letting the operator say so is the only way to get there. The second is for
// machines better known by what they run — a Proxmox host, a Debian VM — where
// a generic server picture says less than the logo does. A variant is for a
// kind of device that comes in shapes too different to share one picture: a
// NAS with eight bays is not the two-bay box, and a Mac mini is not an iMac.
//
// Closed on both halves, like Category and VendorKey, so a stored icon always
// names something the UI can resolve or deliberately fall back from. Whether a
// picture has actually been *drawn* for a given vendor/category pair is the
// UI's business (see IMAGES in the SPA's features/devices/icons.ts): an
// undrawn pair falls back to the category's own picture rather than failing,
// which is what lets the artwork keep growing without a config migration.
type Icon string

// iconOSPrefix marks an operating-system icon.
const iconOSPrefix = "os/"

var thesvgIconPattern = regexp.MustCompile(`^thesvg:[a-z0-9][a-z0-9-]{0,95}$`)

// operatingSystems is the vocabulary of OS marks, in the order a picker shows
// them: desktop and phone systems first, then the Linux family, then what a
// homelab runs. macOS and iOS share "apple" — the mark is the same, and their
// own wordmarks are unreadable at icon size.
var operatingSystems = []string{
	"windows", "apple", "android", "harmonyos",
	"linux", "ubuntu", "debian", "fedora", "archlinux",
	"freebsd", "proxmox", "openwrt", "raspberrypi",
}

// variants is the vocabulary of variant pictures, keyed by what they are a
// variant *of*: a category, or a vendor's take on one. Closed like the rest.
//
// It may run a step ahead of the artwork, and the Synology bay counts do: a
// variant nobody has painted is a config value the picker never offers — the
// SPA draws only what its IMAGES map carries — but it is *valid*, so the day a
// picture lands the operator does not have to wait for a validator change to
// use it.
var variants = map[string][]string{
	// The bay-count family used to hang off the category, as "nas/4bay". The
	// pictures were always Synology-shaped — the DS box is what the set is
	// drawn from — so the picker files them under the maker now. These keys
	// stay valid because a config that stored one must still load, and the SPA
	// still draws them from the moved pictures (see MOVED_ICONS in
	// features/devices/icons.ts); nothing offers them any more.
	"nas": {"4bay", "5bay", "6bay", "8bay"},
	// One to twelve bays, drawn as one product line so the family reads as one.
	// "2bay" is absent on purpose: the two-bay box is the vendor picture
	// itself, "synology/nas", which is also what a detected Synology NAS wears
	// before anyone says how many bays it has. The rest are one swap of a
	// number in one prompt, and the picker shows the ones that have been
	// painted.
	"synology/nas":   {"1bay", "3bay", "4bay", "5bay", "6bay", "8bay", "12bay"},
	"apple/desktop":  {"mini", "studio"},
	"intel/desktop":  {"nuc12extreme"},
	"xiaomi/router":  {"be7000"},
	"tp-link/router": {"xdr6080"},
	// A tower server is not the rack unit `server` draws, and a mini PC is
	// not the monitor-and-tower `desktop` draws. Each is the same *kind* of
	// thing in a shape nothing else in the set shares, which is what a
	// variant is for — and both are what a home actually runs a server on.
	"server": {"tower"},
	// iKOOLCORE models are explicit mini-PC choices; the NIC's vendor does
	// not reliably identify the manufacturer of the enclosure.
	"desktop": {"mini", "ikoolcorer1", "ikoolcorer2", "ikoolcorer2max"},
}

// OperatingSystems returns the OS vocabulary in display order.
func OperatingSystems() []string {
	return slices.Clone(operatingSystems)
}

// Validate reports why i is not a legal icon, or nil. The empty icon is legal
// and means "follow category and vendor".
func (i Icon) Validate() error {
	if i == "" {
		return nil
	}
	s := string(i)
	if strings.HasPrefix(s, "thesvg:") {
		if !thesvgIconPattern.MatchString(s) {
			return fmt.Errorf("icon %q needs a theSVG slug of 1–96 lowercase letters, digits or hyphens, starting with a letter or digit", s)
		}
		return nil
	}
	if os, ok := strings.CutPrefix(s, iconOSPrefix); ok {
		if !slices.Contains(operatingSystems, os) {
			return fmt.Errorf("unknown operating system %q in icon %q; valid values are %s",
				os, s, strings.Join(operatingSystems, ", "))
		}
		return nil
	}

	parts := strings.Split(s, "/")
	if len(parts) == 2 && parts[0] != "" && Category(parts[0]).Valid() {
		return validateVariant(s, parts[0], parts[1])
	}
	if len(parts) == 3 {
		base := parts[0] + "/" + parts[1]
		if err := validateVendorCategory(s, parts[0], parts[1]); err != nil {
			return err
		}
		return validateVariant(s, base, parts[2])
	}
	if len(parts) != 2 {
		return fmt.Errorf("icon %q is none of <vendor>/<category>, <category>/<variant>, "+
			"<vendor>/<category>/<variant> or os/<system>", s)
	}
	return validateVendorCategory(s, parts[0], parts[1])
}

func validateVendorCategory(s, vendor, category string) error {
	if !slices.Contains(VendorKeys(), VendorKey(vendor)) {
		return fmt.Errorf("unknown vendor %q in icon %q", vendor, s)
	}
	// A vendor's picture of "nothing in particular" is the one thing ICONS.md
	// refuses to draw, so the category half has to name a real kind of thing.
	if c := Category(category); c == "" || c == CategoryUnknown || !c.Valid() {
		return fmt.Errorf("icon %q needs a category after the vendor; valid values are %s",
			s, joinCategories())
	}
	return nil
}

func validateVariant(s, base, variant string) error {
	known, ok := variants[base]
	if !ok {
		return fmt.Errorf("icon %q: %q has no variants", s, base)
	}
	if !slices.Contains(known, variant) {
		return fmt.Errorf("unknown variant %q in icon %q; valid values are %s",
			variant, s, strings.Join(known, ", "))
	}
	return nil
}

// JSONSchema publishes the shape rather than the full list: vendor × category
// is several hundred strings, which would bloat the schema and every tool
// definition built from it for no gain over the validator's own message.
func (Icon) JSONSchema() *jsonschema.Schema {
	return &jsonschema.Schema{
		Type:  "string",
		Title: "Client picture",
		Description: "A picture chosen for this device, overriding the one its " +
			"category and vendor would pick: <vendor>/<category> such as " +
			"\"apple/laptop\", optionally followed by /<variant> such as " +
			"\"apple/desktop/mini\", <category>/<variant> such as \"server/tower\", " +
			"os/<system> such as \"os/debian\", or thesvg:<slug> such as \"thesvg:proxmox\". Empty " +
			"means the picture follows category and vendor.",
		Pattern: `^$|^[a-z0-9-]+/[a-z0-9]+(/[a-z0-9]+)?$|^thesvg:[a-z0-9][a-z0-9-]{0,95}$`,
		Default: "",
	}
}

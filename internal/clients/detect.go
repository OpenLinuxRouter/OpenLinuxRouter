package clients

import (
	"strings"

	"github.com/open-linux-router/open-linux-router/internal/clients/ouidb"
	"github.com/open-linux-router/open-linux-router/internal/core"
)

// Category detection: a guess, labelled as a guess, that an operator can
// override permanently.
//
// The governing rule comes from icon-style-spec.md, and it is about honesty
// rather than accuracy. A client inventory whose pictures are subtly wrong is
// worse than one that admits it only knows the category — so every rule here
// either fires on strong evidence or does not fire at all. There is no
// "probably a laptop because the vendor mostly makes laptops": a miss falls
// through to an unset category, the UI says nothing, and the operator's answer
// is asked for instead of guessed at.
//
// Two signals, in this order:
//
//  1. **Hostname.** What the client calls itself. Much the stronger signal, and
//     the one the operator can verify by looking at the client, which matters
//     when they are deciding whether to trust the guess.
//  2. **OUI.** The vendor half of the MAC, read twice. ouiTable below is a
//     short hand-placed list, and it is the only thing here allowed to produce
//     a *category*, because it only lists vendors that make one kind of thing.
//     internal/clients/ouidb is the IEEE registry and produces a *vendor name*
//     for almost any globally-assigned address — and never a category, because
//     knowing Apple registered a prefix says nothing about whether the client
//     is a phone, a watch, a laptop or a TV box. That asymmetry is why Vendor
//     and Category are separate fields rather than one answer.
//
// Neither signal reaches a locally-administered address, which is most phones
// and laptops now that MAC randomisation is on by default. Those get no vendor
// and no category, and the screen has to be readable anyway.

// Detected is what inference produced. Category may be empty, and that is a
// result rather than a failure — it means nothing here was confident enough.
type Detected struct {
	Category Category
	Vendor   string

	// VendorKey is the icon slug for Vendor, and is empty for the great
	// majority of vendors, which have no artwork and never will. Resolved here
	// rather than derived from Vendor by the UI: the name is a label and is
	// allowed to be improved, and a slug computed from it would orphan every
	// picture keyed on the old spelling without anything failing. See vendor.go.
	VendorKey VendorKey

	// Reason names the signal that fired, so the UI can say *why* it thinks a
	// device is a printer. An unexplained guess is one an operator has no basis
	// to accept or correct.
	Reason string
}

// hostnameRule matches one token of a lower-cased hostname.
//
// Order matters: the first match wins, so more specific patterns come first.
// "appletv" must beat "apple", and "galaxytab" must beat "galaxy" or every
// Samsung tablet is filed as a phone. Note that no rule matches "switch" at
// all: on a home LAN that word is a games console about as often as it is a
// network switch, and a coin flip does not belong in an inventory.
type hostnameRule struct {
	needle   string
	category Category

	// exact requires the token to equal the needle rather than merely start
	// with it. Set for needles short or generic enough to be the opening of an
	// unrelated word: "nas" begins "nasa", "pixel" begins "pixelbook" (a
	// laptop, not a phone), "echo" begins "echolab".
	//
	// The cost is a miss on forms like "nas01". That is the right way round to
	// fail: a client that falls through to unset gets asked about once and the
	// answer is stored forever, whereas a confidently wrong category is a
	// picture the operator has to notice is wrong before they can fix it.
	exact bool
}

// Needles are matched against whole tokens (see hostnameRule.matches), so they
// never contain a separator themselves — the tokeniser also offers the whole
// name with separators stripped, which is what lets "appletv" match a host
// announcing itself as "Apple-TV-Living-Room".
var hostnameRules = []hostnameRule{
	// Appliances. Mostly Xiaomi-ecosystem hostnames, which name the product
	// in the second token: "zhimi-airpurifier-ma4", "yunmi-waterpuri-lx12".
	// "airer" is exact so it cannot match inside a longer word; it is a
	// clothes-drying rack ("hyd-airer-znlyj"), not an air purifier.
	//
	// First in the table because the vendor usually leads the hostname, and
	// "xiaomi" below would otherwise file an air conditioner as a phone.
	{"airpurifier", CategoryAirPurifier, false},
	{"waterpuri", CategoryWaterPurifier, false},
	{"humidifier", CategoryHumidifier, false},
	{"aircondition", CategoryAirCon, false},
	{"airer", CategoryAirer, true},
	{"fridge", CategoryFridge, true},
	{"washer", CategoryWasher, true},

	// Media first: several contain a vendor word that later rules also match.
	{"appletv", CategoryTV, false},
	{"chromecast", CategoryTV, false},
	{"firetv", CategoryTV, false},
	{"roku", CategoryTV, false},
	{"bravia", CategoryTV, false},
	{"webos", CategoryTV, false},
	{"samsungtv", CategoryTV, false},
	{"shield", CategoryTV, true},

	{"homepod", CategorySpeaker, false},
	{"sonos", CategorySpeaker, false},
	{"echo", CategorySpeaker, true},
	{"alexa", CategorySpeaker, true},

	{"playstation", CategoryConsole, false},
	{"ps4", CategoryConsole, true},
	{"ps5", CategoryConsole, true},
	{"xbox", CategoryConsole, false},
	{"nintendo", CategoryConsole, false},
	{"steamdeck", CategoryConsole, false},

	// Personal. The tablet rules precede the phone rules they share a prefix
	// with, so "galaxytabs8" is not filed as a phone.
	{"applewatch", CategoryWatch, false},
	{"iphone", CategoryPhone, false},
	{"ipad", CategoryTablet, false},
	{"macbook", CategoryLaptop, false},
	{"imac", CategoryDesktop, true},
	{"kindle", CategoryEreader, false},
	{"kobo", CategoryEreader, true},
	{"remarkable", CategoryEreader, false},

	{"galaxytab", CategoryTablet, false},
	{"pixeltablet", CategoryTablet, false},
	{"galaxy", CategoryPhone, false},
	{"pixel", CategoryPhone, true},
	{"oneplus", CategoryPhone, false},
	{"redmi", CategoryPhone, false},
	{"xiaomi", CategoryPhone, false},

	{"thinkpad", CategoryLaptop, false},
	{"elitebook", CategoryLaptop, false},
	{"probook", CategoryLaptop, false},
	{"latitude", CategoryLaptop, false},
	{"ideapad", CategoryLaptop, false},
	{"zenbook", CategoryLaptop, false},
	{"framework", CategoryLaptop, false},
	{"laptop", CategoryLaptop, false},
	{"desktop", CategoryDesktop, false},

	// Computing and peripherals.
	{"raspberrypi", CategorySBC, false},
	{"beaglebone", CategorySBC, false},
	{"odroid", CategorySBC, false},

	{"diskstation", CategoryNAS, false},
	{"synology", CategoryNAS, false},
	{"truenas", CategoryNAS, false},
	{"freenas", CategoryNAS, false},
	{"unraid", CategoryNAS, false},
	{"qnap", CategoryNAS, false},
	{"nas", CategoryNAS, true},

	{"laserjet", CategoryPrinter, false},
	{"officejet", CategoryPrinter, false},
	{"deskjet", CategoryPrinter, false},
	{"envy", CategoryPrinter, true},
	{"brother", CategoryPrinter, false},
	{"epson", CategoryPrinter, true},
	{"canon", CategoryPrinter, true},
	{"printer", CategoryPrinter, false},

	// Home automation.
	{"doorbell", CategoryDoorbell, false},
	{"thermostat", CategoryThermostat, false},
	{"ecobee", CategoryThermostat, false},
	{"camera", CategoryCamera, false},
	{"ipcam", CategoryCamera, false},
	{"roborock", CategoryVacuum, false},
	{"roomba", CategoryVacuum, false},

	// Network infrastructure.
	//
	// The provider's box first, because it is upstream of everything else and
	// because an ONT is the one device on a home LAN that announces its own
	// part number as its hostname: "HG8145V5" is a Huawei fibre modem,
	// "ZXHN-F670L" a ZTE one. That is the most reliable fingerprint in this
	// table — nobody renames the box the installer left behind.
	//
	// "ont" and "modem" are exact for the reason the struct documents: both
	// are the opening of unrelated words, and an operator who has to correct a
	// confidently wrong picture is worse off than one who was simply asked.
	{"hg8145", CategoryModem, false},
	{"hg8245", CategoryModem, false},
	{"hg8546", CategoryModem, false},
	{"hg8045", CategoryModem, false},
	{"hg6543", CategoryModem, false},
	{"zxhn", CategoryModem, false},
	{"fiberhome", CategoryModem, false},
	{"gpon", CategoryModem, false},
	{"ont", CategoryModem, true},
	{"modem", CategoryModem, true},

	{"unifi", CategoryAccessPoint, false},
	{"accesspoint", CategoryAccessPoint, false},
	{"openwrt", CategoryRouter, false},
	{"router", CategoryRouter, false},
}

// ouiEntry is one hand-placed prefix: the name to print and the kind of thing
// it is.
//
// Category may be left empty, and the zero value is meaningful — it says "we
// want this vendor's name but will not claim to know what the client is". No
// entry needs it today, because a vendor whose name we have no quarrel with is
// better served by the registry than by a line here. The field stays because
// the pairing is the point: an Apple OUI tells us Apple, and choosing between a
// phone, a watch, a laptop and a TV box from three octets would be inventing
// information.
type ouiEntry struct {
	vendor   string
	category Category
}

// ouiTable is the hand-maintained overlay on top of the IEEE registry.
//
// It used to be the whole vendor lookup: forty prefixes standing in for a
// database nobody had written. internal/clients/ouidb is that database now, and
// this table kept the two jobs a registry cannot do.
//
//  1. **Category.** IEEE records who registered a prefix and stops there. That a
//     Raspberry Pi Foundation OUI means a single-board computer is a judgement
//     about a product line, and it lives here because a person made it.
//  2. **The name we would rather print.** The registry says "Signify
//     Netherlands"; the label under the lamp in someone's hall should say
//     "Philips Hue". The generator carries an alias list for the general case,
//     but an entry here wins over both, and wins as a unit.
//
// So it stays small — smaller than it was, in fact. Every vendor-only entry is
// gone: twenty hand-copied Apple, Samsung, Intel, Ubiquiti and TP-Link prefixes
// were a worse answer than the registry's fifteen hundred, and keeping them
// would have implied this table was still where vendors come from. What is left
// all carries a category, which is the thing the registry genuinely cannot
// supply.
//
// Being unlisted is no longer a dead end: a prefix that misses here falls
// through to the registry for its vendor, and to no category at all — the
// honest outcome, and the one an operator fixes by naming the client once.
//
// Keyed by 24-bit prefix, lower-case and colon-separated, the form core.OUI
// returns. The registry's 28- and 36-bit blocks are ouidb's business; nothing
// worth a hand-written category has yet needed finer than an MA-L.
var ouiTable = map[string]ouiEntry{
	// Single-product vendors: the category is safe.
	"b8:27:eb": {"Raspberry Pi", CategorySBC},
	"dc:a6:32": {"Raspberry Pi", CategorySBC},
	"e4:5f:01": {"Raspberry Pi", CategorySBC},
	"28:cd:c1": {"Raspberry Pi", CategorySBC},
	"00:11:32": {"Synology", CategoryNAS},
	"00:17:88": {"Philips Hue", CategoryLight},
	"00:0e:58": {"Sonos", CategorySpeaker},
	"48:a6:b8": {"Sonos", CategorySpeaker},
	"5c:aa:fd": {"Sonos", CategorySpeaker},
	"00:1b:a9": {"Brother", CategoryPrinter},
	"00:09:bf": {"Nintendo", CategoryConsole},
	"98:b6:e9": {"Nintendo", CategoryConsole},
	"e8:4e:ce": {"Nintendo", CategoryConsole},

	// Espressif: the chip inside a very large fraction of DIY and cheap
	// commercial home-automation gear. "Sensor" is the least wrong single word
	// for it and is frequently what these actually are.
	"24:0a:c4": {"Espressif", CategorySensor},
	"30:ae:a4": {"Espressif", CategorySensor},
	"84:0d:8e": {"Espressif", CategorySensor},
	"a4:cf:12": {"Espressif", CategorySensor},
	"bc:dd:c2": {"Espressif", CategorySensor},
	"cc:50:e3": {"Espressif", CategorySensor},
}

// Detect infers what it can from a MAC and an observed hostname.
//
// It never sees stored identity, and that is deliberate: the caller applies the
// operator's answer on top (see resolve in list.go), so there is exactly one
// place where "override beats detection" is implemented and it cannot be
// bypassed by a future caller of this function.
func Detect(mac, hostname string) Detected {
	var d Detected

	if oui, ok := core.OUI(mac); ok {
		if entry, found := ouiTable[oui]; found {
			d.Vendor = entry.vendor
			if entry.category != "" {
				d.Category = entry.category
				d.Reason = "vendor " + entry.vendor
			}
		}
	}

	// The registry answers whatever the overlay did not. Second, so a
	// hand-placed name keeps winning: ouiTable exists partly to disagree with
	// IEEE about what a vendor is called, and a lookup that overwrote it would
	// make those entries silently decorative.
	//
	// No category comes from here, ever. The registry knows who registered a
	// prefix and nothing about what was built with it, and a vendor name is not
	// evidence of a client kind — which is why this sets one field.
	if d.Vendor == "" {
		if vendor, found := ouidb.Vendor(mac); found {
			d.Vendor = vendor
		}
	}

	// Resolved once, from whichever of the two sources answered, so both routes
	// to a vendor produce the same key. Doing it per-source would let ouiTable
	// and the registry disagree about what "Philips Hue" is called in a
	// filename, which is the whole thing VendorKey exists to prevent.
	d.VendorKey = VendorKeyFor(d.Vendor)

	// Hostname last in code, first in precedence: it overwrites an OUI-derived
	// category because it is the stronger signal.
	if tokens := hostnameTokens(hostname); len(tokens) > 0 {
		for _, rule := range hostnameRules {
			if rule.matches(tokens) {
				d.Category = rule.category
				d.Reason = "hostname matches " + rule.needle
				break
			}
		}
	}

	return d
}

// matches reports whether any token satisfies the rule.
//
// Anchored to a token boundary rather than searched for anywhere in the name,
// and the difference is not academic: "nas" appears inside "jonas-laptop", and a
// substring match would confidently file someone's laptop as network storage.
func (r hostnameRule) matches(tokens []string) bool {
	for _, t := range tokens {
		if t == r.needle {
			return true
		}
		if !r.exact && strings.HasPrefix(t, r.needle) {
			return true
		}
	}
	return false
}

// hostnameTokens lower-cases the name a client announced and splits it on every
// separator clients are known to use, so that "Toms-iPhone", "toms_iphone",
// "TOMS IPHONE" and "toms.iphone.local" all yield the same tokens.
func hostnameTokens(h string) []string {
	h = strings.ToLower(strings.TrimSpace(h))
	if h == "" {
		return nil
	}
	fields := strings.FieldsFunc(h, func(r rune) bool {
		switch r {
		case '-', '_', ' ', '.', ':', ',', '\'':
			return true
		}
		return false
	})

	// The unsplit name is a token too. Some clients announce "MacBookPro" with
	// no separator at all, and a rule anchored to a token boundary would
	// otherwise never see it.
	if len(fields) > 1 {
		fields = append(fields, strings.NewReplacer(
			"-", "", "_", "", " ", "", ".", "",
		).Replace(h))
	}
	return fields
}

package clients

import (
	"strings"
	"testing"
)

func TestValidateAcceptsAReasonableConfig(t *testing.T) {
	cfg := Config{Clients: []Client{
		{MAC: "aa:bb:cc:dd:ee:ff", Name: "Study laptop", Category: CategoryLaptop},
		{MAC: "11:22:33:44:55:66", Model: "synology/ds224plus", Notes: "loft"},
		// Nothing but a MAC is legal: it is how a statically-addressed printer
		// gets onto the list at all.
		{MAC: "22:33:44:55:66:77"},
	}}
	if res := Validate(cfg); !res.OK() {
		t.Errorf("unexpected errors: %s", problemStrings(res.Errors))
	}
}

func TestValidateRejectsADuplicateMAC(t *testing.T) {
	cfg := Config{Clients: []Client{
		{MAC: "aa:bb:cc:dd:ee:ff", Name: "one"},
		{MAC: "AA-BB-CC-DD-EE-FF", Name: "two"},
	}}
	cfg.Normalize()

	res := Validate(cfg)
	if res.OK() {
		t.Fatal("Validate accepted two entries for the same hardware")
	}
	if !strings.Contains(problemStrings(res.Errors), "already described") {
		t.Errorf("error should explain the duplicate, got: %s", problemStrings(res.Errors))
	}
}

func TestValidateRejectsABadMAC(t *testing.T) {
	for _, mac := range []string{"", "   ", "nonsense", "aa:bb:cc:dd:ee"} {
		cfg := Config{Clients: []Client{{MAC: mac}}}
		if res := Validate(cfg); res.OK() {
			t.Errorf("Validate accepted MAC %q", mac)
		}
	}
}

func TestValidateRejectsAnUnknownCategory(t *testing.T) {
	cfg := Config{Clients: []Client{{MAC: "aa:bb:cc:dd:ee:ff", Category: "toaster"}}}
	res := Validate(cfg)
	if res.OK() {
		t.Fatal("Validate accepted a category outside the vocabulary")
	}
	// The message has to list the alternatives, or the operator is left guessing.
	if !strings.Contains(problemStrings(res.Errors), "laptop") {
		t.Errorf("error should list valid values, got: %s", problemStrings(res.Errors))
	}
}

// An empty category means "unset, detection may answer" and must stay legal —
// the schema publishes it, so rejecting it here would refuse a document this
// module itself accepts.
func TestValidateAcceptsAnUnsetCategory(t *testing.T) {
	cfg := Config{Clients: []Client{{MAC: "aa:bb:cc:dd:ee:ff", Category: ""}}}
	if res := Validate(cfg); !res.OK() {
		t.Errorf("Validate rejected an unset category: %s", problemStrings(res.Errors))
	}
}

func TestValidateRejectsAnOverlongName(t *testing.T) {
	cfg := Config{Clients: []Client{{
		MAC:  "aa:bb:cc:dd:ee:ff",
		Name: strings.Repeat("a", MaxNameLen+1),
	}}}
	if res := Validate(cfg); res.OK() {
		t.Error("Validate accepted a name past the limit")
	}
}

func TestValidateRejectsControlCharactersInAName(t *testing.T) {
	cfg := Config{Clients: []Client{{MAC: "aa:bb:cc:dd:ee:ff", Name: "study\nlaptop"}}}
	if res := Validate(cfg); res.OK() {
		t.Error("Validate accepted a newline in a name, which breaks every single-line rendering of it")
	}
}

// The model addresses an image asset, so its shape is a safety property rather
// than a style preference: a value containing a path separator or dot-dot would
// be a traversal the asset layer should never have to be suspicious of.
func TestValidateRejectsAnUnsafeModel(t *testing.T) {
	for _, model := range []string{
		"../../etc/passwd",
		"..",
		"synology/../../x",
		"vendor/model/extra",
		"Synology/DS224Plus", // upper case: Normalize folds it, so a raw one is a bug
		"has space",
		"trailing/",
		"/leading",
		"semi;colon",
	} {
		cfg := Config{Clients: []Client{{MAC: "aa:bb:cc:dd:ee:ff", Model: model}}}
		if res := Validate(cfg); res.OK() {
			t.Errorf("Validate accepted model %q", model)
		}
	}
}

func TestValidateAcceptsReasonableModels(t *testing.T) {
	for _, model := range []string{
		"synology/ds224plus",
		"raspberrypi/4b",
		"ds224plus",
		"hp/laserjet-m140we",
		"ubiquiti/u6_lite",
		"apple/macbook-air.m2",
	} {
		cfg := Config{Clients: []Client{{MAC: "aa:bb:cc:dd:ee:ff", Model: model}}}
		if res := Validate(cfg); !res.OK() {
			t.Errorf("Validate rejected model %q: %s", model, problemStrings(res.Errors))
		}
	}
}

func TestValidateAddressesProblemsToTheOffendingField(t *testing.T) {
	cfg := Config{Clients: []Client{
		{MAC: "aa:bb:cc:dd:ee:ff"},
		{MAC: "nonsense"},
	}}
	res := Validate(cfg)
	if res.OK() {
		t.Fatal("expected an error")
	}
	if got := res.Errors[0].Path; got != "clients[1].mac" {
		t.Errorf("Path = %q, want %q so a UI can attach it to the field", got, "clients[1].mac")
	}
}

func problemStrings(ps []Problem) string {
	out := make([]string, 0, len(ps))
	for _, p := range ps {
		out = append(out, p.String())
	}
	return strings.Join(out, "; ")
}

func TestValidateIcons(t *testing.T) {
	for _, icon := range []Icon{"", "apple/laptop", "apple/phone", "os/debian", "os/proxmox", "huawei/laptop",
		"nas/4bay", "nas/8bay", "apple/desktop/mini", "apple/desktop/studio",
		"synology/nas/1bay", "synology/nas/4bay", "synology/nas/12bay",
		"server/tower", "desktop/mini", "intel/desktop",
		"desktop/ikoolcorer1", "desktop/ikoolcorer2", "desktop/ikoolcorer2max",
		"thesvg:proxmox", "thesvg:home-assistant", "thesvg:1password"} {
		cfg := Config{Clients: []Client{{MAC: "aa:bb:cc:dd:ee:ff", Icon: icon}}}
		if res := Validate(cfg); !res.OK() {
			t.Errorf("Validate rejected icon %q: %s", icon, problemStrings(res.Errors))
		}
	}
	for _, icon := range []Icon{
		"thesvg:", "thesvg:../proxmox", "thesvg:Proxmox", "thesvg:-proxmox",
		"thesvg:proxmox?raw=1", Icon("thesvg:" + strings.Repeat("a", 97)),
		"apple",                // a vendor with no kind of thing is what ICONS.md refuses to draw
		"apple/unknown",        // the same, spelled out
		"apple/toaster",        // not a category
		"acme/laptop",          // not a vendor we know
		"os/beos",              // not an OS we have a mark for
		"os/",                  // nothing after the prefix
		"../etc/passwd",        // not a vendor either, and must never reach a path
		"apple/laptop/x",       // laptops have no variants
		"apple/desktop/x",      // not a variant of the iMac's category
		"nas/3bay",             // not a variant anyone drew
		"synology/nas/2bay",    // the two-bay box is the vendor picture itself
		"synology/nas/7bay",    // no such count in the family
		"synology/router/4bay", // routers have no variants
		"desktop/ikoolcorer3",  // no artwork for an unregistered model
		"router/4bay",          // routers have no variants
		"modem/4bay",           // nor modems
		"dell/desktop/mini",    // the brandless mini is every vendor's; Dell's is nobody's
		"server/tower/2u",      // two slashes at most, and a tower has no bays
		"apple/desktop/mini/x", // two slashes at most
	} {
		cfg := Config{Clients: []Client{{MAC: "aa:bb:cc:dd:ee:ff", Icon: icon}}}
		if res := Validate(cfg); res.OK() {
			t.Errorf("Validate accepted icon %q", icon)
		}
	}
}

// Normalize folds case, so a hand-written "Apple/Laptop" is stored as the key
// it means rather than rejected.
func TestNormalizeFoldsIconCase(t *testing.T) {
	c := Config{Clients: []Client{{MAC: "aa:bb:cc:dd:ee:ff", Icon: " OS/Debian "}}}
	c.Normalize()
	if got := c.Clients[0].Icon; got != "os/debian" {
		t.Errorf("Normalize left icon as %q, want %q", got, "os/debian")
	}
}

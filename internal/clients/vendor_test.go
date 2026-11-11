package clients

import (
	"strings"
	"testing"

	"github.com/open-linux-router/open-linux-router/internal/clients/ouidb"
)

func TestVendorKeyFor(t *testing.T) {
	tests := []struct {
		vendor string
		want   VendorKey
	}{
		{"Apple", "apple"},
		{"TP-Link", "tp-link"},
		{"Raspberry Pi", "raspberry-pi"},
		{"Philips Hue", "philips-hue"},

		// Case and stray space are the caller's accidents, not a different
		// vendor.
		{"apple", "apple"},
		{"  TP-Link  ", "tp-link"},

		// The normal outcome. Most of the registry's thirty thousand vendors
		// have no key, and a client made by one of them is not a problem to be
		// solved — it shows its name and falls through to its category.
		{"WNC", ""},
		{"Data Electronic Clients", ""},
		{"", ""},
	}
	for _, tc := range tests {
		if got := VendorKeyFor(tc.vendor); got != tc.want {
			t.Errorf("VendorKeyFor(%q) = %q, want %q", tc.vendor, got, tc.want)
		}
	}
}

// The guarantee that makes a closed list worth keeping: every key names a
// vendor that some real hardware address actually resolves to.
//
// The failure this catches is drift, and it is silent without a test. The
// generator's alias list decides what a vendor is *called*; vendorKeyByName
// joins on that name. Improve an alias — "Raspberry Pi" to "Raspberry Pi
// Foundation", say — and the join breaks, every Pi stops having a key, and the
// only symptom is a picture quietly not appearing on a screen nobody is looking
// at. Here it is a build failure with the vendor named.
func TestEveryVendorKeyIsReachable(t *testing.T) {
	// Names the detector can actually produce: the hand overlay's, plus every
	// name in the shipped registry.
	reachable := map[string]bool{}
	for _, entry := range ouiTable {
		reachable[strings.ToLower(entry.vendor)] = true
	}
	for _, vendor := range ouidb.Vendors() {
		reachable[strings.ToLower(vendor)] = true
	}

	for name, key := range vendorKeyByName {
		if !reachable[name] {
			t.Errorf("vendor key %q is keyed on %q, which no address resolves to; "+
				"the name it joins on has changed", key, name)
		}
	}
}

// Keys are filenames and URL fragments. A key with a capital, a space or a
// slash in it would work on one developer's filesystem and not on the next.
func TestVendorKeysAreSlugs(t *testing.T) {
	for _, key := range VendorKeys() {
		for _, r := range string(key) {
			if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
				continue
			}
			t.Errorf("vendor key %q contains %q; keys are lower-case, digits and hyphens", key, r)
			break
		}
	}
}

// Two names may share a key — a vendor with several spellings is still one
// vendor — but a key must not be spelled two ways, or half the devices go to
// one filename and half to another.
func TestVendorKeysAreDistinctPerName(t *testing.T) {
	seen := map[VendorKey]string{}
	for name, key := range vendorKeyByName {
		if first, dup := seen[key]; dup {
			t.Logf("key %q is shared by %q and %q, which is allowed", key, first, name)
		}
		seen[key] = name
	}
	if len(seen) == 0 {
		t.Fatal("no vendor keys at all")
	}
}

// The end-to-end claim: the addresses from a real LAN come out with the key the
// icon ladder will look artwork up by.
func TestDetectSetsTheVendorKey(t *testing.T) {
	got := Detect("18:f2:2c:d8:b0:e6", "")
	if got.Vendor != "TP-Link" || got.VendorKey != "tp-link" {
		t.Errorf("Detect = vendor %q key %q, want %q and %q",
			got.Vendor, got.VendorKey, "TP-Link", "tp-link")
	}

	// A vendor with no key still has a name. This is the common case, and the
	// UI has to render it without a picture rather than treat it as missing.
	got = Detect("38:b8:00:a5:f3:cf", "")
	if got.Vendor != "WNC" {
		t.Errorf("Vendor = %q, want %q", got.Vendor, "WNC")
	}
	if got.VendorKey != "" {
		t.Errorf("VendorKey = %q, want it empty", got.VendorKey)
	}

	// And a randomised address has neither.
	got = Detect("ce:16:7e:f8:a8:11", "")
	if got.Vendor != "" || got.VendorKey != "" {
		t.Errorf("Detect = vendor %q key %q, want both empty", got.Vendor, got.VendorKey)
	}
}

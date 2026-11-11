package clients

import "testing"

func TestDetectFromHostname(t *testing.T) {
	tests := []struct {
		hostname string
		want     Category
	}{
		{"Toms-iPhone", CategoryPhone},
		{"toms_iphone", CategoryPhone},
		{"TOMS IPHONE", CategoryPhone},
		{"iPhone", CategoryPhone},
		// No separator at all, which is how several clients announce themselves.
		{"MacBookPro", CategoryLaptop},
		{"macbook-air", CategoryLaptop},
		{"iPad-Pro", CategoryTablet},
		{"kindle-paperwhite", CategoryEreader},
		{"DiskStation", CategoryNAS},
		{"office-nas", CategoryNAS},
		{"raspberrypi", CategorySBC},
		{"HP-LaserJet-M140we", CategoryPrinter},
		{"living-room-appletv", CategoryTV},
		{"Apple-TV-Living-Room", CategoryTV},
		{"Chromecast-Bedroom", CategoryTV},
		{"Sonos-Kitchen", CategorySpeaker},
		{"PlayStation-5", CategoryConsole},
		{"front-doorbell", CategoryDoorbell},
		{"unifi-ap-loft", CategoryAccessPoint},
		{"zhimi-airpurifier-ma4_miap1234", CategoryAirPurifier},
		{"yunmi-waterpuri-lx12_mibt4a2c", CategoryWaterPurifier},
		{"deerma-humidifier-jsq", CategoryHumidifier},
		{"xiaomi-aircondition-ma2", CategoryAirCon},
		{"hyd-airer-znlyj_mibt4a2c", CategoryAirer},
		{"viomi-fridge-v3", CategoryFridge},
		{"viomi-washer-v5", CategoryWasher},
		// The provider's box, announcing its own part number, which is how
		// most ONTs arrive on a LAN.
		{"HG8145V5", CategoryModem},
		{"hg8245h", CategoryModem},
		{"ZXHN-F670L", CategoryModem},
		{"FiberHome-AN5506", CategoryModem},
		{"home-gpon", CategoryModem},
		{"modem", CategoryModem},
	}
	for _, tc := range tests {
		got := Detect("aa:bb:cc:dd:ee:ff", tc.hostname)
		if got.Category != tc.want {
			t.Errorf("Detect(hostname=%q).Category = %q, want %q",
				tc.hostname, got.Category, tc.want)
		}
		if got.Reason == "" {
			t.Errorf("Detect(hostname=%q) gave no reason; an unexplained guess is one "+
				"an operator has no basis to accept", tc.hostname)
		}
	}
}

// The bug a naive strings.Contains would have: "nas" sits inside "jonas", and a
// substring match would confidently file someone's laptop as network storage.
func TestDetectDoesNotMatchInsideAWord(t *testing.T) {
	tests := []struct {
		hostname string
		notWant  Category
	}{
		{"jonas-laptop", CategoryNAS},
		{"jonas", CategoryNAS},
		{"thomas", CategoryNAS},
		{"nasa-workstation", CategoryNAS},
		// "ont" and "modem" are the opening of unrelated words, which is why
		// both rules are exact. A hostname that merely begins with one is not
		// evidence of anything, and the provider's box is exactly the client
		// whose name people *do* change.
		{"ontario-laptop", CategoryModem},
		{"modemrouter", CategoryModem},
	}
	for _, tc := range tests {
		got := Detect("aa:bb:cc:dd:ee:ff", tc.hostname)
		if got.Category == tc.notWant {
			t.Errorf("Detect(hostname=%q).Category = %q; %q appears only inside a word",
				tc.hostname, got.Category, "nas")
		}
	}

	// The positive control: "jonas-laptop" should still be read as a laptop.
	if got := Detect("aa:bb:cc:dd:ee:ff", "jonas-laptop"); got.Category != CategoryLaptop {
		t.Errorf("Detect(\"jonas-laptop\").Category = %q, want %q", got.Category, CategoryLaptop)
	}
}

func TestDetectPrefersTheMoreSpecificRule(t *testing.T) {
	// "galaxytab" must beat "galaxy", or every tablet is filed as a phone.
	if got := Detect("aa:bb:cc:dd:ee:ff", "Galaxy-Tab-S8"); got.Category != CategoryTablet {
		t.Errorf("Galaxy-Tab-S8 = %q, want %q", got.Category, CategoryTablet)
	}
	if got := Detect("aa:bb:cc:dd:ee:ff", "Galaxy-S21"); got.Category != CategoryPhone {
		t.Errorf("Galaxy-S21 = %q, want %q", got.Category, CategoryPhone)
	}
}

func TestDetectFromOUI(t *testing.T) {
	// A globally-administered Raspberry Pi prefix: vendor makes one kind of
	// thing, so the category is safe.
	got := Detect("b8:27:eb:11:22:33", "")
	if got.Vendor != "Raspberry Pi" {
		t.Errorf("Vendor = %q, want %q", got.Vendor, "Raspberry Pi")
	}
	if got.Category != CategorySBC {
		t.Errorf("Category = %q, want %q", got.Category, CategorySBC)
	}
}

// Apple makes phones, tablets, laptops, watches and TV boxes. Three octets
// cannot choose between them, and inventing an answer is exactly what
// icon-style-spec.md forbids.
//
// This prefix used to be hand-listed in ouiTable; the vendor now comes from the
// IEEE registry instead, which is the whole point of ouidb — and the answer
// still stops at the name. The registry is not allowed to suggest a category
// however confident it sounds.
func TestDetectFromOUIGivesVendorWithoutGuessingCategory(t *testing.T) {
	got := Detect("a4:83:e7:11:22:33", "")
	if got.Vendor != "Apple" {
		t.Errorf("Vendor = %q, want %q", got.Vendor, "Apple")
	}
	if got.Category != "" {
		t.Errorf("Category = %q, want it unset: the vendor makes many kinds of device",
			got.Category)
	}
}

// The screen this was all for: a LAN with DHCP off, so no client has announced
// a hostname and every row falls back to its hardware address. Before the
// registry the list was six identical hex strings; the three globally-assigned
// addresses on it should now say who built them.
func TestDetectNamesVendorsFromTheRegistry(t *testing.T) {
	tests := []struct{ mac, want string }{
		{"18:f2:2c:d8:b0:e6", "TP-Link"},
		{"38:b8:00:a5:f3:cf", "WNC"},
		{"a0:31:db:7f:13:f8", "Huawei"},
	}
	for _, tc := range tests {
		got := Detect(tc.mac, "")
		if got.Vendor != tc.want {
			t.Errorf("Detect(%q).Vendor = %q, want %q", tc.mac, got.Vendor, tc.want)
		}
		if got.Category != "" {
			t.Errorf("Detect(%q).Category = %q, want it unset: a vendor name is not "+
				"evidence of a kind of device", tc.mac, got.Category)
		}
	}
	// And the other three rows on that screen stay blank, because a randomised
	// address has no vendor to find. Half a real network looks like this now.
	for _, mac := range []string{
		"ce:16:7e:f8:a8:11",
		"d6:34:90:89:67:b9",
		"de:3a:2e:40:89:a1",
	} {
		if got := Detect(mac, ""); got.Vendor != "" {
			t.Errorf("Detect(%q).Vendor = %q, want it empty: the address is "+
				"locally administered", mac, got.Vendor)
		}
	}
}

// ouiTable's surviving job. IEEE knows this prefix as Signify, formerly Philips
// Lighting; the label under a lamp in someone's hall should say Philips Hue.
// The hand-written entry has to win, or those entries are decorative.
func TestOverlayBeatsTheRegistry(t *testing.T) {
	got := Detect("00:17:88:11:22:33", "")
	if got.Vendor != "Philips Hue" {
		t.Errorf("Vendor = %q, want %q: a hand-placed name outranks the registry",
			got.Vendor, "Philips Hue")
	}
	if got.Category != CategoryLight {
		t.Errorf("Category = %q, want %q", got.Category, CategoryLight)
	}
}

func TestHostnameBeatsOUI(t *testing.T) {
	// Espressif's prefix implies a sensor, but the client says it is a doorbell.
	got := Detect("24:0a:c4:11:22:33", "front-doorbell")
	if got.Category != CategoryDoorbell {
		t.Errorf("Category = %q, want %q: the hostname is the stronger signal",
			got.Category, CategoryDoorbell)
	}
	// The vendor survives, because it is a separate fact.
	if got.Vendor != "Espressif" {
		t.Errorf("Vendor = %q, want it preserved", got.Vendor)
	}
}

func TestDetectGivesNothingWhenItKnowsNothing(t *testing.T) {
	got := Detect("aa:bb:cc:dd:ee:ff", "some-random-box")
	if got.Category != "" {
		t.Errorf("Category = %q, want it unset rather than a guess", got.Category)
	}
	if got.Vendor != "" {
		t.Errorf("Vendor = %q, want it empty for an unlisted, locally-administered prefix", got.Vendor)
	}
}

func TestDetectToleratesAnUnparseableMAC(t *testing.T) {
	// Should not panic, and should still read the hostname.
	got := Detect("nonsense", "Toms-iPhone")
	if got.Category != CategoryPhone {
		t.Errorf("Category = %q, want %q", got.Category, CategoryPhone)
	}
}

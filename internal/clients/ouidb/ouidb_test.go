package ouidb

import (
	"strings"
	"testing"
)

// The table has to actually be in the binary. A build that embedded an empty
// or truncated oui.gz would fail nothing else here — every lookup would just
// report "no vendor", which is a legal answer — so this is the test that
// notices.
func TestTableIsPopulated(t *testing.T) {
	if n := Len(); n < 40_000 {
		t.Fatalf("table holds %d prefixes; the three IEEE registries are around "+
			"53,000 between them, so oui.gz is missing or truncated", n)
	}
}

func TestVendor(t *testing.T) {
	tests := []struct {
		name string
		mac  string
		want string
	}{
		// The three globally-assigned addresses from a real LAN with DHCP off,
		// which is the case that prompted the whole table: without it every one
		// of these rows is a bare hex string.
		{"MA-L, aliased", "18:f2:2c:d8:b0:e6", "TP-Link"},
		{"MA-L, registry name kept", "38:b8:00:a5:f3:cf", "WNC"},
		{"MA-L, shouting name fixed", "a0:31:db:7f:13:f8", "Huawei"},

		// Case and separator are the caller's business, not ours.
		{"upper case", "18:F2:2C:D8:B0:E6", "TP-Link"},
		{"hyphenated", "18-f2-2c-d8-b0-e6", "TP-Link"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := Vendor(tc.mac)
			if !ok {
				t.Fatalf("Vendor(%q) found nothing, want %q", tc.mac, tc.want)
			}
			if got != tc.want {
				t.Errorf("Vendor(%q) = %q, want %q", tc.mac, got, tc.want)
			}
		})
	}
}

// The bug a three-octet lookup has, and the reason core.OUIPrefixes returns
// three prefixes. 8c:1f:64 is an MA-L that IEEE subdivided and sold on in
// 36-bit blocks, so it is shared by hundreds of unrelated companies. A lookup
// that stopped at three octets would answer all of them identically.
func TestVendorPrefersTheLongestBlock(t *testing.T) {
	// Two MA-S assignments inside the same MA-L.
	first, ok := Vendor("8c:1f:64:af:a0:01")
	if !ok {
		t.Fatal("Vendor found nothing for a registered MA-S block")
	}
	second, ok := Vendor("8c:1f:64:9b:90:01")
	if !ok {
		t.Fatal("Vendor found nothing for a registered MA-S block")
	}
	if first == second {
		t.Errorf("both MA-S blocks in 8c:1f:64 resolved to %q; the lookup is "+
			"reading three octets and naming whichever company was listed first", first)
	}
}

// IEEE keeps the parent of a subdivided block registered to itself. Reporting
// that would put "IEEE Registration Authority" under a device, which teaches an
// operator that the vendor column is not worth reading.
func TestVendorNeverNamesTheRegistry(t *testing.T) {
	// An MA-S block inside 8c:1f:64 that nobody has registered.
	if got, ok := Vendor("8c:1f:64:ff:f0:01"); ok {
		if strings.Contains(strings.ToLower(got), "ieee") {
			t.Errorf("Vendor = %q; the subdivided parent block should have been dropped", got)
		}
	}
}

// A randomised MAC has no vendor to find, and this is the case that keeps
// growing: every current phone and laptop OS randomises per network by default.
func TestVendorRefusesLocallyAdministered(t *testing.T) {
	// The three locally-administered addresses from the same real LAN.
	for _, mac := range []string{
		"ce:16:7e:f8:a8:11",
		"d6:34:90:89:67:b9",
		"de:3a:2e:40:89:a1",
	} {
		if got, ok := Vendor(mac); ok {
			t.Errorf("Vendor(%q) = %q; a locally-administered address was invented "+
				"by the client and has no vendor", mac, got)
		}
	}
}

func TestVendorRejectsNonsense(t *testing.T) {
	for _, mac := range []string{"", "  ", "not-a-mac", "aa:bb:cc:dd:ee"} {
		if got, ok := Vendor(mac); ok {
			t.Errorf("Vendor(%q) = %q, want no answer", mac, got)
		}
	}
}

// Names go under a device on a card a couple of hundred pixels wide. The
// generator strips company forms so they fit; this is the check that it ran.
func TestNamesAreNotLegalNames(t *testing.T) {
	m := table()
	var offenders []string
	for prefix, vendor := range m {
		lower := strings.ToLower(vendor)
		for _, tail := range []string{
			", inc.", ", inc", " inc.", " co.,ltd", " co., ltd", " co.,ltd.",
			" gmbh", " corporation", " limited",
		} {
			if strings.HasSuffix(lower, tail) {
				offenders = append(offenders, prefix+" = "+vendor)
			}
		}
		if len(offenders) > 5 {
			break
		}
	}
	if len(offenders) > 0 {
		t.Errorf("company forms survived cleanup: %s", strings.Join(offenders, "; "))
	}
}

package clients

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestARPParsesTheTable(t *testing.T) {
	src := ARP{Path: filepath.Join("testdata", "arp")}

	sightings, problems, err := src.Presence(context.Background())
	if err != nil {
		t.Fatalf("Presence failed: %v", err)
	}

	// Three usable rows: two complete, one complete-but-stale. The
	// all-zero row is an unresolved entry and names no device; the last two
	// rows are malformed.
	if len(sightings) != 3 {
		t.Fatalf("got %d sightings, want 3:\n%+v", len(sightings), sightings)
	}

	first := sightings[0]
	if first.MAC != "aa:bb:cc:dd:ee:ff" {
		t.Errorf("MAC = %q", first.MAC)
	}
	if first.IP != "192.168.1.100" {
		t.Errorf("IP = %q", first.IP)
	}
	if first.Source != SourceARP {
		t.Errorf("Source = %q, want %q", first.Source, SourceARP)
	}
	if !first.Active {
		t.Error("a complete entry (flags 0x2) should be active")
	}
	if first.Hostname != "" {
		t.Errorf("Hostname = %q; ARP carries none", first.Hostname)
	}

	// Flags 0x0 means the kernel has an address but does not consider the
	// neighbour reachable. Reported, because "this was here" is worth showing,
	// but not as presence.
	stale := sightings[2]
	if stale.MAC != "22:33:44:55:66:77" {
		t.Fatalf("third sighting = %q, want the stale entry", stale.MAC)
	}
	if stale.Active {
		t.Error("an incomplete entry (flags 0x0) should not be active")
	}

	// Both malformed rows are reported rather than silently dropped: a table
	// producing garbage is a fact about the box worth surfacing.
	if len(problems) != 2 {
		t.Errorf("got %d problems, want 2 (bad flags, short line):\n%s",
			len(problems), problemStrings(problems))
	}
}

// An unresolved entry is a normal transient state, not a fault, so it must not
// generate a problem — otherwise a healthy box shows warnings constantly.
func TestARPSkipsUnresolvedEntriesQuietly(t *testing.T) {
	src := ARP{Path: filepath.Join("testdata", "arp")}
	sightings, _, err := src.Presence(context.Background())
	if err != nil {
		t.Fatalf("Presence failed: %v", err)
	}
	for _, s := range sightings {
		if s.MAC == incompleteMAC {
			t.Errorf("the all-zero placeholder MAC reached the caller")
		}
	}
}

// Every non-Linux developer box. The list must still render, and it must say
// what it cannot see — a lease-only list that looked complete would be the one
// genuinely misleading outcome.
func TestARPReportsAMissingTableWithoutFailing(t *testing.T) {
	src := ARP{Path: filepath.Join("testdata", "does-not-exist")}

	sightings, problems, err := src.Presence(context.Background())
	if err != nil {
		t.Fatalf("a missing table should not be an error, got: %v", err)
	}
	if len(sightings) != 0 {
		t.Errorf("got %d sightings from a missing file", len(sightings))
	}
	if len(problems) != 1 {
		t.Fatalf("got %d problems, want 1", len(problems))
	}
	// The message has to state the consequence, not just the cause.
	if !strings.Contains(problems[0].Message, "added by hand") {
		t.Errorf("problem should say what the operator will notice, got: %q",
			problems[0].Message)
	}
}

func TestARPName(t *testing.T) {
	if got := (ARP{}).Name(); got != SourceARP {
		t.Errorf("Name = %q, want %q", got, SourceARP)
	}
}

func TestARPDefaultsToTheKernelTable(t *testing.T) {
	if got := (ARP{}).path(); got != ARPPath {
		t.Errorf("path = %q, want %q", got, ARPPath)
	}
}

// The interface is the whole reason this source can place a statically
// addressed device, so it has to survive parsing.
func TestARPCarriesTheInterface(t *testing.T) {
	sightings, _, err := (ARP{Path: "testdata/arp"}).Presence(context.Background())
	if err != nil {
		t.Fatalf("Presence: %v", err)
	}
	if len(sightings) == 0 {
		t.Fatal("no sightings")
	}
	for _, s := range sightings {
		if s.Interface == "" {
			t.Errorf("%s carries no interface", s.MAC)
		}
	}
}

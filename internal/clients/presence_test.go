package clients

import (
	"net/netip"
	"testing"
)

func TestOwnersPrefersTheNeighbourTableOverALease(t *testing.T) {
	addr := netip.MustParseAddr("192.168.1.20")
	owners := Owners([]Sighting{
		// A lease that has not expired, taken by the client that used to hold
		// the address.
		{MAC: "aa:bb:cc:dd:ee:01", IP: addr.String(), Source: SourceDHCPLease, Active: true},
		// The neighbour table, which is what the kernel currently believes.
		{MAC: "AA:BB:CC:DD:EE:02", IP: addr.String(), Source: SourceND, Active: false},
	})
	if owners[addr] != "aa:bb:cc:dd:ee:02" {
		t.Fatalf("a lease outranked the neighbour table: %v", owners)
	}
}

func TestOwnersKeepsOneClientAcrossItsAddresses(t *testing.T) {
	v4 := netip.MustParseAddr("192.168.1.20")
	v6 := netip.MustParseAddr("2001:db8::20")
	owners := Owners([]Sighting{
		{MAC: "aa:bb:cc:dd:ee:01", IP: v4.String(), Source: SourceARP, Active: true},
		{MAC: "aa:bb:cc:dd:ee:01", IP: v6.String(), Source: SourceND, Active: true},
		{MAC: "not-a-mac", IP: "192.168.1.21", Source: SourceARP, Active: true},
	})
	if len(owners) != 2 || owners[v4] != "aa:bb:cc:dd:ee:01" || owners[v6] != "aa:bb:cc:dd:ee:01" {
		t.Fatalf("addresses were not joined to one client: %v", owners)
	}
}

//go:build !linux

package clients

import "context"

// ND has no neighbour table to read off Linux, and says so where the developer
// can see it rather than reporting that no IPv6 client exists.
type ND struct{}

// Name identifies the source in problem messages.
func (ND) Name() Source { return SourceND }

// Presence reports the gap as a problem, the way ARP does for a box with no
// /proc/net/arp: an empty list and no explanation would read as "no IPv6".
func (ND) Presence(context.Context) ([]Sighting, []Problem, error) {
	return nil, []Problem{{
		Path:    string(SourceND),
		Message: "the IPv6 neighbour table is only readable on Linux, so clients' IPv6 addresses will not appear",
	}}, nil
}

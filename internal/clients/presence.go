package clients

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// The presence half of a client (design.md §4.4): observed, read through the
// thing that observes it, never stored as truth.
//
// Sources are declared here, by the consumer, for the reasons LinkView gives in
// internal/dhcp/link.go: it states exactly the facts this module needs instead
// of exposing all of another module's surface, and it lets the module be built
// and tested before those sources exist. It also keeps `clients` from importing
// `dhcp` — the lease adapter is wired in internal/daemon, so the dependency is one
// interface pointing the way the DAG (§4.1) says it should.

// Source names where a sighting came from. It reaches the UI, because "this
// device is here" and "this client took a lease 40 seconds ago" are different
// claims and only one of them is evidence the DHCP server is working.
type Source string

const (
	// SourceDHCPLease is the lease database: authoritative about DHCP, blind to
	// anything statically addressed.
	SourceDHCPLease Source = "dhcp-lease"

	// SourceARP is the kernel neighbour table: sees the statically-addressed
	// printer that never speaks DHCP, which is the case §10 decision 7 was
	// worried about.
	SourceARP Source = "arp"
)

// Sighting is one source's observation of one hardware address.
type Sighting struct {
	MAC      string
	IP       string
	Hostname string
	Source   Source

	// Interface is the kernel interface the source saw the client on, empty
	// when the source cannot say. Only ARP can today — a lease file records no
	// interface — which is why a client the neighbour table has aged out has to
	// be placed by its address instead. See NetworkView in list.go.
	Interface string

	// Active means the source considers this current: an unexpired lease, or a
	// reachable neighbour entry. A stale entry is still reported — it is how
	// "this was here yesterday" gets on screen — but it is not presence.
	Active bool

	// Expires is nil for a source that has no notion of expiry, and for a lease
	// that never expires.
	Expires *time.Time
}

// PresenceSource is anything that can say which hardware addresses it has seen.
//
// Problems are returned alongside the data rather than as an error, because a
// partial answer is the normal case and is worth having: an unreadable ARP
// table must not take the lease-derived half of the list down with it. Only a
// failure that makes the whole source meaningless is an error.
type PresenceSource interface {
	// Name identifies the source in problem messages.
	Name() Source

	// Presence returns what the source can see right now.
	Presence(ctx context.Context) ([]Sighting, []Problem, error)
}

// Presence is every source's observations of one device, merged.
type Presence struct {
	MAC string

	// IPs is every address this MAC was seen at, deduplicated. Plural because a
	// dual-stack client legitimately has several, and because a client that just
	// moved between pools briefly has two — collapsing that to one field would
	// mean picking a winner arbitrarily and hiding the interesting case.
	IPs []string

	// Interfaces is every interface a source saw this client on, deduplicated.
	// Plural for the reason IPs is plural: a client that moved between networks
	// legitimately has two, and collapsing that to one would mean picking a
	// winner arbitrarily. Empty when no source could say, which is the normal
	// case for a client that is away — its lease outlives its neighbour entry.
	Interfaces []string

	// Hostname is what the client called itself, when any source heard it.
	Hostname string

	// Sources is which sources saw it, in a stable order.
	Sources []Source

	// Active is true when any source considers it current.
	Active bool

	// Expires is the lease expiry, when a lease was seen.
	Expires *time.Time
}

// Gather reads every source, tolerating individual failures.
//
// A source that errors is reported as a problem and contributes nothing. This
// is the same shape getStatus uses in internal/dhcp/http.go for the systemd
// query: each half of an answer is reported independently and neither can
// suppress the other. On a developer box with no /proc/net/arp that is the
// difference between a working screen with a note on it and a 500.
func Gather(ctx context.Context, sources ...PresenceSource) ([]Sighting, []Problem) {
	var (
		all      []Sighting
		problems []Problem
	)
	for _, src := range sources {
		if src == nil {
			continue
		}
		seen, probs, err := src.Presence(ctx)
		problems = append(problems, probs...)
		if err != nil {
			problems = append(problems, Problem{
				Path:    string(src.Name()),
				Message: fmt.Sprintf("could not read %s: %v", src.Name(), err),
			})
			continue
		}
		all = append(all, seen...)
	}
	return all, problems
}

// Merge folds sightings into one Presence per device, keyed by canonical MAC.
//
// Sightings whose MAC will not parse are dropped and reported rather than
// silently discarded: a lease file or ARP table producing garbage is a fact
// about the box worth surfacing, and the alternative is a list that quietly
// shrinks.
func Merge(sightings []Sighting) (map[string]Presence, []Problem) {
	var problems []Problem
	out := map[string]Presence{}

	for _, s := range sightings {
		mac, err := core.NormalizeMAC(s.MAC)
		if err != nil {
			problems = append(problems, Problem{
				Path:    string(s.Source),
				Message: fmt.Sprintf("ignoring a sighting with an unreadable MAC: %v", err),
			})
			continue
		}

		p, ok := out[mac]
		if !ok {
			p = Presence{MAC: mac}
		}

		if ip := strings.TrimSpace(s.IP); ip != "" && !slices.Contains(p.IPs, ip) {
			p.IPs = append(p.IPs, ip)
		}
		if iface := strings.TrimSpace(s.Interface); iface != "" && !slices.Contains(p.Interfaces, iface) {
			p.Interfaces = append(p.Interfaces, iface)
		}

		// A DHCP lease heard the client state its own name; ARP never carries
		// one. So a lease hostname replaces whatever else is there, and any
		// other source only fills a gap.
		if h := strings.TrimSpace(s.Hostname); h != "" {
			if p.Hostname == "" || s.Source == SourceDHCPLease {
				p.Hostname = h
			}
		}

		if !slices.Contains(p.Sources, s.Source) {
			p.Sources = append(p.Sources, s.Source)
		}

		p.Active = p.Active || s.Active

		if s.Expires != nil && s.Source == SourceDHCPLease {
			p.Expires = s.Expires
		}

		out[mac] = p
	}

	// Deterministic order inside each device, so two reads of an unchanged
	// network produce byte-identical JSON and a UI does not repaint for nothing.
	for mac, p := range out {
		slices.Sort(p.IPs)
		slices.Sort(p.Interfaces)
		slices.Sort(p.Sources)
		out[mac] = p
	}

	return out, problems
}

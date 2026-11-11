package daemon

import (
	"context"
	"fmt"
	"net/netip"
	"strings"

	"github.com/open-linux-router/open-linux-router/internal/clients"
	"github.com/open-linux-router/open-linux-router/internal/core"
	"github.com/open-linux-router/open-linux-router/internal/dns"
	"github.com/open-linux-router/open-linux-router/internal/ingress"
)

// Adapters joining the ingress module to the two it reads.
//
// Here rather than inside any of the three, for the reason the dhcp↔clients
// adapters are here: `ingress` declares the interfaces it needs (DNSView,
// ClientView) and imports neither `dns` nor `clients`, so design.md §4.1's
// arrows keep pointing one way and the introduction happens at the one place
// that already knows the whole module list.

// ingressDNS is ingress's window onto the dns module.
//
// One fact, read per request through dns's own Load: the suffix the resolver
// answers authoritatively. It is read only so that a published domain falling
// inside it is refused rather than silently unresolved (internal/ingress
// validate.go), and reading it live rather than caching it keeps a `olr dns set`
// from leaving the refusal pointing at a stale zone.
type ingressDNS struct {
	applier dns.Applier
}

func (d ingressDNS) LocalDomain() string {
	cfg, err := d.applier.Load()
	if err != nil {
		// An unreadable dns config reads as "no local domain", which the
		// ingress validator treats as "nothing to collide with". Refusing
		// instead would make a broken dns config block every ingress edit; the
		// collision it would guard against is reported by `olr dns` itself.
		return ""
	}
	return cfg.LocalDomainOrDefault()
}

// ingressClients is ingress's window onto the clients module.
//
// MACs identify clients across renames. Names remain accepted for legacy
// configurations and CLI input.
type ingressClients struct {
	applier clients.Applier
}

// Upgrade references against the old inventory before a rename or removal.
// Both changes land in one store write, without reloading the proxy.
func preserveIngressClients(doc *core.Document, view ingress.ClientView) error {
	if _, ok := doc.Raw(ingress.ModuleName); !ok {
		return nil
	}
	cfg, err := ingress.FromDocument(*doc)
	if err != nil {
		return err
	}
	cfg.ResolveClientReferences(view)
	data, err := ingress.MarshalConfig(cfg)
	if err != nil {
		return err
	}
	doc.Set(ingress.ModuleName, data)
	return nil
}

func (d ingressClients) Client(name string) (ingress.ClientInfo, error) {
	resolved, _, err := d.applier.List(context.Background())
	if err != nil {
		return ingress.ClientInfo{}, err
	}

	want := strings.ToLower(strings.TrimSpace(name))
	if mac, err := core.NormalizeMAC(want); err == nil {
		for _, r := range resolved {
			if r.MAC == mac {
				return clientInfo(r), nil
			}
		}
		return ingress.ClientInfo{}, fmt.Errorf("%q: %w", name, ingress.ErrNoSuchClient)
	}
	var match *ingress.ClientInfo
	for _, r := range resolved {
		if !strings.EqualFold(r.DisplayName(), want) {
			continue
		}
		if match != nil {
			return ingress.ClientInfo{}, fmt.Errorf("client name %q is ambiguous; use its MAC address", name)
		}
		info := clientInfo(r)
		match = &info
	}
	if match != nil {
		return *match, nil
	}
	return ingress.ClientInfo{}, fmt.Errorf("%q: %w", name, ingress.ErrNoSuchClient)
}

// clientInfo picks the one address a published service should point at.
//
// The fixed address wins outright, and nothing else is even considered when one
// exists. That is not a preference — it is the whole of docs/ingress.md §1.2:
// a published service needs an address that will still be this client's
// tomorrow, and only a reservation promises that. A client with no reservation
// still reports whatever it was last seen at, because the validator's refusal
// reads far better when it can say what the client *is* rather than only that
// it has no fixed address.
func clientInfo(r clients.Resolved) ingress.ClientInfo {
	info := ingress.ClientInfo{MAC: r.MAC, Name: r.DisplayName()}

	if r.FixedIP != "" {
		if addr, err := netip.ParseAddr(r.FixedIP); err == nil {
			info.Addr, info.Fixed = addr, true
			return info
		}
	}

	if r.Presence == nil {
		return info
	}
	// The first address that parses, preferring IPv4: a published upstream is
	// dialled by the proxy, and a link-local IPv6 address — which is what a
	// neighbour table most often offers — is not something to hand a dialler as
	// a stable target.
	var v6 netip.Addr
	for _, ip := range r.Presence.IPs {
		addr, err := netip.ParseAddr(ip)
		if err != nil {
			continue
		}
		if addr.Is4() {
			info.Addr = addr
			return info
		}
		if !v6.IsValid() && !addr.IsLinkLocalUnicast() {
			v6 = addr
		}
	}
	info.Addr = v6
	return info
}

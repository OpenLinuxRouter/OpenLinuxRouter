package daemon

import (
	"context"
	"log/slog"
	"net/netip"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/core"
	"github.com/open-linux-router/open-linux-router/internal/dial"
	"github.com/open-linux-router/open-linux-router/internal/firewall"
	"github.com/open-linux-router/open-linux-router/internal/ingress"
	"github.com/open-linux-router/open-linux-router/internal/iptv"
	"github.com/open-linux-router/open-linux-router/internal/link"
	"github.com/open-linux-router/open-linux-router/internal/remote"
)

// The adapter joining `firewall` to the modules it is built from.
//
// Here for the reason links.go gives about the others: the firewall declares
// what it needs as firewall.Boundary and imports none of the modules that
// answer, so design.md §4.1's arrows keep pointing one way.

// firewallBoundary is the firewall's window onto link, remote and ingress.
type firewallBoundary struct {
	facts link.Facts
	store *core.Store
}

// Inside implements firewall.Boundary: every network member, and the dial-in
// tunnel. A dialled-in phone is meant to be on the network — that is the
// whole of what remote access promises — so its interface is trusted the
// same way the LAN's is.
func (b firewallBoundary) Inside() ([]string, error) {
	networks, err := b.facts.Networks()
	if err != nil {
		return nil, err
	}
	var out []string
	for _, n := range networks {
		out = append(out, n.Members...)
	}

	doc, err := b.store.Load()
	if err != nil {
		return nil, err
	}
	rc, err := remote.FromDocument(doc)
	if err != nil {
		return nil, err
	}
	if rc.WireGuard.Enabled {
		out = append(out, rc.WireGuard.InterfaceOrDefault())
	}
	return out, nil
}

// Openings implements firewall.Boundary: the ports olr itself serves to the
// outside. Port forwards are not here — the forward chain lets through what a
// forward translated, so they need no port of their own.
func (b firewallBoundary) Openings() ([]firewall.Opening, error) {
	doc, err := b.store.Load()
	if err != nil {
		return nil, err
	}
	var out []firewall.Opening

	rc, err := remote.FromDocument(doc)
	if err != nil {
		return nil, err
	}
	if rc.WireGuard.Enabled {
		out = append(out, firewall.Opening{For: "remote access (WireGuard)",
			Protocol: firewall.UDP, Port: rc.WireGuard.PortOrDefault()})
	}
	// The proxy listens on every interface and has no other shape, so switching
	// it on is what opens the port. Nothing here is conditional on the tunnel:
	// the two ways in are independent.
	if s := rc.Socks; s.Enabled {
		out = append(out, firewall.Opening{For: "remote access (SOCKS5)",
			Protocol: firewall.TCP, Port: s.PortOrDefault()})
	}

	ic, err := ingress.FromDocument(doc)
	if err != nil {
		return nil, err
	}
	// 80 for the redirect, 443 for everything, 443/udp for HTTP/3 — but only
	// when something is actually exposed.
	//
	// The private site still answers on 443 for this network, and inside
	// traffic is accepted by the Inside rule above, so leaving the port shut at
	// the boundary costs nothing. What it buys is the one property this feature
	// is supposed to have: a box where nothing is exposed is indistinguishable
	// from outside from a box where ingress is switched off — even if a port
	// forward in front of it is still there.
	if ic.Enabled && ic.HasExposed() {
		out = append(out,
			firewall.Opening{For: "ingress", Protocol: firewall.TCP, Port: 80},
			firewall.Opening{For: "ingress", Protocol: firewall.TCP, Port: 443},
			firewall.Opening{For: "ingress", Protocol: firewall.UDP, Port: 443},
		)
	}

	// The IPv6 tunnel arrives as protocol 41 from the broker. Conntrack lets
	// replies in for a while after this box last sent, which is why a tunnel
	// appears to work behind the firewall — until it has been quiet for ten
	// minutes and the first inbound IPv6 connection is dropped.
	dc, err := dial.FromDocument(doc)
	if err != nil {
		return nil, err
	}
	if tunnel := dc.ActiveTunnel(); tunnel != nil {
		out = append(out, firewall.Opening{For: "IPv6 tunnel",
			Protocol: firewall.SixInFour, From: tunnel.Server})
	}
	return out, nil
}

func (b firewallBoundary) IPTV() (firewall.IPTV, error) {
	doc, err := b.store.Load()
	if err != nil {
		return firewall.IPTV{}, err
	}
	cfg, err := iptv.FromDocument(doc)
	if err != nil {
		return firewall.IPTV{}, err
	}
	resolved, err := iptv.Resolve(cfg, iptvLinks{facts: b.facts})
	if err != nil {
		return firewall.IPTV{}, err
	}
	return firewall.IPTV{Upstream: resolved.Upstream, Downstream: resolved.Downstream}, nil
}

// InterfaceOf implements firewall.Boundary.
func (b firewallBoundary) InterfaceOf(addr netip.Addr) (string, bool) {
	all, err := b.facts.Interfaces()
	if err != nil {
		return "", false
	}
	for _, info := range all {
		for _, p := range info.Prefixes {
			if p.Addr().Unmap() == addr {
				return info.Name, true
			}
		}
	}
	return "", false
}

// firewallFollows lists the modules whose applies change what the firewall is
// built from: the networks (inside), and the two that serve ports to the
// outside. Port forwards are absent on purpose — see Openings.
var firewallFollows = map[string]bool{
	dial.ModuleName:    true,
	link.ModuleName:    true,
	remote.ModuleName:  true,
	ingress.ModuleName: true,
}

// followFirewall re-applies the firewall whenever one of those modules
// announces a change, so a WireGuard port that moves is let in the moment it
// does, and a removed network stops being trusted.
//
// Driven by the event bus rather than by each module's Dependents hook,
// because `remote` has none and every one of them already announces. The
// re-apply takes the global lock like any other apply; events are published
// after an apply releases it, so this never waits on the change that caused it.
func followFirewall(ctx context.Context, a firewall.Applier, srv *core.Server, logger *slog.Logger) {
	events, cancel := srv.Events().Subscribe()
	defer cancel()
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-events:
			if !ok {
				return
			}
			if ev.Type != core.EventApplied || !firewallFollows[ev.Module] {
				continue
			}
			reapplyFirewall(ctx, a, srv, logger, ev.Module)
		}
	}
}

// reapplyFirewall programs stored intent again, under the apply lock.
//
// Silent when the firewall is off or nothing changed, which is nearly always.
func reapplyFirewall(ctx context.Context, a firewall.Applier, srv *core.Server, logger *slog.Logger, cause string) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	var (
		plan firewall.Plan
		err  error
	)
	if lockErr := srv.ApplyLock().Do(ctx, func() error {
		if cause != "startup" {
			c, loadErr := a.Load()
			if loadErr != nil {
				err = loadErr
				return nil
			}
			if len(c.Ruleset) > 0 {
				plan.Empty = true
				return nil
			}
		}
		plan, _, err = a.Reapply(ctx)
		return nil
	}); lockErr != nil {
		err = lockErr
	}
	switch {
	case err != nil:
		logger.Error("firewall could not be applied", "after", cause, "error", err)
	case !plan.Empty:
		logger.Info("firewall applied", "after", cause, "changes", len(plan.Changes))
		srv.Events().Publish(core.Event{Type: core.EventApplied, Module: firewall.ModuleName})
	}
}

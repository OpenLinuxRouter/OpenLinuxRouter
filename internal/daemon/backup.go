package daemon

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"slices"

	"github.com/open-linux-router/open-linux-router/internal/clients"
	"github.com/open-linux-router/open-linux-router/internal/core"
	"github.com/open-linux-router/open-linux-router/internal/devices"
	"github.com/open-linux-router/open-linux-router/internal/dhcp"
	"github.com/open-linux-router/open-linux-router/internal/dial"
	"github.com/open-linux-router/open-linux-router/internal/dns"
	"github.com/open-linux-router/open-linux-router/internal/firewall"
	"github.com/open-linux-router/open-linux-router/internal/gateway"
	"github.com/open-linux-router/open-linux-router/internal/gateway/nat"
	"github.com/open-linux-router/open-linux-router/internal/ingress"
	"github.com/open-linux-router/open-linux-router/internal/iptv"
	"github.com/open-linux-router/open-linux-router/internal/link"
	"github.com/open-linux-router/open-linux-router/internal/qos"
	"github.com/open-linux-router/open-linux-router/internal/remote"
	"github.com/open-linux-router/open-linux-router/internal/socksout"
	"github.com/open-linux-router/open-linux-router/internal/system"
)

// The complete file shape, for REST/MCP schema projection. Runtime state and
// generated backend files deliberately remain outside this document.
type configurationSchema struct {
	Schema   string           `json:"$schema"`
	System   *system.Config   `json:"system,omitempty"`
	Link     *link.Config     `json:"link,omitempty"`
	Dial     *dial.Config     `json:"dial,omitempty"`
	DHCP     *dhcp.Config     `json:"dhcp,omitempty"`
	DNS      *dns.Config      `json:"dns,omitempty"`
	Clients  *clients.Config  `json:"clients,omitempty"`
	Devices  *devices.Config  `json:"devices,omitempty"`
	Gateway  *gateway.Config  `json:"gateway,omitempty"`
	IPTV     *iptv.Config     `json:"iptv,omitempty"`
	SocksOut *socksout.Config `json:"socksout,omitempty"`
	QoS      *qos.Config      `json:"qos,omitempty"`
	Remote   *remote.Config   `json:"remote,omitempty"`
	Ingress  *ingress.Config  `json:"ingress,omitempty"`
	Firewall *firewall.Config `json:"firewall,omitempty"`
}

func configurationStore(path string) *core.Store {
	return core.NewStore(path, system.ModuleName, link.ModuleName, dial.ModuleName,
		dhcp.ModuleName, dns.ModuleName, clients.ModuleName, devices.ModuleName, gateway.ModuleName,
		iptv.ModuleName, socksout.ModuleName, qos.ModuleName, remote.ModuleName,
		ingress.ModuleName, firewall.ModuleName)
}

// Validate against the proposed document, never the currently installed LANs,
// reservations or device inventory. A private scratch store lets the existing
// read-through adapters and validators share that one candidate consistently.
func validateBackup(ctx context.Context, doc core.Document, source link.Source) error {
	var typed configurationSchema
	if err := doc.DecodeStrict(&typed); err != nil {
		return err
	}
	dir, err := os.MkdirTemp("", "olr-restore-validation-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	store := configurationStore(filepath.Join(dir, "olr.json"))
	if err := store.Save(doc); err != nil {
		return err
	}
	if source == nil {
		source = link.Kernel
	}
	observed, err := source()
	if err != nil {
		return err
	}
	observed = slices.Clone(observed)
	lc, err := link.FromDocument(doc)
	if err != nil {
		return err
	}
	if err := link.Validate(lc, observed).Err(); err != nil {
		return err
	}
	for _, name := range lc.Adopted {
		if !slices.ContainsFunc(observed, func(i link.Interface) bool { return i.Name == name }) {
			return fmt.Errorf("interface %q is not present on this machine; edit interface names in the backup before restoring", name)
		}
	}
	dc, err := dial.FromDocument(doc)
	if err != nil {
		return err
	}
	// Validators must see addresses the proposal will install, rather than
	// rejecting a valid renumbering because its addresses are not live yet.
	for i := range observed {
		observed[i].Prefixes = append([]netip.Prefix(nil), observed[i].Prefixes...)
		for _, want := range link.DesiredFor(lc, netip.Prefix{}) {
			if observed[i].Name == want.Interface {
				observed[i].Prefixes = append(observed[i].Prefixes, want.Addrs...)
				observed[i].Prefixes = append(observed[i].Prefixes, want.Addrs6...)
			}
		}
		if want := dial.DesiredFor(dc); observed[i].Name == want.Interface && want.Address.IsValid() {
			observed[i].Prefixes = append(observed[i].Prefixes, want.Address)
		}
	}
	facts := link.Facts{Store: store, Source: func() ([]link.Interface, error) { return observed, nil }}
	dhcpApplier := dhcp.Applier{Store: store, Networks: dhcpNetworkView{facts: facts}}
	dnsApplier := dns.Applier{Store: store}
	deviceApplier := clients.Applier{Store: store, Fixed: dhcpFixedAddresses{applier: dhcpApplier}}
	checks := []func() error{
		func() error {
			c, e := system.FromDocument(doc)
			if e != nil {
				return e
			}
			if c.Access != nil && c.Access.Password != nil && !c.Access.Password.ValidHash() {
				return errors.New("system.access.password contains an invalid password hash")
			}
			return nil
		},
		func() error { return dial.Validate(dc, dialLinkView{facts: facts}).Err() },
		func() error {
			c, e := dhcp.FromDocument(doc)
			if e != nil {
				return e
			}
			return dhcp.Validate(c, dhcpNetworkView{facts: facts}).Err()
		},
		func() error {
			c, e := dns.FromDocument(doc)
			if e != nil {
				return e
			}
			return dns.Validate(c, dnsLinkView{facts: facts}, dnsReservations{applier: dhcpApplier}).Err()
		},
		func() error {
			c, e := clients.FromDocument(doc)
			if e != nil {
				return e
			}
			return clients.Validate(c).Err()
		},
		func() error {
			c, e := devices.FromDocument(doc)
			if e != nil {
				return e
			}
			return c.Validate()
		},
		func() error {
			c, e := gateway.FromDocument(doc)
			if e != nil {
				return e
			}
			if e = gateway.Validate(c, gatewayLinkView{facts: facts}).Err(); e != nil {
				return e
			}
			networks, e := (gatewayLinkView{facts: facts}).Networks()
			if e != nil {
				return e
			}
			// The tunnel's network, when the backup being restored has one.
			// Read here rather than through gateway's DialInView because this
			// validates a document that is not the live store yet.
			var dialIn *nat.DialIn
			if rc, e := remote.FromDocument(doc); e != nil {
				return e
			} else if rc.WireGuard.Enabled {
				dialIn = &nat.DialIn{
					Prefix: rc.WireGuard.SubnetOrDefault(),
					Tunnel: rc.WireGuard.InterfaceOrDefault(),
				}
			}
			return nat.Validate(c.NAT(dial.DesiredFor(dc).Interface, networks, dialIn), natLinkView{facts: facts}).Err()
		},
		func() error {
			c, e := remote.FromDocument(doc)
			if e != nil {
				return e
			}
			return errors.Join(remote.ValidateWireGuard(c, remoteNetworks{facts: facts}).Err(), remote.ValidateSocks(c).Err())
		},
		func() error {
			c, e := ingress.FromDocument(doc)
			if e != nil {
				return e
			}
			return ingress.Validate(c, ingressDNS{applier: dnsApplier}, ingressDevices{applier: deviceApplier}).Err()
		},
		func() error {
			c, e := iptv.FromDocument(doc)
			if e != nil {
				return e
			}
			_, e = iptv.Resolve(c, iptvLinks{facts: facts})
			return e
		},
		func() error {
			c, e := socksout.FromDocument(doc)
			if e != nil {
				return e
			}
			return c.Validate()
		},
		func() error {
			c, e := qos.FromDocument(doc)
			if e != nil {
				return e
			}
			return c.Validate()
		},
		func() error {
			c, e := firewall.FromDocument(doc)
			if e != nil {
				return e
			}
			if len(c.Ruleset) > 0 {
				return firewall.ValidateNative(c.Ruleset)
			}
			inside, e := (firewallBoundary{facts: facts, store: store}).Inside()
			if e != nil {
				return e
			}
			return firewall.Validate(c, inside)
		},
	}
	for _, check := range checks {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := check(); err != nil {
			return err
		}
	}
	return nil
}

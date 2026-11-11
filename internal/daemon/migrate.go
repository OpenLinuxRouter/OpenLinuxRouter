package daemon

import (
	"fmt"
	"log/slog"
	"net/netip"

	"github.com/open-linux-router/open-linux-router/internal/clients"
	"github.com/open-linux-router/open-linux-router/internal/core"
	"github.com/open-linux-router/open-linux-router/internal/devices"
	"github.com/open-linux-router/open-linux-router/internal/dhcp"
	"github.com/open-linux-router/open-linux-router/internal/gateway"
	"github.com/open-linux-router/open-linux-router/internal/ingress"
	"github.com/open-linux-router/open-linux-router/internal/link"
	"github.com/open-linux-router/open-linux-router/internal/qos"
)

// Migrating an old document, in the one place that can.
//
// Two conversions live here, and they share a write rather than a reason.
//
// # Pools keyed by interface (pre-0.3)
//
// A pool used to be keyed by kernel interface and to carry its own IPv4 range.
// It is now keyed by a network, and the network owns the subnet. Converting the
// pool itself is `dhcp`'s job and it does it while parsing (dhcp.LegacyPool) —
// but the *key* names an object `dhcp` does not own and cannot invent. Only
// this package sees both modules, so this is where a network gets created for
// each interface that had a pool.
//
// # Why at startup rather than on first write
//
// The alternative is to leave the old document alone and convert on the way
// past, every time. That means every surface carries the old shape forever, the
// two shapes have to agree about everything, and `olr.json` — which design.md
// §10 says is the recovery path you SSH in and read — has a schema that depends
// on when the box was installed. One conversion, written back once, keeps the
// file honest.
//
// # What it deliberately does not do
//
// It does not touch the kernel. The network it creates carries the subnet the
// interface *already has*, so applying it is a no-op on a box that was working
// — nobody's addresses move because olr was upgraded. If the interface has no
// IPv4 address the network is created without a subnet, which validates as a
// warning rather than an error and leaves the operator a row to fix instead of
// a parse failure.
//
// # The network spelled `group` (pre-rename)
//
// The network object was called `group` in the schema, the API and the code
// while every page and command called it a network. The word is now kept for
// sets of devices, so the stored keys moved: `link.groups` became
// `link.networks` and a pool's `group` became `network`. Reading the old keys
// is each module's job, and each does it while parsing (link.UnmarshalConfig,
// dhcp.UnmarshalConfig). Writing the new ones back is this file's, for the same
// reason as above — one conversion, written once, keeps the file honest — and a
// pre-0.3 document goes through both conversions in the same write, since the
// pools and networks it creates are marshalled in the current shape anyway.

// migrateDocument converts an old configuration in place, returning whether
// anything changed.
//
// It is idempotent: a document already in the current shape parses as one, so
// dhcp.Config.Legacy is empty, neither module reports an old key, and there is
// nothing to do. Everything it changes is written in one store.Save.
func migrateDocument(store *core.Store, facts link.Facts, log *slog.Logger) (bool, error) {
	doc, err := store.Load()
	if err != nil {
		return false, err
	}
	doc, moved, err := renameVocabulary(doc)
	if err != nil {
		return false, err
	}

	dhcpCfg, err := dhcp.FromDocument(doc)
	if err != nil {
		return false, err
	}

	rewriteLink := hasLegacyKeys(doc, link.ModuleName, link.HasLegacyKeys)
	rewriteDhcp := hasLegacyKeys(doc, dhcp.ModuleName, dhcp.HasLegacyKeys)
	legacyPools := len(dhcpCfg.Legacy) > 0
	if !legacyPools && !rewriteLink && !rewriteDhcp && !moved {
		return false, nil
	}

	linkCfg, err := link.FromDocument(doc)
	if err != nil {
		return false, err
	}

	if legacyPools {
		if err := migratePools(&linkCfg, &dhcpCfg, facts, log); err != nil {
			return false, err
		}
		// Both, whatever the keys said: the pools were re-keyed and the
		// networks they name were created, and neither half is any use alone.
		rewriteLink, rewriteDhcp = true, true
	}

	// Only the subtrees that changed are set. Re-marshalling one that had
	// nothing to convert would at best rewrite it identically, and a module
	// with no subtree at all would gain one it never had.
	if rewriteLink {
		linkData, err := link.MarshalConfig(linkCfg)
		if err != nil {
			return false, err
		}
		doc.Set(link.ModuleName, linkData)
	}
	if rewriteDhcp {
		dhcpData, err := dhcp.MarshalConfig(dhcpCfg)
		if err != nil {
			return false, err
		}
		doc.Set(dhcp.ModuleName, dhcpData)
	}

	// Both modules in one write. The store is a single document and a single
	// rename (core/store.go), so there is no window where the pools are keyed by
	// network and the networks do not exist yet — which would be a box that
	// refuses to serve DHCP until somebody noticed.
	if err := store.Save(doc); err != nil {
		return false, fmt.Errorf("storing the migrated configuration in %s: %w", store.Path(), err)
	}

	if legacyPools {
		log.Info("migrated dhcp pools onto networks",
			"pools", len(dhcpCfg.Pools), "networks", len(linkCfg.Networks))
	}
	if moved {
		log.Info("renamed the stored vocabulary: devices are network equipment, clients are the things on the LAN")
	}
	if !legacyPools && !moved {
		log.Info("renamed the stored network keys from group to network",
			"link", rewriteLink, "dhcp", rewriteDhcp)
	}
	return true, nil
}

// renameVocabulary moves the box's stored words to the ones it uses now.
//
// `devices` used to name the MAC-keyed inventory of things on the LAN and
// `network` the equipment olr reads; today `devices` is the equipment and
// `clients` the things on the LAN. The clients subtree is rewritten as well as
// re-keyed, because its list was stored under `devices` inside the subtree too.
//
// Three modules keep their own per-client entries — `qos` priorities, `gateway`
// exit overrides and `ingress` published services — and each called those
// `devices` or `device` for the same reason. Their parsers read the old keys
// and say so, so the rewrite here is one marshal each, only when needed.
func renameVocabulary(doc core.Document) (core.Document, bool, error) {
	moved := false
	if raw, ok := doc.Raw("devices"); ok {
		if _, taken := doc.Raw(clients.ModuleName); !taken {
			// A clients subtree decodes as one; an equipment subtree stored
			// under the new name does not (its entries carry adapter and
			// address), so it is left for the devices module to read.
			if c, err := clients.UnmarshalConfig(raw); err == nil && c.Legacy {
				data, err := clients.MarshalConfig(c)
				if err != nil {
					return doc, false, err
				}
				doc.Set(clients.ModuleName, data)
				doc.Delete("devices")
				moved = true
			}
		}
	}
	if raw, ok := doc.Raw("network"); ok {
		if _, taken := doc.Raw(devices.ModuleName); !taken {
			doc.Set(devices.ModuleName, raw)
			doc.Delete("network")
			moved = true
		}
	}
	if raw, ok := doc.Raw(qos.ModuleName); ok && qos.HasLegacyKeys(raw) {
		c, err := qos.Parse(raw)
		if err != nil {
			return doc, false, err
		}
		data, err := qos.MarshalConfig(c)
		if err != nil {
			return doc, false, err
		}
		doc.Set(qos.ModuleName, data)
		moved = true
	}
	if raw, ok := doc.Raw(gateway.ModuleName); ok && gateway.HasLegacyKeys(raw) {
		c, err := gateway.UnmarshalConfig(raw)
		if err != nil {
			return doc, false, err
		}
		data, err := gateway.MarshalConfig(c)
		if err != nil {
			return doc, false, err
		}
		doc.Set(gateway.ModuleName, data)
		moved = true
	}
	if raw, ok := doc.Raw(ingress.ModuleName); ok {
		c, err := ingress.UnmarshalConfig(raw)
		if err != nil {
			return doc, false, err
		}
		if c.Legacy {
			data, err := ingress.MarshalConfig(c)
			if err != nil {
				return doc, false, err
			}
			doc.Set(ingress.ModuleName, data)
			moved = true
		}
	}
	return doc, moved, nil
}

// hasLegacyKeys asks a module whether its stored subtree still uses a key the
// module has since renamed. A document without the subtree has nothing to
// rename.
func hasLegacyKeys(doc core.Document, module string, check func([]byte) bool) bool {
	raw, ok := doc.Raw(module)
	return ok && check(raw)
}

// migratePools creates a network for each interface a pre-0.3 pool was keyed
// by, and keys the pool by it.
func migratePools(linkCfg *link.Config, dhcpCfg *dhcp.Config, facts link.Facts, log *slog.Logger) error {
	observed, err := facts.Interfaces()
	if err != nil {
		// Without the interface list there is no subnet to give the networks,
		// and a migration that silently dropped every subnet would renumber the
		// box on the next apply. Refusing leaves the old document intact and
		// readable, which is the recoverable failure.
		return fmt.Errorf("reading interfaces to migrate the dhcp pools: %w", err)
	}
	prefixes := map[string]netip.Prefix{}
	for _, info := range observed {
		if p, ok := core.FirstIPv4(info.Prefixes); ok {
			prefixes[info.Name] = p
		}
	}

	for i, old := range dhcpCfg.Legacy {
		iface := old.Interface
		if iface == "" {
			continue
		}

		// The network takes the interface's name. It is not a pretty name, and
		// that is the right trade: the operator can rename it, and anything else
		// would be us inventing a name they never chose for a network they
		// already have.
		name := iface
		if _, exists := linkCfg.Network(name); !exists {
			n := link.Network{Name: name, Members: []string{iface}}
			if prefix, ok := prefixes[iface]; ok {
				// The address the interface holds *now*, so this migration
				// changes nothing on the box. A router address different from
				// the one already configured would move it on the next apply.
				router := prefix.Addr()
				n.IPv4 = &link.NetworkIPv4{Subnet: prefix.Masked(), Router: &router}
			} else {
				log.Warn("migrated a dhcp pool whose interface has no IPv4 address",
					"interface", iface, "network", name)
			}
			linkCfg.SetNetwork(n)
			// A pool on an interface nobody adopted could not have been applied,
			// but it could have been stored — and dropping the adoption here
			// would turn a config that was merely invalid into one that is
			// invalid for a second, more confusing reason.
			linkCfg.Adopt(iface)
		}
		dhcpCfg.Pools[i].Network = name
	}

	dhcpCfg.Legacy = nil
	dhcpCfg.Normalize()
	linkCfg.Normalize()
	return nil
}

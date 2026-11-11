package daemon

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/open-linux-router/open-linux-router/internal/clients"
	"github.com/open-linux-router/open-linux-router/internal/core"
	"github.com/open-linux-router/open-linux-router/internal/devices"
	"github.com/open-linux-router/open-linux-router/internal/dhcp"
	"github.com/open-linux-router/open-linux-router/internal/gateway"
	"github.com/open-linux-router/open-linux-router/internal/ingress"
	"github.com/open-linux-router/open-linux-router/internal/link"
	"github.com/open-linux-router/open-linux-router/internal/qos"
)

// Migrating a 0.2.0 document.
//
// The failure this guards against is not subtle: 0.2.0 shipped, so there are
// boxes in the field whose `olr.json` keys pools by interface. Getting this
// wrong means a router that stops serving DHCP after an upgrade, which nobody
// notices until the leases expire and then everything breaks at once.

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func storeWith(t *testing.T, document string) *core.Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "olr.json")
	if document != "" {
		if err := os.WriteFile(path, []byte(document), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return core.NewStore(path, link.ModuleName, dhcp.ModuleName)
}

// The 0.2.0 shape: pools keyed by interface, range at the top level, IPv6 as a
// sibling `ra` field, and no networks anywhere.
const legacyDocument = `{
  "link": {"adopted": ["lan0"]},
  "dhcp": {
    "enabled": true,
    "pools": [{
      "interface": "lan0",
      "start": "192.168.1.100",
      "end": "192.168.1.200",
      "lease_time": "6h",
      "domain": "lan",
      "ra": "slaac"
    }],
    "reservations": [{"mac": "aa:bb:cc:dd:ee:ff", "ip": "192.168.1.50"}]
  }
}`

func TestMigrateCreatesANetworkForEachLegacyPool(t *testing.T) {
	store := storeWith(t, legacyDocument)
	facts := testFacts(t, "")
	facts.Store = store

	changed, err := migrateDocument(store, facts, quietLogger())
	if err != nil {
		t.Fatalf("migrateDocument: %v", err)
	}
	if !changed {
		t.Fatal("reported no change for a document in the old shape")
	}

	doc, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	linkCfg, err := link.FromDocument(doc)
	if err != nil {
		t.Fatal(err)
	}
	dhcpCfg, err := dhcp.FromDocument(doc)
	if err != nil {
		t.Fatal(err)
	}

	n, ok := linkCfg.Network("lan0")
	if !ok {
		t.Fatalf("no network was created; networks = %+v", linkCfg.Networks)
	}
	if len(n.Members) != 1 || n.Members[0] != "lan0" {
		t.Errorf("Members = %v, want [lan0]", n.Members)
	}

	// The subnet the interface *already has*, so applying the migrated config
	// is a no-op on a box that was working. Anything else renumbers a network
	// because olr was upgraded.
	if n.IPv4 == nil || n.IPv4.Subnet.String() != "192.168.1.0/24" {
		t.Fatalf("IPv4 = %+v, want the interface's own 192.168.1.0/24", n.IPv4)
	}
	if n.IPv4.RouterAddr().String() != "192.168.1.2" {
		t.Errorf("router = %s, want the address lan0 already holds (192.168.1.2)",
			n.IPv4.RouterAddr())
	}

	if len(dhcpCfg.Pools) != 1 {
		t.Fatalf("pools = %+v", dhcpCfg.Pools)
	}
	p := dhcpCfg.Pools[0]
	switch {
	case p.Network != "lan0":
		t.Errorf("Network = %q, want lan0", p.Network)
	case p.IPv4 == nil:
		t.Fatal("the IPv4 range was dropped")
	// Carried across explicitly rather than left to be derived: devices hold
	// addresses from this range right now, and re-deriving would renumber a
	// working network.
	case p.IPv4.Start.String() != "192.168.1.100" || p.IPv4.End.String() != "192.168.1.200":
		t.Errorf("range = %s-%s, want the range that was stored", p.IPv4.Start, p.IPv4.End)
	case p.RA() != dhcp.RASLAAC:
		t.Errorf("ipv6 mode = %q, want the `ra` field's slaac", p.RA())
	case p.LeaseTime.String() != "6h":
		t.Errorf("lease = %s, want 6h", p.LeaseTime)
	case p.Domain != "lan":
		t.Errorf("domain = %q", p.Domain)
	}

	if len(dhcpCfg.Reservations) != 1 {
		t.Errorf("reservations were lost: %+v", dhcpCfg.Reservations)
	}
}

// The migrated document has to validate, or the box comes up refusing to serve
// the configuration it just rewrote — which is worse than not migrating at all.
func TestMigratedConfigValidates(t *testing.T) {
	store := storeWith(t, legacyDocument)
	facts := testFacts(t, "")
	facts.Store = store

	if _, err := migrateDocument(store, facts, quietLogger()); err != nil {
		t.Fatal(err)
	}

	doc, _ := store.Load()
	dhcpCfg, _ := dhcp.FromDocument(doc)
	if res := dhcp.Validate(dhcpCfg, dhcpNetworkView{facts: facts}); !res.OK() {
		t.Errorf("the migrated config does not validate: %v", res.Errors)
	}
}

// Running twice must not create a second network or re-key anything: a document
// already in the current shape parses as one, so there is nothing to convert.
func TestMigrateIsIdempotent(t *testing.T) {
	store := storeWith(t, legacyDocument)
	facts := testFacts(t, "")
	facts.Store = store

	if _, err := migrateDocument(store, facts, quietLogger()); err != nil {
		t.Fatal(err)
	}
	first, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}

	changed, err := migrateDocument(store, facts, quietLogger())
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Error("reported a change on the second run")
	}

	second, _ := store.Load()
	firstDhcp, _ := dhcp.FromDocument(first)
	secondDhcp, _ := dhcp.FromDocument(second)
	if len(firstDhcp.Pools) != len(secondDhcp.Pools) {
		t.Errorf("pools changed on the second run: %d then %d",
			len(firstDhcp.Pools), len(secondDhcp.Pools))
	}
}

func TestMigrateLeavesACurrentDocumentAlone(t *testing.T) {
	const current = `{"link":{"adopted":["lan0"],` +
		`"networks":[{"name":"lan","members":["lan0"],"ipv4":{"subnet":"192.168.1.0/24"}}]},` +
		`"dhcp":{"enabled":true,"pools":[{"network":"lan"}]}}`

	store := storeWith(t, current)
	facts := testFacts(t, "")
	facts.Store = store

	changed, err := migrateDocument(store, facts, quietLogger())
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Error("rewrote a document that was already in the current shape")
	}
}

// A pool on an interface with no IPv4 address still migrates. The network is
// created without a subnet, which validates as a warning and leaves a row the
// operator can fix — rather than a parse error naming a field they never typed.
func TestMigrateKeepsAPoolWhoseInterfaceHasNoAddress(t *testing.T) {
	const onAbsent = `{"dhcp":{"enabled":true,"pools":[{"interface":"nosuch0",` +
		`"start":"10.0.0.10","end":"10.0.0.20"}]}}`

	store := storeWith(t, onAbsent)
	facts := testFacts(t, "")
	facts.Store = store

	if _, err := migrateDocument(store, facts, quietLogger()); err != nil {
		t.Fatalf("refused to migrate a pool on an absent interface: %v", err)
	}

	doc, _ := store.Load()
	linkCfg, _ := link.FromDocument(doc)
	n, ok := linkCfg.Network("nosuch0")
	if !ok {
		t.Fatal("no network was created for the pool")
	}
	if n.IPv4 != nil {
		t.Errorf("invented a subnet for an interface with no address: %+v", n.IPv4)
	}
	// Adoption comes along: the pool could not have been applied without it,
	// and dropping it turns a merely-invalid config into a confusingly-invalid
	// one.
	if !linkCfg.IsAdopted("nosuch0") {
		t.Error("the interface was not adopted, so the network cannot validate")
	}
}

// Both modules are written in one store.Save. The store is a single document
// and a single rename, so there is no window where the pools are keyed by
// network and the networks do not exist yet.
func TestMigrateWritesBothModulesAtOnce(t *testing.T) {
	store := storeWith(t, legacyDocument)
	facts := testFacts(t, "")
	facts.Store = store

	if _, err := migrateDocument(store, facts, quietLogger()); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(store.Path())
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if !strings.Contains(text, `"networks"`) || !strings.Contains(text, `"network"`) {
		t.Errorf("the file has one half of the migration but not the other:\n%s", text)
	}
	if strings.Contains(text, `"interface"`) {
		t.Errorf("the old key survived the rewrite:\n%s", text)
	}
	// A pre-0.3 document goes all the way to the current keys in the same
	// write, not to the spelling that was current when 0.3 shipped.
	if strings.Contains(text, `"groups"`) || strings.Contains(text, `"group"`) {
		t.Errorf("the pre-0.3 migration wrote the network's old name:\n%s", text)
	}
}

// The document every box that ran a version before the rename has: networks
// under `groups`, and each pool naming its network as `group`.
const groupKeyedDocument = `{
  "link": {
    "adopted": ["lan0"],
    "groups": [{"name": "lan", "members": ["lan0"], "ipv4": {"subnet": "192.168.1.0/24"}}]
  },
  "dhcp": {
    "enabled": true,
    "pools": [{"group": "lan", "ipv4": {"start": "192.168.1.100", "end": "192.168.1.200"}}]
  }
}`

func TestMigrateRenamesTheNetworkKeys(t *testing.T) {
	store := storeWith(t, groupKeyedDocument)
	facts := testFacts(t, "")
	facts.Store = store

	changed, err := migrateDocument(store, facts, quietLogger())
	if err != nil {
		t.Fatalf("migrateDocument: %v", err)
	}
	if !changed {
		t.Fatal("reported no change for a document spelling the network `group`")
	}

	raw, err := os.ReadFile(store.Path())
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if strings.Contains(text, `"groups"`) || strings.Contains(text, `"group"`) {
		t.Errorf("an old key survived the rewrite:\n%s", text)
	}
	if !strings.Contains(text, `"networks"`) || !strings.Contains(text, `"network"`) {
		t.Errorf("the new keys were not written:\n%s", text)
	}

	// Nothing but the spelling moved: the network, its subnet, and the pool's
	// range are what they were.
	doc, _ := store.Load()
	linkCfg, err := link.FromDocument(doc)
	if err != nil {
		t.Fatal(err)
	}
	n, ok := linkCfg.Network("lan")
	if !ok || n.IPv4 == nil || n.IPv4.Subnet.String() != "192.168.1.0/24" {
		t.Fatalf("network = %+v, want lan on 192.168.1.0/24", linkCfg.Networks)
	}
	dhcpCfg, err := dhcp.FromDocument(doc)
	if err != nil {
		t.Fatal(err)
	}
	p, ok := dhcpCfg.Pool("lan")
	if !ok || p.IPv4 == nil || p.IPv4.Start.String() != "192.168.1.100" {
		t.Fatalf("pools = %+v, want lan's range carried across", dhcpCfg.Pools)
	}
	if res := dhcp.Validate(dhcpCfg, dhcpNetworkView{facts: facts}); !res.OK() {
		t.Errorf("the renamed config does not validate: %v", res.Errors)
	}

	again, err := migrateDocument(store, facts, quietLogger())
	if err != nil {
		t.Fatal(err)
	}
	if again {
		t.Error("reported a change on the second run")
	}
	after, _ := os.ReadFile(store.Path())
	if string(after) != text {
		t.Errorf("the second run rewrote the file:\n%s\nthen\n%s", text, after)
	}
}

// Only the subtree that carried an old key is rewritten. A box with networks
// and no dhcp configuration must not gain a dhcp section because of a rename in
// link.
func TestMigrateRenamesOnlyTheSubtreeThatNeedsIt(t *testing.T) {
	const linkOnly = `{"link":{"adopted":["lan0"],` +
		`"groups":[{"name":"lan","members":["lan0"],"ipv4":{"subnet":"192.168.1.0/24"}}]}}`

	store := storeWith(t, linkOnly)
	facts := testFacts(t, "")
	facts.Store = store

	changed, err := migrateDocument(store, facts, quietLogger())
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("reported no change for a link subtree spelling the network `group`")
	}
	doc, _ := store.Load()
	if _, ok := doc.Raw(dhcp.ModuleName); ok {
		t.Error("renaming link's key created a dhcp section")
	}
	raw, _ := doc.Raw(link.ModuleName)
	if link.HasLegacyKeys(raw) {
		t.Errorf("link still carries the old key: %s", raw)
	}
}

// The vocabulary swap: `devices` used to be the things on the LAN and
// `network` the equipment olr reads. Both move, in one write, and the clients
// subtree is rewritten as well as re-keyed — its list was stored under
// `devices` inside the subtree too.
func TestMigrateRenamesTheTwoInventories(t *testing.T) {
	const document = `{
  "devices": {
    "devices": [{"mac": "aa:bb:cc:dd:ee:ff", "name": "printer"}],
    "groups": [{"name": "Serving"}]
  },
  "network": {
    "devices": [{"id": "switch", "adapter": "mercury", "address": "http://192.168.1.2", "roles": ["switch"]}]
  }
}`
	path := filepath.Join(t.TempDir(), "olr.json")
	if err := os.WriteFile(path, []byte(document), 0o644); err != nil {
		t.Fatal(err)
	}
	store := core.NewStore(path, clients.ModuleName, devices.ModuleName)

	changed, err := migrateDocument(store, testFacts(t, ""), quietLogger())
	if err != nil {
		t.Fatalf("migrateDocument: %v", err)
	}
	if !changed {
		t.Fatal("reported no change for a document in the old vocabulary")
	}

	doc, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := doc.Raw("network"); ok {
		t.Error("the old equipment key survived the migration")
	}
	clientsCfg, err := clients.FromDocument(doc)
	if err != nil {
		t.Fatal(err)
	}
	if len(clientsCfg.Clients) != 1 || clientsCfg.Clients[0].Name != "printer" {
		t.Errorf("client inventory = %+v, want the printer", clientsCfg.Clients)
	}
	if len(clientsCfg.Groups) != 1 || clientsCfg.Groups[0].Name != "Serving" {
		t.Errorf("groups = %+v, want Serving", clientsCfg.Groups)
	}
	devicesCfg, err := devices.FromDocument(doc)
	if err != nil {
		t.Fatal(err)
	}
	if len(devicesCfg.Devices) != 1 || devicesCfg.Devices[0].ID != "switch" {
		t.Errorf("device inventory = %+v, want the switch", devicesCfg.Devices)
	}

	// Idempotent: a second run finds nothing to do.
	changed, err = migrateDocument(store, testFacts(t, ""), quietLogger())
	if err != nil {
		t.Fatalf("second migrateDocument: %v", err)
	}
	if changed {
		t.Fatal("the migration rewrote a document already in the current shape")
	}
}

// A document already using `devices` for equipment must not be mistaken for the
// old client inventory: the two are told apart by shape, not only by key.
func TestMigrateLeavesTheEquipmentInventoryAlone(t *testing.T) {
	const document = `{
  "devices": {"devices": [{"id": "ap", "adapter": "openwrt", "address": "http://192.168.1.3"}]}
}`
	path := filepath.Join(t.TempDir(), "olr.json")
	if err := os.WriteFile(path, []byte(document), 0o644); err != nil {
		t.Fatal(err)
	}
	store := core.NewStore(path, clients.ModuleName, devices.ModuleName)

	if _, err := migrateDocument(store, testFacts(t, ""), quietLogger()); err != nil {
		t.Fatalf("migrateDocument: %v", err)
	}
	doc, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	devicesCfg, err := devices.FromDocument(doc)
	if err != nil {
		t.Fatalf("equipment inventory was lost: %v", err)
	}
	if len(devicesCfg.Devices) != 1 || devicesCfg.Devices[0].ID != "ap" {
		t.Errorf("device inventory = %+v, want the access point", devicesCfg.Devices)
	}
	if _, ok := doc.Raw(clients.ModuleName); ok {
		t.Error("an equipment document was re-keyed as a client inventory")
	}
}

// The per-client entries inside other modules moved with the same vocabulary:
// qos priorities, gateway exit overrides and ingress published services all
// called the client they name a `device`.
func TestMigrateRenamesThePerClientEntries(t *testing.T) {
	const document = `{
  "qos": {"devices": [{"mac": "aa:bb:cc:dd:ee:ff", "priority": "high"}]},
  "gateway": {"devices": [{"mac": "aa:bb:cc:dd:ee:ff", "exit": "Proxy"}]},
  "ingress": {"enabled": false, "services": [{"name": "nas", "upstream": {"device": "nuc", "port": 8080}}]}
}`
	path := filepath.Join(t.TempDir(), "olr.json")
	if err := os.WriteFile(path, []byte(document), 0o644); err != nil {
		t.Fatal(err)
	}
	store := core.NewStore(path, qos.ModuleName, gateway.ModuleName, ingress.ModuleName)

	changed, err := migrateDocument(store, testFacts(t, ""), quietLogger())
	if err != nil {
		t.Fatalf("migrateDocument: %v", err)
	}
	if !changed {
		t.Fatal("reported no change for a document in the old vocabulary")
	}

	doc, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	qosCfg, err := qos.FromDocument(doc)
	if err != nil {
		t.Fatal(err)
	}
	if len(qosCfg.Clients) != 1 || qosCfg.Clients[0].Priority != qos.High {
		t.Errorf("qos clients = %+v, want the high-priority entry", qosCfg.Clients)
	}
	gwCfg, err := gateway.FromDocument(doc)
	if err != nil {
		t.Fatal(err)
	}
	if len(gwCfg.Clients) != 1 || gwCfg.Clients[0].Exit != "Proxy" {
		t.Errorf("gateway clients = %+v, want the Proxy override", gwCfg.Clients)
	}
	inCfg, err := ingress.FromDocument(doc)
	if err != nil {
		t.Fatal(err)
	}
	if len(inCfg.Services) != 1 || inCfg.Services[0].Upstream.Client != "nuc" {
		t.Errorf("ingress upstream = %+v, want the nuc client", inCfg.Services)
	}
	if inCfg.Legacy {
		t.Error("the migrated ingress document still reports a legacy key")
	}

	changed, err = migrateDocument(store, testFacts(t, ""), quietLogger())
	if err != nil {
		t.Fatalf("second migrateDocument: %v", err)
	}
	if changed {
		t.Fatal("the migration rewrote a document already in the current shape")
	}
}

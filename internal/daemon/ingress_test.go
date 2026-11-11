package daemon

import (
	"path/filepath"
	"testing"

	"github.com/open-linux-router/open-linux-router/internal/clients"
	"github.com/open-linux-router/open-linux-router/internal/core"
	"github.com/open-linux-router/open-linux-router/internal/ingress"
)

func TestIngressDeviceReferencesSurviveRename(t *testing.T) {
	const mac = "aa:bb:cc:00:00:01"
	store := core.NewStore(filepath.Join(t.TempDir(), "olr.json"), "clients", "ingress")
	a := clients.Applier{Store: store}
	cfg := clients.Config{Clients: []clients.Client{{MAC: mac, Name: "nas"}}}
	if _, err := a.Save(cfg); err != nil {
		t.Fatal(err)
	}
	doc, _ := store.Load()
	doc.Set("ingress", []byte(`{"services":[{"name":"files","upstream":{"client":"nas","port":80}},{"name":"missing","upstream":{"client":"unknown","port":80}}]}`))
	if err := store.Save(doc); err != nil {
		t.Fatal(err)
	}
	view := ingressClients{applier: a}
	loaded, err := (ingress.Applier{Store: store, Clients: view}).Load()
	if err != nil || loaded.Services[0].Upstream.Client != mac {
		t.Fatalf("legacy reference was not resolved on read: %+v, %v", loaded, err)
	}
	a.BeforeSave = func(doc *core.Document) error { return preserveIngressClients(doc, view) }
	cfg.Clients[0].Name = "storage"
	if _, err := a.Save(cfg); err != nil {
		t.Fatal(err)
	}
	doc, _ = store.Load()
	published, err := ingress.FromDocument(doc)
	if err != nil {
		t.Fatal(err)
	}
	if published.Services[0].Upstream.Client != mac || published.Services[1].Upstream.Client != "unknown" {
		t.Fatalf("references = %+v", published.Services)
	}
	info, err := view.Client(mac)
	if err != nil || info.Name != "storage" || info.MAC != mac {
		t.Fatalf("renamed device = %+v, %v", info, err)
	}
	if _, err := view.Client("nas"); err == nil {
		t.Fatal("old name unexpectedly resolves")
	}
	// A later device adopting the old name must not inherit the service.
	cfg.Clients = append(cfg.Clients, clients.Client{MAC: "aa:bb:cc:00:00:02", Name: "nas"})
	if _, err := a.Save(cfg); err != nil {
		t.Fatal(err)
	}
	info, err = view.Client(published.Services[0].Upstream.Client)
	if err != nil || info.MAC != mac {
		t.Fatalf("service moved to another device: %+v, %v", info, err)
	}
	cfg.Clients[1].Name = "storage"
	if _, err := a.Save(cfg); err != nil {
		t.Fatal(err)
	}
	if _, err := view.Client("storage"); err == nil {
		t.Fatal("ambiguous name unexpectedly resolves")
	}
	if _, err := view.Client(mac); err != nil {
		t.Fatal("MAC must still resolve when labels collide:", err)
	}
}

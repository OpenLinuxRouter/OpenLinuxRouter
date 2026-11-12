package gateway

import (
	"context"
	"net/netip"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/core"
	"github.com/open-linux-router/open-linux-router/internal/runtimedb"
)

func TestActivitySQLiteImportAndBaseline(t *testing.T) {
	dir := t.TempDir()
	legacy := filepath.Join(dir, "activity.json")
	a, err := OpenActivity(legacy)
	if err != nil {
		t.Fatal(err)
	}
	addr := netip.MustParseAddr("192.0.2.1")
	at := time.Now().Truncate(30 * time.Minute)
	a.observe(at, []Usage{{Addr: addr, UpBytes: 100}}, true)
	a.observe(at.Add(time.Second), []Usage{{Addr: addr, UpBytes: 120}}, true)
	if err := a.Save(); err != nil {
		t.Fatal(err)
	}
	db, err := runtimedb.Open(filepath.Join(dir, "runtime.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	b, err := OpenActivityDB(db, legacy)
	if err != nil {
		t.Fatal(err)
	}
	b.observe(at.Add(2*time.Second), []Usage{{Addr: addr, UpBytes: 900}}, true)
	b.observe(at.Add(3*time.Second), []Usage{{Addr: addr, UpBytes: 910}}, true)
	if err := b.Save(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacy, []byte("corrupt"), 0600); err != nil {
		t.Fatal(err)
	}
	c, err := OpenActivityDB(db, legacy)
	if err != nil {
		t.Fatal(err)
	}
	if got := c.snapshot("", []netip.Addr{addr}); len(got) != 1 || got[0].Bytes.Up != 30 {
		t.Fatalf("history %+v", got)
	}
}

func TestLatencyConfigMigrationClearAndRename(t *testing.T) {
	store := core.NewStore(filepath.Join(t.TempDir(), "olr.json"), ModuleName)
	legacy := store.Path() + ".latency-sites"
	if err := os.WriteFile(legacy, []byte(`[{"name":"Example","url":"https://example.org"}]`), 0600); err != nil {
		t.Fatal(err)
	}
	m, err := OpenCustomLatencyConfig(store)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Config()) != 1 {
		t.Fatal("targets not imported")
	}
	if err := m.Replace(nil); err != nil {
		t.Fatal(err)
	}
	m, err = OpenCustomLatencyConfig(store)
	if err != nil || len(m.Config()) != 0 {
		t.Fatal("cleared targets resurrected", err)
	}
	c := Config{Exits: []Exit{{Name: "old"}}, LatencySites: []CustomLatencySite{{Name: "Example", URL: "https://example.org", Exit: "old"}}}
	if !c.Rename("old", "new") || c.LatencySites[0].Exit != "new" {
		t.Fatal("rename did not cascade")
	}
}

func TestLatencyTargetSurvivesRestart(t *testing.T) {
	store := core.NewStore(filepath.Join(t.TempDir(), "olr.json"), ModuleName)
	custom, err := OpenCustomLatencyConfig(store)
	if err != nil {
		t.Fatal(err)
	}
	sites := []CustomLatencySite{{Name: "Example", URL: "https://example.org/"}}
	if err := custom.Replace(sites); err != nil {
		t.Fatal(err)
	}
	lock := core.NewLock()
	m := NewLatencyMonitor()
	m.Store, m.Lock = store, lock
	m.dnsProbe = nil
	m.probe = func(_ context.Context, url string) (float64, error) {
		if url == m.targets[1].URL {
			return 1, nil
		}
		return 10, nil
	}
	m.sample(context.Background())
	restored, err := OpenLatencyMonitor(store, lock)
	if err != nil {
		t.Fatal(err)
	}
	if restored.Snapshot().Target != "Baidu" {
		t.Fatal("saved target missing before first probe")
	}
	restored.dnsProbe = nil
	restored.probe = func(_ context.Context, url string) (float64, error) {
		if url != m.targets[1].URL {
			t.Errorf("restart probed unselected target: %s", url)
		}
		return 8, nil
	}
	restored.sample(context.Background())
	if s := restored.Snapshot(); s.Target != "Baidu" || *s.Milliseconds != 8 {
		t.Fatalf("restored: %+v", s)
	}
	if err := lock.Do(context.Background(), func() error { return restored.SetTarget(m.targets[3].URL) }); err != nil {
		t.Fatal(err)
	}
	restored.mu.Lock()
	target, err := restored.loadTarget()
	restored.mu.Unlock()
	if err != nil || target != m.targets[3].URL {
		t.Fatalf("manual selection not saved: %s %v", target, err)
	}
	doc, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := FromDocument(doc)
	if err != nil || len(cfg.LatencySites) != 1 || cfg.LatencySites[0] != sites[0] {
		t.Fatalf("target selection lost custom sites: %+v %v", cfg.LatencySites, err)
	}
}

package gateway

import (
	"context"
	"net/netip"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/runtimedb"
)

func TestActivityCeilingEvictsOldestAndKeepsRecording(t *testing.T) {
	a := NewActivity()
	// A real ceiling is a quarter of a million pairs; lowering it is what makes
	// the reached-ceiling path testable without materializing them.
	a.ceiling = 3
	at := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	addr := netip.MustParseAddr("192.168.1.2")
	busy := netip.MustParseAddr("192.168.1.3")
	older := at.Add(-time.Hour).Unix()
	newer := at.Add(-30 * time.Minute).Unix()
	a.buckets[older] = map[string]activityBytes{busy.String(): {Up: 1}, addr.String(): {Up: 1}}
	a.buckets[newer] = map[string]activityBytes{busy.String(): {Up: 1}}
	a.entries = 3

	a.observe(at, []Usage{{Addr: addr, UpBytes: 100}}, true)
	a.observe(at.Add(10*time.Second), []Usage{{Addr: addr, UpBytes: 150}}, true)

	if got := a.snapshot("", []netip.Addr{addr}); len(got) != 1 || got[0].Bytes.Up != 50 {
		t.Fatalf("a full history stopped recording: %+v", got)
	}
	if _, ok := a.buckets[older]; ok {
		t.Fatal("the oldest bucket was kept instead of making room")
	}
	if _, ok := a.buckets[newer]; !ok {
		t.Fatal("a newer bucket was evicted before the oldest")
	}
	if a.entries > a.ceiling {
		t.Fatalf("ceiling exceeded: %d entries", a.entries)
	}
}

func TestActivitySaveForgetsEvictedBuckets(t *testing.T) {
	store, err := runtimedb.Open(filepath.Join(t.TempDir(), "runtime.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Now()
	old := now.Add(-2 * time.Hour).Truncate(30 * time.Minute).Unix()
	addr := netip.MustParseAddr("192.168.1.2")
	a := NewActivity()
	a.db = store
	a.buckets[old] = map[string]activityBytes{addr.String(): {Up: 10, Down: 5}}
	a.entries = 1
	if err := a.Save(); err != nil {
		t.Fatal(err)
	}
	if rows, err := store.Activity(0, now.Unix()); err != nil || len(rows) != 1 {
		t.Fatalf("history was not stored: %+v %v", rows, err)
	}

	delete(a.buckets, old)
	a.entries = 0
	if err := a.Save(); err != nil {
		t.Fatal(err)
	}
	if rows, err := store.Activity(0, now.Unix()); err != nil || len(rows) != 0 {
		t.Fatalf("an evicted bucket would come back after a restart: %+v %v", rows, err)
	}
}

func TestActivityPersistenceAndRestartBaseline(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "activity.json")
	a, err := OpenActivity(path)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Now().Truncate(30 * time.Minute)
	addr := netip.MustParseAddr("192.168.1.2")
	sample := func(up uint64) []Usage { return []Usage{{Addr: addr, UpBytes: up}} }
	a.observe(at, sample(100), true)
	a.observe(at.Add(10*time.Second), sample(150), true)
	if err := a.Save(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("history permissions: %v, %v", info, err)
	}
	b, err := OpenActivity(path)
	if err != nil {
		t.Fatal(err)
	}
	b.observe(at.Add(20*time.Second), sample(1000), true)
	if got := b.snapshot("", []netip.Addr{addr}); len(got) != 1 || got[0].Bytes.Up != 50 {
		t.Fatalf("restart counted downtime: %+v", got)
	}
	b.observe(at.Add(30*time.Second), sample(1010), true)
	if err := b.Save(); err != nil {
		t.Fatal(err)
	}
	c, err := OpenActivity(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := c.snapshot("", []netip.Addr{addr}); len(got) != 1 || got[0].Bytes.Up != 60 {
		t.Fatalf("new samples did not survive restart: %+v", got)
	}
}

func TestActivityRestorePrunesAndRejectsInvalidHistory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "activity.json")
	a, err := OpenActivity(path)
	if err != nil {
		t.Fatal(err)
	}
	addr := netip.MustParseAddr("2001:db8::1")
	a.buckets[time.Now().Add(-ActivityRetention-time.Hour).Unix()] = map[string]activityBytes{addr.String(): {Up: 10}}
	if err := a.Save(); err != nil {
		t.Fatal(err)
	}
	b, err := OpenActivity(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(b.snapshot("", []netip.Addr{addr})) != 0 {
		t.Fatal("expired history restored")
	}
	for _, data := range []string{`{"version":2}`, `{"version":1,"buckets":{"1":`, `{"version":1,"buckets":{"9999999999":{"not-an-ip":{"up":1}}}}`} {
		if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := OpenActivity(path); err == nil {
			t.Fatalf("invalid history accepted: %s", data)
		}
	}
}

func TestActivitySaveFailurePreservesMemory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "parent")
	if err := os.WriteFile(path, []byte("blocked"), 0o600); err != nil {
		t.Fatal(err)
	}
	a := NewActivity()
	a.path = filepath.Join(path, "activity.json")
	addr := netip.MustParseAddr("192.168.1.2")
	a.buckets[time.Now().Unix()] = map[string]activityBytes{addr.String(): {Up: 10}}
	if err := a.Save(); err == nil {
		t.Fatal("write failure hidden")
	}
	if got := a.snapshot("", []netip.Addr{addr}); len(got) != 1 || got[0].Bytes.Up != 10 {
		t.Fatal("failed save lost history")
	}
}

func TestActivityDifferencesAndGaps(t *testing.T) {
	a := NewActivity()
	at := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	addr := netip.MustParseAddr("192.168.1.2")
	sample := func(up uint64) []Usage { return []Usage{{Addr: addr, UpBytes: up}} }
	a.observe(at, sample(100), true)
	if len(a.snapshot("", []netip.Addr{addr})) != 0 {
		t.Fatal("baseline is not activity")
	}
	a.observe(at.Add(time.Minute), sample(140), true)
	a.observe(at.Add(2*time.Minute), sample(10), true)
	a.observe(at.Add(3*time.Minute), sample(15), true)
	result := a.snapshot("", []netip.Addr{addr})
	if len(result) != 1 || result[0].Bytes.Up != 45 {
		t.Fatalf("reset corrupted deltas: %+v", result)
	}
	a.observe(at.Add(10*time.Minute), sample(900), true)
	if a.snapshot("", []netip.Addr{addr})[0].Bytes.Up != 45 {
		t.Fatal("gap counted as activity")
	}
	a.observe(at.Add(11*time.Minute), nil, false)
	a.observe(at.Add(12*time.Minute), sample(1000), true)
	if a.snapshot("", []netip.Addr{addr})[0].Bytes.Up != 45 {
		t.Fatal("failed sample retained baseline")
	}
	a.observe(at.Add(ActivityRetention+time.Hour), sample(2000), true)
	if len(a.snapshot("", []netip.Addr{addr})) != 0 {
		t.Fatal("history was not expired")
	}
}

func TestActivityShortSamplingInterval(t *testing.T) {
	a := NewActivity()
	at := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	addr := netip.MustParseAddr("192.168.1.2")
	a.observe(at, []Usage{{Addr: addr, UpBytes: 100}}, true)
	a.observe(at.Add(10*time.Second), []Usage{{Addr: addr, UpBytes: 150, DownBytes: 25}}, true)
	result := a.snapshot("", []netip.Addr{addr})
	if len(result) != 1 || result[0].Bytes.Up != 50 || result[0].Bytes.Down != 25 {
		t.Fatalf("short interval lost traffic: %+v", result)
	}
}

func TestActivityBoundaryZeroAndExitAggregation(t *testing.T) {
	a := NewActivity()
	at := time.Date(2026, 10, 14, 12, 29, 0, 0, time.UTC)
	addr := netip.MustParseAddr("192.168.1.2")
	sample := func(up uint64) []Usage {
		return []Usage{{Addr: addr, UpBytes: up}, {Addr: addr, Exit: "vpn", DownBytes: up}}
	}
	a.observe(at, sample(100), true)
	a.observe(at.Add(time.Minute), sample(200), true)
	if len(a.snapshot("", []netip.Addr{addr})) != 0 {
		t.Fatal("boundary interval was attributed to one bucket")
	}
	a.observe(at.Add(2*time.Minute), sample(200), true)
	result := a.snapshot("", []netip.Addr{addr})
	if len(result) != 1 || result[0].Bytes.Up != 0 {
		t.Fatal("observed zero was lost")
	}
	a.observe(at.Add(3*time.Minute), sample(250), true)
	result = a.snapshot("", []netip.Addr{addr})
	if result[0].Bytes.Up != 50 || result[0].Bytes.Down != 50 {
		t.Fatalf("exits were not aggregated: %+v", result)
	}
}

func TestActivityFoldsOneClientAcrossAddresses(t *testing.T) {
	a := NewActivity()
	mac := "aa:bb:cc:dd:ee:ff"
	v4 := netip.MustParseAddr("192.168.1.2")
	v6 := netip.MustParseAddr("2001:db8::2")
	a.owners = map[netip.Addr]string{v4: mac, v6: mac}
	at := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	sample := func(up4, up6 uint64) []Usage {
		return []Usage{{Addr: v4, UpBytes: up4}, {Addr: v6, UpBytes: up6}}
	}
	a.observe(at, sample(100, 1000), true)
	a.observe(at.Add(time.Minute), sample(140, 1300), true)

	got := a.snapshot(mac, nil)
	if len(got) != 1 || got[0].Bytes.Up != 340 {
		t.Fatalf("a client's addresses were not one row: %+v", got)
	}
	if a.entries != 1 {
		t.Fatalf("one client spent %d entries on one bucket", a.entries)
	}
	// The addresses are not keys any more, so an address query finds nothing.
	// That is the point: the page asks by client.
	if got := a.snapshot("", []netip.Addr{v4, v6}); len(got) != 0 {
		t.Fatalf("history is still address-keyed: %+v", got)
	}
}

func TestActivityKeepsUnattributedAddressesQueryable(t *testing.T) {
	a := NewActivity()
	peer := netip.MustParseAddr("10.99.0.2")
	at := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	a.observe(at, []Usage{{Addr: peer, UpBytes: 0}}, true)
	a.observe(at.Add(time.Minute), []Usage{{Addr: peer, UpBytes: 70}}, true)

	if got := a.snapshot("aa:bb:cc:dd:ee:ff", nil); len(got) != 0 {
		t.Fatalf("a peer's traffic landed on a client: %+v", got)
	}
	if got := a.snapshot("", []netip.Addr{peer}); len(got) != 1 || got[0].Bytes.Up != 70 {
		t.Fatalf("unattributed traffic lost: %+v", got)
	}
}

func TestActivityAdoptFoldsStoredAddressHistory(t *testing.T) {
	dir := t.TempDir()
	store, err := runtimedb.Open(filepath.Join(dir, "runtime.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Now()
	bucket := now.Add(-time.Hour).Truncate(30 * time.Minute).Unix()
	addr := netip.MustParseAddr("192.168.1.2")
	if err := store.SaveActivity([]runtimedb.Activity{{Bucket: bucket, Subject: addr.String(), Up: 10, Down: 2}}, 0); err != nil {
		t.Fatal(err)
	}
	a, err := OpenActivityDB(store, filepath.Join(dir, "activity.json"))
	if err != nil {
		t.Fatal(err)
	}
	mac := "aa:bb:cc:dd:ee:ff"
	a.Owners = func(context.Context) map[netip.Addr]string {
		return map[netip.Addr]string{addr: mac}
	}
	if err := a.Adopt(context.Background()); err != nil {
		t.Fatal(err)
	}
	rows, err := store.Activity(0, now.Unix())
	if err != nil || len(rows) != 1 || rows[0].Subject != mac || rows[0].Up != 10 {
		t.Fatalf("stored history was not re-keyed: %+v %v", rows, err)
	}
	if got := a.snapshot(mac, nil); len(got) != 1 || got[0].Bytes.Up != 10 {
		t.Fatalf("folded history is not readable by client: %+v", got)
	}
	// Running it again must be a no-op rather than a rewrite.
	if err := a.Adopt(context.Background()); err != nil {
		t.Fatal(err)
	}
	if rows, err := store.Activity(0, now.Unix()); err != nil || len(rows) != 1 {
		t.Fatalf("second fold disturbed the store: %+v %v", rows, err)
	}
}

func TestActivityDropsTrafficNoScreenCanShow(t *testing.T) {
	a := NewActivity()
	peer := netip.MustParseAddr("10.6.0.2")
	router := netip.MustParseAddr("2001:db8::1")
	client := netip.MustParseAddr("192.168.1.2")
	mac := "aa:bb:cc:dd:ee:ff"
	a.owners = map[netip.Addr]string{client: mac}
	a.Names = func(context.Context) map[netip.Addr]bool {
		return map[netip.Addr]bool{peer: true}
	}
	a.named = a.Names(context.Background())
	at := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	sample := func(up uint64) []Usage {
		return []Usage{
			{Addr: client, UpBytes: up},
			{Addr: peer, UpBytes: up},
			{Addr: router, UpBytes: up},
		}
	}
	a.observe(at, sample(100), true)
	a.observe(at.Add(time.Minute), sample(140), true)

	if got := a.snapshot(mac, nil); len(got) != 1 || got[0].Bytes.Up != 40 {
		t.Fatalf("client traffic lost: %+v", got)
	}
	if got := a.snapshot("", []netip.Addr{peer}); len(got) != 1 || got[0].Bytes.Up != 40 {
		t.Fatalf("peer traffic lost: %+v", got)
	}
	if got := a.snapshot("", []netip.Addr{router}); len(got) != 0 {
		t.Fatalf("unshowable traffic was stored: %+v", got)
	}
	if a.entries != 2 {
		t.Fatalf("stored %d entries for two showable rows", a.entries)
	}
}

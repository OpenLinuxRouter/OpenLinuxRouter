package gateway

import (
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
	a.buckets[older] = map[netip.Addr]activityBytes{busy: {Up: 1}, addr: {Up: 1}}
	a.buckets[newer] = map[netip.Addr]activityBytes{busy: {Up: 1}}
	a.entries = 3

	a.observe(at, []Usage{{Addr: addr, UpBytes: 100}}, true)
	a.observe(at.Add(10*time.Second), []Usage{{Addr: addr, UpBytes: 150}}, true)

	if got := a.snapshot([]netip.Addr{addr}); len(got) != 1 || got[0].Bytes.Up != 50 {
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
	a.buckets[old] = map[netip.Addr]activityBytes{addr: {Up: 10, Down: 5}}
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
	if got := b.snapshot([]netip.Addr{addr}); len(got) != 1 || got[0].Bytes.Up != 50 {
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
	if got := c.snapshot([]netip.Addr{addr}); len(got) != 1 || got[0].Bytes.Up != 60 {
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
	a.buckets[time.Now().Add(-ActivityRetention-time.Hour).Unix()] = map[netip.Addr]activityBytes{addr: {Up: 10}}
	if err := a.Save(); err != nil {
		t.Fatal(err)
	}
	b, err := OpenActivity(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(b.snapshot([]netip.Addr{addr})) != 0 {
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
	a.buckets[time.Now().Unix()] = map[netip.Addr]activityBytes{addr: {Up: 10}}
	if err := a.Save(); err == nil {
		t.Fatal("write failure hidden")
	}
	if got := a.snapshot([]netip.Addr{addr}); len(got) != 1 || got[0].Bytes.Up != 10 {
		t.Fatal("failed save lost history")
	}
}

func TestActivityDifferencesAndGaps(t *testing.T) {
	a := NewActivity()
	at := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	addr := netip.MustParseAddr("192.168.1.2")
	sample := func(up uint64) []Usage { return []Usage{{Addr: addr, UpBytes: up}} }
	a.observe(at, sample(100), true)
	if len(a.snapshot([]netip.Addr{addr})) != 0 {
		t.Fatal("baseline is not activity")
	}
	a.observe(at.Add(time.Minute), sample(140), true)
	a.observe(at.Add(2*time.Minute), sample(10), true)
	a.observe(at.Add(3*time.Minute), sample(15), true)
	result := a.snapshot([]netip.Addr{addr})
	if len(result) != 1 || result[0].Bytes.Up != 45 {
		t.Fatalf("reset corrupted deltas: %+v", result)
	}
	a.observe(at.Add(10*time.Minute), sample(900), true)
	if a.snapshot([]netip.Addr{addr})[0].Bytes.Up != 45 {
		t.Fatal("gap counted as activity")
	}
	a.observe(at.Add(11*time.Minute), nil, false)
	a.observe(at.Add(12*time.Minute), sample(1000), true)
	if a.snapshot([]netip.Addr{addr})[0].Bytes.Up != 45 {
		t.Fatal("failed sample retained baseline")
	}
	a.observe(at.Add(ActivityRetention+time.Hour), sample(2000), true)
	if len(a.snapshot([]netip.Addr{addr})) != 0 {
		t.Fatal("history was not expired")
	}
}

func TestActivityShortSamplingInterval(t *testing.T) {
	a := NewActivity()
	at := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	addr := netip.MustParseAddr("192.168.1.2")
	a.observe(at, []Usage{{Addr: addr, UpBytes: 100}}, true)
	a.observe(at.Add(10*time.Second), []Usage{{Addr: addr, UpBytes: 150, DownBytes: 25}}, true)
	result := a.snapshot([]netip.Addr{addr})
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
	if len(a.snapshot([]netip.Addr{addr})) != 0 {
		t.Fatal("boundary interval was attributed to one bucket")
	}
	a.observe(at.Add(2*time.Minute), sample(200), true)
	result := a.snapshot([]netip.Addr{addr})
	if len(result) != 1 || result[0].Bytes.Up != 0 {
		t.Fatal("observed zero was lost")
	}
	a.observe(at.Add(3*time.Minute), sample(250), true)
	result = a.snapshot([]netip.Addr{addr})
	if result[0].Bytes.Up != 50 || result[0].Bytes.Down != 50 {
		t.Fatalf("exits were not aggregated: %+v", result)
	}
}

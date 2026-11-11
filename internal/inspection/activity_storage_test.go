package inspection

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"testing"
	"time"
)

const activityTestMAC = "aa:bb:cc:dd:ee:01"
const activityOtherMAC = "aa:bb:cc:dd:ee:02"

func durableActivity(t *testing.T, dir string, now *time.Time, flows *[]Flow) *Activity {
	t.Helper()
	a := NewActivity(func(context.Context) ([]Flow, error) { return *flows, nil }, nil)
	a.now = func() time.Time { return *now }
	if err := a.SetStorage(dir); err != nil {
		t.Fatal(err)
	}
	return a
}

func beginActivityDevice(t *testing.T, a *Activity, mac string, ip netip.Addr) {
	t.Helper()
	if err := a.BeginDevice(context.Background(), mac, []string{ip.String()}); err != nil {
		t.Fatal(err)
	}
}

func sampleActivity(t *testing.T, a *Activity) {
	t.Helper()
	if err := a.Sample(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func usageTotal(buckets []ActivityBucket) (up, down uint64) {
	for _, bucket := range buckets {
		for _, row := range bucket.Sites {
			up += row.Up
			down += row.Down
		}
	}
	return
}

func historyActivity(t *testing.T, a *Activity, mac string, window time.Duration) activityResponse {
	t.Helper()
	out, err := a.History(mac, window)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestActivityDurableRestartBaselinesAndRetainsHistory(t *testing.T) {
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	started := now
	ip := netip.MustParseAddr("172.16.1.92")
	flows := []Flow{{Src: ip, Dst: netip.MustParseAddr("203.0.113.9"), Sport: 1, Dport: 443, Proto: "tcp", BytesOut: 1000, BytesIn: 2000}}
	dir := t.TempDir()
	a := durableActivity(t, dir, &now, &flows)
	beginActivityDevice(t, a, activityTestMAC, ip)
	flows[0].BytesOut += 100
	flows[0].BytesIn += 200
	sampleActivity(t, a)
	sampleActivity(t, a)
	if err := a.Checkpoint(); err != nil {
		t.Fatal(err)
	}

	// Simulate a crash without End, then two hours of unobserved traffic.
	now = now.Add(2 * time.Hour)
	flows[0].BytesOut = 9000
	flows[0].BytesIn = 20000
	a = durableActivity(t, dir, &now, &flows)
	beginActivityDevice(t, a, "AA-BB-CC-DD-EE-01", ip)
	sampleActivity(t, a)
	if up, down := usageTotal(a.Snapshot([]netip.Addr{ip})); up != 100 || down != 200 {
		t.Fatalf("reload or baseline charged downtime: %d/%d", up, down)
	}
	flows[0].BytesOut += 10
	flows[0].BytesIn += 20
	sampleActivity(t, a)
	out := historyActivity(t, a, activityTestMAC, 4*time.Hour)
	if up, down := usageTotal(out.Buckets); up != 110 || down != 220 {
		t.Fatalf("restart total=%d/%d, want 110/220", up, down)
	}
	if len(out.Buckets) != 2 {
		t.Fatalf("fabricated downtime buckets: %d", len(out.Buckets))
	}
	if out.Since == nil || !out.Since.Equal(started) || !out.Recording {
		t.Fatalf("lost series metadata: %+v", out)
	}
	var meta activityMetadata
	if err := decodeActivityFile(filepath.Join(dir, activityTestMAC, "state.json"), &meta); err != nil {
		t.Fatal(err)
	}
	if !meta.BaselineAt.Equal(now) || !meta.SampledAt.Equal(now) {
		t.Fatalf("baseline/sample timestamps not checkpointed: %+v", meta)
	}

	// Restart again in the same minute: persisted minute totals must be merged,
	// and neither the hydrated ring nor the conntrack baseline may be replayed.
	a = durableActivity(t, dir, &now, &flows)
	beginActivityDevice(t, a, activityTestMAC, ip)
	flows[0].BytesOut += 5
	flows[0].BytesIn += 10
	sampleActivity(t, a)
	if up, down := usageTotal(historyActivity(t, a, activityTestMAC, 4*time.Hour).Buckets); up != 115 || down != 230 {
		t.Fatalf("same-minute resume total=%d/%d", up, down)
	}
}

func TestActivityDurableStopPreservesAndSeparatesMACs(t *testing.T) {
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	ip := netip.MustParseAddr("172.16.1.92")
	flows := []Flow{{Src: ip, Dst: netip.MustParseAddr("203.0.113.9"), Sport: 1, Dport: 443, Proto: "tcp"}}
	dir := t.TempDir()
	a := durableActivity(t, dir, &now, &flows)
	beginActivityDevice(t, a, activityTestMAC, ip)
	flows[0].BytesOut = 100
	sampleActivity(t, a)
	a.End()
	flows[0].BytesOut = 1000
	sampleActivity(t, a)
	if !a.Started().IsZero() {
		t.Fatal("End left sampling enabled")
	}
	out := historyActivity(t, a, activityTestMAC, 24*time.Hour)
	if up, _ := usageTotal(out.Buckets); up != 100 || out.Recording {
		t.Fatalf("stop erased history or kept recording: %+v", out)
	}

	// Reusing an IP cannot reuse another client's history.
	now = now.Add(time.Hour)
	beginActivityDevice(t, a, activityOtherMAC, ip)
	flows[0].BytesOut += 7
	sampleActivity(t, a)
	if up, _ := usageTotal(historyActivity(t, a, activityOtherMAC, 24*time.Hour).Buckets); up != 7 {
		t.Fatalf("other MAC total=%d", up)
	}
	if up, _ := usageTotal(historyActivity(t, a, activityTestMAC, 24*time.Hour).Buckets); up != 100 {
		t.Fatalf("first MAC total=%d", up)
	}
	a.End()
	a = durableActivity(t, dir, &now, &flows)
	if up, _ := usageTotal(historyActivity(t, a, activityTestMAC, 24*time.Hour).Buckets); up != 100 {
		t.Fatalf("stopped history lost after restart: %d", up)
	}
	beginActivityDevice(t, a, activityTestMAC, ip)
	sampleActivity(t, a)
	if up, _ := usageTotal(historyActivity(t, a, activityTestMAC, 24*time.Hour).Buckets); up != 100 {
		t.Fatalf("resume replayed other client: %d", up)
	}
}

func writeActivityFixture(t *testing.T, path string, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestActivityDurableRetentionAndBoundedRanges(t *testing.T) {
	now := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	deviceDir := filepath.Join(dir, activityTestMAC)
	if err := os.Mkdir(deviceDir, 0700); err != nil {
		t.Fatal(err)
	}
	for i := 0; i <= 31*24; i++ {
		at := now.Add(-time.Duration(i) * time.Hour)
		shard := activityShard{Version: 1, Buckets: []ActivityBucket{{At: at, Sites: []SiteUsage{{Site: fmt.Sprintf("site-%d.test", i), Up: 1, Down: 2}}}}}
		writeActivityFixture(t, filepath.Join(deviceDir, fmt.Sprintf("%d.json", at.Unix())), shard)
	}
	writeActivityFixture(t, filepath.Join(deviceDir, "state.json"), activityMetadata{Version: 1, Since: now.Add(-31 * 24 * time.Hour)})
	// A process dying before rename may leave a private temporary file; it is
	// ignored as history and removed during the next storage initialization.
	if err := os.WriteFile(filepath.Join(deviceDir, ".checkpoint-abandoned"), []byte("partial"), 0600); err != nil {
		t.Fatal(err)
	}
	var flows []Flow
	a := durableActivity(t, dir, &now, &flows)
	expired := filepath.Join(deviceDir, fmt.Sprintf("%d.json", now.Add(-31*24*time.Hour).Unix()))
	if _, err := os.Stat(expired); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expired hour kept: %v", err)
	}
	if _, err := os.Stat(filepath.Join(deviceDir, ".checkpoint-abandoned")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("abandoned checkpoint kept: %v", err)
	}
	for _, window := range []time.Duration{4 * time.Hour, 24 * time.Hour, 7 * 24 * time.Hour, activityRetention} {
		out := historyActivity(t, a, activityTestMAC, window)
		want := uint64(window/time.Hour) + 1
		if up, down := usageTotal(out.Buckets); up != want || down != want*2 {
			t.Errorf("%v total=%d/%d, want %d/%d", window, up, down, want, want*2)
		}
		if len(out.Buckets) > 241 {
			t.Errorf("%v returned %d display buckets", window, len(out.Buckets))
		}
		for _, bucket := range out.Buckets {
			if len(bucket.Sites) > activitySites {
				t.Fatalf("unbounded sites: %d", len(bucket.Sites))
			}
		}
		if out.WindowSec != int(window.Seconds()) || out.BucketSec < 60 {
			t.Fatalf("invalid dimensions: %+v", out)
		}
	}
	if _, err := a.History(activityTestMAC, 31*24*time.Hour); err == nil {
		t.Fatal("accepted range beyond retention")
	}
	ip := netip.MustParseAddr("172.16.1.92")
	beginActivityDevice(t, a, activityTestMAC, ip)
	if len(a.order) > activityBuckets || a.entries > activityEntries {
		t.Fatalf("loaded history without ring bounds: %d/%d", len(a.order), a.entries)
	}
	a.End()
	now = now.Add(31 * 24 * time.Hour)
	sampleActivity(t, a)
	if _, err := os.Stat(filepath.Join(deviceDir, fmt.Sprintf("%d.json", now.Add(-31*24*time.Hour).Unix()))); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("idle recorder retained expired history: %v", err)
	}
}

func TestActivityDurableGlobalByteBoundAndPrivateFiles(t *testing.T) {
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	ip := netip.MustParseAddr("172.16.1.92")
	flows := []Flow{{Src: ip, Dst: netip.MustParseAddr("203.0.113.9"), Proto: "tcp"}}
	dir := t.TempDir()
	a := durableActivity(t, dir, &now, &flows)
	a.storageLimit = 1024
	for i := 0; i < 16; i++ {
		mac := activityTestMAC
		if i%2 != 0 {
			mac = activityOtherMAC
		}
		beginActivityDevice(t, a, mac, ip)
		flows[0].BytesOut++
		sampleActivity(t, a)
		a.End()
		now = now.Add(time.Hour)
	}
	var total int64
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if d.IsDir() {
			if info.Mode().Perm() != 0700 {
				t.Errorf("directory %s mode=%o", path, info.Mode().Perm())
			}
			return nil
		}
		total += info.Size()
		if info.Mode().Perm() != 0600 {
			t.Errorf("file %s mode=%o", path, info.Mode().Perm())
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if total > a.storageLimit {
		t.Fatalf("archive size %d exceeds %d", total, a.storageLimit)
	}
	if len(historyActivity(t, a, activityTestMAC, 24*time.Hour).Buckets) >= 8 {
		t.Fatal("quota never evicted oldest history")
	}
}

func TestActivityCorruptCheckpointReportsAndRetriesWithoutReplay(t *testing.T) {
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	ip := netip.MustParseAddr("172.16.1.92")
	flows := []Flow{{Src: ip, Dst: netip.MustParseAddr("203.0.113.9"), Proto: "tcp"}}
	dir := t.TempDir()
	a := durableActivity(t, dir, &now, &flows)
	beginActivityDevice(t, a, activityTestMAC, ip)
	flows[0].BytesOut = 100
	sampleActivity(t, a)
	path := a.shardPath(activityTestMAC, now.Truncate(time.Hour).Unix())
	saved, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, []byte("{partial"), 0600); err != nil {
		t.Fatal(err)
	}
	flows[0].BytesOut = 150
	if err = a.Sample(context.Background()); err == nil {
		t.Fatal("corrupt checkpoint silently replaced")
	}
	if a.Problem() == "" {
		t.Fatal("checkpoint failure missing from status")
	}
	if _, err = a.History(activityTestMAC, 4*time.Hour); err == nil {
		t.Fatal("corrupt history silently omitted")
	}
	fresh := durableActivity(t, dir, &now, &flows)
	if err = fresh.BeginDevice(context.Background(), activityTestMAC, []string{ip.String()}); err == nil {
		t.Fatal("corrupt archive accepted on resume")
	}
	a.End()
	if !a.Started().IsZero() {
		t.Fatal("failed final checkpoint left recording enabled")
	}
	if err = a.Checkpoint(); err == nil {
		t.Fatal("failed End lost checkpoint error")
	}
	// Restore the known-good committed file; the retained pending delta is
	// committed exactly once, even if Checkpoint is called repeatedly.
	if err = os.WriteFile(path, saved, 0600); err != nil {
		t.Fatal(err)
	}
	if err = a.Checkpoint(); err != nil {
		t.Fatal(err)
	}
	if err = a.Checkpoint(); err != nil {
		t.Fatal(err)
	}
	if a.Problem() != "" {
		t.Fatalf("retry left stale error: %s", a.Problem())
	}
	if up, _ := usageTotal(historyActivity(t, a, activityTestMAC, 4*time.Hour).Buckets); up != 150 {
		t.Fatalf("retry lost or replayed delta: %d", up)
	}
}

func TestActivityCorruptMetadataAndInvalidFiles(t *testing.T) {
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	var flows []Flow
	dir := t.TempDir()
	a := durableActivity(t, dir, &now, &flows)
	ip := netip.MustParseAddr("172.16.1.92")
	beginActivityDevice(t, a, activityTestMAC, ip)
	metaPath := filepath.Join(dir, activityTestMAC, "state.json")
	if err := os.WriteFile(metaPath, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := a.Checkpoint(); err == nil {
		t.Fatal("invalid metadata overwritten without error")
	}
	if _, err := a.History(activityTestMAC, 4*time.Hour); err == nil {
		t.Fatal("invalid metadata accepted")
	}
	var nilActivity *Activity
	if err := nilActivity.Checkpoint(); err != nil {
		t.Fatal(err)
	}
	if err := nilActivity.BeginDevice(context.Background(), activityTestMAC, nil); err != nil {
		t.Fatal(err)
	}
	if nilActivity.Problem() != "" {
		t.Fatal("nil recorder has a problem")
	}
}

func TestActivityHTTPMACRangesAndLegacyAddresses(t *testing.T) {
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	ip := netip.MustParseAddr("172.16.1.92")
	flows := []Flow{{Src: ip, Dst: netip.MustParseAddr("203.0.113.9"), Proto: "tcp"}}
	a := durableActivity(t, t.TempDir(), &now, &flows)
	beginActivityDevice(t, a, activityTestMAC, ip)
	flows[0].BytesOut = 100
	sampleActivity(t, a)
	s := &Service{Activity: a}
	for _, query := range []string{"mac=" + activityTestMAC, "mac=" + activityTestMAC + "&range=24h", "mac=" + activityTestMAC + "&range=7d", "mac=" + activityTestMAC + "&range=30d", "address=" + ip.String()} {
		rec := httptest.NewRecorder()
		s.activity(rec, httptest.NewRequest(http.MethodGet, "/activity?"+query, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status %d: %s", query, rec.Code, rec.Body.String())
		}
		var out activityResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		if up, _ := usageTotal(out.Buckets); up != 100 || !out.Recording {
			t.Fatalf("%s response=%+v", query, out)
		}
	}
	for _, query := range []string{"mac=../../escape", "mac=" + activityTestMAC + "&range=31d", "mac=" + activityTestMAC + "&range=0h", "mac=" + activityTestMAC + "&range=5s", "address=" + ip.String() + "&range=7d", "address=bad", ""} {
		rec := httptest.NewRecorder()
		s.activity(rec, httptest.NewRequest(http.MethodGet, "/activity?"+query, nil))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s status=%d", query, rec.Code)
		}
	}
	a.End()
	rec := httptest.NewRecorder()
	s.activity(rec, httptest.NewRequest(http.MethodGet, "/activity?mac="+activityTestMAC+"&range=30d", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("stopped history status=%d", rec.Code)
	}
	var out activityResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if up, _ := usageTotal(out.Buckets); up != 100 || out.Recording {
		t.Fatalf("stopped history=%+v", out)
	}
}

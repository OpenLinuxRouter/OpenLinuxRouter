package inspection

import (
	"context"
	"net/netip"
	"testing"
	"time"
)

func TestSiteOfReducesToRegistrableDomain(t *testing.T) {
	cases := map[string]string{
		"rr1---sn-4g5e6nez.googlevideo.com":    "googlevideo.com",
		"API.Bilibili.com.":                    "bilibili.com",
		"apd-pcdnwxstat.teg.tencent-cloud.net": "tencent-cloud.net",
		// A multi-label suffix is not a site: the list decides which labels are
		// the suffix, so these do not reduce to com.cn or co.uk.
		"static.qq.com.cn": "qq.com.cn",
		"www.bbc.co.uk":    "bbc.co.uk",
		// Private suffixes name an object store, not a site.
		"a.b.github.io":           "github.io",
		"bucket.s3.amazonaws.com": "amazonaws.com",
		// An unknown namespace has no list entry; two labels is the fallback.
		"host.internal": "host.internal",
		"localhost":     "localhost",
		"":              unnamedSite,
	}
	for name, want := range cases {
		if got := siteOf(name); got != want {
			t.Errorf("siteOf(%q) = %q, want %q", name, got, want)
		}
	}
}

// A client's flow lands under the name the relay saw for that address, and an
// unresolved one lands under 未命名 rather than disappearing.
func TestSampleAttributesAndKeepsUnnamed(t *testing.T) {
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	client := netip.MustParseAddr("172.16.1.92")
	video := netip.MustParseAddr("203.0.113.9")
	other := netip.MustParseAddr("203.0.113.10")

	flows := []Flow{
		{Src: client, Dst: video, Sport: 1, Dport: 443, Proto: "tcp"},
		{Src: client, Dst: other, Sport: 2, Dport: 443, Proto: "tcp"},
		// LAN-to-LAN and router-originated traffic never belong to a client's chart.
		{Src: client, Dst: netip.MustParseAddr("172.16.1.1"), Sport: 3, Dport: 80, Proto: "tcp", BytesOut: 5, BytesIn: 5},
	}
	a := NewActivity(
		func(context.Context) ([]Flow, error) { return flows, nil },
		func(context.Context) ([]Name, error) {
			return []Name{{Client: client, Addr: video, Name: "rr1---sn-4g5e6nez.googlevideo.com"}}, nil
		},
	)
	a.now = func() time.Time { return now }
	a.Begin(context.Background(), []string{client.String()})
	flows[0].BytesOut, flows[0].BytesIn = 100, 1000
	flows[1].BytesOut, flows[1].BytesIn = 10, 100
	if err := a.Sample(context.Background()); err != nil {
		t.Fatal(err)
	}

	buckets := a.Snapshot([]netip.Addr{client})
	if len(buckets) != 1 {
		t.Fatalf("got %d buckets, want 1", len(buckets))
	}
	got := map[string][2]uint64{}
	for _, row := range buckets[0].Sites {
		got[row.Site] = [2]uint64{row.Up, row.Down}
	}
	if want := [2]uint64{100, 1000}; got["googlevideo.com"] != want {
		t.Errorf("googlevideo.com = %v, want %v", got["googlevideo.com"], want)
	}
	if want := [2]uint64{10, 100}; got[unnamedSite] != want {
		t.Errorf("%s = %v, want %v", unnamedSite, got[unnamedSite], want)
	}
	if len(got) != 2 {
		t.Errorf("LAN traffic leaked into the chart: %v", got)
	}
}

// The counters already on the table when the session starts are history, not
// traffic: only what grew after Begin belongs to the session.
func TestBeginBaselinesTheTable(t *testing.T) {
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	client := netip.MustParseAddr("172.16.1.92")
	remote := netip.MustParseAddr("203.0.113.9")
	flows := []Flow{{Src: client, Dst: remote, Sport: 1, Dport: 443, Proto: "tcp", BytesOut: 5000, BytesIn: 90000}}
	a := NewActivity(func(context.Context) ([]Flow, error) { return flows, nil }, nil)
	a.now = func() time.Time { return now }
	a.Begin(context.Background(), []string{client.String()})
	_ = a.Sample(context.Background())
	if buckets := a.Snapshot([]netip.Addr{client}); len(buckets) != 0 {
		t.Errorf("charged the session with pre-session bytes: %d buckets", len(buckets))
	}

	flows[0].BytesOut, flows[0].BytesIn = 5100, 90200
	_ = a.Sample(context.Background())
	rows := a.Snapshot([]netip.Addr{client})[0].Sites
	if rows[0].Up != 100 || rows[0].Down != 200 {
		t.Errorf("got %d/%d, want 100/200", rows[0].Up, rows[0].Down)
	}
}

// Reading the same counters twice adds nothing the second time.
func TestSampleCountsOnlyTheDelta(t *testing.T) {
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	client := netip.MustParseAddr("172.16.1.92")
	remote := netip.MustParseAddr("203.0.113.9")
	flows := []Flow{{Src: client, Dst: remote, Sport: 1, Dport: 443, Proto: "tcp"}}
	a := NewActivity(func(context.Context) ([]Flow, error) { return flows, nil }, nil)
	a.now = func() time.Time { return now }
	a.Begin(context.Background(), []string{client.String()})
	flows[0].BytesOut, flows[0].BytesIn = 300, 5000
	_ = a.Sample(context.Background())

	rows := a.Snapshot([]netip.Addr{client})[0].Sites
	if rows[0].Up != 300 || rows[0].Down != 5000 {
		t.Errorf("got %d/%d, want 300/5000", rows[0].Up, rows[0].Down)
	}
}

// The series belongs to the session: sampling before Begin records nothing, and
// End drops what the session saw so turning inspection off leaves no history.
func TestActivityRecordsOnlyWhileSessionRuns(t *testing.T) {
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	client := netip.MustParseAddr("172.16.1.92")
	remote := netip.MustParseAddr("203.0.113.9")
	flows := []Flow{{Src: client, Dst: remote, Sport: 1, Dport: 443, Proto: "tcp"}}
	a := NewActivity(func(context.Context) ([]Flow, error) { return flows, nil }, nil)
	a.now = func() time.Time { return now }

	if err := a.Sample(context.Background()); err != nil {
		t.Fatal(err)
	}
	if buckets := a.Snapshot([]netip.Addr{client}); len(buckets) != 0 {
		t.Errorf("recorded %d buckets before the session began", len(buckets))
	}

	a.Begin(context.Background(), []string{client.String()})
	flows[0].BytesOut, flows[0].BytesIn = 200, 2000
	_ = a.Sample(context.Background())
	if buckets := a.Snapshot([]netip.Addr{client}); len(buckets) != 1 {
		t.Fatalf("got %d buckets during the session, want 1", len(buckets))
	}

	a.End()
	if buckets := a.Snapshot([]netip.Addr{client}); len(buckets) != 0 {
		t.Errorf("End kept %d buckets", len(buckets))
	}
	flows[0].BytesOut, flows[0].BytesIn = 400, 4000
	_ = a.Sample(context.Background())
	if buckets := a.Snapshot([]netip.Addr{client}); len(buckets) != 0 {
		t.Errorf("recorded %d buckets after the session ended", len(buckets))
	}
}

// Traffic from an address the session does not inspect is not the chart's data.
func TestSampleIgnoresAddressesOutsideTheSession(t *testing.T) {
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	watched := netip.MustParseAddr("172.16.1.92")
	unwatched := netip.MustParseAddr("172.16.1.93")
	remote := netip.MustParseAddr("203.0.113.9")
	flows := []Flow{
		{Src: watched, Dst: remote, Sport: 1, Dport: 443, Proto: "tcp"},
		{Src: unwatched, Dst: remote, Sport: 2, Dport: 443, Proto: "tcp", BytesOut: 700, BytesIn: 7000},
	}
	a := NewActivity(func(context.Context) ([]Flow, error) { return flows, nil }, nil)
	a.now = func() time.Time { return now }
	a.Begin(context.Background(), []string{watched.String()})
	flows[0].BytesOut, flows[0].BytesIn = 100, 1000
	_ = a.Sample(context.Background())

	if got := a.Snapshot([]netip.Addr{unwatched}); len(got) != 0 {
		t.Errorf("recorded another address's traffic: %d buckets", len(got))
	}
	if got := a.Snapshot([]netip.Addr{watched}); len(got) != 1 {
		t.Fatalf("got %d buckets for the watched address, want 1", len(got))
	}
}

// The chart's axis grows with the switch and stops at the ring: a short
// inspection stays readable, a long one scrolls instead of promising memory
// that was pruned.
func TestActivityWindowGrowsAndCaps(t *testing.T) {
	cases := map[time.Duration]time.Duration{
		0:                   15 * time.Minute,
		2 * time.Minute:     15 * time.Minute,
		15 * time.Minute:    15 * time.Minute,
		61 * time.Minute:    61 * time.Minute,
		3 * time.Hour:       3 * time.Hour,
		5 * time.Hour:       activityBuckets * activityWidth,
		30 * 24 * time.Hour: activityBuckets * activityWidth,
	}
	for elapsed, want := range cases {
		if got := activityWindow(elapsed); got != want {
			t.Errorf("activityWindow(%v) = %v, want %v", elapsed, got, want)
		}
	}
}

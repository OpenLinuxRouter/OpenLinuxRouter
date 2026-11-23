package gateway

import (
	"encoding/json"
	"net/netip"
	"testing"
	"time"
)

// capacityTime is a fixed start so a test asserts on rates and coverage
// rather than on the clock.
func capacityTime() time.Time {
	return time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC)
}

func capacityUsage(down, up uint64) []Usage {
	return []Usage{{Addr: netip.MustParseAddr("192.168.1.20"), DownBytes: down, UpBytes: up}}
}

// One row, sampled every ten seconds. The counters are cumulative, exactly
// what Traffic returns; a blank step repeats the previous reading.
type capacityStep struct {
	at       time.Duration
	down, up uint64
}

func feedCapacity(c *Capacity, start time.Time, steps []capacityStep) {
	for _, step := range steps {
		c.observe(start.Add(step.at), capacityUsage(step.down, step.up), true)
	}
}

// The minute keeps the maximum, not the mean: a capacity baseline exists to
// remember the burst, and averaging the six readings in a minute erases it.
func TestCapacityKeepsTheBurstNotTheMean(t *testing.T) {
	c := NewCapacity()
	start := capacityTime()
	feedCapacity(c, start, []capacityStep{
		{at: 0, down: 0, up: 0},
		{at: 10 * time.Second, down: 1000, up: 100},   // 800 / 80 bps
		{at: 20 * time.Second, down: 3000, up: 300},   // 1600 / 160 bps
		{at: 30 * time.Second, down: 3500, up: 350},   // 400 / 40 bps
		{at: 40 * time.Second, down: 8500, up: 4000},  // 4000 / 2920 bps, the burst
		{at: 50 * time.Second, down: 9000, up: 4100},  // 400 / 80 bps
		{at: 60 * time.Second, down: 10000, up: 4200}, // next minute: 800 / 80 bps
	})

	v := c.View(start.Add(time.Minute))
	if v.State != capacityWarming {
		t.Errorf("state = %q with two minutes, want warming", v.State)
	}
	if v.RX.Points != 2 || v.TX.Points != 2 {
		t.Fatalf("points = %d/%d, want 2/2: %+v", v.RX.Points, v.TX.Points, v)
	}
	if v.RX.PeakBps != 4000 || v.RX.P99Bps != 4000 {
		t.Errorf("rx peak/p99 = %d/%d, want 4000/4000 (two positive minutes)", v.RX.PeakBps, v.RX.P99Bps)
	}
	if v.TX.PeakBps != 2920 || v.TX.P99Bps != 2920 {
		t.Errorf("tx peak/p99 = %d/%d, want 2920/2920", v.TX.PeakBps, v.TX.P99Bps)
	}
	if got := v.RX.Since; got == nil || got.Unix() != start.Truncate(time.Minute).Unix() {
		t.Errorf("since = %v, want the first retained minute", got)
	}
}

// The 99th percentile is the nearest rank: on a short series it equals the
// peak, and on a long one it leaves the top 1% out rather than interpolating
// a reading the box never took.
func TestPercentile99IsNearestRank(t *testing.T) {
	for _, tc := range []struct {
		name   string
		values []int64
		want   int64
	}{
		{"empty", nil, 0},
		{"one", []int64{7}, 7},
		{"two", []int64{7, 9}, 9},
		{"thirty takes the maximum", series(1, 30), 30},
		{"hundred takes the ninety-ninth", series(1, 100), 99},
		{"thousand takes the nine-hundred-ninetieth", series(1, 1000), 990},
		{"the top one percent is left out", append(series(1, 100), 1000), 100},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := percentile99(tc.values); got != tc.want {
				t.Errorf("percentile99(%v) = %d, want %d", tc.values, got, tc.want)
			}
		})
	}
}

func series(from, to int64) []int64 {
	out := make([]int64, 0, to-from+1)
	for v := from; v <= to; v++ {
		out = append(out, v)
	}
	return out
}

// Zero-demand minutes count as coverage but not as evidence of capacity: a
// network that sleeps must not report a p99 of zero.
func TestCapacityIgnoresIdleMinutesInTheDistribution(t *testing.T) {
	c := NewCapacity()
	start := capacityTime()
	// One burst, then two and a half minutes of a flat counter. Every one of
	// the three minutes is sampled and counted; only the burst is capacity.
	var down uint64
	steps := []capacityStep{{at: 0, down: down}}
	for at := 10 * time.Second; at < 3*time.Minute; at += 10 * time.Second {
		if at == 10*time.Second {
			down = 5000 // +5000 bytes over ten seconds, 4000 bps
		}
		steps = append(steps, capacityStep{at: at, down: down})
	}
	feedCapacity(c, start, steps)

	v := c.View(start.Add(3 * time.Minute))
	if v.RX.Points != 3 {
		t.Fatalf("points = %d, want every sampled minute counted", v.RX.Points)
	}
	if v.RX.PeakBps != 4000 || v.RX.P99Bps != 4000 {
		t.Errorf("rx peak/p99 = %d/%d, want the one positive minute", v.RX.PeakBps, v.RX.P99Bps)
	}
}

// A sample the router could not take is not a zero. It drops the baseline, so
// the recovery sample measures from itself forward rather than billing the
// outage as traffic.
func TestCapacitySkipsUnreadableSamplesWithoutPhantomZeroes(t *testing.T) {
	c := NewCapacity()
	at := capacityTime()

	c.observe(at, capacityUsage(1000, 100), true)
	c.observe(at.Add(10*time.Second), nil, false)
	c.observe(at.Add(20*time.Second), capacityUsage(9000, 900), true)
	if got := c.View(at.Add(20 * time.Second)); got.RX.Points != 0 {
		t.Fatalf("an unreadable sample was recorded: %+v", got)
	}
	c.observe(at.Add(30*time.Second), capacityUsage(11000, 1100), true)

	v := c.View(at.Add(30 * time.Second))
	if v.RX.Points != 1 || v.TX.Points != 1 {
		t.Fatalf("points = %d/%d, want only the sample after the gap", v.RX.Points, v.TX.Points)
	}
	// 2000 bytes and 200 bytes over ten seconds.
	if v.RX.PeakBps != 1600 || v.TX.PeakBps != 160 {
		t.Errorf("rx/tx = %d/%d, want 1600/160; the outage was counted as traffic",
			v.RX.PeakBps, v.TX.PeakBps)
	}
}

// The ring is the bound: the oldest minute goes, the window shortens, and
// since moves with it instead of pretending the full retention is covered.
func TestCapacityRingIsBoundedAndSinceMoves(t *testing.T) {
	c := &Capacity{ring: make([]capacityMinute, 3)}
	start := capacityTime()
	c.recordLocked(start, 100, 10)
	c.recordLocked(start.Add(time.Minute), 200, 20)
	c.recordLocked(start.Add(2*time.Minute), 300, 30)

	v := c.View(start.Add(3 * time.Minute))
	if v.RX.Points != 3 || v.RX.PeakBps != 300 {
		t.Fatalf("three retained minutes: %+v", v.RX)
	}
	if got, want := v.RX.Since.Unix(), start.Unix(); got != want {
		t.Errorf("since = %d, want %d", got, want)
	}

	c.recordLocked(start.Add(3*time.Minute), 50, 5)
	v = c.View(start.Add(4 * time.Minute))
	if v.RX.Points != 3 {
		t.Errorf("points = %d after the ring wrapped, want 3", v.RX.Points)
	}
	if v.RX.PeakBps != 300 {
		t.Errorf("peak = %d, want 300; the oldest minute's 100 should have gone, not the peak", v.RX.PeakBps)
	}
	if got, want := v.RX.Since.Unix(), start.Add(time.Minute).Unix(); got != want {
		t.Errorf("since = %d, want the second minute %d", got, want)
	}
}

// Thirty minutes is the point where a p99 has a tail to speak of; before
// that the answer says it is still warming rather than presenting six
// minutes as a measured capacity.
func TestCapacityWarmsAtThirtyMinutes(t *testing.T) {
	c := &Capacity{ring: make([]capacityMinute, 60)}
	start := capacityTime()
	for i := 0; i < capacityWarmingPoints-1; i++ {
		c.recordLocked(start.Add(time.Duration(i)*time.Minute), 1000, 100)
	}
	if got := c.View(start.Add((capacityWarmingPoints - 1) * time.Minute)); got.State != capacityWarming {
		t.Errorf("state = %q with %d minutes, want warming", got.State, capacityWarmingPoints-1)
	}

	c.recordLocked(start.Add(capacityWarmingPoints*time.Minute), 1000, 100)
	if got := c.View(start.Add(capacityWarmingPoints * time.Minute)); got.State != capacityMeasured {
		t.Errorf("state = %q with %d minutes, want measured", got.State, capacityWarmingPoints)
	}
}

// A different uplink is a different link. Keeping the old peak would attribute
// one interface's capacity to another.
func TestCapacityResetsWhenTheUplinkChanges(t *testing.T) {
	c := NewCapacity()
	at := capacityTime()
	c.SetUplink("ens18")
	c.recordLocked(at, 1000, 100)
	if got := c.View(at); got.Uplink != "ens18" || got.RX.Points != 1 {
		t.Fatalf("first uplink: %+v", got)
	}

	c.SetUplink("ens19")
	got := c.View(at)
	if got.Uplink != "ens19" || got.RX.Points != 0 || got.RX.Since != nil {
		t.Fatalf("history survived an uplink change: %+v", got)
	}
}

// The wire shape is the contract the page and the tool are written against,
// so the names are asserted rather than left to a struct rename.
func TestCapacityViewJSONShape(t *testing.T) {
	c := NewCapacity()
	at := capacityTime()
	c.SetUplink("ens18")
	c.recordLocked(at, 1500, 250)
	c.recordLocked(at.Add(time.Minute), 1000, 200)
	c.recordLocked(at.Add(2*time.Minute), 1200, 300)

	encoded, err := json.Marshal(c.View(at.Add(3 * time.Minute)))
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(encoded, &got); err != nil {
		t.Fatal(err)
	}
	if got["uplink"] != "ens18" || got["state"] != "warming" {
		t.Fatalf("top-level fields: %s", encoded)
	}
	if _, ok := got["as_of"].(string); !ok {
		t.Fatalf("as_of is not a timestamp: %s", encoded)
	}
	for _, direction := range []string{"rx", "tx"} {
		object, ok := got[direction].(map[string]any)
		if !ok {
			t.Fatalf("%s is not an object: %s", direction, encoded)
		}
		for _, field := range []string{"p99_bps", "peak_bps", "points", "since"} {
			if _, ok := object[field]; !ok {
				t.Errorf("%s.%s is missing: %s", direction, field, encoded)
			}
		}
	}
}

func TestCapacityViewCarriesTheMeasuredValues(t *testing.T) {
	c := NewCapacity()
	at := capacityTime()
	c.SetUplink("ens18")
	c.recordLocked(at, 1_430_000_000, 210_000_000)
	c.recordLocked(at.Add(time.Minute), 1_500_000_000, 260_000_000)

	v := c.View(at.Add(2 * time.Minute))
	if v.RX.PeakBps != 1_500_000_000 || v.RX.P99Bps != 1_500_000_000 {
		t.Errorf("rx = %+v", v.RX)
	}
	if v.TX.PeakBps != 260_000_000 || v.TX.P99Bps != 260_000_000 {
		t.Errorf("tx = %+v", v.TX)
	}
}

// The name and the read-only-ness are the contract MCP derives the tool from,
// and the page calls the same route. Freeze both here rather than relying on a
// review to notice a rename.
func TestCapacityRouteIsAReadOnlyTool(t *testing.T) {
	for _, route := range (HTTP{}).Routes() {
		if route.Path != "/capacity" {
			continue
		}
		if route.Method != "GET" || route.Mutating {
			t.Errorf("capacity route is %s mutating=%v, want a GET read", route.Method, route.Mutating)
		}
		if route.Tool != "show capacity" {
			t.Errorf("capacity tool = %q, want %q", route.Tool, "show capacity")
		}
		if route.Handler == nil {
			t.Error("capacity route has no handler")
		}
		return
	}
	t.Fatal("gateway does not serve /capacity")
}

// A gap longer than the window does not turn old minutes into current ones:
// the ring has not overwritten them yet, but they are outside the retention
// and must not inflate points or drag since backwards.
func TestCapacityExpiresMinutesOutsideTheWindow(t *testing.T) {
	c := &Capacity{ring: make([]capacityMinute, 8)}
	start := capacityTime()
	c.recordLocked(start, 1000, 100)
	later := start.Add(31 * 24 * time.Hour)
	c.recordLocked(later, 2000, 200)

	v := c.View(later)
	if v.RX.Points != 1 {
		t.Errorf("points = %d, want only the minute inside the window", v.RX.Points)
	}
	if v.RX.Since == nil || v.RX.Since.Unix() != later.Truncate(time.Minute).Unix() {
		t.Errorf("since = %v, want the recent minute", v.RX.Since)
	}
	if v.RX.PeakBps != 2000 {
		t.Errorf("peak = %d, want the retained minute's 2000", v.RX.PeakBps)
	}
}

package gateway

import (
	"sort"
	"sync"
	"time"
)

// capacityDays and capacityMinutes are how far back the capacity sampler's
// one-minute peaks are kept.
//
// Thirty days at a minute apiece is 43,200 readings per direction, about
// 1 MiB for the ring — small enough to hold in memory and long enough that
// "the peak this month" is a fact about a month rather than about the last
// time somebody opened the page. The series is deliberately not persisted:
// after a restart the sampler starts again, which is why the API publishes
// points and since instead of claiming the full window.
const (
	capacityDays          = 30
	capacityMinutes       = capacityDays * 24 * 60
	capacityWarmingPoints = 30
)

// capacityInterval is the traffic sampler's cadence. A sample further away
// than two of these is a gap, not a slow sample: the counters kept moving
// while nothing was reading them, and dividing that movement by the gap would
// turn one outage into a burst of whatever the house did in the meantime.
const capacityInterval = 10 * time.Second

const (
	capacityWarming  = "warming"
	capacityMeasured = "measured"
)

// Capacity is the throughput the box has demonstrated for its clients: for
// each direction, the highest ten-second rate seen in each minute, retained
// for thirty days and reduced on read to a peak and a p99.
//
// It answers a different question from the activity sampler beside it. That
// one records per-client volumes for ninety days; this one records the
// aggregate rate the box actually moved, which is what a performance page
// needs as a denominator. It is fed the same numbers from the same ten-second
// walk, so it costs one pass over the usage rows and no extra kernel read.
type Capacity struct {
	mu sync.Mutex

	// uplink names the interface the retained history belongs to. SetUplink
	// discards the history when the name changes, because a peak measured on
	// the old way out is not a fact about the new one.
	uplink string

	// previous is the last sample's counters, keyed as the activity sampler
	// keys its rows so that a client's two exits stay two series. A missing
	// key is new traffic and is not counted; a falling counter is a reset and
	// is skipped rather than becoming an underflow.
	previous map[string]capacitySample
	last     time.Time

	// ring is a ring of one-minute peaks, oldest at next once it is full. Its
	// length is the retention, so a test can build a short one instead of
	// forty-three thousand minutes of history.
	ring  []capacityMinute
	next  int
	count int
}

type capacitySample struct {
	up   uint64
	down uint64
}

type capacityMinute struct {
	// at is the Unix minute the readings belong to.
	at int64
	// rx and tx are bits per second, down and up respectively: the
	// direction names follow the interface counters, where rx is what the
	// box received from the internet.
	rx int64
	tx int64
}

// capacityView is the measured baseline as the API and MCP publish it.
//
// Points and Since are repeated per direction even though both are recorded
// together: a client should be able to read one direction's object without
// assuming the other's window, and a future sampler that drops a direction
// would otherwise silently change the other's meaning.
type capacityView struct {
	Uplink string                `json:"uplink"`
	RX     capacityDirectionView `json:"rx"`
	TX     capacityDirectionView `json:"tx"`

	// State is warming until there are enough minutes to be meaningful. A
	// fresh process is honest about that rather than presenting six minutes
	// as a measured capacity.
	State string    `json:"state"`
	AsOf  time.Time `json:"as_of"`
}

type capacityDirectionView struct {
	P99Bps  int64 `json:"p99_bps"`
	PeakBps int64 `json:"peak_bps"`

	// Points is the number of one-minute buckets retained, and Since is the
	// oldest of them. Together they say how much history the two rates above
	// actually rest on, so a page can say "based on 6 hours" instead of
	// "30 days".
	Points int        `json:"points"`
	Since  *time.Time `json:"since,omitempty"`
}

// NewCapacity builds a sampler with the full thirty-day ring.
func NewCapacity() *Capacity {
	return &Capacity{ring: make([]capacityMinute, capacityMinutes)}
}

// SetUplink records which interface this history measures, discarding it when
// the interface changes.
//
// Read from the route rather than every sample so that the sampler itself
// needs no window onto `dial`: the name is only needed when somebody asks,
// and a read of the stored configuration every ten seconds would buy nothing
// the answer does not already carry.
func (c *Capacity) SetUplink(name string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if name == c.uplink {
		return
	}
	c.uplink = name
	c.previous = nil
	c.last = time.Time{}
	c.next, c.count = 0, 0
}

// observe folds one ten-second traffic sample into the minute being built.
//
// valid is false when the configuration or the kernel could not be read. The
// baseline is dropped rather than differenced, so the recovery sample is a
// new baseline instead of one enormous burst covering the outage. No bucket
// is written for such a sample at all: a zero there would be a reading the
// router never took.
func (c *Capacity) observe(now time.Time, usage []Usage, valid bool) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	if !valid {
		c.previous = nil
		c.last = time.Time{}
		return
	}

	next := make(map[string]capacitySample, len(usage))
	for _, u := range usage {
		next[capacityKey(u)] = capacitySample{up: u.UpBytes, down: u.DownBytes}
	}
	elapsed := now.Sub(c.last)
	if c.last.IsZero() || elapsed <= 0 || elapsed > 2*capacityInterval {
		c.previous, c.last = next, now
		return
	}

	// Sum the per-row differences rather than differencing a grand total: a
	// client that appears between samples arrives with its lifetime counters,
	// and a total diff would credit all of that lifetime to this ten seconds.
	var up, down uint64
	for key, current := range next {
		old, seen := c.previous[key]
		if !seen || current.up < old.up || current.down < old.down {
			continue
		}
		up += current.up - old.up
		down += current.down - old.down
	}
	c.previous, c.last = next, now

	seconds := elapsed.Seconds()
	c.recordLocked(now, bitsPerSecond(down, seconds), bitsPerSecond(up, seconds))
}

func capacityKey(u Usage) string {
	return u.Addr.String() + "\x00" + u.Exit
}

// bitsPerSecond is a byte count over an elapsed interval. The interval is
// the one the sampler actually observed, not a nominal ten seconds: a tick
// that arrived late measured a longer interval and must say so, or the rate
// would be inflated by exactly the delay.
func bitsPerSecond(bytes uint64, seconds float64) int64 {
	if bytes == 0 || seconds <= 0 {
		return 0
	}
	return int64(float64(bytes) * 8 / seconds)
}

// recordLocked keeps the maximum rate seen in each minute, not the mean. A
// short burst is what a capacity baseline is trying to capture; averaging it
// over the minute would erase the only evidence of what the link can do.
//
// The caller holds the lock.
func (c *Capacity) recordLocked(now time.Time, rx, tx int64) {
	if len(c.ring) == 0 {
		c.ring = make([]capacityMinute, capacityMinutes)
	}
	minute := now.Unix() / 60
	if c.count > 0 {
		last := &c.ring[(c.next+len(c.ring)-1)%len(c.ring)]
		switch {
		case minute == last.at:
			if rx > last.rx {
				last.rx = rx
			}
			if tx > last.tx {
				last.tx = tx
			}
			return
		case minute < last.at:
			// The clock stepped back. Rewriting a minute already behind the
			// edge would corrupt the series; skipping the sample loses
			// nothing that was actually observed.
			return
		}
	}

	c.ring[c.next] = capacityMinute{at: minute, rx: rx, tx: tx}
	c.next = (c.next + 1) % len(c.ring)
	if c.count < len(c.ring) {
		c.count++
	}
}

// View reduces the retained minutes to what a page reads. now is the caller's
// instant and also the end of the window: a minute older than the retention
// is not part of the answer even if the ring has not overwritten it yet. That
// happens whenever sampling stops for a while — with stats off, or across a
// restart — and without the cutoff a resumed series would keep a bucket from
// months ago and report a thirty-day window that spanned far more.
func (c *Capacity) View(now time.Time) capacityView {
	v := capacityView{AsOf: now, State: capacityWarming}
	if c == nil {
		return v
	}

	c.mu.Lock()
	points := c.count
	v.Uplink = c.uplink
	cutoff := now.Add(-capacityDays*24*time.Hour).Unix() / 60
	current := now.Unix() / 60
	var since time.Time
	kept := 0
	// Only positive peaks enter the distribution. A minute in which the
	// house used nothing says what was asked for, not what the uplink could
	// carry; including those minutes would drag a p99 toward zero on any
	// network that sleeps, which is the opposite of a capacity baseline.
	rx := make([]int64, 0, points)
	tx := make([]int64, 0, points)
	for i := 0; i < points; i++ {
		index := i
		if points == len(c.ring) {
			index = (c.next + i) % len(c.ring)
		}
		minute := c.ring[index]
		if minute.at < cutoff || minute.at > current {
			continue
		}
		if kept == 0 {
			since = time.Unix(minute.at*60, 0)
		}
		kept++
		if minute.rx > 0 {
			rx = append(rx, minute.rx)
		}
		if minute.tx > 0 {
			tx = append(tx, minute.tx)
		}
	}
	c.mu.Unlock()

	v.RX = capacityDirection(kept, since, rx)
	v.TX = capacityDirection(kept, since, tx)
	if kept >= capacityWarmingPoints {
		v.State = capacityMeasured
	}
	return v
}

func capacityDirection(points int, since time.Time, values []int64) capacityDirectionView {
	view := capacityDirectionView{Points: points}
	if points > 0 {
		t := since
		view.Since = &t
	}
	if len(values) == 0 {
		return view
	}
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
	view.PeakBps = values[len(values)-1]
	view.P99Bps = percentile99(values)
	return view
}

// percentile99 is the capacity series' percentile: the nearest-rank 99th of
// the minute peaks. On a fresh series it therefore equals the peak until there
// are enough minutes for the tail to mean anything, and it never interpolates
// between two readings the box did not take. The general form lives beside the
// connection census, which asks the same question at p50 and p90.
func percentile99(sorted []int64) int64 { return percentile(sorted, 99) }

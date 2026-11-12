package gateway

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/netip"
	"os"
	"sync"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/core"
	"github.com/open-linux-router/open-linux-router/internal/runtimedb"
)

type activityBytes struct {
	Up   uint64 `json:"up"`
	Down uint64 `json:"down"`
}
type activityBucket struct {
	At    time.Time     `json:"at"`
	Bytes activityBytes `json:"bytes"`
}

// activityRetentionDays and ActivityRetention are how far back the sampler's
// half-hour buckets are kept, and therefore the window a client page's total
// covers.
//
// The number is a UI contract, not only a storage choice: a client card draws
// the matrix and then, underneath it, one total that is exactly that matrix
// added up, so the two agree by construction and a reader who wonders where the
// number came from can see the cells it came from. Fourteen days answered that
// for a fortnight — long enough to look like a total, too short to be one —
// while what it replaced (the kernel's cumulative counter, reset by every
// restart and expired after a day of idle) covered an unexplained interval that
// no two screens agreed on (docs/ui-content-guide.md).
const (
	activityRetentionDays = 90
	ActivityRetention     = activityRetentionDays * 24 * time.Hour
)

// ActivityEntries bounds retained (bucket, address) pairs, and with them the
// memory the sampler holds and the rows a restore reads.
//
// Ninety days is 4,320 buckets, so a house with thirty active addresses spends
// roughly 130,000 of these. The ceiling leaves room for IPv6 rotation and a few
// exits, and still stops a pathological network from growing the map without
// bound. It is a *memory* bound, not a retention promise: when it is reached the
// oldest buckets go, so the window silently shortens instead of recording
// stopping. That direction is deliberate — a matrix whose left edge starts later
// is readable, while one that stops moving is indistinguishable from a broken
// router, and by the time entries expire nobody can tell how long it has been
// quiet. Privacy addresses make the ceiling reachable: one telephone rotating its
// IPv6 address daily fills it in weeks, not years.
const ActivityEntries = runtimedb.ActivityLimit

// activityView is the retained history as the UI reads it, and nothing else.
//
// Days travels with the buckets so a screen can say "last 90 days" without
// keeping its own copy of the retention window that could drift from this one.
// A total is deliberately not sent: it is the sum of these buckets, and adding
// them where they are drawn is what makes the number under the matrix and the
// matrix itself agree.
type activityView struct {
	Days    int              `json:"days"`
	Buckets []activityBucket `json:"buckets"`
}

// Activity retains bounded counter differences, never raw flows.
type Activity struct {
	mu       sync.Mutex
	previous map[string]Usage
	last     time.Time
	buckets  map[int64]map[netip.Addr]activityBytes
	entries  int
	// ceiling is ActivityEntries. It is a field only so a test can lower it
	// without materializing a quarter of a million map entries; nothing else
	// changes it.
	ceiling int
	// trimmed records that the ceiling has cost history, so the reason is
	// logged once rather than on every sample that keeps the series alive.
	trimmed bool
	path    string
	db      *runtimedb.Store
	saved   map[int64]map[netip.Addr]activityBytes
}

// OpenActivityDB imports the old checkpoint once and restores retained buckets.
func OpenActivityDB(db *runtimedb.Store, legacy string) (*Activity, error) {
	err := db.Import("activity", legacy, func(data []byte, tx *sql.Tx) error {
		var stored struct {
			Version int                                    `json:"version"`
			Buckets map[int64]map[netip.Addr]activityBytes `json:"buckets"`
		}
		if err := json.Unmarshal(data, &stored); err != nil {
			return err
		}
		if stored.Version != 1 {
			return fmt.Errorf("unsupported activity history version %d", stored.Version)
		}
		rows := []runtimedb.Activity{}
		now := time.Now()
		for at, bucket := range stored.Buckets {
			if at < now.Add(-ActivityRetention).Unix() || at > now.Unix() {
				continue
			}
			for addr, value := range bucket {
				if !addr.IsValid() {
					return fmt.Errorf("invalid activity address")
				}
				rows = append(rows, runtimedb.Activity{Bucket: at, Address: addr.String(), Up: value.Up, Down: value.Down})
			}
		}
		if len(rows) > ActivityEntries {
			return fmt.Errorf("activity history exceeds entry limit")
		}
		return runtimedb.PutActivity(tx, rows)
	})
	if err != nil {
		return nil, err
	}
	a := NewActivity()
	a.db = db
	now := time.Now()
	rows, err := db.Activity(now.Add(-ActivityRetention).Unix(), now.Unix())
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		addr, err := netip.ParseAddr(row.Address)
		if err != nil {
			return nil, err
		}
		if a.buckets[row.Bucket] == nil {
			a.buckets[row.Bucket] = map[netip.Addr]activityBytes{}
		}
		a.buckets[row.Bucket][addr] = activityBytes{Up: row.Up, Down: row.Down}
		a.entries++
	}
	a.saved = map[int64]map[netip.Addr]activityBytes{}
	for at, bucket := range a.buckets {
		a.saved[at] = map[netip.Addr]activityBytes{}
		for addr, v := range bucket {
			a.saved[at][addr] = v
		}
	}
	return a, nil
}

func NewActivity() *Activity {
	return &Activity{buckets: make(map[int64]map[netip.Addr]activityBytes), ceiling: ActivityEntries}
}

// OpenActivity restores history, but never restores counter baselines across restarts.
func OpenActivity(path string) (*Activity, error) {
	a := NewActivity()
	a.path = path
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return a, nil
	}
	if err != nil {
		return nil, err
	}
	var stored struct {
		Version int                                    `json:"version"`
		Buckets map[int64]map[netip.Addr]activityBytes `json:"buckets"`
	}
	if err := json.Unmarshal(data, &stored); err != nil {
		return nil, fmt.Errorf("reading activity history: %w", err)
	}
	if stored.Version != 1 {
		return nil, fmt.Errorf("unsupported activity history version %d", stored.Version)
	}
	now := time.Now()
	for at, bucket := range stored.Buckets {
		if at < now.Add(-ActivityRetention).Unix() || at > now.Unix() {
			continue
		}
		for addr := range bucket {
			if !addr.IsValid() {
				return nil, fmt.Errorf("invalid activity address")
			}
		}
		a.entries += len(bucket)
		if a.entries > ActivityEntries {
			return nil, fmt.Errorf("activity history exceeds entry limit")
		}
		a.buckets[at] = bucket
	}
	return a, nil
}

// Save checkpoints history atomically. Counter baselines are deliberately omitted.
func (a *Activity) Save() error {
	if a.db != nil {
		a.mu.Lock()
		defer a.mu.Unlock()
		rows := []runtimedb.Activity{}
		for at, bucket := range a.buckets {
			for addr, value := range bucket {
				old, ok := a.saved[at][addr]
				if !ok || old != value {
					rows = append(rows, runtimedb.Activity{Bucket: at, Address: addr.String(), Up: value.Up, Down: value.Down})
				}
			}
		}
		if err := a.db.SaveActivity(rows, a.persistedCutoff()); err != nil {
			return err
		}
		if a.saved == nil {
			a.saved = map[int64]map[netip.Addr]activityBytes{}
		}
		for _, r := range rows {
			addr := netip.MustParseAddr(r.Address)
			if a.saved[r.Bucket] == nil {
				a.saved[r.Bucket] = map[netip.Addr]activityBytes{}
			}
			a.saved[r.Bucket][addr] = activityBytes{Up: r.Up, Down: r.Down}
		}
		for at := range a.saved {
			if a.buckets[at] == nil {
				delete(a.saved, at)
			}
		}
		return nil
	}
	if a.path == "" {
		return nil
	}
	a.mu.Lock()
	data, err := json.Marshal(struct {
		Version int                                    `json:"version"`
		Buckets map[int64]map[netip.Addr]activityBytes `json:"buckets"`
	}{1, a.buckets})
	a.mu.Unlock()
	if err != nil {
		return err
	}
	return core.WriteFileAtomic(a.path, data, 0o600)
}

// persistedCutoff is the oldest bucket still worth storing. Rows older than it
// are deleted with the checkpoint, so a bucket the ceiling evicted cannot return
// on the next restart and fill the map again. Retention alone is not enough: it
// would keep everything the ceiling dropped for another ninety days.
//
// Nothing retained means nothing to keep, and the whole table goes. An empty
// series is not a load error — every caller that cares reads the buckets — and
// leaving rows behind would resurrect history no screen is showing. The caller
// holds the lock.
func (a *Activity) persistedCutoff() int64 {
	oldest, found := int64(0), false
	for at := range a.buckets {
		if !found || at < oldest {
			oldest, found = at, true
		}
	}
	if !found {
		return time.Now().Unix() + 1
	}
	return oldest
}

func (a *Activity) observe(now time.Time, usage []Usage, valid bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for at, bucket := range a.buckets {
		if at < now.Add(-ActivityRetention).Unix() {
			a.entries -= len(bucket)
			delete(a.buckets, at)
		}
	}
	if !valid {
		a.previous = nil
		a.last = time.Time{}
		return
	}
	next := make(map[string]Usage, len(usage))
	at := now.Truncate(30 * time.Minute).Unix()
	continuous := !a.last.IsZero() && now.After(a.last) && now.Sub(a.last) <= 2*time.Minute && a.last.Truncate(30*time.Minute).Unix() == at
	for _, u := range usage {
		key := u.Addr.String() + "\x00" + u.Exit
		next[key] = u
		old, exists := a.previous[key]
		if !continuous || !exists || u.UpBytes < old.UpBytes || u.DownBytes < old.DownBytes {
			continue
		}
		bucket := a.buckets[at]
		if bucket == nil {
			bucket = make(map[netip.Addr]activityBytes)
			a.buckets[at] = bucket
		}
		value, held := bucket[u.Addr]
		if !held {
			if !a.makeRoom(at) {
				continue
			}
			a.entries++
		}
		value.Up += u.UpBytes - old.UpBytes
		value.Down += u.DownBytes - old.DownBytes
		bucket[u.Addr] = value
	}
	a.previous, a.last = next, now
}

// makeRoom frees one entry at the ceiling by dropping the oldest whole bucket,
// and reports whether a new (bucket, address) pair may be recorded.
//
// The bucket being written is never a candidate, so the sample in hand always
// lands and the matrix's right edge keeps moving. Whole buckets go rather than
// single addresses because a displayed cell has to stay internally consistent:
// a four-hour total partly made of evicted half-hours would be a number nobody
// could check. The caller holds the lock.
func (a *Activity) makeRoom(current int64) bool {
	for a.entries >= a.ceiling {
		oldest, found := int64(0), false
		for at := range a.buckets {
			if at == current {
				continue
			}
			if !found || at < oldest {
				oldest, found = at, true
			}
		}
		if !found {
			// Everything retained is the bucket being written; the ceiling is
			// smaller than a single sample and cannot be honoured.
			return false
		}
		a.entries -= len(a.buckets[oldest])
		delete(a.buckets, oldest)
		if !a.trimmed {
			a.trimmed = true
			slog.Warn("activity history reached its entry ceiling; the oldest retained buckets are dropped so recording continues", "ceiling", a.ceiling)
		}
	}
	return true
}

func (a *Activity) Run(ctx context.Context, applier Applier) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	checkpoint := time.NewTicker(time.Minute)
	defer checkpoint.Stop()
	for {
		cfg, err := applier.Load()
		var usage []Usage
		valid := err == nil && cfg.StatsOrDefault()
		if valid {
			usage, err = applier.Traffic(ctx)
			valid = err == nil
		}
		a.observe(time.Now(), usage, valid)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-checkpoint.C:
			if err := a.Save(); err != nil {
				slog.Error("could not save activity history", "error", err)
			}
		}
	}
}

// view is the retained history for these addresses, tagged with the window it
// was cut from, so a caller never has to assume how far back it reaches.
func (a *Activity) view(addresses []netip.Addr) activityView {
	return activityView{Days: activityRetentionDays, Buckets: a.snapshot(addresses)}
}

func (a *Activity) snapshot(addresses []netip.Addr) []activityBucket {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]activityBucket, 0)
	for at, values := range a.buckets {
		var total activityBytes
		found := false
		for _, addr := range addresses {
			if v, ok := values[addr]; ok {
				found = true
				total.Up += v.Up
				total.Down += v.Down
			}
		}
		if found {
			out = append(out, activityBucket{At: time.Unix(at, 0), Bytes: total})
		}
	}
	return out
}

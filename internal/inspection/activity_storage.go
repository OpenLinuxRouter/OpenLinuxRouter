package inspection

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

const (
	activityRetention          = 30 * 24 * time.Hour
	activityStorageBytes int64 = 64 << 20
	activityShardBytes   int64 = 2 << 20
)

type activityMetadata struct {
	Version   int       `json:"version"`
	Since     time.Time `json:"since"`
	SampledAt time.Time `json:"sampled_at"`
	// Every restart establishes a fresh conntrack baseline. Persisting the last
	// successful baseline time documents the observation gap without replaying
	// counters accumulated while the daemon was down.
	BaselineAt time.Time `json:"baseline_at"`
}

type activityShard struct {
	Version int              `json:"version"`
	Buckets []ActivityBucket `json:"buckets"`
}

// SetStorage enables durable per-client history beneath dir. Call before BeginDevice.
// The archive is separate from the flow archive and has its own 64 MiB bound.
func (a *Activity) SetStorage(dir string) error {
	if a == nil {
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.started.IsZero() {
		return errors.New("cannot change activity storage while recording")
	}
	if strings.TrimSpace(dir) == "" {
		return errors.New("activity storage directory is empty")
	}
	root, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	if err = privateActivityDir(root); err != nil {
		return err
	}
	a.storage = root
	return a.pruneStorageLocked(0, "")
}

func privateActivityDir(path string) error {
	if err := os.MkdirAll(path, 0700); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("activity directory is not a real directory: %s", path)
	}
	return os.Chmod(path, 0700)
}

// BeginDevice resumes the client's retained series, with a new live baseline.
// Reading existing counters afresh deliberately excludes all downtime traffic.
func (a *Activity) BeginDevice(ctx context.Context, mac string, addresses []string) error {
	if a == nil {
		return nil
	}
	mac, err := core.NormalizeMAC(mac)
	if err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if err = a.checkpointLocked(); err != nil {
		return err
	}
	if a.storage == "" {
		return errors.New("activity storage is not configured")
	}
	if err = privateActivityDir(filepath.Join(a.storage, mac)); err != nil {
		return err
	}
	if err = a.pruneStorageLocked(0, ""); err != nil {
		return err
	}
	meta, err := a.metadataLocked(mac)
	if err != nil {
		return err
	}
	// Read and validate the live window before replacing a running recorder.
	history, err := a.historyLocked(mac, a.now().Add(-activityBuckets*activityWidth), a.now(), activityWidth)
	if err != nil {
		return err
	}
	a.reset()
	a.beginLocked(ctx, addresses)
	a.client = mac
	a.deviceSince = meta.Since
	if a.deviceSince.IsZero() || a.deviceSince.After(a.now()) {
		a.deviceSince = a.started
	}
	if cutoff := a.now().Add(-activityRetention); a.deviceSince.Before(cutoff) {
		a.deviceSince = cutoff
	}
	a.sampledAt = a.started
	// Only the live ring is hydrated, never all thirty days. Old addresses may
	// have changed; MAC queries remain authoritative for retained client history.
	var addr netip.Addr
	for candidate := range a.watched {
		if !addr.IsValid() || candidate.Less(addr) {
			addr = candidate
		}
	}
	if addr.IsValid() {
		for _, bucket := range history {
			for _, row := range bucket.Sites {
				// Hydrating is not new traffic and must not be appended to pending.
				client := a.client
				a.client = ""
				a.add(bucket.At, addr, row.Site, row.Up, row.Down)
				a.client = client
			}
		}
		a.prune()
	}
	if err = a.checkpointLocked(); err != nil {
		a.started = time.Time{}
		a.watched = nil
		return err
	}
	return nil
}

// Checkpoint synchronously persists pending minute deltas and sample metadata.
// Only touched hourly shards are rewritten; a shard contains at most 60 minutes.
func (a *Activity) Checkpoint() error {
	if a == nil {
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.checkpointLocked()
}

func (a *Activity) checkpointLocked() error {
	if a.storage == "" {
		return a.storageErr
	}
	if a.client == "" {
		a.storageErr = a.pruneStorageLocked(0, "")
		return a.storageErr
	}
	hours := map[int64][]int64{}
	for minute, sites := range a.pending {
		folded := foldSites(sites)
		a.pending[minute] = map[string]*counters{}
		for _, row := range folded {
			mergeUsage(a.pending[minute], row.Site, row.Up, row.Down)
		}
		hour := minute / 3600 * 3600
		hours[hour] = append(hours[hour], minute)
	}
	ordered := make([]int64, 0, len(hours))
	for hour := range hours {
		ordered = append(ordered, hour)
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i] < ordered[j] })
	for _, hour := range ordered {
		path := a.shardPath(a.client, hour)
		shard, err := readActivityShard(path, hour)
		if err != nil {
			a.storageErr = err
			return err
		}
		buckets := map[int64]map[string]*counters{}
		for _, bucket := range shard.Buckets {
			sites := map[string]*counters{}
			for _, row := range bucket.Sites {
				mergeUsage(sites, row.Site, row.Up, row.Down)
			}
			buckets[bucket.At.Unix()] = sites
		}
		for _, minute := range hours[hour] {
			if buckets[minute] == nil {
				buckets[minute] = map[string]*counters{}
			}
			for site, c := range a.pending[minute] {
				mergeUsage(buckets[minute], site, c.up, c.down)
			}
		}
		shard = activityShard{Version: 1, Buckets: make([]ActivityBucket, 0, len(buckets))}
		for minute, sites := range buckets {
			shard.Buckets = append(shard.Buckets, ActivityBucket{At: time.Unix(minute, 0).UTC(), Sites: foldSites(sites)})
		}
		sort.Slice(shard.Buckets, func(i, j int) bool { return shard.Buckets[i].At.Before(shard.Buckets[j].At) })
		err = a.writeJSONLocked(path, shard)
		var committed *activityCommittedError
		if err == nil || errors.As(err, &committed) {
			// A directory sync failure after rename must be reported, but the
			// replacement already happened and its deltas cannot be applied again.
			for _, minute := range hours[hour] {
				delete(a.pending, minute)
			}
		}
		if err != nil {
			a.storageErr = err
			return err
		}
	}
	if _, err := a.metadataLocked(a.client); err != nil {
		a.storageErr = err
		return err
	}
	meta := activityMetadata{Version: 1, Since: a.deviceSince, SampledAt: a.sampledAt, BaselineAt: a.baselineAt}
	if err := a.writeJSONLocked(filepath.Join(a.storage, a.client, "state.json"), meta); err != nil {
		a.storageErr = err
		return err
	}
	a.storageErr = a.pruneStorageLocked(0, "")
	return a.storageErr
}

func mergeUsage(sites map[string]*counters, site string, up, down uint64) {
	c := sites[site]
	if c == nil {
		c = &counters{}
		sites[site] = c
	}
	c.up += up
	c.down += down
}

func (a *Activity) shardPath(mac string, hour int64) string {
	return filepath.Join(a.storage, mac, strconv.FormatInt(hour, 10)+".json")
}

func decodeActivityFile(path string, out any) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("invalid activity file %s", path)
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err = f.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() > activityShardBytes {
		return fmt.Errorf("invalid activity file %s", path)
	}
	dec := json.NewDecoder(io.LimitReader(f, activityShardBytes+1))
	if err = dec.Decode(out); err != nil {
		return fmt.Errorf("read activity %s: %w", path, err)
	}
	var extra any
	if err = dec.Decode(&extra); err != io.EOF {
		return fmt.Errorf("trailing activity data in %s", path)
	}
	return nil
}

func readActivityShard(path string, hour int64) (activityShard, error) {
	var shard activityShard
	err := decodeActivityFile(path, &shard)
	if errors.Is(err, os.ErrNotExist) {
		return activityShard{Version: 1}, nil
	}
	if err != nil {
		return shard, err
	}
	if shard.Version != 1 || len(shard.Buckets) > 60 {
		return shard, fmt.Errorf("invalid activity shard %s", path)
	}
	seen := map[int64]bool{}
	for _, bucket := range shard.Buckets {
		minute := bucket.At.Unix()
		if minute%60 != 0 || minute < hour || minute >= hour+3600 || seen[minute] || len(bucket.Sites) > activitySites {
			return shard, fmt.Errorf("invalid activity bucket in %s", path)
		}
		seen[minute] = true
		for _, row := range bucket.Sites {
			if row.Site == "" || len(row.Site) > 1024 {
				return shard, fmt.Errorf("invalid activity site in %s", path)
			}
		}
	}
	return shard, nil
}

func (a *Activity) metadataLocked(mac string) (activityMetadata, error) {
	var meta activityMetadata
	err := decodeActivityFile(filepath.Join(a.storage, mac, "state.json"), &meta)
	if errors.Is(err, os.ErrNotExist) {
		return meta, nil
	}
	if err != nil {
		return meta, err
	}
	if meta.Version != 1 || meta.Since.IsZero() {
		return meta, errors.New("invalid activity metadata")
	}
	return meta, nil
}

// History returns retained MAC history, aggregating into at most 241 buckets.
// Empty time slots are omitted, including periods when sampling was stopped.
func (a *Activity) History(mac string, window time.Duration) (activityResponse, error) {
	out := activityResponse{Buckets: []ActivityBucket{}, Sampled: int(SampleInterval.Seconds())}
	if a == nil {
		return out, nil
	}
	normalized, err := core.NormalizeMAC(mac)
	if err != nil {
		return out, err
	}
	if window < time.Minute || window > activityRetention {
		return out, errors.New("activity range must be greater than zero and at most 30 days")
	}
	width := ((window/time.Second+239)/240 + 59) / 60 * 60 * time.Second
	out.WindowSec = int(window.Seconds())
	out.BucketSec = int(width.Seconds())
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.storage == "" {
		return out, nil
	}
	now := a.now()
	// At most 241 aligned columns include the partial buckets at both edges.
	edge := now.Add(-window).Truncate(activityWidth)
	out.Buckets, err = a.historyLocked(normalized, edge, now, width)
	if err != nil {
		return out, err
	}
	meta, err := a.metadataLocked(normalized)
	if err != nil {
		return out, err
	}
	out.Recording = a.client == normalized && !a.started.IsZero()
	if !meta.Since.IsZero() {
		since := meta.Since
		if edge.After(since) {
			since = edge
		}
		out.Since = &since
	}
	return out, nil
}

func (a *Activity) historyLocked(mac string, from, to time.Time, width time.Duration) ([]ActivityBucket, error) {
	merged := map[int64]map[string]*counters{}
	for hour := from.Truncate(time.Hour); !hour.After(to); hour = hour.Add(time.Hour) {
		shard, err := readActivityShard(a.shardPath(mac, hour.Unix()), hour.Unix())
		if err != nil {
			return nil, err
		}
		for _, bucket := range shard.Buckets {
			if bucket.At.Before(from) || bucket.At.After(to) {
				continue
			}
			key := bucket.At.Truncate(width).Unix()
			if merged[key] == nil {
				merged[key] = map[string]*counters{}
			}
			for _, row := range bucket.Sites {
				mergeUsage(merged[key], row.Site, row.Up, row.Down)
			}
			// Bound query memory even when every minute introduces new sites.
			folded := foldSites(merged[key])
			merged[key] = map[string]*counters{}
			for _, row := range folded {
				mergeUsage(merged[key], row.Site, row.Up, row.Down)
			}
		}
	}
	out := make([]ActivityBucket, 0, len(merged))
	for key, sites := range merged {
		out = append(out, ActivityBucket{At: time.Unix(key, 0).UTC(), Sites: foldSites(sites)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].At.Before(out[j].At) })
	return out, nil
}

func (a *Activity) writeJSONLocked(path string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if int64(len(data)) > activityShardBytes {
		return errors.New("activity checkpoint exceeds shard limit")
	}
	// Reserve temporary-file space as well as the existing shard; no write can
	// make the managed archive exceed its global byte limit.
	if err = a.pruneStorageLocked(int64(len(data)), path); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".checkpoint-")
	if err != nil {
		return err
	}
	temp := f.Name()
	// temp is an absolute path returned by CreateTemp inside the verified client
	// directory; cleanup never traverses a caller-provided/computed target.
	defer os.Remove(temp)
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err = os.Rename(temp, path); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return &activityCommittedError{err}
	}
	defer dir.Close()
	if err = dir.Sync(); err != nil {
		return &activityCommittedError{err}
	}
	return nil
}

type activityDiskFile struct {
	path string
	hour int64
	size int64
}

func (a *Activity) pruneStorageLocked(reserve int64, protected string) error {
	if a.storage == "" {
		return nil
	}
	clients, err := os.ReadDir(a.storage)
	if err != nil {
		return err
	}
	var files []activityDiskFile
	var total int64
	cutoff := a.now().Add(-activityRetention).Unix()
	for _, client := range clients {
		if !client.IsDir() {
			continue
		}
		normalized, err := core.NormalizeMAC(client.Name())
		if err != nil || normalized != client.Name() {
			continue
		}
		dir := filepath.Join(a.storage, client.Name())
		entries, err := os.ReadDir(dir)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			path := filepath.Join(dir, entry.Name())
			info, err := entry.Info()
			if err != nil {
				return err
			}
			if !info.Mode().IsRegular() {
				continue
			}
			hour, parseErr := strconv.ParseInt(strings.TrimSuffix(entry.Name(), ".json"), 10, 64)
			shard := parseErr == nil && strings.HasSuffix(entry.Name(), ".json") && hour%3600 == 0
			temporary := strings.HasPrefix(entry.Name(), ".checkpoint-")
			if temporary || (shard && hour+3600 <= cutoff) {
				// Both components are validated above; this is one direct child of the
				// configured private root/client, never a symlink or arbitrary path.
				if err = os.Remove(path); err != nil {
					return err
				}
				continue
			}
			total += info.Size()
			if shard {
				files = append(files, activityDiskFile{path: path, hour: hour, size: info.Size()})
			}
		}
	}
	limit := a.storageLimit
	if limit == 0 {
		limit = activityStorageBytes
	}
	sort.Slice(files, func(i, j int) bool {
		if files[i].hour == files[j].hour {
			return files[i].path < files[j].path
		}
		return files[i].hour < files[j].hour
	})
	for _, file := range files {
		if total+reserve <= limit {
			break
		}
		if file.path == protected {
			continue
		}
		if err = os.Remove(file.path); err != nil {
			return err
		}
		total -= file.size
	}
	if total+reserve > limit {
		return errors.New("activity archive byte limit reached")
	}
	return nil
}

// Problem reports the last durable checkpoint failure for the service status.
func (a *Activity) Problem() string {
	if a == nil {
		return ""
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.storageErr != nil {
		return a.storageErr.Error()
	}
	return ""
}

type activityCommittedError struct{ err error }

func (e *activityCommittedError) Error() string { return e.err.Error() }
func (e *activityCommittedError) Unwrap() error { return e.err }

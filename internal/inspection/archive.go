package inspection

import (
	"container/heap"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/core"
	"golang.org/x/sys/unix"
)

const (
	archiveRetentionDays       = 30
	archiveMaxBytes      int64 = 5 * 1024 * 1024 * 1024
	archiveMetadataLimit       = 4 * 1024 * 1024
)

var archiveID = regexp.MustCompile(`^[0-9]{20}-[0-9a-f]{32}$`)

// historyEntry contains only the list fields. Body previews stay in the
// sidecar and the full native flow is streamed separately.
type historyEntry struct {
	ID     string  `json:"id"`
	At     float64 `json:"at"`
	Method string  `json:"method"`
	URL    string  `json:"url"`
	Status int     `json:"status"`
	Bytes  int64   `json:"bytes"`
}

type historyResponse struct {
	Entries       []historyEntry `json:"entries"`
	NextBefore    string         `json:"next_before"`
	RetainedBytes int64          `json:"retained_bytes"`
	TotalBytes    int64          `json:"total_bytes"`
	RetentionDays int            `json:"retention_days"`
	MaxBytes      int64          `json:"max_bytes"`
}

type historyPolicy struct {
	Days     int   `json:"days"`
	MaxBytes int64 `json:"max_bytes"`
}

func (s *Service) readHistoryPolicy() historyPolicy {
	policy := historyPolicy{Days: archiveRetentionDays, MaxBytes: archiveMaxBytes}
	root, err := os.OpenRoot(s.Dir)
	if err != nil {
		return policy
	}
	defer root.Close()
	file, err := archiveOpen(root, "retention.json")
	if err != nil {
		return policy
	}
	defer file.Close()
	var saved historyPolicy
	if json.NewDecoder(io.LimitReader(file, 4096)).Decode(&saved) == nil && saved.Days >= 1 && saved.Days <= 365 && saved.MaxBytes >= 1024*1024 && saved.MaxBytes <= 1024*1024*1024*1024 {
		return saved
	}
	return policy
}

// archiveOpen rejects symlinks and non-regular files. os.Root also confines
// every open beneath the archive if a directory is replaced concurrently.
func archiveOpen(root *os.Root, name string) (*os.File, error) {
	info, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, os.ErrPermission
	}
	file, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	info, err = file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		file.Close()
		return nil, os.ErrPermission
	}
	return file, nil
}

func (s *Service) historyRoot() (*os.Root, error) {
	confdir, err := os.OpenRoot(s.Dir)
	if err != nil {
		return nil, err
	}
	defer confdir.Close()
	info, err := confdir.Lstat("history")
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, os.ErrPermission
	}
	return confdir.OpenRoot("history")
}

func archiveDevice(root *os.Root, mac string) (*os.Root, error) {
	info, err := root.Lstat(mac)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, os.ErrPermission
	}
	return root.OpenRoot(mac)
}

func archiveReadEntry(root *os.Root, id string) (historyEntry, error) {
	var entry historyEntry
	file, err := archiveOpen(root, id+".meta")
	if err != nil {
		return entry, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return entry, err
	}
	if info.Size() > archiveMetadataLimit {
		return entry, fmt.Errorf("archive metadata is too large")
	}
	if err = json.NewDecoder(io.LimitReader(file, archiveMetadataLimit+1)).Decode(&entry); err != nil {
		return entry, err
	}
	if entry.ID != id || entry.Bytes < 0 {
		return entry, fmt.Errorf("invalid archive metadata")
	}
	return entry, nil
}

// archiveWalk keeps at most one directory batch in memory and never opens a
// native flow. Only O(page size) metadata is kept by callers.
func archiveWalk(ctx context.Context, root *os.Root, visit func(string, *os.Root, string, string, int64) error) error {
	directories, err := root.Open(".")
	if err != nil {
		return err
	}
	defer directories.Close()
	for {
		batch, readErr := directories.ReadDir(128)
		for _, directory := range batch {
			if err := ctx.Err(); err != nil {
				return err
			}
			mac, err := core.NormalizeMAC(directory.Name())
			if err != nil || mac != directory.Name() || !directory.IsDir() {
				continue
			}
			client, err := archiveDevice(root, mac)
			if err != nil {
				continue
			}
			err = archiveWalkDevice(ctx, client, func(id, suffix string, size int64) error { return visit(mac, client, id, suffix, size) })
			client.Close()
			if err != nil {
				return err
			}
		}
		if errors.Is(readErr, io.EOF) {
			return nil
		}
		if readErr != nil {
			return readErr
		}
	}
}

func archiveWalkDevice(ctx context.Context, client *os.Root, visit func(string, string, int64) error) error {
	files, err := client.Open(".")
	if err != nil {
		return err
	}
	defer files.Close()
	for {
		batch, readErr := files.ReadDir(128)
		for _, file := range batch {
			if err := ctx.Err(); err != nil {
				return err
			}
			suffix := filepath.Ext(file.Name())
			id := strings.TrimSuffix(file.Name(), suffix)
			if (suffix != ".meta" && suffix != ".json" && suffix != ".mitm") || !archiveID.MatchString(id) {
				continue
			}
			info, err := file.Info()
			if err != nil || !info.Mode().IsRegular() {
				continue
			}
			if err := visit(id, suffix, info.Size()); err != nil {
				return err
			}
		}
		if errors.Is(readErr, io.EOF) {
			return nil
		}
		if readErr != nil {
			return readErr
		}
	}
}

func archiveExpired(id string, policy historyPolicy) bool {
	micros, err := strconv.ParseInt(id[:20], 10, 64)
	return err != nil || micros < time.Now().Add(-time.Duration(policy.Days)*24*time.Hour).UnixMicro()
}

type historyHeap []historyEntry

func (h historyHeap) Len() int           { return len(h) }
func (h historyHeap) Less(i, j int) bool { return h[i].ID < h[j].ID }
func (h historyHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *historyHeap) Push(value any)    { *h = append(*h, value.(historyEntry)) }
func (h *historyHeap) Pop() any {
	old := *h
	value := old[len(old)-1]
	*h = old[:len(old)-1]
	return value
}

// history serves authenticated core routes; it deliberately does not depend
// on the live proxy or s.mu, so stopping inspection does not hide its history.
func (s *Service) history(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	clearBefore := fmt.Sprintf("%020d", time.Now().UnixMicro())
	mac, err := core.NormalizeMAC(r.URL.Query().Get("mac"))
	if err != nil {
		core.WriteError(w, http.StatusBadRequest, "a valid client MAC is required")
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodDelete {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	before := r.URL.Query().Get("before")
	if before != "" && !archiveID.MatchString(before) {
		core.WriteError(w, http.StatusBadRequest, "invalid history cursor")
		return
	}
	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		limit, err = strconv.Atoi(raw)
		if err != nil || limit < 1 || limit > 200 {
			core.WriteError(w, http.StatusBadRequest, "limit must be between 1 and 200")
			return
		}
	}
	search := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("search")))
	if len(search) > 1024 {
		core.WriteError(w, http.StatusBadRequest, "search is too long")
		return
	}
	if err := s.SweepHistory(r.Context()); err != nil {
		core.WriteError(w, http.StatusInternalServerError, "could not enforce history retention")
		return
	}
	policy := s.readHistoryPolicy()
	out := historyResponse{Entries: []historyEntry{}, RetentionDays: policy.Days, MaxBytes: policy.MaxBytes}
	root, err := s.historyRoot()
	if errors.Is(err, os.ErrNotExist) {
		core.WriteJSON(w, http.StatusOK, out)
		return
	}
	if err != nil {
		core.WriteError(w, http.StatusInternalServerError, "could not open flow history")
		return
	}
	defer root.Close()
	if r.Method == http.MethodDelete {
		unlock, err := archiveLock(r.Context(), root)
		if err != nil {
			core.WriteError(w, http.StatusInternalServerError, "could not lock flow history")
			return
		}
		defer unlock()
		defer archiveChanged(root)
		client, err := archiveDevice(root, mac)
		if err == nil {
			// Keep directories and temporary files; the clear watermark also
			// prevents a later websocket completion from restoring this snapshot.
			clearBefore, err = archiveAdvanceClear(client, clearBefore)
			if err == nil {
				err = archiveWalkDevice(r.Context(), client, func(id, suffix string, _ int64) error {
					if suffix != ".meta" || id[:20] > clearBefore {
						return nil
					}
					for _, extension := range []string{".meta", ".json", ".mitm"} {
						if err := client.Remove(id + extension); err != nil && !errors.Is(err, os.ErrNotExist) {
							return err
						}
					}
					return nil
				})
			}
			client.Close()
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			core.WriteError(w, http.StatusInternalServerError, "could not clear flow history")
			return
		}
	}
	selected := &historyHeap{}
	err = archiveWalk(r.Context(), root, func(deviceMAC string, client *os.Root, id, suffix string, size int64) error {
		out.TotalBytes += size
		if deviceMAC != mac {
			return nil
		}
		out.RetainedBytes += size
		if suffix != ".meta" || (before != "" && id >= before) || archiveExpired(id, policy) {
			return nil
		}
		entry, err := archiveReadEntry(client, id)
		if err != nil {
			return nil
		} // Ignore partial/corrupt entries; never fail the whole page.
		info, err := client.Lstat(id + ".mitm")
		if err != nil || !info.Mode().IsRegular() {
			return nil
		}
		if search != "" && !strings.Contains(strings.ToLower(entry.Method+" "+entry.URL+" "+strconv.Itoa(entry.Status)), search) {
			return nil
		}
		heap.Push(selected, entry)
		if selected.Len() > limit+1 {
			heap.Pop(selected)
		}
		return nil
	})
	if err != nil {
		core.WriteError(w, http.StatusInternalServerError, "could not read flow history")
		return
	}
	sort.Slice(*selected, func(i, j int) bool { return (*selected)[i].ID > (*selected)[j].ID })
	if selected.Len() > limit {
		*selected = (*selected)[:limit]
		out.NextBefore = (*selected)[limit-1].ID
	}
	out.Entries = []historyEntry(*selected)
	core.WriteJSON(w, http.StatusOK, out)
}

func (s *Service) historyFile(w http.ResponseWriter, r *http.Request, detail bool) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	mac, err := core.NormalizeMAC(r.URL.Query().Get("mac"))
	id := r.URL.Query().Get("id")
	if err != nil || !archiveID.MatchString(id) {
		core.WriteError(w, http.StatusBadRequest, "a valid client MAC and flow ID are required")
		return
	}
	if archiveExpired(id, s.readHistoryPolicy()) {
		http.NotFound(w, r)
		return
	}
	root, err := s.historyRoot()
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer root.Close()
	client, err := archiveDevice(root, mac)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer client.Close()
	if _, err := archiveReadEntry(client, id); err != nil {
		http.NotFound(w, r)
		return
	}
	extension := ".mitm"
	if detail {
		extension = ".json"
	}
	file, err := archiveOpen(client, id+extension)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	if detail {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
	} else {
		w.Header().Set("Content-Disposition", `attachment; filename="olr-`+strings.ReplaceAll(mac, ":", "")+"-"+id+`.mitm"`)
	}
	http.ServeContent(w, r, id+extension, info.ModTime(), file)
}

// archiveAdvanceClear is called under archiveLock. A request may have captured
// its cutoff before a newer request acquired the lock, so never lower it.
func archiveAdvanceClear(root *os.Root, cutoff string) (string, error) {
	file, err := archiveOpen(root, ".cleared_before")
	if err == nil {
		data, readErr := io.ReadAll(io.LimitReader(file, 64))
		file.Close()
		if readErr != nil {
			return "", readErr
		}
		previous := strings.TrimSpace(string(data))
		if len(previous) != 20 {
			return "", fmt.Errorf("invalid history clear watermark")
		}
		if _, err := strconv.ParseUint(previous, 10, 64); err != nil {
			return "", err
		}
		if previous > cutoff {
			cutoff = previous
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	return cutoff, archiveWriteMarker(root, ".cleared_before", cutoff)
}

func archiveWriteMarker(root *os.Root, name, value string) error {
	if info, err := root.Lstat(name); err == nil && !info.Mode().IsRegular() {
		return os.ErrPermission
	}
	file, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	if _, err := io.WriteString(file, value); err != nil {
		return err
	}
	return file.Sync()
}

func archiveChanged(root *os.Root) error {
	return archiveWriteMarker(root, ".generation", strconv.FormatInt(time.Now().UnixNano(), 10))
}

// archiveLock is shared with fcntl.flock in the addon. Cancellation keeps a
// request from waiting indefinitely behind a slow disk write.
func archiveLock(ctx context.Context, root *os.Root) (func(), error) {
	if info, err := root.Lstat(".archive.lock"); err == nil && !info.Mode().IsRegular() {
		return nil, os.ErrPermission
	}
	file, err := root.OpenFile(".archive.lock", os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	for {
		err = unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			return func() { _ = unix.Flock(int(file.Fd()), unix.LOCK_UN); _ = file.Close() }, nil
		}
		if !errors.Is(err, unix.EWOULDBLOCK) && !errors.Is(err, unix.EAGAIN) {
			file.Close()
			return nil, err
		}
		select {
		case <-ctx.Done():
			file.Close()
			return nil, ctx.Err()
		case <-time.After(25 * time.Millisecond):
		}
	}
}

type archiveVictim struct{ mac, id string }
type archiveVictims []archiveVictim

func (h archiveVictims) Len() int           { return len(h) }
func (h archiveVictims) Less(i, j int) bool { return h[i].id > h[j].id }
func (h archiveVictims) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *archiveVictims) Push(value any)    { *h = append(*h, value.(archiveVictim)) }
func (h *archiveVictims) Pop() any {
	old := *h
	value := old[len(old)-1]
	*h = old[:len(old)-1]
	return value
}

// SweepHistory enforces the global storage policy without the session mutex.
// The daemon calls it at startup and periodically even with inspection off.
// Memory stays bounded; only a batch of the 128 oldest filenames is retained.
func (s *Service) SweepHistory(ctx context.Context) error {
	root, err := s.historyRoot()
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer root.Close()
	unlock, err := archiveLock(ctx, root)
	if err != nil {
		return err
	}
	defer unlock()
	changed := false
	defer func() {
		if changed {
			_ = archiveChanged(root)
		}
	}()
	policy := s.readHistoryPolicy()
	var total int64
	err = archiveWalk(ctx, root, func(_ string, client *os.Root, id, suffix string, size int64) error {
		if archiveExpired(id, policy) {
			changed = true
			err := client.Remove(id + suffix)
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return err
		}
		total += size
		return nil
	})
	if err != nil {
		return err
	}
	for total > policy.MaxBytes {
		victims := &archiveVictims{}
		err := archiveWalk(ctx, root, func(mac string, _ *os.Root, id, suffix string, _ int64) error {
			if suffix != ".mitm" {
				return nil
			}
			heap.Push(victims, archiveVictim{mac: mac, id: id})
			if victims.Len() > 128 {
				heap.Pop(victims)
			}
			return nil
		})
		if err != nil {
			return err
		}
		if victims.Len() == 0 {
			return fmt.Errorf("archive contains no removable flow files")
		}
		sort.Slice(*victims, func(i, j int) bool { return (*victims)[i].id < (*victims)[j].id })
		var removed int64
		for _, victim := range *victims {
			client, err := archiveDevice(root, victim.mac)
			if err != nil {
				return err
			}
			for _, suffix := range []string{".meta", ".json", ".mitm"} {
				info, err := client.Lstat(victim.id + suffix)
				if errors.Is(err, os.ErrNotExist) {
					continue
				}
				if err != nil {
					client.Close()
					return err
				}
				if err := client.Remove(victim.id + suffix); err != nil {
					client.Close()
					return err
				}
				changed = true
				total -= info.Size()
				removed += info.Size()
			}
			client.Close()
			if total <= policy.MaxBytes {
				break
			}
		}
		if removed == 0 {
			return fmt.Errorf("archive retention made no progress")
		}
	}
	return nil
}

func (s *Service) historyFlow(w http.ResponseWriter, r *http.Request)   { s.historyFile(w, r, false) }
func (s *Service) historyDetail(w http.ResponseWriter, r *http.Request) { s.historyFile(w, r, true) }

package clients

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/core"
	"github.com/open-linux-router/open-linux-router/internal/runtimedb"
)

// What the router remembers about a client that is no longer here: when it was
// last heard from, and the name it last called itself.
//
// Presence says whether a client is here now, and says nothing once it has
// gone: a client that left at breakfast and one that left last month are both
// "away", and a quiet device that is here is only "idle". The map wants the
// one answer that covers all three — "seen 2h ago" — and no source holds it.
// The lease file records an expiry, the neighbour table a state, and neither
// survives the client being gone long enough to matter.
//
// The name has the same shape of problem, from the same cause. A client's name
// arrives with its DHCP lease; the lease is twelve hours by default, so a phone
// left in a bag for two days comes back to the map as a bare MAC. The client
// did not forget who it was — the router did. So both are kept here, and both
// are kept apart from olr.json on purpose. Intent is what an operator said;
// this is what the box observed, and it lives where observations do, under
// /var/lib, in a record that can be deleted at any time and costs nothing but
// history.
//
// **A remembered name is not an operator's name.** It is displayed while no
// source can say anything better, and it carries the time it was heard, so a
// surface can say "this is what it called itself, three days ago". It is never
// promoted into stored intent: that stays something a human typed, and the
// difference between the two is the whole point of the split in design.md §4.4.
//
// **Heard, not listed.** A sighting in the sense of Presence is not evidence:
// the kernel keeps a STALE neighbour entry for a client that left an hour ago,
// and /proc/net/arp reports it as complete. What counts here is the neighbour
// being REACHABLE — the kernel confirmed it answers within the last half
// minute — which is also what any traffic through the router keeps it. A
// device that only ever talks to its neighbours on the same network is not
// heard, the same blind spot the traffic counters have. A name needs no
// neighbour entry: the lease is evidence enough, and it is what outlives a
// sleeping phone.
//
// **Sampled in the background.** The page is not open most of the time, and a
// timestamp or name that only moved while somebody was looking would be a
// record of the operator. Samples are cheap (one netlink dump, one lease file)
// and taken every few seconds; the record is written every few minutes, and on
// the way down, because a router's disk is often flash and a write every sample
// would be wear for nothing.

// SeenPath is where olrd kept the record before it moved into SQLite, under an
// optional root. It is read once as a legacy import and never written.
func SeenPath(root string) string {
	return filepath.Join(root, "/var/lib/open-linux-router/devices/seen.json")
}

// HeardSource says which hardware addresses have just been heard from.
type HeardSource interface {
	Heard(ctx context.Context) ([]string, error)
}

// NameSource says what each client called itself. Hostnames are keyed by MAC in
// canonical form; the keys need not be canonical from the source, and
// unreadable ones are skipped.
//
// Separate from HeardSource because the evidence is different. Being heard is
// the neighbour table confirming an answer, which a client that is only asleep
// fails; announcing a name is the client's lease, which it keeps while asleep.
type NameSource interface {
	Hostnames(ctx context.Context) (map[string]string, error)
}

// Seen is the record, in memory, with the store behind it.
type Seen struct {
	// Path is the legacy file. Empty keeps the record in memory only, which is
	// what the daemon does now that SQLite is the store.
	Path string

	// Source is what is sampled for last-heard times. Nil means nothing is ever
	// heard, and the record only holds what the store had.
	Source HeardSource

	// Names is what is sampled for announced names. Nil means no name is ever
	// remembered, and the record only holds what the store had.
	Names NameSource

	Log *slog.Logger
	db  *runtimedb.Store

	mu      sync.Mutex
	records map[string]record
	dirty   bool
	// failing and namesFailing are whether the last sample of each source
	// failed, so a box whose kernel or lease file refuses the read logs it once
	// rather than every few seconds.
	failing      bool
	namesFailing bool
}

// record is the two observations about one MAC that outlive the client.
type record struct {
	// heard is when the kernel last confirmed it answering.
	heard time.Time

	// hostname is what the client last called itself, and namedAt when that was
	// heard. A client renewing its lease keeps namedAt moving, so the name is
	// forgotten only after the client has been away, not while it is here.
	hostname string
	namedAt  time.Time
}

// Forget drops a client neither heard from nor named in this long. Most of what
// the record would otherwise grow by is phones' randomised addresses, each a
// device that existed for one visit; a real device away this long is one nobody
// needs a "seen" time or a name for.
//
// The two dates are considered together rather than separately: a name is
// evidence that the client was here, so a client heard recently keeps the name
// it announced when it was last in range.
const Forget = 90 * 24 * time.Hour

// fresh reports whether anything about the client is recent enough to keep.
func (r record) fresh(now time.Time) bool {
	recent := func(at time.Time) bool { return !at.IsZero() && now.Sub(at) <= Forget }
	return recent(r.heard) || recent(r.namedAt)
}

func OpenSeenDB(db *runtimedb.Store, legacy string, source HeardSource, names NameSource, log *slog.Logger) (*Seen, error) {
	err := db.Import("seen", legacy, func(data []byte, tx *sql.Tx) error {
		f, err := decodeSeenFile(data)
		if err != nil {
			return err
		}
		rows := map[string]runtimedb.Seen{}
		for mac, at := range f.Seen {
			canon, err := core.NormalizeMAC(mac)
			if err != nil {
				return err
			}
			row := rows[canon]
			row.MAC, row.At = canon, at
			rows[canon] = row
		}
		for mac, n := range f.Names {
			canon, err := core.NormalizeMAC(mac)
			if err != nil {
				return err
			}
			row := rows[canon]
			row.MAC, row.Hostname, row.HostnameAt = canon, n.Hostname, n.At
			rows[canon] = row
		}
		out := make([]runtimedb.Seen, 0, len(rows))
		for _, row := range rows {
			out = append(out, row)
		}
		return runtimedb.PutSeen(tx, out)
	})
	if err != nil {
		return nil, err
	}
	rows, err := db.Seen()
	if err != nil {
		return nil, err
	}
	s := NewSeen("", source, names, log)
	s.db = db
	for _, row := range rows {
		s.rememberHeard(row.MAC, row.At)
		s.rememberName(row.MAC, row.Hostname, row.HostnameAt)
	}
	return s, nil
}

// seenFile is the legacy file's shape. Versioned because it outlives the
// release that wrote it: version 1 held only last-heard times, and version 2
// adds the names clients announced.
type seenFile struct {
	Version int                  `json:"version"`
	Seen    map[string]time.Time `json:"seen"`
	Names   map[string]seenName  `json:"names,omitempty"`
}

type seenName struct {
	Hostname string    `json:"hostname"`
	At       time.Time `json:"at"`
}

// decodeSeenFile reads either version. Version 1 is what an install upgraded
// from the file era has, and it simply carries no names.
func decodeSeenFile(data []byte) (seenFile, error) {
	var f seenFile
	if err := json.Unmarshal(data, &f); err != nil {
		return seenFile{}, err
	}
	switch f.Version {
	case 1, 2:
	default:
		return seenFile{}, fmt.Errorf("unsupported seen version %d", f.Version)
	}
	return f, nil
}

// NewSeen reads the record, when there is one. An unreadable file is logged
// and started over: it is history, and a router that refused to start for the
// want of it would be a bad trade.
func NewSeen(path string, source HeardSource, names NameSource, log *slog.Logger) *Seen {
	s := &Seen{Path: path, Source: source, Names: names, Log: log, records: map[string]record{}}
	if path == "" {
		return s
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return s
	}
	var f seenFile
	if err == nil {
		f, err = decodeSeenFile(data)
	}
	if err != nil {
		s.logger().Warn("starting the last-seen record over", "path", path, "error", err)
		return s
	}
	for mac, at := range f.Seen {
		if canon, err := core.NormalizeMAC(mac); err == nil {
			s.rememberHeard(canon, at)
		}
	}
	for mac, n := range f.Names {
		if canon, err := core.NormalizeMAC(mac); err == nil && strings.TrimSpace(n.Hostname) != "" {
			s.rememberName(canon, n.Hostname, n.At)
		}
	}
	return s
}

func (s *Seen) logger() *slog.Logger {
	if s.Log != nil {
		return s.Log
	}
	return slog.Default()
}

// rememberHeard moves the last-heard time forward, never back. Callers hold the
// lock, or own the record before it is shared.
func (s *Seen) rememberHeard(mac string, at time.Time) {
	r := s.records[mac]
	if at.After(r.heard) {
		r.heard = at
	}
	s.records[mac] = r
}

func (s *Seen) rememberName(mac, hostname string, at time.Time) {
	r := s.records[mac]
	if !at.After(r.namedAt) {
		return
	}
	r.hostname, r.namedAt = hostname, at
	s.records[mac] = r
}

// Record marks the addresses as heard at `at`. Unreadable ones are skipped:
// the source is the kernel, and one odd entry is not worth the rest.
func (s *Seen) Record(at time.Time, macs []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, mac := range macs {
		canon, err := core.NormalizeMAC(mac)
		if err != nil {
			continue
		}
		if r := s.records[canon]; at.After(r.heard) {
			r.heard = at
			s.records[canon] = r
			s.dirty = true
		}
	}
}

// Remember records what each client called itself, at `at`.
//
// A name is only ever replaced by a later one: a client that announces a
// hostname does so for as long as it is here, and an observation from before
// the one already held says nothing new.
func (s *Seen) Remember(at time.Time, names map[string]string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for mac, name := range names {
		canon, err := core.NormalizeMAC(mac)
		if err != nil {
			continue
		}
		if name = strings.TrimSpace(name); name == "" {
			continue
		}
		if r := s.records[canon]; at.After(r.namedAt) {
			r.hostname, r.namedAt = name, at
			s.records[canon] = r
			s.dirty = true
		}
	}
}

// LastSeen is when the client was last heard from.
func (s *Seen) LastSeen(mac string) (time.Time, bool) {
	if s == nil {
		return time.Time{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.records[mac]
	if !ok || r.heard.IsZero() {
		return time.Time{}, false
	}
	return r.heard, true
}

// Hostname is the last name the client announced, and when it was heard.
//
// The name is an observation with a date, never stored intent: see the file
// comment. A device that has never announced anything has none, which is not
// the same as one that announced an empty name — that is not recorded at all.
func (s *Seen) Hostname(mac string) (string, time.Time, bool) {
	if s == nil {
		return "", time.Time{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.records[mac]
	if !ok || r.hostname == "" {
		return "", time.Time{}, false
	}
	return r.hostname, r.namedAt, true
}

// Sample asks each source once and records what it said. One failing source
// does not stop the other: a box that cannot read its neighbour table can still
// remember names from leases.
func (s *Seen) Sample(ctx context.Context, now time.Time) {
	if s.Source != nil {
		macs, err := s.Source.Heard(ctx)
		s.mu.Lock()
		was := s.failing
		s.failing = err != nil
		s.mu.Unlock()
		switch {
		case err != nil && !was:
			s.logger().Warn("could not read the neighbour table; last-seen times will not move", "error", err)
		case err == nil:
			s.Record(now, macs)
		}
	}

	if s.Names != nil {
		names, err := s.Names.Hostnames(ctx)
		s.mu.Lock()
		was := s.namesFailing
		s.namesFailing = err != nil
		s.mu.Unlock()
		switch {
		case err != nil && !was:
			s.logger().Warn("could not read announced names; devices will fall back to their addresses", "error", err)
		case err == nil:
			s.Remember(now, names)
		}
	}
}

// Flush writes the record when it has changed, dropping what is past Forget.
func (s *Seen) Flush(now time.Time) error {
	s.mu.Lock()
	for mac, r := range s.records {
		if !r.fresh(now) {
			delete(s.records, mac)
			s.dirty = true
		}
	}
	if (s.Path == "" && s.db == nil) || !s.dirty {
		s.mu.Unlock()
		return nil
	}
	if s.db != nil {
		defer s.mu.Unlock()
		rows := []runtimedb.Seen{}
		for mac, r := range s.records {
			rows = append(rows, runtimedb.Seen{MAC: mac, At: r.heard, Hostname: r.hostname, HostnameAt: r.namedAt})
		}
		if err := s.db.SaveSeen(rows); err != nil {
			return err
		}
		s.dirty = false
		return nil
	}
	seen := map[string]time.Time{}
	names := map[string]seenName{}
	for mac, r := range s.records {
		if !r.heard.IsZero() {
			seen[mac] = r.heard
		}
		if r.hostname != "" {
			names[mac] = seenName{Hostname: r.hostname, At: r.namedAt}
		}
	}
	data, err := json.Marshal(seenFile{Version: 2, Seen: seen, Names: names})
	s.dirty = false
	s.mu.Unlock()
	if err != nil {
		return err
	}
	if err := core.WriteFileAtomic(s.Path, data, 0o644); err != nil {
		s.mu.Lock()
		s.dirty = true
		s.mu.Unlock()
		return fmt.Errorf("writing %s: %w", s.Path, err)
	}
	return nil
}

// Run samples every `every` and writes every `write` until ctx is done. The
// last write is the caller's: see Flush.
func (s *Seen) Run(ctx context.Context, every, write time.Duration) {
	sample := time.NewTicker(every)
	defer sample.Stop()
	flush := time.NewTicker(write)
	defer flush.Stop()
	s.Sample(ctx, time.Now())
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-sample.C:
			s.Sample(ctx, now)
		case now := <-flush.C:
			if err := s.Flush(now); err != nil {
				s.logger().Warn("could not save what the router remembers about devices", "error", err)
			}
		}
	}
}

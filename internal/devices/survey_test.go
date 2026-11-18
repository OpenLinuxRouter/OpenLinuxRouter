package devices

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// switchAdapter is a box that answers reads and forwarding lookups from a fixed
// table, and can be told to stop answering.
type switchAdapter struct {
	mu    sync.Mutex
	table map[string]string
	down  bool
}

func (a *switchAdapter) Read(_ context.Context, d Device) (Snapshot, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.down {
		return Snapshot{}, errors.New("no answer")
	}
	return Snapshot{DeviceID: d.ID, Collections: map[string]Collection{"ports": {State: "ok"}}}, nil
}

func (a *switchAdapter) LookupFDB(_ context.Context, d Device, q FDBQuery) (FDBResult, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.down {
		return FDBResult{}, errors.New("no answer")
	}
	out := FDBResult{DeviceID: d.ID, Entries: []FDBEntry{}}
	if port, ok := a.table[q.MAC]; ok {
		out.Entries = append(out.Entries, FDBEntry{MAC: q.MAC, PortID: port})
	}
	return out, nil
}

func surveyFixture(t *testing.T) (*Surveyor, *switchAdapter, *time.Time) {
	t.Helper()
	store := core.NewStore(filepath.Join(t.TempDir(), "olr.json"), ModuleName, "other")
	devices := []Device{
		{ID: "sw", Adapter: "mercury", Address: "http://192.0.2.2", PasswordEnv: "SW_PASSWORD"},
		{ID: "ap", Adapter: "openwrt", Address: "http://192.0.2.3", PasswordEnv: "AP_PASSWORD"},
	}
	if _, err := Save(store, Config{Devices: devices}); err != nil {
		t.Fatal(err)
	}
	service := NewService(store)
	sw := &switchAdapter{table: map[string]string{"aa:00:00:00:00:01": "3", "aa:00:00:00:00:02": "5"}}
	service.Adapters["mercury"] = sw
	service.Adapters["openwrt"] = &testAdapter{}
	now := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	s := &Surveyor{
		Service: service,
		Addresses: func(context.Context) ([]string, error) {
			return []string{"AA:00:00:00:00:01", "aa:00:00:00:00:02", "aa:00:00:00:00:03", "aa:00:00:00:00:01"}, nil
		},
		MaxAge: time.Minute,
	}
	s.Now = func() time.Time { return now }
	return s, sw, &now
}

// wait polls until no survey is running.
func wait(t *testing.T, s *Surveyor) Survey {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		running := s.last.Running
		got := s.copyLocked()
		s.mu.Unlock()
		if !running {
			return got
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("survey did not finish")
	return Survey{}
}

func TestSurveyServesAtOnceAndFillsInBehind(t *testing.T) {
	s, _, _ := surveyFixture(t)
	first := s.Current()
	if !first.Running || len(first.Devices) != 0 {
		t.Fatalf("first request: %+v", first)
	}
	done := wait(t, s)
	if done.Devices["ap"].Snapshot == nil || done.Devices["sw"].Snapshot == nil {
		t.Fatalf("readings missing: %+v", done.Devices)
	}
	fdb := done.Devices["sw"].FDB
	if len(fdb) != 2 || fdb[0].PortID != "3" || fdb[1].PortID != "5" {
		t.Fatalf("sweep: %+v", fdb)
	}
	if done.Devices["ap"].FDB != nil {
		t.Fatal("a box that cannot be asked has no table")
	}
}

func TestSurveyRefreshesOnlyWhenStale(t *testing.T) {
	s, _, now := surveyFixture(t)
	s.Current()
	wait(t, s)
	if s.Current().Running {
		t.Fatal("a fresh survey started another")
	}
	*now = now.Add(2 * time.Minute)
	if !s.Current().Running {
		t.Fatal("a stale survey did not refresh")
	}
	wait(t, s)
}

func TestSurveyKeepsTheLastAnswerWhenABoxStopsAnswering(t *testing.T) {
	s, sw, now := surveyFixture(t)
	s.Current()
	before := wait(t, s)
	sw.mu.Lock()
	sw.down = true
	sw.mu.Unlock()
	*now = now.Add(2 * time.Minute)
	s.Current()
	after := wait(t, s)
	got := after.Devices["sw"]
	if got.Snapshot == nil || got.ReadError == "" {
		t.Fatalf("reading: %+v", got)
	}
	if len(got.FDB) != len(before.Devices["sw"].FDB) {
		t.Fatalf("table was replaced by an unanswered sweep: %+v", got.FDB)
	}
}

package clients

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

type heardFunc func() ([]string, error)

func (f heardFunc) Heard(context.Context) ([]string, error) { return f() }

type namedFunc func() (map[string]string, error)

func (f namedFunc) Hostnames(context.Context) (map[string]string, error) { return f() }

func TestSeenRecordsAndSurvivesARestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "devices", "seen.json")
	at := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)

	s := NewSeen(path, heardFunc(func() ([]string, error) { return []string{"AA-BB-CC-DD-EE-01"}, nil }), nil, nil)
	s.Sample(context.Background(), at)
	if err := s.Flush(at); err != nil {
		t.Fatal(err)
	}

	again := NewSeen(path, nil, nil, nil)
	got, ok := again.LastSeen("aa:bb:cc:dd:ee:01")
	if !ok || !got.Equal(at) {
		t.Fatalf("after a restart got %v %v, want %v", got, ok, at)
	}
}

func TestSeenRemembersTheNameAClientAnnounced(t *testing.T) {
	path := filepath.Join(t.TempDir(), "devices", "seen.json")
	at := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)

	// The case the record exists for: a lease that announced a name and a
	// neighbour entry that never confirmed an answer. The name is what survives
	// the client leaving, so it is kept even though nothing was heard.
	s := NewSeen(path, nil, namedFunc(func() (map[string]string, error) {
		return map[string]string{"AA-BB-CC-DD-EE-01": "Sams-iPhone"}, nil
	}), nil)
	s.Sample(context.Background(), at)
	if err := s.Flush(at); err != nil {
		t.Fatal(err)
	}

	again := NewSeen(path, nil, nil, nil)
	name, when, ok := again.Hostname("aa:bb:cc:dd:ee:01")
	if !ok || name != "Sams-iPhone" || !when.Equal(at) {
		t.Fatalf("after a restart got %q %v %v, want %q at %v", name, when, ok, "Sams-iPhone", at)
	}
	if _, ok := again.LastSeen("aa:bb:cc:dd:ee:01"); ok {
		t.Error("a name was mistaken for a hearing")
	}
}

func TestSeenNeverMovesBackwards(t *testing.T) {
	s := NewSeen("", nil, nil, nil)
	later := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	s.Record(later, []string{"aa:bb:cc:dd:ee:01"})
	s.Record(later.Add(-time.Hour), []string{"aa:bb:cc:dd:ee:01"})
	if got, _ := s.LastSeen("aa:bb:cc:dd:ee:01"); !got.Equal(later) {
		t.Fatalf("got %v, want %v", got, later)
	}

	s.Remember(later, map[string]string{"aa:bb:cc:dd:ee:01": "current-name"})
	s.Remember(later.Add(-time.Hour), map[string]string{"aa:bb:cc:dd:ee:01": "older-name"})
	if name, at, _ := s.Hostname("aa:bb:cc:dd:ee:01"); name != "current-name" || !at.Equal(later) {
		t.Fatalf("got %q at %v, want %q at %v", name, at, "current-name", later)
	}
}

func TestSeenForgetsWhatIsLongGone(t *testing.T) {
	path := filepath.Join(t.TempDir(), "seen.json")
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	s := NewSeen(path, nil, nil, nil)
	s.Record(now.Add(-Forget-time.Hour), []string{"aa:bb:cc:dd:ee:01"})
	s.Record(now, []string{"aa:bb:cc:dd:ee:02"})
	s.Remember(now.Add(-Forget-time.Hour), map[string]string{"aa:bb:cc:dd:ee:03": "old-name"})
	s.Remember(now, map[string]string{"aa:bb:cc:dd:ee:04": "current-name"})
	if err := s.Flush(now); err != nil {
		t.Fatal(err)
	}
	again := NewSeen(path, nil, nil, nil)
	if _, ok := again.LastSeen("aa:bb:cc:dd:ee:01"); ok {
		t.Error("a device gone past Forget is still on the record")
	}
	if _, ok := again.LastSeen("aa:bb:cc:dd:ee:02"); !ok {
		t.Error("a device heard now was dropped")
	}
	if _, _, ok := again.Hostname("aa:bb:cc:dd:ee:03"); ok {
		t.Error("a name gone past Forget is still on the record")
	}
	if name, _, ok := again.Hostname("aa:bb:cc:dd:ee:04"); !ok || name != "current-name" {
		t.Errorf("a device named now was dropped: %q %v", name, ok)
	}
}

func TestSeenKeepsANameWhileTheDeviceIsStillHeard(t *testing.T) {
	path := filepath.Join(t.TempDir(), "seen.json")
	named := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	s := NewSeen(path, nil, nil, nil)
	s.Remember(named, map[string]string{"aa:bb:cc:dd:ee:01": "phone"})
	// The lease was not renewed for a while, but the client itself was heard
	// recently. Being here is evidence the name is still the client's.
	s.Record(named.Add(Forget-time.Hour), []string{"aa:bb:cc:dd:ee:01"})
	if err := s.Flush(named.Add(Forget + time.Hour)); err != nil {
		t.Fatal(err)
	}
	again := NewSeen(path, nil, nil, nil)
	if name, _, ok := again.Hostname("aa:bb:cc:dd:ee:01"); !ok || name != "phone" {
		t.Fatalf("a heard device lost its name: %q %v", name, ok)
	}
}

func TestSeenAnEmptyNameIsNotAClaim(t *testing.T) {
	s := NewSeen("", nil, nil, nil)
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	s.Remember(now, map[string]string{"aa:bb:cc:dd:ee:01": "  "})
	if _, _, ok := s.Hostname("aa:bb:cc:dd:ee:01"); ok {
		t.Fatal("an empty name was recorded")
	}
}

func TestSeenStartsOverOnAnUnreadableFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "seen.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := NewSeen(path, nil, nil, nil)
	s.Record(time.Now(), []string{"aa:bb:cc:dd:ee:01"})
	if err := s.Flush(time.Now()); err != nil {
		t.Fatalf("could not write over the unreadable file: %v", err)
	}
}

func TestSeenAFailingSourceMovesNothing(t *testing.T) {
	s := NewSeen("", heardFunc(func() ([]string, error) { return nil, errors.New("no netlink") }),
		namedFunc(func() (map[string]string, error) { return nil, errors.New("no leases") }), nil)
	s.Sample(context.Background(), time.Now())
	s.Sample(context.Background(), time.Now())
	if _, ok := s.LastSeen("aa:bb:cc:dd:ee:01"); ok {
		t.Fatal("a failed read recorded something")
	}
	if _, _, ok := s.Hostname("aa:bb:cc:dd:ee:01"); ok {
		t.Fatal("a failed read recorded a name")
	}
}

// One source failing is not the other one's problem: a box that cannot read its
// neighbour table still has a lease file with names in it.
func TestSeenOneSourceFailingLeavesTheOtherWorking(t *testing.T) {
	s := NewSeen("", heardFunc(func() ([]string, error) { return nil, errors.New("no netlink") }),
		namedFunc(func() (map[string]string, error) {
			return map[string]string{"aa:bb:cc:dd:ee:01": "phone"}, nil
		}), nil)
	s.Sample(context.Background(), time.Now())
	if name, _, ok := s.Hostname("aa:bb:cc:dd:ee:01"); !ok || name != "phone" {
		t.Fatalf("a name was lost to an unrelated source: %q %v", name, ok)
	}
}

func TestListCarriesLastSeen(t *testing.T) {
	store := core.NewStore(filepath.Join(t.TempDir(), "olr.json"), ModuleName)
	at := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	seen := NewSeen("", nil, nil, nil)
	seen.Record(at, []string{"aa:bb:cc:dd:ee:01"})
	a := Applier{
		Store:    store,
		Presence: []PresenceSource{fixedSource{arp("aa:bb:cc:dd:ee:01", "192.168.1.5", true), arp("aa:bb:cc:dd:ee:02", "192.168.1.6", true)}},
		Seen:     seen,
	}
	list, _, err := a.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	byMAC := map[string]Resolved{}
	for _, r := range list {
		byMAC[r.MAC] = r
	}
	if got := byMAC["aa:bb:cc:dd:ee:01"].LastSeen; got == nil || !got.Equal(at) {
		t.Errorf("heard device: last seen %v, want %v", got, at)
	}
	if got := byMAC["aa:bb:cc:dd:ee:02"].LastSeen; got != nil {
		t.Errorf("unheard device: last seen %v, want none", got)
	}
}

type fixedSource []Sighting

func (fixedSource) Name() Source { return SourceARP }

func (f fixedSource) Presence(context.Context) ([]Sighting, []Problem, error) { return f, nil, nil }

package clients

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/runtimedb"
)

func TestSeenSQLiteImportKeepsTimesAndNames(t *testing.T) {
	dir := t.TempDir()
	legacy := filepath.Join(dir, "seen.json")
	imported := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	// The file an install upgraded from the file era may have: times only.
	// A v2 file carries names too, and the import is the one path that has to
	// read both shapes.
	data, err := json.Marshal(seenFile{
		Version: 2,
		Seen:    map[string]time.Time{"AA-BB-CC-DD-EE-FF": imported},
		Names:   map[string]seenName{"AA-BB-CC-DD-EE-FF": {Hostname: "old-printer", At: imported}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacy, data, 0o600); err != nil {
		t.Fatal(err)
	}

	db, err := runtimedb.Open(filepath.Join(dir, "runtime.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s, err := OpenSeenDB(db, legacy, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if at, ok := s.LastSeen("aa:bb:cc:dd:ee:ff"); !ok || !at.Equal(imported) {
		t.Fatal("import lost sighting")
	}
	if name, at, ok := s.Hostname("aa:bb:cc:dd:ee:ff"); !ok || name != "old-printer" || !at.Equal(imported) {
		t.Fatalf("import lost the remembered name: %q %v %v", name, at, ok)
	}
}

func TestSeenSQLiteKeepsNamesAndRetention(t *testing.T) {
	dir := t.TempDir()
	db, err := runtimedb.Open(filepath.Join(dir, "runtime.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)

	s, err := OpenSeenDB(db, filepath.Join(dir, "seen.json"), nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	s.Record(now, []string{"aa:bb:cc:dd:ee:ff"})
	s.Remember(now, map[string]string{"aa:bb:cc:dd:ee:ff": "printer"})
	if err := s.Flush(now); err != nil {
		t.Fatal(err)
	}

	// A restart reads both back from the table, not only the only timestamp.
	s, err = OpenSeenDB(db, filepath.Join(dir, "seen.json"), nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if at, ok := s.LastSeen("aa:bb:cc:dd:ee:ff"); !ok || !at.Equal(now) {
		t.Fatalf("restart lost the sighting: %v %v", at, ok)
	}
	if name, _, ok := s.Hostname("aa:bb:cc:dd:ee:ff"); !ok || name != "printer" {
		t.Fatalf("restart lost the name: %q %v", name, ok)
	}

	if err := s.Flush(now.Add(Forget + time.Hour)); err != nil {
		t.Fatal(err)
	}
	s, err = OpenSeenDB(db, filepath.Join(dir, "seen.json"), nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := s.LastSeen("aa:bb:cc:dd:ee:ff"); ok {
		t.Fatal("expired record resurrected")
	}
	if _, _, ok := s.Hostname("aa:bb:cc:dd:ee:ff"); ok {
		t.Fatal("expired name resurrected")
	}
}

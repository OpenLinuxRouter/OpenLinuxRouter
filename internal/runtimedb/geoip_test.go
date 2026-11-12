package runtimedb

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
)

func TestGeoIPMigrationAndEviction(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "runtime.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	// Recreate the preceding schema and exercise the actual upgrade path. The
	// device table is recreated in its version-1 shape too: a database that old
	// has no hostname columns, and the migration adds them.
	revertActivityToAddresses(t, s)
	if _, err := s.db.Exec("DROP TABLE latency_samples; DROP TABLE geoip_cache; DROP TABLE device_seen; CREATE TABLE device_seen (mac TEXT PRIMARY KEY, seen_at TEXT NOT NULL); PRAGMA user_version=1"); err != nil {
		t.Fatal(err)
	}
	if err := s.initialize(); err != nil {
		t.Fatal(err)
	}
	if err := s.transaction(func(tx *sql.Tx) error {
		for i := 0; i < 2000; i++ {
			if _, err := tx.Exec("INSERT INTO geoip_cache(ip,data,fetched,used) VALUES(?,?,?,?)", fmt.Sprint(i), []byte(`{}`), 1, i); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.TouchGeoIP("0", 3000); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveGeoIP(GeoIP{IP: "new", Data: []byte(`{}`), Fetched: 2, Used: 4000}); err != nil {
		t.Fatal(err)
	}
	rows, err := s.GeoIPs()
	if err != nil || len(rows) != 2000 {
		t.Fatalf("cache: %d %v", len(rows), err)
	}
	found := map[string]bool{}
	for _, r := range rows {
		found[r.IP] = true
	}
	if !found["0"] || !found["new"] || found["1"] {
		t.Fatal("LRU eviction ignored disk recency")
	}
}

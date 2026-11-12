package runtimedb

import (
	"path/filepath"
	"testing"
	"time"
)

func TestLatencySamplesUpgradeRestartAndLatestStartedAttempt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.db")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 30, 12, 40, 0, 123, time.UTC)
	if err := db.SaveActivity([]Activity{{Bucket: now.Unix(), Subject: "192.0.2.1", Up: 42}}, 0); err != nil {
		t.Fatal(err)
	}
	revertActivityToAddresses(t, db)
	if _, err := db.db.Exec("DROP TABLE latency_samples; PRAGMA user_version=3"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	db, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	value := 12.5
	hour := now.Truncate(time.Hour)
	first := LatencySample{URL: "https://a.example/", HourStart: hour, CheckedAt: now.Add(-time.Minute), Milliseconds: &value}
	failure := first
	failure.CheckedAt = now
	failure.Milliseconds = nil
	failure.Error = "Timed out"
	old := first
	old.CheckedAt = now.Add(-2 * time.Minute)
	cutoff := now.Add(-48 * time.Hour)
	if err := db.SaveLatencySamples([]LatencySample{first, failure, old}, cutoff, now); err != nil {
		t.Fatal(err)
	}
	route := first
	route.Exit = "Proxy"
	prior := first
	prior.HourStart = hour.Add(-time.Hour)
	prior.CheckedAt = prior.HourStart.Add(30 * time.Minute)
	if err := db.SaveLatencySamples([]LatencySample{route, prior}, cutoff, now); err != nil {
		t.Fatal(err)
	}
	db.Close()
	db, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rows, err := db.LatencySamples(cutoff, now)
	if err != nil || len(rows) != 3 {
		t.Fatalf("restore %d %v", len(rows), err)
	}
	if !rows[0].HourStart.Equal(prior.HourStart) {
		t.Fatal("history not chronological")
	}
	for _, row := range rows {
		if row.Exit == "" && row.HourStart.Equal(hour) && (row.Milliseconds != nil || row.Error != "Timed out" || !row.CheckedAt.Equal(now)) {
			t.Fatalf("failure replaced by older success: %+v", row)
		}
	}
	activity, err := db.Activity(0, now.Unix())
	if err != nil || len(activity) != 1 || activity[0].Up != 42 {
		t.Fatalf("upgrade lost activity %+v %v", activity, err)
	}
	future := first
	future.HourStart = now.Add(time.Hour)
	future.CheckedAt = future.HourStart
	stale := first
	stale.HourStart = cutoff.Add(-time.Hour)
	stale.CheckedAt = stale.HourStart
	if err := db.SaveLatencySamples([]LatencySample{future, stale}, hour, now); err != nil {
		t.Fatal(err)
	}
	rows, err = db.LatencySamples(time.Time{}, now.Add(24*time.Hour))
	if err != nil || len(rows) != 2 {
		t.Fatalf("retention kept stale/future rows: %d %v", len(rows), err)
	}
}

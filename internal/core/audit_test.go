package core

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAuditRecordsAndReadsNewestFirst(t *testing.T) {
	dir := t.TempDir()
	a := NewAudit(dir)

	for _, module := range []string{"dhcp", "dns", "ingress"} {
		a.Record(AuditEntry{Module: module, Method: "PUT", Path: "/api/" + module + "/config", Status: 200})
	}

	entries, err := a.Read(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("got %d entries, want 3", len(entries))
	}
	if entries[0].Module != "ingress" || entries[2].Module != "dhcp" {
		t.Fatalf("entries are not newest first: %+v", entries)
	}
	if entries[0].Outcome != "succeeded" {
		t.Fatalf("outcome = %q, want succeeded", entries[0].Outcome)
	}
	if entries[0].At.IsZero() {
		t.Fatal("entry has no timestamp")
	}
}

func TestAuditLimitAndEmptyRecord(t *testing.T) {
	a := NewAudit(t.TempDir())

	entries, err := a.Read(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("empty record returned %d entries", len(entries))
	}

	for i := 0; i < 12; i++ {
		a.Record(AuditEntry{Module: "link", Status: 200})
	}
	entries, err = a.Read(5)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 5 {
		t.Fatalf("limit ignored: got %d entries", len(entries))
	}
}

func TestAuditOutcomes(t *testing.T) {
	for _, tc := range []struct {
		status int
		want   string
	}{
		{200, "succeeded"},
		{204, "succeeded"},
		{409, "refused"},
		{422, "refused"},
		{500, "failed"},
	} {
		if got := auditOutcome(tc.status); got != tc.want {
			t.Errorf("status %d: outcome %q, want %q", tc.status, got, tc.want)
		}
	}
}

func TestAuditRotatesAndKeepsOldestBound(t *testing.T) {
	dir := t.TempDir()
	a := &Audit{dir: dir, max: 220, keep: 2}

	for i := 0; i < 40; i++ {
		a.Record(AuditEntry{Module: "demo", Method: "PUT", Path: fmt.Sprintf("/api/demo/%d", i), Status: 200})
	}

	// keep=2 means the current segment plus `.1` and `.2`.
	for _, name := range []string{AuditFile, AuditFile + ".1", AuditFile + ".2"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("segment %s missing: %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, AuditFile+".3")); !os.IsNotExist(err) {
		t.Errorf("segment .3 exists beyond keep: %v", err)
	}

	entries, err := a.Read(1000)
	if err != nil {
		t.Fatal(err)
	}
	// Rotation drops history by design: the point is that the record is
	// bounded, not that it is complete.
	if len(entries) == 0 || len(entries) >= 40 {
		t.Fatalf("rotation kept %d of 40 entries; want a bounded slice", len(entries))
	}
	if entries[0].Path != "/api/demo/39" {
		t.Fatalf("newest entry = %q, want the last write", entries[0].Path)
	}
}

func TestAuditSkipsMalformedLines(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, AuditFile)
	body := `{"module":"dns","status":200,"outcome":"succeeded"}` + "\n" +
		"not json\n" +
		`{"module":"link","status":200,"outcome":"succeeded"}` + "\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	a := NewAudit(dir)
	entries, err := a.Read(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2 (malformed line skipped)", len(entries))
	}
}

func TestAuditFileIsNotWorldReadable(t *testing.T) {
	// A subdirectory, so the mode is the one Record creates rather than the
	// one the test harness happened to use (0755 on darwin).
	dir := filepath.Join(t.TempDir(), "audit")
	a := NewAudit(dir)
	a.Record(AuditEntry{Module: "dns", Status: 200})

	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o700 {
		t.Errorf("audit directory mode = %o, want 700", perm)
	}
	info, err = os.Stat(filepath.Join(dir, AuditFile))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("audit file mode = %o, want 600", perm)
	}
}

func TestNilAuditIsSafe(t *testing.T) {
	var a *Audit
	a.Record(AuditEntry{Module: "dns"})
	entries, err := a.Read(10)
	if err != nil || entries != nil {
		t.Fatalf("nil audit: entries=%v err=%v", entries, err)
	}
	if a.Dir() != "" {
		t.Fatalf("nil audit Dir() = %q", a.Dir())
	}
}

func TestAuditSourceOnlyTrustsForwardedForFromLoopback(t *testing.T) {
	for _, tc := range []struct {
		name      string
		remote    string
		forwarded string
		want      string
		via       string
	}{
		{"direct", "192.168.1.10:5555", "", "192.168.1.10", ""},
		{"direct with spoofed header", "192.168.1.10:5555", "203.0.113.9", "192.168.1.10", ""},
		{"caddy", "127.0.0.1:41234", "203.0.113.9", "203.0.113.9", "203.0.113.9"},
		{"caddy after a forged hop", "127.0.0.1:41234", "10.0.0.1, 203.0.113.9", "203.0.113.9", "10.0.0.1, 203.0.113.9"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("PUT", "/api/dns/config", nil)
			r.RemoteAddr = tc.remote
			if tc.forwarded != "" {
				r.Header.Set("X-Forwarded-For", tc.forwarded)
			}
			source, forwarded := auditSource(r)
			if source != tc.want || forwarded != tc.via {
				t.Fatalf("auditSource = (%q, %q), want (%q, %q)", source, forwarded, tc.want, tc.via)
			}
		})
	}
}

// ---------------------------------------------------------------- server

func TestMutatingRouteIsAudited(t *testing.T) {
	dir := t.TempDir()
	s := New()
	s.Audit = NewAudit(dir)

	var gotName string
	s.Mount("demo", []Route{
		{
			Method: "PUT", Path: "/things/{name}", Summary: "Change a thing",
			Mutating: true,
			Handler: func(w http.ResponseWriter, r *http.Request) {
				gotName = r.PathValue("name")
				w.WriteHeader(http.StatusOK)
			},
		},
		{
			Method: "GET", Path: "/things", Summary: "List things",
			Handler: func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) },
		},
	}, nil)

	req := httptest.NewRequest("PUT", "/api/demo/things/rustfs?dry_run=true", nil)
	req.RemoteAddr = "172.16.1.50:1234"
	req.Header.Set("User-Agent", "test-agent")
	s.Handler().ServeHTTP(httptest.NewRecorder(), req)

	if gotName != "rustfs" {
		t.Fatalf("handler saw name %q", gotName)
	}
	entries, err := s.Audit.Read(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(entries))
	}
	e := entries[0]
	if e.Module != "demo" || e.Method != "PUT" || e.Path != "/api/demo/things/rustfs" {
		t.Fatalf("entry = %+v", e)
	}
	if e.Source != "172.16.1.50" {
		t.Fatalf("source = %q", e.Source)
	}
	if e.UserAgent != "test-agent" {
		t.Fatalf("user agent = %q", e.UserAgent)
	}
	if !e.DryRun {
		t.Fatal("dry run was not recorded as such")
	}
}

func TestReadRouteIsNotAudited(t *testing.T) {
	s := New()
	s.Audit = NewAudit(t.TempDir())
	s.Mount("demo", []Route{{
		Method: "GET", Path: "/things", Summary: "List things",
		Handler: func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) },
	}}, nil)

	s.Handler().ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/api/demo/things", nil))

	entries, err := s.Audit.Read(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("a read was recorded: %+v", entries)
	}
}

func TestRefusedChangeIsAuditedAsRefused(t *testing.T) {
	s := New()
	s.Audit = NewAudit(t.TempDir())
	s.Mount("demo", []Route{{
		Method: "DELETE", Path: "/things/{name}", Summary: "Remove a thing",
		Mutating: true,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			WriteError(w, http.StatusConflict, "confirmation required")
		},
	}}, nil)

	s.Handler().ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("DELETE", "/api/demo/things/x", nil))

	entries, err := s.Audit.Read(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Outcome != "refused" || entries[0].Status != http.StatusConflict {
		t.Fatalf("entries = %+v", entries)
	}
}

func TestAuditEndpointAnswersWithAnEmptyRecord(t *testing.T) {
	s := New()
	s.Mount("demo", []Route{{
		Method: "GET", Path: "/things", Summary: "List things",
		Handler: func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) },
	}}, nil)

	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/api/audit", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var body struct {
		Entries []AuditEntry `json:"entries"`
		AsOf    time.Time    `json:"as_of"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("response is not JSON: %v", err)
	}
	if body.Entries == nil || len(body.Entries) != 0 {
		t.Fatalf("entries = %v, want an empty array", body.Entries)
	}
	if body.AsOf.IsZero() {
		t.Fatal("response has no as_of")
	}
}

func TestAuditEndpointRejectsMalformedLimit(t *testing.T) {
	s := New()
	s.Audit = NewAudit(t.TempDir())
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/api/audit?limit=ten", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

// Keep the timestamp type honest: an entry read back must still be a time.
func TestAuditRoundTripKeepsTimestamp(t *testing.T) {
	a := NewAudit(t.TempDir())
	when := time.Date(2026, 11, 13, 18, 50, 26, 0, time.UTC)
	a.Record(AuditEntry{At: when, Module: "ingress", Status: 200})

	entries, err := a.Read(1)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || !entries[0].At.Equal(when) {
		t.Fatalf("timestamp did not survive the round trip: %+v", entries)
	}
}

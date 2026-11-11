package inspection

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const archiveTestMAC = "02:00:00:00:00:01"
const archiveOtherMAC = "02:00:00:00:00:02"

func archiveFixture(t *testing.T, s *Service, mac string, at time.Time, suffix int, method, target string, status int, native []byte) string {
	t.Helper()
	id := fmt.Sprintf("%020d-%032x", at.UnixMicro(), suffix)
	dir := filepath.Join(s.Dir, "history", mac)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	entry := map[string]any{"id": id, "at": at.Unix(), "method": method, "url": target, "status": status, "bytes": len(native), "request": map[string]any{"body": "preview-secret", "headers": []any{map[string]any{"name": "x-test", "value": "one"}}}, "response": nil}
	metadata, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	summary, err := json.Marshal(historyEntry{ID: id, At: float64(at.Unix()), Method: method, URL: target, Status: status, Bytes: int64(len(native))})
	if err != nil {
		t.Fatal(err)
	}
	for extension, data := range map[string][]byte{".mitm": native, ".json": metadata, ".meta": summary} {
		if err := os.WriteFile(filepath.Join(dir, id+extension), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return id
}

func archiveRequest(t *testing.T, s *Service, method, query string) (*httptest.ResponseRecorder, historyResponse) {
	t.Helper()
	request := httptest.NewRequest(method, "/history?mac="+archiveTestMAC+query, nil)
	response := httptest.NewRecorder()
	s.history(response, request)
	var out historyResponse
	if response.Code == http.StatusOK {
		if err := json.Unmarshal(response.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
	}
	return response, out
}

func TestHistoryPagesSearchAndPersistence(t *testing.T) {
	s := &Service{Dir: t.TempDir()}
	now := time.Now().Add(-time.Minute)
	old := archiveFixture(t, s, archiveTestMAC, now, 1, "GET", "https://one.test/old", 200, []byte("old-native"))
	middle := archiveFixture(t, s, archiveTestMAC, now, 2, "POST", "https://two.test/submit", 201, []byte("middle-native"))
	newest := archiveFixture(t, s, archiveTestMAC, now, 3, "GET", "https://one.test/new", 503, []byte("new-native"))
	archiveFixture(t, s, archiveOtherMAC, now, 4, "GET", "https://other.test/", 200, []byte("other-native"))
	response, first := archiveRequest(t, s, http.MethodGet, "&limit=2")
	if response.Code != http.StatusOK || len(first.Entries) != 2 || first.Entries[0].ID != newest || first.Entries[1].ID != middle || first.NextBefore != middle {
		t.Fatalf("first page: %d %+v", response.Code, first)
	}
	if first.TotalBytes <= first.RetainedBytes || first.RetentionDays != 30 || first.MaxBytes != 5*1024*1024*1024 {
		t.Fatalf("storage policy: %+v", first)
	}
	// A fresh Service proves that no session or in-memory index is required.
	response, second := archiveRequest(t, &Service{Dir: s.Dir}, http.MethodGet, "&limit=2&before="+first.NextBefore)
	if response.Code != http.StatusOK || len(second.Entries) != 1 || second.Entries[0].ID != old || second.NextBefore != "" {
		t.Fatalf("second page: %d %+v", response.Code, second)
	}
	_, searched := archiveRequest(t, s, http.MethodGet, "&search="+url.QueryEscape("ONE.TEST"))
	if len(searched.Entries) != 2 {
		t.Fatalf("URL search: %+v", searched)
	}
	_, searched = archiveRequest(t, s, http.MethodGet, "&search=201")
	if len(searched.Entries) != 1 || searched.Entries[0].ID != middle {
		t.Fatalf("status search: %+v", searched)
	}
	_, searched = archiveRequest(t, s, http.MethodGet, "&search=preview-secret")
	if len(searched.Entries) != 0 {
		t.Fatal("search should inspect list metadata only")
	}
	if strings.Contains(response.Body.String(), "preview-secret") {
		t.Fatal("body preview leaked into list")
	}
}

func TestHistoryDownloadAndDetail(t *testing.T) {
	s := &Service{Dir: t.TempDir()}
	native := []byte("native-flow\x00with-request-and-response-bodies")
	id := archiveFixture(t, s, archiveTestMAC, time.Now(), 1, "POST", "https://test/", 200, native)
	request := httptest.NewRequest("GET", "/history/flow?mac="+archiveTestMAC+"&id="+id, nil)
	response := httptest.NewRecorder()
	s.historyFlow(response, request)
	if response.Code != 200 || response.Body.String() != string(native) || !strings.Contains(response.Header().Get("Content-Disposition"), ".mitm") || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("download: %d %s", response.Code, response.Body.String())
	}
	response = httptest.NewRecorder()
	s.historyDetail(response, request)
	if response.Code != 200 || !strings.Contains(response.Body.String(), "preview-secret") || !strings.HasPrefix(response.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("detail: %d %s", response.Code, response.Body.String())
	}
	// A sparse large archive exercises ServeContent's range streaming without
	// allocating a body-sized buffer in the server or test.
	file, err := os.OpenFile(filepath.Join(s.Dir, "history", archiveTestMAC, id+".mitm"), os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(128 * 1024 * 1024); err != nil {
		t.Fatal(err)
	}
	file.Close()
	request.Header.Set("Range", "bytes=0-5")
	response = httptest.NewRecorder()
	s.historyFlow(response, request)
	if response.Code != http.StatusPartialContent || response.Body.String() != "native" {
		t.Fatalf("range: %d %q", response.Code, response.Body.String())
	}
}

func TestHistoryRejectsInvalidAndEscapingPaths(t *testing.T) {
	s := &Service{Dir: t.TempDir()}
	id := archiveFixture(t, s, archiveTestMAC, time.Now(), 1, "GET", "https://test/", 200, []byte("safe"))
	for _, query := range []string{"&limit=0", "&limit=201", "&limit=no", "&before=../../outside"} {
		response, _ := archiveRequest(t, s, http.MethodGet, query)
		if response.Code != http.StatusBadRequest {
			t.Errorf("query %s: %d", query, response.Code)
		}
	}
	for _, query := range []string{"mac=../../outside&id=" + id, "mac=" + archiveTestMAC + "&id=../retention", "mac=" + archiveTestMAC + "&id=" + strings.Repeat("9", 20) + "-" + strings.Repeat("f", 32)} {
		response := httptest.NewRecorder()
		s.historyFlow(response, httptest.NewRequest("GET", "/history/flow?"+query, nil))
		if response.Code != 400 && response.Code != 404 {
			t.Errorf("query %s: %d", query, response.Code)
		}
	}
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret")
	if err := os.WriteFile(secret, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	native := filepath.Join(s.Dir, "history", archiveTestMAC, id+".mitm")
	if err := os.Remove(native); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, native); err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	s.historyFlow(response, httptest.NewRequest("GET", "/history/flow?mac="+archiveTestMAC+"&id="+id, nil))
	if response.Code != http.StatusNotFound || strings.Contains(response.Body.String(), "secret") {
		t.Fatalf("symlink response: %d %s", response.Code, response.Body.String())
	}
	client := filepath.Join(s.Dir, "history", archiveOtherMAC)
	if err := os.Symlink(outside, client); err != nil {
		t.Fatal(err)
	}
	response = httptest.NewRecorder()
	s.historyDetail(response, httptest.NewRequest("GET", "/history/detail?mac="+archiveOtherMAC+"&id="+id, nil))
	if response.Code != http.StatusNotFound {
		t.Fatalf("client symlink: %d", response.Code)
	}
}

func TestHistoryClearKeepsOtherDevicesAndTemporaryFiles(t *testing.T) {
	s := &Service{Dir: t.TempDir()}
	id := archiveFixture(t, s, archiveTestMAC, time.Now().Add(-time.Minute), 1, "GET", "https://test/", 200, []byte("native"))
	other := archiveFixture(t, s, archiveOtherMAC, time.Now().Add(-time.Minute), 2, "GET", "https://other/", 200, []byte("other"))
	pending := filepath.Join(s.Dir, "history", archiveTestMAC, ".in-progress.tmp")
	if err := os.WriteFile(pending, []byte("writing"), 0o600); err != nil {
		t.Fatal(err)
	}
	response, out := archiveRequest(t, s, http.MethodDelete, "")
	if response.Code != 200 || len(out.Entries) != 0 || out.RetainedBytes != 0 {
		t.Fatalf("clear: %d %+v", response.Code, out)
	}
	for _, extension := range []string{".json", ".mitm"} {
		if _, err := os.Stat(filepath.Join(s.Dir, "history", archiveTestMAC, id+extension)); !os.IsNotExist(err) {
			t.Fatalf("file remains: %v", err)
		}
	}
	for _, path := range []string{pending, filepath.Join(s.Dir, "history", archiveOtherMAC, other+".mitm")} {
		if _, err := os.Stat(path); err != nil {
			t.Fatal(err)
		}
	}
}

func TestHistoryClearWatermarkNeverMovesBackward(t *testing.T) {
	s := &Service{Dir: t.TempDir()}
	archiveFixture(t, s, archiveTestMAC, time.Now(), 1, "GET", "https://test/", 200, []byte("native"))
	root, err := s.historyRoot()
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	unlock, err := archiveLock(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	client, err := archiveDevice(root, archiveTestMAC)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	newer := fmt.Sprintf("%020d", time.Now().UnixMicro())
	older := fmt.Sprintf("%020d", time.Now().Add(-time.Minute).UnixMicro())
	if _, err := archiveAdvanceClear(client, newer); err != nil {
		t.Fatal(err)
	}
	cutoff, err := archiveAdvanceClear(client, older)
	if err != nil {
		t.Fatal(err)
	}
	if cutoff != newer {
		t.Fatalf("watermark moved backward: %s != %s", cutoff, newer)
	}
	data, err := os.ReadFile(filepath.Join(s.Dir, "history", archiveTestMAC, ".cleared_before"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != newer {
		t.Fatalf("persisted watermark moved backward: %q", data)
	}
}

func TestHistoryGlobalRetentionOldestFirstAndExpiry(t *testing.T) {
	s := &Service{Dir: t.TempDir()}
	now := time.Now()
	old := archiveFixture(t, s, archiveTestMAC, now.Add(-2*time.Minute), 1, "GET", "https://old/", 200, make([]byte, 600*1024))
	newest := archiveFixture(t, s, archiveOtherMAC, now.Add(-time.Minute), 2, "GET", "https://new/", 200, make([]byte, 600*1024))
	expired := archiveFixture(t, s, archiveOtherMAC, now.Add(-40*24*time.Hour), 3, "GET", "https://expired/", 200, []byte("expired"))
	if err := os.WriteFile(filepath.Join(s.Dir, "retention.json"), []byte(`{"days":30,"max_bytes":1048576}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.SweepHistory(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, pair := range []struct{ mac, id string }{{archiveTestMAC, old}, {archiveOtherMAC, expired}} {
		if _, err := os.Stat(filepath.Join(s.Dir, "history", pair.mac, pair.id+".mitm")); !os.IsNotExist(err) {
			t.Fatalf("old file remains: %s %v", pair.id, err)
		}
	}
	if _, err := os.Stat(filepath.Join(s.Dir, "history", archiveOtherMAC, newest+".mitm")); err != nil {
		t.Fatal(err)
	}
	_, out := archiveRequest(t, s, "GET", "")
	if out.TotalBytes > 1024*1024 || out.MaxBytes != 1024*1024 {
		t.Fatalf("global cap: %+v", out)
	}
}

func TestHistoryEmptyAndIndependentOfSessionMutex(t *testing.T) {
	s := &Service{Dir: t.TempDir()}
	s.mu.Lock()
	defer s.mu.Unlock()
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		response := httptest.NewRecorder()
		s.history(response, httptest.NewRequest("GET", "/history?mac="+archiveTestMAC, nil))
		done <- response
	}()
	select {
	case response := <-done:
		if response.Code != 200 || !strings.Contains(response.Body.String(), `"entries":[]`) {
			t.Fatalf("empty: %d %s", response.Code, response.Body.String())
		}
	case <-time.After(time.Second):
		t.Fatal("history waited on the live session mutex")
	}
}

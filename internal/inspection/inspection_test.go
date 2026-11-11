package inspection

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

func TestStartRequiresIPv4Candidates(t *testing.T) {
	cases := []struct {
		name, ips string
		want      int
	}{
		{"missing", `[]`, http.StatusBadRequest},
		{"too many", `["1.1.1.1","1.1.1.2","1.1.1.3","1.1.1.4","1.1.1.5","1.1.1.6","1.1.1.7","1.1.1.8","1.1.1.9"]`, http.StatusBadRequest},
		{"mixed validity", `["172.16.1.135","224.0.0.1"]`, http.StatusBadRequest},
		{"invalid", `["172.16.1.999"]`, http.StatusBadRequest},
		{"IPv6", `["fd00::1"]`, http.StatusBadRequest},
		{"unspecified", `["0.0.0.0"]`, http.StatusBadRequest},
		{"multicast", `["224.0.0.1"]`, http.StatusBadRequest},
		{"explicit addresses", `["172.16.1.135","172.16.1.39"]`, http.StatusServiceUnavailable},
		{"duplicates", `["172.16.1.135","172.16.1.135"]`, http.StatusServiceUnavailable},
		{"no private restriction", `["8.8.8.8"]`, http.StatusServiceUnavailable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := &Service{Enabled: true}
			req := httptest.NewRequest(http.MethodPost, "/session", strings.NewReader(fmt.Sprintf(`{"mac":"c4:c1:7d:e0:a3:65","ips":%s}`, tc.ips)))
			rec := httptest.NewRecorder()
			// No proxy binary on PATH: valid addresses pass targeting validation.
			t.Setenv("PATH", t.TempDir())
			s.start(rec, req)
			if rec.Code != tc.want || s.snapshot().Active {
				t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestInstallRedirectsDeviceAddresses(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "nft.log")
	script := fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n' \"$*\" >> %q\n", log)
	if err := os.WriteFile(filepath.Join(dir, "nft"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	if err := install("c4:c1:7d:e0:a3:65", []string{"172.16.1.135", "172.16.1.39"}, 0); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "add counter inet olr_inspection redirected") || !strings.Contains(string(data), "ether saddr c4:c1:7d:e0:a3:65 ip saddr { 172.16.1.135, 172.16.1.39 } tcp dport { 80, 443 } counter name redirected redirect to :18081") || !strings.Contains(string(data), "tcp dport 18081 counter name input_seen") || !strings.Contains(string(data), "ether saddr c4:c1:7d:e0:a3:65 ip saddr { 172.16.1.135, 172.16.1.39 } tcp dport 18081 ct original proto-dst { 80, 443 } accept") || !strings.Contains(string(data), "iifname != lo tcp dport 18081 counter name input_dropped drop") || !strings.Contains(string(data), "ip daddr 198.18.0.0/15 return") {
		t.Errorf("missing MAC-bound redirect for client addresses in %s", data)
	}
}

func TestCAReadNeverReturnsKey(t *testing.T) {
	s := &Service{Dir: t.TempDir()}
	rec := httptest.NewRecorder()
	s.ca(rec, httptest.NewRequest(http.MethodGet, "/ca", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("no CA: %d", rec.Code)
	}
}

func TestPublicCAOnlyServesCertificate(t *testing.T) {
	s := &Service{Dir: t.TempDir()}
	get := func() *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		s.PublicCA(rec, httptest.NewRequest(http.MethodGet, "/download/inspection-ca.crt", nil))
		return rec
	}
	if rec := get(); rec.Code != http.StatusNotFound {
		t.Fatalf("missing CA: %d", rec.Code)
	}
	const cert = "-----BEGIN CERTIFICATE-----\npublic\n-----END CERTIFICATE-----\n"
	if err := os.WriteFile(filepath.Join(s.Dir, "mitmproxy-ca-cert.pem"), []byte(cert), 0o600); err != nil {
		t.Fatal(err)
	}
	rec := get()
	if rec.Code != http.StatusOK || rec.Body.String() != cert || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("CA response: %d %q %q", rec.Code, rec.Body.String(), rec.Header().Get("Cache-Control"))
	}
}

func TestStartRejectsStaleListener(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:"+port)
	if err != nil {
		t.Skipf("inspection port unavailable: %v", err)
	}
	defer listener.Close()
	dir := t.TempDir()
	for _, name := range []string{"mitmweb", "nft"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir)
	s := &Service{Enabled: true, Dir: t.TempDir()}
	rec := httptest.NewRecorder()
	s.start(rec, httptest.NewRequest(http.MethodPost, "/session", strings.NewReader(`{"mac":"c4:c1:7d:e0:a3:65","ips":["172.16.1.135"]}`)))
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "already in use") || s.snapshot().Active {
		t.Fatalf("stale listener: %d %s", rec.Code, rec.Body.String())
	}
}

func TestProxyGroupStopsForkedChild(t *testing.T) {
	cmd := exec.Command("sh", "-c", "sleep 30 & wait")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("proxy process group survived cancellation")
	}
}

func TestRedirectCount(t *testing.T) {
	dir := t.TempDir()
	script := `#!/bin/sh
printf '%s\n' '{"nftables":[{"metainfo":{}},{"counter":{"family":"inet","table":"olr_inspection","name":"redirected","packets":12,"bytes":720}}]}'
`
	if err := os.WriteFile(filepath.Join(dir, "nft"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	got, err := inspectionCount("redirected")
	if err != nil || got != 12 {
		t.Fatalf("redirect count = %d, %v", got, err)
	}
}

func TestInstallRoutesProxyThroughExit(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "nft.log")
	script := fmt.Sprintf("#!/bin/sh\nprintf '%%s\n' \"$*\" >> %q\n", log)
	if err := os.WriteFile(filepath.Join(dir, "nft"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	if err := install("c4:c1:7d:e0:a3:65", []string{"172.16.1.135"}, 0x10000); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	if !strings.Contains(got, "type route hook output priority mangle") || !strings.Contains(got, "meta skgid 2147418113 meta mark set 0x10000") || strings.Contains(got, "ip daddr 198.18.0.0/15 return") {
		t.Fatalf("marked inspection rules: %s", got)
	}
}

func TestViewerAccessRequiresSessionAndCookie(t *testing.T) {
	s := &Service{}
	req := httptest.NewRequest(http.MethodPost, "/api/inspection/viewer-access", nil)
	rec := httptest.NewRecorder()
	s.viewerAccess(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("inactive access = %d", rec.Code)
	}
	s.session = &session{webToken: "secret"}
	rec = httptest.NewRecorder()
	s.viewerAccess(rec, req)
	if rec.Code != http.StatusNoContent || len(rec.Result().Cookies()) != 1 {
		t.Fatalf("viewer access = %d", rec.Code)
	}
	cookie := rec.Result().Cookies()[0]
	if !cookie.HttpOnly || cookie.Path != "/inspection-web/" || cookie.SameSite != http.SameSiteStrictMode {
		t.Fatalf("unsafe cookie: %+v", cookie)
	}
	for _, tc := range []struct {
		name   string
		cookie bool
		want   int
	}{
		{"missing cookie", false, http.StatusUnauthorized},
		{"authorized but upstream absent", true, http.StatusBadGateway},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/", nil)
			if tc.cookie {
				request.AddCookie(cookie)
			}
			out := httptest.NewRecorder()
			s.WebUI(out, request)
			if out.Code != tc.want {
				t.Fatalf("status = %d, want %d", out.Code, tc.want)
			}
		})
	}
	s.session = nil
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(cookie)
	s.WebUI(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("stopped viewer = %d", rec.Code)
	}
}

func TestViewerWebSocketUpgrade(t *testing.T) {
	// A 101 response must retain its hop-by-hop upgrade headers. Rewriting it
	// like an HTML response makes ReverseProxy reject the protocol switch.
	listener, err := net.Listen("tcp", "127.0.0.1:18082")
	if err != nil {
		t.Skipf("mitmweb port unavailable: %v", err)
	}
	defer listener.Close()
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/updates" || r.Header.Get("Authorization") != "Bearer secret" || r.Header.Get("Cookie") != "" || r.Header.Get("Origin") != "http://127.0.0.1:18082" {
			http.Error(w, "unexpected request", http.StatusBadRequest)
			return
		}
		conn, rw, err := w.(http.Hijacker).Hijack()
		if err != nil {
			return
		}
		defer conn.Close()
		fmt.Fprint(rw, "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: s3pPLMBiTxaQ9kYGzzhZRbK+xOo=\r\n\r\n")
		rw.Flush()
		// Echo bytes after the upgrade to exercise both tunnel directions.
		payload := make([]byte, 4)
		conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		if _, err := io.ReadFull(rw, payload); err != nil {
			return
		}
		rw.Write(payload)
		rw.Flush()
	})}
	go server.Serve(listener)
	defer server.Close()
	s := &Service{session: &session{webToken: "secret"}}
	mux := http.NewServeMux()
	mux.Handle("/inspection-web/", http.StripPrefix("/inspection-web", http.HandlerFunc(s.WebUI)))
	front := httptest.NewServer(core.WithLogging(core.WithRecovery(core.WithSharedUnits(mux))))
	defer front.Close()
	conn, err := net.Dial("tcp", strings.TrimPrefix(front.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	fmt.Fprintf(conn, "GET /inspection-web/updates HTTP/1.1\r\nHost: %s\r\nOrigin: %s\r\nCookie: %s=secret\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Version: 13\r\nSec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\n\r\n", strings.TrimPrefix(front.URL, "http://"), front.URL, viewerCookie)
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	reader := bufio.NewReader(conn)
	response, err := http.ReadResponse(reader, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("upgrade = %s", response.Status)
	}
	if _, err := conn.Write([]byte("ping")); err != nil {
		t.Fatal(err)
	}
	payload := make([]byte, 4)
	if _, err := io.ReadFull(reader, payload); err != nil || string(payload) != "ping" {
		t.Fatalf("tunnel echo = %q, %v", payload, err)
	}
}

func TestSwitchPersistsAndClears(t *testing.T) {
	dir := t.TempDir()
	s := &Service{Enabled: true, Dir: dir}
	want := &setting{MAC: "c4:c1:7d:e0:a3:65", IPs: []string{"172.16.1.135", "172.16.1.39"}}
	if err := s.writeSetting(want); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, settingFile)
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("setting file: %v %v", info, err)
	}
	got, err := s.readSetting()
	if err != nil || got == nil || got.MAC != want.MAC || len(got.IPs) != 2 {
		t.Fatalf("read back = %+v, %v", got, err)
	}
	if err := s.clearSetting(); err != nil {
		t.Fatal(err)
	}
	if got, err := s.readSetting(); err != nil || got != nil {
		t.Fatalf("after clear = %+v, %v", got, err)
	}
	if err := s.clearSetting(); err != nil {
		t.Fatalf("clearing twice must be quiet: %v", err)
	}
}

func TestResumeKeepsSwitchOnWhenProxyCannotStart(t *testing.T) {
	dir := t.TempDir()
	s := &Service{Enabled: true, Dir: dir}
	if err := s.writeSetting(&setting{MAC: "c4:c1:7d:e0:a3:65", IPs: []string{"172.16.1.135"}}); err != nil {
		t.Fatal(err)
	}
	// No mitmweb on PATH: the restored switch cannot start.
	t.Setenv("PATH", t.TempDir())
	if err := s.Resume(context.Background()); err != nil {
		t.Fatal(err)
	}
	st := s.snapshot()
	if !st.Pending || st.Active || st.MAC != "c4:c1:7d:e0:a3:65" || st.Error == "" {
		t.Fatalf("resumed switch = %+v", st)
	}
	// The failure must not look like the operator turned it off.
	if _, err := os.Stat(filepath.Join(dir, settingFile)); err != nil {
		t.Fatalf("resume dropped the setting: %v", err)
	}
}

func TestResumeWithoutSwitchDoesNothing(t *testing.T) {
	s := &Service{Enabled: true, Dir: t.TempDir()}
	if err := s.Resume(context.Background()); err != nil {
		t.Fatal(err)
	}
	if st := s.snapshot(); st.Active || st.Pending || st.MAC != "" {
		t.Fatalf("resume without a switch = %+v", st)
	}
}

func TestStopClearsPersistedSwitch(t *testing.T) {
	dir := t.TempDir()
	s := &Service{Enabled: true, Dir: dir}
	st := &setting{MAC: "c4:c1:7d:e0:a3:65", IPs: []string{"172.16.1.135"}}
	if err := s.writeSetting(st); err != nil {
		t.Fatal(err)
	}
	s.armed = st
	rec := httptest.NewRecorder()
	s.stop(rec, httptest.NewRequest(http.MethodDelete, "/session", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("stop = %d %s", rec.Code, rec.Body.String())
	}
	if got := s.snapshot(); got.Active || got.Pending || got.MAC != "" {
		t.Fatalf("after stop = %+v", got)
	}
	if _, err := os.Stat(filepath.Join(dir, settingFile)); !os.IsNotExist(err) {
		t.Fatalf("setting survived stop: %v", err)
	}
}

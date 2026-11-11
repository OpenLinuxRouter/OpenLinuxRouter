package inspection

import (
	"bufio"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestTerminateAllowsLauncherCleanup(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "cleaned")
	cmd := exec.Command("sh", "-c", `trap 'printf cleaned > "$1"; exit 0' TERM; printf 'ready\n'; while :; do sleep 1; done`, "proxy", marker)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) })
	if _, err := bufio.NewReader(stdout).ReadString('\n'); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	terminate(-cmd.Process.Pid)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("launcher was killed before cleanup: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("launcher survived termination")
	}
	if data, err := os.ReadFile(marker); err != nil || string(data) != "cleaned" {
		t.Fatalf("launcher did not clean extraction files: %q, %v", data, err)
	}
}

func TestTerminateKillsUnresponsiveGroup(t *testing.T) {
	cmd := exec.Command("sh", "-c", `trap '' TERM; printf 'ready\n'; while :; do sleep 1; done`)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) })
	if _, err := bufio.NewReader(stdout).ReadString('\n'); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	terminate(-cmd.Process.Pid)
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("unresponsive launcher exited without forced termination")
		}
	case <-time.After(time.Second):
		t.Fatal("process group survived forced termination")
	}
}

func TestStartupFailureReportsRedactedDiagnosticsWithoutRedirect(t *testing.T) {
	dir := t.TempDir()
	proxy := `#!/bin/sh
for arg do
  case "$arg" in web_password=*) printf '%s\n' "$arg" >&2 ;; esac
done
printf 'Failed to extract library: No space left on client\n' >&2
exit 1
`
	if err := os.WriteFile(filepath.Join(dir, "mitmweb"), []byte(proxy), 0o700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(dir, "redirected")
	if err := os.WriteFile(filepath.Join(dir, "nft"), []byte("#!/bin/sh\nprintf redirected > '"+marker+"'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	s := &Service{Enabled: true, Dir: t.TempDir(), armed: &setting{MAC: "c4:c1:7d:e0:a3:65", IPs: []string{"172.16.1.135"}}}
	rec := httptest.NewRecorder()
	s.start(rec, httptest.NewRequest(http.MethodPost, "/session", strings.NewReader(`{"mac":"c4:c1:7d:e0:a3:65","ips":["172.16.1.135"]}`)))
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "No space left on client") || !strings.Contains(rec.Body.String(), "web_password=[redacted]") {
		t.Fatalf("startup error = %d %s", rec.Code, rec.Body.String())
	}
	if st := s.snapshot(); st.Active || !st.Pending || !strings.Contains(st.Error, "No space left on client") {
		t.Fatalf("retry status = %+v", st)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("failed startup ran nft: %v", err)
	}
}

func TestProxyDiagnosticsRedactTruncatedToken(t *testing.T) {
	const token = "0123456789abcdef0123456789abcdef0123456789abcdef"
	for cut := 1; cut < len(token); cut++ {
		var log proxyLog
		_, _ = log.Write([]byte("web_password=" + token[:cut]))
		_, _ = log.Write([]byte(token[cut:] + strings.Repeat(".", 4096-(len(token)-cut))))
		if message := log.message(token); message != "[redacted]"+strings.Repeat(".", 4096-(len(token)-cut)) {
			t.Fatalf("token suffix exposed at cut %d: %.60q", cut, message)
		}
	}
}

func TestProxyDiagnosticsStayBounded(t *testing.T) {
	var log proxyLog
	_, _ = log.Write([]byte(strings.Repeat("old", 4096)))
	_, _ = log.Write([]byte("\nlatest error; web_password=viewer-token-0123456789abcdef\n"))
	message := log.message("viewer-token-0123456789abcdef")
	if len(message) > 4096 || !strings.HasSuffix(message, "latest error; web_password=[redacted]") || strings.Contains(message, "viewer-token") {
		t.Fatalf("diagnostic tail length=%d suffix=%q", len(message), message[max(0, len(message)-60):])
	}
}

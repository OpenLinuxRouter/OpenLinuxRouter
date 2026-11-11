package inspection

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

const inspectionUnit = "olr-inspection.service"

// managedState authenticates reattachment to a proxy owned by systemd. It is
// private runtime state, never included in the public status or configuration.
type managedState struct {
	MAC   string   `json:"mac"`
	IPs   []string `json:"ips"`
	Mark  uint32   `json:"mark"`
	Token string   `json:"token"`
	Ready bool     `json:"ready"`
}

func (s *Service) beginActivity(ctx context.Context, mac string, ips []string) error {
	if s.Activity == nil {
		return nil
	}
	err := s.Activity.BeginDevice(ctx, mac, ips)
	if err != nil {
		s.historyProblem = "could not restore activity history: " + err.Error()
	} else {
		s.historyProblem = ""
	}
	return err
}

func (s *Service) rejectManaged(reason error) error {
	if !s.Managed {
		return reason
	}
	return errors.Join(reason, Recover(), s.stopManaged())
}

// ConfigureActivityStorage reports optional analytics failures without making
// the router's control plane depend on writable history storage.
func (s *Service) ConfigureActivityStorage(dir string) error {
	if s.Activity == nil {
		return nil
	}
	if err := s.Activity.SetStorage(dir); err != nil {
		s.historyProblem = "activity history is unavailable: " + err.Error()
		s.Activity = nil
		return err
	}
	return nil
}

func (s *Service) abortManaged(mac string, ips []string, mark uint32, token string, cause error) error {
	cleanErr := cleanup()
	stopErr := s.stopManaged()
	if cleanErr != nil || stopErr != nil {
		q := &session{mac: mac, ips: ips, mark: mark, webToken: token, managed: true, detach: make(chan struct{}), done: make(chan struct{}), retrying: true}
		s.session = q
		go s.retryCleanup(q)
	}
	return errors.Join(cause, cleanErr, stopErr)
}

func (s *Service) openManaged(ctx context.Context, mac string, ips []string, mark uint32, token string, args []string) error {
	// Never treat an unrelated listener as our proxy.
	for _, p := range []string{port, webPort} {
		if conn, err := net.DialTimeout("tcp", "127.0.0.1:"+p, 100*time.Millisecond); err == nil {
			_ = conn.Close()
			return unavailable("inspection port " + p + " is already in use")
		}
	}
	if err := s.startManaged(ctx, mac, ips, mark, token, args); err != nil {
		return unavailable("cannot start inspection service: " + err.Error())
	}
	ready := false
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for !ready {
		if proxyAuthenticated(ctx, token) == nil {
			ready = true
			break
		}
		select {
		case <-ctx.Done():
			_ = s.stopManaged()
			return ctx.Err()
		case <-deadline.C:
			_ = s.stopManaged()
			message := "mitmweb did not become ready; no traffic was redirected"
			logCtx, stopLog := context.WithTimeout(context.Background(), time.Second)
			var log proxyLog
			cmd := exec.CommandContext(logCtx, "journalctl", "--unit", inspectionUnit, "--since", "-1min", "-n", "20", "--no-pager", "-o", "cat")
			cmd.Stdout = &log
			_ = cmd.Run()
			stopLog()
			if detail := log.message(token); detail != "" {
				message += ": " + detail
			}
			return unavailable(message)
		case <-tick.C:
		}
	}
	if err := install(mac, ips, mark); err != nil {
		return s.abortManaged(mac, ips, mark, token, err)
	}
	state := managedState{MAC: mac, IPs: ips, Mark: mark, Token: token, Ready: true}
	data, _ := json.Marshal(state)
	if err := core.WriteFileAtomic(filepath.Join(s.Dir, "proxy.json"), data, 0600); err != nil {
		return s.abortManaged(mac, ips, mark, token, err)
	}
	q := &session{mac: mac, ips: ips, mark: mark, webToken: token, cancel: func() { _ = s.stopManaged() }, done: make(chan struct{}), detach: make(chan struct{}), managed: true}
	s.session = q
	if err := s.beginActivity(ctx, mac, ips); err != nil {
		s.problem = "could not restore activity history: " + err.Error()
	}
	go s.followManaged(q)
	go s.watch(q)
	return nil
}

func (s *Service) unit() core.Unit {
	if s.ProxyUnit != nil {
		return s.ProxyUnit
	}
	u, _ := core.NewUnit(inspectionUnit)
	return u
}

func (s *Service) readManaged() (*managedState, error) {
	data, err := os.ReadFile(filepath.Join(s.Dir, "proxy.json"))
	if err != nil {
		return nil, err
	}
	var st managedState
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, err
	}
	if len(st.Token) != 48 || st.MAC == "" {
		return nil, errors.New("invalid inspection proxy state")
	}
	return &st, nil
}

// systemdArg escapes argv values, including systemd's own specifier and dollar
// expansion. No shell interprets the resulting command line.
func systemdArg(value string) string {
	value = strings.ReplaceAll(value, "%", "%%")
	value = strings.ReplaceAll(value, "$", "$$")
	return strconv.Quote(value)
}

func managedUnit(binary, nftBinary, dir string, args []string, mark uint32) []byte {
	argv := []string{systemdArg(binary)}
	for _, arg := range args {
		argv = append(argv, systemdArg(arg))
	}
	gid := ""
	if mark != 0 {
		gid = fmt.Sprintf("Group=%d\n", proxyGID)
	}
	return []byte(fmt.Sprintf(`[Unit]
Description=Open Linux Router client inspection
After=network.target

[Service]
Type=simple
ExecStart=%s
ExecStopPost=-%s delete table inet olr_inspection
KillMode=control-group
KillSignal=SIGTERM
TimeoutStopSec=5
Restart=no
UMask=0077
Environment=%s
StandardOutput=null
StandardError=journal
%s`, strings.Join(argv, " "), systemdArg(nftBinary), systemdArg("TMPDIR="+filepath.Join(dir, "tmp")), gid))
}

func (s *Service) startManaged(ctx context.Context, mac string, ips []string, mark uint32, token string, args []string) error {
	binary, err := exec.LookPath("mitmweb")
	if err != nil {
		return err
	}
	nftBinary, err := exec.LookPath("nft")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(s.Dir, "tmp"), 0700); err != nil {
		return err
	}
	path := s.UnitPath
	if path == "" {
		path = "/etc/systemd/system/" + inspectionUnit
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	if err := core.WriteFileAtomic(path, managedUnit(binary, nftBinary, s.Dir, args, mark), 0600); err != nil {
		return err
	}
	st := managedState{MAC: mac, IPs: ips, Mark: mark, Token: token}
	data, _ := json.Marshal(st)
	if err := core.WriteFileAtomic(filepath.Join(s.Dir, "proxy.json"), data, 0600); err != nil {
		return err
	}
	if err := core.WriteFileAtomic(filepath.Join(s.Dir, "events.jsonl"), nil, 0600); err != nil {
		return err
	}
	if s.ProxyUnit == nil {
		if err := core.DaemonReload(ctx); err != nil {
			return err
		}
	}
	if err := s.unit().Start(ctx); err != nil {
		return err
	}
	return nil
}

// attachManaged leaves both the proxy and its redirect untouched. Ports alone
// are not proof of identity: mitmweb must accept the saved bearer token too.
func (s *Service) attachManaged(ctx context.Context, mac string, ips []string, mark uint32) (bool, error) {
	st, err := s.readManaged()
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	unitStatus, err := s.unit().Status(ctx)
	if err != nil {
		return false, err
	}
	if !unitStatus.Active {
		return false, nil
	}
	if st.MAC != mac || !slices.Equal(st.IPs, ips) || st.Mark != mark || !st.Ready {
		return false, errors.New("running inspection proxy does not match the selected client or route")
	}
	if err := proxyAuthenticated(ctx, st.Token); err != nil {
		return false, err
	}
	if _, err := inspectionCount("redirected"); err != nil {
		return false, fmt.Errorf("inspection redirect is unavailable: %w", err)
	}
	q := &session{mac: mac, ips: ips, mark: mark, webToken: st.Token, cancel: func() { _ = s.stopManaged() }, done: make(chan struct{}), detach: make(chan struct{}), managed: true}
	s.session = q
	go s.followManaged(q)
	go s.watch(q)
	return true, nil
}

func proxyAuthenticated(ctx context.Context, token string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1:"+webPort+"/", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	client := &http.Client{Timeout: time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	defer client.CloseIdleConnections()
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("inspection viewer authentication failed (%d)", resp.StatusCode)
	}
	conn, err := net.DialTimeout("tcp", "127.0.0.1:"+port, time.Second)
	if err != nil {
		return err
	}
	return conn.Close()
}

func (s *Service) stopManaged() error {
	if s.ProxyUnit == nil {
		path := s.UnitPath
		if path == "" {
			path = "/etc/systemd/system/" + inspectionUnit
		}
		if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
			if _, stateErr := os.Stat(filepath.Join(s.Dir, "proxy.json")); errors.Is(stateErr, os.ErrNotExist) {
				return nil
			}
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	status, err := s.unit().Status(ctx)
	if err != nil {
		return err
	}
	if !status.Active {
		return nil
	}
	return s.unit().Stop(ctx)
}

// followManaged tails the addon's bounded event journal. The independent proxy
// continues writing this journal while the control plane is being redeployed.
func (s *Service) followManaged(q *session) {
	defer close(q.done)
	var file *os.File
	var offset int64
	defer func() {
		if file != nil {
			_ = file.Close()
		}
	}()
	tick := time.NewTicker(250 * time.Millisecond)
	defer tick.Stop()
	lastCheck := time.Time{}
	for {
		select {
		case <-q.detach:
			return
		default:
		}
		path := filepath.Join(s.Dir, "events.jsonl")
		info, err := os.Stat(path)
		if err == nil {
			reopen := file == nil
			if file != nil {
				old, _ := file.Stat()
				reopen = old == nil || !os.SameFile(old, info) || info.Size() < offset
			}
			if reopen {
				if file != nil {
					_ = file.Close()
				}
				file, _ = os.Open(path)
				offset = 0
			}
			if file != nil {
				_, _ = file.Seek(offset, io.SeekStart)
				reader := bufio.NewReaderSize(file, 1<<20)
				for {
					line, readErr := reader.ReadBytes('\n')
					if readErr != nil {
						break
					}
					offset += int64(len(line))
					var event Event
					if json.Unmarshal(line, &event) == nil {
						s.recordEvent(q, event)
					}
				}
			}
		}
		if time.Since(lastCheck) > 2*time.Second {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			status, err := s.unit().Status(ctx)
			cancel()
			if err == nil && !status.Active {
				return
			}
			lastCheck = time.Now()
		}
		select {
		case <-q.detach:
			return
		case <-tick.C:
		}
	}
}

func (s *Service) recordEvent(q *session, event Event) {
	if event.Kind != "request" && event.Kind != "failed" && event.Kind != "accepted" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.session != q {
		return
	}
	if event.Kind == "accepted" {
		q.accepted++
		return
	}
	q.events = append(q.events, event)
	if len(q.events) > maxEvents {
		q.events = q.events[len(q.events)-maxEvents:]
	}
	s.observeName(event, time.Now())
}

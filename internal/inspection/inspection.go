// Package inspection owns one per-client transparent proxy, switched on and
// off from the client page. The setting, CA and bounded request archive survive
// restarts. A systemd-owned proxy keeps capturing across control-plane redeploys.
package inspection

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/x509"
	"embed"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/netip"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

//go:embed addon.py
var addon embed.FS

const ModuleName = "inspection"
const table = "olr_inspection"
const port = "18081"
const webPort = "18082"
const maxEvents = 200

// Reserved only for the short-lived mitmweb process; no system group is created.
const proxyGID = 2147418113

type Event struct {
	Kind            string            `json:"kind"`
	At              float64           `json:"at"`
	Method          string            `json:"method,omitempty"`
	URL             string            `json:"url,omitempty"`
	Status          int               `json:"status,omitempty"`
	Target          string            `json:"target,omitempty"`
	Reason          string            `json:"reason,omitempty"`
	RequestHeaders  map[string]string `json:"request_headers,omitempty"`
	ResponseHeaders map[string]string `json:"response_headers,omitempty"`
	// Client and Server are the pair the activity chart joins on: the client's
	// address and the destination the connection was opened to. The name the
	// request was made under is the URL's host, or Target when a connection
	// failed before there was one.
	Client string `json:"client,omitempty"`
	Server string `json:"server,omitempty"`
}

type Status struct {
	Active       bool   `json:"active"`
	HistoryError string `json:"history_error,omitempty"`
	// Pending is the switch turned on with no proxy behind it: the setting was
	// restored but mitmweb could not start, or the proxy exited. Error says
	// why, so the page can offer a retry instead of showing "off".
	Pending       bool     `json:"pending,omitempty"`
	Error         string   `json:"error,omitempty"`
	MAC           string   `json:"mac,omitempty"`
	IPs           []string `json:"ips,omitempty"`
	Events        []Event  `json:"events"`
	CAPresent     bool     `json:"ca_present"`
	CAFingerprint string   `json:"ca_sha256,omitempty"`
	Redirected    *uint64  `json:"redirected_packets,omitempty"`
	InputSeen     *uint64  `json:"input_packets,omitempty"`
	InputDropped  *uint64  `json:"input_dropped_packets,omitempty"`
	ProxyAccepted uint64   `json:"proxy_accepted"`
	ExitRouted    bool     `json:"exit_routed"`
}

// Flows is the platform's conntrack reader, for callers that own the sampler.
func Flows(ctx context.Context) ([]Flow, error) { return readFlows(ctx) }

type Service struct {
	Enabled bool
	Dir     string
	// Managed leaves the proxy under systemd across control-plane restarts.
	Managed   bool
	ProxyUnit core.Unit
	UnitPath  string
	// ExitMark resolves an explicit per-client exit at session start.
	ExitMark func(string) (uint32, error)
	mu       sync.Mutex
	session  *session
	// armed is the persisted switch: the client it names is being inspected
	// until the operator turns it off. It survives s.session, which only says
	// whether mitmweb is running right now.
	armed *setting
	// problem is why an armed switch has no session, as the client page reads
	// it. Empty while the proxy is running.
	problem        string
	historyProblem string
	// Activity samples while inspection is on and retains per-client history.
	// A nil recorder means this build cannot observe the client's traffic.
	Activity *Activity

	// names is the host each intercepted connection was for, in the shape the
	// activity sampler joins on. It is what the proxy adds to attribution: a
	// connection the relay never named — dialled by address, resolved elsewhere,
	// or opened before the switch — still gets a row of its own instead of
	// 未命名. Locked separately from mu, so the sampler never waits on the
	// session, and always taken after mu, never before it.
	namesMu sync.Mutex
	names   map[nameKey]observedName
}

// observedName is one host a client reached through the proxy.
type observedName struct {
	client netip.Addr
	addr   netip.Addr
	host   string
	at     time.Time
}

// nameKey is (client, destination), the same pairing the relay's map is keyed
// by: two clients reaching one address by different names are different facts.
type nameKey struct {
	client netip.Addr
	addr   netip.Addr
}

// maxObservedNames bounds the proxy's name map. It is small because it covers
// one session: a client left under inspection for a week resolves a few
// thousand hosts at most, and the same recency rule as the relay's map applies.
const maxObservedNames = 1024

type session struct {
	mac      string
	ips      []string
	cancel   func()
	done     chan struct{}
	events   []Event
	accepted uint64
	mark     uint32
	webToken string
	retrying bool
	managed  bool
	detach   chan struct{}
}

func (s *Service) Routes() []core.Route {
	return []core.Route{
		{Method: "GET", Path: "/activity", Tool: "show activity", Query: []core.QueryParam{{Name: "mac", Type: "string", Summary: "Client MAC whose retained samples to read."}, {Name: "range", Type: "string", Summary: "Selected range: 4h, 24h, 7d or 30d."}, {Name: "address", Type: "string", Summary: "Legacy IP address query when mac is omitted."}}, Summary: "Show retained per-site traffic for one client.", Handler: s.activity},
		{Method: "GET", Path: "/status", Tool: "status", Summary: "Show the client request inspection switch and whether its proxy is running.", Handler: s.status},
		{Method: "POST", Path: "/session", Summary: "Turn inspection on for observed client IPv4 addresses; it stays on until turned off.", Mutating: true, Handler: s.start},
		{Method: "DELETE", Path: "/session", Summary: "Stop inspection and remove interception; keep captured history.", Mutating: true, Handler: s.stop},
		{Method: "GET", Path: "/history", Tool: "show history", Query: []core.QueryParam{{Name: "mac", Type: "string", Summary: "Client MAC whose saved requests to read."}, {Name: "before", Type: "string", Summary: "Cursor returned by the preceding page."}, {Name: "limit", Type: "integer", Summary: "Maximum entries, 1 to 200."}, {Name: "search", Type: "string", Summary: "Match method, URL or HTTP status."}}, Summary: "List saved requests for a client.", Handler: s.history},
		{Method: "GET", Path: "/history/detail", Tool: "show request", Query: []core.QueryParam{{Name: "mac", Type: "string", Summary: "Client MAC that owns the archive."}, {Name: "id", Type: "string", Summary: "Saved flow ID from inspection history."}}, Summary: "Read a saved request and response preview.", Handler: s.historyDetail},
		{Method: "GET", Path: "/history/flow", Summary: "Download a complete saved mitmproxy flow.", Handler: s.historyFlow},
		{Method: "DELETE", Path: "/history", Summary: "Delete a client's saved requests and responses.", Mutating: true, Handler: s.history},
		{Method: "GET", Path: "/retention", Tool: "show retention", Summary: "Show inspection archive retention limits.", Handler: s.retention},
		{Method: "PUT", Path: "/retention", Summary: "Set the archive retention period and total disk limit.", Mutating: true, Handler: s.retention},
		{Method: "GET", Path: "/ca", Tool: "show ca", Summary: "Download the public debugging CA certificate, never its private key.", Handler: s.ca},
		{Method: "POST", Path: "/viewer-access", Summary: "Authorize this browser to view the active inspection session.", Mutating: true, Handler: s.viewerAccess},
	}
}

func (s *Service) snapshot() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := Status{Events: []Event{}, HistoryError: s.historyProblem}
	if data, err := os.ReadFile(filepath.Join(s.Dir, "archive-status.json")); err == nil {
		var capture struct {
			Error        string `json:"error"`
			LastFailure  string `json:"last_failure"`
			FailureCount int    `json:"failure_count"`
		}
		if json.Unmarshal(data, &capture) == nil {
			message := capture.Error
			if message == "" && capture.FailureCount > 0 {
				message = "some requests were not saved: " + capture.LastFailure
			}
			out.HistoryError = strings.TrimSpace(out.HistoryError + " " + message)
		} else {
			out.HistoryError = strings.TrimSpace(out.HistoryError + " capture status is unreadable; saved requests may be incomplete")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		out.HistoryError = strings.TrimSpace(out.HistoryError + " capture status is unavailable; saved requests may be incomplete")
	}
	if s.Activity != nil && s.Activity.Problem() != "" {
		out.HistoryError = strings.TrimSpace(out.HistoryError + " " + s.Activity.Problem())
	}
	if q := s.session; q != nil {
		out.Active, out.MAC, out.IPs = true, q.mac, append([]string(nil), q.ips...)
		out.Events = append(out.Events, q.events...)
		out.ProxyAccepted = q.accepted
		out.ExitRouted = q.mark != 0
		out.Error = s.problem
		if n, err := inspectionCount("redirected"); err == nil {
			out.Redirected = &n
		}
		if n, err := inspectionCount("input_seen"); err == nil {
			out.InputSeen = &n
		}
		if n, err := inspectionCount("input_dropped"); err == nil {
			out.InputDropped = &n
		}
	} else if a := s.armed; a != nil {
		out.Pending, out.MAC, out.IPs = true, a.MAC, append([]string(nil), a.IPs...)
		out.Error = s.problem
	}
	if data, err := os.ReadFile(filepath.Join(s.Dir, "mitmproxy-ca-cert.pem")); err == nil {
		out.CAPresent = true
		if block, _ := pem.Decode(data); block != nil {
			if cert, err := x509.ParseCertificate(block.Bytes); err == nil {
				out.CAFingerprint = fmt.Sprintf("%X", sha256.Sum256(cert.Raw))
			}
		}
	}
	return out
}

func (s *Service) status(w http.ResponseWriter, r *http.Request) {
	core.WriteJSON(w, http.StatusOK, s.snapshot())
}

func (s *Service) ca(w http.ResponseWriter, r *http.Request) {
	data, err := os.ReadFile(filepath.Join(s.Dir, "mitmproxy-ca-cert.pem"))
	if err != nil {
		core.WriteError(w, http.StatusNotFound, "start a session to generate the debugging CA")
		return
	}
	// RouteTable/MCP share JSON responses; the PEM is public, the key is not.
	core.WriteJSON(w, http.StatusOK, map[string]string{"pem": string(data)})
}

// PublicCA serves only the public certificate, so a phone can scan a URL
// without receiving the router's administrative API token.
func (s *Service) PublicCA(w http.ResponseWriter, r *http.Request) {
	data, err := os.ReadFile(filepath.Join(s.Dir, "mitmproxy-ca-cert.pem"))
	if err != nil {
		http.Error(w, "start an inspection session to generate the CA", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/x-x509-ca-cert")
	w.Header().Set("Content-Disposition", `attachment; filename="olr-debugging-ca.crt"`)
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(data)
}

const viewerCookie = "olr-inspection-viewer"

func (s *Service) viewerAccess(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	q := s.session
	var token string
	if q != nil {
		token = q.webToken
	}
	s.mu.Unlock()
	if q == nil {
		http.NotFound(w, r)
		return
	}
	if origin := r.Header.Get("Origin"); origin != "" && origin != "http://"+r.Host && origin != "https://"+r.Host {
		http.Error(w, "invalid viewer origin", http.StatusForbidden)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: viewerCookie, Value: token, Path: "/inspection-web/", HttpOnly: true, SameSite: http.SameSiteStrictMode})
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNoContent)
}

// WebUI proxies only a running session to loopback. The browser never receives
// mitmweb's bearer token; an HttpOnly cookie proves access.
func (s *Service) WebUI(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	q := s.session
	var token string
	if q != nil {
		token = q.webToken
	}
	s.mu.Unlock()
	if q == nil {
		http.NotFound(w, r)
		return
	}
	cookie, err := r.Cookie(viewerCookie)
	if err != nil || subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(token)) != 1 {
		http.Error(w, "inspection viewer access required", http.StatusUnauthorized)
		return
	}
	if origin := r.Header.Get("Origin"); origin != "" && origin != "http://"+r.Host && origin != "https://"+r.Host {
		http.Error(w, "invalid viewer origin", http.StatusForbidden)
		return
	}
	if strings.EqualFold(r.Header.Get("Upgrade"), "websocket") && r.URL.Path != "/updates" {
		http.Error(w, "websocket endpoint not allowed", http.StatusForbidden)
		return
	}
	target := &url.URL{Scheme: "http", Host: "127.0.0.1:" + webPort}
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.Transport = &http.Transport{Proxy: nil}
	proxy.Director = func(req *http.Request) {
		req.URL.Scheme, req.URL.Host = target.Scheme, target.Host
		req.Host = target.Host
		req.Header.Del("Cookie")
		req.Header.Set("Authorization", "Bearer "+token)
		if strings.EqualFold(req.Header.Get("Upgrade"), "websocket") {
			req.Header.Set("Origin", "http://"+target.Host)
		}
	}
	proxy.ModifyResponse = func(resp *http.Response) error {
		if resp.StatusCode == http.StatusSwitchingProtocols {
			return nil
		}
		resp.Header.Del("X-Frame-Options")
		resp.Header.Del("Set-Cookie")
		resp.Header.Set("Cache-Control", "no-store")
		resp.Header.Set("Content-Security-Policy", "default-src 'self'; frame-ancestors 'self'; connect-src 'self' ws: wss:; img-src 'self' data:; style-src 'self' 'unsafe-inline'")
		return nil
	}
	proxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, _ error) {
		http.Error(w, "inspection viewer unavailable", http.StatusBadGateway)
	}
	proxy.ServeHTTP(w, r)
}

func (s *Service) start(w http.ResponseWriter, r *http.Request) {
	var body struct {
		MAC string   `json:"mac"`
		IPs []string `json:"ips"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&body); err != nil {
		core.WriteError(w, http.StatusBadRequest, "expected a client MAC and IPv4 addresses")
		return
	}
	if !s.Enabled {
		core.WriteError(w, http.StatusServiceUnavailable, "inspection is available only on the live Linux router")
		return
	}
	normalized, macErr := core.NormalizeMAC(body.MAC)
	if macErr != nil {
		core.WriteError(w, http.StatusBadRequest, macErr.Error())
		return
	}
	if len(body.IPs) == 0 || len(body.IPs) > 8 {
		core.WriteError(w, http.StatusBadRequest, "expected 1 to 8 IPv4 addresses to inspect")
		return
	}
	ips := make([]string, 0, len(body.IPs))
	for _, raw := range body.IPs {
		ip, err := netip.ParseAddr(strings.TrimSpace(raw))
		if err != nil || !ip.Is4() || ip.IsUnspecified() || ip.IsMulticast() || ip.String() == "255.255.255.255" {
			core.WriteError(w, http.StatusBadRequest, "expected unicast IPv4 addresses to inspect")
			return
		}
		if !slices.Contains(ips, ip.String()) {
			ips = append(ips, ip.String())
		}
	}
	s.mu.Lock()
	out, code, message := s.startLocked(normalized, ips, r.Context())
	s.mu.Unlock()
	if code != 0 {
		core.WriteError(w, code, message)
		return
	}
	core.WriteJSON(w, http.StatusOK, out)
}

// startLocked turns the switch on. A first attempt records the setting only
// once the proxy is running, so a failure the operator just saw cannot come
// back after a restart. A retry of an already-armed switch keeps it on and
// replaces the reason with the newer one.
func (s *Service) startLocked(mac string, ips []string, ctx context.Context) (Status, int, string) {
	if s.session != nil {
		return Status{}, http.StatusConflict, "a client is already being inspected"
	}
	if s.armed != nil && !strings.EqualFold(s.armed.MAC, mac) {
		return Status{}, http.StatusConflict, "another client is set to be inspected; turn its switch off first"
	}
	if err := s.open(mac, ips, ctx); err != nil {
		if s.armed != nil {
			s.problem = err.Error()
		}
		var down unavailable
		if errors.As(err, &down) {
			return Status{}, http.StatusServiceUnavailable, err.Error()
		}
		return Status{}, http.StatusInternalServerError, err.Error()
	}
	st := &setting{MAC: mac, IPs: ips}
	if err := s.writeSetting(st); err != nil {
		s.problem = "inspection is running but its setting could not be saved: " + err.Error()
		return s.snapshotLocked(), http.StatusInternalServerError, s.problem
	}
	s.armed, s.problem = st, ""
	return s.snapshotLocked(), 0, ""
}

// open starts mitmweb, installs the redirect and takes ownership of the
// session. It records nothing: start decides what the switch says afterwards,
// and Resume keeps the setting it restored.
func (s *Service) open(mac string, ips []string, ctx context.Context) error {
	var (
		mark uint32
		err  error
	)
	if s.ExitMark != nil {
		mark, err = s.ExitMark(mac)
		if err != nil {
			return s.rejectManaged(unavailable("cannot resolve inspection exit: " + err.Error()))
		}
	}
	if mark&0xff00ffff != 0 || mark == 0x00ff0000 {
		return s.rejectManaged(unavailable("inspection exit has no usable routing mark"))
	}
	if s.Managed {
		attached, attachErr := s.attachManaged(ctx, mac, ips, mark)
		if attached {
			if err := s.beginActivity(ctx, mac, ips); err != nil {
				s.problem = "could not restore activity history: " + err.Error()
			}
			return nil
		}
		// A mismatched or unauthenticated proxy must not retain a redirect.
		if err := errors.Join(cleanup(), s.stopManaged()); err != nil {
			return fmt.Errorf("cannot recover stale inspection proxy: %w", err)
		}
		_ = attachErr // A fresh start below reports the actionable startup error.
	}
	s.killOrphans()
	if _, err = exec.LookPath("mitmweb"); err != nil {
		return unavailable("install mitmproxy (mitmweb) on the router first")
	}
	if _, err = exec.LookPath("nft"); err != nil {
		return unavailable("nft is required for transparent inspection")
	}
	if err = os.MkdirAll(s.Dir, 0o700); err != nil {
		return err
	}
	if err = os.Chmod(s.Dir, 0o700); err != nil {
		return err
	}
	secret := make([]byte, 24)
	if _, err := rand.Read(secret); err != nil {
		return fmt.Errorf("could not create mitmweb access token")
	}
	webToken := hex.EncodeToString(secret)
	script := filepath.Join(s.Dir, "olr-addon.py")
	source, _ := addon.ReadFile("addon.py")
	if err = os.WriteFile(script, source, 0o600); err != nil {
		return err
	}
	args := []string{"--mode", "transparent", "--listen-host", "0.0.0.0", "--listen-port", port,
		"--web-host", "127.0.0.1", "--web-port", webPort, "--no-web-open-browser",
		"--set", "confdir=" + s.Dir, "--set", "termlog_verbosity=error", "--set", "web_password=" + webToken,
		"--set", "olr_device=" + mac, "-s", script}
	if s.Managed {
		return s.openManaged(ctx, mac, ips, mark, webToken, args)
	}
	cmd := exec.Command("mitmweb", args...)
	// mitmdump may fork; its launcher is not the lifetime of the listener.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if mark != 0 {
		cmd.SysProcAttr.Credential = &syscall.Credential{Uid: 0, Gid: proxyGID, NoSetGroups: true}
	}
	stdout, output, err := os.Pipe()
	if err != nil {
		return err
	}
	cmd.Stdout = output
	startupLog := &proxyLog{}
	cmd.Stderr = startupLog
	for _, address := range []string{"127.0.0.1:" + port, "127.0.0.1:" + webPort} {
		if conn, dialErr := net.DialTimeout("tcp", address, 100*time.Millisecond); dialErr == nil {
			conn.Close()
			_ = stdout.Close()
			_ = output.Close()
			return unavailable("inspection port " + address + " is already in use; stop the stale proxy before starting")
		}
	}
	if err = cmd.Start(); err != nil {
		_ = stdout.Close()
		_ = output.Close()
		return err
	}
	_ = output.Close()
	cancel := func() { terminate(-cmd.Process.Pid) }
	// The launcher can exit while its listener child still owns stdout.
	go func() { _ = cmd.Wait() }()
	ready := false
	for i := 0; i < 40; i++ {
		conn, dialErr := net.DialTimeout("tcp", "127.0.0.1:"+port, 100*time.Millisecond)
		if dialErr == nil {
			conn.Close()
			ready = true
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if ready {
		webReady := false
		for i := 0; i < 40; i++ {
			conn, dialErr := net.DialTimeout("tcp", "127.0.0.1:"+webPort, 100*time.Millisecond)
			if dialErr == nil {
				conn.Close()
				webReady = true
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
		ready = webReady
	}
	if !ready {
		cancel()
		_ = stdout.Close()
		message := "mitmweb did not become ready; no traffic was redirected"
		if detail := startupLog.message(webToken); detail != "" {
			message += ": " + detail
		}
		return unavailable(message)
	}
	// A fresh table is owned wholly by this module. Cleanup of a previous
	// daemon's table also runs at startup; never modify someone else's table.
	if err = install(mac, ips, mark); err != nil {
		if cleanupErr := cleanup(); cleanupErr != nil {
			// A partially installed rule might remain. Preserve a session
			// owner so cleanup is retried and no new session can start.
			q := &session{mac: mac, ips: ips, mark: mark, webToken: webToken, cancel: cancel, done: make(chan struct{})}
			s.session = q
			q.retrying = true
			go s.retryCleanup(q)
			_ = stdout.Close()
			return fmt.Errorf("inspection cleanup failed; retrying: %w", cleanupErr)
		}
		cancel()
		_ = stdout.Close()
		return fmt.Errorf("could not install interception: %w", err)
	}
	q := &session{mac: mac, ips: ips, mark: mark, webToken: webToken, cancel: cancel, done: make(chan struct{})}
	s.session = q
	if err := s.beginActivity(ctx, mac, ips); err != nil {
		s.problem = "could not restore activity history: " + err.Error()
	}
	go s.consume(q, stdout)
	go s.watch(q)
	return nil
}

// Resume turns the switch back on after a restart. If the proxy cannot start,
// the setting stays on with the reason in Status.Error rather than pretending
// the operator turned it off.
func (s *Service) Resume(ctx context.Context) error {
	if !s.Enabled {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.session != nil {
		return nil
	}
	// Read under the lock so a switch turned off while startup is still
	// running cannot be resurrected by this read.
	st, err := s.readSetting()
	if err != nil || st == nil {
		if s.Managed {
			if recoverErr := errors.Join(Recover(), s.stopManaged()); recoverErr != nil {
				return errors.Join(err, recoverErr)
			}
		}
		return err
	}
	s.armed = st
	if err := s.open(st.MAC, st.IPs, ctx); err != nil {
		s.problem = err.Error()
		return nil
	}
	s.problem = ""
	return nil
}

func (s *Service) snapshotLocked() Status {
	q := s.session
	if q == nil {
		out := Status{Events: []Event{}, HistoryError: s.historyProblem}
		if a := s.armed; a != nil {
			out.Pending = true
			out.MAC = a.MAC
			out.IPs = append([]string(nil), a.IPs...)
			out.Error = s.problem
		}
		return out
	}
	return Status{Active: true, MAC: q.mac, IPs: append([]string(nil), q.ips...), Events: []Event{}, CAPresent: true, ExitRouted: q.mark != 0, Error: s.problem, HistoryError: s.historyProblem}
}

// Names returns the hosts this session's proxy saw, in the shape the activity
// sampler joins on. It is a copy: the sampler keeps its own index and must not
// read a map a request is folding into.
func (s *Service) Names() []Name {
	if s == nil {
		return nil
	}
	s.namesMu.Lock()
	defer s.namesMu.Unlock()
	out := make([]Name, 0, len(s.names))
	for _, n := range s.names {
		out = append(out, Name{Client: n.client, Addr: n.addr, Name: n.host})
	}
	return out
}

// NamesFrom combines name sources, the first source winning a pairing.
//
// The relay's map covers every client, so it goes first; the proxy's map fills
// in what the relay never saw — a connection dialled by address, resolved
// elsewhere, or opened before the switch. The sampler keeps one name per
// (client, destination), so the earlier source must be the better one.
func NamesFrom(sources ...func(context.Context) ([]Name, error)) func(context.Context) ([]Name, error) {
	return func(ctx context.Context) ([]Name, error) {
		out := make([]Name, 0, 64)
		seen := make(map[nameKey]bool)
		var failure error
		answered := 0
		for _, source := range sources {
			if source == nil {
				continue
			}
			names, err := source(ctx)
			if err != nil {
				failure = err
				continue
			}
			answered++
			for _, n := range names {
				key := nameKey{client: n.Client.Unmap(), addr: n.Addr.Unmap()}
				if seen[key] {
					continue
				}
				seen[key] = true
				out = append(out, n)
			}
		}
		// A source that is down leaves the sample with the others; only when
		// every one failed is there nothing to index, and the sampler keeps the
		// names it already had.
		if answered == 0 && failure != nil {
			return nil, failure
		}
		return out, nil
	}
}

// observeName remembers which host a connection through the proxy was for. The
// caller holds s.mu, which must be taken before namesMu.
//
// A request's URL host is a name the client actually asked for, so this is
// evidence of the same kind as the relay's observation — just evidence the
// relay could not get: a client that dialled an address, resolved over DoH, or
// asked before the switch was on.
func (s *Service) observeName(event Event, now time.Time) {
	host := eventHost(event)
	if host == "" {
		return
	}
	client, err := netip.ParseAddr(event.Client)
	if err != nil {
		return
	}
	addr, err := netip.ParseAddr(event.Server)
	if err != nil {
		return
	}
	key := nameKey{client: client.Unmap(), addr: addr.Unmap()}

	s.namesMu.Lock()
	defer s.namesMu.Unlock()
	if s.names == nil {
		s.names = make(map[nameKey]observedName, 16)
	}
	if _, held := s.names[key]; !held && len(s.names) >= maxObservedNames {
		s.evictNamesLocked()
	}
	s.names[key] = observedName{client: key.client, addr: key.addr, host: host, at: now}
}

// eventHost is the name a request was made under: the URL's host, or the target
// a connection named before it got far enough to have a URL.
func eventHost(event Event) string {
	if event.URL != "" {
		if u, err := url.Parse(event.URL); err == nil && u.Hostname() != "" {
			return u.Hostname()
		}
	}
	if event.Target == "unknown" {
		return ""
	}
	return event.Target
}

// evictNamesLocked drops the tenth of the name map that was seen longest ago.
// The caller holds namesMu.
func (s *Service) evictNamesLocked() {
	if len(s.names) < maxObservedNames {
		return
	}
	oldest := make([]nameKey, 0, len(s.names))
	for key := range s.names {
		oldest = append(oldest, key)
	}
	sort.Slice(oldest, func(i, j int) bool { return s.names[oldest[i]].at.Before(s.names[oldest[j]].at) })
	for _, key := range oldest[:max(1, len(oldest)/10)] {
		delete(s.names, key)
	}
}

func (s *Service) consume(q *session, stdout interface{ Read([]byte) (int, error) }) {
	defer close(q.done)
	defer func() {
		if file, ok := stdout.(*os.File); ok {
			_ = file.Close()
		}
	}()
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 4096), 1<<20)
	for scanner.Scan() {
		var event Event
		if json.Unmarshal(scanner.Bytes(), &event) != nil || (event.Kind != "request" && event.Kind != "failed" && event.Kind != "accepted") {
			continue
		}
		s.mu.Lock()
		if s.session == q {
			if event.Kind == "accepted" {
				q.accepted++
			} else {
				q.events = append(q.events, event)
				s.observeName(event, time.Now())
			}
			if len(q.events) > maxEvents {
				q.events = q.events[len(q.events)-maxEvents:]
			}
		}
		s.mu.Unlock()
	}
}

// watch ends the session when the proxy exits on its own. The switch stays
// where the operator left it; end records why nothing is running.
func (s *Service) watch(q *session) {
	<-q.done
	s.end(q)
}

func (s *Service) end(q *session) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.session != q {
		return
	}
	if err := cleanup(); err != nil {
		// Keep the session visible and retry; do not claim the rule is gone.
		if !q.retrying {
			q.retrying = true
			go s.retryCleanup(q)
		}
		return
	}
	if q.managed {
		if err := s.stopManaged(); err != nil {
			s.problem = "inspection stopped redirecting but its proxy could not stop: " + err.Error()
			if !q.retrying {
				q.retrying = true
				go s.retryCleanup(q)
			}
			return
		}
	} else {
		q.cancel()
	}
	s.session = nil
	if q.managed {
		close(q.detach)
	}
	// Stop sampling while keeping the completed series on disk.
	s.Activity.End()
	// The names were derived from the same session, so they go with it: a later
	// session must not be attributed by what an earlier one happened to see.
	s.namesMu.Lock()
	s.names = nil
	s.namesMu.Unlock()
	if s.armed != nil {
		// The setting outlived the proxy: say so, and let the page retry.
		s.problem = "the inspection proxy exited; retry to start it again"
	}
}

func (s *Service) retryCleanup(q *session) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for range ticker.C {
		s.mu.Lock()
		active := s.session == q
		s.mu.Unlock()
		if !active {
			return
		}
		s.end(q)
	}
}

func (s *Service) stop(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	q := s.session
	s.mu.Unlock()
	if q != nil {
		s.end(q)
	}
	if s.snapshot().Active {
		core.WriteError(w, http.StatusInternalServerError, "interception cleanup failed; retrying automatically")
		return
	}
	// The switch is the durable part: clear it even when there was no proxy,
	// so a failed start cannot leave the client armed across restarts.
	s.mu.Lock()
	s.armed, s.problem = nil, ""
	s.mu.Unlock()
	if err := s.clearSetting(); err != nil {
		core.WriteError(w, http.StatusInternalServerError, "could not clear the inspection setting: "+err.Error())
		return
	}
	core.WriteJSON(w, http.StatusOK, s.snapshot())
}

// Close detaches from a systemd-owned proxy during redeploy. Only the operator's
// switch stops interception. Direct fallback processes retain their old cleanup.
func (s *Service) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	checkpointErr := s.Activity.Checkpoint()
	if s.session == nil {
		return checkpointErr
	}
	if s.session.managed {
		q := s.session
		s.session = nil
		close(q.detach)
		s.Activity.End()
		return checkpointErr
	}
	if err := cleanup(); err != nil {
		return errors.Join(checkpointErr, err)
	}
	s.session.cancel()
	s.session = nil
	s.Activity.End()
	return checkpointErr
}

// unavailable marks a start failure the operator can fix on the router: a
// missing binary, a port something else holds, an exit with no usable mark.
// The API answers 503 for these rather than 500.
type unavailable string

func (e unavailable) Error() string { return string(e) }

func Recover() error {
	if _, err := exec.LookPath("nft"); err != nil {
		return nil
	}
	return cleanup()
}

func nft(args ...string) error {
	out, err := exec.Command("nft", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("nft %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}
func cleanup() error {
	out, err := exec.Command("nft", "delete", "table", "inet", table).CombinedOutput()
	if err == nil || strings.Contains(string(out), "No such file or directory") {
		return nil
	}
	return fmt.Errorf("removing inspection table: %w: %s", err, strings.TrimSpace(string(out)))
}
func install(mac string, ips []string, mark uint32) error {
	// Match both the client MAC and candidate IPv4 addresses before NAT. Exclude
	// local and private destinations so LAN services survive.
	if err := nft("add", "table", "inet", table); err != nil {
		return err
	}
	for _, name := range []string{"redirected", "input_seen", "input_dropped"} {
		if err := nft("add", "counter", "inet", table, name); err != nil {
			return err
		}
	}
	if err := nft("add", "chain", "inet", table, "prerouting", "{ type nat hook prerouting priority dstnat - 1; policy accept; }"); err != nil {
		return err
	}
	if err := nft("add", "chain", "inet", table, "input", "{ type filter hook input priority -1; policy accept; }"); err != nil {
		return err
	}
	if mark != 0 {
		if err := nft("add", "chain", "inet", table, "output", "{ type route hook output priority mangle; policy accept; }"); err != nil {
			return err
		}
		if err := nft("add", "rule", "inet", table, "output", "meta", "skgid", fmt.Sprint(proxyGID), "meta", "mark", "set", fmt.Sprintf("%#x", mark)); err != nil {
			return err
		}
	}
	// Count packets delivered to the local input hook separately from the
	// redirect match, before deciding whether they may enter the listener.
	if err := nft("add", "rule", "inet", table, "input", "iifname", "!=", "lo", "tcp", "dport", port, "counter", "name", "input_seen"); err != nil {
		return err
	}
	// Redirect sets the original destination port in conntrack, but does not
	// necessarily set the DNAT status bit. Permit only this client's
	// connections that originally targeted HTTP(S), not direct LAN proxy use.
	if err := nft("add", "rule", "inet", table, "input", "ether", "saddr", mac, "ip", "saddr", "{", strings.Join(ips, ", "), "}", "tcp", "dport", port, "ct", "original", "proto-dst", "{", "80,", "443", "}", "accept"); err != nil {
		return err
	}
	if err := nft("add", "rule", "inet", table, "input", "iifname", "!=", "lo", "tcp", "dport", port, "counter", "name", "input_dropped", "drop"); err != nil {
		return err
	}
	for _, args := range [][]string{
		{"fib", "daddr", "type", "local", "return"},
		{"ip", "daddr", "10.0.0.0/8", "return"},
		{"ip", "daddr", "172.16.0.0/12", "return"},
		{"ip", "daddr", "192.168.0.0/16", "return"},
	} {
		if err := nft(append([]string{"add", "rule", "inet", table, "prerouting"}, args...)...); err != nil {
			return err
		}
	}
	if mark == 0 {
		// Without an exit mark, mitmdump cannot route a proxy's fake-IP answer.
		if err := nft("add", "rule", "inet", table, "prerouting", "ip", "daddr", "198.18.0.0/15", "return"); err != nil {
			return err
		}
	}
	if err := nft("add", "rule", "inet", table, "prerouting", "ether", "saddr", mac, "ip", "saddr", "{", strings.Join(ips, ", "), "}", "tcp", "dport", "{", "80,", "443", "}", "counter", "name", "redirected", "redirect", "to", ":"+port); err != nil {
		return err
	}
	return nil
}

func inspectionCount(name string) (uint64, error) {
	out, err := exec.Command("nft", "-j", "list", "counter", "inet", table, name).Output()
	if err != nil {
		return 0, err
	}
	var result struct {
		Nftables []struct {
			Counter *struct {
				Packets uint64 `json:"packets"`
			} `json:"counter"`
		} `json:"nftables"`
	}
	if err := json.Unmarshal(out, &result); err != nil {
		return 0, err
	}
	for _, item := range result.Nftables {
		if item.Counter != nil {
			return item.Counter.Packets, nil
		}
	}
	return 0, fmt.Errorf("missing inspection counter")
}

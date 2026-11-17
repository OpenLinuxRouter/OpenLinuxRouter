// Package tools provides on-demand diagnostics run from the router itself.
package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	"github.com/showwin/speedtest-go/speedtest"

	"github.com/open-linux-router/open-linux-router/internal/core"
	"github.com/open-linux-router/open-linux-router/internal/geoip"
)

const ModuleName = "tools"

// serverListLimit caps how many servers the picker returns: enough for a real
// choice, short enough to stay a list rather than a directory.
const serverListLimit = 50

// serverListURL is Ookla's nearby-server payload. One request answers with the
// servers this caller's way out would use, ordered by distance and carrying no
// latency, so opening the picker costs a round trip rather than a ping sweep.
const serverListURL = "https://www.speedtest.net/api/js/servers"

type SpeedResult struct {
	Server   string   `json:"server"`
	Location string   `json:"location"`
	Exit     string   `json:"exit,omitempty"`
	Latency  float64  `json:"latency_ms"`
	Jitter   *float64 `json:"jitter_ms"`
	Download float64  `json:"download_mbps"`
	Upload   float64  `json:"upload_mbps"`
}

type ServerOption struct {
	ID       string `json:"id"`
	Sponsor  string `json:"sponsor"`
	Location string `json:"location"`
}

// ServerList reports the servers the provider lists for one way out, and how
// many it listed before the picker's limit, so a truncated list explains itself.
type ServerList struct {
	Servers []ServerOption `json:"servers"`
	Total   int            `json:"total"`
}

// SpeedProgress is the live state of one speed test.
//
// The page reads it while POST /speedtest is still in flight, so it reports
// only what the run has already observed: the stage it is in, how long it has
// run, and the values that stage settled. Live is the rate the transfer in
// flight is reaching; it is dropped as soon as that transfer ends, because a
// number that has stopped moving is a result rather than progress.
type SpeedProgress struct {
	Running   bool     `json:"running"`
	Provider  string   `json:"provider,omitempty"`
	Exit      string   `json:"exit,omitempty"`
	Phase     string   `json:"phase,omitempty"`
	Server    string   `json:"server,omitempty"`
	Location  string   `json:"location,omitempty"`
	ElapsedMS int64    `json:"elapsed_ms"`
	Latency   *float64 `json:"latency_ms"`
	Download  *float64 `json:"download_mbps"`
	Upload    *float64 `json:"upload_mbps"`
	Live      *float64 `json:"live_mbps,omitempty"`
	Error     string   `json:"error,omitempty"`
}

// The stages a speed test moves through. Ookla runs latency, then download,
// then upload and can report each; the two command-line services measure both
// directions inside one process, so they report transfer for the whole
// measurement rather than a stage they never announced.
const (
	speedPhaseServer   = "server"
	speedPhaseLatency  = "latency"
	speedPhaseDownload = "download"
	speedPhaseUpload   = "upload"
	speedPhaseTransfer = "transfer"
	speedPhaseDone     = "done"
)

// providerServer is one row of Ookla's nearby-server payload, narrowed to the
// fields the picker shows.
type providerServer struct {
	ID      string `json:"id"`
	Sponsor string `json:"sponsor"`
	Name    string `json:"name"`
	Country string `json:"country"`
}

type HTTP struct {
	running     atomic.Bool
	pingRunning atomic.Bool
	tracing     atomic.Bool
	natRunning  atomic.Bool
	quality     qualityRun
	// speedMu guards the progress of the run that holds running. The test
	// writes it from its own goroutines — the Ookla library reports transfer
	// rates from a capture loop — while the page reads it from a poll.
	speedMu      sync.Mutex
	speed        SpeedProgress
	speedStarted time.Time
	speedEnded   time.Time
	// QualityContext follows daemon shutdown; nil is suitable for standalone use.
	QualityContext context.Context
	// QualityPath stores the latest finished assessment; empty disables persistence.
	QualityPath  string
	qualityProbe func(context.Context, qualityTarget, string) QualitySample
	// Wireless reads every managed access point's client table for the Wi-Fi
	// category. Nil means this build has no access point to ask.
	Wireless func(context.Context) (QualityWireless, error)

	Trace  func(context.Context, string) (TraceResult, error)
	RunNAT func(context.Context) (NATResult, error)
	// RunPing is replaceable in tests; nil sends real ICMP echo requests.
	RunPing func(context.Context, string) (PingResult, error)
	// RunEdge is replaceable in tests; nil asks each CDN's own endpoint.
	RunEdge func(context.Context) (EdgeReport, error)
	// DNSLegs names the resolvers this router can ask; nil disables the DNS
	// exit check rather than guessing at one.
	DNSLegs func() ([]DNSLeg, error)
	// Locate resolves the addresses the DNS exit check learned; nil reports
	// addresses without places.
	Locate       func(context.Context, []netip.Addr) (map[string]geoip.Place, error)
	LANAddresses func() ([]string, error)
	LANProbe     func(string) (string, error)
	StartIperf   func(string, int) (func(), <-chan struct{}, error)
	lanMu        sync.Mutex
	lan          *lanSession
	linkMu       sync.Mutex
	linkViewers  []linkTestViewer
	// ResolveExit returns a current fwmark, rejecting disabled or blocked exits.
	ResolveExit func(string) (uint32, error)
	// Run is replaceable in tests; nil uses the actual Ookla network test.
	Run func(context.Context) (SpeedResult, error)
	// RunQuality is replaceable in tests; nil uses the Cloudflare reference
	// transfer for the network quality score.
	RunQuality func(context.Context) (SpeedResult, error)
	// RunCloudflare and RunFast are replaceable in tests; nil runs the installed
	// command-line tools. The mark selects a way out and is zero for the
	// router's default route.
	RunCloudflare func(context.Context, uint32) (SpeedResult, error)
	RunFast       func(context.Context, uint32) (SpeedResult, error)
	// ListServers is replaceable in tests; nil queries Ookla through the
	// selected way out.
	ListServers func(context.Context, string, string) (ServerList, error)
	// LookTool is replaceable in tests; nil searches the router's PATH.
	LookTool func(string) string
}

func (h *HTTP) Routes() []core.Route {
	exit := core.QueryParam{Name: "exit", Type: "string", Summary: "Configured way out; empty uses the router's own default route."}
	search := core.QueryParam{Name: "search", Type: "string", Summary: "Optional word to widen the server list, matched by Ookla against provider, city and country names; empty lists nearby servers."}
	return []core.Route{
		{Method: "GET", Path: "/quality", Tool: "show quality", Summary: "Read the latest network quality assessment, evidence and score coverage without starting tests.", Handler: h.getQuality},
		{Method: "POST", Path: "/quality", Mutating: true, Summary: "Run a full network quality assessment from this router and its managed access points, or rerun one scope and update its scores; the reference transfer consumes bandwidth.", Query: []core.QueryParam{{Name: "scope", Type: "string", Summary: "all (default) runs every test; responses, target, speed, nat and wifi rerun one part and keep the rest."}, {Name: "target", Type: "string", Summary: "Service target ID when scope is target: cloudflare, google or github."}, {Name: "speed", Type: "boolean", Summary: "Include the bandwidth-consuming reference transfer with scope=all; defaults to false."}}, Handler: h.startQuality},
		{Method: "DELETE", Path: "/quality", Mutating: true, Summary: "Cancel the active network quality assessment and retain its completed evidence.", Handler: h.cancelQuality},
		{Method: "GET", Path: "/speedtest/providers", Tool: "show speedtest providers", Summary: "List the speed test services this router can run and whether each command-line tool is installed.", Handler: h.providers},
		{Method: "GET", Path: "/speedtest/servers", Tool: "show speedtest servers", Summary: "List the speed test servers near the selected way out; one network request, no latency probing and no test data.", Query: []core.QueryParam{exit, search}, Handler: h.servers},
		{Method: "GET", Path: "/speedtest/progress", Tool: "show speedtest progress", Summary: "Read the speed test running now: its stage, elapsed time, the rates measured so far, and the last run's outcome when idle.", Handler: h.getSpeedProgress},
		{Method: "POST", Path: "/speedtest", Mutating: true, Summary: "Measure this router's internet latency, download and upload speed against the selected service; consumes bandwidth.", Query: []core.QueryParam{exit, search, {Name: "provider", Type: "string", Summary: "Speed test service: ookla (default, pick a server), cloudflare or fastcom; the command-line services accept a way out."}, {Name: "server_id", Type: "string", Summary: "Speed test server ID from the server list; ookla only, and empty probes the nearby servers and starts with the first that answers."}}, Handler: h.speedtest},
		{Method: "POST", Path: "/nat", Mutating: true, Summary: "Run NatTypeTester’s RFC 3489 and RFC 5780 probes from this router over its default route; sends UDP packets.", Handler: h.nat},
		{Method: "GET", Path: "/lan-test", Tool: "show lan test", Summary: "List LAN addresses and the current temporary iperf3 server session.", Handler: h.lanStatus},
		{Method: "POST", Path: "/lan-test", Mutating: true, Summary: "Start a temporary iperf3 server bound to a configured LAN address; consumes router resources when a client connects.", Handler: h.startLAN},
		{Method: "DELETE", Path: "/lan-test", Mutating: true, Summary: "Stop the temporary iperf3 server.", Handler: h.stopLAN},
		{Method: "POST", Path: "/link-test/session", Mutating: true, Summary: "Authorize this browser to run the embedded LibreSpeed device-to-router test; the cookie it sets is not an API credential.", Handler: h.linkTestSession},
		{Method: "POST", Path: "/ping", Mutating: true, Summary: "Send four ICMP echo requests from this router to a hostname or IP address and report packet loss and latency.", Handler: h.ping},
		{Method: "POST", Path: "/traceroute", Mutating: true, Summary: "Trace the route from this router to a domain or IP using nexttrace; sends network probes.", Handler: h.traceroute},
		{Method: "POST", Path: "/edge", Mutating: true, Summary: "Ask Cloudflare, CloudFront and Fastly which edge point of presence answers this router; sends one HTTPS request per CDN.", Handler: h.edge},
		{Method: "POST", Path: "/dns-exit", Mutating: true, Summary: "Ask each configured resolver where it leaves from, using two whoami names; sends DNS queries from this router.", Handler: h.dnsExit},
		{Method: "POST", Path: "/split-check", Mutating: true, Summary: "Ask a fixed set of public endpoints which address they see this router leave from, and group the answers by address; sends one small request per endpoint.", Handler: h.splitCheck},
	}
}

// exitError marks what the router itself rejected, such as a way out that is
// gone, so a handler answers 400 instead of blaming the network.
type exitError struct{ err error }

func (e exitError) Error() string { return e.err.Error() }
func (e exitError) Unwrap() error { return e.err }

func (h *HTTP) client(exit, search string) (*speedtest.Speedtest, error) {
	config := &speedtest.UserConfig{Keyword: search}
	if exit != "" {
		if h.ResolveExit == nil {
			return nil, exitError{errors.New("exit selection is unavailable")}
		}
		mark, err := h.ResolveExit(exit)
		if err != nil {
			return nil, exitError{err}
		}
		config.DialerControl = markSocket(mark)
	}
	client := speedtest.New(speedtest.WithUserConfig(config))
	// Environment proxy settings would send the transfer through the proxy's
	// route instead of the router default or the requested way out.
	config.T.Proxy = nil
	return client, nil
}

// serverSearch bounds the optional search term. Ookla matches it against
// provider and place names, so it is free text rather than an identifier.
func serverSearch(raw string) (string, error) {
	search := strings.TrimSpace(raw)
	if len(search) > 64 {
		return "", errors.New("server search must be 64 characters or fewer")
	}
	if strings.ContainsFunc(search, unicode.IsControl) {
		return "", errors.New("server search contains control characters")
	}
	return search, nil
}

func (h *HTTP) servers(w http.ResponseWriter, r *http.Request) {
	exit := r.URL.Query().Get("exit")
	search, err := serverSearch(r.URL.Query().Get("search"))
	if err != nil {
		core.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	find := h.ListServers
	if find == nil {
		find = h.listServers
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	list, err := find(ctx, exit, search)
	if err != nil {
		var badExit exitError
		if errors.As(err, &badExit) {
			core.WriteError(w, http.StatusBadRequest, badExit.Error())
			return
		}
		core.WriteError(w, http.StatusServiceUnavailable, "finding test servers: "+err.Error())
		return
	}
	core.WriteJSON(w, http.StatusOK, list)
}

func (h *HTTP) listServers(ctx context.Context, exit, search string) (ServerList, error) {
	client, err := h.client(exit, search)
	if err != nil {
		return ServerList{}, err
	}
	// The speedtest client is the round tripper, so this request carries the
	// selected way out and its user agent, and the list is the one that way out
	// would be offered.
	rows, err := providerServers(ctx, &http.Client{Transport: client}, serverListURL, search)
	if err != nil {
		return ServerList{}, err
	}
	return serverOptions(rows), nil
}

// providerServers asks the provider for its nearby list and returns the rows the
// picker needs.
func providerServers(ctx context.Context, doer *http.Client, endpoint, search string) ([]providerServer, error) {
	target, err := url.Parse(endpoint)
	if err != nil {
		return nil, err
	}
	query := target.Query()
	if search != "" {
		query.Set("search", search)
	}
	target.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := doer.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("provider answered %s", resp.Status)
	}
	var rows []providerServer
	if err := json.NewDecoder(resp.Body).Decode(&rows); err != nil {
		return nil, fmt.Errorf("reading provider response: %w", err)
	}
	return rows, nil
}

// serverOptions keeps the provider's distance order, drops rows the router
// could not test by ID, stops at the picker's limit, and reports how many the
// provider listed so a truncated list explains itself.
func serverOptions(rows []providerServer) ServerList {
	total := 0
	options := make([]ServerOption, 0, min(len(rows), serverListLimit))
	for _, row := range rows {
		if row.ID == "" {
			continue
		}
		total++
		if len(options) < serverListLimit {
			options = append(options, ServerOption{ID: row.ID, Sponsor: row.Sponsor, Location: place(row.Name, row.Country)})
		}
	}
	return ServerList{Servers: options, Total: total}
}

// place joins the two labels Ookla splits a server's site into, without leaving
// a dangling separator when one of them is missing or repeats the other.
func place(name, country string) string {
	switch {
	case name == "":
		return country
	case country == "" || country == name:
		return name
	}
	return name + ", " + country
}

func (h *HTTP) traceroute(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Target string `json:"target"`
	}
	if err := core.DecodeJSON(w, r, &input); err != nil {
		core.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	target := input.Target
	if !validTraceTarget(target) {
		core.WriteError(w, http.StatusUnprocessableEntity, "enter a valid domain name or IP address")
		return
	}
	if !h.tracing.CompareAndSwap(false, true) {
		core.WriteError(w, http.StatusConflict, "a traceroute is already running")
		return
	}
	defer h.tracing.Store(false)
	ctx, cancel := context.WithTimeout(r.Context(), traceTimeout)
	defer cancel()
	run := h.Trace
	if run == nil {
		run = runTrace
	}
	result, err := run(ctx, target)
	if r.Context().Err() != nil {
		return
	}
	if err != nil {
		core.WriteError(w, http.StatusServiceUnavailable, "traceroute failed: "+err.Error())
		return
	}
	core.WriteJSON(w, http.StatusOK, result)
}

// The selectable measurement services. Ookla has a server picker; the other
// two run the command-line tool this package ships.
const (
	speedOokla      = "ookla"
	speedCloudflare = "cloudflare"
	speedFast       = "fastcom"
)

// speedDeadline is how long a run may take before the page is told the service
// never answered. An Ookla run finds a server and measures both directions; the
// command-line tools measure both inside one process, and each gets a second
// attempt inside this budget.
const speedDeadline = 90 * time.Second

// speedAttemptBudget is the share of speedDeadline one command-line attempt may
// spend. A healthy fast.com run takes about 25 seconds and a Cloudflare one
// about 40, so these caps leave room for a retry while still cutting an attempt
// whose connect never completes. It is a variable so tests can shrink the clock.
var speedAttemptBudget = func(provider string) time.Duration {
	if provider == speedCloudflare {
		return 60 * time.Second
	}
	return 40 * time.Second
}

// runSpeedtest runs the selected service and gives a command-line one a second
// attempt when the first produced no result. Those tools resolve hosts and open
// their own connections, so an attempt that lost its SYN to a stalled path is
// usually answered on the next one; Ookla already walks its own server list.
func runSpeedtest(ctx context.Context, provider string, run func(context.Context) (SpeedResult, error)) (SpeedResult, error) {
	result, err := attemptSpeedtest(ctx, provider, run)
	if err == nil || provider == speedOokla || ctx.Err() != nil {
		return result, err
	}
	if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) < speedAttemptBudget(provider)/2 {
		return result, err
	}
	return attemptSpeedtest(ctx, provider, run)
}

func attemptSpeedtest(ctx context.Context, provider string, run func(context.Context) (SpeedResult, error)) (SpeedResult, error) {
	if provider == speedOokla {
		return run(ctx)
	}
	attemptCtx, cancel := context.WithTimeout(ctx, speedAttemptBudget(provider))
	defer cancel()
	return run(attemptCtx)
}

// SpeedProvider is one selectable service and whether this router can run it.
type SpeedProvider struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Available bool   `json:"available"`
	Detail    string `json:"detail,omitempty"`
}

func (h *HTTP) tool(name string) string {
	if h.LookTool != nil {
		return h.LookTool(name)
	}
	return core.LookTool(name)
}

func (h *HTTP) speedProviders() []SpeedProvider {
	providers := []SpeedProvider{
		{ID: speedOokla, Name: "Ookla", Available: true},
		{ID: speedCloudflare, Name: "Cloudflare", Available: h.tool("cloudflare-speed-cli") != ""},
		{ID: speedFast, Name: "fast.com", Available: h.tool("fast-cli") != ""},
	}
	if !providers[1].Available {
		providers[1].Detail = "cloudflare-speed-cli is not installed"
	}
	if !providers[2].Available {
		providers[2].Detail = "fast-cli is not installed"
	}
	return providers
}

func (h *HTTP) providers(w http.ResponseWriter, r *http.Request) {
	core.WriteJSON(w, http.StatusOK, struct {
		Providers []SpeedProvider `json:"providers"`
	}{h.speedProviders()})
}

// exitMark resolves a way out for a command-line tool. An empty name means the
// router's own default route, which needs no mark.
func (h *HTTP) exitMark(exit string) (uint32, error) {
	if exit == "" {
		return 0, nil
	}
	if h.ResolveExit == nil {
		return 0, exitError{errors.New("exit selection is unavailable")}
	}
	mark, err := h.ResolveExit(exit)
	if err != nil {
		return 0, exitError{err}
	}
	return mark, nil
}

// providerRun returns the measurement for a command-line service, honouring a
// test seam before the installed tool.
func (h *HTTP) providerRun(provider string, mark uint32) func(context.Context) (SpeedResult, error) {
	var run func(context.Context, uint32) (SpeedResult, error)
	if provider == speedCloudflare {
		run = h.RunCloudflare
		if run == nil {
			run = runCloudflare
		}
	} else {
		run = h.RunFast
		if run == nil {
			run = runFast
		}
	}
	return func(ctx context.Context) (SpeedResult, error) { return run(ctx, mark) }
}

// The progress of the run that holds running. Every field goes through speedMu
// because the measurement's own goroutines write it while a poll reads it.
func (h *HTTP) startSpeed(provider, exit, phase string) {
	h.speedMu.Lock()
	defer h.speedMu.Unlock()
	h.speed = SpeedProgress{Running: true, Provider: provider, Exit: exit, Phase: phase}
	h.speedStarted, h.speedEnded = time.Now(), time.Time{}
}

func (h *HTTP) setSpeedPhase(phase string) {
	h.speedMu.Lock()
	defer h.speedMu.Unlock()
	if h.speed.Running {
		// A new stage has measured nothing yet, and the previous stage's rate is
		// not its starting point.
		h.speed.Phase, h.speed.Live = phase, nil
	}
}

func (h *HTTP) setSpeedServer(server, location string) {
	h.speedMu.Lock()
	defer h.speedMu.Unlock()
	if h.speed.Running {
		h.speed.Server, h.speed.Location = server, location
	}
}

// setSpeedValue records one settled stage. The live rate goes with it: the
// measured value now describes the stage, and two numbers for one stage would
// disagree the moment the transfer stopped.
func (h *HTTP) setSpeedValue(phase string, value float64) {
	h.speedMu.Lock()
	defer h.speedMu.Unlock()
	if !h.speed.Running {
		return
	}
	switch phase {
	case speedPhaseLatency:
		h.speed.Latency = &value
	case speedPhaseDownload:
		h.speed.Download = &value
	case speedPhaseUpload:
		h.speed.Upload = &value
	}
	h.speed.Live = nil
}

// setSpeedLive records the rate one direction is reaching right now. The
// library's callback keeps firing for as long as that transfer runs, so only
// the direction actually being measured may publish: a late callback must not
// make a finished stage look like it is still moving.
func (h *HTTP) setSpeedLive(direction string, rate float64) {
	h.speedMu.Lock()
	defer h.speedMu.Unlock()
	if h.speed.Running && h.speed.Phase == direction && rate > 0 {
		h.speed.Live = &rate
	}
}

func (h *HTTP) finishSpeed(result *SpeedResult, err error) {
	h.speedMu.Lock()
	defer h.speedMu.Unlock()
	h.speed.Running, h.speed.Live = false, nil
	h.speedEnded = time.Now()
	if err != nil {
		h.speed.Error = err.Error()
		return
	}
	h.speed.Phase = speedPhaseDone
	if result != nil {
		latency, download, upload := result.Latency, result.Download, result.Upload
		h.speed.Latency, h.speed.Download, h.speed.Upload = &latency, &download, &upload
		h.speed.Server, h.speed.Location = result.Server, result.Location
	}
}

// speedProgress copies the state for a reader. Elapsed is measured from the
// run's own start on the router's clock, so two readers cannot disagree about
// how long the test has taken, and a page that polled late is not told less.
func (h *HTTP) speedProgress() SpeedProgress {
	h.speedMu.Lock()
	defer h.speedMu.Unlock()
	progress := h.speed
	end := h.speedEnded
	if progress.Running || end.IsZero() {
		end = time.Now()
	}
	if !h.speedStarted.IsZero() {
		progress.ElapsedMS = end.Sub(h.speedStarted).Milliseconds()
	}
	return progress
}

func (h *HTTP) getSpeedProgress(w http.ResponseWriter, r *http.Request) {
	core.WriteJSON(w, http.StatusOK, h.speedProgress())
}

// speedReport carries a run's milestones into the progress state. Its zero
// value reports nowhere, which keeps the measurement code usable from tests and
// from any caller with no page watching.
type speedReport struct{ h *HTTP }

func (r speedReport) phase(phase string) {
	if r.h != nil {
		r.h.setSpeedPhase(phase)
	}
}

func (r speedReport) server(server, location string) {
	if r.h != nil {
		r.h.setSpeedServer(server, location)
	}
}

func (r speedReport) value(phase string, value float64) {
	if r.h != nil {
		r.h.setSpeedValue(phase, value)
	}
}

// attach installs the transfer callbacks. The client recreates its rate
// directions on Reset, which drops anything set before it, so this runs after
// each Reset and before the transfers it reports on.
func (r speedReport) attach(client *speedtest.Speedtest) {
	if r.h == nil {
		return
	}
	client.SetCallbackDownload(func(rate speedtest.ByteRate) { r.h.setSpeedLive(speedPhaseDownload, rate.Mbps()) })
	client.SetCallbackUpload(func(rate speedtest.ByteRate) { r.h.setSpeedLive(speedPhaseUpload, rate.Mbps()) })
}

func (h *HTTP) speedtest(w http.ResponseWriter, r *http.Request) {
	if !h.running.CompareAndSwap(false, true) {
		core.WriteError(w, http.StatusConflict, "a speed test is already running")
		return
	}
	defer h.running.Store(false)

	query := r.URL.Query()
	provider := query.Get("provider")
	if provider == "" {
		provider = speedOokla
	}
	switch provider {
	case speedOokla, speedCloudflare, speedFast:
	default:
		core.WriteError(w, http.StatusBadRequest, "unknown speed test service")
		return
	}
	exit, id := query.Get("exit"), query.Get("server_id")
	if len(id) > 32 || strings.ContainsAny(id, " \t\r\n") {
		core.WriteError(w, http.StatusBadRequest, "invalid server ID")
		return
	}
	search, err := serverSearch(query.Get("search"))
	if err != nil {
		core.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	var run func(context.Context) (SpeedResult, error)
	report := speedReport{h}
	// The phase a run opens in is all the handler can say before the
	// measurement itself starts: Ookla settles on a server first, while the
	// command-line services measure both directions inside one process.
	phase := speedPhaseTransfer
	if provider == speedOokla {
		phase = speedPhaseServer
		client, err := h.client(exit, search)
		if err != nil {
			core.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
		if run = h.Run; run == nil {
			run = func(ctx context.Context) (SpeedResult, error) { return measure(ctx, client, id, report) }
		}
	} else {
		mark, err := h.exitMark(exit)
		if err != nil {
			var badExit exitError
			if errors.As(err, &badExit) {
				core.WriteError(w, http.StatusBadRequest, badExit.Error())
				return
			}
			core.WriteError(w, http.StatusServiceUnavailable, err.Error())
			return
		}
		run = h.providerRun(provider, mark)
	}
	h.startSpeed(provider, exit, phase)
	ctx, cancel := context.WithTimeout(r.Context(), speedDeadline)
	defer cancel()
	result, err := runSpeedtest(ctx, provider, run)
	if err != nil {
		h.finishSpeed(nil, err)
		if r.Context().Err() != nil {
			return
		}
		if errors.Is(err, context.DeadlineExceeded) {
			// This handler sets the only deadline a run can outlive, and
			// "context deadline exceeded" says nothing about what to try next.
			core.WriteError(w, http.StatusServiceUnavailable, fmt.Sprintf(
				"speed test failed: no result within %d seconds; the service or the way out did not finish the transfer",
				int(speedDeadline/time.Second)))
			return
		}
		core.WriteError(w, http.StatusServiceUnavailable, "speed test failed: "+err.Error())
		return
	}
	result.Exit = exit
	h.finishSpeed(&result, nil)
	core.WriteJSON(w, http.StatusOK, result)
}

func measure(ctx context.Context, client *speedtest.Speedtest, id string, report speedReport) (SpeedResult, error) {
	client.SetCaptureTime(10 * time.Second).SetNThread(4)
	if id != "" {
		// The picker already knows which server the user wants; asking Ookla for
		// that one server keeps the chosen path free of a ping sweep over the
		// rest, and still catches an ID the provider has retired.
		report.phase(speedPhaseServer)
		server, err := client.FetchServerByIDContext(ctx, id)
		if errors.Is(err, speedtest.ErrServerNotFound) {
			return SpeedResult{}, fmt.Errorf("selected server %q is no longer available; refresh the list", id)
		}
		if err != nil {
			return SpeedResult{}, fmt.Errorf("finding test server %s: %w", id, err)
		}
		client.Reset()
		report.attach(client)
		return testServer(ctx, server, report)
	}
	report.phase(speedPhaseServer)
	servers, err := client.FetchServerListContext(ctx)
	if err != nil {
		return SpeedResult{}, fmt.Errorf("finding test servers: %w", err)
	}
	available := *servers.Available()
	if len(available) == 0 {
		return SpeedResult{}, errors.New("no reachable test server available")
	}
	return tryServers(ctx, available, func(ctx context.Context, server *speedtest.Server) (SpeedResult, error) {
		client.Reset()
		report.attach(client)
		return testServer(ctx, server, report)
	})
}

func tryServers(ctx context.Context, servers speedtest.Servers, run func(context.Context, *speedtest.Server) (SpeedResult, error)) (SpeedResult, error) {
	var lastErr error
	for i, server := range servers {
		if i == 3 || ctx.Err() != nil {
			break
		}
		result, err := run(ctx, server)
		if err == nil {
			return result, nil
		}
		lastErr = fmt.Errorf("%s (%s): %w", server.Sponsor, server.Name, err)
	}
	if ctx.Err() != nil {
		return SpeedResult{}, ctx.Err()
	}
	if lastErr == nil {
		return SpeedResult{}, errors.New("no test server available")
	}
	return SpeedResult{}, fmt.Errorf("no usable test server among the first three candidates; last error: %w", lastErr)
}

func testServer(ctx context.Context, server *speedtest.Server, report speedReport) (SpeedResult, error) {
	report.server(server.Sponsor, place(server.Name, server.Country))
	report.phase(speedPhaseLatency)
	if err := server.PingTestContext(ctx, nil); err != nil {
		return SpeedResult{}, fmt.Errorf("measuring latency: %w", err)
	}
	latency := float64(server.Latency) / float64(time.Millisecond)
	report.value(speedPhaseLatency, latency)
	report.phase(speedPhaseDownload)
	if err := server.DownloadTestContext(ctx); err != nil || ctx.Err() != nil {
		return SpeedResult{}, fmt.Errorf("measuring download: %w", firstError(err, ctx.Err()))
	}
	if server.DLSpeed <= 0 {
		return SpeedResult{}, errors.New("download returned no usable data")
	}
	report.value(speedPhaseDownload, server.DLSpeed.Mbps())
	report.phase(speedPhaseUpload)
	if err := server.UploadTestContext(ctx); err != nil || ctx.Err() != nil {
		return SpeedResult{}, fmt.Errorf("measuring upload: %w", firstError(err, ctx.Err()))
	}
	if server.ULSpeed <= 0 {
		return SpeedResult{}, errors.New("upload returned no usable data")
	}
	report.value(speedPhaseUpload, server.ULSpeed.Mbps())
	return SpeedResult{
		Server: server.Sponsor, Location: place(server.Name, server.Country),
		Latency:  latency,
		Jitter:   qualityMS(server.Jitter),
		Download: server.DLSpeed.Mbps(), Upload: server.ULSpeed.Mbps(),
	}, nil
}

func firstError(err, fallback error) error {
	if err != nil {
		return err
	}
	return fallback
}

func (h *HTTP) nat(w http.ResponseWriter, r *http.Request) {
	if !h.natRunning.CompareAndSwap(false, true) {
		core.WriteError(w, http.StatusConflict, "a NatTypeTester test is already running")
		return
	}
	defer h.natRunning.Store(false)
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	run := h.RunNAT
	if run == nil {
		run = runNAT
	}
	result, err := run(ctx)
	if r.Context().Err() != nil {
		return
	}
	if err != nil {
		core.WriteError(w, http.StatusServiceUnavailable, "NatTypeTester failed: "+err.Error())
		return
	}
	core.WriteJSON(w, http.StatusOK, result)
}

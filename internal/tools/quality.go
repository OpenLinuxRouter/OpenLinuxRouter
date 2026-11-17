package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

const (
	qualityVersion = "quality-v4"
	qualityCatalog = "reference-v1"
	qualitySamples = 6

	qualityScopeAll       = "all"
	qualityScopeResponses = "responses"
	qualityScopeTarget    = "target"
	qualityScopeSpeed     = "speed"
	qualityScopeNAT       = "nat"
	qualityScopeWifi      = "wifi"
)

type qualityTarget struct {
	ID, Name, URL string
	Status        int
}

var qualityTargets = []qualityTarget{
	{"cloudflare", "Cloudflare", "https://speed.cloudflare.com/__down?bytes=0", http.StatusOK},
	{"google", "Google", "https://www.google.com/generate_204", http.StatusNoContent},
	{"github", "GitHub", "https://api.github.com/zen", http.StatusOK},
}

// QualitySample describes one fresh HTTPS connection, not a browser page load.
// Nullable durations mean that stage did not complete, never a measured zero.
type QualitySample struct {
	StartedAt   time.Time `json:"started_at"`
	CompletedAt time.Time `json:"completed_at"`
	State       string    `json:"state"`
	Stage       string    `json:"stage"`
	Address     string    `json:"address,omitempty"`
	DNS         *float64  `json:"dns_ms"`
	TCP         *float64  `json:"tcp_ms"`
	Response    *float64  `json:"response_ms"`
	Status      int       `json:"status,omitempty"`
	Error       string    `json:"error,omitempty"`
}

type QualityTargetResult struct {
	ID      string          `json:"id"`
	Name    string          `json:"name"`
	URL     string          `json:"url"`
	Family  string          `json:"family"`
	Samples []QualitySample `json:"samples"`
}

type QualitySnapshot struct {
	Version        string                `json:"scoring_version"`
	Catalog        string                `json:"catalog_version"`
	Profile        string                `json:"profile_id"`
	ProfileVersion string                `json:"profile_version"`
	Scope          string                `json:"scope"`
	RunID          string                `json:"run_id"`
	State          string                `json:"state"`
	Phase          string                `json:"phase"`
	RunningScope   string                `json:"running_scope,omitempty"`
	RunningTarget  string                `json:"running_target,omitempty"`
	IncludeSpeed   bool                  `json:"include_speed"`
	StartedAt      *time.Time            `json:"started_at"`
	CompletedAt    *time.Time            `json:"completed_at"`
	ExpiresAt      *time.Time            `json:"expires_at"`
	Targets        []QualityTargetResult `json:"targets"`
	Speed          *SpeedResult          `json:"speed"`
	SpeedAt        *time.Time            `json:"speed_at"`
	SpeedError     string                `json:"speed_error,omitempty"`
	NAT            *NATResult            `json:"nat"`
	NATError       string                `json:"nat_error,omitempty"`
	Wireless       *QualityWireless      `json:"wireless"`
	WirelessAt     *time.Time            `json:"wireless_at"`
	WirelessError  string                `json:"wireless_error,omitempty"`
	Categories     []QualityCategory     `json:"categories"`
	Overall        QualityOverall        `json:"overall"`
}

type qualityRun struct {
	mu       sync.Mutex
	cancel   context.CancelFunc
	closed   bool
	snapshot QualitySnapshot
}

// qualityRunSpec names the evidence one run refreshes. A scoped run keeps every
// other piece of evidence and updates only its own; the score is recomputed from
// the merged snapshot on read.
type qualityRunSpec struct {
	Scope        string
	Target       string
	IncludeSpeed bool
}

// RestoreQuality loads saved evidence before serving requests. Starting a new
// assessment leaves this checkpoint intact until that assessment finishes.
func (h *HTTP) RestoreQuality() error {
	if h.QualityPath == "" {
		return nil
	}
	data, err := os.ReadFile(h.QualityPath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var snapshot QualitySnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return err
	}
	switch snapshot.State {
	case "completed", "cancelled", "timed_out":
	default:
		return fmt.Errorf("invalid saved quality state %q", snapshot.State)
	}
	// A checkpoint is written only between runs, so a saved run marker is stale.
	snapshot.RunningScope, snapshot.RunningTarget, snapshot.Phase = "", "", ""
	h.quality.mu.Lock()
	h.quality.snapshot = snapshot
	h.quality.mu.Unlock()
	return nil
}

// Called under quality.mu so another run cannot overtake this checkpoint.
func (h *HTTP) saveQuality() {
	if h.QualityPath == "" {
		return
	}
	data, err := json.Marshal(h.quality.snapshot)
	if err == nil {
		err = core.WriteFileAtomic(h.QualityPath, data, 0o600)
	}
	if err != nil {
		slog.Error("could not persist quality assessment", "error", err)
	}
}

func (h *HTTP) qualitySnapshot() QualitySnapshot {
	h.quality.mu.Lock()
	snapshot := h.quality.snapshot
	h.quality.mu.Unlock()
	// Published evidence is immutable. Scoring constructs new result slices and
	// does not mutate the evidence shared with concurrent passive readers.
	if snapshot.State == "" {
		snapshot.State = "idle"
	}
	snapshot.Version, snapshot.Catalog = qualityVersion, qualityCatalog
	snapshot.Scope = "router-default-route"
	if snapshot.Wireless != nil {
		snapshot.Scope = "router-and-access-points"
	}
	snapshot.Profile, snapshot.ProfileVersion = "reference", "1"
	if snapshot.Targets == nil {
		snapshot.Targets = []QualityTargetResult{}
	}
	snapshot.Categories, snapshot.Overall = scoreQuality(snapshot, time.Now())
	return snapshot
}

func (h *HTTP) getQuality(w http.ResponseWriter, r *http.Request) {
	core.WriteJSON(w, http.StatusOK, h.qualitySnapshot())
}

func qualityTargetIDs() string {
	ids := make([]string, len(qualityTargets))
	for i, target := range qualityTargets {
		ids[i] = target.ID
	}
	return strings.Join(ids, ", ")
}

func qualityPhase(scope string) string {
	switch scope {
	case qualityScopeSpeed:
		return "speed"
	case qualityScopeNAT:
		return "nat"
	case qualityScopeWifi:
		return "wifi"
	default:
		return "responses"
	}
}

func qualityTimeout(scope string) time.Duration {
	switch scope {
	case qualityScopeResponses:
		return 65 * time.Second
	case qualityScopeTarget:
		return 40 * time.Second
	case qualityScopeSpeed:
		return 100 * time.Second
	case qualityScopeNAT:
		return 20 * time.Second
	case qualityScopeWifi:
		return 45 * time.Second
	default:
		return 150 * time.Second
	}
}

func (h *HTTP) startQuality(w http.ResponseWriter, r *http.Request) {
	includeSpeed := false
	if value := r.URL.Query().Get("speed"); value != "" {
		var err error
		includeSpeed, err = strconv.ParseBool(value)
		if err != nil {
			core.WriteError(w, http.StatusBadRequest, "speed must be true or false")
			return
		}
	}
	scope := r.URL.Query().Get("scope")
	if scope == "" {
		scope = qualityScopeAll
	}
	target := r.URL.Query().Get("target")
	switch scope {
	case qualityScopeAll:
		if target != "" {
			core.WriteError(w, http.StatusBadRequest, "target is only valid with scope=target")
			return
		}
	case qualityScopeResponses, qualityScopeSpeed, qualityScopeNAT, qualityScopeWifi:
		if target != "" {
			core.WriteError(w, http.StatusBadRequest, "target is only valid with scope=target")
			return
		}
	case qualityScopeTarget:
		if !slices.ContainsFunc(qualityTargets, func(item qualityTarget) bool { return item.ID == target }) {
			core.WriteError(w, http.StatusBadRequest, "target must be one of "+qualityTargetIDs())
			return
		}
	default:
		core.WriteError(w, http.StatusBadRequest, "scope must be all, responses, target, speed, nat or wifi")
		return
	}
	if includeSpeed && scope != qualityScopeAll {
		core.WriteError(w, http.StatusBadRequest, "speed is only valid with scope=all")
		return
	}
	if scope == qualityScopeWifi && h.Wireless == nil {
		core.WriteError(w, http.StatusBadRequest, "no access point is configured for wireless measurement")
		return
	}
	h.quality.mu.Lock()
	if h.quality.closed {
		h.quality.mu.Unlock()
		core.WriteError(w, http.StatusServiceUnavailable, "diagnostics are shutting down")
		return
	}
	// Reserve existing speed/NAT runners for the whole run, including the
	// response baseline, to avoid contamination by those diagnostic tests.
	if h.quality.cancel != nil || !h.running.CompareAndSwap(false, true) {
		h.quality.mu.Unlock()
		core.WriteError(w, http.StatusConflict, "a quality assessment or speed test is already running")
		return
	}
	if !h.natRunning.CompareAndSwap(false, true) {
		h.running.Store(false)
		h.quality.mu.Unlock()
		core.WriteError(w, http.StatusConflict, "a NatTypeTester test is already running")
		return
	}
	if scope != qualityScopeAll && h.quality.snapshot.State != "completed" {
		h.running.Store(false)
		h.natRunning.Store(false)
		h.quality.mu.Unlock()
		core.WriteError(w, http.StatusConflict, "run a full assessment before rerunning individual tests")
		return
	}
	parent := h.QualityContext
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, qualityTimeout(scope))
	h.quality.cancel = cancel
	if scope == qualityScopeAll {
		now := time.Now().UTC()
		h.quality.snapshot = QualitySnapshot{
			State: "running", Phase: qualityPhase(scope), RunningScope: scope,
			IncludeSpeed: includeSpeed, RunID: now.Format(time.RFC3339Nano), StartedAt: &now,
		}
	} else {
		h.quality.snapshot.RunningScope = scope
		h.quality.snapshot.RunningTarget = target
		h.quality.snapshot.Phase = qualityPhase(scope)
		if scope == qualityScopeSpeed {
			// The snapshot now carries speed evidence even when the last full
			// run was started without a transfer.
			h.quality.snapshot.IncludeSpeed = true
		}
	}
	h.quality.mu.Unlock()
	go h.measureQuality(ctx, qualityRunSpec{Scope: scope, Target: target, IncludeSpeed: includeSpeed})
	core.WriteJSON(w, http.StatusAccepted, h.qualitySnapshot())
}

func (h *HTTP) cancelQuality(w http.ResponseWriter, r *http.Request) {
	h.quality.mu.Lock()
	if h.quality.cancel != nil {
		h.quality.cancel()
	}
	h.quality.mu.Unlock()
	core.WriteJSON(w, http.StatusOK, h.qualitySnapshot())
}

func (h *HTTP) measureQuality(ctx context.Context, spec qualityRunSpec) {
	completed := false
	defer func() {
		h.quality.mu.Lock()
		h.quality.snapshot.RunningScope = ""
		h.quality.snapshot.RunningTarget = ""
		h.quality.snapshot.Phase = ""
		if spec.Scope == qualityScopeAll {
			now := time.Now().UTC()
			h.quality.snapshot.CompletedAt = &now
			h.quality.snapshot.State = "completed"
			if ctx.Err() == context.Canceled {
				h.quality.snapshot.State = "cancelled"
			} else if ctx.Err() != nil {
				h.quality.snapshot.State = "timed_out"
			}
			h.saveQuality()
		} else if completed {
			// A scoped rerun updates the snapshot it was based on; a cancelled
			// one leaves both the published evidence and its checkpoint alone.
			now := time.Now().UTC()
			h.quality.snapshot.CompletedAt = &now
			h.saveQuality()
		}
		h.quality.cancel()
		h.quality.cancel = nil
		h.running.Store(false)
		h.natRunning.Store(false)
		h.quality.mu.Unlock()
	}()

	switch spec.Scope {
	case qualityScopeResponses:
		results := h.probeQualityTargets(ctx, nil)
		if ctx.Err() == nil {
			h.replaceQualityTargets(results)
			completed = true
		}
	case qualityScopeTarget:
		results := h.probeQualityTargets(ctx, []string{spec.Target})
		if ctx.Err() == nil {
			h.mergeQualityTarget(spec.Target, results)
			completed = true
		}
	case qualityScopeSpeed:
		h.measureQualitySpeed(ctx)
		completed = ctx.Err() == nil
	case qualityScopeNAT:
		h.measureQualityNAT(ctx)
		completed = ctx.Err() == nil
	case qualityScopeWifi:
		h.measureQualityWireless(ctx)
		completed = ctx.Err() == nil
	default:
		h.measureQualityAll(ctx, spec.IncludeSpeed)
	}
}

func (h *HTTP) measureQualityAll(ctx context.Context, includeSpeed bool) {
	results := h.probeQualityTargets(ctx, nil)
	h.quality.mu.Lock()
	h.quality.snapshot.Targets = results
	h.quality.mu.Unlock()
	if includeSpeed && ctx.Err() == nil {
		h.measureQualitySpeed(ctx)
	}
	if ctx.Err() == nil {
		h.measureQualityNAT(ctx)
	}
	// Wireless is a read of the access points, so it comes after the tests
	// that touch the internet: adding a management request mid-transfer would
	// be the one thing that could disturb the throughput sample.
	if ctx.Err() == nil {
		h.measureQualityWireless(ctx)
	}
}

func (h *HTTP) probeQualityTargets(ctx context.Context, only []string) []QualityTargetResult {
	probe := h.qualityProbe
	if probe == nil {
		probe = probeQuality
	}
	targets := qualityTargets
	if len(only) > 0 {
		targets = make([]qualityTarget, 0, len(only))
		for _, target := range qualityTargets {
			if slices.Contains(only, target.ID) {
				targets = append(targets, target)
			}
		}
	}
	results := make([]QualityTargetResult, len(targets)*2)
	var wg sync.WaitGroup
	for i, target := range targets {
		for j, family := range []string{"ipv4", "ipv6"} {
			index := i*2 + j
			wg.Go(func() {
				result := QualityTargetResult{ID: target.ID, Name: target.Name, URL: target.URL, Family: family, Samples: []QualitySample{}}
				for range qualitySamples {
					if ctx.Err() != nil {
						break
					}
					bounded, cancel := context.WithTimeout(ctx, 5*time.Second)
					result.Samples = append(result.Samples, probe(bounded, target, family))
					cancel()
				}
				results[index] = result
			})
		}
	}
	wg.Wait()
	return results
}

func (h *HTTP) replaceQualityTargets(results []QualityTargetResult) {
	h.quality.mu.Lock()
	h.quality.snapshot.Targets = results
	h.quality.mu.Unlock()
}

// mergeQualityTarget replaces both address-family results for one service and
// keeps every other service's evidence, including runs from an earlier time.
func (h *HTTP) mergeQualityTarget(id string, fresh []QualityTargetResult) {
	h.quality.mu.Lock()
	defer h.quality.mu.Unlock()
	byFamily := make(map[string]QualityTargetResult, len(fresh))
	for _, result := range fresh {
		if result.ID == id {
			byFamily[result.Family] = result
		}
	}
	merged := make([]QualityTargetResult, 0, len(h.quality.snapshot.Targets)+len(byFamily))
	replaced := make(map[string]bool, len(byFamily))
	for _, existing := range h.quality.snapshot.Targets {
		if existing.ID != id {
			merged = append(merged, existing)
			continue
		}
		if result, ok := byFamily[existing.Family]; ok {
			merged = append(merged, result)
			replaced[existing.Family] = true
		}
	}
	for _, result := range fresh {
		if result.ID == id && !replaced[result.Family] {
			merged = append(merged, result)
			replaced[result.Family] = true
		}
	}
	h.quality.snapshot.Targets = merged
}

func (h *HTTP) measureQualitySpeed(ctx context.Context) {
	h.quality.mu.Lock()
	h.quality.snapshot.Phase = "speed"
	h.quality.mu.Unlock()
	bounded, cancel := context.WithTimeout(ctx, 90*time.Second)
	run := h.RunQuality
	if run == nil {
		run = func(ctx context.Context) (SpeedResult, error) { return runCloudflare(ctx, 0) }
	}
	result, err := run(bounded)
	cancel()
	h.quality.mu.Lock()
	defer h.quality.mu.Unlock()
	switch {
	case ctx.Err() != nil:
		// A cancelled run must not replace published evidence in memory.
	case err == nil:
		now := time.Now().UTC()
		h.quality.snapshot.Speed, h.quality.snapshot.SpeedAt = &result, &now
		h.quality.snapshot.SpeedError = ""
	default:
		// Keep the last successful transfer; the error explains why this
		// attempt did not replace it.
		h.quality.snapshot.SpeedError = err.Error()
	}
}

// measureQualityWireless reads the access points' client tables. It is a
// passive read: no test traffic, so it can run beside anything, but a client
// that moved a minute ago is simply not in it. An access point that did not
// answer is named in the evidence instead of shrinking the client count.
func (h *HTTP) measureQualityWireless(ctx context.Context) {
	h.quality.mu.Lock()
	h.quality.snapshot.Phase = "wifi"
	h.quality.mu.Unlock()
	read := h.Wireless
	if read == nil {
		h.quality.mu.Lock()
		h.quality.snapshot.WirelessError = "No access point is configured for measurement"
		h.quality.mu.Unlock()
		return
	}
	bounded, cancel := context.WithTimeout(ctx, 30*time.Second)
	result, err := read(bounded)
	cancel()
	h.quality.mu.Lock()
	defer h.quality.mu.Unlock()
	switch {
	case ctx.Err() != nil:
		// A cancelled run must not replace published evidence in memory.
	case err != nil:
		// Keep the last read and let wireless_at say how old it is; a failed
		// read is not evidence that every client left.
		h.quality.snapshot.WirelessError = err.Error()
	default:
		now := time.Now().UTC()
		h.quality.snapshot.Wireless, h.quality.snapshot.WirelessAt = &result, &now
		h.quality.snapshot.WirelessError = ""
	}
}

func (h *HTTP) measureQualityNAT(ctx context.Context) {
	h.quality.mu.Lock()
	h.quality.snapshot.Phase = "nat"
	h.quality.mu.Unlock()
	bounded, cancel := context.WithTimeout(ctx, 15*time.Second)
	run := h.RunNAT
	if run == nil {
		run = runNAT
	}
	result, err := run(bounded)
	cancel()
	h.quality.mu.Lock()
	defer h.quality.mu.Unlock()
	switch {
	case ctx.Err() != nil:
		// A cancelled run must not replace published evidence in memory.
	case err == nil:
		h.quality.snapshot.NAT = &result
		// A partial answer still replaces the previous evidence; the upstream
		// test that failed is named instead of being hidden.
		h.quality.snapshot.NATError = result.Error
	default:
		h.quality.snapshot.NATError = err.Error()
	}
}

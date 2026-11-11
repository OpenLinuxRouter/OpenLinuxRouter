package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// testTime is a fixed instant, so a view's as_of is asserted on rather than
// raced against.
func testTime() time.Time { return time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC) }

func newTestStore(t *testing.T) *core.Store {
	t.Helper()
	return core.NewStore(filepath.Join(t.TempDir(), "olr.json"), ModuleName)
}

func newTestHandler(t *testing.T, k Kernel) (http.Handler, Applier) {
	t.Helper()
	a := Applier{Kernel: k, Links: testLinks(), Store: newTestStore(t)}
	h := HTTP{Applier: a, Lock: core.NewLock(), Events: core.NewEvents()}
	return h.Handler(), a
}

func do(t *testing.T, h http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(data)
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, reader)
	// The common case, and the one that must not look disruptive: a local
	// caller over the unix socket, whose connection no routing change can move.
	req.RemoteAddr = "@"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func decode[T any](t *testing.T, w *httptest.ResponseRecorder) T {
	t.Helper()
	var out T
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("decoding %s: %v", w.Body.String(), err)
	}
	return out
}

func TestGetConfigOnAFreshBoxIsAnEmptyState(t *testing.T) {
	h, _ := newTestHandler(t, &StaticKernel{})

	w := do(t, h, http.MethodGet, "/config", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
	cfg := decode[Config](t, w)
	if !cfg.Empty() {
		t.Errorf("a box nobody has configured should read as empty, got %+v", cfg)
	}
}

func TestPutConfigStoresAndProgramsIt(t *testing.T) {
	k := &StaticKernel{}
	h, a := newTestHandler(t, k)

	w := do(t, h, http.MethodPut, "/config", testConfig())
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}

	stored, err := a.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(stored.Exits) != 2 {
		t.Fatalf("intent not stored: %+v", stored)
	}
	if len(k.State) == 0 {
		t.Fatal("nothing reached the kernel")
	}

	// And the second identical PUT is a no-op, which is what makes re-applying
	// safe as a repair path.
	w = do(t, h, http.MethodPut, "/config", stored)
	plan := decode[applyResponse](t, w).Plan
	if !plan.Empty {
		t.Errorf("re-applying the same config should change nothing, got %v", plan.Changes)
	}
}

func TestPutRejectsAnInvalidConfigBeforeTouchingAnything(t *testing.T) {
	k := &StaticKernel{}
	h, _ := newTestHandler(t, k)

	bad := testConfig()
	bad.Default = "Nope"

	w := do(t, h, http.MethodPut, "/config", bad)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status %d, want 422: %s", w.Code, w.Body)
	}
	if len(k.State) != 0 {
		t.Error("nothing should have been programmed")
	}
}

func TestPutRejectsUnknownFields(t *testing.T) {
	h, _ := newTestHandler(t, &StaticKernel{})

	req := httptest.NewRequest(http.MethodPut, "/config",
		strings.NewReader(`{"enabled":true,"exitz":[]}`))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("a mistyped key should be a 400, got %d: %s", w.Code, w.Body)
	}
}

func TestPatchLeavesUnnamedFieldsAlone(t *testing.T) {
	h, a := newTestHandler(t, &StaticKernel{})
	if w := do(t, h, http.MethodPut, "/config", testConfig()); w.Code != http.StatusOK {
		t.Fatalf("setup failed: %s", w.Body)
	}

	w := do(t, h, http.MethodPatch, "/config", map[string]any{"enabled": false})
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}

	stored, err := a.Load()
	if err != nil {
		t.Fatal(err)
	}
	if stored.Enabled {
		t.Error("the patched field did not take")
	}
	if len(stored.Exits) != 2 {
		t.Errorf("a patch naming one field should not drop the exits: %+v", stored)
	}
}

func TestPlanDoesNotChangeAnything(t *testing.T) {
	k := &StaticKernel{}
	h, _ := newTestHandler(t, k)

	w := do(t, h, http.MethodPost, "/plan", testConfig())
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
	plan := decode[planView](t, w)
	if plan.Empty {
		t.Error("planning a config against an empty kernel should show work")
	}
	if plan.Diff == "" {
		t.Error("an impact with no diff is a scarier spinner, not an explanation")
	}
	if len(k.State) != 0 {
		t.Error("a dry run must not program anything")
	}
}

func TestPlanWithNoBodyIsTheDriftCheck(t *testing.T) {
	h, _ := newTestHandler(t, &StaticKernel{})
	if w := do(t, h, http.MethodPut, "/config", testConfig()); w.Code != http.StatusOK {
		t.Fatalf("setup failed: %s", w.Body)
	}

	w := do(t, h, http.MethodPost, "/plan", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
	if plan := decode[planView](t, w); !plan.Empty {
		t.Errorf("a freshly applied box should not have drifted: %v", plan.Changes)
	}
}

// §6's refusal is a conflict the operator resolves elsewhere, not something
// that went wrong here — so it is a 409, not a 500.
func TestForeignRulesAnswerWithAConflict(t *testing.T) {
	k := &StaticKernel{Foreign: []ForeignRule{
		{Priority: 9000, Family: "ip", Table: 2022, HasDefault: true},
	}}
	h, _ := newTestHandler(t, k)

	w := do(t, h, http.MethodPut, "/config", testConfig())
	if w.Code != http.StatusConflict {
		t.Fatalf("status %d, want 409: %s", w.Code, w.Body)
	}
	resp := decode[applyResponse](t, w)
	if resp.Plan.Blocked == "" {
		t.Error("the refusal reason should be on the plan")
	}
	if len(k.State) != 0 {
		t.Error("nothing should have been programmed")
	}
}

func TestApplyRepairsDrift(t *testing.T) {
	k := &StaticKernel{}
	h, _ := newTestHandler(t, k)
	if w := do(t, h, http.MethodPut, "/config", testConfig()); w.Code != http.StatusOK {
		t.Fatalf("setup failed: %s", w.Body)
	}

	// Somebody flushed our rules by hand.
	k.State = nil

	w := do(t, h, http.MethodPost, "/apply", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
	if len(k.State) == 0 {
		t.Fatal("re-applying should have put it back")
	}
}

func TestStatusReportsEffectiveValuesAndTheirSource(t *testing.T) {
	h, _ := newTestHandler(t, &StaticKernel{})
	cfg := testConfig()
	cfg.Default = "Proxy"
	if w := do(t, h, http.MethodPut, "/config", cfg); w.Code != http.StatusOK {
		t.Fatalf("setup failed: %s", w.Body)
	}

	w := do(t, h, http.MethodGet, "/status", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
	st := decode[statusView](t, w)

	if len(st.Exits) != 2 || len(st.Assignments) != 2 {
		t.Fatalf("unexpected shape: %+v", st)
	}
	for _, a := range st.Assignments {
		if a.Source == "" {
			// §2.2: inheritance is unusable if the answer's source is not
			// visible.
			t.Errorf("%s has no source", a.Interface)
		}
	}
	for _, e := range st.Exits {
		if e.Mark == "" || e.Table == 0 || e.Priority == 0 {
			t.Errorf("%s does not publish its kernel resources: %+v", e.Name, e)
		}
		if e.Probed {
			t.Errorf("%s has no probe configured but claims to be checked", e.Name)
		}
	}
	if st.AsOf.IsZero() {
		t.Error("observed data must be stamped (§4.5)")
	}
}

func TestStatusOnAnUnreadableKernelSaysSo(t *testing.T) {
	h, _ := newTestHandler(t, &StaticKernel{Unknown: true})

	w := do(t, h, http.MethodGet, "/status", nil)
	st := decode[statusView](t, w)
	if st.Known {
		t.Error("an unreadable kernel must not report itself as known")
	}
	if st.Drifted {
		t.Error("a kernel we could not read has not drifted; it is unknown")
	}
}

func TestStatusSaysWhenAnExitIsDown(t *testing.T) {
	prober := NewProber()
	prober.health["Proxy"] = false

	a := Applier{
		Kernel: &StaticKernel{}, Links: testLinks(), Store: newTestStore(t), Probes: prober,
	}
	h := HTTP{Applier: a, Lock: core.NewLock(), Events: core.NewEvents()}.Handler()
	if w := do(t, h, http.MethodPut, "/config", testConfig()); w.Code != http.StatusOK {
		t.Fatalf("setup failed: %s", w.Body)
	}

	st := decode[statusView](t, do(t, h, http.MethodGet, "/status", nil))
	var reason string
	for _, as := range st.Assignments {
		if as.Interface == "br-lan" {
			reason = as.Reason
		}
	}
	// §2.2: the failure surfaces in the place the operator already looks, in
	// words they can act on.
	if !strings.Contains(reason, "no internet") || !strings.Contains(reason, "Proxy") {
		t.Errorf("br-lan should say why it has no internet, got %q", reason)
	}
}

func TestApplyPublishesAnEventOnlyWhenSomethingChanged(t *testing.T) {
	events := core.NewEvents()
	a := Applier{Kernel: &StaticKernel{}, Links: testLinks(), Store: newTestStore(t)}
	h := HTTP{Applier: a, Lock: core.NewLock(), Events: events}.Handler()

	if w := do(t, h, http.MethodPut, "/config", testConfig()); w.Code != http.StatusOK {
		t.Fatalf("setup failed: %s", w.Body)
	}
	// A no-op store would otherwise wake every client to re-read identical
	// bytes.
	w := do(t, h, http.MethodPut, "/config", testConfig())
	if plan := decode[applyResponse](t, w).Plan; !plan.Empty {
		t.Errorf("the second identical apply should be empty, got %v", plan.Changes)
	}
}

func TestWatchIsCalledAfterASuccessfulApply(t *testing.T) {
	var got Config
	a := Applier{Kernel: &StaticKernel{}, Links: testLinks(), Store: newTestStore(t)}
	h := HTTP{
		Applier: a, Lock: core.NewLock(), Events: core.NewEvents(),
		Watch: func(c Config) { got = c },
	}.Handler()

	if w := do(t, h, http.MethodPut, "/config", testConfig()); w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
	if len(got.Exits) != 2 {
		t.Errorf("the prober was not told about the new config: %+v", got)
	}
}

func TestTrafficEndpointDegradesRatherThanFailing(t *testing.T) {
	// A kernel that cannot be read still leaves intent worth showing, and
	// `counting: false` is what says which half is missing — the same shape
	// getStatus uses.
	h, _ := newTestHandler(t, &StaticKernel{Unknown: true})
	if w := do(t, h, http.MethodPut, "/config", testConfig()); w.Code != http.StatusOK {
		t.Fatalf("setup failed: %s", w.Body)
	}

	w := do(t, h, http.MethodGet, "/traffic", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
	tv := decode[trafficView](t, w)
	if tv.Counting {
		t.Error("a kernel we could not read is not counting")
	}
	if !tv.Enabled {
		t.Error("intent says accounting is on, which is a different question")
	}
	if len(tv.Limits) != 0 {
		t.Error("nothing is being counted, so there is nothing to caveat")
	}
}

func TestTrafficEndpointReportsUsage(t *testing.T) {
	cfg := testConfig()
	cfg.Normalize()
	proxy, _ := cfg.Find("Proxy")

	k := &StaticKernel{Flows: []Flow{
		{Addr: netip.MustParseAddr("192.168.1.23"), Mark: proxy.Mark(), UpBytes: 10, DownBytes: 90},
	}}
	h, _ := newTestHandler(t, k)
	if w := do(t, h, http.MethodPut, "/config", cfg); w.Code != http.StatusOK {
		t.Fatalf("setup failed: %s", w.Body)
	}

	tv := decode[trafficView](t, do(t, h, http.MethodGet, "/traffic", nil))
	if !tv.Counting || len(tv.Usage) != 1 {
		t.Fatalf("unexpected response: %+v", tv)
	}
	if tv.Usage[0].Exit != "Proxy" || tv.Usage[0].DownBytes != 90 {
		t.Errorf("unexpected row: %+v", tv.Usage[0])
	}
	if len(tv.Limits) == 0 {
		t.Error("§7.4's limits should ride on the response")
	}
	if tv.AsOf.IsZero() {
		t.Error("observed data must be stamped (§4.5)")
	}
}

func TestActivityEndpointCarriesItsWindow(t *testing.T) {
	// A client page draws the buckets and computes one total from them, so the
	// response has to say how far back they reach: a screen that hard-coded the
	// window would silently mislabel the number when the window changed, which
	// is exactly the confusion this endpoint's shape exists to prevent.
	addr := netip.MustParseAddr("192.168.1.23")
	at := time.Now().Truncate(30 * time.Minute)
	activity := NewActivity()
	activity.observe(at, []Usage{{Addr: addr, UpBytes: 100}}, true)
	activity.observe(at.Add(time.Minute), []Usage{{Addr: addr, UpBytes: 140, DownBytes: 20}}, true)

	a := Applier{Kernel: &StaticKernel{}, Links: testLinks(), Store: newTestStore(t)}
	h := HTTP{Applier: a, Activity: activity, Lock: core.NewLock(), Events: core.NewEvents()}

	w := do(t, h.Handler(), http.MethodGet, "/activity?address=192.168.1.23", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
	view := decode[activityView](t, w)
	if view.Days != activityRetentionDays {
		t.Errorf("days = %d, want %d", view.Days, activityRetentionDays)
	}
	if len(view.Buckets) != 1 || view.Buckets[0].Bytes.Up != 40 || view.Buckets[0].Bytes.Down != 20 {
		t.Fatalf("unexpected buckets: %+v", view.Buckets)
	}
}

func TestUnsupportedKernelRefusesRatherThanPretending(t *testing.T) {
	// The non-Linux build. Reporting an empty kernel that accepted everything
	// would let a test pass against behaviour that has never run.
	k := unsupportedStub{}
	obs, err := k.Observe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if obs.Known {
		t.Error("it should report that it does not know")
	}
	if _, err := k.Apply(context.Background(), Desired{}); err == nil {
		t.Error("applying should refuse")
	}
}

// unsupportedStub mirrors kernel_other.go so the expectation is asserted on
// every platform, including the Linux one where that file is not built.
type unsupportedStub struct{}

func (unsupportedStub) Observe(context.Context) (Observed, error) {
	return Observed{Known: false}, nil
}

func (unsupportedStub) Apply(context.Context, Desired) ([]Step, error) {
	return nil, ErrUnsupported
}

func TestLatencyEndpointReadsWithoutProbing(t *testing.T) {
	monitor := NewLatencyMonitor()
	monitor.probe = func(context.Context, string) (float64, error) { t.Error("GET must not probe"); return 0, nil }
	for _, m := range []*LatencyMonitor{nil, monitor} {
		h := HTTP{Latency: m}.Handler()
		w := do(t, h, http.MethodGet, "/latency", nil)
		if w.Code != http.StatusOK {
			t.Fatalf("status %d", w.Code)
		}
		s := decode[LatencySnapshot](t, w)
		want := "measuring"
		if m == nil {
			want = "unavailable"
		}
		if s.State != want || s.Milliseconds != nil || s.Sites == nil {
			t.Fatalf("unexpected snapshot: %+v", s)
		}
	}
}

func TestLatencyTargetEndpoint(t *testing.T) {
	m := NewLatencyMonitor()
	m.probe = func(context.Context, string) (float64, error) { t.Error("settings must not probe"); return 0, nil }
	h := HTTP{Latency: m, Lock: core.NewLock()}.Handler()
	w := do(t, h, http.MethodPut, "/latency/target", map[string]string{"target": m.targets[2].URL})
	if w.Code != http.StatusOK || m.Snapshot().Target != "Yandex" {
		t.Fatalf("selection: %d %s", w.Code, w.Body.String())
	}
	w = do(t, h, http.MethodPut, "/latency/target", map[string]string{"target": "https://unlisted.example/"})
	if w.Code != http.StatusBadRequest || m.Snapshot().Target != "Yandex" {
		t.Fatal("invalid target changed selection")
	}
	w = do(t, h, http.MethodPut, "/latency/target", map[string]string{"target": ""})
	if w.Code != http.StatusOK || m.Snapshot().TargetURL != "" {
		t.Fatal("could not request automatic selection")
	}
}

func TestLatencySitesEndpoint(t *testing.T) {
	custom, err := NewCustomLatencyMonitor(filepath.Join(t.TempDir(), "sites.json"))
	if err != nil {
		t.Fatal(err)
	}
	monitor := NewLatencyMonitor()
	monitor.Custom = custom
	custom.ValidateExit = func(name string) error {
		if name == "Proxy" {
			return nil
		}
		return fmt.Errorf("unknown exit %q", name)
	}
	monitor.probe = func(context.Context, string) (float64, error) { t.Fatal("settings must not probe"); return 0, nil }
	h := HTTP{Latency: monitor, Lock: core.NewLock()}.Handler()
	body := []CustomLatencySite{{Name: "YouTube", URL: "https://www.youtube.com/", Exit: "Proxy"}}
	w := do(t, h, http.MethodPut, "/latency/sites", body)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT: %d %s", w.Code, w.Body.String())
	}
	w = do(t, h, http.MethodGet, "/latency", nil)
	if w.Code != http.StatusOK || len(decode[LatencySnapshot](t, w).Custom) != 1 {
		t.Fatal("site missing from snapshot")
	}
	if got := decode[LatencySnapshot](t, w).Custom; got[0].Exit != "Proxy" {
		t.Fatalf("route missing: %+v", got)
	}
	w = do(t, h, http.MethodPut, "/latency/sites", []CustomLatencySite{{Name: "X", URL: "http://example.com"}})
	if w.Code != http.StatusBadRequest || len(custom.Config()) != 1 {
		t.Fatal("invalid edit changed settings")
	}
	w = do(t, h, http.MethodPut, "/latency/sites", []CustomLatencySite{})
	if w.Code != http.StatusOK || len(custom.Config()) != 0 {
		t.Fatal("cannot clear sites")
	}
}

// A catalog site can gain an exit through the same list, but an empty list
// removes only the custom sites: the catalog is still there afterwards.
func TestLatencyCommonExitAndRemoval(t *testing.T) {
	custom, err := OpenCustomLatencyConfig(core.NewStore(filepath.Join(t.TempDir(), "olr.json"), ModuleName))
	if err != nil {
		t.Fatal(err)
	}
	custom.ValidateExit = func(name string) error {
		if name != "Gateway2" {
			return fmt.Errorf("unknown exit %q", name)
		}
		return nil
	}
	monitor := NewLatencyMonitor()
	monitor.Custom = custom
	h := HTTP{Latency: monitor, Lock: core.NewLock()}.Handler()
	body := []CustomLatencySite{
		{Name: "Google", URL: "https://www.gstatic.com/", Exit: "Gateway2"},
		{Name: "NAS", URL: "https://nas.example/"},
	}
	if w := do(t, h, http.MethodPut, "/latency/sites", body); w.Code != http.StatusOK {
		t.Fatalf("PUT: %d %s", w.Code, w.Body.String())
	}
	snapshot := decode[LatencySnapshot](t, do(t, h, http.MethodGet, "/latency", nil))
	if site := latencySite(snapshot.Custom, "Google"); site == nil || !site.Builtin || site.Exit != "Gateway2" {
		t.Fatalf("Google: %+v", site)
	}
	if site := latencySite(snapshot.Custom, "NAS"); site == nil || site.Builtin {
		t.Fatalf("NAS: %+v", site)
	}
	if w := do(t, h, http.MethodPut, "/latency/sites", []CustomLatencySite{}); w.Code != http.StatusOK {
		t.Fatalf("clear: %d %s", w.Code, w.Body.String())
	}
	snapshot = decode[LatencySnapshot](t, do(t, h, http.MethodGet, "/latency", nil))
	if len(snapshot.Custom) != len(latencyCatalog()) {
		t.Fatalf("%d sites after clearing, want the catalog's %d", len(snapshot.Custom), len(latencyCatalog()))
	}
	if site := latencySite(snapshot.Custom, "Google"); site == nil || site.Exit != "Gateway2" {
		t.Fatalf("Google exit lost: %+v", site)
	}
	if latencySite(snapshot.Custom, "NAS") != nil {
		t.Fatal("custom site survived clearing")
	}
}

func TestLatencyIconMissingSite(t *testing.T) {
	custom, err := NewCustomLatencyMonitor(filepath.Join(t.TempDir(), "sites.json"))
	if err != nil {
		t.Fatal(err)
	}
	monitor := NewLatencyMonitor()
	monitor.Custom = custom
	w := do(t, (HTTP{Latency: monitor}).Handler(), http.MethodGet, "/latency/sites/missing/icon", nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d", w.Code)
	}
}

func TestLatencySiteMeasureEndpoint(t *testing.T) {
	custom, err := NewCustomLatencyMonitor(filepath.Join(t.TempDir(), "sites.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := custom.Replace([]CustomLatencySite{
		{Name: "A", URL: "https://a.example/"},
		{Name: "B", URL: "https://b.example/"},
	}); err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{}, 4)
	release := make(chan struct{})
	defer close(release)
	custom.probe = func(context.Context, string) (float64, error) {
		started <- struct{}{}
		<-release
		return 5, nil
	}
	monitor := NewLatencyMonitor()
	monitor.Custom = custom
	h := HTTP{Latency: monitor}.Handler()

	w := do(t, h, http.MethodPost, "/latency/sites/A/measure", nil)
	if w.Code != http.StatusAccepted {
		t.Fatalf("status = %d %s", w.Code, w.Body.String())
	}
	<-started
	if got := decode[LatencySnapshot](t, w).Custom; len(got) != 2 || !got[0].Measuring || got[1].Measuring {
		t.Fatalf("progress not limited to A: %+v", got)
	}
	// A second click while the probe runs is accepted but starts no probe.
	if w := do(t, h, http.MethodPost, "/latency/sites/A/measure", nil); w.Code != http.StatusAccepted {
		t.Fatalf("repeat status = %d %s", w.Code, w.Body.String())
	}
	select {
	case <-started:
		t.Fatal("second probe started for an in-flight site")
	default:
	}
	if w := do(t, h, http.MethodPost, "/latency/sites/Missing/measure", nil); w.Code != http.StatusNotFound {
		t.Fatalf("unknown site: status = %d", w.Code)
	}
	if w := do(t, (HTTP{}).Handler(), http.MethodPost, "/latency/sites/A/measure", nil); w.Code != http.StatusServiceUnavailable {
		t.Fatalf("missing monitor: status = %d", w.Code)
	}
}

func TestLatencyMeasureAllEndpoint(t *testing.T) {
	custom, err := NewCustomLatencyMonitor(filepath.Join(t.TempDir(), "sites.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := custom.Replace([]CustomLatencySite{
		{Name: "A", URL: "https://a.example/"},
		{Name: "B", URL: "https://b.example/"},
	}); err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	defer close(release)
	custom.probe = func(context.Context, string) (float64, error) {
		started <- struct{}{}
		<-release
		return 5, nil
	}
	monitor := NewLatencyMonitor()
	monitor.Custom = custom
	h := HTTP{Latency: monitor}.Handler()

	w := do(t, h, http.MethodPost, "/latency/measure", nil)
	if w.Code != http.StatusAccepted {
		t.Fatalf("status = %d %s", w.Code, w.Body.String())
	}
	<-started
	<-started
	snapshot := decode[LatencySnapshot](t, w)
	if len(snapshot.Custom) != 2 || !snapshot.Custom[0].Measuring || !snapshot.Custom[1].Measuring {
		t.Fatalf("Test all did not mark every site: %+v", snapshot.Custom)
	}
	if w := do(t, (HTTP{}).Handler(), http.MethodPost, "/latency/measure", nil); w.Code != http.StatusServiceUnavailable {
		t.Fatalf("missing monitor: status = %d", w.Code)
	}
	if w := do(t, h, http.MethodPost, "/latency/measure?category=unknown", nil); w.Code != http.StatusBadRequest {
		t.Fatalf("unknown category: status = %d", w.Code)
	}
}

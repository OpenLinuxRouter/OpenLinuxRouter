package ingress

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

func TestRepairMultipleInvalidServicesOneAtATime(t *testing.T) {
	h, a := testHTTP(t)
	publish(t, h)
	files, err := os.ReadFile(a.Paths.Conf)
	if err != nil {
		t.Fatal(err)
	}
	cfg := good()
	cfg.Services[0].Upstream.Client = "old-nuc"
	cfg.Services = append(cfg.Services, Service{Name: "storage", Upstream: Upstream{Client: "old-storage", Port: 80}})
	doc, _ := a.Store.Load()
	data, _ := MarshalConfig(cfg)
	doc.Set(ModuleName, data)
	if err := a.Store.Save(doc); err != nil {
		t.Fatal(err)
	}
	// An invalid replacement must not be accepted just because another error exists.
	w := do(t, h, "PUT", "/services/grafana", `{"upstream":{"client":"nuc","port":0}}`)
	if w.Code != 422 {
		t.Fatalf("invalid repair = %d: %s", w.Code, w.Body)
	}
	w = do(t, h, "PUT", "/services/grafana?dry_run=true", `{"upstream":{"client":"nuc","port":3000}}`)
	if w.Code != 200 {
		t.Fatalf("dry run = %d: %s", w.Code, w.Body)
	}
	unchanged, _ := a.Load()
	if unchanged.Services[0].Upstream.Client != "old-nuc" {
		t.Fatal("dry run saved changes")
	}
	w = do(t, h, "PUT", "/services/grafana", `{"upstream":{"client":"nuc","port":3000}}`)
	if w.Code != 200 {
		t.Fatalf("first repair = %d: %s", w.Code, w.Body)
	}
	resp := decode[applyResponse](t, w)
	if !resp.Pending || len(resp.Problems) != 1 || len(resp.Steps) != 0 {
		t.Fatalf("repair response = %+v", resp)
	}
	stored, _ := a.Load()
	if stored.Services[0].Upstream.Client != "nuc" {
		t.Fatal("repair not saved")
	}
	after, _ := os.ReadFile(a.Paths.Conf)
	if !bytes.Equal(after, files) {
		t.Fatal("pending repair rewrote the running configuration")
	}
	status := decode[statusResponse](t, do(t, h, "GET", "/status", ""))
	if len(status.Problems) != 1 {
		t.Fatalf("remaining errors lost on refresh: %+v", status.Problems)
	}
	w = do(t, h, "PUT", "/services/storage", `{"upstream":{"client":"nuc","port":8080}}`)
	if w.Code != 200 {
		t.Fatalf("final repair = %d: %s", w.Code, w.Body)
	}
	resp = decode[applyResponse](t, w)
	if resp.Pending || len(resp.Steps) == 0 {
		t.Fatalf("final repair did not apply: %+v", resp)
	}
	after, _ = os.ReadFile(a.Paths.Conf)
	if !bytes.Contains(after, []byte("8080")) {
		t.Fatal("final repair missing from Caddy configuration")
	}
}

// The two things on this surface that break silently, and so are the two worth
// testing here rather than trusting:
//
//   - the disruptive gate. A UI with a delete button relies on the daemon
//     refusing, not on the button having asked. If this stops returning 409,
//     every client silently starts unpublishing addresses without a prompt.
//   - the credential round trip. GET redacts, and a client that saves what it
//     read must not store the mask. Both halves are invisible when wrong: the
//     proxy keeps running and the next renewal, weeks later, fails.
//
// Everything else here is covered where the decision lives — validate_test.go,
// plan_test.go, apply_test.go.

func testHTTP(t *testing.T) (http.Handler, Applier) {
	t.Helper()
	applier, proxy := testApplier(t)
	proxy.active = true

	h := HTTP{Applier: applier, Lock: core.NewLock(), Events: core.NewEvents()}
	return h.Handler(), applier
}

func do(t *testing.T, h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, path, nil)
	} else {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func decode[T any](t *testing.T, w *httptest.ResponseRecorder) T {
	t.Helper()
	var out T
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("decoding %s: %v", w.Body, err)
	}
	return out
}

// publish gets a service onto disk and serving, which is what makes a later
// delete disruptive.
func publish(t *testing.T, h http.Handler) {
	t.Helper()
	body := `{"enabled":true,"domain":"svc.example.com",` +
		`"certificate":{"provider":"cloudflare","provider_token":"tok"},` +
		`"services":[{"name":"grafana","upstream":{"client":"nuc","port":3000}}]}`
	if w := do(t, h, http.MethodPut, "/config?confirm=true", body); w.Code != http.StatusOK {
		t.Fatalf("setup PUT = %d: %s", w.Code, w.Body)
	}
}

func TestDeletingAPublishedServiceIsRefusedWithoutConfirm(t *testing.T) {
	h, applier := testHTTP(t)
	publish(t, h)

	w := do(t, h, http.MethodDelete, "/services/grafana", "")
	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", w.Code, w.Body)
	}

	resp := decode[applyResponse](t, w)
	if resp.Plan.Impact != ImpactDisruptive {
		t.Errorf("impact = %s, want disruptive", resp.Plan.Impact)
	}
	// The refusal has to name what would be lost, or the dialog built on it
	// cannot say anything useful.
	if resp.Error == nil || !strings.Contains(resp.Error.Message, "grafana.svc.example.com") {
		t.Errorf("the refusal must name the address, got %+v", resp.Error)
	}
	if !strings.Contains(resp.Error.Message, "confirm=true") {
		t.Errorf("the refusal must say how to go ahead, got %q", resp.Error.Message)
	}

	// Nothing was written. This is the half that matters: a 409 that had already
	// applied would be worse than no gate at all.
	stored, err := applier.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(stored.Services) != 1 {
		t.Fatalf("stored services = %v, want the service untouched", stored.Services)
	}
	if resp.Config == nil || len(resp.Config.Services) != 1 {
		t.Error("the refusal should carry the config that is still stored")
	}
}

func TestDeletingWithConfirmGoesAhead(t *testing.T) {
	h, applier := testHTTP(t)
	publish(t, h)

	if w := do(t, h, http.MethodDelete, "/services/grafana?confirm=true", ""); w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body)
	}
	stored, err := applier.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(stored.Services) != 0 {
		t.Errorf("stored services = %v, want none", stored.Services)
	}
}

// Bare `?confirm` is true, because that is how a flag reads in a URL.
func TestBareConfirmCounts(t *testing.T) {
	h, _ := testHTTP(t)
	publish(t, h)
	if w := do(t, h, http.MethodDelete, "/services/grafana?confirm", ""); w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body)
	}
}

func TestMalformedConfirmIsRefused(t *testing.T) {
	h, _ := testHTTP(t)
	publish(t, h)
	// Refused rather than ignored: a client that meant to confirm and mistyped is
	// much better off hearing about it than having the change quietly made.
	if w := do(t, h, http.MethodDelete, "/services/grafana?confirm=yes-please", ""); w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", w.Code, w.Body)
	}
}

func TestDryRunWritesNothing(t *testing.T) {
	h, applier := testHTTP(t)
	publish(t, h)

	w := do(t, h, http.MethodDelete, "/services/grafana?dry_run=true", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body)
	}
	// A dry run answers with the plan alone, the same shape POST /plan gives.
	plan := decode[planView](t, w)
	if plan.Impact != ImpactDisruptive {
		t.Errorf("impact = %s, want disruptive", plan.Impact)
	}
	stored, _ := applier.Load()
	if len(stored.Services) != 1 {
		t.Errorf("a dry run must change nothing, got %v", stored.Services)
	}
}

func TestDeletingSomethingThatIsNotThereIs404(t *testing.T) {
	h, _ := testHTTP(t)
	publish(t, h)
	if w := do(t, h, http.MethodDelete, "/services/ghost?confirm=true", ""); w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: %s", w.Code, w.Body)
	}
}

func TestGetConfigRedactsTheCredential(t *testing.T) {
	h, _ := testHTTP(t)
	publish(t, h)

	w := do(t, h, http.MethodGet, "/config", "")
	if strings.Contains(w.Body.String(), `"tok"`) {
		t.Fatalf("the credential reached the response:\n%s", w.Body)
	}
	cfg := decode[Config](t, w)
	if cfg.Certificate.Token != RedactedToken {
		t.Errorf("token = %q, want the mask", cfg.Certificate.Token)
	}
}

// The suffix is served from this module's own config now, and both the observed
// surfaces read it there — a status that still asked the resolver would report
// one domain while the Caddyfile served another.
func TestStatusAndServicesReportTheConfiguredDomain(t *testing.T) {
	h, _ := testHTTP(t)
	publish(t, h)

	status := decode[statusResponse](t, do(t, h, http.MethodGet, "/status", ""))
	if status.Domain != "svc.example.com" {
		t.Errorf("status domain = %q, want the configured suffix", status.Domain)
	}

	services := decode[servicesResponse](t, do(t, h, http.MethodGet, "/services", ""))
	if services.Domain != "svc.example.com" {
		t.Errorf("services domain = %q, want the configured suffix", services.Domain)
	}
	if len(services.Services) != 1 || services.Services[0].URL != "https://grafana.svc.example.com" {
		t.Errorf("service url = %+v, want the configured suffix assembled into it", services.Services)
	}
}

// The failure this guards against: a UI renders the mask into a form, saves the
// form, and stores eight asterisks as the credential. Nothing breaks until a
// renewal fails weeks later.
func TestSavingTheMaskKeepsTheStoredCredential(t *testing.T) {
	h, applier := testHTTP(t)
	publish(t, h)

	read := decode[Config](t, do(t, h, http.MethodGet, "/config", ""))
	back, err := json.Marshal(read)
	if err != nil {
		t.Fatal(err)
	}
	if w := do(t, h, http.MethodPut, "/config?confirm=true", string(back)); w.Code != http.StatusOK {
		t.Fatalf("PUT = %d: %s", w.Code, w.Body)
	}

	stored, err := applier.Load()
	if err != nil {
		t.Fatal(err)
	}
	if stored.Certificate.Token != "tok" {
		t.Fatalf("token = %q, want the original to have survived the round trip", stored.Certificate.Token)
	}
}

func TestANewCredentialStillReplacesTheOldOne(t *testing.T) {
	h, applier := testHTTP(t)
	publish(t, h)

	body := `{"certificate":{"provider_token":"rotated"}}`
	if w := do(t, h, http.MethodPatch, "/config?confirm=true", body); w.Code != http.StatusOK {
		t.Fatalf("PATCH = %d: %s", w.Code, w.Body)
	}
	stored, _ := applier.Load()
	if stored.Certificate.Token != "rotated" {
		t.Errorf("token = %q, want rotated", stored.Certificate.Token)
	}
	// And the patch must not have taken the services with it — the whole reason
	// services get item routes (RFC 7386 replaces an array wholesale).
	if len(stored.Services) != 1 {
		t.Errorf("services = %v, want the one that was there", stored.Services)
	}
}

func TestPlanDiffNeverCarriesTheCredential(t *testing.T) {
	h, _ := testHTTP(t)
	publish(t, h)

	w := do(t, h, http.MethodPatch, "/config?dry_run=true",
		`{"certificate":{"provider_token":"brand-new-secret"}}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body)
	}
	if strings.Contains(w.Body.String(), "brand-new-secret") {
		t.Fatalf("a plan diff leaked the credential:\n%s", w.Body)
	}
	if !strings.Contains(w.Body.String(), "withheld") {
		t.Errorf("the operator still has to be told the file changed:\n%s", w.Body)
	}
}

func TestPutServicePathWinsOverBody(t *testing.T) {
	h, applier := testHTTP(t)
	publish(t, h)

	// A caller that disagrees with itself has a bug; honouring the URL is what
	// keeps PUT idempotent on the address it was sent to.
	body := `{"name":"somethingelse","upstream":{"client":"nuc","port":9000}}`
	if w := do(t, h, http.MethodPut, "/services/grafana?confirm=true", body); w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body)
	}
	stored, _ := applier.Load()
	if len(stored.Services) != 1 || stored.Services[0].Name != "grafana" {
		t.Fatalf("stored = %v, want one service named grafana", stored.Services)
	}
	if stored.Services[0].Upstream.Port != 9000 {
		t.Errorf("port = %d, want the body's 9000", stored.Services[0].Upstream.Port)
	}
}

// Re-applying stored intent needs no confirmation: the operator confirmed it when
// they stored it, and the drifted box is the one that most needs repairing.
func TestReapplyNeedsNoConfirm(t *testing.T) {
	h, _ := testHTTP(t)
	publish(t, h)
	if w := do(t, h, http.MethodPost, "/apply", ""); w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body)
	}
}

func TestServiceLinkPathRoundTrips(t *testing.T) {
	h, applier := testHTTP(t)
	publish(t, h)
	w := do(t, h, http.MethodPut, "/services/grafana", `{"upstream":{"client":"nuc","port":3000},"link_path":"/ui"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("save: %d %s", w.Code, w.Body)
	}
	stored, err := applier.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := stored.Services[0].LinkPath; got != "/ui" {
		t.Fatalf("stored path = %q", got)
	}
	got := do(t, h, http.MethodGet, "/config", "")
	if !strings.Contains(got.Body.String(), `"link_path":"/ui"`) {
		t.Fatalf("config = %s", got.Body)
	}
}

func TestServiceIconUsesStoredLinkPath(t *testing.T) {
	h, applier := testHTTP(t)
	publish(t, h)
	if w := do(t, h, http.MethodPut, "/services/grafana", `{"upstream":{"client":"nuc","port":3000},"link_path":"/ui"}`); w.Code != http.StatusOK {
		t.Fatalf("save: %d %s", w.Code, w.Body)
	}
	var paths []string
	iconHandler := HTTP{Applier: applier, IconClient: &http.Client{Transport: iconTransport(func(r *http.Request) (*http.Response, error) {
		paths = append(paths, r.URL.Path)
		body := ""
		status := http.StatusOK
		if r.URL.Path == "/ui" {
			body = `<html><head><link rel="icon" href="/ui/icon.png"></head></html>`
		} else if r.URL.Path == "/ui/icon.png" {
			body = "\x89PNG\r\n\x1a\n" + strings.Repeat("x", 512)
		} else {
			status = http.StatusNotFound
		}
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header), Request: r}, nil
	})}}.Handler()
	w := do(t, iconHandler, http.MethodGet, "/services/grafana/icon", "")
	if w.Code != http.StatusOK || w.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("icon: %d %s (%v)", w.Code, w.Body, paths)
	}
	if paths[0] != "/ui" {
		t.Fatalf("first path = %s", paths[0])
	}
}

func TestServiceCustomIconRoundTrip(t *testing.T) {
	h, applier := testHTTP(t)
	publish(t, h)
	icon := "data:image/png;base64,iVBORw0KGgoAAA=="
	body := `{"upstream":{"client":"nuc","port":3000},"icon":"` + icon + `"}`
	if w := do(t, h, http.MethodPut, "/services/grafana", body); w.Code != http.StatusOK {
		t.Fatalf("save: %d %s", w.Code, w.Body)
	}
	got := do(t, h, http.MethodGet, "/config", "")
	if !strings.Contains(got.Body.String(), icon) {
		t.Fatalf("icon not persisted: %s", got.Body)
	}
	routes := HTTP{Applier: applier, IconClient: &http.Client{Transport: iconTransport(func(*http.Request) (*http.Response, error) {
		t.Fatal("custom icon should not discover favicon")
		return nil, nil
	})}}.Handler()
	w := do(t, routes, http.MethodGet, "/services/grafana/icon", "")
	if w.Code != http.StatusOK || w.Header().Get("Content-Type") != "image/png" || !strings.HasPrefix(w.Body.String(), "\x89PNG\r\n\x1a\n") {
		t.Fatalf("icon: %d %q", w.Code, w.Body.String())
	}
	if w := do(t, h, http.MethodPut, "/services/grafana", `{"upstream":{"client":"nuc","port":3000},"icon":"thesvg:../escape"}`); w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid icon: %d %s", w.Code, w.Body)
	}
}

package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

func signal(v int) *int { return &v }

func wirelessFixture(reads *atomic.Int32, signals []int) func(context.Context) (QualityWireless, error) {
	return func(context.Context) (QualityWireless, error) {
		reads.Add(1)
		report := QualityWireless{ObservedAt: time.Now().UTC(), APs: []WirelessAP{{ID: "ap", Name: "Study AP", State: "ok"}}}
		for i, value := range signals {
			report.Links = append(report.Links, WirelessLink{AP: "ap", MAC: fmt.Sprintf("mac-%d", i), SignalDBm: signal(value)})
		}
		if len(report.Links) > 0 {
			report.APs[0].Links = len(report.Links)
		}
		return report, nil
	}
}

// A link is scored on its own signal, an SNR cap can only lower that score, and
// a mean keeps one bad link from disappearing behind five good ones.
func TestWirelessScoreUsesSignalAndSNR(t *testing.T) {
	score, reason := wirelessScore(&QualityWireless{
		APs: []WirelessAP{{ID: "ap", State: "ok"}},
		Links: []WirelessLink{
			{MAC: "a", SignalDBm: signal(-50)},
			{MAC: "b", SignalDBm: signal(-60)},
			{MAC: "c", SignalDBm: signal(-70)},
		},
	})
	if score == nil || math.Abs(*score-85) > 1e-9 {
		t.Fatalf("mean of 100, 90 and 65: %v (%s)", score, reason)
	}
	if reason == "" {
		t.Fatal("a scored wireless category must say what it measured")
	}

	capped, _ := wirelessScore(&QualityWireless{
		APs:   []WirelessAP{{ID: "ap", State: "ok"}},
		Links: []WirelessLink{{MAC: "a", SignalDBm: signal(-50), SNRDb: signal(10)}},
	})
	if capped == nil || math.Abs(*capped-20) > 1e-9 {
		t.Fatalf("strong signal in a noisy band must be capped by SNR: %v", capped)
	}

	// A link the access point reported without a signal level is not a zero.
	partial, _ := wirelessScore(&QualityWireless{
		APs:   []WirelessAP{{ID: "ap", State: "ok"}},
		Links: []WirelessLink{{MAC: "a"}, {MAC: "b", SignalDBm: signal(-60)}},
	})
	if partial == nil || *partial != 90 {
		t.Fatalf("missing signal must be skipped, not scored zero: %v", partial)
	}

	for _, evidence := range []*QualityWireless{
		nil,
		{},
		{APs: []WirelessAP{{ID: "ap", State: "ok"}}},
	} {
		if score, reason := wirelessScore(evidence); score != nil || reason == "" {
			t.Fatalf("no observable link must stay unscored: %v %q", score, reason)
		}
	}
}

func TestQualityWirelessRunAndScopedRerun(t *testing.T) {
	var reads atomic.Int32
	first := wirelessFixture(&reads, []int{-50, -70})
	h := &HTTP{
		QualityPath:  filepath.Join(t.TempDir(), "quality.json"),
		qualityProbe: func(context.Context, qualityTarget, string) QualitySample { return qualityFixture("ok", 10)[0] },
		RunNAT:       func(context.Context) (NATResult, error) { return NATResult{Mapped: "203.0.113.1:1234"}, nil },
		Wireless:     first,
	}
	defer h.Close()
	routes := core.RouteTable(h.Routes())

	w := httptest.NewRecorder()
	routes.ServeHTTP(w, httptest.NewRequest("POST", "/quality", nil))
	if w.Code != http.StatusAccepted {
		t.Fatalf("full run: %d %s", w.Code, w.Body.String())
	}
	done := waitQualitySettled(t, h)
	if reads.Load() != 1 || done.Wireless == nil || len(done.Wireless.Links) != 2 || done.WirelessAt == nil {
		t.Fatalf("wireless evidence: reads=%d snapshot=%+v", reads.Load(), done.Wireless)
	}
	if wifi := qualityCategory(done, "wifi"); wifi.Score == nil || math.Abs(*wifi.Score-82.5) > 1e-9 || wifi.State != "ready" {
		t.Fatalf("wifi category: %+v", wifi)
	}
	// A measured Wi-Fi category joins the denominators instead of diluting the
	// categories that were already scored.
	if done.Overall.Available != 100 || done.Overall.Coverage <= 47.9 {
		t.Fatalf("wifi measurement must add its budget: %+v", done.Overall)
	}

	// The rerun replaces only the links and leaves every other measurement as
	// it was.
	h.Wireless = wirelessFixture(&reads, []int{-85})
	w = httptest.NewRecorder()
	routes.ServeHTTP(w, httptest.NewRequest("POST", "/quality?scope=wifi", nil))
	if w.Code != http.StatusAccepted {
		t.Fatalf("wifi rerun: %d %s", w.Code, w.Body.String())
	}
	after := waitQualitySettled(t, h)
	if reads.Load() != 2 || after.Wireless == nil || len(after.Wireless.Links) != 1 {
		t.Fatalf("wifi rerun evidence: reads=%d %+v", reads.Load(), after.Wireless)
	}
	if !reflect.DeepEqual(done.Targets, after.Targets) || !reflect.DeepEqual(done.NAT, after.NAT) {
		t.Fatal("wifi rerun replaced unrelated evidence")
	}
	if wifi := qualityCategory(after, "wifi"); wifi.Score == nil || *wifi.Score != 10 {
		t.Fatalf("wifi rerun did not rescore: %+v", wifi)
	}

	// Finished evidence survives a restart, timestamp included.
	h2 := &HTTP{QualityPath: h.QualityPath}
	if err := h2.RestoreQuality(); err != nil {
		t.Fatal(err)
	}
	if restored := h2.qualitySnapshot(); !reflect.DeepEqual(after, restored) {
		t.Fatalf("wireless evidence changed after restart: before=%+v after=%+v", after.Wireless, restored.Wireless)
	}
	raw, err := json.Marshal(after)
	if err != nil || !json.Valid(raw) || after.Wireless.Links[0].MAC != "mac-0" {
		t.Fatalf("wireless evidence must serialize with the snapshot: %v", err)
	}
}

func TestQualityWirelessNeedsAnAccessPoint(t *testing.T) {
	h := &HTTP{QualityPath: filepath.Join(t.TempDir(), "quality.json")}
	defer h.Close()
	routes := core.RouteTable(h.Routes())
	w := httptest.NewRecorder()
	routes.ServeHTTP(w, httptest.NewRequest("POST", "/quality?scope=wifi", nil))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("wifi scope without a source: %d %s", w.Code, w.Body.String())
	}
}

// An access point that stops answering explains itself and keeps the links the
// last read saw; it must not score zero or look like every client left.
func TestQualityWirelessFailureKeepsPreviousEvidence(t *testing.T) {
	var reads atomic.Int32
	h := &HTTP{
		QualityPath:  filepath.Join(t.TempDir(), "quality.json"),
		qualityProbe: func(context.Context, qualityTarget, string) QualitySample { return qualityFixture("ok", 10)[0] },
		RunNAT:       func(context.Context) (NATResult, error) { return NATResult{}, nil },
		Wireless:     wirelessFixture(&reads, []int{-60}),
	}
	defer h.Close()
	routes := core.RouteTable(h.Routes())
	routes.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/quality", nil))
	before := waitQualitySettled(t, h)
	if before.Wireless == nil {
		t.Fatal("first read did not land")
	}

	h.Wireless = func(context.Context) (QualityWireless, error) {
		return QualityWireless{}, errors.New("dial tcp 192.0.2.1:80: connection refused")
	}
	w := httptest.NewRecorder()
	routes.ServeHTTP(w, httptest.NewRequest("POST", "/quality?scope=wifi", nil))
	if w.Code != http.StatusAccepted {
		t.Fatalf("rerun: %d %s", w.Code, w.Body.String())
	}
	after := waitQualitySettled(t, h)
	if after.WirelessError == "" || after.Wireless == nil || len(after.Wireless.Links) != 1 {
		t.Fatalf("failed read must keep the previous links and name the failure: %+v", after)
	}
	if wifi := qualityCategory(after, "wifi"); wifi.Score == nil || *wifi.Score != 90 {
		t.Fatalf("previous evidence must still score: %+v", wifi)
	}
	if after.State != "completed" {
		t.Fatalf("state=%s", after.State)
	}
}

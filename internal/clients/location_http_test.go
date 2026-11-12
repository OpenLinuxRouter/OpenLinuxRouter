package clients

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// fakeLocator stands in for the equipment inventory.
type fakeLocator struct {
	locations []Location
	problems  []core.Problem
	err       error
	asked     []string
}

func (f *fakeLocator) Locations(_ context.Context, mac string) ([]Location, []core.Problem, error) {
	f.asked = append(f.asked, mac)
	return f.locations, f.problems, f.err
}

func locationServer(l Locator) http.Handler {
	h := HTTP{Applier: Applier{Locator: l}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/clients/{mac}/location", h.getLocation)
	return mux
}

func getLocation(t *testing.T, srv http.Handler, mac string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/clients/"+mac+"/location", nil))
	return rec
}

// What the equipment said is passed through verbatim: the port is the label on
// the switch, and a device that cannot answer is a problem rather than a silent
// "not there".
func TestLocationReturnsEachDevicesAnswerVerbatim(t *testing.T) {
	when := time.Date(2026, 11, 12, 15, 0, 0, 0, time.UTC)
	loc := &fakeLocator{
		locations: []Location{
			{DeviceID: "switch", Device: "MercurySwitch", Port: "5", VLAN: 10, ObservedAt: when},
		},
		problems: []core.Problem{{Path: "ap", Message: "XiaomiBE7000 cannot report which of its ports a client is on"}},
	}

	rec := getLocation(t, locationServer(loc), "aa:bb:cc:dd:ee:ff")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}

	var got locationResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v; body = %s", err, rec.Body)
	}
	if got.MAC != "aa:bb:cc:dd:ee:ff" {
		t.Errorf("MAC = %q", got.MAC)
	}
	if len(got.Locations) != 1 {
		t.Fatalf("locations = %+v", got.Locations)
	}
	l := got.Locations[0]
	if l.DeviceID != "switch" || l.Device != "MercurySwitch" || l.Port != "5" || l.VLAN != 10 {
		t.Errorf("location = %+v", l)
	}
	if !l.ObservedAt.Equal(when) {
		t.Errorf("ObservedAt = %v, want %v", l.ObservedAt, when)
	}
	if len(got.Problems) != 1 || got.Problems[0].Path != "ap" {
		t.Errorf("problems = %+v", got.Problems)
	}
}

// The path value is a MAC before it is a key: anything else is a 400, not a
// lookup that quietly finds nothing.
func TestLocationRejectsAnInvalidMAC(t *testing.T) {
	loc := &fakeLocator{}
	rec := getLocation(t, locationServer(loc), "not-a-mac")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if len(loc.asked) != 0 {
		t.Errorf("the inventory was asked about %v", loc.asked)
	}
}

// The path value reaches the inventory canonicalised, so callers can paste any
// case and the equipment is asked about one device rather than two.
func TestLocationNormalisesTheMACBeforeAsking(t *testing.T) {
	loc := &fakeLocator{}
	if rec := getLocation(t, locationServer(loc), "AA:BB:CC:DD:EE:FF"); rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	if len(loc.asked) != 1 || loc.asked[0] != "aa:bb:cc:dd:ee:ff" {
		t.Errorf("asked = %v, want the canonical MAC", loc.asked)
	}
}

// No inventory means no answer, and the shape still holds: an empty list rather
// than null, so a client renders "no equipment can say" without special-casing.
func TestLocationWithoutEquipmentIsAnEmptyList(t *testing.T) {
	rec := getLocation(t, locationServer(nil), "aa:bb:cc:dd:ee:ff")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"locations":[]`) {
		t.Errorf("body = %s, want an empty locations array", body)
	}
}

// A failure to read the inventory at all is a 500 rather than a claim that no
// device knows the client.
func TestLocationReportsAnUnreadableInventory(t *testing.T) {
	rec := getLocation(t, locationServer(&fakeLocator{err: errors.New("store unreadable")}), "aa:bb:cc:dd:ee:ff")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}

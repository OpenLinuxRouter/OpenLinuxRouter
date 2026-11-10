package network

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

type testAdapter struct {
	calls int
	fail  bool
	scan  ScanResult
}

func (a *testAdapter) Read(_ context.Context, d Device) (Snapshot, error) {
	return Snapshot{DeviceID: d.ID, ObservedAt: time.Now(), Collections: map[string]Collection{"ports": {State: "ok"}}}, nil
}
func (a *testAdapter) Scan(_ context.Context, d Device, radioID string) (ScanResult, error) {
	result := a.scan
	result.DeviceID = d.ID
	result.RadioID = radioID
	return result, nil
}
func (a *testAdapter) Plan(_ context.Context, d Device, op Operation) (OperationPlan, error) {
	return OperationPlan{DeviceID: d.ID, Operation: op, Supported: true, Impact: "Disconnect the selected client."}, nil
}
func (a *testAdapter) Execute(_ context.Context, d Device, op Operation) (OperationResult, error) {
	a.calls++
	if a.fail {
		return OperationResult{}, errors.New("lost reply")
	}
	return OperationResult{DeviceID: d.ID, Operation: op, State: "accepted", ObservedAt: time.Now()}, nil
}
func testService(t *testing.T) (*Service, *testAdapter) {
	t.Helper()
	store := core.NewStore(filepath.Join(t.TempDir(), "olr.json"), ModuleName, "other")
	_, err := Save(store, Config{Devices: []Device{{ID: "ap", Adapter: "openwrt", Address: "http://192.0.2.1", PasswordEnv: "AP_PASSWORD"}}})
	if err != nil {
		t.Fatal(err)
	}
	s := NewService(store)
	adapter := &testAdapter{}
	s.Adapters["openwrt"] = adapter
	return s, adapter
}
func TestOperationConfirmationIsBoundAndConsumedBeforeExecution(t *testing.T) {
	for _, lost := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "lost reply"}[lost], func(t *testing.T) {
			s, a := testService(t)
			a.fail = lost
			op := Operation{Kind: "client.disconnect", Target: "wlan0", ClientMAC: "aa:bb:cc:dd:ee:ff"}
			if _, err := s.Execute(context.Background(), "ap", op, ""); !errors.Is(err, ErrConfirmation) {
				t.Fatal(err)
			}
			plan, err := s.Plan(context.Background(), "ap", op)
			if err != nil || plan.Confirmation == "" || a.calls != 0 {
				t.Fatalf("plan=%+v err=%v calls=%d", plan, err, a.calls)
			}
			_, err = s.Execute(context.Background(), "ap", op, plan.Confirmation)
			if lost && err == nil {
				t.Fatal("lost outcome not reported")
			}
			if _, err = s.Execute(context.Background(), "ap", op, plan.Confirmation); !errors.Is(err, ErrConfirmation) {
				t.Fatal(err)
			}
			if a.calls != 1 {
				t.Fatalf("operation replayed: %d", a.calls)
			}
		})
	}
}
func TestOperationConfirmationRefusesChangedDeviceAndExpiredPlans(t *testing.T) {
	for _, change := range []string{"address", "operation", "expired"} {
		t.Run(change, func(t *testing.T) {
			s, a := testService(t)
			op := Operation{Kind: "client.disconnect", Target: "wlan0", ClientMAC: "aa:bb:cc:dd:ee:ff"}
			plan, err := s.Plan(context.Background(), "ap", op)
			if err != nil {
				t.Fatal(err)
			}
			switch change {
			case "address":
				c, _ := Load(s.Store)
				c.Devices[0].Address = "http://192.0.2.2"
				if _, err = Save(s.Store, c); err != nil {
					t.Fatal(err)
				}
			case "operation":
				op.Target = "wlan1"
			case "expired":
				p := s.pending[plan.Confirmation]
				p.Expires = time.Now().Add(-time.Second)
				s.pending[plan.Confirmation] = p
			}
			if _, err = s.Execute(context.Background(), "ap", op, plan.Confirmation); !errors.Is(err, ErrConfirmation) {
				t.Fatal(err)
			}
			if a.calls != 0 {
				t.Fatal("changed operation executed")
			}
		})
	}
}
func TestInventoryValidationAndOtherModulePreservation(t *testing.T) {
	s, _ := testService(t)
	doc, _ := s.Store.Load()
	doc.Set("other", []byte(`{"kept":true}`))
	if err := s.Store.Save(doc); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{
		`{"devices":[{"id":"a","adapter":"unknown","address":"http://192.0.2.1"}]}`,
		`{"devices":[{"id":"a","adapter":"openwrt","address":"http://user:password@192.0.2.1"}]}`,
		`{"devices":[{"id":"a","adapter":"openwrt","address":"http://192.0.2.1/ubus"}]}`,
		`{"devices":[{"id":"a","adapter":"openwrt","address":"http://192.0.2.1","password":"secret"}]}`,
		`{"devices":[{"id":"a","adapter":"openwrt","address":"http://192.0.2.1","roles":["printer"]}]}`,
		`{"devices":[]} {}`,
	} {
		if _, err := UnmarshalConfig([]byte(bad)); err == nil {
			t.Fatalf("accepted %s", bad)
		}
	}
	c, err := UnmarshalConfig([]byte(`{"devices":[{"id":"a","adapter":"mercury","address":"https://192.0.2.1/","roles":["switch","router","switch"]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = Save(s.Store, c); err != nil {
		t.Fatal(err)
	}
	doc, _ = s.Store.Load()
	raw, _ := doc.Raw("other")
	if !strings.Contains(string(raw), "true") {
		t.Fatal("dropped other module")
	}
}
func TestNetworkHTTPStrictInputsAndPublishedContract(t *testing.T) {
	s, a := testService(t)
	h := core.RouteTable((HTTP{Service: s, Lock: core.NewLock()}).Routes())
	tests := []struct {
		method, path, body string
		code               int
	}{
		{"GET", "/devices/missing/snapshot", "", 404},
		{"GET", "/devices/ap/snapshot", "", 200},
		{"GET", "/devices/ap/fdb?mac=invalid", "", 400},
		{"GET", "/devices/ap/fdb?mac=aa:bb:cc:dd:ee:ff&vlan=4095", "", 400},
		{"POST", "/devices/ap/operations/plan", `{"kind":"device.reboot","target":"x","client_mac":"aa:bb:cc:dd:ee:ff"}`, 422},
		{"POST", "/devices/ap/operations/plan", `{"kind":"client.disconnect","target":"wlan0","client_mac":"aa:bb:cc:dd:ee:ff","extra":true}`, 400},
		{"POST", "/devices/ap/operations/execute", `{"operation":{"kind":"client.disconnect","target":"wlan0","client_mac":"aa:bb:cc:dd:ee:ff"},"confirmation":"bogus"}`, 409},
		{"POST", "/devices/ap/scan", `{}`, 400},
		{"POST", "/devices/ap/scan", `{"radio_id":"phy1-ap0","extra":true}`, 400},
		{"POST", "/devices/ap/scan", `{"radio_id":"phy1-ap0"}`, 200},
		{"GET", "/contract", "", 200},
	}
	for _, tt := range tests {
		r := httptest.NewRequest(tt.method, tt.path, bytes.NewBufferString(tt.body))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tt.code {
			t.Errorf("%s %s = %d: %s", tt.method, tt.path, w.Code, w.Body)
		}
	}
	if a.calls != 0 {
		t.Fatal("read/preview invoked remote action")
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/contract", nil))
	var contract struct {
		Version int                        `json:"version"`
		Schemas map[string]json.RawMessage `json:"schemas"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &contract); err != nil || contract.Version != 1 || len(contract.Schemas) != 10 {
		t.Fatalf("contract=%s err=%v", w.Body, err)
	}
	if !bytes.Contains(contract.Schemas["device"], []byte(`"ap"`)) {
		t.Fatal("role enum missing")
	}
}
func TestProductionServiceResolvesCredentialsAndExecutesReturnedPlan(t *testing.T) {
	f := newOpenwrtTestServer(t)
	t.Setenv("DEVICE_PASSWORD", "LOGIN_SECRET")
	store := core.NewStore(filepath.Join(t.TempDir(), "olr.json"), ModuleName)
	device := f.device()
	device.Adapter = "openwrt"
	if _, err := Save(store, Config{Devices: []Device{device}}); err != nil {
		t.Fatal(err)
	}
	s := NewService(store)
	snapshot, err := s.Read(context.Background(), device.ID)
	if err != nil || !snapshot.Capabilities.Supports("client.disconnect") {
		t.Fatalf("snapshot %+v err %v", snapshot, err)
	}
	plan, err := s.Plan(context.Background(), device.ID, openwrtTestOperation())
	if err != nil || plan.Operation.ClientMAC != "02:aa:bb:cc:dd:ee" {
		t.Fatalf("plan %+v err %v", plan, err)
	}
	result, err := s.Execute(context.Background(), device.ID, plan.Operation, plan.Confirmation)
	if err != nil || result.State != "accepted" || f.count("hostapd.phy0-ap0", "del_client") != 1 {
		t.Fatalf("result %+v err %v", result, err)
	}
}

func TestHTTPQueryPreviewCanBeUsedByGeneratedTools(t *testing.T) {
	s, a := testService(t)
	h := core.RouteTable((HTTP{Service: s, Lock: core.NewLock()}).Routes())
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/devices/ap/operations/plan?kind=client.disconnect&target=wlan0&mac=02:aa:bb:cc:dd:ee", nil))
	var plan OperationPlan
	if err := json.Unmarshal(w.Body.Bytes(), &plan); err != nil || w.Code != 200 || plan.Confirmation == "" || a.calls != 0 {
		t.Fatalf("response %d %s err=%v", w.Code, w.Body, err)
	}
}

func TestInventoryWriteHasNoRemoteEffectsAndRejectsStaleETag(t *testing.T) {
	s, a := testService(t)
	h := core.RouteTable((HTTP{Service: s, Lock: core.NewLock()}).Routes())
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/config", nil))
	etag := w.Header().Get("ETag")
	if etag == "" {
		t.Fatal("missing ETag")
	}
	r := httptest.NewRequest("PUT", "/devices/switch", strings.NewReader(`{"adapter":"mercury","address":"http://192.0.2.3"}`))
	r.Header.Set("If-Match", etag)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	r = httptest.NewRequest("DELETE", "/devices/ap", nil)
	r.Header.Set("If-Match", etag)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 412 {
		t.Fatal(w.Code, w.Body)
	}
	if a.calls != 0 {
		t.Fatal("inventory changed remote device")
	}
}

type readOnlyAdapter struct{}

func (readOnlyAdapter) Read(_ context.Context, d Device) (Snapshot, error) {
	return Snapshot{DeviceID: d.ID}, nil
}

func TestScanNeedsARadioAndAnAdapterThatCanScan(t *testing.T) {
	s, _ := testService(t)
	if _, err := s.Scan(context.Background(), "ap", ""); !errors.Is(err, ErrInvalid) {
		t.Fatalf("empty radio = %v", err)
	}
	if _, err := s.Scan(context.Background(), "missing", "phy1-ap0"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown device = %v", err)
	}
	s.Adapters["openwrt"] = readOnlyAdapter{}
	if _, err := s.Scan(context.Background(), "ap", "phy1-ap0"); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("read-only adapter = %v", err)
	}
}

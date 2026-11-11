package devices

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

const openwrtTestSession = "0123456789abcdef0123456789abcdef"

type openwrtTestRequest struct {
	JSONRPC string            `json:"jsonrpc"`
	ID      uint64            `json:"id"`
	Method  string            `json:"method"`
	Params  []json.RawMessage `json:"params"`
}
type openwrtTestCall struct {
	Object, Method string
	Args           map[string]any
}
type openwrtTestServer struct {
	t          *testing.T
	server     *httptest.Server
	fixtures   map[string]json.RawMessage
	mu         sync.Mutex
	calls      []openwrtTestCall
	loginCount int
	username   string
	override   func(http.ResponseWriter, *http.Request, openwrtTestRequest, openwrtTestCall) bool
}

func newOpenwrtTestServer(t *testing.T) *openwrtTestServer {
	t.Helper()
	data, err := os.ReadFile("testdata/openwrt.json")
	if err != nil {
		t.Fatal(err)
	}
	f := &openwrtTestServer{t: t, username: "root"}
	if err := json.Unmarshal(data, &f.fixtures); err != nil {
		t.Fatal(err)
	}
	f.server = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.server.Close)
	return f
}
func (f *openwrtTestServer) serve(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || r.URL.Path != "/ubus" || r.Header.Get("Content-Type") != "application/json" {
		f.t.Errorf("unexpected HTTP request: %s %s", r.Method, r.URL.Path)
		http.Error(w, "bad request", 400)
		return
	}
	var req openwrtTestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		f.t.Errorf("decode: %v", err)
		return
	}
	if req.JSONRPC != "2.0" || req.ID == 0 {
		f.t.Errorf("invalid framing: %+v", req)
	}
	var call openwrtTestCall
	var token string
	if len(req.Params) > 0 {
		_ = json.Unmarshal(req.Params[0], &token)
	}
	if req.Method == "call" {
		if len(req.Params) != 4 {
			f.t.Errorf("call params count: %d", len(req.Params))
			return
		}
		_ = json.Unmarshal(req.Params[1], &call.Object)
		_ = json.Unmarshal(req.Params[2], &call.Method)
		_ = json.Unmarshal(req.Params[3], &call.Args)
	} else if req.Method == "list" {
		if len(req.Params) != 2 {
			f.t.Errorf("list params count: %d", len(req.Params))
			return
		}
		call.Method = "list"
		_ = json.Unmarshal(req.Params[1], &call.Object)
	} else {
		f.t.Errorf("unexpected RPC method %s", req.Method)
		return
	}
	f.mu.Lock()
	f.calls = append(f.calls, call)
	if call.Object == "session" && call.Method == "login" {
		f.loginCount++
	}
	f.mu.Unlock()
	if call.Object == "session" && call.Method == "login" {
		if token != openwrtAnonymous || !reflect.DeepEqual(call.Args, map[string]any{"username": f.username, "password": "LOGIN_SECRET"}) {
			f.t.Errorf("unexpected login framing")
		}
	} else if token != openwrtTestSession {
		f.t.Errorf("call lacks authenticated session")
	}
	if f.override != nil && f.override(w, r, req, call) {
		return
	}
	if call.Object == "session" && call.Method == "login" {
		openwrtTestReply(w, req, 0, map[string]any{"ubus_rpc_session": openwrtTestSession})
		return
	}
	key := call.Object + "." + call.Method
	if call.Method == "list" {
		key = "list"
		if call.Object != "hostapd.phy0-ap0" {
			f.t.Errorf("unexpected list target %q", call.Object)
		}
	}
	if call.Method == "access" && !reflect.DeepEqual(call.Args, map[string]any{"scope": "ubus", "object": "hostapd.phy0-ap0", "function": "del_client"}) {
		f.t.Errorf("unexpected ACL query: %+v", call.Args)
	}
	payload, ok := f.fixtures[key]
	if !ok {
		openwrtTestReply(w, req, 3, nil)
		return
	}
	if req.Method == "list" {
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": payload})
		return
	}
	openwrtTestReply(w, req, 0, payload)
}
func openwrtTestReply(w http.ResponseWriter, req openwrtTestRequest, code int, payload any) {
	result := []any{code}
	if payload != nil {
		result = append(result, payload)
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
}
func (f *openwrtTestServer) adapter() *OpenWrt {
	return &OpenWrt{Client: f.server.Client(), Password: func(key string) (string, error) {
		if key != "DEVICE_PASSWORD" {
			f.t.Errorf("password key %q", key)
		}
		return "LOGIN_SECRET", nil
	}}
}
func (f *openwrtTestServer) device() Device {
	return Device{ID: "ap", Address: f.server.URL, PasswordEnv: "DEVICE_PASSWORD"}
}
func (f *openwrtTestServer) count(object, method string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.calls {
		if c.Object == object && c.Method == method {
			n++
		}
	}
	return n
}
func openwrtTestOperation() Operation {
	return Operation{Kind: "client.disconnect", Target: "phy0-ap0", ClientMAC: "02:AA:BB:CC:DD:EE"}
}

func TestOpenWrtRead(t *testing.T) {
	f := newOpenwrtTestServer(t)
	snap, err := f.adapter().Read(context.Background(), f.device())
	if err != nil {
		t.Fatal(err)
	}
	if snap.DeviceID != "ap" || snap.Identity.Model != "Example OpenWrt AP" || snap.Identity.Hostname != "edge-ap" || snap.Identity.UptimeSeconds == nil || *snap.Identity.UptimeSeconds != 12345 {
		t.Fatalf("identity: %+v", snap.Identity)
	}
	for _, name := range []string{"identity", "ports", "statistics", "radios", "wireless", "associations"} {
		if snap.Collections[name].State != "ok" {
			t.Errorf("%s: %+v", name, snap.Collections[name])
		}
	}
	for _, name := range []string{"vlans", "pvids", "fdb", "neighbors"} {
		if snap.Collections[name].State != "unsupported" {
			t.Errorf("%s: %+v", name, snap.Collections[name])
		}
	}
	ports := map[string]Port{}
	for _, p := range snap.Ports {
		ports[p.ID] = p
	}
	bridge := ports["br-lan"]
	if bridge.RXBytes == nil || *bridge.RXBytes != 9007199254740993 || !reflect.DeepEqual(bridge.Members, []string{"lan1", "lan2"}) || !reflect.DeepEqual(bridge.Addresses, []string{"192.0.2.1/24", "2001:db8::1/64"}) {
		t.Fatalf("bridge: %+v", bridge)
	}
	if ports["eth0.20"].ParentID != "eth0" || ports["eth0.20"].Kind != "vlan" || ports["lan1"].Kind != "other" || ports["phy0-ap0"].Kind != "wireless" {
		t.Fatal("missing parent or incorrect normalized port kind")
	}
	if ports["lan1"].SpeedMbps == nil || *ports["lan1"].SpeedMbps != 1000 || ports["lan1"].Duplex != "full" {
		t.Fatal("link speed")
	}
	if ports["lan2"].RXBytes != nil || ports["lan2"].SpeedMbps != nil || ports["lan2"].LinkUp == nil || *ports["lan2"].LinkUp {
		t.Fatal("missing metrics must remain unknown, explicit carrier false preserved")
	}
	if len(snap.Radios) != 1 || snap.Radios[0].WidthMHz == nil || *snap.Radios[0].WidthMHz != 80 || snap.Radios[0].ConfiguredWidthMHz == nil || *snap.Radios[0].ConfiguredWidthMHz != 80 || snap.Radios[0].Channel == nil || *snap.Radios[0].Channel != 36 {
		t.Fatalf("radios: %+v", snap.Radios)
	}
	radio := snap.Radios[0]
	if radio.NoiseDBm == nil || *radio.NoiseDBm != -95 || radio.TxPowerDBm == nil || *radio.TxPowerDBm != 20 || radio.Country != "US" || radio.UtilizationPercent == nil || *radio.UtilizationPercent != 17.5 || !reflect.DeepEqual(radio.Protocols, []string{"n", "ac", "ax"}) {
		t.Fatalf("radio observations: %+v", radio)
	}
	if len(snap.Wireless) != 1 || snap.Wireless[0].BSSID != "02:11:22:33:44:60" || snap.Wireless[0].SSID != "Test network" {
		t.Fatalf("wireless: %+v", snap.Wireless)
	}
	if len(snap.Associations) != 2 {
		t.Fatalf("associations: %+v", snap.Associations)
	}
	a := snap.Associations[0]
	if a.RadioID != "radio0" || a.RXRateMbps == nil || *a.RXRateMbps != 144.4 || a.TXRateMbps == nil || *a.TXRateMbps != 866.7 || a.RXBytes == nil || *a.RXBytes != 333 || a.ConnectedSeconds == nil || *a.ConnectedSeconds != 99 {
		t.Fatalf("association: %+v", a)
	}
	if a.SignalAvgDBm == nil || *a.SignalAvgDBm != -53 || a.NoiseDBm == nil || *a.NoiseDBm != -95 || a.ExpectedMbps == nil || *a.ExpectedMbps != 866.7 || a.RXMCS == nil || *a.RXMCS != 7 || a.TXMCS == nil || *a.TXMCS != 9 || a.RXWidthMHz == nil || *a.RXWidthMHz != 40 || a.TXWidthMHz == nil || *a.TXWidthMHz != 80 || a.Retries == nil || *a.Retries != 3 || a.Failed == nil || *a.Failed != 4 || a.RXPackets == nil || *a.RXPackets != 9 || a.TXPackets == nil || *a.TXPackets != 10 || a.TXAirtimeUS == nil || *a.TXAirtimeUS != 1234 || a.MFP == nil || *a.MFP || a.WMM == nil || !*a.WMM {
		t.Fatalf("association enrichment: %+v", a)
	}
	if snap.Associations[1].SignalDBm != nil || snap.Associations[1].RXBytes != nil {
		t.Fatal("missing client metrics must remain unknown")
	}
	if !snap.Capabilities.Supports("client.disconnect") || snap.Capabilities.Supports("fdb.lookup") || snap.Capabilities.Supports("device.reboot") {
		t.Fatalf("capabilities: %+v", snap.Capabilities)
	}
	data, _ := json.Marshal(snap)
	for _, secret := range []string{"WIFI_SECRET_MUST_NOT_ESCAPE", "LOGIN_SECRET", openwrtTestSession} {
		if strings.Contains(string(data), secret) {
			t.Fatal("secret escaped snapshot")
		}
	}
	if f.count("session", "login") != 1 || f.count("hostapd.phy0-ap0", "del_client") != 0 {
		t.Fatal("read performed mutation or extra login")
	}
}
func TestOpenWrtPartialFailures(t *testing.T) {
	f := newOpenwrtTestServer(t)
	f.override = func(w http.ResponseWriter, r *http.Request, req openwrtTestRequest, c openwrtTestCall) bool {
		if c.Object == "system" && c.Method == "info" {
			openwrtTestReply(w, req, 6, nil)
			return true
		}
		if c.Object == "hostapd.phy0-ap0" && c.Method == "get_clients" {
			_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":`+fmt.Sprint(req.ID)+`,"result":[0,{"clients":null,"key":"SECRET"}]}`)
			return true
		}
		if c.Object == "iwinfo" && c.Method == "assoclist" {
			openwrtTestReply(w, req, 6, nil)
			return true
		}
		return false
	}
	snap, err := f.adapter().Read(context.Background(), f.device())
	if err != nil {
		t.Fatal(err)
	}
	if snap.Identity.Model == "" || snap.Identity.UptimeSeconds != nil || snap.Collections["identity"].State != "unsupported" || snap.Collections["associations"].State != "error" || snap.Collections["ports"].State != "ok" {
		t.Fatalf("partial snapshot: %+v", snap)
	}
	if f.count("session", "login") != 2 || f.count("system", "info") != 2 {
		t.Fatal("read retry not bounded")
	}
	if snap.Capabilities.Supports("associations.read") {
		t.Fatal("failed collection advertised as supported")
	}
	data, _ := json.Marshal(snap)
	if strings.Contains(string(data), "SECRET") {
		t.Fatal("response secret leaked")
	}
}
func TestOpenWrtReadRefreshOnlyOnce(t *testing.T) {
	for _, alwaysDenied := range []bool{false, true} {
		t.Run(fmt.Sprint(alwaysDenied), func(t *testing.T) {
			f := newOpenwrtTestServer(t)
			boardCalls := 0
			f.override = func(w http.ResponseWriter, r *http.Request, req openwrtTestRequest, c openwrtTestCall) bool {
				if c.Object == "system" {
					if c.Method == "board" {
						boardCalls++
					}
					if alwaysDenied || (c.Method == "board" && boardCalls == 1) {
						openwrtTestReply(w, req, 6, nil)
						return true
					}
				}
				return false
			}
			snap, err := f.adapter().Read(context.Background(), f.device())
			if err != nil {
				t.Fatal(err)
			}
			if f.count("session", "login") != 2 || f.count("system", "board") != 2 {
				t.Fatal("refresh count")
			}
			if !alwaysDenied && snap.Collections["identity"].State != "ok" {
				t.Fatal("refresh failed")
			}
		})
	}
}
func TestOpenWrtPlanAndExecute(t *testing.T) {
	f := newOpenwrtTestServer(t)
	op := openwrtTestOperation()
	plan, err := f.adapter().Plan(context.Background(), f.device(), op)
	if err != nil || !plan.Supported {
		t.Fatalf("plan %+v, %v", plan, err)
	}
	if f.count("hostapd.phy0-ap0", "del_client") != 0 {
		t.Fatal("plan mutated device")
	}
	result, err := f.adapter().Execute(context.Background(), f.device(), op)
	if err != nil || result.State != "accepted" {
		t.Fatalf("execute %+v, %v", result, err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.calls {
		if c.Method == "del_client" {
			n++
			want := map[string]any{"addr": "02:aa:bb:cc:dd:ee", "deauth": true, "reason": float64(5), "ban_time": float64(0)}
			if !reflect.DeepEqual(c.Args, want) {
				t.Fatalf("mutation params: %+v", c.Args)
			}
		}
	}
	if n != 1 {
		t.Fatalf("mutation count %d", n)
	}
}
func TestOpenWrtDisconnectRequiresMethodAndACL(t *testing.T) {
	for _, scenario := range []string{"missing-method", "missing-argument", "denied", "access-error", "unobserved", "invalid-mac", "unsupported-operation"} {
		t.Run(scenario, func(t *testing.T) {
			f := newOpenwrtTestServer(t)
			op := openwrtTestOperation()
			wantErr := ErrUnsupported
			switch scenario {
			case "missing-method":
				f.fixtures["list"] = json.RawMessage(`{"hostapd.phy0-ap0":{"get_clients":{}}}`)
			case "missing-argument":
				f.fixtures["list"] = json.RawMessage(`{"hostapd.phy0-ap0":{"del_client":{"addr":"String"}}}`)
			case "denied":
				f.fixtures["session.access"] = json.RawMessage(`{"access":false}`)
			case "access-error":
				f.override = func(w http.ResponseWriter, r *http.Request, req openwrtTestRequest, c openwrtTestCall) bool {
					if c.Method == "access" {
						openwrtTestReply(w, req, 6, nil)
						return true
					}
					return false
				}
			case "unobserved":
				op.Target = "phy9-ap9"
				wantErr = ErrInvalid
			case "invalid-mac":
				op.ClientMAC = "ff:ff:ff:ff:ff:ff"
				wantErr = ErrInvalid
			case "unsupported-operation":
				op.Kind = "device.reboot"
			}
			plan, err := f.adapter().Plan(context.Background(), f.device(), op)
			if !errors.Is(err, wantErr) || plan.Supported {
				t.Fatalf("plan %+v err %v", plan, err)
			}
			result, err := f.adapter().Execute(context.Background(), f.device(), op)
			if !errors.Is(err, wantErr) || result.State != "unknown" {
				t.Fatalf("execute %+v err %v", result, err)
			}
			if f.count("hostapd.phy0-ap0", "del_client") != 0 {
				t.Fatal("unsupported mutation sent")
			}
		})
	}
}
func TestOpenWrtExecuteNeverReplays(t *testing.T) {
	for _, scenario := range []string{"permission", "timeout", "lost-reply"} {
		t.Run(scenario, func(t *testing.T) {
			f := newOpenwrtTestServer(t)
			f.override = func(w http.ResponseWriter, r *http.Request, req openwrtTestRequest, c openwrtTestCall) bool {
				if c.Method != "del_client" {
					return false
				}
				switch scenario {
				case "permission":
					openwrtTestReply(w, req, 6, nil)
				case "timeout":
					<-r.Context().Done()
				case "lost-reply":
					conn, _, err := w.(http.Hijacker).Hijack()
					if err == nil {
						_ = conn.Close()
					}
				}
				return true
			}
			ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
			defer cancel()
			result, err := f.adapter().Execute(ctx, f.device(), openwrtTestOperation())
			if err == nil || result.State != "unknown" {
				t.Fatalf("result %+v, err %v", result, err)
			}
			if f.count("hostapd.phy0-ap0", "del_client") != 1 || f.count("session", "login") != 1 {
				t.Fatal("mutation replayed or reauthenticated")
			}
		})
	}
}
func TestOpenWrtMalformedAndSanitizedResponses(t *testing.T) {
	for _, response := range []string{`LOGIN_SECRET`, `{"jsonrpc":"2.0","id":1,"result":[0]}`, `{"jsonrpc":"2.0","id":999,"result":[0,{}]}`, `{"jsonrpc":"2.0","id":1,"result":{}}`, `{"jsonrpc":"2.0","id":1,"result":[0,null]}`, `{"jsonrpc":"2.0","id":1,"error":{"code":-999,"message":"LOGIN_SECRET"}}`} {
		t.Run(response, func(t *testing.T) {
			f := newOpenwrtTestServer(t)
			f.override = func(w http.ResponseWriter, r *http.Request, req openwrtTestRequest, c openwrtTestCall) bool {
				_, _ = io.WriteString(w, response)
				return true
			}
			_, err := f.adapter().Read(context.Background(), f.device())
			if err == nil || strings.Contains(err.Error(), "LOGIN_SECRET") {
				t.Fatalf("unsafe/missing error: %v", err)
			}
		})
	}
}
func TestOpenWrtRejectsRedirectAndUntrustedTLS(t *testing.T) {
	reached := false
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { reached = true }))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer redirect.Close()
	o := &OpenWrt{Password: func(string) (string, error) { return "SECRET", nil }}
	_, err := o.Read(context.Background(), Device{Address: redirect.URL})
	if err == nil || reached {
		t.Fatal("credential redirect followed")
	}
	tls := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer tls.Close()
	_, err = o.Read(context.Background(), Device{Address: tls.URL})
	if err == nil {
		t.Fatal("untrusted TLS accepted")
	}
}
func TestOpenWrtCredentialErrors(t *testing.T) {
	f := newOpenwrtTestServer(t)
	o := f.adapter()
	o.Password = func(string) (string, error) { return "", errors.New("SECRET") }
	_, err := o.Read(context.Background(), f.device())
	if !errors.Is(err, ErrInvalid) || strings.Contains(err.Error(), "SECRET") {
		t.Fatalf("credential resolver error %v", err)
	}
	f.override = func(w http.ResponseWriter, r *http.Request, req openwrtTestRequest, c openwrtTestCall) bool {
		if c.Method == "login" {
			openwrtTestReply(w, req, 6, nil)
			return true
		}
		return false
	}
	_, err = f.adapter().Read(context.Background(), f.device())
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("login rejection: %v", err)
	}
}
func TestOpenWrtExplicitUsername(t *testing.T) {
	f := newOpenwrtTestServer(t)
	f.username = "operator"
	d := f.device()
	d.Username = f.username
	_, err := f.adapter().Read(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}
}

func TestOpenWrtMalformedCollectionsDoNotReportEmptySuccess(t *testing.T) {
	for _, tc := range []struct{ key, payload, collection string }{
		{"system.board", `{}`, "identity"},
		{"system.info", `{}`, "identity"},
		{"network.device.status", `{"eth0":null}`, "ports"},
		{"network.interface.dump", `{}`, "ports"},
		{"network.wireless.status", `{"radio0":null}`, "wireless"},
	} {
		t.Run(tc.key+tc.payload, func(t *testing.T) {
			f := newOpenwrtTestServer(t)
			f.fixtures[tc.key] = json.RawMessage(tc.payload)
			snap, err := f.adapter().Read(context.Background(), f.device())
			if err != nil {
				t.Fatal(err)
			}
			if snap.Collections[tc.collection].State != "error" {
				t.Fatalf("collection %+v", snap.Collections[tc.collection])
			}
		})
	}
}

func TestOpenWrtOptionalMetricsAndEmptyInventory(t *testing.T) {
	f := newOpenwrtTestServer(t)
	delete(f.fixtures, "iwinfo.assoclist")
	f.fixtures["network.device.status"] = json.RawMessage(`{"lan1":{"up":true,"speed":null}}`)
	snap, err := f.adapter().Read(context.Background(), f.device())
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Ports) != 2 || snap.Ports[0].ID != "lan1" || snap.Ports[0].SpeedMbps != nil || snap.Ports[0].RXBytes != nil {
		t.Fatal("null or absent metrics became zeros")
	}
	if snap.Collections["associations"].State != "ok" || len(snap.Associations) != 2 || snap.Associations[0].SignalDBm == nil || *snap.Associations[0].SignalDBm != -52 || snap.Associations[0].RXRateMbps != nil {
		t.Fatal("optional iwinfo failure damaged hostapd observations")
	}
	f.fixtures["network.wireless.status"] = json.RawMessage(`{}`)
	snap, err = f.adapter().Read(context.Background(), f.device())
	if err != nil || snap.Collections["wireless"].State != "ok" || len(snap.Wireless) != 0 || snap.Capabilities.Supports("client.disconnect") {
		t.Fatalf("empty wireless inventory: %+v %v", snap, err)
	}
}

func TestOpenWrtResponseSizeBound(t *testing.T) {
	f := newOpenwrtTestServer(t)
	f.override = func(w http.ResponseWriter, r *http.Request, req openwrtTestRequest, c openwrtTestCall) bool {
		_, _ = io.WriteString(w, strings.Repeat("x", openwrtMaxBody+1))
		return true
	}
	_, err := f.adapter().Read(context.Background(), f.device())
	if err == nil || !strings.Contains(err.Error(), "size limit") {
		t.Fatalf("oversize body error %v", err)
	}
}

func TestOpenWrtLoginHTTPClassification(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			f := newOpenwrtTestServer(t)
			f.override = func(w http.ResponseWriter, r *http.Request, req openwrtTestRequest, c openwrtTestCall) bool {
				http.Error(w, "SECRET", status)
				return true
			}
			_, err := f.adapter().Read(context.Background(), f.device())
			want := ErrInvalid
			if status == http.StatusNotFound {
				want = ErrUnsupported
			}
			if !errors.Is(err, want) || strings.Contains(err.Error(), "SECRET") {
				t.Fatalf("status error %v", err)
			}
		})
	}
}

func TestOpenWrtTotalReadFailure(t *testing.T) {
	for _, permissionDenied := range []bool{true, false} {
		t.Run(fmt.Sprint(permissionDenied), func(t *testing.T) {
			f := newOpenwrtTestServer(t)
			f.override = func(w http.ResponseWriter, r *http.Request, req openwrtTestRequest, c openwrtTestCall) bool {
				if c.Object == "session" && c.Method == "login" {
					return false
				}
				if permissionDenied {
					openwrtTestReply(w, req, 6, nil)
				} else {
					_, _ = io.WriteString(w, `INVALID_BODY_SECRET`)
				}
				return true
			}
			snap, err := f.adapter().Read(context.Background(), f.device())
			if err == nil || strings.Contains(err.Error(), "SECRET") {
				t.Fatalf("unsafe or missing total failure: %v", err)
			}
			if permissionDenied && !errors.Is(err, ErrUnsupported) {
				t.Fatalf("total denied read: %v", err)
			}
			for _, name := range []string{"identity", "ports", "radios", "wireless", "associations"} {
				if snap.Collections[name].State == "ok" {
					t.Fatalf("%s falsely succeeded", name)
				}
			}
			if permissionDenied && f.count("session", "login") != 2 {
				t.Fatal("permission retry budget exceeded")
			}
		})
	}
}

func TestOpenWrtRolesUseDeclaredAndObservedAP(t *testing.T) {
	for _, tc := range []struct {
		name, wireless string
		declared, want []Role
	}{
		{name: "ap only", want: []Role{AP}},
		{name: "declared plus ap", declared: []Role{Router, Switch}, want: []Role{Router, Switch, AP}},
		{name: "declared ap once", declared: []Role{AP}, want: []Role{AP}},
		{name: "no wireless", wireless: `{}`},
		{name: "declared without wireless", wireless: `{}`, declared: []Role{Router}, want: []Role{Router}},
		{name: "station", wireless: `{"radio0":{"up":true,"disabled":false,"interfaces":[{"ifname":"phy0-sta0","config":{"mode":"sta","ssid":"upstream"}}]}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newOpenwrtTestServer(t)
			if tc.wireless != "" {
				f.fixtures["network.wireless.status"] = json.RawMessage(tc.wireless)
			}
			d := f.device()
			d.Roles = tc.declared
			snap, err := f.adapter().Read(context.Background(), d)
			if err != nil || !reflect.DeepEqual(snap.Identity.Roles, tc.want) {
				t.Fatalf("roles %v, want %v; err %v", snap.Identity.Roles, tc.want, err)
			}
		})
	}
}

func TestOpenWrtRadioChannelRequiresRuntimeObservation(t *testing.T) {
	for _, tc := range []struct {
		name, status, info string
		want               *int
	}{
		{name: "configured channel is not runtime", status: `{"bssid":"02:11:22:33:44:60","ssid":"Test network"}`, info: `{"noise":-95}`},
		{name: "zero runtime channel unknown", status: `{"bssid":"02:11:22:33:44:60","channel":0}`, info: `{"noise":-95}`},
		{name: "iwinfo supplies runtime channel", status: `{"bssid":"02:11:22:33:44:60","ssid":"Test network"}`, info: `{"channel":36}`, want: Ptr(36)},
		{name: "runtime overrides configuration", status: `{"bssid":"02:11:22:33:44:60","channel":149}`, info: `{"channel":36}`, want: Ptr(149)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newOpenwrtTestServer(t)
			f.fixtures["hostapd.phy0-ap0.get_status"] = json.RawMessage(tc.status)
			f.fixtures["iwinfo.info"] = json.RawMessage(tc.info)
			snap, err := f.adapter().Read(context.Background(), f.device())
			if err != nil || len(snap.Radios) != 1 || !reflect.DeepEqual(snap.Radios[0].Channel, tc.want) {
				t.Fatalf("radio channel %+v; err %v", snap.Radios, err)
			}
		})
	}
}

func TestOpenWrtDiscardsInvalidPartialDecodes(t *testing.T) {
	f := newOpenwrtTestServer(t)
	f.fixtures["system.board"] = json.RawMessage(`{"model":"TAINTED_BOARD","hostname":"TAINTED_HOST","release":{"version":123}}`)
	f.fixtures["system.info"] = json.RawMessage(`{"uptime":"invalid"}`)
	f.fixtures["network.interface.dump"] = json.RawMessage(`{"interface":[{"l3_device":"br-lan","ipv4-address":[{"address":"192.0.2.1","mask":24}]},{"l3_device":123}]}`)
	snap, err := f.adapter().Read(context.Background(), f.device())
	if err != nil {
		t.Fatal(err)
	}
	if snap.Identity.Model != "" || snap.Identity.Hostname != "" || snap.Identity.Firmware != "" || snap.Identity.UptimeSeconds != nil || snap.Collections["identity"].State != "error" {
		t.Fatalf("invalid identity data reused: %+v", snap.Identity)
	}
	for _, port := range snap.Ports {
		if len(port.Addresses) != 0 {
			t.Fatal("invalid interface dump partial data reused")
		}
	}
	if snap.Collections["ports"].State != "error" || snap.Collections["wireless"].State != "ok" {
		t.Fatal("partial failure damaged healthy collections")
	}
}

type openwrtTestRoundTrip func(*http.Request) (*http.Response, error)

func (f openwrtTestRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestOpenWrtDisconnectDiscoveryFailuresStayUnknown(t *testing.T) {
	for _, tc := range []struct {
		scenario, want string
	}{
		{"list-invalid-json", "unknown"},
		{"list-http-error", "unknown"},
		{"access-missing", "unknown"},
		{"access-invalid-json", "unknown"},
		{"status-invalid-json", "unknown"},
		{"clients-invalid-json", "supported"},
		{"access-deadline", "unknown"},
	} {
		scenario := tc.scenario
		t.Run(scenario, func(t *testing.T) {
			f := newOpenwrtTestServer(t)
			if scenario == "access-missing" {
				f.fixtures["session.access"] = json.RawMessage(`{}`)
			}
			f.override = func(w http.ResponseWriter, r *http.Request, req openwrtTestRequest, c openwrtTestCall) bool {
				broken := (scenario == "list-invalid-json" && req.Method == "list") || (scenario == "access-invalid-json" && c.Method == "access") || (scenario == "status-invalid-json" && c.Method == "get_status") || (scenario == "clients-invalid-json" && c.Method == "get_clients")
				if broken {
					_, _ = io.WriteString(w, "INVALID_SECRET")
					return true
				}
				if scenario == "list-http-error" && req.Method == "list" {
					http.Error(w, "HTTP_SECRET", http.StatusInternalServerError)
					return true
				}
				return false
			}
			o := f.adapter()
			if scenario == "access-deadline" {
				base := o.Client.Transport
				if base == nil {
					base = http.DefaultTransport
				}
				o.Client.Transport = openwrtTestRoundTrip(func(r *http.Request) (*http.Response, error) {
					copyBody, err := r.GetBody()
					if err != nil {
						return nil, err
					}
					defer copyBody.Close()
					var req openwrtTestRequest
					if err := json.NewDecoder(copyBody).Decode(&req); err != nil {
						return nil, err
					}
					if len(req.Params) == 4 {
						var object, method string
						_ = json.Unmarshal(req.Params[1], &object)
						_ = json.Unmarshal(req.Params[2], &method)
						if object == "session" && method == "access" {
							return nil, context.DeadlineExceeded
						}
					}
					return base.RoundTrip(r)
				})
			}
			snap, err := o.Read(context.Background(), f.device())
			if err != nil || snap.Capabilities["client.disconnect"].State != tc.want {
				t.Fatalf("capability %+v, want %s, read error %v", snap.Capabilities["client.disconnect"], tc.want, err)
			}
			// get_clients is not a planning prerequisite; its read failure still
			// must not prove that the independent del_client method is absent.
			if scenario != "clients-invalid-json" {
				plan, err := o.Plan(context.Background(), f.device(), openwrtTestOperation())
				if err == nil || errors.Is(err, ErrUnsupported) || plan.Supported || strings.Contains(err.Error(), "SECRET") {
					t.Fatalf("plan %+v, unsafe or misclassified error %v", plan, err)
				}
				if scenario == "access-deadline" && !errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("lost deadline identity: %v", err)
				}
				result, err := o.Execute(context.Background(), f.device(), openwrtTestOperation())
				if err == nil || errors.Is(err, ErrUnsupported) || result.State != "unknown" {
					t.Fatalf("execute %+v, error %v", result, err)
				}
			}
			if f.count("hostapd.phy0-ap0", "del_client") != 0 {
				t.Fatal("failed discovery performed a mutation")
			}
		})
	}
}

func TestOpenWrtWirelessFallsBackToLuciRPC(t *testing.T) {
	f := newOpenwrtTestServer(t)
	var radios map[string]any
	if err := json.Unmarshal(f.fixtures["network.wireless.status"], &radios); err != nil {
		t.Fatal(err)
	}
	delete(f.fixtures, "network.wireless.status")
	radio := radios["radio0"].(map[string]any)
	for _, raw := range radio["interfaces"].([]any) {
		intf := raw.(map[string]any)
		intf["iwinfo"] = map[string]any{"ssid": "Test network", "bssid": "02:11:22:33:44:60", "channel": 36, "htmode": "VHT80", "hwmodes": []string{"n", "ac", "ax"}, "noise": -95, "txpower": 20, "country": "US"}
	}
	data, _ := json.Marshal(map[string]any{"radio0": radio})
	f.fixtures["luci-rpc.getWirelessDevices"] = data
	snap, err := f.adapter().Read(context.Background(), f.device())
	if err != nil {
		t.Fatal(err)
	}
	if snap.Collections["wireless"].State != "ok" || len(snap.Radios) != 1 || len(snap.Wireless) != 1 || snap.Wireless[0].BSSID != "02:11:22:33:44:60" {
		t.Fatalf("fallback wireless: %+v %+v", snap.Radios, snap.Wireless)
	}
	if snap.Radios[0].WidthMHz == nil || *snap.Radios[0].WidthMHz != 80 || snap.Radios[0].NoiseDBm == nil || *snap.Radios[0].NoiseDBm != -95 {
		t.Fatalf("fallback radio observations: %+v", snap.Radios[0])
	}
}

func TestOpenWrtAssociationsFallBackToIWinfo(t *testing.T) {
	f := newOpenwrtTestServer(t)
	f.fixtures["hostapd.phy0-ap0.get_clients"] = json.RawMessage(`{"clients":null}`)
	snap, err := f.adapter().Read(context.Background(), f.device())
	if err != nil {
		t.Fatal(err)
	}
	if snap.Collections["associations"].State != "ok" || len(snap.Associations) != 1 {
		t.Fatalf("iwinfo fallback associations: %+v", snap.Associations)
	}
	if snap.Associations[0].TXRateMbps == nil || *snap.Associations[0].TXRateMbps != 866.7 {
		t.Fatalf("fallback association metrics: %+v", snap.Associations[0])
	}
}

func TestOpenWrtScanReportsNeighbours(t *testing.T) {
	f := newOpenwrtTestServer(t)
	result, err := f.adapter().Scan(context.Background(), f.device(), "phy1-ap0")
	if err != nil {
		t.Fatal(err)
	}
	if result.DeviceID != "ap" || result.RadioID != "phy1-ap0" || len(result.Neighbours) != 5 {
		t.Fatalf("scan result: %+v", result)
	}
	var order []string
	for _, n := range result.Neighbours {
		order = append(order, n.BSSID)
	}
	// Channel order first, strongest first within a channel; the repeat hearing
	// and the unparseable BSSID in the fixture are dropped.
	want := []string{"00:11:22:33:44:59", "00:11:22:33:44:58", "00:11:22:33:44:55", "00:11:22:33:44:56", "00:11:22:33:44:57"}
	if !reflect.DeepEqual(order, want) {
		t.Fatalf("order %v", order)
	}
	byBSSID := map[string]Neighbour{}
	for _, n := range result.Neighbours {
		byBSSID[n.BSSID] = n
	}
	check := func(mac string, width int, transport, encryption string, open bool, signal int) {
		t.Helper()
		n := byBSSID[mac]
		if n.WidthMHz == nil || *n.WidthMHz != width || n.Transport != transport || n.Encryption != encryption || n.Open == nil || *n.Open != open || n.SignalDBm == nil || *n.SignalDBm != signal {
			t.Fatalf("%s = %+v", mac, n)
		}
	}
	check("00:11:22:33:44:55", 80, "ac", "WPA2", false, -62)
	check("00:11:22:33:44:56", 40, "n", "WPA+WPA2", false, -70)
	check("00:11:22:33:44:57", 20, "n", "", true, -80)
	check("00:11:22:33:44:58", 160, "ac", "WPA3", false, -88)
	// No channel, no width, no SSID and no privacy block: every one of those
	// stays absent rather than being reported as zero or "open".
	if bare := byBSSID["00:11:22:33:44:59"]; bare.WidthMHz != nil || bare.Channel != nil || bare.SSID != "" || bare.Transport != "" || bare.Encryption != "" || bare.Open != nil {
		t.Fatalf("unknown neighbour filled in: %+v", bare)
	}
	f.mu.Lock()
	last := f.calls[len(f.calls)-1]
	f.mu.Unlock()
	if last.Object != "iwinfo" || last.Method != "scan" || last.Args["device"] != "phy1-ap0" {
		t.Fatalf("scan call: %+v", last)
	}
}

func TestOpenWrtScanPermissionAndMalformedAnswer(t *testing.T) {
	t.Run("denied", func(t *testing.T) {
		f := newOpenwrtTestServer(t)
		f.override = func(w http.ResponseWriter, r *http.Request, req openwrtTestRequest, c openwrtTestCall) bool {
			if c.Object == "iwinfo" && c.Method == "scan" {
				openwrtTestReply(w, req, 6, nil)
				return true
			}
			return false
		}
		if _, err := f.adapter().Scan(context.Background(), f.device(), "phy1-ap0"); !errors.Is(err, ErrUnsupported) {
			t.Fatalf("denied scan = %v", err)
		}
	})
	t.Run("malformed", func(t *testing.T) {
		f := newOpenwrtTestServer(t)
		f.override = func(w http.ResponseWriter, r *http.Request, req openwrtTestRequest, c openwrtTestCall) bool {
			if c.Object == "iwinfo" && c.Method == "scan" {
				_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":`+fmt.Sprint(req.ID)+`,"result":[0,{"neighbours":[]}]}`)
				return true
			}
			return false
		}
		if _, err := f.adapter().Scan(context.Background(), f.device(), "phy1-ap0"); err == nil || errors.Is(err, ErrUnsupported) {
			t.Fatalf("malformed scan = %v", err)
		}
	})
}

func TestOpenWrtScanAcceptsTheSnapshotRadioID(t *testing.T) {
	f := newOpenwrtTestServer(t)
	result, err := f.adapter().Scan(context.Background(), f.device(), "radio0")
	if err != nil {
		t.Fatal(err)
	}
	if result.RadioID != "radio0" || len(result.Neighbours) != 5 {
		t.Fatalf("scan: %+v", result)
	}
	f.mu.Lock()
	last := f.calls[len(f.calls)-1]
	f.mu.Unlock()
	// radio0 is phy0-ap0 in the inventory fixture; iwinfo must be asked for the
	// interface, not for netifd's radio name.
	if last.Method != "scan" || last.Args["device"] != "phy0-ap0" {
		t.Fatalf("scan call: %+v", last)
	}
}

func TestOpenWrtScanRejectsAnUnknownRadio(t *testing.T) {
	f := newOpenwrtTestServer(t)
	f.override = func(w http.ResponseWriter, r *http.Request, req openwrtTestRequest, c openwrtTestCall) bool {
		if c.Object == "iwinfo" && c.Method == "devices" {
			openwrtTestReply(w, req, 0, map[string]any{"devices": []string{"phy0-ap0"}})
			return true
		}
		return false
	}
	if _, err := f.adapter().Scan(context.Background(), f.device(), "radio9"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unknown radio = %v", err)
	}
}

func TestOpenWrtUsesAStoredCredential(t *testing.T) {
	f := newOpenwrtTestServer(t)
	d := f.device()
	d.Password = "LOGIN_SECRET"
	d.PasswordEnv = ""
	// No resolver at all: the inventory's own secret is the only source.
	o := &OpenWrt{Client: f.server.Client()}
	snap, err := o.Read(context.Background(), d)
	if err != nil || snap.Identity.Model == "" {
		t.Fatalf("read with stored credential: %+v %v", snap.Identity, err)
	}
}

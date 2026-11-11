package devices

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func mercuryFixture(t *testing.T, name string) string {
	t.Helper()
	b, e := os.ReadFile("testdata/mercury_" + name)
	if e != nil {
		t.Fatal(e)
	}
	return string(b)
}

type mercuryTestDevice struct {
	mu             sync.Mutex
	pages          map[string]string
	requests       []string
	posts          int
	initialCookies []string
	loginFailure   bool
}

func mercuryServer(t *testing.T, authenticated bool) (*httptest.Server, *mercuryTestDevice) {
	t.Helper()
	f := &mercuryTestDevice{pages: map[string]string{
		"/MainRpm.htm": mercuryFixture(t, "main.htm"), "/PortStatisticsRpm.htm": mercuryFixture(t, "statistics.htm"),
		"/Vlan8021QRpm.htm": mercuryFixture(t, "vlans.htm"), "/Vlan8021QPvidRpm.htm": mercuryFixture(t, "pvids.htm"),
		"/": mercuryFixture(t, "root.htm"), "/cryp_new.js": mercuryFixture(t, "crypto.js"),
		"/mac_address_search.cgi": mercuryFixture(t, "mac_empty.htm"),
	}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.requests = append(f.requests, r.Method+" "+r.RequestURI)
		switch r.URL.Path {
		case "/logon.cgi":
			f.posts++
			if r.Method != "POST" {
				t.Error("login must use POST")
			}
			if e := r.ParseForm(); e != nil {
				t.Error(e)
			}
			if r.PostForm.Get("username") == "" || r.PostForm.Get("password") != "77VtUtxvyTefbwK" || r.PostForm.Get("logon") != "登录" || len(r.PostForm) != 3 {
				t.Error("unexpected login form")
			}
			if f.loginFailure {
				io.WriteString(w, `var logonInfo = new Array(1,0,0); secret upstream message`)
				return
			}
			http.SetCookie(w, &http.Cookie{Name: "SessionID", Value: "synthetic-session", Path: "/"})
			io.WriteString(w, `var logonInfo = new Array(0,0,0);`)
			return
		case "/cryp_new.js":
		default:
			if r.Method != "GET" {
				t.Errorf("unexpected mutation: %s %s", r.Method, r.URL.Path)
			}
			if r.URL.Path == "/MainRpm.htm" {
				f.initialCookies = append(f.initialCookies, r.Header.Get("Cookie"))
			}
			if authenticated {
				if c, e := r.Cookie("SessionID"); e != nil || c.Value != "synthetic-session" {
					io.WriteString(w, `<form action="logon.cgi"><input id="plain_password"></form>`)
					return
				}
			}
		}
		page, ok := f.pages[r.URL.Path]
		if !ok {
			t.Errorf("unknown request %s", r.RequestURI)
			w.WriteHeader(404)
			return
		}
		io.WriteString(w, page)
	}))
	t.Cleanup(srv.Close)
	return srv, f
}
func mercuryDevice(address string) Device {
	return Device{ID: "switch", Address: address, Username: "fixture-user", PasswordEnv: "FIXTURE_PASSWORD"}
}
func mercuryAdapter(client *http.Client) *Mercury {
	return &Mercury{Client: client, Password: func(key string) (string, error) {
		if key != "FIXTURE_PASSWORD" {
			return "", errors.New("secret resolver diagnostic")
		}
		return "test-pass", nil
	}}
}

func TestMercuryReadCapturedPages(t *testing.T) {
	srv, f := mercuryServer(t, true)
	m := mercuryAdapter(srv.Client())
	s, e := m.Read(context.Background(), mercuryDevice(srv.URL))
	if e != nil {
		t.Fatal(e)
	}
	if s.DeviceID != "switch" || s.Identity.Model != "SE109 Pro" || s.Identity.Firmware != "1.0.0 Build 20240914 Rel.40108" || s.Identity.Manufacturer != "Mercury" {
		t.Fatalf("identity: %+v", s.Identity)
	}
	if !reflect.DeepEqual(s.Identity.MACs, []string{"02:00:00:00:00:09"}) || s.Identity.UptimeSeconds == nil || *s.Identity.UptimeSeconds != 9146785 {
		t.Fatalf("identity fields: %+v", s.Identity)
	}
	if len(s.Ports) != 9 {
		t.Fatalf("ports: %d", len(s.Ports))
	}
	if s.Ports[0].Kind != "ethernet" || s.Ports[8].Kind != "fiber" {
		t.Fatalf("captured media: port 1 %q, port 9 %q", s.Ports[0].Kind, s.Ports[8].Kind)
	}
	if s.VLANEnabled == nil || !*s.VLANEnabled {
		t.Fatal("captured global VLAN enabled state lost")
	}
	p := s.Ports[2]
	if p.ID != "3" || p.LinkUp == nil || !*p.LinkUp || p.SpeedMbps == nil || *p.SpeedMbps != 1000 || p.Duplex != "full" {
		t.Fatalf("link: %+v", p)
	}
	if p.RXMbps == nil || *p.RXMbps != 242.412 || *p.TXMbps != 6.205 {
		t.Fatalf("rates: %+v", p)
	}
	if *p.TXPackets != 1001004328 || *p.TXErrors != 1 || *p.RXPackets != 25870636 || *p.RXErrors != 0 {
		t.Fatalf("counters: %+v", p)
	}
	if p.RXBytes != nil || p.TXBytes != nil || p.PoE != nil {
		t.Fatal("unsupported counters or PoE fabricated")
	}
	if *s.Ports[1].SpeedMbps != 2500 || *s.Ports[0].LinkUp || s.Ports[0].SpeedMbps != nil {
		t.Fatal("link mode mapping")
	}
	if len(s.VLANs) != 2 || !reflect.DeepEqual(s.VLANs[1].Untagged, []string{"4", "6"}) || len(s.VLANs[1].Tagged) != 0 || *s.Ports[3].PVID != 10 || *s.Ports[5].PVID != 10 {
		t.Fatalf("VLANs: %+v", s.VLANs)
	}
	for _, key := range []string{"identity", "ports", "statistics", "vlans", "pvids"} {
		if s.Collections[key].State != "ok" || s.Collections[key].ObservedAt.IsZero() {
			t.Errorf("collection %s: %+v", key, s.Collections[key])
		}
	}
	if s.Capabilities.Supports("fdb.list") || !s.Capabilities.Supports("fdb.lookup") || s.Capabilities.Supports("client.disconnect") {
		t.Fatal("incorrect capabilities")
	}
	for _, key := range []string{"ports.configure", "vlans.configure", "wireless.configure", "poe.configure", "device.reboot"} {
		if s.Capabilities[key].State != "unsupported" || s.Capabilities[key].Reason == "" {
			t.Errorf("configuration capability %s: %+v", key, s.Capabilities[key])
		}
	}
	if _, ok := any(m).(Operator); ok {
		t.Fatal("write capability must not be implemented")
	}
	if f.posts != 1 {
		t.Fatalf("login count %d", f.posts)
	}
	for _, r := range f.requests {
		if strings.Contains(r, "_set.cgi") || strings.Contains(r, "Set.cgi") {
			t.Fatalf("mutation %s", r)
		}
	}
}

func TestMercuryPartialSections(t *testing.T) {
	tests := []struct{ name, path, old, replacement, section string }{
		{"statistics count mismatch", "/PortStatisticsRpm.htm", "max_port_num = 9", "max_port_num = 8", "statistics"},
		{"statistics short array", "/PortStatisticsRpm.htm", "pkts:[", "pkts:[], unused:[", "statistics"},
		{"statistics negative", "/PortStatisticsRpm.htm", "pkts:[4466", "pkts:[-1", "statistics"},
		{"main excessive count", "/MainRpm.htm", "max_port_num = 9", "max_port_num = 999999999999999999999999999", "ports"},
		{"main short state", "/MainRpm.htm", "state:[1,1,1,1,1,1,1,1,1,0,0]", "state:[1]", "ports"},
		{"vlan excessive count", "/Vlan8021QRpm.htm", "count:2", "count:1000000", "vlans"},
		{"vlan invalid bitmap", "/Vlan8021QRpm.htm", "0x1D7", "0x10000", "vlans"},
		{"pvid short array", "/Vlan8021QPvidRpm.htm", "1,1,1,10,1,10,1,1,1", "1,1", "pvids"},
		{"identity unknown schema", "/MainRpm.htm", "hardwareStr:[\"SE109 Pro\"]", "hardwareStr:[\"Unknown Switch\"]", "identity"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			srv, f := mercuryServer(t, false)
			f.pages[test.path] = strings.Replace(f.pages[test.path], test.old, test.replacement, 1)
			s, e := mercuryAdapter(srv.Client()).Read(context.Background(), mercuryDevice(srv.URL))
			if e != nil {
				t.Fatal(e)
			}
			if s.Collections[test.section].State != "error" {
				t.Fatalf("%s not marked error: %+v", test.section, s.Collections)
			}
			if s.Capabilities[test.section+".read"].State != "unknown" {
				t.Fatal("failed schema claimed supported")
			}
			for _, key := range []string{"identity", "ports", "statistics", "vlans", "pvids"} {
				if key != test.section && s.Collections[key].State != "ok" {
					t.Errorf("successful %s lost: %+v", key, s.Collections[key])
				}
			}
			if len(s.Ports) != 9 {
				t.Fatal("successful port collection lost")
			}
			if test.section == "statistics" && s.Ports[0].TXPackets != nil {
				t.Fatal("partial invalid statistics were applied")
			}
		})
	}
}
func TestMercuryDisabledVLANsKeepConfiguredData(t *testing.T) {
	srv, f := mercuryServer(t, false)
	f.pages["/Vlan8021QRpm.htm"] = strings.Replace(f.pages["/Vlan8021QRpm.htm"], "state:1", "state:0", 1)
	f.pages["/Vlan8021QPvidRpm.htm"] = strings.Replace(f.pages["/Vlan8021QPvidRpm.htm"], "state:1", "state:0", 1)
	s, err := mercuryAdapter(srv.Client()).Read(context.Background(), mercuryDevice(srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	if s.VLANEnabled == nil || *s.VLANEnabled {
		t.Fatal("disabled global VLAN state lost")
	}
	if len(s.VLANs) != 2 || s.Ports[3].PVID == nil || *s.Ports[3].PVID != 10 || s.Collections["vlans"].State != "ok" || s.Collections["pvids"].State != "ok" {
		t.Fatal("disabled VLAN configuration must remain observable")
	}
}
func TestMercuryUnknownVLANState(t *testing.T) {
	srv, f := mercuryServer(t, false)
	f.pages["/Vlan8021QRpm.htm"] = strings.Replace(f.pages["/Vlan8021QRpm.htm"], "state:1", "state:2", 1)
	s, err := mercuryAdapter(srv.Client()).Read(context.Background(), mercuryDevice(srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	if s.VLANEnabled != nil || s.Collections["vlans"].State != "error" || s.Collections["pvids"].State != "ok" {
		t.Fatal("invalid VLAN state invented a global value or lost PVIDs")
	}
}
func TestMercuryPortMediaCodes(t *testing.T) {
	page := mercuryFixture(t, "main.htm")
	original := "port_type:[0,0,0,0,0,0,0,0,1,0,0]"
	for _, test := range []struct {
		name, replacement string
		kinds             []string
	}{
		{"known and ambiguous codes", "port_type:[0,1,2,6,3,4,5,99,-1]", []string{"ethernet", "fiber", "fiber", "fiber", "other", "other", "other", "other", "other"}},
		{"missing array", "unused_media:[]", []string{"other", "other", "other", "other", "other", "other", "other", "other", "other"}},
		{"short array", "port_type:[0]", []string{"ethernet", "other", "other", "other", "other", "other", "other", "other", "other"}},
		{"malformed values", "port_type:['0',null,{},true]", []string{"other", "other", "other", "other", "other", "other", "other", "other", "other"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			ports, err := mercuryMainPorts(strings.Replace(page, original, test.replacement, 1))
			if err != nil {
				t.Fatal(err)
			}
			for i, want := range test.kinds {
				if ports[i].Kind != want {
					t.Errorf("port %d kind %q, want %q", i+1, ports[i].Kind, want)
				}
			}
		})
	}
}
func TestMercuryStatisticsCannotInventPortMedia(t *testing.T) {
	srv, f := mercuryServer(t, false)
	f.pages["/MainRpm.htm"] = "unknown firmware page"
	s, err := mercuryAdapter(srv.Client()).Read(context.Background(), mercuryDevice(srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	if s.Collections["ports"].State != "error" || s.Collections["statistics"].State != "ok" || len(s.Ports) != 9 {
		t.Fatal("partial statistics were lost")
	}
	for _, p := range s.Ports {
		if p.Kind != "other" {
			t.Errorf("fallback port %s invented medium %q", p.ID, p.Kind)
		}
	}
}

func TestMercuryOptionalUnknownValues(t *testing.T) {
	page := mercuryFixture(t, "main.htm")
	page = strings.Replace(page, "spd_act:[0,7", "spd_act:[0,99", 1)
	ports, e := mercuryMainPorts(page)
	if e != nil {
		t.Fatal(e)
	}
	if ports[1].LinkUp != nil || ports[1].SpeedMbps != nil || ports[1].Duplex != "" {
		t.Fatal("unknown speed invented")
	}
	page = strings.Replace(page, "105 day - 20 hour - 46 min - 25 sec", "999999999999999999999999999999 day - 0 hour - 0 min - 0 sec", 1)
	id, e := mercuryIdentity(page)
	if e != nil || id.UptimeSeconds != nil {
		t.Fatalf("unknown uptime: %+v %v", id, e)
	}
}
func TestMercuryAllPagesUnknown(t *testing.T) {
	srv, f := mercuryServer(t, false)
	for k := range f.pages {
		f.pages[k] = "unsupported firmware"
	}
	s, e := mercuryAdapter(srv.Client()).Read(context.Background(), mercuryDevice(srv.URL))
	if e == nil {
		t.Fatal("expected error")
	}
	for _, k := range []string{"identity", "ports", "statistics", "vlans", "pvids"} {
		if s.Collections[k].State != "error" {
			t.Fatal(k)
		}
	}
}

func TestMercuryLoginFailureAndUnknownFirmware(t *testing.T) {
	for _, unknown := range []bool{false, true} {
		t.Run(fmt.Sprint(unknown), func(t *testing.T) {
			srv, f := mercuryServer(t, true)
			f.loginFailure = true
			if unknown {
				f.pages["/cryp_new.js"] = `function hex_md5(x){return stolen(x)}`
			}
			calls := 0
			m := &Mercury{Client: srv.Client(), Password: func(string) (string, error) { calls++; return "test-pass", nil }}
			s, e := m.Read(context.Background(), mercuryDevice(srv.URL))
			if e == nil {
				t.Fatal("login unexpectedly worked")
			}
			if calls != 1 {
				t.Fatalf("password attempts %d", calls)
			}
			expected := 1
			if unknown {
				expected = 0
			}
			if f.posts != expected {
				t.Fatalf("login POST count %d want %d", f.posts, expected)
			}
			b, _ := json.Marshal(s)
			if strings.Contains(string(b), "secret upstream") || strings.Contains(string(b), "test-pass") || strings.Contains(e.Error(), "test-pass") {
				t.Fatal("secret leaked")
			}
		})
	}
}
func TestMercuryPasswordResolverErrorIsPrivate(t *testing.T) {
	srv, _ := mercuryServer(t, true)
	m := &Mercury{Client: srv.Client(), Password: func(string) (string, error) { return "", errors.New("very private diagnostic") }}
	s, e := m.Read(context.Background(), mercuryDevice(srv.URL))
	b, _ := json.Marshal(s)
	if e == nil || strings.Contains(string(b), "very private") || strings.Contains(e.Error(), "very private") {
		t.Fatal("private resolver error leaked")
	}
}
func TestMercurySessionIsolation(t *testing.T) {
	srv, f := mercuryServer(t, true)
	m := mercuryAdapter(srv.Client())
	for i := 0; i < 2; i++ {
		d := mercuryDevice(srv.URL)
		d.ID = fmt.Sprint(i)
		if _, e := m.Read(context.Background(), d); e != nil {
			t.Fatal(e)
		}
	}
	if f.posts != 2 {
		t.Fatalf("sessions shared: %d logins", f.posts)
	}
	if len(f.initialCookies) != 4 || f.initialCookies[0] != "" || f.initialCookies[2] != "" {
		t.Fatalf("inherited cookies: %v", f.initialCookies)
	}
}

func TestMercuryLookupFDBQuery(t *testing.T) {
	for _, vlan := range []*int{nil, Ptr(10)} {
		t.Run(fmt.Sprint(vlan), func(t *testing.T) {
			srv, f := mercuryServer(t, true)
			f.pages["/mac_address_search.cgi"] = `<script>var mac_ds={is_qvlan:1,macNum:3,mac_info:[{mac:'02-00-00-00-00-AA',vid:10,port:4},{mac:'0',vid:0,port:0},{mac:'02:00:00:00:00:ab',vid:10,port:5},]};</script>`
			result, e := mercuryAdapter(srv.Client()).LookupFDB(context.Background(), mercuryDevice(srv.URL), FDBQuery{MAC: "0200000000AA", VLAN: vlan})
			if e != nil {
				t.Fatal(e)
			}
			if len(result.Entries) != 1 || result.Entries[0].MAC != "02:00:00:00:00:aa" || result.Entries[0].PortID != "4" || *result.Entries[0].VLAN != 10 || result.Entries[0].Type != "" {
				t.Fatalf("result: %+v", result)
			}
			want := "GET /mac_address_search.cgi?token=2468013579&txt_macAddress_search=02%3A00%3A00%3A00%3A00%3Aaa&txt_vid_search="
			if vlan != nil {
				want += "10"
			}
			if f.requests[len(f.requests)-1] != want {
				t.Fatalf("request %q want %q", f.requests[len(f.requests)-1], want)
			}
		})
	}
}
func TestMercuryLookupEmptyAndInvalidRows(t *testing.T) {
	for _, page := range []string{"", `var mac_ds={macNum:1,mac_info:[{mac:'0',vid:0,port:0}]};`, `var mac_ds={macNum:1,mac_info:[{mac:'02:00:00:00:00:aa',vid:10,port:0}]};`, `var mac_ds={macNum:1,mac_info:[{mac:'02:00:00:00:00:aa',vid:10,port:10}]};`} {
		srv, f := mercuryServer(t, false)
		if page != "" {
			f.pages["/mac_address_search.cgi"] = page
		}
		r, e := mercuryAdapter(srv.Client()).LookupFDB(context.Background(), mercuryDevice(srv.URL), FDBQuery{MAC: "02:00:00:00:00:aa"})
		if e != nil {
			t.Fatal(e)
		}
		if r.Entries == nil || len(r.Entries) != 0 {
			t.Fatal("placeholder fabricated an entry")
		}
	}
}
func TestMercuryInvalidLookupDoesNotRequest(t *testing.T) {
	srv, f := mercuryServer(t, false)
	for _, q := range []FDBQuery{{MAC: "invalid"}, {MAC: "02:00:00:00:00:aa", VLAN: Ptr(0)}, {MAC: "02:00:00:00:00:aa", VLAN: Ptr(4095)}, {MAC: "02:00:00:00:00:aa?token=evil"}} {
		if _, e := mercuryAdapter(srv.Client()).LookupFDB(context.Background(), mercuryDevice(srv.URL), q); !errors.Is(e, ErrInvalid) {
			t.Fatal(e)
		}
	}
	if len(f.requests) != 0 {
		t.Fatal("invalid request reached server")
	}
}
func TestMercuryLookupMalformedCount(t *testing.T) {
	srv, f := mercuryServer(t, false)
	f.pages["/mac_address_search.cgi"] = `var mac_ds={macNum:2,mac_info:[]};`
	if _, e := mercuryAdapter(srv.Client()).LookupFDB(context.Background(), mercuryDevice(srv.URL), FDBQuery{MAC: "02:00:00:00:00:aa"}); e == nil {
		t.Fatal("short array accepted")
	}
}

func TestMercuryHTTPBoundsAndCancellation(t *testing.T) {
	t.Run("body limit", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			io.WriteString(w, strings.Repeat("x", mercuryBodyLimit+1))
		}))
		defer srv.Close()
		s, e := mercuryAdapter(srv.Client()).session(mercuryDevice(srv.URL))
		if e != nil {
			t.Fatal(e)
		}
		if _, e = s.request(context.Background(), "MainRpm.htm", nil); e == nil {
			t.Fatal("large body accepted")
		}
	})
	t.Run("cancellation", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
		defer srv.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
		defer cancel()
		s, _ := mercuryAdapter(srv.Client()).session(mercuryDevice(srv.URL))
		if _, e := s.request(ctx, "MainRpm.htm", nil); !errors.Is(e, context.DeadlineExceeded) {
			t.Fatalf("cancellation: %v", e)
		}
	})
	t.Run("configured timeout bounded", func(t *testing.T) {
		m := &Mercury{Client: &http.Client{Timeout: time.Hour}}
		s, e := m.session(mercuryDevice("http://example.invalid"))
		if e != nil || s.client.Timeout != 10*time.Second {
			t.Fatal("timeout not bounded")
		}
	})
}
func TestMercuryRedirectNeverForwardsCredentials(t *testing.T) {
	hits := 0
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits++ }))
	defer target.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/credential-destination", http.StatusTemporaryRedirect)
	}))
	defer srv.Close()
	s, _ := mercuryAdapter(srv.Client()).session(mercuryDevice(srv.URL))
	_, e := s.request(context.Background(), "logon.cgi", url.Values{"password": {"synthetic-secret"}})
	if e == nil || hits != 0 || strings.Contains(e.Error(), "synthetic-secret") || strings.Contains(e.Error(), target.URL) {
		t.Fatal("unsafe redirect or error leak")
	}
}
func TestMercuryHTTPSectionFailureIsPrivate(t *testing.T) {
	main := mercuryFixture(t, "main.htm")
	vlan := mercuryFixture(t, "vlans.htm")
	pvid := mercuryFixture(t, "pvids.htm")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/MainRpm.htm":
			io.WriteString(w, main)
		case "/Vlan8021QRpm.htm":
			io.WriteString(w, vlan)
		case "/Vlan8021QPvidRpm.htm":
			io.WriteString(w, pvid)
		default:
			http.Error(w, "private upstream body with synthetic-secret", http.StatusServiceUnavailable)
		}
	}))
	defer srv.Close()
	s, e := mercuryAdapter(srv.Client()).Read(context.Background(), mercuryDevice(srv.URL))
	if e != nil || s.Collections["statistics"].State != "error" || s.Collections["ports"].State != "ok" || s.Collections["vlans"].State != "ok" || s.Collections["pvids"].State != "ok" {
		t.Fatalf("partial snapshot: %+v, %v", s.Collections, e)
	}
	b, _ := json.Marshal(s)
	if strings.Contains(string(b), "synthetic-secret") || strings.Contains(string(b), "private upstream") {
		t.Fatal("upstream error body leaked")
	}
}
func TestMercuryDefaultEnvironmentPassword(t *testing.T) {
	srv, _ := mercuryServer(t, true)
	t.Setenv("FIXTURE_PASSWORD", "test-pass")
	m := &Mercury{Client: srv.Client()}
	if _, e := m.Read(context.Background(), mercuryDevice(srv.URL)); e != nil {
		t.Fatal(e)
	}
}

func TestMercuryInvalidAddress(t *testing.T) {
	for _, address := range []string{"file:///etc/passwd", "http://user:secret@example.invalid", "http://example.invalid/path", "http://example.invalid?token=secret", "http://example.invalid#secret"} {
		if _, e := (&Mercury{}).session(mercuryDevice(address)); !errors.Is(e, ErrInvalid) || strings.Contains(e.Error(), "secret") {
			t.Fatalf("address %q: %v", address, e)
		}
	}
}

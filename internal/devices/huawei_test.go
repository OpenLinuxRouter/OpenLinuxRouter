package devices

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
)

func huaweiFixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile("testdata/huawei_" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

const huaweiTestToken = "0123456789abcdef0123456789abcdef"

type huaweiTestDevice struct {
	mu       sync.Mutex
	pages    map[string]string
	requests []string
	logins   int
	password string
	fail     map[string]bool
}

func huaweiServer(t *testing.T) (*httptest.Server, *huaweiTestDevice) {
	t.Helper()
	device := &huaweiTestDevice{
		password: "s3cret",
		fail:     map[string]bool{},
		pages: map[string]string{
			"/html/ssmp/deviceinfo/deviceinfo.asp":          huaweiFixture(t, "deviceinfo.txt"),
			"/html/amp/opticinfo/opticinfo.asp":             huaweiFixture(t, "opticinfo.txt"),
			"/html/amp/ethinfo/ethinfo.asp":                 huaweiFixture(t, "ethinfo.txt"),
			"/html/bbsp/common/wan_list_cache_wan.asp":      huaweiFixture(t, "wan.txt"),
			"/html/bbsp/common/get_wan_list_ipwanstat.asp":  huaweiFixture(t, "wan_ip_stats.txt"),
			"/html/bbsp/common/get_wan_list_pppwanstat.asp": huaweiFixture(t, "wan_ppp_stats.txt"),
			"/html/bbsp/common/wan_list_cache_wanipv6.asp":  huaweiFixture(t, "wan6.txt"),
		},
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		device.mu.Lock()
		defer device.mu.Unlock()
		device.requests = append(device.requests, r.Method+" "+r.URL.Path)
		if device.fail[r.URL.Path] {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		switch r.URL.Path {
		case "/asp/GetRandCount.asp":
			if r.Method != http.MethodPost {
				t.Error("the token must be fetched with POST")
			}
			_, _ = w.Write([]byte("\ufeff" + huaweiTestToken))
			return
		case "/login.cgi":
			device.logins++
			if err := r.ParseForm(); err != nil {
				t.Error(err)
			}
			form := r.PostForm
			if form.Get("UserName") != "user" || form.Get("Language") != "chinese" || form.Get("x.X_HW_Token") != huaweiTestToken {
				t.Errorf("unexpected login form %q", form.Encode())
			}
			if form.Get("PassWord") != "czNjcmV0" {
				_, _ = w.Write([]byte(`<script>var pageName = '/login.asp';</script>`))
				return
			}
			http.SetCookie(w, &http.Cookie{Name: "CookieHttp", Value: "sid=abc123:Language:chinese:id=1", Path: "/"})
			_, _ = w.Write([]byte(`<script>var pageName = 'index.asp';</script>`))
			return
		}
		cookie, err := r.Cookie("CookieHttp")
		if err != nil || !strings.HasPrefix(cookie.Value, "sid=abc123") {
			_, _ = w.Write([]byte(`<script src="safelogin.js"></script><input id="txt_Password"><script>url:'/asp/GetRandCount.asp'</script>`))
			return
		}
		page, ok := device.pages[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(page))
	}))
	t.Cleanup(server.Close)
	return server, device
}

func huaweiDeviceFor(t *testing.T, server *httptest.Server) Device {
	t.Helper()
	return Device{ID: "ont", Adapter: "huawei", Address: server.URL, Password: "s3cret"}
}

func TestHuaweiRead(t *testing.T) {
	server, device := huaweiServer(t)
	adapter := &Huawei{}
	snapshot, err := adapter.Read(context.Background(), huaweiDeviceFor(t, server))
	if err != nil {
		t.Fatal(err)
	}
	if device.logins != 1 {
		t.Fatalf("logged in %d times, want 1", device.logins)
	}
	if got := snapshot.Identity; got.Manufacturer != "Huawei" || got.Model != "V271-20" || got.Serial != "48575443A1B2C3D4" {
		t.Fatalf("identity %#v", got)
	}
	if snapshot.Identity.Firmware != "V5R023C10S328 3A07.C" {
		t.Fatalf("firmware %q", snapshot.Identity.Firmware)
	}
	if len(snapshot.Identity.MACs) != 9 || snapshot.Identity.MACs[0] != "9C:A2:F4:11:22:30" {
		t.Fatalf("macs %#v", snapshot.Identity.MACs)
	}
	if snapshot.Identity.UptimeSeconds == nil || *snapshot.Identity.UptimeSeconds != 9958197 {
		t.Fatalf("uptime %#v", snapshot.Identity.UptimeSeconds)
	}

	ont := snapshot.ONT
	if ont == nil {
		t.Fatal("no ONT section")
	}
	if ont.PON.Mode != "10G EPON" || ont.PON.State != "ONLINE" || ont.PON.Registered == nil || !*ont.PON.Registered {
		t.Fatalf("PON %#v", ont.PON)
	}
	if ont.Customization != "BJUNICOM" {
		t.Fatalf("customization %q", ont.Customization)
	}
	if ont.Health == nil || ont.Health.CPUPercent == nil || *ont.Health.CPUPercent != 14 || *ont.Health.MemoryPercent != 27 || *ont.Health.NATSessions != 1799 {
		t.Fatalf("health %#v", ont.Health)
	}
	optical := ont.Optical
	if optical == nil || optical.RXDBm == nil || *optical.RXDBm != -13.98 || *optical.TXDBm != 4.05 {
		t.Fatalf("optical %#v", optical)
	}
	if optical.RXRangeDBm == nil || optical.RXRangeDBm.Min != -28.5 || optical.RXRangeDBm.Max != -10 {
		t.Fatalf("rx range %#v", optical.RXRangeDBm)
	}
	if optical.TXRangeDBm == nil || optical.TXRangeDBm.Min != 0.6 || optical.TXRangeDBm.Max != 5.6 {
		t.Fatalf("tx range %#v", optical.TXRangeDBm)
	}
	if optical.TemperatureC == nil || *optical.TemperatureC != 56 || *optical.VoltageMV != 3310 || *optical.CurrentMA != 16 {
		t.Fatalf("module readings %#v", optical)
	}
	if optical.ModuleVendor != "HUAWEI" || optical.ModuleSerial != "2437L0000001" || optical.DateCode != "241031" {
		t.Fatalf("module identity %#v", optical)
	}
	if optical.TXWavelengthNM == nil || *optical.TXWavelengthNM != 1310 || *optical.RXWavelengthNM != 1490 {
		t.Fatalf("wavelengths %#v", optical)
	}
	if optical.LOS == nil || *optical.LOS {
		t.Fatalf("los %#v", optical.LOS)
	}

	if len(snapshot.Ports) != 5 || snapshot.Ports[0].ID != "pon" || snapshot.Ports[0].Kind != "fiber" {
		t.Fatalf("ports %#v", snapshot.Ports)
	}
	lan1 := snapshot.Ports[1]
	if lan1.ID != "lan1" || lan1.Kind != "ethernet" || lan1.LinkUp == nil || !*lan1.LinkUp || lan1.Duplex != "full" {
		t.Fatalf("lan1 %#v", lan1)
	}
	if lan1.SpeedMbps == nil || *lan1.SpeedMbps != 2500 {
		t.Fatalf("lan1 speed %#v", lan1.SpeedMbps)
	}
	if lan1.TXBytes == nil || *lan1.TXBytes != 17114983546356 || lan1.RXBytes == nil || *lan1.RXBytes != 29825702368648 {
		t.Fatalf("lan1 bytes %#v %#v", lan1.TXBytes, lan1.RXBytes)
	}
	if lan1.TXPackets == nil || *lan1.TXPackets != 21703552876 || lan1.RXPackets == nil || *lan1.RXPackets != 35075776121 {
		t.Fatalf("lan1 packets %#v %#v", lan1.TXPackets, lan1.RXPackets)
	}
	if lan2 := snapshot.Ports[2]; lan2.LinkUp == nil || *lan2.LinkUp || lan2.SpeedMbps != nil {
		t.Fatalf("lan2 %#v", lan2)
	}

	if len(ont.WAN) != 5 {
		t.Fatalf("wan %#v", ont.WAN)
	}
	internet := wanByName(t, ont.WAN, "2_INTERNET_R_VID_3961")
	if internet.Interface != "PPPoE" || internet.Service != "INTERNET" || internet.Status != "Connected" {
		t.Fatalf("internet %#v", internet)
	}
	if internet.Address != "203.0.113.7" || internet.Gateway != "203.0.113.1" {
		t.Fatalf("internet address %#v", internet)
	}
	if internet.VLAN == nil || *internet.VLAN != 3961 || internet.Priority == nil || *internet.Priority != 0 {
		t.Fatalf("internet vlan %#v", internet)
	}
	if len(internet.DNS) != 2 || internet.DNS[0] != "198.51.100.53" {
		t.Fatalf("internet dns %#v", internet.DNS)
	}
	if internet.UptimeSeconds == nil || *internet.UptimeSeconds != 9958146 {
		t.Fatalf("internet uptime %#v", internet.UptimeSeconds)
	}
	if internet.TXBytes == nil || *internet.TXBytes != 1045140336 || internet.RXBytes == nil || *internet.RXBytes != 3546344634 {
		t.Fatalf("internet counters %#v", internet)
	}
	if len(internet.IPv6) != 2 || internet.IPv6[0] != "2001:db8:1:2:3:4:5:6" || internet.IPv6Prefix != "2001:db8:cafe::/60" {
		t.Fatalf("internet ipv6 %#v %q", internet.IPv6, internet.IPv6Prefix)
	}

	iptv := wanByName(t, ont.WAN, "4_IPTV_R_VID_3964")
	if iptv.Interface != "DHCP" || iptv.Address != "10.99.2.16" || iptv.VLAN == nil || *iptv.VLAN != 3964 {
		t.Fatalf("iptv %#v", iptv)
	}
	if iptv.Priority == nil || *iptv.Priority != 4 {
		t.Fatalf("iptv priority %#v", iptv.Priority)
	}
	if iptv.TXBytes == nil || *iptv.TXBytes != 451078867 {
		t.Fatalf("iptv counters %#v", iptv.TXBytes)
	}

	idle := wanByName(t, ont.WAN, "3_OTHER_R_VID_3962")
	if idle.Status != "Disconnected" || idle.Address != "" {
		t.Fatalf("idle %#v", idle)
	}

	for _, name := range []string{"identity", "optical", "ports", "wan"} {
		if state := snapshot.Collections[name].State; state != "ok" {
			t.Fatalf("collection %s is %q: %s", name, state, snapshot.Collections[name].Error)
		}
	}
	if !snapshot.Capabilities.Supports("optical.read") || !snapshot.Capabilities.Supports("wan.read") {
		t.Fatalf("capabilities %#v", snapshot.Capabilities)
	}
}

func wanByName(t *testing.T, connections []WANConnection, name string) WANConnection {
	t.Helper()
	for _, connection := range connections {
		if connection.Name == name {
			return connection
		}
	}
	t.Fatalf("no WAN connection %q", name)
	return WANConnection{}
}

func TestHuaweiWANCountersPreferTheWidePair(t *testing.T) {
	// Firmware that reports the high/low pair must not fall back to the 32-bit
	// number, which has already wrapped by the time a busy line passes 4 GiB.
	page := `function() { return new Array(new WaninfoStats("InternetGatewayDevice.WANDevice.1.WANConnectionDevice.2.WANPPPConnection.1.Stats","7","9","3","4","0","0","0","0","0","0","2","7","1","9"),null); }`
	connection := WANConnection{Path: "InternetGatewayDevice.WANDevice.1.WANConnectionDevice.2.WANPPPConnection.1"}
	byPath := map[string]*WANConnection{connection.Path: &connection}
	huaweiApplyWANStats(page, byPath)
	if connection.TXBytes == nil || *connection.TXBytes != 2<<32+7 {
		t.Fatalf("sent bytes %#v", connection.TXBytes)
	}
	if connection.RXBytes == nil || *connection.RXBytes != 1<<32+9 {
		t.Fatalf("received bytes %#v", connection.RXBytes)
	}
	if connection.TXPackets == nil || *connection.TXPackets != 3 {
		t.Fatalf("sent packets %#v", connection.TXPackets)
	}
}

func TestHuaweiLoginIsNotRetried(t *testing.T) {
	server, device := huaweiServer(t)
	d := huaweiDeviceFor(t, server)
	d.Password = "wrong"
	_, err := (&Huawei{}).Read(context.Background(), d)
	if !errors.Is(err, errHuaweiAuth) {
		t.Fatalf("got %v, want an authentication error", err)
	}
	if device.logins != 1 {
		t.Fatalf("submitted the form %d times, want 1", device.logins)
	}
}

func TestHuaweiSectionFailureIsLocal(t *testing.T) {
	server, device := huaweiServer(t)
	device.fail["/html/amp/opticinfo/opticinfo.asp"] = true
	snapshot, err := (&Huawei{}).Read(context.Background(), huaweiDeviceFor(t, server))
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Collections["optical"].State != "error" {
		t.Fatalf("optical collection %#v", snapshot.Collections["optical"])
	}
	if snapshot.ONT.Optical != nil {
		t.Fatal("a failed optical page must not leave a reading behind")
	}
	if snapshot.Collections["identity"].State != "ok" || snapshot.ONT.Health == nil {
		t.Fatal("one failed page suppressed the identity section")
	}
	if snapshot.Ports == nil || len(snapshot.Ports) != 5 {
		t.Fatal("one failed page suppressed the port table")
	}
}

func TestHuaweiOpticalRange(t *testing.T) {
	tx, rx, ok := huaweiOpticalRange("0.6-5.6dBm:-28.5--10.0dBm")
	if !ok || tx.Min != 0.6 || tx.Max != 5.6 || rx.Min != -28.5 || rx.Max != -10 {
		t.Fatalf("range %#v %#v", tx, rx)
	}
	if _, _, ok := huaweiOpticalRange("auto"); ok {
		t.Fatal("accepted a page without a range")
	}
}

func TestHuaweiArgumentsRejectsMalformedPages(t *testing.T) {
	if _, err := huaweiObjects(`... new stOpticInfo("a", "b"`, "stOpticInfo"); err == nil {
		t.Fatal("accepted an unterminated call")
	}
	if _, err := huaweiObjects(`new stOpticInfo("a\", "b")`, "stOpticInfo"); err == nil {
		t.Fatal("accepted an unterminated string")
	}
	args, err := huaweiObjects(`new stOpticInfo("a\x2eb","-1.5",null)`, "stOpticInfo")
	if err != nil || len(args) != 1 || len(args[0]) != 3 {
		t.Fatalf("args %#v %v", args, err)
	}
	if value := huaweiField(args[0], 0); value != "a.b" {
		t.Fatalf("escape decoded to %q", value)
	}
	if value := huaweiField(args[0], 1); value != "-1.5" {
		t.Fatalf("bare value decoded to %q", value)
	}
}

func TestHuaweiCountersDoNotWrap(t *testing.T) {
	if got := huaweiCounter("5", "228716396"); got == nil || *got != 21703552876 {
		t.Fatalf("counter %#v", got)
	}
	if got := huaweiCounter("4294967295", "1"); got == nil || *got != 4294967295<<32+1 {
		t.Fatalf("largest counter %#v", got)
	}
	if got := huaweiCounter("4294967296", "0"); got != nil {
		t.Fatal("accepted a high word that would overflow")
	}
	if got := huaweiBytes("0", "1", "4294967296", "0"); got != nil {
		t.Fatal("accepted byte counters that would overflow")
	}
	if got := huaweiPercent("14%"); got == nil || *got != 14 {
		t.Fatalf("percent %#v", got)
	}
	if got := huaweiPercent("101%"); got != nil {
		t.Fatal("accepted an impossible percentage")
	}
}

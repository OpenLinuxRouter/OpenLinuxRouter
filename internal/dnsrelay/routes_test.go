package dnsrelay

import (
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

func TestRoutesSelectDeviceBeforeNetworkAndDirectOverrides(t *testing.T) {
	clash := netip.MustParseAddrPort("172.16.1.137:53")
	r := Routes{
		Networks: []RouteNetwork{{Prefix: netip.MustParsePrefix("172.16.1.0/24"), Exit: "Clash"}},
		Clients: []RouteClient{
			{MAC: "aa:bb:cc:dd:ee:01", Exit: "", Prefixes: []netip.Prefix{netip.MustParsePrefix("172.16.1.0/24")}},
			{MAC: "aa:bb:cc:dd:ee:02", Exit: "Clash", Prefixes: []netip.Prefix{netip.MustParsePrefix("172.16.1.0/24")}},
		},
		Exits: []RouteExit{{Name: "Clash", DNS: clash}},
	}
	cases := []struct {
		ip, mac string
		want    bool
	}{
		{"172.16.1.10", "aa:bb:cc:dd:ee:01", false},
		{"172.16.1.11", "aa:bb:cc:dd:ee:02", true},
		{"172.16.1.12", "", false},
		{"192.168.1.20", "", false},
	}
	for _, tc := range cases {
		got, ok, uncertain := r.Select(netip.MustParseAddr(tc.ip), tc.mac)
		if (tc.ip == "172.16.1.12") != uncertain || ok != tc.want || (ok && got != clash) {
			t.Errorf("%s/%s -> %s %v", tc.ip, tc.mac, got, ok)
		}
	}
}

func TestIdentityFromActiveLeaseAndARP(t *testing.T) {
	dir := t.TempDir()
	leases, arp := filepath.Join(dir, "leases"), filepath.Join(dir, "arp")
	now := time.Unix(1800000000, 0)
	data := "1800001000 aa:bb:cc:dd:ee:01 172.16.1.10 phone *\n" +
		"1799999999 aa:bb:cc:dd:ee:02 172.16.1.11 old *\n"
	if err := os.WriteFile(leases, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(arp, []byte("IP address HW type Flags HW address Mask Device\n"+
		"172.16.1.12 0x1 0x2 aa:bb:cc:dd:ee:03 * ens19\n"+
		"172.16.1.10 0x1 0x2 aa:bb:cc:dd:ee:04 * ens19\n"), 0600); err != nil {
		t.Fatal(err)
	}
	got := collectIdentity(leases, arp, now)
	_, conflict := collectIdentityState(leases, arp, now)
	if !conflict[netip.MustParseAddr("172.16.1.10")] {
		t.Error("conflicting identity was not flagged")
	}
	if got[netip.MustParseAddr("172.16.1.10")] != "" {
		t.Error("conflicting identity was trusted")
	}
	if got[netip.MustParseAddr("172.16.1.11")] != "" {
		t.Error("expired lease was trusted")
	}
	if got[netip.MustParseAddr("172.16.1.12")] != "aa:bb:cc:dd:ee:03" {
		t.Errorf("ARP identity: %v", got)
	}
}

func TestIdentityCacheSeesANewLeaseAfterRefresh(t *testing.T) {
	dir := t.TempDir()
	lease := filepath.Join(dir, "leases")
	cache := identityCache{leases: lease}
	ip := netip.MustParseAddr("172.16.1.50")
	if mac, _ := cache.mac(ip); mac != "" {
		t.Fatalf("unexpected identity %q", mac)
	}
	if err := os.WriteFile(lease, []byte("0 aa:bb:cc:dd:ee:ff 172.16.1.50 phone *\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cache.mu.Lock()
	cache.until = time.Time{}
	cache.mu.Unlock()
	if mac, _ := cache.mac(ip); mac != "aa:bb:cc:dd:ee:ff" {
		t.Fatalf("lease refresh = %q", mac)
	}
}

func TestRelayRoutesPublicQueriesButKeepsLocalAndBlocked(t *testing.T) {
	olr, olrAsked := countingUpstream(t)
	clash, clashAsked := countingUpstream(t)
	cfg := loopbackConfig(t, olr.addr())
	cfg.RoutesFile = filepath.Join(t.TempDir(), "routes.json")
	cfg.LeaseFile = filepath.Join(t.TempDir(), "leases")
	cfg.ARPFile = filepath.Join(t.TempDir(), "arp")
	cfg.QueryLogEntries = 8
	routes := Routes{LocalDomain: "home.arpa", Clients: []RouteClient{{MAC: "aa:bb:cc:dd:ee:01", Exit: "Clash", Prefixes: []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")}}},
		Exits: []RouteExit{{Name: "Clash", DNS: clash.addr()}}}
	data, err := MarshalRoutes(routes)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg.RoutesFile, data, 0600); err != nil {
		t.Fatal(err)
	}
	lease := "0 aa:bb:cc:dd:ee:01 127.0.0.1 phone *\n"
	if err := os.WriteFile(cfg.LeaseFile, []byte(lease), 0600); err != nil {
		t.Fatal(err)
	}
	r, at := startRelay(t, cfg, []Policy{{Name: "block", Block: []string{"bad.example"}}})
	ask(t, at, buildQuery(t, 1, "public.example.", dnsmessage.TypeA))
	conn, err := net.DialTCP("tcp", nil, net.TCPAddrFromAddrPort(at))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := writeTCPMessage(conn, buildQuery(t, 4, "tcp.example.", dnsmessage.TypeA)); err != nil {
		t.Fatal(err)
	}
	if _, err := readTCPMessage(conn); err != nil {
		t.Fatal(err)
	}
	ask(t, at, buildQuery(t, 2, "host.home.arpa.", dnsmessage.TypeA))
	ask(t, at, buildQuery(t, 3, "bad.example.", dnsmessage.TypeA))
	if n := clashAsked(); n != 2 {
		t.Errorf("Clash asked %d times", n)
	}
	if n := olrAsked(); n != 1 {
		t.Errorf("OLR asked %d times", n)
	}
	for i := 0; i < 20 && r.log.Held() < 4; i++ {
		time.Sleep(10 * time.Millisecond)
	}
	if rows := r.log.Snapshot(-1); len(rows) < 4 {
		t.Fatalf("query log lost routed DNS: %+v", rows)
	}
}

func TestUnknownClientInAMixedExitNetworkGetsFailure(t *testing.T) {
	r := Routes{Networks: []RouteNetwork{{Prefix: netip.MustParsePrefix("172.16.1.0/24")}},
		Clients: []RouteClient{{MAC: "aa:bb:cc:dd:ee:ff", Exit: "Clash", Prefixes: []netip.Prefix{netip.MustParsePrefix("172.16.1.0/24")}}},
		Exits:   []RouteExit{{Name: "Clash", DNS: netip.MustParseAddrPort("172.16.1.137:53")}}}
	_, _, uncertain := r.Select(netip.MustParseAddr("172.16.1.50"), "")
	if !uncertain {
		t.Fatal("unidentified client might receive an incompatible answer")
	}
	_, selected, uncertain := r.Select(netip.MustParseAddr("172.16.1.50"), "aa:bb:cc:dd:ee:01")
	if selected || uncertain {
		t.Fatal("a known direct device must use OLR")
	}
}

func TestKnownDirectOverrideBeatsNetworkDNS(t *testing.T) {
	r := Routes{Networks: []RouteNetwork{{Prefix: netip.MustParsePrefix("172.16.1.0/24"), Exit: "Clash"}},
		Clients: []RouteClient{{MAC: "aa:bb:cc:dd:ee:01", Prefixes: []netip.Prefix{netip.MustParsePrefix("172.16.1.0/24")}}},
		Exits:   []RouteExit{{Name: "Clash", DNS: netip.MustParseAddrPort("172.16.1.137:53")}}}
	_, selected, uncertain := r.Select(netip.MustParseAddr("172.16.1.50"), "aa:bb:cc:dd:ee:01")
	if selected || uncertain {
		t.Fatalf("direct device took Clash DNS: selected=%v uncertain=%v", selected, uncertain)
	}
}

func TestNetworkOverrideBeatsBoxDefaultDNS(t *testing.T) {
	r := Routes{Default: "Clash", Networks: []RouteNetwork{{Prefix: netip.MustParsePrefix("172.16.1.0/24")}},
		Exits: []RouteExit{{Name: "Clash", DNS: netip.MustParseAddrPort("172.16.1.137:53")}}}
	_, selected, uncertain := r.Select(netip.MustParseAddr("172.16.1.50"), "")
	if selected || uncertain {
		t.Fatal("network direct override should keep OLR DNS")
	}
}

func TestLocalFailureResponseKeepsTheQuestion(t *testing.T) {
	query := buildQuery(t, 33, "example.com.", dnsmessage.TypeA)
	response, err := SynthesizeFailure(query)
	if err != nil {
		t.Fatal(err)
	}
	obs, err := Observe(response)
	if err != nil || obs.Rcode != "SERVFAIL" || obs.Name != "example.com" {
		t.Fatalf("failure response: %+v %v", obs, err)
	}
}

func TestRoutesReloadChangesUpstreamWithoutRestart(t *testing.T) {
	olr, olrAsked := countingUpstream(t)
	clash, clashAsked := countingUpstream(t)
	cfg := loopbackConfig(t, olr.addr())
	cfg.RoutesFile = filepath.Join(t.TempDir(), "routes.json")
	write := func(exit bool) {
		routes := Routes{LocalDomain: "home.arpa"}
		if exit {
			routes.Default = "Clash"
			routes.Exits = []RouteExit{{Name: "Clash", DNS: clash.addr()}}
		}
		data, err := MarshalRoutes(routes)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(cfg.RoutesFile, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(false)
	r, at := startRelay(t, cfg, nil)
	ask(t, at, buildQuery(t, 1, "before.example.", dnsmessage.TypeA))
	write(true)
	if err := r.Reload(); err != nil {
		t.Fatal(err)
	}
	ask(t, at, buildQuery(t, 2, "after.example.", dnsmessage.TypeA))
	if olrAsked() != 1 || clashAsked() != 1 {
		t.Fatalf("reload did not switch DNS: olr=%d clash=%d", olrAsked(), clashAsked())
	}
}

func TestRoutedQueriesStillRecordTheSelectedUpstream(t *testing.T) {
	olr, _ := countingUpstream(t)
	clash, _ := countingUpstream(t)
	cfg := loopbackConfig(t, olr.addr())
	cfg.QueryLogEntries = 8
	cfg.RoutesFile = filepath.Join(t.TempDir(), "routes.json")
	routes := Routes{Default: "Clash", Exits: []RouteExit{{Name: "Clash", DNS: clash.addr()}}}
	data, _ := MarshalRoutes(routes)
	if err := os.WriteFile(cfg.RoutesFile, data, 0600); err != nil {
		t.Fatal(err)
	}
	r, at := startRelay(t, cfg, nil)
	ask(t, at, buildQuery(t, 1, "example.com.", dnsmessage.TypeA))
	for i := 0; i < 20 && r.log.Held() == 0; i++ {
		time.Sleep(10 * time.Millisecond)
	}
	rows := r.log.Snapshot(-1)
	if len(rows) != 1 || rows[0].Upstream != clash.addr().String() {
		t.Fatalf("upstream not logged: %+v", rows)
	}
}

func TestMalformedRouteReloadKeepsPreviousSelection(t *testing.T) {
	olr, _ := countingUpstream(t)
	clash, clashAsked := countingUpstream(t)
	cfg := loopbackConfig(t, olr.addr())
	cfg.RoutesFile = filepath.Join(t.TempDir(), "routes.json")
	data, _ := MarshalRoutes(Routes{Default: "Clash", Exits: []RouteExit{{Name: "Clash", DNS: clash.addr()}}})
	if err := os.WriteFile(cfg.RoutesFile, data, 0600); err != nil {
		t.Fatal(err)
	}
	r, at := startRelay(t, cfg, nil)
	if err := os.WriteFile(cfg.RoutesFile, []byte("{broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := r.Reload(); err == nil {
		t.Fatal("bad route file was accepted")
	}
	ask(t, at, buildQuery(t, 1, "example.com.", dnsmessage.TypeA))
	if clashAsked() != 1 {
		t.Fatal("malformed reload replaced the last good DNS selection")
	}
}

func TestMissingRoutesFileIsAnIncompleteApply(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.json")
	if _, err := LoadRoutes(path); err == nil {
		t.Fatal("a configured but missing DNS route file was ignored")
	}
	if _, err := LoadRoutes(""); err != nil {
		t.Fatalf("legacy relay with no route path: %v", err)
	}
}

func TestAmbiguousIdentityReturnsServfailBeforeUpstream(t *testing.T) {
	olr, olrAsked := countingUpstream(t)
	clash, clashAsked := countingUpstream(t)
	cfg := loopbackConfig(t, olr.addr())
	cfg.RoutesFile = filepath.Join(t.TempDir(), "routes.json")
	cfg.LeaseFile = filepath.Join(t.TempDir(), "leases")
	cfg.ARPFile = filepath.Join(t.TempDir(), "arp")
	routes := Routes{Clients: []RouteClient{{MAC: "aa:bb:cc:dd:ee:01", Exit: "Clash", Prefixes: []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")}}},
		Exits: []RouteExit{{Name: "Clash", DNS: clash.addr()}}}
	data, _ := MarshalRoutes(routes)
	if err := os.WriteFile(cfg.RoutesFile, data, 0600); err != nil {
		t.Fatal(err)
	}
	_, at := startRelay(t, cfg, nil)
	response := ask(t, at, buildQuery(t, 1, "example.com.", dnsmessage.TypeA))
	obs, err := Observe(response)
	if err != nil || obs.Rcode != "SERVFAIL" {
		t.Fatalf("uncertain DNS: %+v %v", obs, err)
	}
	if olrAsked() != 0 || clashAsked() != 0 {
		t.Fatal("ambiguous query reached an upstream")
	}
}

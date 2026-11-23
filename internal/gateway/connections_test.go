package gateway

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/netip"
	"testing"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// connectionTable is a kernel-format table with the cases the census has to
// tell apart, in the shape the live box prints them: an ordinary outbound TCP
// connection, a UDP one, an unanswered SYN, and a forwarded connection whose
// reply tuple carries the translated inside address. The last four lines are
// the ones it must not read: the box's own conversation, loopback, ICMP, an
// IGMP/unknown entry, and a line with an address the kernel could not have
// printed.
const connectionTable = `ipv4     2 tcp      6 431999 ESTABLISHED src=172.16.1.32 dst=223.73.85.15 sport=56789 dport=443 packets=10 bytes=1000 src=223.73.85.15 dst=192.168.1.2 sport=443 dport=56789 packets=4 bytes=400 [ASSURED] mark=0 zone=0 use=2
ipv4     2 udp      17 29 src=172.16.1.32 dst=8.8.8.8 sport=5353 dport=53 packets=2 bytes=200 src=8.8.8.8 dst=172.16.1.32 sport=53 dport=5353 packets=2 bytes=200 [ASSURED] mark=0 zone=0 use=2
ipv4     2 tcp      6 28 SYN_SENT src=172.16.1.32 dst=223.73.67.182 sport=41421 dport=63219 packets=1 bytes=60 [UNREPLIED] src=223.73.67.182 dst=192.168.1.2 sport=63219 dport=41421 packets=0 bytes=0 mark=0 zone=0 use=2
ipv4     2 tcp      6 345456 ESTABLISHED src=223.73.85.15 dst=192.168.1.2 sport=13913 dport=56789 packets=2 bytes=112 src=172.16.1.32 dst=223.73.85.15 sport=56789 dport=13913 packets=1 bytes=60 [ASSURED] mark=0 zone=0 use=2
ipv4     2 udp      17 20 src=172.16.1.50 dst=9.9.9.9 sport=1000 dport=9999 packets=1 bytes=50 src=9.9.9.9 dst=172.16.1.50 sport=9999 dport=1000 packets=1 bytes=50 [ASSURED] mark=0 zone=0 use=2
ipv4     2 tcp      6 100 ESTABLISHED src=192.168.1.2 dst=203.0.113.5 sport=40000 dport=443 packets=3 bytes=900 src=203.0.113.5 dst=192.168.1.2 sport=443 dport=40000 packets=3 bytes=900 [ASSURED] mark=0 zone=0 use=2
ipv4     2 udp      17 0 src=127.0.0.1 dst=127.0.0.1 sport=41665 dport=5353 packets=1 bytes=70 src=127.0.0.1 dst=127.0.0.1 sport=5353 dport=41665 packets=1 bytes=131 mark=0 zone=0 use=2
ipv4     2 icmp     1 26 src=192.168.1.1 dst=192.168.1.2 type=8 code=0 id=1 src=192.168.1.2 dst=192.168.1.1 type=0 code=0 id=1 mark=0 zone=0 use=2
ipv4     2 unknown  2 593 src=172.16.1.32 dst=224.0.0.1 [UNREPLIED] src=224.0.0.1 dst=172.16.1.32 mark=0 zone=0 use=2
ipv4     2 tcp      6 12 ESTABLISHED src=not-an-address dst=192.168.1.2 sport=1 dport=2 packets=1 bytes=1 src=192.168.1.2 dst=not-an-address sport=2 dport=1 packets=1 bytes=1 mark=0 use=1
`

// connectionNext is the same table ten seconds later. The outbound connection
// moved 2500 bytes out and 50000 in, the forwarded one 12500 in, the idle UDP
// and the unanswered SYN nothing at all, another client's flow is gone, and a
// new connection has appeared with its whole lifetime inside this window.
const connectionNext = `ipv4     2 tcp      6 431999 ESTABLISHED src=172.16.1.32 dst=223.73.85.15 sport=56789 dport=443 packets=10 bytes=3500 src=223.73.85.15 dst=192.168.1.2 sport=443 dport=56789 packets=4 bytes=50400 [ASSURED] mark=0 zone=0 use=2
ipv4     2 udp      17 29 src=172.16.1.32 dst=8.8.8.8 sport=5353 dport=53 packets=2 bytes=200 src=8.8.8.8 dst=172.16.1.32 sport=53 dport=5353 packets=2 bytes=200 [ASSURED] mark=0 zone=0 use=2
ipv4     2 tcp      6 28 SYN_SENT src=172.16.1.32 dst=223.73.67.182 sport=41421 dport=63219 packets=1 bytes=60 [UNREPLIED] src=223.73.67.182 dst=192.168.1.2 sport=63219 dport=41421 packets=0 bytes=0 mark=0 zone=0 use=2
ipv4     2 tcp      6 345456 ESTABLISHED src=223.73.85.15 dst=192.168.1.2 sport=13913 dport=56789 packets=2 bytes=12612 src=172.16.1.32 dst=223.73.85.15 sport=56789 dport=13913 packets=1 bytes=60 [ASSURED] mark=0 zone=0 use=2
ipv4     2 tcp      6 500 ESTABLISHED src=172.16.1.40 dst=1.1.1.1 sport=50000 dport=443 packets=4 bytes=1000 src=1.1.1.1 dst=172.16.1.40 sport=443 dport=50000 packets=9 bytes=8000 [ASSURED] mark=0 zone=0 use=2
`

func mustAddr(raw string) netip.Addr { return netip.MustParseAddr(raw) }

// connectionJoin is the presence answer the two walks are attributed with: the
// NAS on 172.16.1.32 and the laptop on 172.16.1.40, each with an owner and a
// name. 172.16.1.50 was a phone that has since gone quiet.
func connectionJoin() ClientJoin {
	mac := func(suffix string) string { return "aa:bb:cc:dd:ee:" + suffix }
	return ClientJoin{
		Owners: map[netip.Addr]string{
			mustAddr("172.16.1.32"): "90:09:d0:2b:2d:75",
			mustAddr("172.16.1.40"): mac("40"),
			mustAddr("172.16.1.50"): mac("50"),
		},
		Names: map[string]string{
			"90:09:d0:2b:2d:75": "DS1522plus",
			mac("40"):           "Laptop",
			mac("50"):           "Phone",
		},
	}
}

// connectionLocal is the set of addresses the box holds in these fixtures: its
// LAN address, its NAT address on the uplink, and loopback.
func connectionLocal() map[netip.Addr]bool {
	return map[netip.Addr]bool{
		mustAddr("172.16.1.1"):  true,
		mustAddr("192.168.1.2"): true,
		mustAddr("127.0.0.1"):   true,
	}
}

// walkConnections hands the sampler the given tables in order, so a test can
// assert on the difference between two real samples rather than on a mocked
// one.
func walkConnections(walks ...string) *Connections {
	c := NewConnections()
	next := 0
	c.read = func() (conntrackSample, error) {
		table := walks[len(walks)-1]
		if next < len(walks) {
			table = walks[next]
			next++
		}
		return conntrackSample{entries: parseConntrack([]byte(table)), local: connectionLocal()}, nil
	}
	return c
}

// TwoSamples is the fixture most tests want: everything above, after the
// second walk.
func twoSamples(t *testing.T) *Connections {
	t.Helper()
	c := walkConnections(connectionTable, connectionNext)
	start := capacityTime()
	c.sample(start, connectionJoin(), nil)
	c.sample(start.Add(connectionWindow), connectionJoin(), nil)
	return c
}

// The parser reads the directions as the kernel prints them. The outbound
// connection's original tuple runs from the client out; the forwarded one's
// reply tuple carries the translated inside address because its destination
// was the box's public one.
func TestParseConntrackTellsTheDirectionsApart(t *testing.T) {
	// Seven, not five: the parser reads every TCP and UDP entry. The box's own
	// conversation and the loopback one are dropped by classification, which is
	// where "not a client's" belongs — a parser that decided it would be a
	// second copy of that rule.
	entries := parseConntrack([]byte(connectionTable))
	if len(entries) != 7 {
		t.Fatalf("parsed %d entries, want the seven TCP/UDP connections: %+v", len(entries), entries)
	}

	outbound := entries[0]
	if outbound.proto != "tcp" || outbound.state != "ESTABLISHED" || outbound.unreplied {
		t.Errorf("outbound = %+v", outbound)
	}
	if got := outbound.original.src.String(); got != "172.16.1.32" {
		t.Errorf("original src = %s, want the client", got)
	}
	if got := outbound.original.dst.String(); got != "223.73.85.15" {
		t.Errorf("original dst = %s, want the far end", got)
	}
	if outbound.original.sport != 56789 || outbound.original.dport != 443 {
		t.Errorf("original ports = %d/%d, want 56789/443", outbound.original.sport, outbound.original.dport)
	}
	if outbound.original.bytes != 1000 || outbound.reply.bytes != 400 {
		t.Errorf("counters = %d/%d, want 1000/400", outbound.original.bytes, outbound.reply.bytes)
	}

	udp := entries[1]
	if udp.proto != "udp" || udp.state != "" || udp.original.bytes != 200 {
		t.Errorf("udp = %+v; want no state and its own counters", udp)
	}

	unreplied := entries[2]
	if !unreplied.unreplied || unreplied.state != connectionSynSent {
		t.Errorf("unreplied = %+v", unreplied)
	}
	if unreplied.reply.bytes != 0 {
		t.Errorf("an unanswered connection moved %d bytes toward the client", unreplied.reply.bytes)
	}

	forwarded := entries[3]
	if got := forwarded.original.src.String(); got != "223.73.85.15" {
		t.Errorf("forwarded original src = %s, want the remote opener", got)
	}
	if got := forwarded.reply.src.String(); got != "172.16.1.32" {
		t.Errorf("forwarded reply src = %s, want the translated inside address", got)
	}

	// IPv6 prints its addresses fully expanded; the parser may not assume a
	// compressed form.
	ipv6 := parseConntrack([]byte(
		"ipv6     10 tcp      6 431986 ESTABLISHED src=2001:0470:000a:0652:0000:0000:0000:0002 " +
			"dst=2606:4700:0020:0000:0000:0000:ac43:4bac sport=51220 dport=443 packets=24 bytes=3638 " +
			"src=2606:4700:0020:0000:0000:0000:ac43:4bac dst=2001:0470:000a:0652:0000:0000:0000:0002 " +
			"sport=443 dport=51220 packets=23 bytes=7934 [ASSURED] mark=0 zone=0 use=2\n"))
	if len(ipv6) != 1 || !ipv6[0].original.src.Is6() || ipv6[0].reply.bytes != 7934 {
		t.Fatalf("ipv6 entry = %+v", ipv6)
	}
}

// A line the parser cannot read is dropped rather than guessed at: the census
// may be shorter than the table, but it may never contain a connection or a
// byte count the kernel did not print.
func TestParseConntrackSkipsUnreadableLines(t *testing.T) {
	for _, table := range []string{
		"ipv4 2 tcp 6\n",
		"ipv4 2 tcp 6 12 ESTABLISHED src=172.16.1.32 dst=8.8.8.8 sport=1 dport=2 bytes=4\n",
		"ipv4 2 tcp 6 12 ESTABLISHED src=172.16.1.32 dst=8.8.8.8 sport=x dport=2 bytes=4 " +
			"src=8.8.8.8 dst=172.16.1.32 sport=2 dport=x bytes=4\n",
		"ipv4 2 tcp 6 12 ESTABLISHED src=172.16.1.32 dst=8.8.8.8 sport=1 dport=2 bytes=4 " +
			"src=8.8.8.8 dst=172.16.1.32 sport=2 dport=1 bytes=notanumber\n",
	} {
		if got := parseConntrack([]byte(table)); len(got) != 0 {
			t.Errorf("parsed %+v from %q", got, table)
		}
	}

	// A good line beside a bad one still lands.
	good := "ipv4 2 udp 17 9 src=172.16.1.32 dst=8.8.8.8 sport=1 dport=2 bytes=4 " +
		"src=8.8.8.8 dst=172.16.1.32 sport=2 dport=1 bytes=4\n"
	if got := parseConntrack([]byte(good + "garbage\n")); len(got) != 1 {
		t.Fatalf("parsed %d entries beside a malformed line, want 1", len(got))
	}
}

// The percentiles are nearest-rank, which is what makes them checkable by hand:
// the p-th is the ceil(p/100·n)th smallest, and no reading is ever invented
// between two the box took.
func TestPercentileIsNearestRank(t *testing.T) {
	for _, tc := range []struct {
		name   string
		values []int64
		pct    int
		want   int64
	}{
		{"empty", nil, 90, 0},
		{"one is every percentile", []int64{7}, 50, 7},
		{"four at the median takes the second", []int64{1, 2, 3, 4}, 50, 2},
		{"a hundred at the median", series(1, 100), 50, 50},
		{"a hundred at the ninetieth", series(1, 100), 90, 90},
		{"a hundred at the ninety-ninth", series(1, 100), 99, 99},
		{"the top one percent is left out", append(series(1, 100), 1000), 99, 100},
		{"a thousand at the median", series(1, 1000), 50, 500},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := percentile(tc.values, tc.pct); got != tc.want {
				t.Errorf("percentile(%v, %d) = %d, want %d", tc.values, tc.pct, got, tc.want)
			}
		})
	}
}

// The census orients each connection so that rx is toward the client and tx
// away from it, whichever direction the kernel opened it in. The forwarded
// connection's 12500 bytes are download, not upload.
func TestConnectionsOrientRatesTowardTheClient(t *testing.T) {
	c := twoSamples(t)
	sample := c.lastSample()
	if sample.state != connectionReady || sample.window != connectionWindow {
		t.Fatalf("sample = %+v", sample)
	}

	nas := connectionClientByIP(t, c.view(), "172.16.1.32")
	if nas.MAC != "90:09:d0:2b:2d:75" || nas.Name != "DS1522plus" {
		t.Errorf("client identity = %q/%q", nas.MAC, nas.Name)
	}
	if nas.Connections != 4 || nas.TCP != 3 || nas.UDP != 1 {
		t.Errorf("counts = %d connections, %d tcp, %d udp; want 4/3/1", nas.Connections, nas.TCP, nas.UDP)
	}
	if nas.HalfOpen != 1 {
		t.Errorf("half_open = %d, want the one unanswered SYN", nas.HalfOpen)
	}
	// 50000 bytes of download plus 12500 of forwarded download over ten
	// seconds; 2500 bytes of upload.
	if nas.RXBps != 50000 || nas.TXBps != 2000 {
		t.Errorf("rates = rx %d tx %d, want 50000/2000", nas.RXBps, nas.TXBps)
	}

	// The per-connection distribution is over all four live connections,
	// including the two that moved nothing: an idle connection is part of the
	// answer, so the median is the idle rate rather than the busy one.
	if nas.PerConnection.P50Bps != 0 {
		t.Errorf("p50 = %d, want 0 with two of four connections idle", nas.PerConnection.P50Bps)
	}
	if nas.PerConnection.P90Bps != 40000 || nas.PerConnection.P99Bps != 40000 {
		t.Errorf("p90/p99 = %d/%d, want 40000/40000", nas.PerConnection.P90Bps, nas.PerConnection.P99Bps)
	}
}

// A flow that appeared between two walks was born inside the window, so its
// counters are this window's and not a lifetime to be differenced away.
func TestConnectionsBillANewFlowToItsFirstWindow(t *testing.T) {
	laptop := connectionClientByIP(t, twoSamples(t).view(), "172.16.1.40")
	if laptop.Connections != 1 || laptop.RXBps != 6400 || laptop.TXBps != 800 {
		t.Errorf("new flow = %+v, want 8000 bytes in and 1000 out over ten seconds", laptop)
	}
	if laptop.Name != "Laptop" {
		t.Errorf("name = %q", laptop.Name)
	}
}

// A connection that is gone is not in the sample: the census is every flow the
// table holds now, not a retained history of everything it ever held.
func TestConnectionsForgetAFlowThatEnded(t *testing.T) {
	for _, client := range twoSamples(t).view().Clients {
		if client.IP == "172.16.1.50" {
			t.Fatalf("a closed flow's client is still listed: %+v", client)
		}
	}
}

// Rates need two walks. The first says the counts and says it is warming rather
// than presenting a table of zero rates as a measurement.
func TestConnectionsWarmBeforeTheyHaveAWindow(t *testing.T) {
	c := walkConnections(connectionTable)
	c.sample(capacityTime(), connectionJoin(), nil)

	view := c.view()
	if view.State != connectionWarming || view.WindowSeconds != int(connectionWindow/time.Second) {
		t.Fatalf("state = %q window = %d, want warming over the nominal window", view.State, view.WindowSeconds)
	}
	if view.AsOf != capacityTime() {
		t.Errorf("as_of = %v, want the sample instant", view.AsOf)
	}
	nas := connectionClientByIP(t, view, "172.16.1.32")
	if nas.Connections != 4 || nas.HalfOpen != 1 {
		t.Errorf("counts should be real while warming: %+v", nas)
	}
	if nas.RXBps != 0 || nas.TXBps != 0 || nas.PerConnection.P99Bps != 0 {
		t.Errorf("rates before a second walk = %+v", nas)
	}
}

// A client with no connections is absent, not a zero row: the census is what
// the table holds, and it must not look like every client on the network is
// present but idle.
func TestConnectionsOmitClientsWithNoConnections(t *testing.T) {
	c := walkConnections("", "")
	c.sample(capacityTime(), connectionJoin(), nil)
	c.sample(capacityTime().Add(connectionWindow), connectionJoin(), nil)

	view := c.view()
	if len(view.Clients) != 0 {
		t.Fatalf("an empty table produced %+v", view.Clients)
	}
	if view.Clients == nil {
		t.Error("clients must be an empty list, not null")
	}
	if flows := c.flows(connectionFlowLimit); len(flows.Flows) != 0 || flows.Flows == nil {
		t.Errorf("flows = %+v, want an empty list", flows.Flows)
	}
}

// A table that could not be read is said out loud. An empty census would claim
// no client has a connection, which is the one thing an unreadable file does
// not establish.
func TestConnectionsReportAnUnreadableTable(t *testing.T) {
	c := NewConnections()
	c.read = func() (conntrackSample, error) {
		return conntrackSample{}, errors.New("open /proc/net/nf_conntrack: permission denied")
	}
	c.sample(capacityTime(), connectionJoin(), nil)

	view := c.view()
	if view.State != connectionUnavailable {
		t.Errorf("state = %q, want unavailable", view.State)
	}
	if view.Error == "" {
		t.Error("an unreadable table must carry the reason it could not be read")
	}
	if len(view.Clients) != 0 {
		t.Errorf("clients = %+v, want none", view.Clients)
	}
	if flows := c.flows(connectionFlowLimit); flows.State != connectionUnavailable || len(flows.Flows) != 0 {
		t.Errorf("flows = %+v", flows)
	}
}

// A kernel that is not counting and olr with statistics off are both "no
// sample", and both say which switch to look at rather than publishing zeroes.
func TestConnectionsReportWhyTheyAreNotSampling(t *testing.T) {
	for _, tc := range []struct {
		name   string
		setup  func(*Connections)
		reason error
	}{
		{"kernel not counting", func(c *Connections) {
			c.read = func() (conntrackSample, error) { return conntrackSample{}, errConntrackNotCounting }
		}, nil},
		{"statistics off", func(*Connections) {}, errConnectionStatsOff},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := NewConnections()
			tc.setup(c)
			reason := tc.reason
			if reason == nil {
				reason = errConntrackNotCounting
			}
			c.sample(capacityTime(), connectionJoin(), reason)
			view := c.view()
			if view.State != connectionDisabled || view.Error == "" {
				t.Fatalf("view = %+v, want disabled with the reason", view)
			}
		})
	}

	// The sysctl error takes the same path when the reader reports it itself.
	c := NewConnections()
	c.read = func() (conntrackSample, error) { return conntrackSample{}, errConntrackNotCounting }
	c.sample(capacityTime(), connectionJoin(), nil)
	if got := c.view().State; got != connectionDisabled {
		t.Errorf("state = %q, want disabled for an un-counting kernel", got)
	}
}

// The wire shape is the contract the page and the tool are written against, so
// the names are asserted rather than left to a struct rename.
func TestConnectionsAndFlowsJSONShape(t *testing.T) {
	c := twoSamples(t)

	encoded, err := json.Marshal(c.view())
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(encoded, &got); err != nil {
		t.Fatal(err)
	}
	if _, ok := got["as_of"].(string); !ok {
		t.Fatalf("as_of is not a timestamp: %s", encoded)
	}
	if got["window_seconds"] != float64(10) || got["state"] != connectionReady {
		t.Fatalf("top-level fields: %s", encoded)
	}
	clients, ok := got["clients"].([]any)
	if !ok || len(clients) != 2 {
		t.Fatalf("clients: %s", encoded)
	}
	client, ok := clients[0].(map[string]any)
	if !ok {
		t.Fatalf("client is not an object: %s", encoded)
	}
	for _, field := range []string{
		"ip", "mac", "name", "connections", "tcp", "udp", "half_open",
		"rx_bps", "tx_bps", "per_connection",
	} {
		if _, ok := client[field]; !ok {
			t.Errorf("client.%s is missing: %s", field, encoded)
		}
	}
	per, ok := client["per_connection"].(map[string]any)
	if !ok {
		t.Fatalf("per_connection is not an object: %s", encoded)
	}
	for _, field := range []string{"p50_bps", "p90_bps", "p99_bps"} {
		if _, ok := per[field]; !ok {
			t.Errorf("per_connection.%s is missing: %s", field, encoded)
		}
	}

	encoded, err = json.Marshal(c.flows(connectionFlowLimit))
	if err != nil {
		t.Fatal(err)
	}
	got = map[string]any{}
	if err := json.Unmarshal(encoded, &got); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"as_of", "window_seconds", "state", "flows"} {
		if _, ok := got[field]; !ok {
			t.Errorf("flows response is missing %s: %s", field, encoded)
		}
	}
	flows, ok := got["flows"].([]any)
	if !ok || len(flows) != 5 {
		t.Fatalf("flows: %s", encoded)
	}
	flow, ok := flows[0].(map[string]any)
	if !ok {
		t.Fatalf("flow is not an object: %s", encoded)
	}
	for _, field := range []string{
		"proto", "src", "dst", "sport", "dport", "state", "rx_bps", "tx_bps", "client",
	} {
		if _, ok := flow[field]; !ok {
			t.Errorf("flow.%s is missing: %s", field, encoded)
		}
	}
	if flow["client"] != "DS1522plus" {
		t.Errorf("flow client = %v, want the inside address's name", flow["client"])
	}
	// Nothing here may claim a total the sample did not take: the response is
	// the head of the list, and a count of the tail would be invented from it.
	for _, forbidden := range []string{"total", "count", "other", "truncated"} {
		if _, ok := got[forbidden]; ok {
			t.Errorf("flows response invented %q: %s", forbidden, encoded)
		}
	}
}

// The routes are the whole surface: the page and the MCP tool both read these
// bodies, and the tool is derived from this table rather than written twice.
func TestConnectionsRoutesAreReadOnlyTools(t *testing.T) {
	want := map[string]string{
		"/connections": "show connections",
		"/flows":       "show flows",
	}
	seen := map[string]bool{}
	for _, route := range (HTTP{}).Routes() {
		tool, ok := want[route.Path]
		if !ok {
			continue
		}
		seen[route.Path] = true
		if route.Method != http.MethodGet || route.Mutating {
			t.Errorf("%s is %s mutating=%v, want a GET read", route.Path, route.Method, route.Mutating)
		}
		if route.Tool != tool {
			t.Errorf("%s tool = %q, want %q", route.Path, route.Tool, tool)
		}
		if route.Summary == "" {
			t.Errorf("%s has no summary, which is the text an agent decides on", route.Path)
		}
		if route.Handler == nil {
			t.Errorf("%s has no handler", route.Path)
		}
		if route.Path == "/flows" {
			// The MCP tool's arguments are this table; a limit that is not
			// declared here is a limit an agent cannot pass.
			if len(route.Query) != 1 || route.Query[0].Name != "limit" || route.Query[0].Type != "integer" {
				t.Errorf("/flows query = %+v, want one integer limit", route.Query)
			}
		}
	}
	for path := range want {
		if !seen[path] {
			t.Errorf("gateway does not serve %s", path)
		}
	}
}

func TestGetConnectionsServesTheLastSample(t *testing.T) {
	a := Applier{Store: newTestStore(t), Uplink: StaticUplink("ens18")}
	h := HTTP{Applier: a, Connections: twoSamples(t), Lock: core.NewLock(), Events: core.NewEvents()}.Handler()

	w := do(t, h, http.MethodGet, "/connections", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /connections status = %d: %s", w.Code, w.Body)
	}
	view := decode[connectionsView](t, w)
	if view.State != connectionReady || len(view.Clients) != 2 {
		t.Fatalf("view = %+v", view)
	}
	if view.Clients[0].IP != "172.16.1.32" {
		t.Errorf("clients are not ordered by rate: %+v", view.Clients)
	}

	if w := do(t, (HTTP{}).Handler(), http.MethodGet, "/connections", nil); w.Code != http.StatusServiceUnavailable {
		t.Errorf("missing sampler: status = %d, want 503", w.Code)
	}
}

func TestGetFlowsLimitsAndCaps(t *testing.T) {
	a := Applier{Store: newTestStore(t), Uplink: StaticUplink("ens18")}
	h := HTTP{Applier: a, Connections: twoSamples(t), Lock: core.NewLock(), Events: core.NewEvents()}.Handler()

	w := do(t, h, http.MethodGet, "/flows", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /flows status = %d: %s", w.Code, w.Body)
	}
	view := decode[flowsView](t, w)
	if len(view.Flows) != 5 {
		t.Fatalf("default limit returned %d flows, want all five in the sample", len(view.Flows))
	}
	if view.Flows[0].TXBps != 2000 || view.Flows[0].RXBps != 40000 {
		t.Errorf("first flow = %+v, want the busiest one first", view.Flows[0])
	}
	// The forwarded connection arrives normalised to the client's end, so src
	// is the inside address for it too.
	if view.Flows[1].Src != "172.16.1.32" || view.Flows[1].Dport != 13913 || view.Flows[1].RXBps != 10000 {
		t.Errorf("forwarded flow = %+v", view.Flows[1])
	}

	view = decode[flowsView](t, do(t, h, http.MethodGet, "/flows?limit=2", nil))
	if len(view.Flows) != 2 {
		t.Errorf("limit=2 returned %d flows", len(view.Flows))
	}
	view = decode[flowsView](t, do(t, h, http.MethodGet, "/flows?limit=1000", nil))
	if len(view.Flows) != 5 {
		t.Errorf("a limit above the ceiling returned %d flows, want the whole sample", len(view.Flows))
	}

	for _, query := range []string{"?limit=0", "?limit=-1", "?limit=two"} {
		if w := do(t, h, http.MethodGet, "/flows"+query, nil); w.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", query, w.Code)
		}
	}
	if w := do(t, (HTTP{}).Handler(), http.MethodGet, "/flows", nil); w.Code != http.StatusServiceUnavailable {
		t.Errorf("missing sampler: status = %d, want 503", w.Code)
	}
}

// connectionClientByIP is one row of a census, or a failure naming what was
// there instead.
func connectionClientByIP(t *testing.T, view connectionsView, ip string) connectionClientView {
	t.Helper()
	for _, client := range view.Clients {
		if client.IP == ip {
			return client
		}
	}
	t.Fatalf("no client row for %s in %+v", ip, view.Clients)
	return connectionClientView{}
}

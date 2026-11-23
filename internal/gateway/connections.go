package gateway

import (
	"bytes"
	"errors"
	"math"
	"net/netip"
	"sort"
	"sync"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// connectionWindow is the census's window, and it is the activity sampler's
// cadence rather than a second timer: the conntrack walk is folded into the
// same ten-second pass that fills the accounting history and the capacity
// baseline, so a box watching all three pays for one tick and one file read.
const connectionWindow = capacityInterval

// connectionFlowLimit and connectionFlowMax are what GET /flows returns by
// default and the most it will return. The cap exists because a page shows a
// table, not a table dump: a torrent client can hold thousands of connections,
// and the answer to "why is this 10 MB/s" is the head of the sorted list.
const (
	connectionFlowLimit = 20
	connectionFlowMax   = 200
)

// The census is a sample, so it says what it is: warming before a second walk
// exists to difference against, ready once rates are real, disabled when olr's
// statistics switch is off, and unavailable when the table could not be read.
const (
	connectionWarming     = "warming"
	connectionReady       = "ready"
	connectionDisabled    = "disabled"
	connectionUnavailable = "unavailable"
)

// connectionSynSent is the one TCP state that means the same thing as the
// kernel's [UNREPLIED] flag in a census that counts what has not answered.
const connectionSynSent = "SYN_SENT"

// errConntrackNotCounting is the table being readable while the kernel is not
// counting per connection. It is a state rather than a failure — the file is
// fine — but every rate in it would be a zero nobody measured, so the sampler
// reports the switch instead of the census.
var errConntrackNotCounting = errors.New(
	"the kernel is not counting packets and bytes per connection (net.netfilter.nf_conntrack_acct is off)")

// errConnectionStatsOff is olr's own switch rather than the kernel's. It is a
// separate error from the one above because an operator turns one of them back
// on in the gateway configuration and the other in a sysctl.
var errConnectionStatsOff = errors.New(
	"statistics are off, so the connection census is not sampled")

// conntrackSample is one walk of the kernel's connection table: the connections
// it holds and the addresses that belong to this box itself.
//
// The local set travels with the entries because classification needs both and
// they must come from the same moment. A flow whose source is an address this
// box gained or lost between two reads would otherwise be attributed to a
// client that never carried it.
type conntrackSample struct {
	entries []conntrackEntry
	local   map[netip.Addr]bool
}

// Connections is the live connection census: which client holds which
// connections, and how its throughput divides across them.
//
// It answers the question a performance page asks after capacity has said "the
// uplink is doing 10 MB/s": which client is responsible, and how evenly that
// client's rate is spread over its connections. Deliberately observation only —
// nothing here routes, marks or counts, it differences the counters the kernel
// already keeps.
//
// Sampled, never served live. /proc/net/nf_conntrack is thousands of lines on a
// busy box, and reading it inside a request would put the table's size on the
// handler's latency and let two screens take two different snapshots of one
// moment. The ten-second pass takes one sample and the routes serve the last,
// which is why every answer carries as_of.
type Connections struct {
	mu sync.Mutex

	// read is the platform's conntrack reader. A field rather than a direct
	// call so that a test can hand the sampler a table without /proc.
	read func() (conntrackSample, error)

	// previous is the last walk's per-connection counters, and last is when it
	// was taken. A key that is missing here was created inside the window, and
	// one that fell back is a reused 5-tuple; both are treated as a new
	// connection rather than as an underflow.
	previous map[connectionKey]connectionCounters
	last     time.Time

	// snapshot is the whole answer, rebuilt per sample so a handler does no
	// arithmetic and never blocks on a walk.
	snapshot connectionSnapshot
}

type connectionKey struct {
	proto        string
	src, dst     netip.Addr
	sport, dport uint16
}

type connectionCounters struct {
	original, reply uint64
}

// connectionSnapshot is one sample reduced to what both routes serve. The flow
// list is retained whole and cut per request, because the limit belongs to the
// caller and the order is the sample's.
type connectionSnapshot struct {
	asOf    time.Time
	window  time.Duration
	state   string
	reason  string
	clients []connectionClientView
	flows   []connectionFlow
}

// connectionClient is one client while the sample is being built. Its rates are
// kept unsorted until the percentiles are asked for, once per sample.
type connectionClient struct {
	addr        netip.Addr
	mac, name   string
	connections int
	tcp, udp    int
	halfOpen    int
	rx, tx      int64
	rates       []int64
}

// connectionFlow is one live connection, already turned so that src is the
// client's end. The client's name is resolved when the sample is built, not
// when it is served: the join is the one that was current at as_of.
type connectionFlow struct {
	proto        string
	src, dst     netip.Addr
	sport, dport uint16
	state        string
	rx, tx       int64
	client       string
}

// connectionsView is GET /api/gateway/connections as the API and MCP publish
// it.
type connectionsView struct {
	AsOf          time.Time `json:"as_of"`
	WindowSeconds int       `json:"window_seconds"`

	// State and Error say whether the rates below are a measurement. An
	// unreadable table is reported here rather than as an empty census: "no
	// client has a connection" and "the table could not be read" are
	// different answers and only one of them is true.
	State string `json:"state"`
	Error string `json:"error,omitempty"`

	Clients []connectionClientView `json:"clients"`
}

type connectionClientView struct {
	IP   string `json:"ip"`
	MAC  string `json:"mac,omitempty"`
	Name string `json:"name,omitempty"`

	// Connections is tcp + udp: only protocols with ports at both ends are
	// connections here (see parseConntrack), so the three always agree.
	Connections int `json:"connections"`
	TCP         int `json:"tcp"`
	UDP         int `json:"udp"`

	// HalfOpen counts connections the far end has not answered, which is what
	// separates a client that is being refused from one that is simply busy.
	HalfOpen int `json:"half_open"`

	// RXBps is toward the client and TXBps away from it, in bits per second,
	// matching /capacity's units. Bytes are what the accounting sets count;
	// bits are what a link is sold in.
	RXBps int64 `json:"rx_bps"`
	TXBps int64 `json:"tx_bps"`

	PerConnection connectionPercentiles `json:"per_connection"`
}

// connectionPercentiles is the distribution of one client's per-connection
// receive rates in the window. Idle connections are part of it: "how many
// connections are doing nothing" is half the answer to why a client's total
// does not match the sum being moved.
type connectionPercentiles struct {
	P50Bps int64 `json:"p50_bps"`
	P90Bps int64 `json:"p90_bps"`
	P99Bps int64 `json:"p99_bps"`
}

// flowsView is GET /api/gateway/flows as the API and MCP publish it.
//
// Flows is the head of the sample, not a total: no field here counts the rest,
// because a number invented by subtracting a top-N sum from a sum over all
// flows would be a claim the sample does not make. A caller that wants the
// total reads each client's rx_bps and tx_bps from /connections, which is a sum
// over everything the sample saw.
type flowsView struct {
	AsOf          time.Time `json:"as_of"`
	WindowSeconds int       `json:"window_seconds"`
	State         string    `json:"state"`
	Error         string    `json:"error,omitempty"`

	Flows []connectionFlowView `json:"flows"`
}

type connectionFlowView struct {
	Proto string `json:"proto"`

	// Src is the client's end of the connection and Dst the far end, whichever
	// way round the kernel opened it — the same normalisation that makes
	// client match src for a forwarded connection as well as an outbound one.
	Src   string `json:"src"`
	Dst   string `json:"dst"`
	Sport uint16 `json:"sport"`
	Dport uint16 `json:"dport"`
	State string `json:"state"`
	RXBps int64  `json:"rx_bps"`
	TXBps int64  `json:"tx_bps"`

	// Client is the display name of the inside address's owner, falling back
	// to the MAC so a recognised device is never anonymous; empty means no
	// presence source could place the address.
	Client string `json:"client"`
}

// NewConnections builds a sampler that reads this platform's connection table.
func NewConnections() *Connections {
	return &Connections{
		read: readConntrack,
		snapshot: connectionSnapshot{
			window:  connectionWindow,
			state:   connectionWarming,
			clients: []connectionClientView{},
			flows:   []connectionFlow{},
		},
	}
}

// sample folds one walk into the last answer.
//
// reason is why no walk was taken: nil samples the table, and a non-nil reason
// publishes itself as disabled. That is a different state from a table that
// could not be read, because a switch somebody turned off is not a fault.
//
// A sample is also where the baseline moves. Every measured window is the exact
// interval between two walks, not a nominal ten seconds: a tick that arrives
// late measured a longer interval and must say so, or every rate in the window
// would be inflated by the delay.
func (c *Connections) sample(now time.Time, join ClientJoin, reason error) {
	if c == nil {
		return
	}

	// The walk happens before the lock, the way the capacity sampler reads its
	// counters before it starts folding: a handler asking for the last sample
	// waits for a map swap, never for the kernel to finish a table read.
	var sample conntrackSample
	var err error
	if reason == nil {
		sample, err = c.read()
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if reason != nil {
		c.previous, c.last = nil, time.Time{}
		c.snapshot = connectionSnapshot{
			asOf: now, window: connectionWindow,
			state: connectionDisabled, reason: reason.Error(),
			clients: []connectionClientView{}, flows: []connectionFlow{},
		}
		return
	}
	if err != nil {
		c.previous, c.last = nil, time.Time{}
		state := connectionUnavailable
		if errors.Is(err, errConntrackNotCounting) {
			state = connectionDisabled
		}
		c.snapshot = connectionSnapshot{
			asOf: now, window: connectionWindow,
			state: state, reason: err.Error(),
			clients: []connectionClientView{}, flows: []connectionFlow{},
		}
		return
	}

	elapsed := now.Sub(c.last)
	// Two of these is a gap, not a slow sample: the counters kept moving while
	// nothing read them, and differencing across the gap would bill an outage
	// as one burst. The recovery walk is the new baseline, which is why it is
	// published as warming rather than as a window of zero rates.
	measured := !c.last.IsZero() && elapsed >= time.Second && elapsed <= 2*connectionWindow
	window, state := connectionWindow, connectionWarming
	if measured {
		window, state = elapsed, connectionReady
	}
	seconds := elapsed.Seconds()

	clients := make(map[netip.Addr]*connectionClient)
	flows := make([]connectionFlow, 0, len(sample.entries))
	previous := make(map[connectionKey]connectionCounters, len(sample.entries))

	for _, entry := range sample.entries {
		inside, originalAway, ok := connectionInside(entry, sample.local)
		if !ok {
			continue
		}
		key := connectionKey{
			proto: entry.proto,
			src:   entry.original.src, dst: entry.original.dst,
			sport: entry.original.sport, dport: entry.original.dport,
		}
		previous[key] = connectionCounters{
			original: entry.original.bytes, reply: entry.reply.bytes,
		}

		var rxBytes, txBytes uint64
		if measured {
			original, reply := connectionDelta(entry, c.previous[key])
			if originalAway {
				txBytes, rxBytes = original, reply
			} else {
				rxBytes, txBytes = original, reply
			}
		}
		rx := bitsPerSecond(rxBytes, seconds)
		tx := bitsPerSecond(txBytes, seconds)

		client := clients[inside]
		if client == nil {
			client = &connectionClient{addr: inside, mac: join.Owners[inside]}
			if client.mac != "" {
				client.name = join.Names[client.mac]
			}
			clients[inside] = client
		}
		client.connections++
		if entry.proto == "tcp" {
			client.tcp++
		} else {
			client.udp++
		}
		if entry.unreplied || entry.state == connectionSynSent {
			client.halfOpen++
		}
		client.rx += rx
		client.tx += tx
		client.rates = append(client.rates, rx)

		flow := connectionFlow{
			proto: entry.proto, state: entry.state, rx: rx, tx: tx,
		}
		if originalAway {
			flow.src, flow.dst = entry.original.src, entry.original.dst
			flow.sport, flow.dport = entry.original.sport, entry.original.dport
		} else {
			flow.src, flow.dst = entry.reply.src, entry.reply.dst
			flow.sport, flow.dport = entry.reply.sport, entry.reply.dport
		}
		// The label is the client's, not the flow's: a page that names a flow
		// differently from the client it is filed under would look like two
		// devices. The MAC is the fallback only when no source named it.
		flow.client = client.name
		if flow.client == "" {
			flow.client = client.mac
		}
		flows = append(flows, flow)
	}

	c.previous, c.last = previous, now
	c.snapshot = connectionSnapshot{
		asOf: now, window: window, state: state,
		clients: connectionClients(clients), flows: connectionFlows(flows),
	}
}

// connectionDelta is what one connection moved in the window.
//
// A missing key reads as a zero old value, so its subtraction yields the whole
// entry — and that is right rather than merely convenient: a conntrack entry is
// created by its first packet, so a key the previous walk did not have was born
// inside this window and every byte it holds belongs to this window. A reused
// 5-tuple can only move its counters backwards, so the same guard that catches
// an underflow also catches the reuse, and the new entry's counters are again
// this window's.
//
// That is the one place this deliberately differs from the capacity sampler's
// "a missing key is not counted": there a client's counters can predate the
// window, while here the entry itself cannot.
func connectionDelta(entry conntrackEntry, old connectionCounters) (original, reply uint64) {
	if entry.original.bytes < old.original || entry.reply.bytes < old.reply {
		return entry.original.bytes, entry.reply.bytes
	}
	return entry.original.bytes - old.original, entry.reply.bytes - old.reply
}

// connectionClients reduces the per-client accumulators to the published rows,
// percentiles and all.
func connectionClients(clients map[netip.Addr]*connectionClient) []connectionClientView {
	out := make([]connectionClientView, 0, len(clients))
	for _, client := range clients {
		sort.Slice(client.rates, func(i, j int) bool { return client.rates[i] < client.rates[j] })
		out = append(out, connectionClientView{
			IP: client.addr.String(), MAC: client.mac, Name: client.name,
			Connections: client.connections, TCP: client.tcp, UDP: client.udp,
			HalfOpen: client.halfOpen, RXBps: client.rx, TXBps: client.tx,
			PerConnection: connectionPercentiles{
				P50Bps: percentile(client.rates, 50),
				P90Bps: percentile(client.rates, 90),
				P99Bps: percentile(client.rates, 99),
			},
		})
	}
	// The page is asking who is responsible, so the busiest client comes
	// first. The address breaks ties so two samples of the same traffic order
	// the list the same way.
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i].RXBps+out[i].TXBps, out[j].RXBps+out[j].TXBps
		if a != b {
			return a > b
		}
		return out[i].IP < out[j].IP
	})
	return out
}

// connectionFlows orders the sample by rate. Everything after the total is a
// tie-break, so a list of equally idle connections still has one stable order.
func connectionFlows(flows []connectionFlow) []connectionFlow {
	sort.Slice(flows, func(i, j int) bool {
		a, b := flows[i], flows[j]
		if at, bt := a.rx+a.tx, b.rx+b.tx; at != bt {
			return at > bt
		}
		if c := a.src.Compare(b.src); c != 0 {
			return c < 0
		}
		if c := a.dst.Compare(b.dst); c != 0 {
			return c < 0
		}
		if a.sport != b.sport {
			return a.sport < b.sport
		}
		if a.dport != b.dport {
			return a.dport < b.dport
		}
		return a.proto < b.proto
	})
	return flows
}

// lastSample copies the published sample under the lock. The slices it shares
// are never mutated after publication — each sample builds new ones — so a
// handler can reduce them without holding the lock while it marshals.
func (c *Connections) lastSample() connectionSnapshot {
	if c == nil {
		return connectionSnapshot{
			window: connectionWindow, state: connectionWarming,
			clients: []connectionClientView{}, flows: []connectionFlow{},
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.snapshot
}

// view is the census as GET /api/gateway/connections serves it.
func (c *Connections) view() connectionsView {
	sample := c.lastSample()
	return connectionsView{
		AsOf: sample.asOf, WindowSeconds: windowSeconds(sample.window),
		State: sample.state, Error: sample.reason,
		Clients: append([]connectionClientView{}, sample.clients...),
	}
}

// flows is the head of the same sample as GET /api/gateway/flows serves it.
func (c *Connections) flows(limit int) flowsView {
	sample := c.lastSample()
	if limit < 0 {
		limit = 0
	}
	if limit > len(sample.flows) {
		limit = len(sample.flows)
	}
	out := make([]connectionFlowView, 0, limit)
	for _, flow := range sample.flows[:limit] {
		out = append(out, connectionFlowView{
			Proto: flow.proto,
			Src:   flow.src.String(), Dst: flow.dst.String(),
			Sport: flow.sport, Dport: flow.dport,
			State: flow.state, RXBps: flow.rx, TXBps: flow.tx,
			Client: flow.client,
		})
	}
	return flowsView{
		AsOf: sample.asOf, WindowSeconds: windowSeconds(sample.window),
		State: sample.state, Error: sample.reason,
		Flows: out,
	}
}

// windowSeconds is the window in whole seconds. The rates were divided by the
// exact interval, so this is the label rather than the divisor; a window under
// a second cannot occur because sample() never measures one.
func windowSeconds(window time.Duration) int {
	seconds := int(window.Round(time.Second) / time.Second)
	if seconds < 1 {
		return int(connectionWindow / time.Second)
	}
	return seconds
}

// percentile is the nearest-rank percentile: with n values, the ceil(pct*n/100)
// th smallest. On a handful of readings it therefore equals the maximum until
// there is enough of a tail to mean anything, and it never interpolates a rate
// the box did not measure.
func percentile(sorted []int64, pct int) int64 {
	if len(sorted) == 0 {
		return 0
	}
	rank := (pct*len(sorted) + 99) / 100
	if rank < 1 {
		rank = 1
	}
	if rank > len(sorted) {
		rank = len(sorted)
	}
	return sorted[rank-1]
}

// --- the table -------------------------------------------------------------

// conntrackTuple is one direction of a connection as the kernel prints it: the
// tuple and the bytes it has moved.
type conntrackTuple struct {
	src, dst     netip.Addr
	sport, dport uint16
	bytes        uint64
}

// conntrackEntry is one TCP or UDP connection the parser understood.
//
// original is the direction the connection was opened in and reply the other
// one. Which of the two runs toward the client is not a property of the entry
// but of who the client is, so classification happens after parsing.
type conntrackEntry struct {
	proto     string
	state     string
	original  conntrackTuple
	reply     conntrackTuple
	unreplied bool
}

// parseConntrack reads the kernel's text table into entries.
//
// Only TCP and UDP are kept. A "connection" here is a conversation with ports
// at both ends, and the census publishes tcp and udp counts that add up to its
// connection total; the ICMP, IGMP and tunnelled entries the table also tracks
// have no ports, so a per-connection rate for one would be a row with no
// identity. The endpoint's subject is clients' connections, not every packet
// conntrack has a slot for.
//
// The walk is literal: read once, then one pass with a token cursor, because
// the table is thousands of lines and this runs every ten seconds. A token is a
// sub-slice of the buffer, so a line allocates nothing but the strings
// netip.ParseAddr needs. A line the parser cannot read is dropped rather than
// guessed at — a made-up connection or a zero-byte reading would be reported as
// traffic that did not happen.
func parseConntrack(data []byte) []conntrackEntry {
	entries := make([]conntrackEntry, 0, bytes.Count(data, []byte{'\n'}))
	for len(data) > 0 {
		line := data
		if i := bytes.IndexByte(data, '\n'); i >= 0 {
			line, data = data[:i], data[i+1:]
		} else {
			data = nil
		}
		if entry, ok := parseConntrackLine(line); ok {
			entries = append(entries, entry)
		}
	}
	return entries
}

// parseConntrackLine reads one line of the form
//
//	ipv4 2 tcp 6 345456 ESTABLISHED src=… dst=… sport=… dport=… packets=… bytes=… \
//	     src=… dst=… sport=… dport=… packets=… bytes=… [ASSURED] mark=0 use=2
//
// The header is family, layer-3 protocol number, protocol name, layer-4
// protocol number and timeout. A TCP entry prints its state next; UDP has none.
// Everything after that is key=value tokens, with the flags the kernel appends
// ([ASSURED], [UNREPLIED], [OFFLOAD]) as bare tokens between them.
func parseConntrackLine(line []byte) (conntrackEntry, bool) {
	var (
		entry conntrackEntry
		index int
		tuple = -1
	)

	var proto []byte
	for field := 0; field < 5; field++ {
		var token []byte
		token, index = nextField(line, index)
		if len(token) == 0 {
			return conntrackEntry{}, false
		}
		if field == 2 {
			proto = token
		}
	}
	switch {
	case bytes.Equal(proto, []byte("tcp")):
		entry.proto = "tcp"
	case bytes.Equal(proto, []byte("udp")):
		entry.proto = "udp"
	default:
		return conntrackEntry{}, false
	}

	// The state is the only field between the timeout and the first tuple that
	// is neither key=value nor a flag.
	if token, next := nextField(line, index); len(token) > 0 && token[0] != '[' && bytes.IndexByte(token, '=') < 0 {
		entry.state = string(token)
		index = next
	}

	for index < len(line) {
		var token []byte
		token, index = nextField(line, index)
		if len(token) == 0 {
			continue
		}
		if bytes.Equal(token, []byte("[UNREPLIED]")) {
			entry.unreplied = true
			continue
		}
		equals := bytes.IndexByte(token, '=')
		if equals < 0 {
			// [ASSURED], [OFFLOAD] and anything else the kernel flags.
			continue
		}
		key, value := token[:equals], token[equals+1:]

		if bytes.Equal(key, []byte("src")) {
			tuple++
			if tuple > 1 {
				return conntrackEntry{}, false
			}
			addr, err := netip.ParseAddr(string(value))
			if err != nil {
				return conntrackEntry{}, false
			}
			addr = addr.Unmap()
			if tuple == 0 {
				entry.original.src = addr
			} else {
				entry.reply.src = addr
			}
			continue
		}

		var direction *conntrackTuple
		switch tuple {
		case 0:
			direction = &entry.original
		case 1:
			direction = &entry.reply
		default:
			continue
		}
		switch {
		case bytes.Equal(key, []byte("dst")):
			addr, err := netip.ParseAddr(string(value))
			if err != nil {
				return conntrackEntry{}, false
			}
			direction.dst = addr.Unmap()
		case bytes.Equal(key, []byte("sport")):
			port, ok := parseConntrackUint(value)
			if !ok || port > math.MaxUint16 {
				return conntrackEntry{}, false
			}
			direction.sport = uint16(port)
		case bytes.Equal(key, []byte("dport")):
			port, ok := parseConntrackUint(value)
			if !ok || port > math.MaxUint16 {
				return conntrackEntry{}, false
			}
			direction.dport = uint16(port)
		case bytes.Equal(key, []byte("bytes")):
			n, ok := parseConntrackUint(value)
			if !ok {
				return conntrackEntry{}, false
			}
			direction.bytes = n
		}
	}

	if !entry.original.src.IsValid() || !entry.original.dst.IsValid() ||
		!entry.reply.src.IsValid() || !entry.reply.dst.IsValid() {
		return conntrackEntry{}, false
	}
	return entry, true
}

// nextField returns the space-separated field at or after index, and the index
// just past it. The field is a sub-slice of line, so walking a line does not
// split it.
func nextField(line []byte, index int) (field []byte, next int) {
	for index < len(line) && (line[index] == ' ' || line[index] == '\t') {
		index++
	}
	start := index
	for index < len(line) && line[index] != ' ' && line[index] != '\t' {
		index++
	}
	return line[start:index], index
}

// parseConntrackUint is strconv.ParseUint without the string conversion, for
// the same reason nextField exists. An overflowing value is refused rather than
// wrapped: a wrapped byte counter would become an enormous rate.
func parseConntrackUint(field []byte) (uint64, bool) {
	if len(field) == 0 {
		return 0, false
	}
	var n uint64
	for _, c := range field {
		if c < '0' || c > '9' || n > (math.MaxUint64-uint64(c-'0'))/10 {
			return 0, false
		}
		n = n*10 + uint64(c-'0')
	}
	return n, true
}

// --- who a connection belongs to -------------------------------------------

// connectionInside names the client a tracked connection belongs to, and
// whether the original direction runs away from it.
//
// The four cases are the whole attribution model, and they are decided from the
// two source addresses rather than from the destination, because NAT rewrites
// the destination of one direction and not the other:
//
//   - The box opened it: its own traffic, not a client's, so it is left out.
//   - It ends on the box: a client talking to the resolver, the admin UI or a
//     relayed stream belongs to the client that opened it. §7.1 counts those
//     bytes for that client, so the census has to see them too.
//   - Ordinary outbound: the opener is the client.
//   - Forwarded (DNAT): the remote opened it and the reply tuple carries the
//     translated local endpoint, which is the client the port forward lands on.
func connectionInside(entry conntrackEntry, local map[netip.Addr]bool) (inside netip.Addr, originalAway, ok bool) {
	switch {
	case local[entry.original.src]:
		return netip.Addr{}, false, false
	case local[entry.reply.src]:
		if insideAddr(entry.original.src) {
			return entry.original.src, true, true
		}
		return netip.Addr{}, false, false
	case insideAddr(entry.original.src):
		return entry.original.src, true, true
	case insideAddr(entry.reply.src):
		return entry.reply.src, false, true
	}
	return netip.Addr{}, false, false
}

// insideAddr reports whether a is one of the address spaces this box serves.
//
// Private and CGNAT are the house and the carrier's front porch; link-local is
// the client that could not get a lease and gave itself 169.254/16, whose
// traffic is still the client's. Public addresses are never a client here: this
// router forwards for networks it is inside of, so a public source is the far
// end of something.
func insideAddr(a netip.Addr) bool {
	return a.IsValid() && (a.IsPrivate() || a.IsLinkLocalUnicast() || core.IsCGNAT(a))
}

package inspection

import (
	"context"
	"errors"
	"net/netip"
	"strings"
	"testing"
	"time"
)

// The activity chart joins conntrack on (client, destination). A request the
// relay never named — dialled by address, resolved over DoH, or opened before
// the switch — still has to produce that pairing, or its bytes land in 未命名.
func TestProxyRequestsNameTheirFlow(t *testing.T) {
	s := &Service{session: &session{done: make(chan struct{})}}

	s.consume(s.session, strings.NewReader(
		`{"kind":"request","url":"https://api.example.com/v1/ping","client":"192.168.1.10","server":"93.184.216.34"}`+"\n"))

	got := s.Names()
	if len(got) != 1 {
		t.Fatalf("recorded %d names, want 1: %+v", len(got), got)
	}
	if got[0].Name != "api.example.com" {
		t.Errorf("name = %q, want the URL's host", got[0].Name)
	}
	if got[0].Client != netip.MustParseAddr("192.168.1.10") || got[0].Addr != netip.MustParseAddr("93.184.216.34") {
		t.Errorf("pairing = %s→%s, want the event's endpoints", got[0].Client, got[0].Addr)
	}
}

// A connection that failed before there was a URL is exactly the case where the
// proxy has a name and nothing else does.
func TestFailedConnectionsNameWhatTheyReached(t *testing.T) {
	s := &Service{session: &session{done: make(chan struct{})}}

	s.consume(s.session, strings.NewReader(
		`{"kind":"failed","target":"push.example.net","client":"192.168.1.10","server":"203.0.113.5"}`+"\n"))

	got := s.Names()
	if len(got) != 1 || got[0].Name != "push.example.net" {
		t.Fatalf("names = %+v, want the failed target", got)
	}
}

// Half a pairing is not a pairing: without both addresses there is nothing to
// join conntrack on, and guessing would attribute bytes to the wrong row.
func TestNamesWithoutEndpointsAreIgnored(t *testing.T) {
	s := &Service{session: &session{done: make(chan struct{})}}

	s.consume(s.session, strings.NewReader(strings.Join([]string{
		`{"kind":"request","url":"https://no-addresses.example/a"}`,
		`{"kind":"request","url":"https://no-server.example/a","client":"192.168.1.10"}`,
		`{"kind":"failed","target":"unknown","client":"192.168.1.10","server":"203.0.113.5"}`,
	}, "\n")+"\n"))

	if got := s.Names(); len(got) != 0 {
		t.Errorf("recorded %+v, want nothing", got)
	}
}

// The proxy's map is bounded for the same reason the relay's is: a client left
// under inspection enumerates addresses for as long as nobody stops it.
func TestProxyNamesAreBounded(t *testing.T) {
	s := &Service{}
	now := time.Now()

	s.mu.Lock()
	for i := range maxObservedNames + 100 {
		addr := netip.AddrFrom4([4]byte{10, byte(i >> 16), byte(i >> 8), byte(i)})
		s.observeName(Event{URL: "https://example.com/", Client: "192.168.1.10", Server: addr.String()}, now.Add(time.Duration(i)*time.Second))
	}
	s.mu.Unlock()

	if n := len(s.Names()); n > maxObservedNames {
		t.Errorf("held %d names, above the %d cap", n, maxObservedNames)
	}
}

// The relay's map covers every client, so it must win a pairing the proxy also
// saw: its record is the name the client typed, CNAME chain and all.
func TestNamesFromGivesTheRelayTheFirstSay(t *testing.T) {
	client := netip.MustParseAddr("192.168.1.10")
	addr := netip.MustParseAddr("93.184.216.34")
	relay := func(context.Context) ([]Name, error) {
		return []Name{{Client: client, Addr: addr, Name: "relay.example"}}, nil
	}
	proxy := func(context.Context) ([]Name, error) {
		return []Name{{Client: client, Addr: addr, Name: "proxy.example"}}, nil
	}

	got, err := NamesFrom(relay, proxy)(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "relay.example" {
		t.Errorf("names = %+v, want the relay's", got)
	}
}

// A source that is down leaves the sample with the others; only when every one
// failed does the sampler keep the names it already had.
func TestNamesFromSurvivesAQuietSource(t *testing.T) {
	broken := func(context.Context) ([]Name, error) { return nil, errors.New("relay socket closed") }
	working := func(context.Context) ([]Name, error) {
		return []Name{{Client: netip.MustParseAddr("192.168.1.10"), Addr: netip.MustParseAddr("203.0.113.5"), Name: "proxy.example"}}, nil
	}

	got, err := NamesFrom(broken, working)(context.Background())
	if err != nil || len(got) != 1 || got[0].Name != "proxy.example" {
		t.Fatalf("got %+v, %v; want the working source's names", got, err)
	}

	if _, err := NamesFrom(broken)(context.Background()); err == nil {
		t.Error("no source failed, yet the call reported success")
	}
}

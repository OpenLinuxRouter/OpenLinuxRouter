package dns

import (
	"errors"
	"net/netip"
	"strings"
	"testing"
)

// testLinks is the interface picture every test in this package validates
// against: one adopted LAN interface, one adopted WAN, one the operator never
// handed us.
func testLinks() StaticLinks {
	return StaticLinks{
		"lan0": {Name: "lan0", Adopted: true, Up: true,
			Prefixes: []netip.Prefix{netip.MustParsePrefix("192.168.1.1/24"), netip.MustParsePrefix("fd00::1/64")}},
		"wan0": {Name: "wan0", Adopted: true, Up: true,
			Prefixes: []netip.Prefix{netip.MustParsePrefix("203.0.113.7/24")}},
		"guest0": {Name: "guest0", Adopted: false, Up: true,
			Prefixes: []netip.Prefix{netip.MustParsePrefix("192.168.30.1/24")}},
	}
}

// validConfig is the smallest configuration that should pass cleanly, so every
// test below can state exactly the one thing it is breaking.
func validConfig() Config {
	c := Config{
		Enabled: true,
		Listen:  []netip.AddrPort{netip.MustParseAddrPort("192.168.1.1:53")},
	}
	c.Normalize()
	return c
}

func errorPaths(r Result) []string {
	out := make([]string, 0, len(r.Errors))
	for _, p := range r.Errors {
		out = append(out, p.Path)
	}
	return out
}

func hasProblem(problems []Problem, pathPrefix string) bool {
	for _, p := range problems {
		if strings.HasPrefix(p.Path, pathPrefix) {
			return true
		}
	}
	return false
}

func TestValidateAcceptsAMinimalConfig(t *testing.T) {
	if res := Validate(validConfig(), testLinks(), nil); !res.OK() {
		t.Errorf("a minimal valid config was rejected: %v", errorPaths(res))
	}
}

func TestValidatePrivateDomains(t *testing.T) {
	t.Run("malformed is refused", func(t *testing.T) {
		c := validConfig()
		c.PrivateDomains = []string{"bad domain"}
		res := Validate(c, testLinks(), nil)
		if !hasProblem(res.Errors, "private_domains[0]") {
			t.Fatalf("expected an error at private_domains[0], got %v", errorPaths(res))
		}
	})

	t.Run("inside the local domain is called out", func(t *testing.T) {
		c := validConfig()
		c.PrivateDomains = []string{c.LocalDomainOrDefault()}
		res := Validate(c, testLinks(), nil)
		if !hasProblem(res.Warnings, "private_domains[0]") {
			t.Fatalf("expected a warning at private_domains[0], got %v", errorPaths(res))
		}
	})

	t.Run("a claimed zone is fine", func(t *testing.T) {
		c := validConfig()
		c.PrivateDomains = []string{"home.example.com"}
		if res := Validate(c, testLinks(), nil); !res.OK() {
			t.Fatalf("a zone beside the local domain was rejected: %v", errorPaths(res))
		}
	})
}

func TestValidateListen(t *testing.T) {
	tests := []struct {
		name    string
		edit    func(*Config)
		wantErr string // a path prefix; empty means "must pass"
	}{
		{
			// The default, and the thing that cannot go stale. What keeps this
			// inbound access is the firewall's job.
			name: "nothing given means the wildcard",
			edit: func(c *Config) { c.Listen = nil },
		},
		{
			// The explicit spelling of the same default. Refusing it would be a
			// rule with nothing behind it.
			name: "wildcard address",
			edit: func(c *Config) { c.Listen = []netip.AddrPort{netip.MustParseAddrPort("0.0.0.0:53")} },
		},
		{
			// RelayPort and the hijack rule can each name only one port, so a
			// set that disagrees would leave addresses unreachable through the
			// redirect without saying so.
			name: "listen addresses disagreeing about the port",
			edit: func(c *Config) {
				c.Listen = []netip.AddrPort{
					netip.MustParseAddrPort("192.168.1.1:53"),
					netip.MustParseAddrPort("[fd00::1]:5300"),
				}
			},
			wantErr: "listen[1]",
		},
		{
			name: "no port",
			edit: func(c *Config) {
				c.Listen = []netip.AddrPort{netip.AddrPortFrom(netip.MustParseAddr("192.168.1.1"), 0)}
			},
			wantErr: "listen[0]",
		},
		{
			name: "an address that is on no interface",
			edit: func(c *Config) {
				c.Listen = []netip.AddrPort{netip.MustParseAddrPort("10.9.9.9:53")}
			},
			wantErr: "listen[0]",
		},
		{
			// design.md §3.4 is adopt-only. A resolver appearing on an
			// interface nobody handed us is the exact surprise that forbids.
			name: "an unadopted interface",
			edit: func(c *Config) {
				c.Listen = []netip.AddrPort{netip.MustParseAddrPort("192.168.30.1:53")}
			},
			wantErr: "listen[0]",
		},
		{
			// unbound's port. Two things cannot hold it.
			name: "colliding with the resolver",
			edit: func(c *Config) {
				c.Listen = []netip.AddrPort{netip.MustParseAddrPort("127.0.0.1:5353")}
			},
			wantErr: "listen[0]",
		},
		{
			name: "listed twice",
			edit: func(c *Config) {
				c.Listen = append(c.Listen, netip.MustParseAddrPort("192.168.1.1:53"))
			},
			wantErr: "listen[1]",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := validConfig()
			tc.edit(&cfg)
			res := Validate(cfg, testLinks(), nil)
			if tc.wantErr == "" {
				if !res.OK() {
					t.Errorf("unexpected errors: %v", errorPaths(res))
				}
				return
			}
			if !hasProblem(res.Errors, tc.wantErr) {
				t.Errorf("want an error at %q, got %v", tc.wantErr, errorPaths(res))
			}
		})
	}
}

func TestValidateUpstream(t *testing.T) {
	t.Run("forwarding with no servers", func(t *testing.T) {
		cfg := validConfig()
		cfg.Upstream.Mode = ModeForward
		if res := Validate(cfg, testLinks(), nil); !hasProblem(res.Errors, "upstream.servers") {
			t.Errorf("want an error, got %v", errorPaths(res))
		}
	})

	// With the wildcard as the default, "ourselves" is every address this box
	// holds — so the loop check reads the interfaces, not the listen list. A
	// check against c.Listen would pass this config silently and build a loop.
	t.Run("forwarding to ourselves with no pinned listen address", func(t *testing.T) {
		cfg := validConfig()
		cfg.Listen = nil
		cfg.Upstream = Upstream{
			Mode:    ModeForward,
			Servers: []netip.AddrPort{netip.MustParseAddrPort("192.168.1.1:53")},
		}
		if res := Validate(cfg, testLinks(), nil); !hasProblem(res.Errors, "upstream.servers[0]") {
			t.Errorf("a forwarding loop was accepted: %v", errorPaths(res))
		}
	})

	t.Run("forwarding to ourselves", func(t *testing.T) {
		cfg := validConfig()
		cfg.Upstream = Upstream{
			Mode:    ModeForward,
			Servers: []netip.AddrPort{netip.MustParseAddrPort("192.168.1.1:53")},
		}
		if res := Validate(cfg, testLinks(), nil); !hasProblem(res.Errors, "upstream.servers[0]") {
			t.Errorf("a forwarding loop was accepted: %v", errorPaths(res))
		}
	})

	t.Run("TLS without a certificate name warns", func(t *testing.T) {
		cfg := validConfig()
		cfg.Upstream = Upstream{
			Mode:    ModeForward,
			Servers: []netip.AddrPort{netip.MustParseAddrPort("1.1.1.1:853")},
			TLS:     true,
		}
		res := Validate(cfg, testLinks(), nil)
		if !res.OK() {
			t.Fatalf("this should warn, not fail: %v", errorPaths(res))
		}
		if !hasProblem(res.Warnings, "upstream.tls_name") {
			t.Error("no warning that the upstream is unauthenticated")
		}
	})

	t.Run("servers listed but unused warns", func(t *testing.T) {
		cfg := validConfig()
		cfg.Upstream = Upstream{Servers: []netip.AddrPort{netip.MustParseAddrPort("1.1.1.1:53")}}
		res := Validate(cfg, testLinks(), nil)
		if !hasProblem(res.Warnings, "upstream.servers") {
			t.Error("forwarders under the recursing default were accepted silently")
		}
	})
}

func TestValidatePolicies(t *testing.T) {
	t.Run("two default policies", func(t *testing.T) {
		// Which one applies would otherwise depend on the order they happen to
		// be in, which is not a thing an operator can reason about.
		cfg := validConfig()
		cfg.Policies = []Policy{{Name: "a"}, {Name: "b"}}
		if res := Validate(cfg, testLinks(), nil); !hasProblem(res.Errors, "policies[1].clients") {
			t.Errorf("two default policies were accepted: %v", errorPaths(res))
		}
	})

	t.Run("duplicate names", func(t *testing.T) {
		cfg := validConfig()
		cfg.Policies = []Policy{
			{Name: "kids", Clients: []netip.Prefix{netip.MustParsePrefix("192.168.1.50/32")}},
			{Name: "kids", Clients: []netip.Prefix{netip.MustParsePrefix("192.168.1.51/32")}},
		}
		if res := Validate(cfg, testLinks(), nil); !hasProblem(res.Errors, "policies[1].name") {
			t.Errorf("a duplicate policy name was accepted: %v", errorPaths(res))
		}
	})

	t.Run("one prefix claimed twice", func(t *testing.T) {
		// The most-specific rule cannot break this tie, so which one wins would
		// be an accident.
		cfg := validConfig()
		cfg.Policies = []Policy{
			{Name: "a", Clients: []netip.Prefix{netip.MustParsePrefix("192.168.1.50/32")}},
			{Name: "b", Clients: []netip.Prefix{netip.MustParsePrefix("192.168.1.50/32")}},
		}
		res := Validate(cfg, testLinks(), nil)
		if !hasProblem(res.Errors, "policies[1].clients[0]") {
			t.Errorf("an ambiguous client claim was accepted: %v", errorPaths(res))
		}
	})

	t.Run("a name that is a filename", func(t *testing.T) {
		// It becomes a path under policy.d, so a slash is a traversal rather
		// than a typo.
		cfg := validConfig()
		cfg.Policies = []Policy{{Name: "../../etc/passwd"}}
		if res := Validate(cfg, testLinks(), nil); !hasProblem(res.Errors, "policies[0].name") {
			t.Errorf("a path was accepted as a policy name: %v", errorPaths(res))
		}
	})

	t.Run("a URL in a blocklist", func(t *testing.T) {
		// The actual failure mode: a pasted URL blocks nothing and says nothing.
		cfg := validConfig()
		cfg.Policies = []Policy{{Name: "kids", Block: []string{"https://example.com/ads"}}}
		cfg.Normalize()
		if res := Validate(cfg, testLinks(), nil); !hasProblem(res.Errors, "policies[0].block[0]") {
			t.Errorf("a URL was accepted as a domain: %v", errorPaths(res))
		}
	})

	t.Run("a name in both lists warns", func(t *testing.T) {
		cfg := validConfig()
		cfg.Policies = []Policy{{
			Name: "kids", Block: []string{"example.com"}, Allow: []string{"example.com"},
		}}
		res := Validate(cfg, testLinks(), nil)
		if !res.OK() {
			t.Fatalf("this should warn, not fail: %v", errorPaths(res))
		}
		if !hasProblem(res.Warnings, "policies[0].allow") {
			t.Error("no warning that the block entry is dead")
		}
	})

	t.Run("policies without a redirect warn", func(t *testing.T) {
		// docs/dns.md §2's only dangerous misconfiguration: blocking applies to
		// clients that ask us, and nothing makes them.
		cfg := validConfig()
		cfg.Policies = []Policy{{Name: "kids", Block: []string{"example.com"}}}
		res := Validate(cfg, testLinks(), nil)
		if !hasProblem(res.Warnings, "hijack.enabled") {
			t.Error("no warning that the blocklist is advisory")
		}
	})
}

func TestValidateHijack(t *testing.T) {
	t.Run("no interfaces", func(t *testing.T) {
		// "All of them" would capture the WAN side, so it cannot be the meaning
		// of an empty list.
		cfg := validConfig()
		cfg.Hijack = Hijack{Enabled: true}
		if res := Validate(cfg, testLinks(), nil); !hasProblem(res.Errors, "hijack.interfaces") {
			t.Errorf("want an error, got %v", errorPaths(res))
		}
	})

	t.Run("an unadopted interface", func(t *testing.T) {
		cfg := validConfig()
		cfg.Hijack = Hijack{Enabled: true, Interfaces: []string{"guest0"}}
		if res := Validate(cfg, testLinks(), nil); !hasProblem(res.Errors, "hijack.interfaces[0]") {
			t.Errorf("want an error, got %v", errorPaths(res))
		}
	})

	t.Run("an unknown interface", func(t *testing.T) {
		cfg := validConfig()
		cfg.Hijack = Hijack{Enabled: true, Interfaces: []string{"nope0"}}
		if res := Validate(cfg, testLinks(), nil); !hasProblem(res.Errors, "hijack.interfaces[0]") {
			t.Errorf("want an error, got %v", errorPaths(res))
		}
	})

	t.Run("without blocking DoT warns", func(t *testing.T) {
		// Ranked by cost to defeat: leaving the cheap door open makes the
		// expensive work pointless.
		cfg := validConfig()
		cfg.Hijack = Hijack{Enabled: true, Interfaces: []string{"lan0"}}
		res := Validate(cfg, testLinks(), nil)
		if !res.OK() {
			t.Fatalf("this should warn, not fail: %v", errorPaths(res))
		}
		if !hasProblem(res.Warnings, "hijack.block_dot") {
			t.Error("no warning that DNS-over-TLS routes around the redirect")
		}
	})

	// There is no v6 gap left to warn about. `redirect` rewrites to the
	// incoming interface's own address in whichever family the packet arrived
	// in, so a v4-only listen address no longer leaves IPv6 uncaptured.
	t.Run("a v4-only listen address no longer leaves a v6 gap", func(t *testing.T) {
		cfg := validConfig()
		cfg.Hijack = Hijack{Enabled: true, Interfaces: []string{"lan0"}, BlockDoT: true}
		res := Validate(cfg, testLinks(), nil)
		if hasProblem(res.Warnings, "listen") {
			t.Errorf("warned about a family gap the redirect no longer has: %v", res.Warnings)
		}
	})
}

// The escape hatch is a passthrough, not a free-for-all: a directive that
// silently overrode a rendered one would defeat the point of having it declared.
func TestValidateExtraConf(t *testing.T) {
	for _, denied := range []string{
		"interface: 0.0.0.0",
		"access-control: 0.0.0.0/0 allow",
		"forward-zone:",
		"  forward-addr: 8.8.8.8",
		"username: root",
	} {
		cfg := validConfig()
		cfg.ExtraConf = denied
		res := Validate(cfg, testLinks(), nil)
		if len(res.Errors) == 0 {
			t.Errorf("%q was accepted in the escape hatch", denied)
		}
	}

	// Anything olr does not render passes through untouched, which is the
	// whole promise of the hatch.
	cfg := validConfig()
	cfg.ExtraConf = "# a comment\nserver:\n  msg-cache-size: 8m\n"
	if res := Validate(cfg, testLinks(), nil); !res.OK() {
		t.Errorf("a legitimate escape-hatch setting was rejected: %v", errorPaths(res))
	}
}

func TestCheckName(t *testing.T) {
	for _, ok := range []string{"example.com", "a.b.c.example.com", "xn--kbenhavn-54a.example", "_dmarc.example.com"} {
		if err := checkName(ok); err != nil {
			t.Errorf("checkName(%q) = %v, want nil", ok, err)
		}
	}
	for _, bad := range []string{"", "example .com", "example..com", ".example.com", "-bad.example", "http://x/y"} {
		if err := checkName(bad); err == nil {
			t.Errorf("checkName(%q) = nil, want an error", bad)
		}
	}
}

func hostConfig(hosts ...Host) Config {
	c := validConfig()
	c.Hosts = hosts
	c.Normalize()
	return c
}

func TestValidateLocalDomain(t *testing.T) {
	tests := []struct {
		name     string
		domain   string
		wantErr  bool
		wantWarn bool
	}{
		{name: "the default is fine", domain: ""},
		{name: "home.arpa", domain: "home.arpa"},
		{name: "internal is reserved too", domain: "internal"},
		{name: "a subdomain of one you own", domain: "lan.example.com"},
		// mDNS owns .local. Publishing it here resolves on some clients and not
		// others, with nothing on this box to explain the difference.
		{name: "local belongs to mDNS", domain: "local", wantErr: true},
		{name: "not a name at all", domain: "http://nas", wantErr: true},
		// Not reserved by anybody, so it may one day be delegated and every
		// name under it would start resolving on the internet.
		{name: "an unreserved single label", domain: "lan", wantWarn: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := validConfig()
			c.LocalDomain = tt.domain
			c.Normalize()
			res := Validate(c, testLinks(), nil)

			if got := hasProblem(res.Errors, "local_domain"); got != tt.wantErr {
				t.Errorf("error = %v, want %v (%v)", got, tt.wantErr, res.Errors)
			}
			if got := hasProblem(res.Warnings, "local_domain"); got != tt.wantWarn {
				t.Errorf("warning = %v, want %v (%v)", got, tt.wantWarn, res.Warnings)
			}
		})
	}
}

func TestValidateHosts(t *testing.T) {
	addr := func(s string) []netip.Addr { return []netip.Addr{netip.MustParseAddr(s)} }

	tests := []struct {
		name  string
		hosts []Host
		want  bool
	}{
		{name: "a name and an address on the LAN",
			hosts: []Host{{Name: "sony-tv", Addrs: addr("192.168.1.50")}}},
		{name: "both families on one name",
			hosts: []Host{{Name: "nas", Addrs: []netip.Addr{
				netip.MustParseAddr("192.168.1.10"), netip.MustParseAddr("fd00::10")}}}},
		{name: "a name several labels deep",
			hosts: []Host{{Name: "tv.living-room", Addrs: addr("192.168.1.50")}}},

		{name: "no name", hosts: []Host{{Addrs: addr("192.168.1.50")}}, want: true},
		{name: "no address", hosts: []Host{{Name: "sony-tv"}}, want: true},
		{name: "an underscore no browser agrees about",
			hosts: []Host{{Name: "sony_tv", Addrs: addr("192.168.1.50")}}, want: true},
		{name: "the unspecified address",
			hosts: []Host{{Name: "sony-tv", Addrs: addr("0.0.0.0")}}, want: true},
		{name: "the local domain itself",
			hosts: []Host{{Name: DefaultLocalDomain, Addrs: addr("192.168.1.50")}}, want: true},
		{name: "the same name twice", hosts: []Host{
			{Name: "sony-tv", Addrs: addr("192.168.1.50")},
			{Name: "sony-tv", Addrs: addr("192.168.1.51")},
		}, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Not through hostConfig: Normalize would collapse the duplicate
			// case before the rule that catches it ever runs.
			c := validConfig()
			c.Hosts = tt.hosts
			res := Validate(c, testLinks(), nil)

			if got := hasProblem(res.Errors, "hosts["); got != tt.want {
				t.Errorf("error = %v, want %v (%v)", got, tt.want, res.Errors)
			}
		})
	}
}

// A local name pointing off this network is legal — it may be a host across a
// VPN — and it is also exactly what a typo'd octet looks like. Warn, do not
// refuse.
func TestValidateWarnsAboutAnAddressOffTheNetworkItServes(t *testing.T) {
	c := hostConfig(Host{Name: "nas", Addrs: []netip.Addr{netip.MustParseAddr("10.9.9.9")}})
	res := Validate(c, testLinks(), nil)

	if !res.OK() {
		t.Errorf("an off-LAN address was refused: %v", errorPaths(res))
	}
	if !hasProblem(res.Warnings, "hosts[0].addresses[0]") {
		t.Errorf("no warning for an address off every served network: %v", res.Warnings)
	}
}

func TestValidateAcceptsHostsOnEveryServedFamily(t *testing.T) {
	c := hostConfig(Host{Name: "nas", Addrs: []netip.Addr{
		netip.MustParseAddr("192.168.1.10"),
		netip.MustParseAddr("fd00::10"),
	}})
	res := Validate(c, testLinks(), nil)

	if !res.OK() {
		t.Fatalf("rejected: %v", errorPaths(res))
	}
	if hasProblem(res.Warnings, "hosts[") {
		t.Errorf("warned about addresses on the served networks: %v", res.Warnings)
	}
}

// staticReservations is a ReservationView backed by a slice, for tests.
type staticReservations []Reservation

func (s staticReservations) Reservations() ([]Reservation, error) { return s, nil }

// failingReservations stands for a dhcp config that cannot be read.
type failingReservations struct{}

func (failingReservations) Reservations() ([]Reservation, error) {
	return nil, errors.New("cannot read the configuration document")
}

// The failure this cross-check exists for: the reservation was renumbered, DHCP
// moved the client, and the name goes on answering where it used to be.
func TestValidateWarnsWhenANameAndItsReservationDisagree(t *testing.T) {
	c := hostConfig(Host{Name: "sony-tv", Addrs: []netip.Addr{netip.MustParseAddr("192.168.1.50")}})
	view := staticReservations{
		{MAC: "aa:bb:cc:dd:ee:ff", IP: netip.MustParseAddr("192.168.1.51"), Hostname: "sony-tv"},
	}

	res := Validate(c, testLinks(), view)

	// A warning and not an error, deliberately: both values were typed on
	// purpose, and refusing would let a dhcp edit make the dns config
	// unappliable — the coupling this design exists to avoid.
	if !res.OK() {
		t.Errorf("a disagreement was refused rather than reported: %v", errorPaths(res))
	}
	if !hasProblem(res.Warnings, "hosts[0].addresses") {
		t.Fatalf("no warning: %v", res.Warnings)
	}
	// The message has to carry both addresses, or it sends the operator hunting
	// through two modules to find out which one moved.
	msg := res.Warnings[len(res.Warnings)-1].Message
	for _, want := range []string{"192.168.1.50", "192.168.1.51", "aa:bb:cc:dd:ee:ff"} {
		if !strings.Contains(msg, want) {
			t.Errorf("the warning does not mention %q: %s", want, msg)
		}
	}
}

func TestValidateAcceptsANameThatMatchesItsReservation(t *testing.T) {
	tests := []struct {
		name  string
		hosts []Host
		view  staticReservations
	}{
		{
			name:  "the same address",
			hosts: []Host{{Name: "sony-tv", Addrs: []netip.Addr{netip.MustParseAddr("192.168.1.50")}}},
			view:  staticReservations{{MAC: "aa:bb:cc:dd:ee:ff", IP: netip.MustParseAddr("192.168.1.50"), Hostname: "sony-tv"}},
		},
		{
			// One name, both families. The reservation only knows about the v4
			// one, and that is a match rather than a disagreement.
			name: "among several addresses",
			hosts: []Host{{Name: "nas", Addrs: []netip.Addr{
				netip.MustParseAddr("192.168.1.10"), netip.MustParseAddr("fd00::10")}}},
			view: staticReservations{{MAC: "aa:bb:cc:dd:ee:01", IP: netip.MustParseAddr("192.168.1.10"), Hostname: "nas"}},
		},
		{
			// Written in full on the dhcp side. Both spellings are the same
			// name, or the check would fire on a config that is correct.
			name:  "a reservation hostname written in full",
			hosts: []Host{{Name: "nas", Addrs: []netip.Addr{netip.MustParseAddr("192.168.1.10")}}},
			view:  staticReservations{{MAC: "aa:bb:cc:dd:ee:01", IP: netip.MustParseAddr("192.168.1.10"), Hostname: "NAS.home.arpa."}},
		},
		{
			// Most reservations have no name at all, and they must match
			// nothing rather than everything.
			name:  "a nameless reservation at a different address",
			hosts: []Host{{Name: "sony-tv", Addrs: []netip.Addr{netip.MustParseAddr("192.168.1.50")}}},
			view:  staticReservations{{MAC: "aa:bb:cc:dd:ee:02", IP: netip.MustParseAddr("192.168.1.99")}},
		},
		{
			name:  "a reservation for some other client",
			hosts: []Host{{Name: "sony-tv", Addrs: []netip.Addr{netip.MustParseAddr("192.168.1.50")}}},
			view:  staticReservations{{MAC: "aa:bb:cc:dd:ee:03", IP: netip.MustParseAddr("192.168.1.99"), Hostname: "printer"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := Validate(hostConfig(tt.hosts...), testLinks(), tt.view)
			if hasProblem(res.Warnings, "hosts[0].addresses") {
				t.Errorf("warned about a config that agrees: %v", res.Warnings)
			}
		})
	}
}

// A dhcp module that cannot be read costs the cross-check and nothing else. It
// must not fail the dns config, which would let one module's broken document
// block edits to another's.
func TestValidateSurvivesAnUnreadableDhcpConfig(t *testing.T) {
	c := hostConfig(Host{Name: "sony-tv", Addrs: []netip.Addr{netip.MustParseAddr("192.168.1.50")}})

	for _, view := range []ReservationView{nil, failingReservations{}} {
		res := Validate(c, testLinks(), view)
		if !res.OK() {
			t.Errorf("%T: refused: %v", view, errorPaths(res))
		}
		if hasProblem(res.Warnings, "hosts[0].addresses") {
			t.Errorf("%T: warned with nothing to compare against: %v", view, res.Warnings)
		}
	}
}

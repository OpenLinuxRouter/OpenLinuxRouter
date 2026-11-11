package ingress

import (
	"net/netip"
	"strings"
	"testing"
)

// good returns a config that validates cleanly, so each test can break exactly
// one thing and the failure it asserts is unambiguous.
func good() Config {
	return Config{
		Enabled: true,
		// Deliberately not the resolver's zone: publishing aside, a domain that
		// has nothing to do with what this network answers is the ordinary case.
		Domain: "svc.example.com",
		Certificate: Certificate{
			Provider: "cloudflare",
			Token:    "tok",
			Email:    "ops@example.com",
		},
		Services: []Service{{
			Name:     "grafana",
			Upstream: Upstream{Client: "nuc", Port: 3000},
		}},
	}
}

// goodDNS is the resolver's default zone, not the published domain: they are
// separate settings, and the common arrangement is clients here and services
// somewhere the operator owns.
func goodDNS() StaticDNS {
	return StaticDNS{Domain: "home.arpa"}
}

func goodDevices() StaticClients {
	return StaticClients{
		"nuc": {Addr: netip.MustParseAddr("192.168.1.50"), Fixed: true},
		"tv":  {Addr: netip.MustParseAddr("192.168.1.77")},
	}
}

// findError reports whether any error is attached to path and mentions want.
func findError(t *testing.T, r Result, path, want string) {
	t.Helper()
	for _, p := range r.Errors {
		if p.Path == path && strings.Contains(p.Message, want) {
			return
		}
	}
	t.Fatalf("no error at %q containing %q; got %v", path, want, r.Errors)
}

func TestValidateAcceptsAGoodConfig(t *testing.T) {
	r := Validate(good(), goodDNS(), goodDevices())
	if !r.OK() {
		t.Fatalf("expected valid, got %v", r.Errors)
	}
	if len(r.Warnings) != 0 {
		t.Fatalf("expected no warnings, got %v", r.Warnings)
	}
}

// The first error most operators will ever see from this module: the field is
// empty, because nothing is published yet.
func TestValidateRequiresADomain(t *testing.T) {
	c := good()
	c.Domain = ""
	r := Validate(c, goodDNS(), goodDevices())
	findError(t, r, "domain", "required")
	if !strings.Contains(r.Errors[0].Message, "olr ingress set --domain") {
		t.Fatalf("the refusal must carry the remedy, got %q", r.Errors[0].Message)
	}
}

// Now a *choice*, not an inherited fact: pointing the published domain at the
// resolver's default would still be unissuable.
func TestValidateRefusesUnissuableDomains(t *testing.T) {
	for _, domain := range []string{"home.arpa", "my.lan", "box.local", "x.internal", "a.test"} {
		t.Run(domain, func(t *testing.T) {
			c := good()
			c.Domain = domain
			findError(t, Validate(c, goodDNS(), goodDevices()), "domain", "reserved")
		})
	}
}

func TestValidateRefusesASingleLabelDomain(t *testing.T) {
	c := good()
	c.Domain = "router"
	findError(t, Validate(c, goodDNS(), goodDevices()), "domain", "single label")
}

// The one cross-module rule left. The resolver answers its own zone
// authoritatively, so a published name inside it is never asked of public DNS:
// the record exists, the Caddyfile is right, and every client on this network
// gets NXDOMAIN.
func TestValidateRefusesADomainInsideTheResolverZone(t *testing.T) {
	for _, domain := range []string{"home.example.com", "svc.home.example.com"} {
		t.Run(domain, func(t *testing.T) {
			c := good()
			c.Domain = domain
			r := Validate(c, StaticDNS{Domain: "home.example.com"}, goodDevices())
			findError(t, r, "domain", "would never be asked of your provider's DNS")
			if !strings.Contains(r.Errors[0].Message, "pub.example.com") {
				t.Fatalf("the refusal should suggest a sibling domain, got %q", r.Errors[0].Message)
			}
		})
	}
}

// The other direction is survivable but worth saying out loud: a wildcard for
// the parent also matches the resolver's zone apex.
func TestValidateWarnsWhenTheWildcardCoversTheResolverZone(t *testing.T) {
	c := good()
	c.Domain = "example.com"
	r := Validate(c, StaticDNS{Domain: "home.example.com"}, goodDevices())
	if !r.OK() {
		t.Fatalf("expected valid, got %v", r.Errors)
	}
	found := false
	for _, p := range r.Warnings {
		if p.Path == "domain" && strings.Contains(p.Message, "also matches home.example.com") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a warning about the wildcard covering the resolver zone, got %v", r.Warnings)
	}
}

func TestValidateSkipsDomainChecksWhenDisabled(t *testing.T) {
	c := good()
	c.Enabled = false
	c.Domain = ""
	// A disabled module must not hold the config invalid — the operator has to
	// be able to save an incomplete setup and come back to it.
	if r := Validate(c, goodDNS(), goodDevices()); !r.OK() {
		t.Fatalf("disabled config should validate, got %v", r.Errors)
	}
}

func TestValidateCertificate(t *testing.T) {
	tests := []struct {
		name string
		edit func(*Config)
		path string
		want string
	}{
		{"no provider", func(c *Config) { c.Certificate.Provider = "" }, "certificate.provider", "required"},
		{"no token", func(c *Config) { c.Certificate.Token = "" }, "certificate.provider_token", "required"},
		{"named resolver", func(c *Config) { c.Certificate.Resolvers = []string{"dns.example.com"} },
			"certificate.resolvers[0]", "not an IP address"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := good()
			tc.edit(&c)
			findError(t, Validate(c, goodDNS(), goodDevices()), tc.path, tc.want)
		})
	}
}

// The provider name is deliberately not checked here. Whether the binary has
// that module is a question about a file on disk, and this layer is pure — the
// real check is `caddy validate` on the rendered file, before it is applied.
func TestValidateDoesNotJudgeTheProviderName(t *testing.T) {
	c := good()
	c.Certificate.Provider = "something-only-their-build-has"
	if r := Validate(c, goodDNS(), goodDevices()); !r.OK() {
		t.Fatalf("expected valid, got %v", r.Errors)
	}
}

func TestValidateAcceptsResolversWithPorts(t *testing.T) {
	c := good()
	c.Certificate.Resolvers = []string{"1.1.1.1", "9.9.9.9:53", "[2606:4700:4700::1111]:53"}
	if r := Validate(c, goodDNS(), goodDevices()); !r.OK() {
		t.Fatalf("expected valid, got %v", r.Errors)
	}
}

// A nested name renders into a perfectly valid Caddyfile and then fails TLS at
// the first request, because a wildcard covers exactly one label.
func TestValidateRefusesANestedName(t *testing.T) {
	c := good()
	c.Services[0].Name = "grafana.lab"
	r := Validate(c, goodDNS(), goodDevices())
	findError(t, r, "services[0].name", "more than one label")
}

// A client and a published service may share a label now, because they are
// names in different zones: sony-tv.home.arpa is the resolver's, and
// sony-tv.<published domain> is the proxy's.
func TestValidateAllowsANameADSDevicesAlsoHas(t *testing.T) {
	c := good()
	c.Services[0].Name = "printer"
	dns := StaticDNS{Domain: "home.example.com"}
	if r := Validate(c, dns, goodDevices()); !r.OK() {
		t.Fatalf("separate namespaces should not collide, got %v", r.Errors)
	}
}

func TestValidateName(t *testing.T) {
	tests := []struct{ name, want string }{
		{"", "required"},
		{"*", "the wildcard is what serves all of them"},
		{"gra fana", "letters, digits and hyphens"},
		{"-grafana", "may not begin or end with a hyphen"},
		{strings.Repeat("a", 64), "63 characters"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := good()
			c.Services[0].Name = tc.name
			findError(t, Validate(c, goodDNS(), goodDevices()), "services[0].name", tc.want)
		})
	}
}

func TestValidateRefusesDuplicateNames(t *testing.T) {
	c := good()
	c.Services = append(c.Services, Service{Name: "grafana", Upstream: Upstream{Client: "nuc", Port: 9000}})
	findError(t, Validate(c, goodDNS(), goodDevices()), "services[1].name", "already published")
}

// docs/ingress.md §1.2: a dynamic address is refused, not warned about, because
// the config works right up until a lease turns over.
func TestValidateRefusesADeviceWithoutAFixedAddress(t *testing.T) {
	c := good()
	c.Services[0].Upstream.Client = "tv"
	r := Validate(c, goodDNS(), goodDevices())
	findError(t, r, "services[0].upstream.client", "no fixed address")
	findError(t, r, "services[0].upstream.client", "olr dhcp add reservation")
}

func TestValidateUpstream(t *testing.T) {
	tests := []struct {
		name string
		up   Upstream
		path string
		want string
	}{
		{"neither", Upstream{Port: 80}, "services[0].upstream", "either a client"},
		{"both", Upstream{Client: "nuc", Host: "10.0.0.1", Port: 80}, "services[0].upstream", "alternatives"},
		{"unknown client", Upstream{Client: "ghost", Port: 80}, "services[0].upstream.client", "no client named"},
		{"no port", Upstream{Client: "nuc"}, "services[0].upstream.port", "required"},
		{"bad scheme", Upstream{Client: "nuc", Port: 80, Scheme: "ftp"}, "services[0].upstream.scheme", "unknown scheme"},
		{"host with port", Upstream{Host: "10.0.0.1:80", Port: 80}, "services[0].upstream.host", "port belongs in the port field"},
		{"unspecified", Upstream{Host: "0.0.0.0", Port: 80}, "services[0].upstream.host", "not a destination"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := good()
			c.Services[0].Upstream = tc.up
			findError(t, Validate(c, goodDNS(), goodDevices()), tc.path, tc.want)
		})
	}
}

// A literal hostname works and reintroduces a dependency the client form
// removes, so it is a remark rather than a refusal.
func TestValidateWarnsOnAHostnameUpstream(t *testing.T) {
	c := good()
	c.Services[0].Upstream = Upstream{Host: "nas.internal", Port: 5000}
	r := Validate(c, goodDNS(), goodDevices())
	if !r.OK() {
		t.Fatalf("expected valid, got %v", r.Errors)
	}
	if len(r.Warnings) != 1 || !strings.Contains(r.Warnings[0].Message, "resolved at connection time") {
		t.Fatalf("expected one propagation warning, got %v", r.Warnings)
	}
}

func TestValidateWarnsWhenEnabledAndEmpty(t *testing.T) {
	c := good()
	c.Services = nil
	r := Validate(c, goodDNS(), goodDevices())
	if !r.OK() {
		t.Fatalf("expected valid, got %v", r.Errors)
	}
	if len(r.Warnings) != 1 || !strings.Contains(r.Warnings[0].Message, "nothing is published") {
		t.Fatalf("expected the empty-but-enabled warning, got %v", r.Warnings)
	}
}

func TestValidateRefusesEscapeHatchOverreach(t *testing.T) {
	c := good()
	c.ExtraConf = "# fine\ntls internal\n"
	findError(t, Validate(c, goodDNS(), goodDevices()), "raw_caddyfile", "which this module renders")
}

func TestValidateAllowsOrdinaryEscapeHatchContent(t *testing.T) {
	c := good()
	c.ExtraConf = "status.example.com {\n\trespond \"ok\"\n}\n"
	if r := Validate(c, goodDNS(), goodDevices()); !r.OK() {
		t.Fatalf("expected valid, got %v", r.Errors)
	}
}

func TestErrNamesTheModule(t *testing.T) {
	c := good()
	c.Certificate.Provider = ""
	err := Validate(c, goodDNS(), goodDevices()).Err()
	if err == nil || !strings.Contains(err.Error(), "invalid ingress configuration") {
		t.Fatalf("got %v", err)
	}
}

func TestValidateServiceLinkPath(t *testing.T) {
	for _, path := range []string{"/ui", "/ui/", "/app/v2"} {
		c := good()
		c.Services[0].LinkPath = path
		if r := Validate(c, goodDNS(), goodDevices()); !r.OK() {
			t.Errorf("%q: %v", path, r.Errors)
		}
	}
	for _, path := range []string{"ui", "//other.example/", "https://other.example/", "/ui?token=secret", "/ui?", "/ui#section", "/%2f%2fother", "/ui\\evil"} {
		c := good()
		c.Services[0].LinkPath = path
		r := Validate(c, goodDNS(), goodDevices())
		if r.OK() {
			t.Errorf("%q: expected invalid path", path)
		}
	}
}

func TestValidateRequestHeaders(t *testing.T) {
	for _, tc := range []struct {
		name    string
		headers []RequestHeader
		path    string
	}{
		{"empty name", []RequestHeader{{Name: ""}}, "name"},
		{"invalid name", []RequestHeader{{Name: "X Bad"}}, "name"},
		{"duplicate", []RequestHeader{{Name: "Host"}, {Name: "host"}}, "name"},
		{"newline", []RequestHeader{{Name: "X-Test", Value: "ok\r\nheader: bad"}}, "value"},
		{"remove with value", []RequestHeader{{Name: "X-Test", Remove: true, Value: "bad"}}, "value"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := good()
			c.Services[0].Upstream.RequestHeaders = tc.headers
			r := Validate(c, goodDNS(), goodDevices())
			if r.OK() || !strings.HasSuffix(r.Errors[0].Path, tc.path) {
				t.Fatalf("expected %s error, got %v", tc.path, r.Errors)
			}
		})
	}
}

func TestCloneKeepsRequestHeadersIndependent(t *testing.T) {
	original := good()
	original.Services[0].Upstream.RequestHeaders = []RequestHeader{{Name: "Host", Value: "192.168.1.50"}}
	copy := original.Clone()
	copy.Services[0].Upstream.RequestHeaders[0].Value = "another-host"
	if got := original.Services[0].Upstream.RequestHeaders[0].Value; got != "192.168.1.50" {
		t.Fatalf("clone changed stored header: %q", got)
	}
}

func TestValidateServiceIcon(t *testing.T) {
	for _, icon := range []string{"", "thesvg:grafana", "data:image/png;base64,iVBORw0KGgoAAA=="} {
		c := good()
		c.Services[0].Icon = icon
		if r := Validate(c, goodDNS(), goodDevices()); !r.OK() {
			t.Errorf("%q: %v", icon, r.Errors)
		}
	}
	for _, icon := range []string{"thesvg:../bad", "thesvg:", "https://elsewhere/icon.svg", "data:image/svg+xml;base64,PHN2Zz4=", "data:image/png;base64,bad"} {
		c := good()
		c.Services[0].Icon = icon
		if Validate(c, goodDNS(), goodDevices()).OK() {
			t.Errorf("accepted %q", icon)
		}
	}
}

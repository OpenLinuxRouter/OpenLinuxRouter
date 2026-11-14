package ingress

import (
	"strings"
	"testing"
)

func renderGood(t *testing.T, edit func(*Config)) Rendered {
	t.Helper()
	c := good()
	if edit != nil {
		edit(&c)
	}
	out, err := NewCaddy(RootedPaths(t.TempDir())).Render(c, goodDevices())
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	return out
}

func conf(t *testing.T, r Rendered) string {
	t.Helper()
	for _, f := range r.Files {
		if strings.HasSuffix(f.Path, "Caddyfile") {
			return string(f.Data)
		}
	}
	t.Fatal("no Caddyfile rendered")
	return ""
}

func envFile(t *testing.T, r Rendered) File {
	t.Helper()
	for _, f := range r.Files {
		if strings.HasSuffix(f.Path, "caddy.env") {
			return f
		}
	}
	t.Fatal("no env file rendered")
	return File{}
}

func TestRenderPublishesOneWildcardSite(t *testing.T) {
	got := conf(t, renderGood(t, nil))

	// One site block for the wildcard, not one per name — a block per name
	// would ask for a certificate per name and defeat the wildcard.
	if !strings.Contains(got, "*.svc.example.com {") {
		t.Errorf("expected a wildcard site block:\n%s", got)
	}
	if strings.Contains(got, "grafana.svc.example.com {") {
		t.Errorf("a per-name site block would request its own certificate:\n%s", got)
	}
	if !strings.Contains(got, "@svc_grafana {\n\t\thost grafana.svc.example.com\n\t\tremote_ip private_ranges\n\t}") {
		t.Errorf("expected a host matcher guarded by source:\n%s", got)
	}
	if !strings.Contains(got, "reverse_proxy 192.168.1.50:3000") {
		t.Errorf("expected the client's address resolved into the target:\n%s", got)
	}
}

// The subtlest line in the file: without it, issuance hangs forever on a box
// whose DNS is working exactly as designed.
func TestRenderAlwaysPinsACMEResolvers(t *testing.T) {
	got := conf(t, renderGood(t, nil))
	if !strings.Contains(got, "resolvers 1.1.1.1 8.8.8.8") {
		t.Fatalf("the propagation check must not use this box's own resolver:\n%s", got)
	}
}

func TestRenderHonoursExplicitResolvers(t *testing.T) {
	got := conf(t, renderGood(t, func(c *Config) {
		c.Certificate.Resolvers = []string{"8.8.8.8"}
	}))
	if !strings.Contains(got, "resolvers 8.8.8.8") {
		t.Fatalf("expected the configured resolvers:\n%s", got)
	}
}

// The credential must not be in the file an operator reads, diffs or pastes.
func TestRenderKeepsTheTokenOutOfTheCaddyfile(t *testing.T) {
	r := renderGood(t, func(c *Config) { c.Certificate.Token = "super-secret-value" })

	if got := conf(t, r); strings.Contains(got, "super-secret-value") {
		t.Fatalf("the token leaked into the Caddyfile:\n%s", got)
	} else if !strings.Contains(got, "{env."+TokenEnv+"}") {
		t.Fatalf("expected the environment indirection:\n%s", got)
	}

	env := envFile(t, r)
	if !strings.Contains(string(env.Data), TokenEnv+"=super-secret-value") {
		t.Fatalf("the token must reach the unit somehow: %s", env.Data)
	}
	if env.Mode != 0o600 {
		t.Errorf("env file mode = %v, want 0600", env.Mode)
	}
	if !env.Secret {
		t.Error("the env file must be marked secret, or a plan diff prints the token")
	}
}

func TestRenderClosesUnpublishedNames(t *testing.T) {
	got := conf(t, renderGood(t, nil))
	if !strings.Contains(got, "abort") {
		t.Fatalf("an unpublished name under the wildcard must not get a 200:\n%s", got)
	}
}

// The admin endpoint stays, because `caddy reload` is its client, but only on a
// unix socket. The default, localhost:2019, is reachable by every local user.
func TestRenderPutsTheAdminAPIOnASocketOnly(t *testing.T) {
	b := NewCaddy(RootedPaths(t.TempDir()))
	out, err := b.Render(good(), goodDevices())
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	got := conf(t, out)
	if strings.Contains(got, "admin off") {
		t.Fatalf("admin off leaves `caddy reload` with nothing to talk to (docs/ingress.md §7.2):\n%s", got)
	}
	if want := "admin unix/" + b.Paths.Admin; !strings.Contains(got, want) {
		t.Fatalf("want %q, so the admin API is a socket rather than a TCP port:\n%s", want, got)
	}
}

func TestRenderHTTPSUpstream(t *testing.T) {
	got := conf(t, renderGood(t, func(c *Config) {
		c.Services[0].Upstream.Scheme = SchemeHTTPS
	}))
	if !strings.Contains(got, "reverse_proxy https://192.168.1.50:3000") {
		t.Errorf("expected an https upstream:\n%s", got)
	}
	if !strings.Contains(got, "tls_insecure_skip_verify") {
		t.Errorf("the self-signed NAS is the whole reason this scheme exists:\n%s", got)
	}
}

func TestRenderHostUpstream(t *testing.T) {
	got := conf(t, renderGood(t, func(c *Config) {
		c.Services[0].Upstream = Upstream{Host: "127.0.0.1", Port: 3000}
	}))
	if !strings.Contains(got, "reverse_proxy 127.0.0.1:3000") {
		t.Fatalf("expected the literal host:\n%s", got)
	}
}

func TestRenderRequestHeaders(t *testing.T) {
	for _, scheme := range []Scheme{SchemeHTTP, SchemeHTTPS} {
		t.Run(string(scheme), func(t *testing.T) {
			got := conf(t, renderGood(t, func(c *Config) {
				c.Services[0].Upstream.Scheme = scheme
				c.Services[0].Upstream.RequestHeaders = []RequestHeader{
					{Name: "Host", Value: "192.168.1.50"},
					{Name: "X-Note", Value: "a b"},
					{Name: "X-Remove", Remove: true},
				}
			}))
			for _, want := range []string{`header_up Host "192.168.1.50"`, `header_up X-Note "a b"`, `header_up -X-Remove`} {
				if !strings.Contains(got, want) {
					t.Errorf("missing %q:\n%s", want, got)
				}
			}
		})
	}
}

func TestRenderQuotesHeaderValues(t *testing.T) {
	got := conf(t, renderGood(t, func(c *Config) {
		c.Services[0].Upstream.RequestHeaders = []RequestHeader{
			{Name: "X-Note", Value: `a "quote" \\ slash`},
			{Name: "X-Empty"},
		}
	}))
	for _, want := range []string{`header_up X-Note "a \"quote\" \\\\ slash"`, `header_up X-Empty ""`} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q:\n%s", want, got)
		}
	}
}

// A disabled module must render a file that parses. Otherwise it becomes the
// reason an unrelated change fails `caddy validate`.
func TestRenderDisabledIsStillValidSyntax(t *testing.T) {
	got := conf(t, renderGood(t, func(c *Config) { c.Enabled = false }))
	if strings.Contains(got, "reverse_proxy") {
		t.Fatalf("a disabled module must publish nothing:\n%s", got)
	}
	if strings.Count(got, "{") != strings.Count(got, "}") {
		t.Fatalf("unbalanced braces:\n%s", got)
	}
}

func TestRenderEscapeHatchIsAppended(t *testing.T) {
	got := conf(t, renderGood(t, func(c *Config) {
		c.ExtraConf = "status.example.com {\n\trespond \"ok\"\n}"
	}))
	if !strings.Contains(got, "respond \"ok\"") {
		t.Fatalf("expected the escape hatch verbatim:\n%s", got)
	}
}

func TestRenderCarriesAnOwnershipHeader(t *testing.T) {
	for _, f := range renderGood(t, nil).Files {
		if !strings.Contains(string(f.Data), "Generated by open-linux-router") {
			t.Errorf("%s has no ownership header", f.Path)
		}
	}
}

func TestRenderIsSortedAndStable(t *testing.T) {
	first := renderGood(t, nil)
	for i := 1; i < len(first.Files); i++ {
		if first.Files[i-1].Path >= first.Files[i].Path {
			t.Fatalf("files are not sorted: %v", first.Paths())
		}
	}
}

// Comments are most of this renderer's output. If rewording one counted as a
// change, every olr release would reload every deployed proxy — which means
// dropping every connection through it — for the sake of a sentence.
func TestCanonicalIgnoresComments(t *testing.T) {
	a := Canonical([]byte("# one\n\nfoo bar\n"))
	b := Canonical([]byte("# a different comment\nfoo bar\n\n# trailing\n"))
	if a != b {
		t.Fatalf("comments changed the canonical form:\n%q\n%q", a, b)
	}
	if a != "foo bar" {
		t.Fatalf("got %q", a)
	}
}

func TestCanonicalKeepsRealChanges(t *testing.T) {
	if Canonical([]byte("reverse_proxy 10.0.0.1:80")) == Canonical([]byte("reverse_proxy 10.0.0.2:80")) {
		t.Fatal("a changed upstream must not normalise away")
	}
}

func TestRedactedHidesTheToken(t *testing.T) {
	c := good()
	c.Certificate.Token = "super-secret-value"
	r := c.Redacted()
	if r.Certificate.Token != RedactedToken {
		t.Errorf("token = %q, want %q", r.Certificate.Token, RedactedToken)
	}
	if c.Certificate.Token != "super-secret-value" {
		t.Error("Redacted must not mutate its receiver")
	}
}

func TestNormalizeStripsTheDomainSuffix(t *testing.T) {
	c := Config{Domain: "home.example.com"}
	c.SetService(Service{Name: "Grafana.Home.Example.com", Upstream: Upstream{Host: "10.0.0.1", Port: 80}})
	c.SetService(Service{Name: "grafana", Upstream: Upstream{Host: "10.0.0.2", Port: 80}})

	if len(c.Services) != 1 {
		t.Fatalf("both spellings are one service, got %d: %v", len(c.Services), c.Services)
	}
	if c.Services[0].Upstream.Host != "10.0.0.2" {
		t.Errorf("the second write should have replaced the first, got %q", c.Services[0].Upstream.Host)
	}
}

func TestLinkPathDoesNotChangeProxyConfiguration(t *testing.T) {
	c := good()
	backend := NewCaddy(RootedPaths(t.TempDir()))
	before, err := backend.Render(c, goodDevices())
	if err != nil {
		t.Fatal(err)
	}
	c.Services[0].LinkPath = "/ui"
	after, err := backend.Render(c, goodDevices())
	if err != nil {
		t.Fatal(err)
	}
	if conf(t, before) != conf(t, after) {
		t.Fatal("link path changed the Caddyfile")
	}
}

// ---------------------------------------------------------------- exposure

func TestRenderPublishesExposedServicesOnTheirOwnSuffix(t *testing.T) {
	got := conf(t, renderGood(t, func(c *Config) {
		c.PublicDomain = "example.com"
		c.Services[0].Expose = ExposurePublic
	}))

	if !strings.Contains(got, "\n*.example.com {") {
		t.Fatalf("expected a public site block:\n%s", got)
	}
	if !strings.Contains(got, "@pub_grafana host grafana.example.com") {
		t.Fatalf("expected a public matcher:\n%s", got)
	}
	// The private name is still there, and still guarded: exposing a service
	// adds a name, it does not move one.
	if !strings.Contains(got, "\n\t@svc_grafana {\n\t\thost grafana.svc.example.com\n\t\tremote_ip private_ranges\n\t}") {
		t.Fatalf("expected the private name to stay guarded:\n%s", got)
	}

	public := got[strings.Index(got, "*.example.com {"):]
	if strings.Contains(public, "grafana.svc.example.com") {
		t.Fatalf("the public block must not carry the private name:\n%s", public)
	}
}

func TestRenderPublicSiteOnlyCarriesExposedServices(t *testing.T) {
	got := conf(t, renderGood(t, func(c *Config) {
		c.PublicDomain = "example.com"
		c.Services = append(c.Services, Service{
			Name: "nas", Upstream: Upstream{Client: "nuc", Port: 5000},
		})
		c.Services[0].Expose = ExposurePublic
	}))

	public := got[strings.Index(got, "*.example.com {"):]
	if strings.Contains(public, "nas.example.com") {
		t.Fatalf("a service that was not exposed is in the public block:\n%s", public)
	}
	if !strings.Contains(public, "grafana.example.com") {
		t.Fatalf("the exposed service is missing from the public block:\n%s", public)
	}
}

func TestRenderRestrictedServiceCarriesItsSources(t *testing.T) {
	got := conf(t, renderGood(t, func(c *Config) {
		c.PublicDomain = "example.com"
		c.Services[0].Expose = ExposureRestricted
		c.Services[0].AllowFrom = []string{"198.51.100.7", "203.0.113.0/24"}
	}))

	want := "\n\t@pub_grafana {\n\t\thost grafana.example.com\n\t\tremote_ip 198.51.100.7 203.0.113.0/24\n\t}"
	if !strings.Contains(got, want) {
		t.Fatalf("expected the source matcher:\n%s", got)
	}
}

func TestRenderOmitsThePublicSiteWhenNothingIsExposed(t *testing.T) {
	got := conf(t, renderGood(t, func(c *Config) {
		c.PublicDomain = "example.com"
	}))

	if strings.Contains(got, "*.example.com {") {
		t.Fatalf("a public site block with nothing in it would still request a certificate:\n%s", got)
	}
	if strings.Contains(got, "log_append") {
		t.Fatalf("no exposed service means no access log:\n%s", got)
	}
}

func TestRenderAccessLogDropsTheQueryString(t *testing.T) {
	got := conf(t, renderGood(t, func(c *Config) {
		c.PublicDomain = "example.com"
		c.Services[0].Expose = ExposurePublic
	}))

	for _, want := range []string{
		"output file ",
		"roll_size 10MiB",
		"request>uri delete",
		"log_append request_path {http.request.uri.path}",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("access log is missing %q:\n%s", want, got)
		}
	}
	// One logger, on the public block only. The private block is on someone's
	// LAN and its traffic is not this feature's business.
	if n := strings.Count(got, "log_append"); n != 1 {
		t.Fatalf("expected exactly one access log, got %d:\n%s", n, got)
	}
}

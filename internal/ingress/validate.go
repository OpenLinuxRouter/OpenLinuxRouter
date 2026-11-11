package ingress

import (
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"slices"
	"strconv"
	"strings"
)

// Validation is the highest-value mechanism in the apply story (design.md
// §5.3.1), and it carries more weight here than in most modules because of what
// a bad apply costs: a Caddyfile that Caddy rejects takes down *every*
// published service at once, not the one being edited (docs/ingress.md §7.1).
//
// It is pure — no files, no network, no root — so the whole rule set is
// table-tested and `olr ingress` can check a config on a laptop.

// Problem is one validation finding, addressed by a JSON-ish path so a UI can
// attach it to the field that caused it.
type Problem struct {
	Path    string
	Message string
}

func (p Problem) String() string {
	if p.Path == "" {
		return p.Message
	}
	return p.Path + ": " + p.Message
}

// Result separates the fatal from the merely suspect.
type Result struct {
	Errors   []Problem
	Warnings []Problem
}

func (r *Result) errorf(path, format string, args ...any) {
	r.Errors = append(r.Errors, Problem{Path: path, Message: fmt.Sprintf(format, args...)})
}

func (r *Result) warnf(path, format string, args ...any) {
	r.Warnings = append(r.Warnings, Problem{Path: path, Message: fmt.Sprintf(format, args...)})
}

// OK reports whether the config can be applied.
func (r Result) OK() bool { return len(r.Errors) == 0 }

// Err collapses the errors into one, or nil.
func (r Result) Err() error {
	if r.OK() {
		return nil
	}
	msgs := make([]error, len(r.Errors))
	for i, p := range r.Errors {
		msgs[i] = errors.New(p.String())
	}
	return fmt.Errorf("invalid ingress configuration:\n  %w", errors.Join(msgs...))
}

// unissuableSuffixes are domains no public CA will ever issue a certificate
// for, because nobody can prove they own them.
//
// This is the error an operator hits first and it has to explain itself, not
// just refuse. It is also the reason this module's domain is not `dns`'s:
// `home.arpa` is the resolver's default, and it can never hold a published
// service (docs/ingress.md §4).
var unissuableSuffixes = []string{
	"arpa", "local", "localhost", "internal", "lan", "home", "intranet",
	"private", "corp", "test", "invalid", "example", "onion",
}

// Validate checks a config against itself and against the two modules whose
// facts it depends on.
func Validate(c Config, dns DNSView, clients ClientView) Result {
	var r Result

	domain := normalizeDomain(c.Domain)
	validateDomain(&r, c, domain, dns)

	if c.Enabled && len(c.Services) == 0 {
		r.warnf("services", "ingress is enabled but nothing is published, so the proxy will answer every name with a closed connection")
	}

	validateCertificate(&r, c)
	validateServices(&r, c, domain, clients)
	validateExtraConf(&r, c.ExtraConf)

	return r
}

// validateDomain checks the suffix this module publishes under.
//
// The field is ours (Config.Domain), so most of these rules are about the
// certificate: a wildcard needs a registrable domain, and no CA issues for a
// reserved suffix. The one rule that still involves another module is the
// overlap, and it is worth the read: the resolver answers its own zone
// authoritatively, so a published domain inside it is a name that exists in
// your DNS provider's zone, in the Caddyfile, and nowhere a client can see.
func validateDomain(r *Result, c Config, domain string, dns DNSView) {
	if !c.Enabled {
		return
	}

	if domain == "" {
		r.errorf("domain", "required: the suffix published names live under, e.g. home.example.com. "+
			"The wildcard certificate is for *.<domain> and that is the zone you add records to — set it with `olr ingress set --domain`")
		return
	}

	if !strings.Contains(domain, ".") {
		r.errorf("domain", "%q is a single label; a certificate needs a registrable domain such as home.example.com", domain)
		return
	}

	// The check is on the final label rather than the whole suffix, because
	// `home.arpa`, `foo.arpa` and a bare `arpa` all fail for the same reason
	// and an operator who invented their own should get the same explanation.
	labels := strings.Split(domain, ".")
	if tld := labels[len(labels)-1]; slices.Contains(unissuableSuffixes, tld) {
		r.errorf("domain", "no certificate authority will issue for %q: .%s is reserved and cannot be proved to belong to anyone. "+
			"Publishing over HTTPS needs a domain you actually own — which is why this is a separate setting from the resolver's local domain, not the same one",
			domain, tld)
		return
	}

	local := ""
	if dns != nil {
		local = normalizeDomain(dns.LocalDomain())
	}
	switch {
	case local == "":
		// No resolver zone configured; nothing to overlap with.
	case domain == local || strings.HasSuffix(domain, "."+local):
		r.errorf("domain", "the resolver answers %s and everything under it, so a name published here would never be asked of your "+
			"provider's DNS: anyone using this network's resolver would get NXDOMAIN for a service that is otherwise working. "+
			"Publish under a domain beside it rather than inside it%s", qualify("*", local), besideSuffix(local))
	case strings.HasSuffix(local, "."+domain):
		r.warnf("domain", "the wildcard certificate for %s also matches %s, the zone the resolver answers for client names. "+
			"The two coexist, but a service named %q would sit exactly on top of that zone",
			qualify("*", domain), local, strings.SplitN(local, ".", 2)[0])
	}
}

// besideSuffix suggests a domain next to the resolver's zone rather than under
// it — the parent with one more label in front, which is the shape people
// reach for by hand (home.example.com → pub.example.com).
func besideSuffix(local string) string {
	if i := strings.Index(local, "."); i >= 0 && i+1 < len(local) {
		return " (for example " + "pub." + local[i+1:] + ")"
	}
	return ""
}

func validateCertificate(r *Result, c Config) {
	if !c.Enabled {
		return
	}

	// Presence only. Whether the name is one the proxy binary actually has is a
	// question about a file on disk, and Validate is pure (see the header) — so
	// it is asked where it can be answered, by `caddy validate` on the rendered
	// file before it is applied. Guessing here with a list of our own is what
	// providers.go exists to explain we no longer do.
	if c.Certificate.Provider == "" {
		r.errorf("certificate.provider", "required: the DNS provider hosting your domain, so the ACME challenge record can be written (see `olr ingress show providers`)")
	}

	if c.Certificate.Token == "" {
		r.errorf("certificate.provider_token", "required: an API credential for the DNS provider. Scope it to the single zone if the provider allows it")
	}

	for i, s := range c.Certificate.Resolvers {
		path := fmt.Sprintf("certificate.resolvers[%d]", i)
		if _, err := netip.ParseAddr(s); err == nil {
			continue
		}
		if host, port, err := splitHostPort(s); err == nil {
			if _, err := netip.ParseAddr(host); err == nil && port != "" {
				continue
			}
		}
		r.errorf(path, "%q is not an IP address; the propagation check must not depend on name resolution it is trying to establish", s)
	}
}

func validateServices(r *Result, c Config, domain string, clients ClientView) {
	seen := map[string]int{}
	for i, s := range c.Services {
		path := fmt.Sprintf("services[%d]", i)

		if s.Name == "" {
			r.errorf(path+".name", "required")
			continue
		}
		if first, dup := seen[s.Name]; dup {
			r.errorf(path+".name", "%q is already published at services[%d]", s.Name, first)
			continue
		}
		seen[s.Name] = i

		validateName(r, path, s.Name, domain)

		if err := ValidateCustomIcon(s.Icon); err != nil {
			r.errorf(path+".icon", "%v", err)
		}
		if s.LinkPath != "" {
			parsed, err := url.Parse(s.LinkPath)
			if err != nil || !strings.HasPrefix(s.LinkPath, "/") || strings.HasPrefix(s.LinkPath, "//") ||
				parsed.IsAbs() || parsed.Host != "" || parsed.User != nil || parsed.Fragment != "" ||
				parsed.ForceQuery || parsed.RawQuery != "" || parsed.EscapedPath() != s.LinkPath || strings.Contains(s.LinkPath, "%") || strings.Contains(s.LinkPath, "\\") {
				r.errorf(path+".link_path", "use a local path such as /ui, without a query or fragment")
			}
		}
		validateUpstream(r, path+".upstream", s.Upstream, clients)
	}
}

// validateName enforces what the wildcard certificate can actually cover.
//
// The subtle half is the dot. One certificate for `*.home.example.com` is what
// makes publishing a service free of any certificate step at all (§1), and a
// wildcard matches **exactly one label**: it covers `grafana.home.example.com`
// and does not cover `grafana.lab.home.example.com`. A nested name would
// therefore render into a perfectly valid Caddyfile and then fail TLS at the
// first request, which is a long way from where the mistake was made.
func validateName(r *Result, path, name, domain string) {
	if strings.Contains(name, ".") {
		r.errorf(path+".name", "%q has more than one label; the wildcard certificate covers %s and nothing deeper, "+
			"so a nested name would serve a certificate no browser accepts", name, qualify("*", domain))
		return
	}
	if name == "*" {
		r.errorf(path+".name", "a service needs its own name; the wildcard is what serves all of them")
		return
	}
	if len(name) > 63 {
		r.errorf(path+".name", "a DNS label may not exceed 63 characters")
	}
	for _, c := range name {
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' {
			continue
		}
		r.errorf(path+".name", "%q contains %q; a name may use letters, digits and hyphens", name, string(c))
		return
	}
	if strings.HasPrefix(name, "-") || strings.HasSuffix(name, "-") {
		r.errorf(path+".name", "%q may not begin or end with a hyphen", name)
	}
}

func validateUpstream(r *Result, path string, u Upstream, clients ClientView) {
	switch {
	case u.Client == "" && u.Host == "":
		r.errorf(path, "required: either a client to publish, or a literal host for something that is not a client")
	case u.Client != "" && u.Host != "":
		r.errorf(path, "client and host are alternatives; set one")
	case u.Client != "":
		validateDeviceUpstream(r, path, u, clients)
	default:
		validateHostUpstream(r, path, u)
	}

	if u.Port == 0 {
		r.errorf(path+".port", "required: a client has an address, never a port somebody meant")
	}
	if !u.Scheme.Valid() {
		r.errorf(path+".scheme", "unknown scheme %q (want %v)", u.Scheme, Schemes())
	}
	seen := map[string]bool{}
	for i, h := range u.RequestHeaders {
		p := fmt.Sprintf("%s.request_headers[%d]", path, i)
		if !validHeaderName(h.Name) {
			r.errorf(p+".name", "use a nonempty HTTP header name (letters, digits, or RFC 9110 token punctuation)")
		}
		key := strings.ToLower(h.Name)
		if seen[key] {
			r.errorf(p+".name", "duplicate header %q", h.Name)
		}
		seen[key] = true
		if h.Remove && h.Value != "" {
			r.errorf(p+".value", "a removed header cannot also have a value")
		}
		for _, c := range h.Value {
			if c < 0x20 || c == 0x7f {
				r.errorf(p+".value", "control characters are not allowed in a header value")
				break
			}
		}
	}
}

func validHeaderName(name string) bool {
	if name == "" {
		return false
	}
	for _, c := range name {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || strings.ContainsRune("!#$%&'*+-.^_`|~", c) {
			continue
		}
		return false
	}
	return true
}

// validateDeviceUpstream is where docs/ingress.md §1.2 is enforced.
//
// A client holding a dynamic address is refused rather than warned about,
// which is a stronger stance than this codebase usually takes. The reason is
// what the failure looks like: the config keeps working for days or weeks, and
// then a lease turns over and the published name proxies to whatever machine
// took the address — a guest's phone, with someone's Grafana on the other side
// of it. A warning is not proportionate to landing there by default.
//
// The refusal carries the remedy, per §5.6: olr does not pin the address on the
// operator's behalf, because reserving an address is a change to another
// module's config and inferring it is exactly what that rule forbids.
func validateDeviceUpstream(r *Result, path string, u Upstream, clients ClientView) {
	info, err := clients.Client(u.Client)
	if errors.Is(err, ErrNoSuchClient) {
		r.errorf(path+".client", "no client named %q; `olr clients list` shows what this network knows about", u.Client)
		return
	}
	if err != nil {
		r.errorf(path+".client", "%v", err)
		return
	}

	if !info.Fixed {
		r.errorf(path+".client", "%q has no fixed address, so publishing it would proxy to whatever machine holds its address at the time. "+
			"Give it one first: `olr dhcp add reservation --client %s`", u.Client, u.Client)
		return
	}
	if !info.Addr.IsValid() {
		r.errorf(path+".client", "%q has a fixed address reserved but has never been seen on the network", u.Client)
	}
}

func validateHostUpstream(r *Result, path string, u Upstream) {
	host := u.Host
	if addr, err := netip.ParseAddr(host); err == nil {
		if addr.IsUnspecified() {
			r.errorf(path+".host", "%q is not a destination", host)
		}
		return
	}
	if strings.ContainsAny(host, " \t/:") {
		r.errorf(path+".host", "%q is neither an address nor a hostname; the port belongs in the port field", host)
		return
	}
	// A bare hostname is allowed and resolved by Caddy at dial time. Worth a
	// remark rather than a refusal: it works, and it reintroduces the
	// dependency on name resolution that using a client was meant to remove.
	r.warnf(path+".host", "%q is resolved at connection time, so this service depends on that name continuing to resolve; a client is the stabler form", host)
}

// ownedDirectives are the Caddyfile directives this module renders itself.
//
// The escape hatch is additive by design (design.md §3.2 rule 5). Letting it
// restate one of these would let the rendered file contradict the config that
// produced it, which defeats the single-source rule the hatch exists to
// preserve. The check is textual and therefore approximate — Caddyfile is
// nested and we are not parsing it — so it errs toward allowing, and the
// authoritative check is `caddy validate` before apply (§7.1).
var ownedDirectives = map[string]string{
	"tls":   "certificates come from the certificate section, so that one wildcard covers every published name",
	"email": "set from certificate.acme_email",
}

func validateExtraConf(r *Result, extra string) {
	if strings.TrimSpace(extra) == "" {
		return
	}
	for i, line := range strings.Split(extra, "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) == 0 || strings.HasPrefix(fields[0], "#") {
			continue
		}
		if why, owned := ownedDirectives[fields[0]]; owned {
			r.errorf("raw_caddyfile", "line %d sets %q, which this module renders: %s", i+1, fields[0], why)
		}
	}
}

// splitHostPort is net.SplitHostPort without pulling in net for one call, and
// without its acceptance of an empty port.
func splitHostPort(s string) (host, port string, err error) {
	i := strings.LastIndex(s, ":")
	if i < 0 {
		return "", "", errors.New("no port")
	}
	host, port = strings.TrimSuffix(strings.TrimPrefix(s[:i], "["), "]"), s[i+1:]
	if _, err := strconv.ParseUint(port, 10, 16); err != nil {
		return "", "", err
	}
	return host, port, nil
}

// qualify renders a stored name as the operator sees it.
func qualify(name, domain string) string {
	if domain == "" {
		return name
	}
	return name + "." + domain
}

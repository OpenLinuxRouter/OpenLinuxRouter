package ingress

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// ModuleName is the path segment, config section and event label for this
// module.
//
// Neither `proxy` nor `gateway` was available: docs/gateway.md uses both for
// traffic on its way *out* — an exit, and the operator's proxy box. This
// module is the other direction, so it needed a third word.
const ModuleName = "ingress"

// Config is the ingress module's intent: which internal services are reachable
// by name, and what it takes to serve them over HTTPS.
//
// It is the single source for the CLI flags, the REST body, the UI form and the
// MCP tool definition (design.md §3.2 rule 3).
//
// The domain is here, and it is this module's own fact. It was `dns.LocalDomain`
// for a while, on the theory that one namespace for the whole box could not
// drift; what that actually bought was a published name that only existed if
// this module also wrote it into the resolver, and a network-wide rename forced
// on anybody who wanted one service over HTTPS (docs/ingress.md §3). It is not
// the resolver's domain any more: the records live in public DNS, where the
// operator puts them, and what the resolver answers is a separate question.
//
// Which leaves exactly one thing this module must know about the resolver: a
// published domain that falls *inside* the zone the resolver answers
// authoritatively would never be asked of public DNS at all, so validate.go
// refuses it. That read is the whole of the coupling.
type Config struct {
	// Enabled controls whether the proxy runs at all. Disabling stops the
	// service and leaves the configuration in place, so published services can
	// be turned back on without retyping them.
	Enabled bool `json:"enabled"`

	// Legacy records that a stored document still named its upstream's client
	// `client`, the word this module used before the two inventories were told
	// apart. Never serialised, so one save writes the current shape.
	Legacy bool `json:"-"`

	// Domain is the suffix published names live under, e.g. `home.example.com`.
	//
	// Owned here rather than by `dns`, and deliberately not the same value: the
	// wildcard certificate is for `*.<this>`, the records the operator writes
	// point at this box, and neither has anything to do with what the resolver
	// calls the clients in the house.
	//
	// One label, though: the wildcard covers `grafana.<domain>` and not
	// `grafana.lab.<domain>`, so a deeper suffix would only move the problem.
	Domain string `json:"domain,omitempty"`

	// Certificate is how the wildcard is obtained. One certificate serves every
	// published name, which is what makes adding a service a two-field
	// operation (docs/ingress.md §1).
	Certificate Certificate `json:"certificate"`

	// Services are the published names, keyed by Name.
	Services []Service `json:"services,omitempty"`

	// ExtraConf is the module's declared escape hatch (design.md §3.2 rule 5):
	// appended verbatim to the rendered Caddyfile. Caddy can do vastly more
	// than publish an internal service, and this field plus the upstream
	// documentation is the whole of our answer to all of it.
	ExtraConf string `json:"raw_caddyfile,omitempty"`
}

// Certificate holds what ACME needs and nothing else.
//
// DNS-01 is the only challenge that works for a name which does not resolve
// from outside, and docs/ingress.md §4 records why the other two are dead:
// HTTP-01 would mean exposing the router to get a certificate for something
// that is not exposed, and a local CA would mean installing a root certificate
// on every phone, TV and console on the network.
type Certificate struct {
	// Provider names the DNS provider whose API Caddy writes the challenge
	// record through. The vocabulary is Caddy's, not ours — see schema.go.
	Provider string `json:"provider,omitempty"`

	// Token is the provider's API credential.
	//
	// This is the first credential for a third-party account olr has ever
	// held, and there is no secrets store to put it in (docs/ingress.md §4.1).
	// It therefore lives in the config document like any other field, with two
	// obligations that the rest of this module has to keep:
	//
	//   - it is redacted on every surface that prints config or a plan diff,
	//     because `olr ingress plan` showing an operator their own API token in
	//     a terminal is how tokens end up in bug reports; and
	//   - it is rendered into an environment file the unit reads, never into
	//     the Caddyfile, so that the file we might reasonably show somebody
	//     never contains it in the first place.
	//
	// Scope it to the one zone if the provider supports it. The setup copy
	// should say so, because the blast radius of an account-wide key here is
	// the operator's whole DNS.
	Token string `json:"provider_token,omitempty"`

	// Email is the ACME account contact. Optional; Let's Encrypt uses it only
	// to warn about expiry, which is a warning we would rather they received.
	Email string `json:"acme_email,omitempty"`

	// Resolvers are the DNS servers Caddy uses to confirm the challenge record
	// has propagated.
	//
	// They are public resolvers by default, and the reason is narrower than it
	// used to be. This box's own resolver is not authoritative for the
	// published domain any more (§3), so it *can* see the `_acme-challenge`
	// record — but it also applies this network's blocking policies and
	// whatever the upstream does, and the one lookup that must not be
	// mistaken for a failure is the one that decides whether a certificate is
	// issued. Asking two resolvers that are not ours keeps that question away
	// from every filter we render.
	Resolvers []string `json:"resolvers,omitempty"`
}

// DefaultResolvers are public resolvers used only for ACME propagation checks.
//
// Two, from different operators, because this is the one lookup on the box that
// must not depend on our own resolver (see Certificate.Resolvers).
//
// **Two is not redundancy**, which is the opposite of what this comment used to
// claim. Caddy's propagation check asks every listed resolver in turn, and an
// unreachable one fails the check outright: a real box behind an ISP that drops
// traffic to 9.9.9.9 logged `dial tcp 9.9.9.9:53: i/o timeout` on every attempt
// and never obtained a certificate, while 1.1.1.1 beside it answered fine. So
// the list is chosen for being reachable from the most networks, not for
// diversity, and an operator on a network that blocks one of these replaces it
// in `resolvers` rather than adding to it.
//
// This is not a privacy leak worth worrying about: the only names asked are
// `_acme-challenge` records the operator is simultaneously publishing to the
// public DNS, and the CA is about to query them from the outside anyway.
var DefaultResolvers = []string{"1.1.1.1", "8.8.8.8"}

// ResolversOrDefault resolves the empty case.
func (c Certificate) ResolversOrDefault() []string {
	if len(c.Resolvers) == 0 {
		return slices.Clone(DefaultResolvers)
	}
	return slices.Clone(c.Resolvers)
}

// Service is one published name.
//
// Two routing fields, and that is the module's entire claim: the certificate,
// the proxy stanza and the `:80` redirect are all derived from things
// established once when the module was enabled (docs/ingress.md §1). The DNS
// record is the operator's, in their own zone — §3.
type Service struct {
	// Name is one label, relative to Config.Domain: "grafana", not
	// "grafana.home.example.com". Normalize strips the suffix if it was typed,
	// so both spellings are one entry rather than two that diff against each
	// other forever.
	Name string `json:"name"`

	// Upstream is where a request for this name is sent.
	Upstream Upstream `json:"upstream"`

	// LinkPath is the optional page opened from the UI, not a proxy rewrite.
	// A dashboard under /ui still receives requests through the same host.
	LinkPath string `json:"link_path,omitempty"`

	// Icon overrides automatic favicon discovery; empty uses the site icon.
	Icon string `json:"icon,omitempty"`
}

// Upstream is the target of a published service.
//
// Client and Host are alternatives and exactly one must be set. The pair exists
// because most targets are clients this network already knows by name, and some
// are not: a container on the router itself, or something behind another
// router.
type Upstream struct {
	// Client identifies a client by MAC in the `clients` module (design.md §4.4).
	// Legacy names and CLI names are resolved to MACs when stored. It is the
	// preferred form and the reason is §4.1: a client's address belongs to
	// `clients` and `dhcp`, so referencing the client reads it per request
	// instead of copying it here where it would rot the next time the lease
	// changed.
	//
	// Choosing a client that has no fixed address is refused rather than
	// rendered — see validate.go, and docs/ingress.md §1.2.
	Client string `json:"client,omitempty"`

	// Host is a literal address or hostname, for targets that are not clients.
	Host string `json:"host,omitempty"`

	// Port is the upstream's port. Required in both forms: a client knows its
	// address, never which of its ports somebody meant.
	Port uint16 `json:"port"`

	// Scheme is how to speak to the upstream. Empty means SchemeHTTP.
	//
	// SchemeHTTPS exists for the NAS and hypervisor UIs that only listen on
	// HTTPS with a self-signed certificate. Verification is deliberately not
	// attempted for those (render.go) — the connection is over the operator's
	// own LAN to a client they named, and demanding a valid certificate there
	// would make the common case impossible while protecting against an
	// attacker who is already inside the network.
	Scheme Scheme `json:"scheme,omitempty"`

	// RequestHeaders change the request sent to this upstream, not the response
	// sent to the browser. An empty list preserves Caddy's default headers.
	RequestHeaders []RequestHeader `json:"request_headers,omitempty"`
}

// RequestHeader sets a header (including an empty value), or removes it.
type RequestHeader struct {
	Name   string `json:"name"`
	Value  string `json:"value,omitempty"`
	Remove bool   `json:"remove,omitempty"`
}

// Target renders the upstream as Caddy's dial address, given a resolved
// address for the client form.
func (u Upstream) Target(resolved string) string {
	host := u.Host
	if u.Client != "" {
		host = resolved
	}
	return fmt.Sprintf("%s:%d", host, u.Port)
}

// Scheme is how olr speaks to an upstream.
type Scheme string

// The upstream schemes.
const (
	SchemeHTTP  Scheme = "http"
	SchemeHTTPS Scheme = "https"
)

// Schemes lists the vocabulary. One source, so the enum published in the schema
// and the check in the validator cannot disagree.
func Schemes() []Scheme { return []Scheme{SchemeHTTP, SchemeHTTPS} }

// Valid reports whether s is a known scheme. The empty string is valid and
// means SchemeHTTP.
func (s Scheme) Valid() bool { return s == "" || slices.Contains(Schemes(), s) }

// OrDefault resolves the empty value.
func (s Scheme) OrDefault() Scheme {
	if s == "" {
		return SchemeHTTP
	}
	return s
}

// Service returns a published service by name, given relative or in full.
func (c Config) Service(name string) (Service, bool) {
	name = normalizeName(name, c.Domain)
	i := slices.IndexFunc(c.Services, func(s Service) bool { return s.Name == name })
	if i < 0 {
		return Service{}, false
	}
	return c.Services[i], true
}

// SetService adds or replaces a published service, keyed by name.
func (c *Config) SetService(s Service) {
	s.Name = normalizeName(s.Name, c.Domain)
	if i := slices.IndexFunc(c.Services, func(e Service) bool { return e.Name == s.Name }); i >= 0 {
		c.Services[i] = s
		c.Normalize()
		return
	}
	c.Services = append(c.Services, s)
	c.Normalize()
}

// RemoveService drops a published service, reporting whether there was one.
func (c *Config) RemoveService(name string) bool {
	name = normalizeName(name, c.Domain)
	i := slices.IndexFunc(c.Services, func(s Service) bool { return s.Name == name })
	if i < 0 {
		return false
	}
	c.Services = slices.Delete(c.Services, i, i+1)
	return true
}

// normalizeName reduces the two spellings of one name to the stored one.
//
// The operator may type "grafana" or "grafana.home.example.com" and mean the
// same service. Storing whichever they typed would let both exist at once, and
// then the rendered Caddyfile would carry two matchers for one hostname with
// the second silently unreachable.
func normalizeName(name, domain string) string {
	name = normalizeDomain(name)
	if domain = normalizeDomain(domain); domain != "" {
		name = strings.TrimSuffix(name, "."+domain)
	}
	return name
}

// normalizeDomain is the canonical spelling of a suffix: lower case, no
// whitespace, no trailing dot. Empty stays empty, which is "not set".
func normalizeDomain(domain string) string {
	return strings.TrimSuffix(strings.TrimSpace(strings.ToLower(domain)), ".")
}

// Normalize puts the config in its canonical form.
//
// Sorting is not cosmetic: the rendered Caddyfile is compared against what is
// on disk to detect drift (§5.4), so a config that reorders itself between two
// identical edits would report a change that is not one.
func (c *Config) Normalize() {
	// Before the services, whose names are stored relative to it.
	c.Domain = normalizeDomain(c.Domain)

	for i := range c.Services {
		s := &c.Services[i]
		s.Name = strings.TrimSpace(strings.ToLower(s.Name))
		s.LinkPath = strings.TrimSpace(s.LinkPath)
		s.Upstream.Host = strings.TrimSpace(s.Upstream.Host)
		s.Upstream.Client = strings.TrimSpace(s.Upstream.Client)
		for j := range s.Upstream.RequestHeaders {
			s.Upstream.RequestHeaders[j].Name = strings.TrimSpace(s.Upstream.RequestHeaders[j].Name)
		}
	}
	slices.SortStableFunc(c.Services, func(a, b Service) int {
		return strings.Compare(a.Name, b.Name)
	})

	c.Certificate.Provider = strings.TrimSpace(strings.ToLower(c.Certificate.Provider))
	c.Certificate.Email = strings.TrimSpace(c.Certificate.Email)
	// The token is not trimmed or cased. It is an opaque credential and
	// "helpfully" altering it would produce an authentication failure whose
	// cause is invisible in every diff, because the diff would look right.
}

// Clone returns a deep copy, so a caller may edit a config without disturbing
// the one held by the store.
func (c Config) Clone() Config {
	out := c
	out.Services = slices.Clone(c.Services)
	for i := range out.Services {
		out.Services[i].Upstream.RequestHeaders = slices.Clone(c.Services[i].Upstream.RequestHeaders)
	}
	out.Certificate.Resolvers = slices.Clone(c.Certificate.Resolvers)
	return out
}

// Redacted returns a copy safe to print, log, or return over the API.
//
// Used by every surface rather than by the ones that looked risky, because the
// failure mode is silent: nothing breaks when a token is printed, it just ends
// up in a scrollback buffer and then in a bug report.
func (c Config) Redacted() Config {
	out := c.Clone()
	if out.Certificate.Token != "" {
		out.Certificate.Token = RedactedToken
	}
	return out
}

// RedactedToken is what stands in for a credential on a printed surface. A
// fixed string rather than a length-preserving mask, so it cannot be mistaken
// for the real value and gives away nothing about it.
const RedactedToken = "********"

// MarshalConfig encodes a config for the store, normalising first so that two
// equivalent configs produce identical bytes.
func MarshalConfig(c Config) ([]byte, error) {
	c.Normalize()
	return json.Marshal(c)
}

// UnmarshalConfig parses a config, rejecting unknown fields.
//
// Strictness is deliberate: a typo'd key that is silently ignored produces a
// router that is quietly not doing what its config says. Here that means a
// service the operator believes is published and is not, or — worse — a
// `provider_token` misspelled into oblivion while the old credential keeps
// working until it is rotated.
func UnmarshalConfig(data []byte) (Config, error) {
	data, legacy, err := renameLegacyClients(data)
	if err != nil {
		return Config{}, fmt.Errorf("%s configuration: %w", ModuleName, err)
	}
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	var c Config
	if err := dec.Decode(&c); err != nil {
		return Config{}, fmt.Errorf("%s configuration: %w", ModuleName, err)
	}
	c.Legacy = legacy
	c.Normalize()
	return c, nil
}

// renameLegacyClients rewrites the pre-rename `device` upstream key to
// `client`. The upstream is the only object in this subtree that ever had that
// key, so a walk of the whole document is safe and keeps the strict decoder
// below doing its job.
func renameLegacyClients(data []byte) ([]byte, bool, error) {
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		return data, false, nil
	}
	if !renameKey(v, "device", "client") {
		return data, false, nil
	}
	out, err := json.Marshal(v)
	return out, true, err
}

// renameKey renames an object key everywhere in a decoded JSON value, leaving
// an existing new key alone.
func renameKey(v any, from, to string) bool {
	switch t := v.(type) {
	case map[string]any:
		changed := false
		if value, ok := t[from]; ok {
			if _, taken := t[to]; !taken {
				t[to] = value
				delete(t, from)
				changed = true
			}
		}
		for _, child := range t {
			if renameKey(child, from, to) {
				changed = true
			}
		}
		return changed
	case []any:
		changed := false
		for _, child := range t {
			if renameKey(child, from, to) {
				changed = true
			}
		}
		return changed
	}
	return false
}

// FromDocument reads this module's section out of the store's document.
func FromDocument(d core.Document) (Config, error) {
	raw, ok := d.Raw(ModuleName)
	if !ok {
		return Config{}, nil
	}
	var c Config
	if err := json.Unmarshal(raw, &c); err != nil {
		return Config{}, fmt.Errorf("%s configuration: %w", ModuleName, err)
	}
	c.Normalize()
	return c, nil
}

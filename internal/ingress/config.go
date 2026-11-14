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

	// PublicDomain is the suffix a service is reachable at from the internet.
	//
	// Empty means nothing is exposed: every published name answers only on this
	// network, and the rendered proxy has no public site block at all. Setting
	// it is what makes the per-service Expose field mean anything — it names the
	// zone the second wildcard certificate is for, and the zone the operator
	// adds records in.
	//
	// It is deliberately a *different* suffix from Domain, and the reason is the
	// one thing DNS cannot do here: a name that must answer 172.16.1.1 inside
	// the house and a public address outside is two answers for one name, and
	// this module owns neither the resolver nor the operator's public zone. Two
	// suffixes give the two answers without split-horizon DNS, and keep the
	// private names out of public DNS entirely — which is what makes the
	// exposure decision fail closed at the name layer as well as at the proxy.
	PublicDomain string `json:"public_domain,omitempty"`

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

	// Expose says who may reach this service. Empty means ExposureLAN, which
	// is what every service was before this field existed and stays the default
	// because a name that becomes public without being asked is the one outcome
	// this module must never produce.
	Expose Exposure `json:"expose,omitempty"`

	// AllowFrom lists the sources a restricted service accepts, as CIDRs or
	// single addresses.
	//
	// Only meaningful with ExposureRestricted, and the validator refuses it
	// anywhere else rather than ignoring it: a list the operator wrote that
	// silently does nothing is the failure this module spends its validation
	// budget refusing.
	AllowFrom []string `json:"allow_from,omitempty"`
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

// Exposure is who may reach a published service.
//
// Three states, not two, because "public" and "public to these addresses" are
// different promises and the middle one is what an operator actually wants when
// they are worried about scanning: the firewall can drop every other source, so
// the port looks closed to the world while the service works for the people it
// is for.
type Exposure string

const (
	// ExposureLAN is reachable only from this network's addresses — the LAN,
	// the dial-in tunnel and the box itself. It is the zero value and the
	// default.
	ExposureLAN Exposure = "lan"

	// ExposurePublic is reachable by anyone on the internet at
	// `<name>.<public_domain>`.
	ExposurePublic Exposure = "public"

	// ExposureRestricted is published like ExposurePublic but only answers the
	// sources in AllowFrom.
	//
	// At the proxy this is a host matcher plus `remote_ip`; the firewall still
	// has to open the port for everyone, because nftables cannot see a Host
	// header. That is why the allow-list is enforced twice and why this state
	// is not called "private".
	ExposureRestricted Exposure = "restricted"
)

// Exposures lists the vocabulary, one source for the schema and the validator.
func Exposures() []Exposure { return []Exposure{ExposureLAN, ExposurePublic, ExposureRestricted} }

// Valid reports whether e is a known exposure. The empty string is valid and
// means ExposureLAN.
func (e Exposure) Valid() bool { return e == "" || slices.Contains(Exposures(), e) }

// OrDefault resolves the empty value.
func (e Exposure) OrDefault() Exposure {
	if e == "" {
		return ExposureLAN
	}
	return e
}

// IsPublic reports whether the service answers outside this network at all.
func (e Exposure) IsPublic() bool { return e.OrDefault() != ExposureLAN }

// OrDefault resolves the empty value.
func (s Scheme) OrDefault() Scheme {
	if s == "" {
		return SchemeHTTP
	}
	return s
}

// HasExposed reports whether any service is reachable from the internet.
//
// The firewall and the renderer both ask this rather than each deciding what
// "exposed" means: one definition, so a box cannot open a port for a site block
// that was never rendered.
func (c Config) HasExposed() bool {
	for _, s := range c.Services {
		if s.Expose.IsPublic() {
			return true
		}
	}
	return false
}

// ExposedCount is how many services answer outside this network.
func (c Config) ExposedCount() int {
	n := 0
	for _, s := range c.Services {
		if s.Expose.IsPublic() {
			n++
		}
	}
	return n
}

// normalizeSources trims, drops empties, sorts and de-duplicates.
//
// Sorting is not cosmetic: allow-list order reaches the rendered Caddyfile, and
// the file is compared byte for byte for drift (docs/ingress.md §7.2a), so a
// list that reorders itself between two identical edits would mark every box
// drifted and reload the proxy for nothing.
func normalizeSources(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
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
	c.PublicDomain = normalizeDomain(c.PublicDomain)

	for i := range c.Services {
		s := &c.Services[i]
		s.Name = strings.TrimSpace(strings.ToLower(s.Name))
		s.LinkPath = strings.TrimSpace(s.LinkPath)
		s.Expose = Exposure(strings.ToLower(strings.TrimSpace(string(s.Expose))))
		// "lan" is what a UI sends for the default; the stored form is empty so
		// that a document written before this field existed and one written
		// after are the same bytes.
		if s.Expose == ExposureLAN {
			s.Expose = ""
		}
		s.AllowFrom = normalizeSources(s.AllowFrom)
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
		out.Services[i].AllowFrom = slices.Clone(c.Services[i].AllowFrom)
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

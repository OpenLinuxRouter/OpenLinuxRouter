package dns

import (
	"errors"
	"fmt"
	"net/netip"
	"slices"
	"strings"
)

// Validation is the highest-value mechanism in the whole apply story
// (design.md §5.3.1). There is no cross-module transaction, so nothing unwinds
// a bad change — but atomic *validation* is cheap where atomic *apply* is not,
// because validation is pure reads and needs no coordination.
//
// For this module the stakes are unusually lopsided. A bad pool costs one
// client an address; a bad resolver costs the whole building the internet, and
// presents to every occupant not as "DNS is down" but as "everything is
// broken". So the rules below lean towards refusing rather than warning
// wherever the failure would be silent or house-wide.

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
	return errors.Join(msgs...)
}

// MaxPolicyNameLen bounds a policy name. It becomes a filename, and it is a
// label in a UI table, so it is short by design rather than by accident.
const MaxPolicyNameLen = 64

// Validate checks a config against itself and against the link module's view of
// the interfaces it names.
//
// It is pure: no files, no netlink, no root. That is what lets the entire rule
// set be table-tested and lets `olr dns` check a config on a laptop.
func Validate(c Config, links LinkView, reservations ReservationView) Result {
	var r Result

	validateListen(&r, c, links)
	validateUpstream(&r, c, links)
	validateHosts(&r, c, links, reservations)
	validatePrivateDomains(&r, c)
	validatePolicies(&r, c)
	validateHijack(&r, c, links)
	validateQueryLog(&r, c)
	validateExtraConf(&r, c.ExtraConf)

	return r
}

// validateListen checks where the relay would answer.
func validateListen(r *Result, c Config, links LinkView) {
	if len(c.Listen) == 0 {
		// Empty is the default and means the wildcard, so there is nothing to
		// check about the address. What is still worth saying is that a router
		// with no adopted interface has no network to serve — and it is a
		// different sentence from "you configured this wrong", because the fix
		// is somewhere else entirely.
		//
		// A warning rather than an error: the listener can start, but there
		// is no managed network to advertise it to yet.
		if c.Enabled && !anyAdopted(links) {
			r.warnf("listen",
				"this router has not been given an interface yet. "+
					"Hand it the interface facing your network")
		}
		return
	}

	// One port across the set, because RelayPort and the hijack rule can only
	// name one and picking the first silently would make the other addresses
	// unreachable through the redirect.
	for i, l := range c.Listen {
		if i > 0 && l.Port() != c.Listen[0].Port() {
			r.errorf(fmt.Sprintf("listen[%d]", i),
				"%s uses a different port from %s; the relay answers on one port, so pin them "+
					"all to the same one", l, c.Listen[0])
			break
		}
	}

	seen := map[netip.AddrPort]bool{}
	for i, l := range c.Listen {
		path := fmt.Sprintf("listen[%d]", i)

		if !l.Addr().IsValid() {
			r.errorf(path, "invalid address")
			continue
		}
		if l.Port() == 0 {
			r.errorf(path, "a port is required; DNS is 53")
			continue
		}
		if seen[l] {
			r.errorf(path, "%s is listed twice", l)
			continue
		}
		seen[l] = true

		if l.Addr().IsUnspecified() {
			// Legal, and the same thing an empty list means. Spelling it out is
			// allowed because an operator who writes 0.0.0.0 means it, and
			// refusing the explicit form of the default would be a rule with
			// nothing behind it. The firewall owns access to the listener.
			continue
		}
		if l.Addr().IsLoopback() {
			if l.Port() == ResolverPort {
				r.errorf(path,
					"%d on loopback is where this module runs unbound, so the relay cannot also "+
						"have it", ResolverPort)
				continue
			}
			r.warnf(path, "%s is loopback, so only this box can resolve through it", l.Addr())
			continue
		}

		info, ok := InterfaceWithAddress(links, l.Addr())
		if !ok {
			r.errorf(path,
				"%s is not an address on any interface olr knows about, so nothing could bind it",
				l.Addr())
			continue
		}
		if !info.Adopted {
			// design.md §3.4: adopt-only. A resolver appearing on an interface
			// the operator never handed us is the exact surprise that forbids,
			// and on a WAN-facing interface it would be surprising.
			r.errorf(path, "%s is on %q, which is not adopted; run `olr adopt %s` first",
				l.Addr(), info.Name, info.Name)
			continue
		}
		if !info.Up {
			r.warnf(path, "%s is on %q, which is down; DNS is configured but will not serve until it comes up",
				l.Addr(), info.Name)
		}
	}
}

// validateUpstream checks how names get resolved.
func validateUpstream(r *Result, c Config, links LinkView) {
	u := c.Upstream
	if !u.Mode.Valid() {
		r.errorf("upstream.mode", "unknown mode %q (want %v)", u.Mode, UpstreamModes())
		return
	}

	if u.Mode.OrDefault() == ModeRecurse {
		if len(u.Servers) > 0 {
			r.warnf("upstream.servers",
				"%d server(s) are listed but the mode is %q, so they are not used", len(u.Servers), ModeRecurse)
		}
		if u.TLS {
			r.warnf("upstream.tls", "there is no forwarder to encrypt to in mode %q", ModeRecurse)
		}
		return
	}

	if len(u.Servers) == 0 {
		r.errorf("upstream.servers", "mode is %q but no servers are listed", ModeForward)
		return
	}
	if u.TLS && u.TLSName == "" {
		// Worth saying plainly, because "TLS: on" reads like it settled the
		// question and it did not.
		r.warnf("upstream.tls_name",
			"without a certificate name the connection is encrypted but the server is not "+
				"authenticated, so anything that can intercept the traffic can still answer")
	}

	// The loop check asks the kernel rather than the listen list.
	//
	// It used to compare against c.Listen, which stopped detecting anything the
	// moment the wildcard became the default: an empty list matched no
	// forwarder, so "forward to 192.168.1.1" — this box — validated cleanly and
	// built a loop. Reading the interfaces is both the fix and the more correct
	// question, because a wildcard-bound relay answers on *every* address this
	// box holds, not on a chosen few.
	for i, srv := range u.Servers {
		path := fmt.Sprintf("upstream.servers[%d]", i)
		if !srv.Addr().IsValid() {
			r.errorf(path, "invalid address")
			continue
		}
		if !ownAddress(c, links, srv.Addr()) {
			continue
		}
		if srv.Port() == 0 || srv.Port() == DNSPort {
			r.errorf(path,
				"%s is this box's own DNS address, so every query would be forwarded back to "+
					"itself", srv.Addr())
		}
	}
}

// validatePrivateDomains checks the zones exempted from anti-rebinding.
//
// A malformed name here is an error rather than a warning, because unbound
// rejects a bad `private-domain` line outright and refuses to start — which
// takes the whole house's resolution down for a setting that only ever relaxes
// a check.
func validatePrivateDomains(r *Result, c Config) {
	local := c.LocalDomainOrDefault()
	for i, d := range c.PrivateDomains {
		path := fmt.Sprintf("private_domains[%d]", i)
		if err := checkName(d); err != nil {
			r.errorf(path, "%q is not a domain name: %v", d, err)
			continue
		}
		if !strings.Contains(d, ".") {
			r.warnf(path, "%q is a single label, so it exempts every name under it; a full domain such as home.example.com is narrower", d)
		}
		if d == local || strings.HasSuffix(d, "."+local) {
			r.warnf(path, "%q is inside the local domain %s, which this box answers itself. "+
				"The exemption does nothing there: unbound never asks anybody for a name in that zone", d, local)
		}
	}
}

// validateHosts checks the names this resolver would answer itself.
//
// The stakes here are lower than for the rest of the module — a bad host entry
// costs one name, not the building's internet — so this leans on warnings where
// the other rules refuse. The exception is the local domain, which is
// house-wide: get it wrong and every local name is wrong at once.
func validateHosts(r *Result, c Config, links LinkView, reservations ReservationView) {
	domain := c.LocalDomainOrDefault()
	switch err := checkName(domain); {
	case err != nil:
		r.errorf("local_domain", "%q is not a valid domain name: %v", c.LocalDomain, err)
		return
	case domain == "local":
		// Not a warning. mDNS owns this, every Apple and most Android clients
		// resolve it by multicast without asking us, and the result is a name
		// that works on some clients and not others with nothing on this box
		// to explain why.
		r.errorf("local_domain",
			"local belongs to mDNS, so half the network would resolve these names by "+
				"multicast and never ask this resolver. Use %s, internal, or a subdomain of a "+
				"domain you own", DefaultLocalDomain)
		return
	case !strings.Contains(domain, ".") && domain != "internal":
		r.warnf("local_domain",
			"%s is a single label that nobody has reserved, so it may one day be delegated on "+
				"the internet and every name under it would start resolving somewhere else. "+
				"%s and internal cannot be", domain, DefaultLocalDomain)
	}

	if len(c.Hosts) == 0 {
		return
	}

	// Check every prefix on an adopted interface, public ones included: a
	// globally routable local host address may be intentional.
	//
	// Empty when link knows nothing yet, in which case the check below simply
	// does not run.
	lan := adoptedPrefixes(links)

	seen := map[string]int{}
	for i, h := range c.Hosts {
		path := fmt.Sprintf("hosts[%d]", i)

		switch {
		case h.Name == "":
			r.errorf(path+".name", "required")
		case h.Name == domain:
			r.errorf(path+".name",
				"%s is the local domain itself, not a name under it", domain)
		case checkHostName(h.Name) != nil:
			r.errorf(path+".name", "%q: %v", h.Name, checkHostName(h.Name))
		case len(c.FQDN(h.Name)) > MaxNameLen:
			r.errorf(path+".name", "%s is longer than %d characters", c.FQDN(h.Name), MaxNameLen)
		}
		if prev, dup := seen[h.Name]; dup {
			r.errorf(path+".name", "%s is already defined by hosts[%d]", h.Name, prev)
		}
		seen[h.Name] = i

		if len(h.Addrs) == 0 {
			r.errorf(path+".addresses",
				"%s has no address, so the name would exist and answer nothing", h.Name)
		}
		for j, a := range h.Addrs {
			apath := fmt.Sprintf("%s.addresses[%d]", path, j)
			switch {
			case !a.IsValid():
				r.errorf(apath, "invalid address")
			case a.IsUnspecified():
				r.errorf(apath, "%s is not an address anything can be reached at", a)
			case a.IsMulticast():
				r.errorf(apath, "%s is a multicast address", a)
			case len(lan) > 0 && !containsAddr(lan, a):
				// A warning and not an error: pointing a local name at
				// something off this network is unusual but legitimate, and the
				// operator may be naming a host across a VPN. A typo'd octet
				// looks exactly the same from here, which is why it is worth
				// saying at all.
				r.warnf(apath,
					"%s is not on any network this resolver serves, so %s would answer with an "+
						"address most clients here cannot reach", a, c.FQDN(h.Name))
			}
		}

		checkAgainstReservation(r, c, h, path, reservations)
	}
}

// checkAgainstReservation reports a local name and a DHCP reservation
// disagreeing about where a client is.
//
// This is the whole return on the subscription described in dhcp.go, and it is
// worth spelling out what it catches. An operator reserves 192.168.1.50 for the
// TV and publishes sony-tv pointing at it. Months later they renumber the
// reservation. DHCP moves the TV; the name goes on answering the old address;
// nothing anywhere says so, and the symptom is a page that will not load on a
// network where everything else works.
//
// Matching is by name and not by address, because the name is what the operator
// typed in both places and the address is the thing that moved. Reservations
// without a hostname are the majority and match nothing.
func checkAgainstReservation(r *Result, c Config, h Host, path string, view ReservationView) {
	domain := c.LocalDomainOrDefault()
	for _, res := range reservationsFor(view) {
		if res.Hostname == "" || normalizeHostName(res.Hostname, domain) != h.Name {
			continue
		}
		if !res.IP.IsValid() || slices.Contains(h.Addrs, res.IP) {
			continue
		}
		// A warning, firmly. Both values are things the operator typed on
		// purpose, and there is a real configuration — a name deliberately
		// pointing somewhere other than the client's own address — that this
		// would otherwise forbid. Refusing would also mean a dhcp edit could
		// make the dns config unappliable, which is the cross-module coupling
		// this design is built to avoid.
		r.warnf(path+".addresses",
			"%s answers %s, but the DHCP reservation for %s hands that client %s. "+
				"One of the two has moved",
			c.FQDN(h.Name), joinAddrs(h.Addrs), res.MAC, res.IP)
		return
	}
}

// joinAddrs renders a host's addresses for a message.
func joinAddrs(addrs []netip.Addr) string {
	out := make([]string, 0, len(addrs))
	for _, a := range addrs {
		out = append(out, a.String())
	}
	return strings.Join(out, ", ")
}

// adoptedPrefixes lists every network on an interface the operator handed us.
//
// A link module that cannot be read yields nothing rather than an error: this
// backs a warning, and an unreadable LinkView is already reported by the rules
// that need it to decide something.
func adoptedPrefixes(links LinkView) []netip.Prefix {
	infos, err := links.Interfaces()
	if err != nil {
		return nil
	}
	var out []netip.Prefix
	for _, info := range infos {
		if !info.Adopted {
			continue
		}
		out = append(out, info.Prefixes...)
	}
	return out
}

// containsAddr reports whether any prefix covers addr.
func containsAddr(prefixes []netip.Prefix, addr netip.Addr) bool {
	for _, p := range prefixes {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}

// checkHostName is checkName plus the one rule a name somebody will type into a
// browser needs and a blocklist pattern does not.
//
// checkName is deliberately lenient about underscores because they appear in
// real names on the internet. A host name is different: an underscore is not
// legal in one, and clients disagree about whether to accept it anyway — so
// `sony_tv` would resolve here and fail in some browsers, which is the worst of
// both outcomes and lands a long way from the field that caused it.
func checkHostName(n string) error {
	if err := checkName(n); err != nil {
		return err
	}
	if strings.Contains(n, "_") {
		return errors.New("contains an underscore, which is not legal in a host name")
	}
	return nil
}

// validatePolicies checks what clients may look up.
func validatePolicies(r *Result, c Config) {
	seenName := map[string]int{}
	seenClient := map[netip.Prefix]int{}
	defaultAt := -1

	for i, p := range c.Policies {
		path := fmt.Sprintf("policies[%d]", i)

		switch {
		case p.Name == "":
			r.errorf(path+".name", "required")
		case len(p.Name) > MaxPolicyNameLen:
			r.errorf(path+".name", "longer than %d characters", MaxPolicyNameLen)
		case !isSlug(p.Name):
			// It becomes a filename under policy.d, so a value with a slash or
			// a dot-dot in it would be a path traversal rather than a typo.
			r.errorf(path+".name",
				"%q must be lowercase letters, digits, dashes and underscores", p.Name)
		}
		if first, dup := seenName[p.Name]; dup {
			r.errorf(path+".name", "%q is already used at policies[%d]", p.Name, first)
		} else if p.Name != "" {
			seenName[p.Name] = i
		}

		if !p.Response.Valid() {
			r.errorf(path+".response", "unknown response %q (want %v)", p.Response, BlockResponses())
		}

		if len(p.Clients) == 0 {
			if defaultAt >= 0 {
				r.errorf(path+".clients",
					"policies[%d] (%q) is already the default policy; only one policy may have no "+
						"clients, or which one applies would depend on the order they happen to be in",
					defaultAt, c.Policies[defaultAt].Name)
			} else {
				defaultAt = i
			}
		}
		for j, cl := range p.Clients {
			cpath := fmt.Sprintf("%s.clients[%d]", path, j)
			if !cl.IsValid() {
				r.errorf(cpath, "invalid prefix")
				continue
			}
			if first, dup := seenClient[cl]; dup && first != i {
				// Same prefix in two policies: the most-specific rule cannot
				// break the tie, so which one wins would be an accident.
				r.errorf(cpath, "%s is also claimed by policies[%d] (%q)", cl, first, c.Policies[first].Name)
				continue
			}
			seenClient[cl] = i
			if cl != cl.Masked() {
				r.warnf(cpath, "%s has host bits set; it covers %s", cl, cl.Masked())
			}
		}

		validateNames(r, path+".block", p.Block)
		validateNames(r, path+".allow", p.Allow)

		for _, a := range p.Allow {
			for _, blk := range p.Block {
				if a == blk {
					r.warnf(path+".allow",
						"%q is in both block and allow; allow wins, so it is not blocked", a)
				}
			}
		}
		if len(p.Block) == 0 && len(p.Allow) > 0 {
			r.warnf(path+".allow", "there is nothing to make an exception to; block is empty")
		}
	}

	if c.Enabled && len(c.Policies) > 0 && !c.Hijack.Enabled {
		// The gap docs/dns.md §2 calls the only dangerous misconfiguration:
		// blocking applies to clients that ask us, and nothing makes them.
		r.warnf("hijack.enabled",
			"policies only apply to clients that use this resolver, and nothing currently forces "+
				"them to. A client configured with a public resolver ignores every rule above")
	}
}

// validateHijack checks the enforcement half.
func validateHijack(r *Result, c Config, links LinkView) {
	h := c.Hijack
	if !h.Enabled {
		return
	}

	if len(h.Interfaces) == 0 {
		r.errorf("hijack.interfaces",
			"required when the hijack is on. Naming no interfaces cannot mean \"all of them\" "+
				"here, because that would capture the WAN side too")
	}

	for i, name := range h.Interfaces {
		path := fmt.Sprintf("hijack.interfaces[%d]", i)
		info, err := links.Interface(name)
		if err != nil {
			r.errorf(path, "%v", err)
			continue
		}
		if !info.Adopted {
			r.errorf(path, "%q is not adopted; run `olr adopt %s` first", name, name)
		}
	}

	// The per-family warnings that used to live here are gone with the rule
	// they described. `redirect` rewrites the destination to the incoming
	// interface's own address in whichever family the packet arrived in, so
	// there is no longer a way to capture one family and silently miss the
	// other — which was the gap worth warning about.

	if !h.BlockDoT {
		// Ranked by cost to defeat in docs/dns.md §2.2: plaintext :53 is cheap
		// to hijack, DoT is cheap to drop, DoH is expensive. Leaving the cheap
		// one open makes the expensive work pointless.
		r.warnf("hijack.block_dot",
			"plaintext DNS is redirected but DNS-over-TLS on 853 is not blocked, so a client can "+
				"still reach a public resolver by switching to it — which browsers and phones do "+
				"on their own")
	}
}

func validateQueryLog(r *Result, c Config) {
	if c.QueryLog.Entries < 0 {
		r.errorf("query_log.entries", "must not be negative")
	}
	// Held in the relay's memory, so this is a memory budget rather than a
	// retention policy. Said as a warning because it is the operator's box.
	const large = 200000
	if c.QueryLog.Entries > large {
		r.warnf("query_log.entries",
			"%d entries are kept in the relay's memory; a log this large is a real cost on a "+
				"small router", c.QueryLog.Entries)
	}
	if !c.QueryLog.Enabled && len(c.Policies) > 0 {
		r.warnf("query_log.enabled",
			"names are being blocked but nothing is recorded, so \"why can this client not reach "+
				"that site\" has no answer to look up")
	}
}

// deniedDirectives are unbound settings the module renders itself. Letting the
// escape hatch set one again would produce a file with two answers, and which
// one unbound honours is not something we want to depend on.
var deniedDirectives = map[string]string{
	"interface":              "the relay owns :53; unbound's address is fixed",
	"port":                   "the relay owns :53; unbound's address is fixed",
	"access-control":         "unbound is reachable from loopback only, by construction",
	"auto-trust-anchor-file": "the trust anchor location is fixed and bootstrapped by the unit",
	"trust-anchor-file":      "the trust anchor location is fixed and bootstrapped by the unit",
	"chroot":                 "managed with the service unit",
	"username":               "managed with the service unit",
	"pidfile":                "managed with the service unit",
	"logfile":                "logs go to the journal (design.md §3.4)",
	"use-syslog":             "logs go to the journal (design.md §3.4)",
	"include":                "the module owns this file; including another would hide configuration from olr",
	"include-toplevel":       "the module owns this file; including another would hide configuration from olr",
}

// forwardDirectives are the ones that would fight the upstream field.
var forwardDirectives = map[string]string{
	"forward-zone":         "use the upstream field",
	"forward-addr":         "use the upstream field",
	"forward-tls-upstream": "use the upstream field",
}

// validateExtraConf keeps the escape hatch from re-answering questions the
// module already answered.
//
// design.md §3.2 rule 5 promises a passthrough, not a free-for-all: the point is
// that unusual settings stay visible to olr, and a directive that silently
// overrode a rendered one would do the opposite.
func validateExtraConf(r *Result, extra string) {
	for i, line := range strings.Split(extra, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		directive, _, _ := strings.Cut(trimmed, ":")
		directive = strings.ToLower(strings.TrimSpace(directive))

		path := fmt.Sprintf("extra_unbound_conf:%d", i+1)
		if why, denied := deniedDirectives[directive]; denied {
			r.errorf(path, "%s is set by olr: %s", directive, why)
			continue
		}
		if why, denied := forwardDirectives[directive]; denied {
			r.errorf(path, "%s is set by olr: %s", directive, why)
		}
	}
}

// validateNames checks a blocklist.
func validateNames(r *Result, path string, names []string) {
	for i, n := range names {
		if err := checkName(n); err != nil {
			r.errorf(fmt.Sprintf("%s[%d]", path, i), "%q: %v", n, err)
		}
	}
}

// MaxNameLen is the DNS limit on a presentation-form name.
const MaxNameLen = 253

// checkName rejects things that are not domain names.
//
// Lenient about what a label may contain — underscores appear in real names and
// refusing them would be us knowing better — and strict about the shapes that
// mean somebody pasted the wrong thing, which is the actual failure mode: a URL
// in a blocklist blocks nothing and says nothing.
func checkName(n string) error {
	switch {
	case n == "":
		return errors.New("empty")
	case len(n) > MaxNameLen:
		return fmt.Errorf("longer than %d characters", MaxNameLen)
	case strings.Contains(n, "/"):
		return errors.New("looks like a URL; use just the host name")
	case strings.ContainsAny(n, " \t"):
		return errors.New("contains whitespace")
	case strings.Contains(n, ".."):
		return errors.New("has an empty label")
	case strings.HasPrefix(n, "."):
		return errors.New("starts with a dot")
	}
	for _, label := range strings.Split(n, ".") {
		switch {
		case label == "":
			return errors.New("has an empty label")
		case len(label) > 63:
			return fmt.Errorf("label %q is longer than 63 characters", label)
		case strings.HasPrefix(label, "-"), strings.HasSuffix(label, "-"):
			return fmt.Errorf("label %q starts or ends with a dash", label)
		}
		for _, c := range label {
			switch {
			case c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '-', c == '_':
			default:
				return fmt.Errorf("label %q contains %q", label, c)
			}
		}
	}
	return nil
}

// isSlug reports whether a policy name is safe as a filename and readable as a
// label.
func isSlug(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '-', c == '_':
		default:
			return false
		}
	}
	return true
}

// ownAddress reports whether an address is one this relay answers on.
//
// Two ways for that to be true, and the wildcard case is the one that is easy
// to forget: with no pinned listen addresses the relay answers on every address
// the box holds, so any of them forms a loop when named as a forwarder. A
// pinned list narrows it back to what was named.
func ownAddress(c Config, links LinkView, addr netip.Addr) bool {
	if len(c.Listen) > 0 {
		for _, l := range c.Listen {
			if l.Addr() == addr {
				return true
			}
		}
		return false
	}
	_, ok := InterfaceWithAddress(links, addr)
	return ok
}

package ingress

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// UnitName is the systemd unit for the bundled proxy.
//
// Not `caddy.service`. The binary we ship is our build (docs/ingress.md §5.2),
// and a box that already runs the distro's Caddy must keep running it — unit
// name, binary path and config path all differ from the packaged ones, so there
// is nothing for the two installs to fight over. This is the difference between
// working and not on any machine that has ever served a web page.
const UnitName = "olr-caddy.service"

// TokenEnv is the environment variable the Caddyfile reads the provider
// credential from.
//
// The indirection is the point. The Caddyfile is the file an operator reads
// when something is wrong, the file `olr ingress plan` diffs, and the file that
// ends up pasted into a forum post. The credential is in none of those because
// it is never written there — only `{env.OLR_INGRESS_DNS_TOKEN}` is.
const TokenEnv = "OLR_INGRESS_DNS_TOKEN"

// Paths locates everything the backend reads or writes.
//
// A struct rather than constants so tests can render into a temporary directory
// and still get a config whose internal references are self-consistent.
type Paths struct {
	// Conf is the Caddyfile.
	Conf string

	// Env holds the provider credential, read by the unit through
	// EnvironmentFile= and never by the Caddyfile directly.
	Env string

	// Data is Caddy's storage: the ACME account key, and the certificate
	// itself. It must survive a package upgrade and a reboot, and it must not
	// be wiped casually — an operator who deletes it is re-issuing from the CA
	// and has rate limits waiting for them.
	Data string

	// Admin is the unix socket Caddy's admin endpoint listens on, and the only
	// place it listens. `caddy reload` is a client of that endpoint, so this is
	// what the unit's ExecReload names — the two must agree, and
	// internal/packaging's tests hold them to it.
	Admin string

	// Log is the access log for services published to the internet.
	//
	// Not a rendered File: the proxy creates and rotates it, and listing it in
	// Rendered would make every apply try to write a log. It is a path olr
	// names and Caddy owns, under the unit's StateDirectory so the sandbox and
	// the backup story already cover it.
	Log string
}

// DefaultPaths is the on-disk layout for a real install.
func DefaultPaths() Paths { return RootedPaths("") }

// RootedPaths is the same layout relocated under root, for development against
// a scratch directory without root or systemd.
func RootedPaths(root string) Paths {
	rendered := filepath.Join(root, "/etc/open-linux-router/rendered/ingress")
	return Paths{
		Conf: filepath.Join(rendered, "Caddyfile"),
		Env:  filepath.Join(rendered, "caddy.env"),
		Data: filepath.Join(root, "/var/lib/open-linux-router/ingress"),
		// Under the unit's own RuntimeDirectory, olr/ingress, mode 0700: root
		// only, and cleared by systemd when the proxy stops, so a socket can
		// never outlive the process that answers on it.
		Admin: filepath.Join(root, "/run/olr/ingress/admin.sock"),
		// Beside Caddy's certificates, inside the unit's StateDirectory
		// (olr-caddy.service), which is what makes it writable without widening
		// the unit's sandbox.
		Log: filepath.Join(root, "/var/lib/open-linux-router/ingress/logs/public.log"),
	}
}

// File is one rendered file.
type File struct {
	// Path is absolute.
	Path string
	// Mode is the file's permissions.
	Mode fs.FileMode
	// Data is the full contents.
	Data []byte
	// Secret suppresses the contents on every surface that displays a file:
	// the plan diff, the drift report, the logs. The file is still written and
	// still compared byte for byte — only its *display* is withheld, because a
	// credential that is correct is no less a credential.
	Secret bool
}

// Rendered is the complete set of files a config produces, sorted by path so
// that drift detection compares like with like.
type Rendered struct {
	Files []File
}

// Get returns a file by path.
func (r Rendered) Get(path string) (File, bool) {
	i := slices.IndexFunc(r.Files, func(f File) bool { return f.Path == path })
	if i < 0 {
		return File{}, false
	}
	return r.Files[i], true
}

// Paths lists the rendered paths, in order.
func (r Rendered) Paths() []string {
	out := make([]string, len(r.Files))
	for i, f := range r.Files {
		out[i] = f.Path
	}
	return out
}

// Caddy renders a config into the proxy's files.
//
// There is no Backend interface, for the same reason internal/dhcp has none:
// docs/ingress.md §10 chose Caddy and rejected the alternatives on the record,
// so by §3.1's test there is exactly one implementation and nothing to iterate
// over.
type Caddy struct {
	Paths Paths

	// Source is the intent file named in every generated file's ownership
	// header. An operator who finds one of these needs to be pointed at the
	// file that produced it (design.md §3.4).
	Source string
}

// NewCaddy returns a backend writing to the given layout.
func NewCaddy(p Paths) Caddy { return Caddy{Paths: p, Source: core.ConfigPath} }

// WithSource names the intent file in generated headers.
func (c Caddy) WithSource(path string) Caddy { c.Source = path; return c }

// Name is the backend's own name, for `status` and `logs`.
//
// It belongs there and nowhere on the common path, per design.md §1's corollary
// that the name of the backend we drive is an implementation detail. An operator
// publishing a service should not have to know a thing called Caddy exists.
func (Caddy) Name() string { return "caddy" }

// Unit is the systemd unit that runs it.
func (Caddy) Unit() string { return UnitName }

// Render turns intent into files.
//
// Addresses are resolved here rather than stored, so the rendered file is a
// snapshot of where clients are at the moment it was written and re-rendering
// is what picks up a move. Validate has already refused any client without a
// fixed address, so in practice these do not change — but the direction of the
// dependency is what matters (§4.1), not how often it pays off.
//
// The suffix comes from the config, not from the resolver: what this file says
// and what the operator's DNS zone contains are the same thing by construction,
// because both are copies of Config.Domain that only one of them is generated
// from (docs/ingress.md §3).
func (c Caddy) Render(cfg Config, clients ClientView) (Rendered, error) {
	domain := normalizeDomain(cfg.Domain)

	conf, err := c.caddyfile(cfg, domain, clients)
	if err != nil {
		return Rendered{}, err
	}

	out := Rendered{Files: []File{
		{Path: c.Paths.Conf, Mode: 0o644, Data: []byte(conf)},
		{
			Path: c.Paths.Env,
			// 0600 and not a byte looser. This is the whole of olr's secrets
			// story for now (docs/ingress.md §4.1), and calling it a stopgap
			// in a document does not excuse rendering it world-readable.
			Mode:   0o600,
			Data:   []byte(c.env(cfg)),
			Secret: true,
		},
	}}
	slices.SortFunc(out.Files, func(a, b File) int { return strings.Compare(a.Path, b.Path) })
	return out, nil
}

// header is the ownership banner every generated file carries (design.md §7).
func (c Caddy) header(what string) string {
	source := c.Source
	if source == "" {
		source = core.ConfigPath
	}
	return fmt.Sprintf(`# %s
#
# Generated by open-linux-router from
#   %s
# Do not edit: this file is rewritten on every apply and your changes will be
# lost. Use `+"`olr ingress add`"+`, or the raw_caddyfile field for settings olr
# does not model.
`, what, source)
}

// Canonical reduces a rendered file to what Caddy actually reads: comments and
// blank lines removed.
//
// This is what stops a comment from being a config change, and this module
// needs it more than most — most of the Caddyfile above is explanation, all of
// it string literals in this file. Without normalisation, rewording one of
// those comments in a new olr release would mark every deployed box as drifted
// and schedule a proxy reload, which here means dropping every connection
// through it for the sake of a sentence nobody read. The file is still
// rewritten; it just stops being a reason to signal the daemon.
func Canonical(data []byte) string {
	var out []string
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		out = append(out, trimmed)
	}
	return strings.Join(out, "\n")
}

func (c Caddy) env(cfg Config) string {
	var b strings.Builder
	b.WriteString(c.header("environment for " + UnitName))
	fmt.Fprintf(&b, "%s=%s\n", TokenEnv, cfg.Certificate.Token)
	return b.String()
}

func (c Caddy) caddyfile(cfg Config, domain string, clients ClientView) (string, error) {
	var b strings.Builder
	b.WriteString(c.header("Caddyfile for " + UnitName))

	c.globals(&b, cfg)

	if !cfg.Enabled || domain == "" {
		// Disabled keeps the configuration and stops the service (Config.Enabled),
		// so what is rendered here is a file that is valid and serves nothing —
		// never a file that fails to parse, because a disabled module must not
		// be the reason a later `caddy validate` fails for an unrelated change.
		b.WriteString("\n# ingress is disabled; no service is published.\n")
		return b.String(), nil
	}

	// The private names, on their own suffix, reachable only from this
	// network. Every service is here — publishing a service to the internet
	// adds a second name for it, it does not move it.
	if err := c.site(&b, cfg, domain, true, clients); err != nil {
		return "", err
	}

	// The public names, when there are any. A config with a public domain but
	// no exposed service renders no second site block at all: the wildcard
	// certificate is not requested and no name is served, so setting the field
	// and exposing nothing costs nothing and leaks nothing.
	if public := normalizeDomain(cfg.PublicDomain); public != "" && cfg.HasExposed() {
		if err := c.site(&b, cfg, public, false, clients); err != nil {
			return "", err
		}
	}

	if extra := strings.TrimSpace(cfg.ExtraConf); extra != "" {
		b.WriteString("\n# --- raw_caddyfile (design.md §3.2 rule 5) ---\n")
		b.WriteString(extra)
		b.WriteString("\n")
	}

	return b.String(), nil
}

func (c Caddy) globals(b *strings.Builder, cfg Config) {
	b.WriteString("\n{\n")

	// Caddy's admin API can mutate configuration without touching this file.
	// docs/ingress.md §7.2 rejects using it that way — config that lives only
	// in a running process is not revisioned and not readable over SSH during
	// an outage — but it cannot be turned off, because `caddy reload` is
	// itself a client of it. `admin off` was tried and every reload after the
	// first start failed, which is every edit to a published service.
	//
	// So it listens on a unix socket rather than on localhost:2019. A TCP port
	// on loopback is reachable by every local user; this socket sits in a
	// root-only directory and Caddy creates it owner-only besides. What is left
	// is the channel the unit's ExecReload uses, reachable by nobody else.
	fmt.Fprintf(b, "\tadmin %s\n", adminAddress(c.Paths.Admin))

	if cfg.Certificate.Email != "" {
		fmt.Fprintf(b, "\temail %s\n", cfg.Certificate.Email)
	}
	b.WriteString("}\n")
}

// adminAddress spells a socket path the way both the `admin` option and
// `caddy reload --address` take it.
func adminAddress(socket string) string { return "unix/" + socket }

func (c Caddy) tls(b *strings.Builder, cfg Config, site string) {
	b.WriteString("\n\t# One wildcard certificate serves every published name, which is what\n")
	b.WriteString("\t# makes publishing a service a two-field operation rather than a\n")
	b.WriteString("\t# certificate ceremony (docs/ingress.md §1).\n")
	b.WriteString("\ttls {\n")
	fmt.Fprintf(b, "\t\tdns %s {env.%s}\n", cfg.Certificate.Provider, TokenEnv)

	// The propagation check must not go through this box's resolver. It is not
	// authoritative for the published domain any more, so it *could* answer —
	// but it also carries this network's blocking policies and whatever
	// upstream it forwards to, and issuance hanging on a filtered lookup is a
	// failure that names nothing. Public resolvers are the independent second
	// opinion, and the CA is about to ask them anyway.
	b.WriteString("\n\t\t# Ask public resolvers, never this box: the lookup that decides whether\n")
	b.WriteString("\t\t# a certificate is issued should not pass through our own filters.\n")
	fmt.Fprintf(b, "\t\tresolvers %s\n", strings.Join(cfg.Certificate.ResolversOrDefault(), " "))
	b.WriteString("\t}\n")
}

// site renders one wildcard site block.
//
// The two blocks are the same shape with two differences: the private one
// guards every name by source address, and the public one logs its access and
// contains only the services that asked to be there. Keeping them one function
// is what stops the guard from being on the public block by mistake — the
// parameter is called `private` and the guard is what it means.
func (c Caddy) site(b *strings.Builder, cfg Config, domain string, private bool, clients ClientView) error {
	site := qualify("*", domain)
	fmt.Fprintf(b, "\n%s {\n", site)
	c.tls(b, cfg, site)

	if !private {
		c.accessLog(b)
	}

	for _, s := range cfg.Services {
		if !private && !s.Expose.IsPublic() {
			continue
		}
		if err := c.service(b, s, domain, private, clients); err != nil {
			return err
		}
	}

	// Anything under the wildcard that is not published closes the connection
	// rather than answering. Without this Caddy's own fallback answers 200 with
	// an empty body, which tells a scanner that the name exists and tells an
	// operator testing a typo that something is listening. `abort` says
	// neither. On the private block it is also what an outside request lands on
	// when its source does not match the service's matcher.
	b.WriteString("\n\thandle {\n\t\tabort\n\t}\n")
	b.WriteString("}\n")
	return nil
}

// accessLog records who reached an exposed service.
//
// Two things are deliberate. **The query string is dropped** and the path is
// re-added as its own field: an S3 request carries its signature in the query,
// and an access log that keeps it is a log that leaks temporary credentials to
// whoever reads the file later. Caddy's default already redacts Authorization
// and Cookie; the query is ours to handle.
//
// And it is a file, with rotation, rather than the journal, because this one is
// bounded by volume rather than by events: a public service can produce
// thousands of lines a day, and journald's retention is the operator's global
// setting, not this feature's.
func (c Caddy) accessLog(b *strings.Builder) {
	if c.Paths.Log == "" {
		return
	}
	b.WriteString("\n\t# Who reached an exposed service. The URI is removed and re-added as a\n")
	b.WriteString("\t# path: an S3 signature lives in the query string.\n")
	b.WriteString("\tlog {\n")
	fmt.Fprintf(b, "\t\toutput file %s {\n", c.Paths.Log)
	b.WriteString("\t\t\troll_size 10MiB\n")
	b.WriteString("\t\t\troll_keep 5\n")
	b.WriteString("\t\t}\n")
	b.WriteString("\t\tformat filter {\n\t\t\tfields {\n\t\t\t\trequest>uri delete\n\t\t\t}\n\t\t}\n")
	b.WriteString("\t}\n")
	b.WriteString("\tlog_append request_path {http.request.uri.path}\n")
}

func (c Caddy) service(b *strings.Builder, s Service, domain string, private bool, clients ClientView) error {
	fqdn := qualify(s.Name, domain)

	resolved := ""
	if s.Upstream.Client != "" {
		info, err := clients.Client(s.Upstream.Client)
		if err != nil {
			return fmt.Errorf("service %q: %w", s.Name, err)
		}
		if !info.Addr.IsValid() {
			return fmt.Errorf("service %q: client %q has no address", s.Name, s.Upstream.Client)
		}
		resolved = info.Addr.String()
	}
	target := s.Upstream.Target(resolved)

	// A host matcher inside one wildcard site block, rather than a site block
	// per name. Separate blocks would each ask for their own certificate and
	// defeat the wildcard — the reason §1 can promise that adding a service
	// involves no certificate step is that no new name is ever requested.
	//
	// On the private block the matcher is a *block* and not a one-liner: the
	// name alone is not what makes it private. Anyone on the internet can send
	// this SNI to this port, and the certificate would match; the source test is
	// what turns a name that is merely unadvertised into one that is not
	// served.
	name := matcherName(s.Name)
	if !private {
		name = publicMatcherName(s.Name)
	}
	switch {
	case private:
		fmt.Fprintf(b, "\n\t@%s {\n", name)
		fmt.Fprintf(b, "\t\thost %s\n", fqdn)
		b.WriteString("\t\tremote_ip private_ranges\n")
		b.WriteString("\t}\n")
	case s.Expose.OrDefault() == ExposureRestricted:
		fmt.Fprintf(b, "\n\t@%s {\n", name)
		fmt.Fprintf(b, "\t\thost %s\n", fqdn)
		fmt.Fprintf(b, "\t\tremote_ip %s\n", strings.Join(s.AllowFrom, " "))
		b.WriteString("\t}\n")
	default:
		fmt.Fprintf(b, "\n\t@%s host %s\n", name, fqdn)
	}
	fmt.Fprintf(b, "\thandle @%s {\n", name)

	if s.Upstream.Scheme.OrDefault() == SchemeHTTPS {
		fmt.Fprintf(b, "\t\treverse_proxy https://%s {\n", target)
		b.WriteString("\t\t\ttransport http {\n")
		// Verification is not attempted, deliberately. The NAS and hypervisor
		// UIs this exists for all present a self-signed certificate, and
		// demanding a valid one would make the common case impossible in
		// exchange for protection against an attacker who is already on the
		// operator's LAN, between olr and a client the operator named.
		b.WriteString("\t\t\t\ttls\n")
		b.WriteString("\t\t\t\ttls_insecure_skip_verify\n")
		b.WriteString("\t\t\t}\n")
		c.requestHeaders(b, s.Upstream.RequestHeaders)
		b.WriteString("\t\t}\n")
	} else {
		if len(s.Upstream.RequestHeaders) == 0 {
			fmt.Fprintf(b, "\t\treverse_proxy %s\n", target)
		} else {
			fmt.Fprintf(b, "\t\treverse_proxy %s {\n", target)
			c.requestHeaders(b, s.Upstream.RequestHeaders)
			b.WriteString("\t\t}\n")
		}
	}

	b.WriteString("\t}\n")
	return nil
}

func (Caddy) requestHeaders(b *strings.Builder, headers []RequestHeader) {
	for _, h := range headers {
		if h.Remove {
			fmt.Fprintf(b, "\t\t\theader_up -%s\n", h.Name)
		} else {
			fmt.Fprintf(b, "\t\t\theader_up %s %s\n", h.Name, strconv.Quote(h.Value))
		}
	}
}

// matcherName makes a Caddy matcher label out of a service name. Names are
// already validated to letters, digits and hyphens, so this only has to avoid
// colliding with Caddy's own tokens.
func matcherName(name string) string { return "svc_" + strings.ReplaceAll(name, "-", "_") }

// publicMatcherName is the exposed name's label. Distinct from matcherName so
// that a rendered file reads as two sets rather than one set used twice; the
// two live in different site blocks and could legally collide, which is exactly
// why a reader should not have to work out that they do not.
func publicMatcherName(name string) string { return "pub_" + strings.ReplaceAll(name, "-", "_") }

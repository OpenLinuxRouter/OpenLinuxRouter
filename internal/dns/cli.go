// Package dns is the DNS module.
//
// It owns unbound and olr's own relay, and nothing else: DHCP stays with the
// dhcp module and dnsmasq (design.md §4.2), which is why that module renders
// `port=0` and leaves :53 free for this one to claim.
package dns

import (
	"context"
	"fmt"
	"net/netip"
	"os/exec"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/open-linux-router/open-linux-router/internal/cli"
	"github.com/open-linux-router/open-linux-router/internal/core"
)

// Every command here is a client of olrd (design.md §6.1). None of them touches
// the config document, the rendered files or systemd directly, and that is the
// point rather than an implementation detail: a CLI that wrote the system on
// its own would be a second writer, holding a lock olrd cannot see, running its
// own copy of the validation rules, and publishing none of the change events
// the UI listens for.

// Command returns the module's command tree. Mounted explicitly by cmd/olr.
func Command() *cobra.Command {
	return cli.NewModule("dns", "DNS resolver and policy (olr-dnsd + unbound)",
		showCommand(),
		setCommand(),
		addCommand(),
		rmCommand(),
		statusCommand(),
		logsCommand(),
		enableCommand(),
		disableCommand(),
		// Built by internal/cli rather than here: it reads core.Blocker off
		// /status and posts to /blockers/fix, neither of which is this module's
		// shape, and dhcp needs the identical command for the identical dnsmasq.
		cli.FixCommand(ModuleName, "DNS's"),
	)
}

// Endpoints this module's commands call. Spelled once so a rename cannot leave
// half the commands pointing at the old path.
const (
	configEndpoint  = core.APIPrefix + "/" + ModuleName + "/config"
	planEndpoint    = core.APIPrefix + "/" + ModuleName + "/plan"
	statusEndpoint  = core.APIPrefix + "/" + ModuleName + "/status"
	queriesEndpoint = core.APIPrefix + "/" + ModuleName + "/queries"
	namesEndpoint   = core.APIPrefix + "/" + ModuleName + "/names"

	// The one endpoint here that belongs to another module. Reading dhcp's
	// stored intent is what lets `add host` fill in an address the operator has
	// already typed once; see addHostCommand for why the join lives in the
	// client. Spelled literally rather than imported from internal/dhcp, which
	// would make this module depend on that one for a string.
	dhcpConfigEndpoint = core.APIPrefix + "/dhcp/config"
)

// ctxOf is the request context, falling back to Background for a command
// invoked outside cobra's execution (which the tests do).
func ctxOf(c *cobra.Command) context.Context {
	if ctx := c.Context(); ctx != nil {
		return ctx
	}
	return context.Background()
}

// loadConfig fetches stored intent.
func loadConfig(c *cobra.Command) (Config, error) {
	var cfg Config
	if err := cli.ClientFor(c).Get(ctxOf(c), configEndpoint, &cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// verb wraps cli.Verb so the shared vocabulary check still runs (design.md
// §6.1: a verb outside the vocabulary panics at startup).
func verb(name, short string, build func(*cobra.Command)) *cobra.Command {
	c := cli.Verb(name, short)
	c.RunE = nil
	build(c)
	return c
}

// ---------------------------------------------------------------- show

func showCommand() *cobra.Command {
	c := verb("show", "Show DNS configuration", func(c *cobra.Command) {
		c.Args = cobra.NoArgs
		c.RunE = func(c *cobra.Command, _ []string) error {
			if err := cli.ValidateOutput(c); err != nil {
				return err
			}
			// --dry-run on `show` is the drift question: plan stored intent
			// against reality and print what does not match (design.md §5.4).
			if cli.DryRun(c) {
				var plan planView
				if err := cli.ClientFor(c).Post(ctxOf(c), planEndpoint, nil, &plan); err != nil {
					return err
				}
				if cli.IsJSON(c) {
					return cli.JSON(c.OutOrStdout(), plan)
				}
				return writePlanText(c.OutOrStdout(), plan, true)
			}

			cfg, err := loadConfig(c)
			if err != nil {
				return err
			}
			if cli.IsJSON(c) {
				return cli.JSON(c.OutOrStdout(), cfg)
			}
			return writeConfigText(c.OutOrStdout(), cfg)
		}
	})

	c.AddCommand(
		showPoliciesCommand(),
		showPolicyCommand(),
		showHostsCommand(),
		showQueriesCommand(),
		showNamesCommand(),
	)
	return c
}

// showPoliciesCommand and showPolicyCommand were one command taking an optional
// name (docs/cli.md R6). Splitting them is what lets each describe itself in one
// line, and what lets the detail half complete its argument — a completer
// cannot suggest policy names for an argument that may equally well be absent.

func showPoliciesCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "policies",
		Short: "List policies",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			if err := cli.ReadOnly(c); err != nil {
				return err
			}
			cfg, err := loadConfig(c)
			if err != nil {
				return err
			}
			if cli.IsJSON(c) {
				return cli.JSON(c.OutOrStdout(), cfg.Policies)
			}
			return writePoliciesText(c.OutOrStdout(), cfg.Policies)
		},
	}
}

func showPolicyCommand() *cobra.Command {
	c := &cobra.Command{
		Use:   "policy <name>",
		Short: "Show one policy in full",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			if err := cli.ReadOnly(c); err != nil {
				return err
			}
			cfg, err := loadConfig(c)
			if err != nil {
				return err
			}
			p, ok := cfg.Policy(args[0])
			if !ok {
				return unknownPolicy(&cfg, args[0])
			}
			if cli.IsJSON(c) {
				return cli.JSON(c.OutOrStdout(), p)
			}
			return writeNamesListText(c.OutOrStdout(), p)
		},
	}
	c.ValidArgsFunction = cli.CompleteArgs(policyNames)
	return c
}

// showHostsCommand lists what this resolver answers for itself.
//
// Deliberately a different word from `show names`, which is the observed list —
// what the clients here looked up. Hosts are the other direction: what this
// network answers when something looks *it* up. Two words for two things beats
// one word an operator has to disambiguate by column heading.
func showHostsCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "hosts",
		Short: "List the local names this resolver answers",
		Long: "List the names this resolver answers itself.\n\n" +
			"Each is published under the local domain, so a host named sony-tv is\n" +
			"reachable as sony-tv.home.arpa from anywhere on this network. The bare\n" +
			"name works too on clients that were handed the domain as their DHCP\n" +
			"search domain — see `olr dhcp set --domain`.",
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			if err := cli.ReadOnly(c); err != nil {
				return err
			}
			cfg, err := loadConfig(c)
			if err != nil {
				return err
			}
			if cli.IsJSON(c) {
				return cli.JSON(c.OutOrStdout(), cfg.Hosts)
			}
			return writeHostsText(c.OutOrStdout(), cfg)
		},
	}
}

func showQueriesCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "queries",
		Short: "List recently answered queries",
		Long: "List the queries the relay has answered.\n\n" +
			"Queries are observed, not configured: they are read from the relay on\n" +
			"every request and are never stored or revisioned by olr (design.md §6.2).\n" +
			"The log lives in the relay's memory, so it starts empty after a restart —\n" +
			"the `since` line in `olr dns status` says when that was.",
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			if err := cli.ReadOnly(c); err != nil {
				return err
			}
			var resp queriesResponse
			if err := cli.ClientFor(c).Get(ctxOf(c), queriesEndpoint, &resp); err != nil {
				return err
			}
			if cli.IsJSON(c) {
				return cli.JSON(c.OutOrStdout(), resp)
			}
			return writeQueriesText(c.OutOrStdout(), resp)
		},
	}
}

func showNamesCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "names",
		Short: "List the addresses each client resolved, and for what",
		Long: "List the domain-to-address map the relay built from the answers it saw.\n\n" +
			"This is what makes traffic attributable: a client connecting to a CDN\n" +
			"address is meaningless on its own, and this says which name it asked for\n" +
			"to get there. Entries expire at the record's TTL plus a grace period.",
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			if err := cli.ReadOnly(c); err != nil {
				return err
			}
			var resp namesResponse
			if err := cli.ClientFor(c).Get(ctxOf(c), namesEndpoint, &resp); err != nil {
				return err
			}
			if cli.IsJSON(c) {
				return cli.JSON(c.OutOrStdout(), resp)
			}
			return writeNamesText(c.OutOrStdout(), resp)
		},
	}
}

// ---------------------------------------------------------------- set

func setCommand() *cobra.Command {
	var f configFlags

	c := verb("set", "Change DNS settings", func(c *cobra.Command) {
		c.Args = cobra.NoArgs
		c.RunE = func(c *cobra.Command, _ []string) error {
			return mutate(c, func(cfg *Config) error { return f.apply(c, cfg) })
		}
	})
	f.register(c)
	return c
}

type configFlags struct {
	filterAAAA  bool
	listen      []string
	mode        string
	servers     []string
	tls         bool
	tlsName     string
	queryLog    bool
	logEntries  int
	hijack      bool
	interfaces  []string
	blockDoT    bool
	localDomain string
	private     []string
}

func (f *configFlags) register(c *cobra.Command) {
	c.Flags().BoolVar(&f.filterAAAA, "filter-aaaa", false, "answer AAAA queries without IPv6 addresses")
	c.Flags().StringArrayVar(&f.listen, "listen", nil,
		"pin DNS to specific addresses, repeatable, e.g. 192.168.1.1:53 "+
			"(default: every address this router holds; pins are yours to keep correct, and olr adds "+
			"the addresses it hands to clients itself)")
	c.Flags().StringVar(&f.mode, "mode", "",
		"how names are resolved: "+joinUpstreamModes())
	c.Flags().StringArrayVar(&f.servers, "upstream", nil,
		"forwarder, repeatable, used with --mode forward, e.g. 1.1.1.1")
	c.Flags().BoolVar(&f.tls, "tls", false, "forward over DNS-over-TLS")
	c.Flags().StringVar(&f.tlsName, "tls-name", "",
		"certificate name the forwarder must present, e.g. cloudflare-dns.com")
	c.Flags().BoolVar(&f.queryLog, "query-log", false, "record answered queries")
	c.Flags().IntVar(&f.logEntries, "query-log-entries", 0,
		"how many answered queries to keep in memory")
	c.Flags().BoolVar(&f.hijack, "redirect", false,
		"redirect clients that use another resolver back to this one")
	c.Flags().StringArrayVar(&f.interfaces, "redirect-on", nil,
		"interface whose forwarded DNS is redirected, repeatable")
	c.Flags().BoolVar(&f.blockDoT, "block-dot", false,
		"drop DNS-over-TLS on 853, so clients cannot route around the redirect")
	c.Flags().StringVar(&f.localDomain, "local-domain", "",
		"suffix local names are published under, e.g. "+DefaultLocalDomain)
	c.Flags().StringArrayVar(&f.private, "private-domain", nil,
		"zone whose public records may point at private addresses, repeatable, e.g. home.example.com "+
			"(without it, a record there is invisible to clients using this resolver)")

	cli.EnumFlag(c, "mode", upstreamModeNames()...)
}

func (f *configFlags) apply(c *cobra.Command, cfg *Config) error {
	changed := c.Flags().Changed
	if changed("filter-aaaa") {
		cfg.FilterAAAA = f.filterAAAA
	}

	if changed("listen") {
		listen, err := parseAddrPorts("--listen", f.listen, DNSPort)
		if err != nil {
			return err
		}
		cfg.Listen = listen
	}
	if changed("mode") {
		mode := UpstreamMode(f.mode)
		if !mode.Valid() {
			return fmt.Errorf("--mode: unknown mode %q (want %s)", f.mode, joinUpstreamModes())
		}
		cfg.Upstream.Mode = mode
	}
	if changed("upstream") {
		// A bare address means the right port for the transport, which is the
		// difference between `--upstream 1.1.1.1 --tls` working and silently
		// trying DoT against plaintext 53.
		port := uint16(53)
		if f.tls || cfg.Upstream.TLS {
			port = DefaultDoTPort
		}
		servers, err := parseAddrPorts("--upstream", f.servers, port)
		if err != nil {
			return err
		}
		cfg.Upstream.Servers = servers
		// Listing forwarders and leaving the mode at its default would be a
		// config that quietly ignores them, so the obvious reading wins.
		if !changed("mode") && len(servers) > 0 {
			cfg.Upstream.Mode = ModeForward
		}
	}
	if changed("tls") {
		cfg.Upstream.TLS = f.tls
	}
	if changed("tls-name") {
		cfg.Upstream.TLSName = f.tlsName
	}
	if changed("query-log") {
		cfg.QueryLog.Enabled = f.queryLog
	}
	if changed("query-log-entries") {
		cfg.QueryLog.Entries = f.logEntries
	}
	if changed("redirect") {
		cfg.Hijack.Enabled = f.hijack
	}
	if changed("redirect-on") {
		cfg.Hijack.Interfaces = f.interfaces
	}
	if changed("block-dot") {
		cfg.Hijack.BlockDoT = f.blockDoT
	}
	if changed("private-domain") {
		cfg.PrivateDomains = f.private
	}

	if changed("local-domain") {
		// Normalize here and not only on the way out: hosts are stored relative
		// to this value, so every later Host lookup in this same edit compares
		// against the canonical form of what was just typed.
		cfg.LocalDomain = f.localDomain
		cfg.Normalize()
	}
	return nil
}

// ---------------------------------------------------------------- add / rm

func addCommand() *cobra.Command {
	c := verb("add", "Add a policy, a blocked name or a local name", func(c *cobra.Command) {})
	c.AddCommand(policyCommand("add"), blockCommand("add"), allowCommand("add"), addHostCommand())
	return c
}

func rmCommand() *cobra.Command {
	c := verb("rm", "Remove a policy, a blocked name or a local name", func(c *cobra.Command) {})
	c.AddCommand(rmPolicyCommand(), blockCommand("rm"), allowCommand("rm"), rmHostCommand())
	return c
}

// addHostCommand names one client.
//
// The address is an argument rather than a flag because it is usually not
// optional: a host with no address is a name that exists and answers nothing,
// which validate refuses. Repeating it gives one name both families.
//
// It may be left out for a client that already has a DHCP reservation with this
// hostname, in which case the address comes from there. That join happens here,
// in the client, and deliberately not in olrd: a reservation and a local name
// are two modules' config, there is no cross-module transaction (§5.3.1), and
// the CLI is an API client that can perfectly well read one and write the other
// (§6.1). What it must not become is olrd *storing* one from the other — see
// internal/dns/dhcp.go for why that direction stays closed.
func addHostCommand() *cobra.Command {
	c := &cobra.Command{
		Use:   "host <name> [<address>]...",
		Short: "Add or replace a local name",
		Long: "Add or replace a name this resolver answers itself.\n\n" +
			"The name is relative to the local domain: `add host sony-tv 192.168.1.50`\n" +
			"publishes sony-tv.home.arpa. Writing it out in full works too and means the\n" +
			"same entry. Give the address twice to publish both an IPv4 and an IPv6 one.\n\n" +
			"Leave the address out for a client that already has a DHCP reservation under\n" +
			"this name, and it is taken from there — `olr dhcp add reservation` once, then\n" +
			"`olr dns add host sony-tv`.\n\n" +
			"This replaces an existing entry outright rather than adding to it, so the\n" +
			"addresses given are the addresses the name will have.",
		Args: cobra.MinimumNArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			if len(args) == 1 {
				addr, err := reservedAddress(c, args[0])
				if err != nil {
					return err
				}
				args = append(args, addr)
			}
			return mutate(c, func(cfg *Config) error { return addHost(cfg, args) })
		},
	}
	// Replacing is as normal as creating here, so completing the names that
	// exist is a convenience rather than an invitation to collide.
	c.ValidArgsFunction = cli.CompleteArgs(hostNames)
	return c
}

func rmHostCommand() *cobra.Command {
	c := &cobra.Command{
		Use:   "host <name>",
		Short: "Remove a local name",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			return mutate(c, func(cfg *Config) error { return removeHost(cfg, args[0]) })
		},
	}
	c.ValidArgsFunction = cli.CompleteArgs(hostNames)
	return c
}

// reservedAddress finds the address DHCP already hands the client with this
// hostname.
//
// Matched on the name the operator typed in both places. The error names the
// alternative rather than just refusing, because "no address given" with no
// further help is a dead end for somebody who does not yet know the two
// commands are related.
func reservedAddress(c *cobra.Command, name string) (string, error) {
	var dhcpCfg struct {
		Reservations []struct {
			MAC      string `json:"mac"`
			IP       string `json:"ip"`
			Hostname string `json:"hostname"`
		} `json:"reservations"`
	}
	if err := cli.ClientFor(c).Get(ctxOf(c), dhcpConfigEndpoint, &dhcpCfg); err != nil {
		return "", fmt.Errorf("no address given, and the DHCP configuration could not be read to find one: %w", err)
	}

	want := strings.ToLower(strings.Trim(strings.TrimSpace(name), "."))
	for _, r := range dhcpCfg.Reservations {
		if strings.ToLower(r.Hostname) == want && r.IP != "" {
			return r.IP, nil
		}
	}
	return "", fmt.Errorf(
		"no address given, and no DHCP reservation is named %q to take one from. "+
			"Give the address, or reserve one first with `olr dhcp add reservation`", name)
}

// addHost is the edit itself, lifted out of RunE so it can be tested without a
// daemon to talk to — the same shape targetPolicy has, for the same reason.
func addHost(cfg *Config, args []string) error {
	h := Host{Name: args[0]}
	for _, raw := range args[1:] {
		addr, err := netip.ParseAddr(raw)
		if err != nil {
			return fmt.Errorf("%q is not an IP address", raw)
		}
		h.Addrs = append(h.Addrs, addr)
	}
	cfg.SetHost(h)
	return nil
}

func removeHost(cfg *Config, name string) error {
	if !cfg.RemoveHost(name) {
		return unknownHost(cfg, name)
	}
	return nil
}

// policyCommand creates or replaces a policy.
func policyCommand(string) *cobra.Command {
	var (
		clients  []string
		block    []string
		allow    []string
		response string
	)

	c := &cobra.Command{
		Use:   "policy <name>",
		Short: "Add or replace a policy",
		Long: "Add or replace a policy: a set of clients and what they may look up.\n\n" +
			"A policy with no --client is the default one, applying to every client no\n" +
			"other policy claims. The most specific matching network wins, so a policy\n" +
			"naming one address beats one naming the whole subnet.",
		Args: cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			return mutate(c, func(cfg *Config) error {
				p, existing := cfg.Policy(args[0])
				p.Name = args[0]
				if !existing {
					p.Block, p.Allow = nil, nil
				}

				if c.Flags().Changed("client") {
					prefixes, err := parsePrefixes("--client", clients)
					if err != nil {
						return err
					}
					p.Clients = prefixes
				}
				if c.Flags().Changed("block") {
					p.Block = block
				}
				if c.Flags().Changed("allow") {
					p.Allow = allow
				}
				if c.Flags().Changed("response") {
					r := BlockResponse(response)
					if !r.Valid() {
						return fmt.Errorf("--response: unknown response %q (want %s)",
							response, joinBlockResponses())
					}
					p.Response = r
				}
				cfg.SetPolicy(p)
				return nil
			})
		},
	}

	c.Flags().StringArrayVar(&clients, "client", nil,
		"a network this policy governs, repeatable, e.g. 192.168.1.50/32 (omit for the default policy)")
	c.Flags().StringArrayVar(&block, "block", nil,
		"a name to block, covering it and everything under it, repeatable")
	c.Flags().StringArrayVar(&allow, "allow", nil,
		"an exception that beats --block, repeatable")
	c.Flags().StringVar(&response, "response", "",
		"what a blocked name answers with: "+joinBlockResponses())
	cli.EnumFlag(c, "response", blockResponseNames()...)
	// `add policy` replaces as well as creates, so completing existing names
	// is a real convenience here rather than a suggestion to collide.
	c.ValidArgsFunction = cli.CompleteArgs(policyNames)
	return c
}

func rmPolicyCommand() *cobra.Command {
	c := &cobra.Command{
		Use:   "policy <name>",
		Short: "Remove a policy",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			return mutate(c, func(cfg *Config) error {
				if !cfg.RemovePolicy(args[0]) {
					return unknownPolicy(cfg, args[0])
				}
				return nil
			})
		},
	}
	c.ValidArgsFunction = cli.CompleteArgs(policyNames)
	return c
}

// blockCommand and allowCommand edit one list inside a policy, which is the
// common operation and the one an operator should not have to restate a whole
// policy to perform.
func blockCommand(mode string) *cobra.Command { return nameListCommand(mode, "block") }
func allowCommand(mode string) *cobra.Command { return nameListCommand(mode, "allow") }

func nameListCommand(mode, list string) *cobra.Command {
	var policy string

	short := map[string]string{
		"block": "names this policy refuses",
		"allow": "exceptions that beat the block list",
	}[list]

	c := &cobra.Command{
		Use:   list + " <name>...",
		Short: map[string]string{"add": "Add to the ", "rm": "Remove from the "}[mode] + short,
		Args:  cobra.MinimumNArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			return mutate(c, func(cfg *Config) error {
				p, err := targetPolicy(cfg, policy)
				if err != nil {
					return err
				}
				current := &p.Block
				if list == "allow" {
					current = &p.Allow
				}
				for _, raw := range args {
					name := NormalizeName(raw)
					if err := checkName(name); err != nil {
						return fmt.Errorf("%q: %v", raw, err)
					}
					if mode == "add" {
						*current = append(*current, name)
						continue
					}
					if !removeName(current, name) {
						return fmt.Errorf("%q is not in %s's %s list", name, p.Name, list)
					}
				}
				cfg.SetPolicy(p)
				return nil
			})
		},
	}

	c.Flags().StringVar(&policy, "policy", "",
		"which policy to edit (defaults to the only one, or the default policy)")
	_ = c.RegisterFlagCompletionFunc("policy", cli.CompleteFlag(policyNames))
	if mode == "rm" {
		// Removing a name should suggest the names that are there. Adding one
		// cannot: the whole point is that it is not in the list yet.
		c.ValidArgsFunction = cli.CompleteEach(namesIn(list))
	}
	return c
}

// namesIn completes from the list the command would edit, resolving the same
// target policy the edit itself will (see targetPolicy).
func namesIn(list string) cli.Fetcher {
	return func(c *cobra.Command) ([]string, error) {
		cfg, err := loadConfig(c)
		if err != nil {
			return nil, err
		}
		name, _ := c.Flags().GetString("policy")
		p, err := targetPolicy(&cfg, name)
		if err != nil {
			return nil, err
		}
		if list == "allow" {
			return p.Allow, nil
		}
		return p.Block, nil
	}
}

// targetPolicy resolves which policy an edit applies to.
//
// Naming one is only necessary once there is more than one, which keeps the
// simple case simple: a house with a single blocklist never types --policy.
func targetPolicy(cfg *Config, name string) (Policy, error) {
	if name != "" {
		p, ok := cfg.Policy(name)
		if !ok {
			return Policy{}, unknownPolicy(cfg, name)
		}
		return p, nil
	}
	switch len(cfg.Policies) {
	case 0:
		// Created rather than refused: `olr dns add block ads.example` on a
		// fresh box should work, and inventing the obvious default is better
		// than an error telling them to run a different command first.
		return Policy{Name: "default"}, nil
	case 1:
		return cfg.Policies[0], nil
	}
	if p, ok := cfg.DefaultPolicy(); ok {
		return p, nil
	}
	names := make([]string, 0, len(cfg.Policies))
	for _, p := range cfg.Policies {
		names = append(names, p.Name)
	}
	return Policy{}, fmt.Errorf(
		"there are %d policies and none of them is the default, so --policy is required (have %s)",
		len(cfg.Policies), strings.Join(names, ", "))
}

func removeName(list *[]string, name string) bool {
	for i, n := range *list {
		if n == name {
			*list = append((*list)[:i], (*list)[i+1:]...)
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------- lifecycle

func enableCommand() *cobra.Command {
	return verb("enable", "Enable DNS", func(c *cobra.Command) {
		c.Args = cobra.NoArgs
		c.RunE = func(c *cobra.Command, _ []string) error {
			return mutate(c, func(cfg *Config) error { cfg.Enabled = true; return nil })
		}
	})
}

func disableCommand() *cobra.Command {
	return verb("disable", "Disable DNS", func(c *cobra.Command) {
		c.Args = cobra.NoArgs
		c.RunE = func(c *cobra.Command, _ []string) error {
			return mutate(c, func(cfg *Config) error { cfg.Enabled = false; return nil })
		}
	})
}

func statusCommand() *cobra.Command {
	return verb("status", "Show service state, query counts and drift", func(c *cobra.Command) {
		c.Args = cobra.NoArgs
		c.RunE = func(c *cobra.Command, _ []string) error {
			if err := cli.ReadOnly(c); err != nil {
				return err
			}
			var status statusResponse
			if err := cli.ClientFor(c).Get(ctxOf(c), statusEndpoint, &status); err != nil {
				return err
			}
			if cli.IsJSON(c) {
				return cli.JSON(c.OutOrStdout(), status)
			}
			return writeStatusText(c.OutOrStdout(), status)
		}
	})
}

func logsCommand() *cobra.Command {
	var (
		lines  int
		follow bool
		which  string
	)

	c := verb("logs", "Show resolver and relay log output", func(c *cobra.Command) {
		c.Args = cobra.NoArgs
		c.Long = "Show the DNS backends' log output.\n\n" +
			"olr stores no logs of its own — journald already does that\n" +
			"(design.md §3.4), so this streams from the journal rather than\n" +
			"reimplementing it. Both units are shown interleaved by default, which\n" +
			"is usually what you want: a relay failing because its upstream died\n" +
			"reads as one story across the two."
		c.RunE = func(c *cobra.Command, _ []string) error {
			// The one command here that does not go through olrd, and it does
			// not because there is nothing for olrd to add: the logs are
			// journald's, the unit names are constants, and streaming a follow
			// through the API would be reimplementing journalctl over HTTP.
			b := Backend{}
			units := b.Units()
			switch which {
			case "":
			case "resolver":
				units = []string{b.ResolverUnit()}
			case "relay":
				units = []string{b.RelayUnit()}
			default:
				return fmt.Errorf("--unit: want resolver or relay, got %q", which)
			}

			journalctl, err := exec.LookPath("journalctl")
			if err != nil {
				return fmt.Errorf(
					"journalctl not found; this module keeps its logs in the journal, under the units %s",
					strings.Join(units, " and "))
			}

			args := []string{"-n", strconv.Itoa(lines)}
			for _, u := range units {
				args = append(args, "-u", u)
			}
			if follow {
				args = append(args, "-f")
			}

			cmd := exec.CommandContext(ctxOf(c), journalctl, args...)
			cmd.Stdout = c.OutOrStdout()
			cmd.Stderr = c.ErrOrStderr()
			return cmd.Run()
		}
	})

	c.Flags().IntVarP(&lines, "lines", "n", 50, "number of lines to show")
	c.Flags().BoolVarP(&follow, "follow", "f", false, "keep streaming new entries")
	c.Flags().StringVar(&which, "unit", "", "show one backend only: resolver or relay")
	cli.EnumFlag(c, "unit", "resolver", "relay")
	return c
}

// ---------------------------------------------------------------- plumbing

// mutate is the shape every change shares: load, edit, plan, then apply unless
// asked not to.
//
// Applying happens on return, with no staged commit (design.md §5.1), so the
// diff and the impact are printed either way — the operator sees what happened
// rather than only that something did.
func mutate(c *cobra.Command, edit func(*Config) error) error {
	if err := cli.ValidateOutput(c); err != nil {
		return err
	}
	ctx, client := ctxOf(c), cli.ClientFor(c)

	cfg, err := loadConfig(c)
	if err != nil {
		return err
	}
	if err := edit(&cfg); err != nil {
		return err
	}

	// A dry run asks what would happen and stops. This is the HTTP spelling of
	// the same question the WebUI asks before every edit (§5.1/§5.3.3), and on
	// this module it matters more than most: the difference between "reload"
	// and "the whole house loses name resolution" is one field.
	if cli.DryRun(c) {
		var plan planView
		if err := client.Post(ctx, planEndpoint, cfg, &plan); err != nil {
			return err
		}
		if cli.IsJSON(c) {
			return cli.JSON(c.OutOrStdout(), plan)
		}
		return writePlanText(c.OutOrStdout(), plan, true)
	}

	var result applyResponse
	if err := client.Put(ctx, configEndpoint+"?confirm=true", cfg, &result); err != nil {
		// Report what landed before returning the failure: there is no
		// rollback, so which steps completed is the operator's starting point
		// (design.md §5.3.2).
		writeStepsText(c.ErrOrStderr(), result.Steps)
		return err
	}
	if cli.IsJSON(c) {
		return cli.JSON(c.OutOrStdout(), result)
	}
	return writePlanText(c.OutOrStdout(), result.Plan, false)
}

// parseAddrPorts reads addresses that may or may not carry a port, supplying
// the default where they do not.
func parseAddrPorts(flag string, values []string, defaultPort uint16) ([]netip.AddrPort, error) {
	out := make([]netip.AddrPort, 0, len(values))
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if ap, err := netip.ParseAddrPort(v); err == nil {
			out = append(out, ap)
			continue
		}
		addr, err := netip.ParseAddr(v)
		if err != nil {
			return nil, fmt.Errorf("%s: %q is not an address or address:port", flag, v)
		}
		out = append(out, netip.AddrPortFrom(addr, defaultPort))
	}
	return out, nil
}

// parsePrefixes reads networks, accepting a bare address as a host route so
// that naming one client does not require typing /32.
func parsePrefixes(flag string, values []string) ([]netip.Prefix, error) {
	out := make([]netip.Prefix, 0, len(values))
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if p, err := netip.ParsePrefix(v); err == nil {
			out = append(out, p)
			continue
		}
		addr, err := netip.ParseAddr(v)
		if err != nil {
			return nil, fmt.Errorf("%s: %q is not a network or an address", flag, v)
		}
		out = append(out, netip.PrefixFrom(addr, addr.BitLen()))
	}
	return out, nil
}

func joinUpstreamModes() string { return strings.Join(upstreamModeNames(), "|") }

func upstreamModeNames() []string {
	parts := make([]string, 0, len(UpstreamModes()))
	for _, m := range UpstreamModes() {
		parts = append(parts, string(m))
	}
	return parts
}

func joinBlockResponses() string { return strings.Join(blockResponseNames(), "|") }

func blockResponseNames() []string {
	parts := make([]string, 0, len(BlockResponses()))
	for _, r := range BlockResponses() {
		parts = append(parts, string(r))
	}
	return parts
}

// ------------------------------------------------- unknown objects (R8)

// unknownPolicy shares cli.UnknownObject's phrasing with the other modules, and
// with it the habit of naming the policies that do exist.
func unknownPolicy(cfg *Config, name string) error {
	return cli.UnknownObject("policy", name, "olr dns add policy <name>", policyNameList(cfg))
}

func policyNameList(cfg *Config) []string {
	out := make([]string, 0, len(cfg.Policies))
	for _, p := range cfg.Policies {
		out = append(out, p.Name)
	}
	return out
}

func unknownHost(cfg *Config, name string) error {
	return cli.UnknownObject("local name", name, "olr dns add host <name> <address>", hostNameList(cfg))
}

func hostNameList(cfg *Config) []string {
	out := make([]string, 0, len(cfg.Hosts))
	for _, h := range cfg.Hosts {
		out = append(out, h.Name)
	}
	return out
}

// ------------------------------------------------- completion (R7)

func policyNames(c *cobra.Command) ([]string, error) {
	cfg, err := loadConfig(c)
	if err != nil {
		return nil, err
	}
	return policyNameList(&cfg), nil
}

func hostNames(c *cobra.Command) ([]string, error) {
	cfg, err := loadConfig(c)
	if err != nil {
		return nil, err
	}
	return hostNameList(&cfg), nil
}

// Package dhcp is the DHCP module.
//
// It owns dnsmasq and nothing else: DNS stays with the dns module and unbound
// (design.md §4.2).
package dhcp

import (
	"context"
	"fmt"
	"io"
	"net/netip"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/open-linux-router/open-linux-router/internal/cli"
	"github.com/open-linux-router/open-linux-router/internal/core"
)

// Every command here is a client of olrd (design.md §6.1). None of them touches
// the config document, the rendered files or systemd directly, and that is the
// point rather than an implementation detail: a CLI that wrote the system on its
// own would be a second writer, holding a lock olrd cannot see, running its own
// copy of the validation rules, and publishing none of the change events the UI
// listens for. §3.6's "one global apply lock" is only true if there is one
// process doing the applying.
//
// The visible consequence is that these commands need olrd running. That is
// consistent with §6.1's two tiers — the `Service:` commands are the tier that
// works with olrd stopped, and they are grouped separately for that reason.

// Command returns the module's command tree. Mounted explicitly by cmd/olr.
func Command() *cobra.Command {
	return cli.NewModule("dhcp", "DHCP server (dnsmasq)",
		showCommand(),
		setCommand(),
		addCommand(),
		rmCommand(),
		statusCommand(),
		logsCommand(),
		enableCommand(),
		disableCommand(),
		// See internal/dns/cli.go: built in internal/cli because the same
		// dnsmasq blocks both modules and both must say the same thing about it.
		cli.FixCommand(ModuleName, "DHCP's"),
	)
}

// Endpoints this module's commands call. Spelled once so a rename cannot leave
// half the commands pointing at the old path.
const (
	configEndpoint = core.APIPrefix + "/" + ModuleName + "/config"
	planEndpoint   = core.APIPrefix + "/" + ModuleName + "/plan"
	statusEndpoint = core.APIPrefix + "/" + ModuleName + "/status"
	leasesEndpoint = core.APIPrefix + "/" + ModuleName + "/leases"
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
	c := verb("show", "Show DHCP configuration", func(c *cobra.Command) {
		c.Args = cobra.NoArgs
		c.RunE = func(c *cobra.Command, _ []string) error {
			if err := cli.ValidateOutput(c); err != nil {
				return err
			}
			// --dry-run on `show` is the drift question: plan stored intent
			// against reality and print what does not match (design.md §5.4).
			// This module used to ignore the flag here while the other two
			// answered it — the same flag, on the same verb, meaning two
			// different things depending on which module you were in.
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
		showPoolsCommand(),
		showPoolCommand(),
		showReservationsCommand(),
		showReservationCommand(),
		showLeasesCommand(),
	)
	return c
}

func showPoolsCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "pools",
		Short: "List address pools",
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
				return cli.JSON(c.OutOrStdout(), cfg.Pools)
			}
			return writePoolsText(c.OutOrStdout(), cfg.Pools)
		},
	}
}

// showPoolCommand and showReservationCommand are the detail halves docs/cli.md
// R6 requires: this module could list both objects but could not show one.
func showPoolCommand() *cobra.Command {
	c := &cobra.Command{
		Use:   "pool <network>",
		Short: "Show one pool in full",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			if err := cli.ReadOnly(c); err != nil {
				return err
			}
			cfg, err := loadConfig(c)
			if err != nil {
				return err
			}
			p, ok := cfg.Pool(args[0])
			if !ok {
				return unknownPool(&cfg, args[0])
			}
			if cli.IsJSON(c) {
				return cli.JSON(c.OutOrStdout(), p)
			}
			return writePoolText(c.OutOrStdout(), p)
		},
	}
	c.ValidArgsFunction = cli.CompleteArgs(poolInterfaces)
	return c
}

func showReservationsCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "reservations",
		Short: "List address reservations",
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
				return cli.JSON(c.OutOrStdout(), cfg.Reservations)
			}
			return writeReservationsText(c.OutOrStdout(), cfg.Reservations)
		},
	}
}

func showReservationCommand() *cobra.Command {
	c := &cobra.Command{
		Use:   "reservation <mac>",
		Short: "Show one reservation in full",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			if err := cli.ReadOnly(c); err != nil {
				return err
			}
			cfg, err := loadConfig(c)
			if err != nil {
				return err
			}
			r, ok := cfg.Reservation(args[0])
			if !ok {
				return unknownReservation(&cfg, args[0])
			}
			if cli.IsJSON(c) {
				return cli.JSON(c.OutOrStdout(), r)
			}
			return writeReservationText(c.OutOrStdout(), r)
		},
	}
	c.ValidArgsFunction = cli.CompleteArgs(reservationMACs)
	return c
}

func showLeasesCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "leases",
		Short: "List current leases",
		Long: "List the leases dnsmasq is currently holding.\n\n" +
			"Leases are observed, not configured: they are read from the daemon's\n" +
			"database and are never stored or revisioned by olr (design.md §6.2).",
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			if err := cli.ReadOnly(c); err != nil {
				return err
			}
			var resp leasesResponse
			if err := cli.ClientFor(c).Get(ctxOf(c), leasesEndpoint, &resp); err != nil {
				return err
			}
			if cli.IsJSON(c) {
				return cli.JSON(c.OutOrStdout(), resp)
			}
			return writeLeasesText(c.OutOrStdout(), resp)
		},
	}
}

// ---------------------------------------------------------------- set / add

func setCommand() *cobra.Command {
	c := verb("set", "Change DHCP configuration", func(c *cobra.Command) {})
	c.AddCommand(poolCommand("set"), extraConfCommand())
	return c
}

func addCommand() *cobra.Command {
	c := verb("add", "Add a reservation or pool", func(c *cobra.Command) {})
	c.AddCommand(poolCommand("add"), reservationCommand())
	return c
}

// poolFlags is the pool's field set, shared by add and set.
//
// The IPv4 and IPv6 halves are separate flags rather than one, mirroring the
// config: `--range` says what addresses to hand out, `--ipv6` says what to
// advertise, and neither implies the other. `--no-range` is what makes a
// RA-only network expressible — it was not, before, because a pool required a
// range to exist at all.
type poolFlags struct {
	rng      string
	noRange  bool
	lease    string
	gateway  string
	dns      []string
	ntp      []string
	domain   string
	ipv6     string
	ipv6MTU  int
	noMTU    bool
	options  []string
	noGate   bool
	clearDNS bool
}

func (f *poolFlags) register(c *cobra.Command) {
	c.Flags().StringVar(&f.rng, "range", "",
		"address range, e.g. 192.168.1.100-192.168.1.200 (default: derived from the network's subnet)")
	c.Flags().BoolVar(&f.noRange, "no-range", false,
		"hand out no IPv4 addresses here, leaving the network to IPv6 alone")
	c.Flags().StringVar(&f.lease, "lease", "", "lease time, e.g. 12h (default 12h)")
	c.Flags().StringVar(&f.gateway, "gateway", "", "gateway to advertise (default: the router itself)")
	c.Flags().BoolVar(&f.noGate, "no-gateway", false, "advertise the router itself as the gateway")
	// StringArray, not StringSlice (docs/cli.md R5). Slice splits on commas and
	// Array does not, and this module shipped one of each with no way to tell
	// them apart in --help — so a DHCP option whose value contained a comma was
	// silently cut in half while the address lists beside it were not.
	c.Flags().StringArrayVar(&f.dns, "dns", nil,
		"DNS server to advertise, repeatable (default: the router itself)")
	c.Flags().BoolVar(&f.clearDNS, "no-dns", false, "advertise the router itself as the DNS server")
	c.Flags().StringArrayVar(&f.ntp, "ntp", nil, "NTP server to advertise, repeatable")
	c.Flags().StringVar(&f.domain, "domain", "", "search domain to advertise")
	c.Flags().StringVar(&f.ipv6, "ipv6", "", fmt.Sprintf("IPv6 mode: %s", joinRAModes()))
	c.Flags().IntVar(&f.ipv6MTU, "ipv6-mtu", 0,
		"MTU to advertise with IPv6, e.g. 1480 when IPv6 leaves by a 6in4 tunnel")
	c.Flags().BoolVar(&f.noMTU, "no-ipv6-mtu", false, "advertise no MTU; clients use their own link's")
	c.Flags().StringArrayVar(&f.options, "option", nil, "extra DHCP option as CODE=VALUE, repeatable")

	c.MarkFlagsMutuallyExclusive("gateway", "no-gateway")
	c.MarkFlagsMutuallyExclusive("dns", "no-dns")
	c.MarkFlagsMutuallyExclusive("range", "no-range")
	c.MarkFlagsMutuallyExclusive("ipv6-mtu", "no-ipv6-mtu")
	cli.EnumFlag(c, "ipv6", raModeNames()...)
}

// apply mutates a pool with only the flags the operator actually gave.
//
// Unset flags are left alone rather than overwritten with zero values, so
// `olr dhcp set pool br-lan --domain lan` is a single-field update and does not
// silently erase the lease time (design.md §10: the relaxed schema projection
// exists for exactly this).
func (f *poolFlags) apply(c *cobra.Command, p *Pool) error {
	if c.Flags().Changed("no-range") && f.noRange {
		p.IPv4 = nil
	}
	if c.Flags().Changed("range") {
		start, end, err := parseRange(f.rng)
		if err != nil {
			return err
		}
		p.IPv4 = &PoolIPv4{Start: start, End: end}
	}
	if c.Flags().Changed("lease") {
		d, err := ParseDuration(f.lease)
		if err != nil {
			return fmt.Errorf("--lease: %w", err)
		}
		p.LeaseTime = d
	}
	if c.Flags().Changed("no-gateway") && f.noGate {
		p.Gateway = nil
	}
	if c.Flags().Changed("gateway") {
		gw, err := netip.ParseAddr(f.gateway)
		if err != nil {
			return fmt.Errorf("--gateway: %q is not an IP address", f.gateway)
		}
		p.Gateway = &gw
	}
	if c.Flags().Changed("no-dns") && f.clearDNS {
		p.DNS = nil
	}
	if c.Flags().Changed("dns") {
		addrs, err := parseAddrs("--dns", f.dns)
		if err != nil {
			return err
		}
		p.DNS = addrs
	}
	if c.Flags().Changed("ntp") {
		addrs, err := parseAddrs("--ntp", f.ntp)
		if err != nil {
			return err
		}
		p.NTP = addrs
	}
	if c.Flags().Changed("domain") {
		p.Domain = f.domain
	}
	if c.Flags().Changed("ipv6") {
		mode := RAMode(f.ipv6)
		if !mode.Valid() {
			return fmt.Errorf("--ipv6: unknown mode %q (want %s)", f.ipv6, joinRAModes())
		}
		if mode.OrDefault() == RAOff {
			p.IPv6 = nil
		} else {
			// The MTU survives a mode change; only --no-ipv6-mtu drops it.
			v6 := PoolIPv6{Mode: mode}
			if p.IPv6 != nil {
				v6.MTU = p.IPv6.MTU
			}
			p.IPv6 = &v6
		}
	}
	switch {
	case f.noMTU:
		if p.IPv6 != nil {
			p.IPv6.MTU = 0
		}
	case c.Flags().Changed("ipv6-mtu"):
		if p.IPv6 == nil {
			return fmt.Errorf("--ipv6-mtu needs IPv6 on this network; set --ipv6 %s too", RASLAAC)
		}
		p.IPv6.MTU = f.ipv6MTU
	}
	if c.Flags().Changed("option") {
		opts, err := parseOptions(f.options)
		if err != nil {
			return err
		}
		p.Options = opts
	}
	return nil
}

func poolCommand(mode string) *cobra.Command {
	flags := &poolFlags{}

	short := "Serve addresses on a network"
	if mode == "set" {
		short = "Change what a network serves"
	}

	c := &cobra.Command{
		Use:   "pool <network>",
		Short: short,
		Long: "Serve addresses on a network.\n\n" +
			"The range is optional: left out, it is derived from the network's subnet,\n" +
			"leaving the low addresses free for clients configured by hand. `olr net show`\n" +
			"prints the range that would be derived.\n\n" +
			"IPv4 and IPv6 are set separately. --range says what addresses to hand out,\n" +
			"--ipv6 says what to advertise, and a network can do either or both.",
		Args: cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			network := args[0]
			return mutate(c, func(cfg *Config) error {
				pool, exists := cfg.Pool(network)
				switch {
				case mode == "add" && exists:
					return fmt.Errorf("%s already has a pool; use `olr dhcp set pool %s` to change it", network, network)
				case mode == "set" && !exists:
					return fmt.Errorf("%s has no pool; use `olr dhcp add pool %s` to create one", network, network)
				}
				pool.Network = network
				if mode == "add" && pool.IPv4 == nil && !flags.noRange {
					// Adding a pool without saying anything means "hand out v4
					// addresses here", with the range derived. Requiring
					// --range was the old shape's doing: there was no subnet to
					// derive from, so there was nothing to default to.
					pool.IPv4 = &PoolIPv4{}
				}
				if err := flags.apply(c, &pool); err != nil {
					return err
				}
				if pool.IPv4 == nil && pool.RA() == RAOff {
					return fmt.Errorf("a pool that serves neither IPv4 nor IPv6 does nothing; " +
						"drop --no-range, or set --ipv6")
				}
				cfg.SetPool(pool)
				return nil
			})
		},
	}
	flags.register(c)
	if mode == "set" {
		// Only `set` addresses an existing pool; `add` is naming a new one, and
		// completing it from what already exists would suggest exactly the
		// interfaces the command is about to refuse (docs/cli.md R7).
		c.ValidArgsFunction = cli.CompleteArgs(poolInterfaces)
	}
	return c
}

func reservationCommand() *cobra.Command {
	var ip, hostname, lease string

	c := &cobra.Command{
		Use:   "reservation <mac>",
		Short: "Reserve an address for a client",
		Long: "Reserve an address for a client.\n\n" +
			"Reserving an address outside the pool's dynamic range is the safer habit:\n" +
			"it cannot then collide with an address the pool hands out.",
		// The MAC is positional (docs/cli.md R2). It was --mac here while
		// `rm reservation <mac>` took it positionally, so the two halves of one
		// object's lifecycle disagreed about what identifies a reservation —
		// and identity in a flag is identity a shell cannot complete.
		Args: cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			return mutate(c, func(cfg *Config) error {
				normalized, err := NormalizeMAC(args[0])
				if err != nil {
					return fmt.Errorf("%q: %w", args[0], err)
				}
				addr, err := netip.ParseAddr(ip)
				if err != nil {
					return fmt.Errorf("--ip: %q is not an IP address", ip)
				}
				res := Reservation{MAC: normalized, IP: addr, Hostname: hostname}
				if lease != "" {
					d, err := ParseDuration(lease)
					if err != nil {
						return fmt.Errorf("--lease: %w", err)
					}
					res.LeaseTime = d
				}
				cfg.SetReservation(res)
				return nil
			})
		},
	}

	c.Flags().StringVar(&ip, "ip", "", "address to reserve (required)")
	c.Flags().StringVar(&hostname, "hostname", "", "hostname to assign the client")
	c.Flags().StringVar(&lease, "lease", "", "lease time for this client")
	_ = c.MarkFlagRequired("ip")
	return c
}

func extraConfCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "extra-conf <file>",
		Short: "Replace the dnsmasq passthrough configuration",
		Long: "Replace the dnsmasq passthrough configuration from a file, or from\n" +
			"standard input when the file is `-`. Pass an empty file to clear it.\n\n" +
			"This is the module's declared escape hatch (design.md §3.2 rule 5). It is\n" +
			"revisioned along with everything else, which is the point: settings olr\n" +
			"does not model stay visible to olr rather than being hand-edited into a\n" +
			"file olr never reads. It may not set directives the module renders itself.",
		Args: cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			var (
				data []byte
				err  error
			)
			if args[0] == "-" {
				data, err = io.ReadAll(c.InOrStdin())
			} else {
				data, err = os.ReadFile(args[0])
			}
			if err != nil {
				return err
			}
			return mutate(c, func(cfg *Config) error {
				cfg.ExtraConf = strings.TrimRight(string(data), "\n")
				return nil
			})
		},
	}
}

// ---------------------------------------------------------------- rm

func rmCommand() *cobra.Command {
	c := verb("rm", "Remove a reservation or pool", func(c *cobra.Command) {})

	rmPool := &cobra.Command{
		Use:   "pool <network>",
		Short: "Remove a network's pool",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			return mutate(c, func(cfg *Config) error {
				if !cfg.RemovePool(args[0]) {
					return unknownPool(cfg, args[0])
				}
				return nil
			})
		},
	}
	rmPool.ValidArgsFunction = cli.CompleteArgs(poolInterfaces)

	rmReservation := &cobra.Command{
		Use:   "reservation <mac>",
		Short: "Remove a reservation",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			return mutate(c, func(cfg *Config) error {
				if !cfg.RemoveReservation(args[0]) {
					return unknownReservation(cfg, args[0])
				}
				return nil
			})
		},
	}
	rmReservation.ValidArgsFunction = cli.CompleteArgs(reservationMACs)

	c.AddCommand(rmPool, rmReservation)
	return c
}

// ---------------------------------------------------------------- lifecycle

func enableCommand() *cobra.Command {
	return verb("enable", "Enable the DHCP service", func(c *cobra.Command) {
		c.Args = cobra.NoArgs
		c.RunE = func(c *cobra.Command, _ []string) error {
			return mutate(c, func(cfg *Config) error { cfg.Enabled = true; return nil })
		}
	})
}

func disableCommand() *cobra.Command {
	return verb("disable", "Disable the DHCP service", func(c *cobra.Command) {
		c.Args = cobra.NoArgs
		c.RunE = func(c *cobra.Command, _ []string) error {
			return mutate(c, func(cfg *Config) error { cfg.Enabled = false; return nil })
		}
	})
}

func statusCommand() *cobra.Command {
	return verb("status", "Show service state, leases and drift", func(c *cobra.Command) {
		c.Args = cobra.NoArgs
		c.RunE = func(c *cobra.Command, _ []string) error {
			if err := cli.ReadOnly(c); err != nil {
				return err
			}
			ctx, client := ctxOf(c), cli.ClientFor(c)

			// Two endpoints because they are two questions and either can fail
			// on its own: §5.4 keeps drift and backend liveness separate, and
			// pool occupancy comes from the lease database rather than from
			// systemd. Neither answer is allowed to suppress the other.
			var status statusResponse
			if err := client.Get(ctx, statusEndpoint, &status); err != nil {
				return err
			}
			var leases leasesResponse
			if err := client.Get(ctx, leasesEndpoint, &leases); err != nil {
				return err
			}

			if cli.IsJSON(c) {
				return cli.JSON(c.OutOrStdout(), map[string]any{
					"status": status, "leases": leases,
				})
			}
			return writeStatusText(c.OutOrStdout(), status, leases)
		}
	})
}

func logsCommand() *cobra.Command {
	var (
		lines  int
		follow bool
	)

	c := verb("logs", "Show dnsmasq log output", func(c *cobra.Command) {
		c.Args = cobra.NoArgs
		c.Long = "Show the DHCP daemon's log output.\n\n" +
			"olr stores no logs of its own — journald already does that\n" +
			"(design.md §3.4), so this streams from the journal rather than\n" +
			"reimplementing it. Anything journalctl can do to these logs, it can\n" +
			"still do directly."
		c.RunE = func(c *cobra.Command, _ []string) error {
			// The one command here that does not go through olrd, and it does
			// not because there is nothing for olrd to add: the logs are
			// journald's, the unit name is a constant, and streaming a follow
			// through the API would be reimplementing journalctl over HTTP.
			// This is a read of somebody else's data, not a write to ours.
			unit := Dnsmasq{}.Unit()

			journalctl, err := exec.LookPath("journalctl")
			if err != nil {
				return fmt.Errorf("journalctl not found; this module keeps its logs in the journal, under the unit %s", unit)
			}

			args := []string{"-u", unit, "-n", strconv.Itoa(lines)}
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
	return c
}

// ---------------------------------------------------------------- plumbing

// mutate is the shape every change shares: load, edit, plan, then apply unless
// asked not to.
//
// Applying happens on return, with no staged commit (design.md §5.1), so the
// diff and the impact are printed either way — the operator sees what happened
// rather than only that something did.
// It is a read-modify-write against the API and takes no lock of its own. The
// window between the GET and the PUT is real, and it is the same window the
// WebUI has; closing it belongs in core — as a revision the write is
// conditional on — rather than in a lock this process holds and olrd cannot see,
// which is what used to be here and never excluded the daemon at all.
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
	// the same question the WebUI asks before every edit (§5.1/§5.3.3).
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
		// (design.md §5.3.2). The body carries them even on a 500, which is why
		// the client decodes it either way.
		writeStepsText(c.ErrOrStderr(), result.Steps)
		return err
	}
	if cli.IsJSON(c) {
		return cli.JSON(c.OutOrStdout(), result)
	}
	return writePlanText(c.OutOrStdout(), result.Plan, false)
}

func parseRange(s string) (start, end netip.Addr, err error) {
	first, last, ok := strings.Cut(s, "-")
	if !ok {
		return start, end, fmt.Errorf("--range: %q is not START-END, e.g. 192.168.1.100-192.168.1.200", s)
	}
	start, err = netip.ParseAddr(strings.TrimSpace(first))
	if err != nil {
		return start, end, fmt.Errorf("--range: %q is not an IP address", first)
	}
	end, err = netip.ParseAddr(strings.TrimSpace(last))
	if err != nil {
		return start, end, fmt.Errorf("--range: %q is not an IP address", last)
	}
	return start, end, nil
}

func parseAddrs(flag string, values []string) ([]netip.Addr, error) {
	out := make([]netip.Addr, 0, len(values))
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		a, err := netip.ParseAddr(v)
		if err != nil {
			return nil, fmt.Errorf("%s: %q is not an IP address", flag, v)
		}
		out = append(out, a)
	}
	return out, nil
}

func parseOptions(values []string) ([]Option, error) {
	out := make([]Option, 0, len(values))
	for _, v := range values {
		name, value, ok := strings.Cut(v, "=")
		if !ok {
			return nil, fmt.Errorf("--option: %q is not CODE=VALUE", v)
		}
		name, value = strings.TrimSpace(name), strings.TrimSpace(value)
		if name == "" || value == "" {
			return nil, fmt.Errorf("--option: %q is not CODE=VALUE", v)
		}
		out = append(out, Option{Option: name, Value: value})
	}
	return out, nil
}

func joinRAModes() string { return strings.Join(raModeNames(), "|") }

func raModeNames() []string {
	modes := RAModes()
	parts := make([]string, len(modes))
	for i, m := range modes {
		parts[i] = string(m)
	}
	return parts
}

// ------------------------------------------------- unknown objects (R8)

// unknownPool and unknownReservation share cli.UnknownObject's phrasing, and
// with it the habit of naming what does exist — this module used to say
// "br-lan has no pool" and leave the operator to go and look.

func unknownPool(cfg *Config, network string) error {
	return cli.UnknownObject("pool", network, "olr dhcp add pool <network>", poolNames(cfg))
}

func unknownReservation(cfg *Config, mac string) error {
	return cli.UnknownObject("reservation", mac, "olr dhcp add reservation <mac>", macNames(cfg))
}

func poolNames(cfg *Config) []string {
	out := make([]string, 0, len(cfg.Pools))
	for _, p := range cfg.Pools {
		out = append(out, p.Network)
	}
	return out
}

func macNames(cfg *Config) []string {
	out := make([]string, 0, len(cfg.Reservations))
	for _, r := range cfg.Reservations {
		out = append(out, r.MAC)
	}
	return out
}

// ------------------------------------------------- completion (R7)

func poolInterfaces(c *cobra.Command) ([]string, error) {
	cfg, err := loadConfig(c)
	if err != nil {
		return nil, err
	}
	return poolNames(&cfg), nil
}

func reservationMACs(c *cobra.Command) ([]string, error) {
	cfg, err := loadConfig(c)
	if err != nil {
		return nil, err
	}
	return macNames(&cfg), nil
}

func humanTime(t time.Time) string {
	if t.IsZero() {
		return "never"
	}
	return t.Local().Format("2006-01-02 15:04:05")
}

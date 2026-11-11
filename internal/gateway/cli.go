package gateway

import (
	"context"
	"fmt"
	"net/http"
	"net/netip"
	"net/url"
	"strings"

	"github.com/spf13/cobra"

	"github.com/open-linux-router/open-linux-router/internal/cli"
	"github.com/open-linux-router/open-linux-router/internal/core"
	"github.com/open-linux-router/open-linux-router/internal/gateway/nat"
)

// Every command here is a client of olrd (design.md §6.1). None of them touches
// the kernel directly, and on this module that is not merely a convention: a
// CLI that programmed nftables and the RPDB itself would be a second writer of
// the one thing this module exists to own, holding a lock olrd cannot see and
// racing the health prober.

// Command returns the module's command tree. Mounted explicitly by cmd/olr.
func Command() *cobra.Command {
	show, set, add, rm, status := showCommand(), setCommand(), addCommand(), rmCommand(), statusCommand()

	// The NAT half's objects hang off this module's verbs rather than off a
	// module of their own: `olr gateway add forward web`, `olr gateway show
	// forwards`. docs/cli.md §1's four positions, with the object to the right
	// of the verb — and one module keeping one command tree, however many
	// packages implement it.
	forwards := nat.Subcommands()
	show.AddCommand(forwards.Show...)
	add.AddCommand(forwards.Add...)
	set.AddCommand(forwards.Set...)
	rm.AddCommand(forwards.Rm...)
	status.AddCommand(forwards.Status...)

	return cli.NewModule("gateway", "The boundary with the internet: exits, NAT and port forwards",
		show, set, add, rm, status,
		enableCommand(),
		disableCommand(),
	)
}

// Endpoints this module's commands call. Spelled once so a rename cannot leave
// half the commands pointing at the old path.
const (
	configEndpoint  = core.APIPrefix + "/" + ModuleName + "/config"
	planEndpoint    = core.APIPrefix + "/" + ModuleName + "/plan"
	applyEndpoint   = core.APIPrefix + "/" + ModuleName + "/apply"
	statusEndpoint  = core.APIPrefix + "/" + ModuleName + "/status"
	trafficEndpoint = core.APIPrefix + "/" + ModuleName + "/traffic"
)

func ctxOf(c *cobra.Command) context.Context {
	if ctx := c.Context(); ctx != nil {
		return ctx
	}
	return context.Background()
}

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
	c := showConfigCommand()
	c.AddCommand(showExitsCommand(), showExitCommand(), showTrafficCommand())
	return c
}

// showExitsCommand and showExitCommand are the list/detail pair docs/cli.md R6
// requires. `olr gateway show` remains the whole-module view.
func showExitsCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "exits",
		Short: "List the ways out of this box",
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
				return cli.JSON(c.OutOrStdout(), cfg.Exits)
			}
			return writeExitsText(c.OutOrStdout(), cfg)
		},
	}
}

func showExitCommand() *cobra.Command {
	c := &cobra.Command{
		Use:   "exit <name>",
		Short: "Show one exit in full",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			if err := cli.ReadOnly(c); err != nil {
				return err
			}
			cfg, err := loadConfig(c)
			if err != nil {
				return err
			}
			e, ok := cfg.Find(args[0])
			if !ok {
				return unknownExit(&cfg, args[0])
			}
			if cli.IsJSON(c) {
				return cli.JSON(c.OutOrStdout(), e)
			}
			return writeExitText(c.OutOrStdout(), cfg, e)
		},
	}
	c.ValidArgsFunction = cli.CompleteArgs(exitNames)
	return c
}

func showTrafficCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "traffic",
		Short: "Show how much each client has used, and through which way out",
		Long: "Show how much each client has sent and received.\n\n" +
			"Counted in the kernel and read on every request — never stored, so the\n" +
			"numbers start from zero when the router reboots. What they cannot see is\n" +
			"printed underneath rather than left to be discovered, because every one of\n" +
			"those limits makes a number smaller than you would expect.",
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			if err := cli.ReadOnly(c); err != nil {
				return err
			}
			var traffic trafficView
			if err := cli.ClientFor(c).Get(ctxOf(c), trafficEndpoint, &traffic); err != nil {
				return err
			}
			if cli.IsJSON(c) {
				return cli.JSON(c.OutOrStdout(), traffic)
			}
			return writeTrafficText(c.OutOrStdout(), traffic)
		},
	}
}

func showConfigCommand() *cobra.Command {
	return verb("show", "Show exits, port forwards, and which networks use what", func(c *cobra.Command) {
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
}

// ---------------------------------------------------------------- set

func setCommand() *cobra.Command {
	c := verb("set", "Change an exit assignment or a port forward", func(*cobra.Command) {})
	c.AddCommand(setDefaultCommand(), setViaCommand(), setClientCommand(), destinationCommand(false), setStatsCommand(), setIPv6ForwardingCommand())
	return c
}

// setStatsCommand is §7.5's visible off switch.
//
// A subcommand of `set` rather than a flag on `enable`/`disable`, because
// accounting has its own lifetime: turning routing off leaves it counting, and
// turning it off leaves routing alone.
func setStatsCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "traffic-counting on|off",
		Short: "Count how much each client uses",
		Long: "Turn per-client traffic counting on or off.\n\n" +
			"Independent of everything else here: with no way out configured it still\n" +
			"counts, it just has nothing to attribute the traffic to. Turning it off\n" +
			"discards the running totals, which are only ever held in memory anyway.",
		Args:      cobra.ExactArgs(1),
		ValidArgs: []string{"on", "off"},
		RunE: func(c *cobra.Command, args []string) error {
			var on bool
			switch args[0] {
			case "on":
				on = true
			case "off":
				on = false
			default:
				return fmt.Errorf("want on or off, got %q", args[0])
			}
			return patchConfig(c, map[string]any{"stats": on})
		},
	}
}

// setIPv6ForwardingCommand is Config.IPv6Forwarding's switch. A sibling of
// traffic-counting rather than a flag on `enable`, because it is a decision of
// its own: turning the gateway on does not turn this on, and nothing else here
// turns it on either.
func setIPv6ForwardingCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "ipv6-forwarding on|off",
		Short: "Let IPv6 pass through this router",
		Long: "Turn IPv6 forwarding on or off.\n\n" +
			"On costs something IPv4 forwarding does not: the kernel drops the IPv6\n" +
			"default route this router learned from upstream on every interface whose\n" +
			"accept_ra is not 2, and stops listening for new ones. The plan names those\n" +
			"interfaces before anything is written. Off writes 0 — it does not merely\n" +
			"stop olr managing the setting.",
		Args:      cobra.ExactArgs(1),
		ValidArgs: []string{"on", "off"},
		RunE: func(c *cobra.Command, args []string) error {
			var on bool
			switch args[0] {
			case "on":
				on = true
			case "off":
				on = false
			default:
				return fmt.Errorf("want on or off, got %q", args[0])
			}
			return patchConfig(c, map[string]any{"ipv6_forwarding": on})
		},
	}
}

func setDefaultCommand() *cobra.Command {
	var noExit bool

	c := &cobra.Command{
		Use:   "default [<exit>]",
		Short: "Set the exit everything uses unless something more specific says otherwise",
		Long: "Set the box-wide exit — the top rung of the ladder.\n\n" +
			"Every network follows this unless it has been given an exit of its own,\n" +
			"so `set default Proxy` plus one `rm via` for the NAS is how you say\n" +
			"\"everything through Proxy except that\". Use --no-exit to go back to the\n" +
			"box's own internet connection.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			if noExit == (len(args) == 1) {
				return fmt.Errorf("give an exit name, or --no-exit for the box's own connection")
			}
			// No check here that the exit exists. Validate does it on the
			// daemon's side against the document it is about to write, and says
			// which exits there are — checking first would be the same rule in a
			// worse place, and one the WebUI would still not get.
			name := ""
			if !noExit {
				name = args[0]
			}
			return patchConfig(c, map[string]any{"default": name})
		},
	}
	// --no-exit rather than --none (docs/cli.md R4): the negative flag names the
	// field it clears, so it reads the same way here as --no-probe does below.
	c.Flags().BoolVar(&noExit, "no-exit", false, "use the box's own internet connection")
	c.ValidArgsFunction = cli.CompleteArgs(exitNames)
	return c
}

func setViaCommand() *cobra.Command {
	c := &cobra.Command{
		Use:   "via <network> <exit>",
		Short: "Send one network's traffic through an exit",
		Long: "Send one network's traffic through an exit.\n\n" +
			"The network is an interface name for now; it becomes a network name when\n" +
			"the link module lands, and the stored configuration will not need changing.\n" +
			"Use `olr gateway rm via <network>` to go back to following the box-wide\n" +
			"setting.",
		Args: cobra.ExactArgs(2),
		RunE: func(c *cobra.Command, args []string) error {
			// As with `set default`, naming an exit that does not exist is
			// Validate's answer to give, not this one's.
			return send(c, http.MethodPut, assignmentEndpoint(args[0]),
				assignmentBody{Exit: args[1]})
		},
	}
	// The first positional is an interface name, which this module does not own
	// a list of until `link` lands; the second is an exit, which it does.
	c.ValidArgsFunction = cli.CompleteArgs(nil, exitNames)
	return c
}

func setClientCommand() *cobra.Command {
	var direct bool
	c := &cobra.Command{
		Use:     "client <mac> [<exit>]",
		Aliases: []string{"device"},
		Short:   "Give one client its own way out",
		Long: "Override the network setting for one client by MAC address. Use --direct " +
			"for this router's normal connection, or `olr gateway rm client <mac>` to inherit again.",
		Args: cobra.RangeArgs(1, 2),
		RunE: func(c *cobra.Command, args []string) error {
			if direct == (len(args) == 2) {
				return fmt.Errorf("give an exit name or --direct for this router's normal connection")
			}
			mac, err := core.NormalizeMAC(args[0])
			if err != nil {
				return err
			}
			exit := ""
			if !direct {
				exit = args[1]
			}
			return send(c, http.MethodPut, clientEndpoint(mac), assignmentBody{Exit: exit})
		},
		ValidArgsFunction: cli.CompleteArgs(assignedClients, exitNames),
	}
	c.Flags().BoolVar(&direct, "direct", false, "use this router's normal connection")
	return c
}

// ---------------------------------------------------------------- add / rm

func addCommand() *cobra.Command {
	c := verb("add", "Add an exit or a port forward", func(*cobra.Command) {})
	c.AddCommand(addExitCommand())
	return c
}

func rmCommand() *cobra.Command {
	c := verb("rm", "Remove an exit, a port forward, or a network's assignment", func(*cobra.Command) {})
	c.AddCommand(rmExitCommand(), rmViaCommand(), rmClientCommand(), destinationCommand(true))
	return c
}

func addExitCommand() *cobra.Command {
	var f exitFlags

	c := &cobra.Command{
		Use:   "exit <name>",
		Short: "Add or replace an exit",
		Long: "Add or replace an exit: a way out of this box.\n\n" +
			"An exit is anything that accepts traffic addressed somewhere else and\n" +
			"takes responsibility for delivering it — a WireGuard or proxy interface,\n" +
			"another box on the network, or nothing at all.\n\n" +
			"  olr gateway add exit Proxy  --next-hop 192.168.1.50 --probe 1.1.1.1:443\n" +
			"  olr gateway add exit Office --interface wg0\n" +
			"  olr gateway add exit Blocked --blocked\n\n" +
			"A SOCKS5 or HTTP proxy port cannot be an exit: the client has to ask it,\n" +
			"in its own protocol, so it carries no UDP, no ICMP and nothing that does\n" +
			"not know about it. Point tun2socks at it and use the interface that makes.",
		Args: cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			// The one command here that still reads before it writes, because
			// the flags are a patch: `--probe` alone means "keep everything
			// else", and f.apply needs the current exit to keep it onto. Only
			// the flags are resolved here — Upsert, the slot it preserves, and
			// the rename cascade all still happen on the daemon's side, under
			// the lock, in the single request below.
			cfg, err := loadConfig(c)
			if err != nil {
				return err
			}
			e, existing := cfg.Find(args[0])
			e.Name = args[0]
			if err := f.apply(c, &e, existing); err != nil {
				return err
			}
			return send(c, http.MethodPut, exitEndpoint(args[0]), e)
		},
	}
	f.register(c)
	return c
}

type exitFlags struct {
	iface     string
	nextHop   string
	dev       string
	blocked   bool
	ipv6      string
	onFailure string
	snat      bool
	noSNAT    bool
	probe     string
	noProbe   bool
	interval  string
	timeout   string
}

func (f *exitFlags) register(c *cobra.Command) {
	c.Flags().StringVar(&f.iface, "interface", "",
		"send traffic out this device, e.g. wg0 or a proxy's TUN")
	c.Flags().StringVar(&f.nextHop, "next-hop", "",
		"hand traffic to this box, e.g. 192.168.1.50")
	c.Flags().StringVar(&f.dev, "dev", "",
		"the interface the next hop is reached through (usually derived)")
	c.Flags().BoolVar(&f.blocked, "blocked", false,
		"refuse traffic, so applications fail immediately instead of hanging")
	c.Flags().StringVar(&f.ipv6, "ipv6", "",
		"what happens to IPv6: "+join(IPv6Modes())+" (default block)")
	c.Flags().StringVar(&f.onFailure, "on-failure", "",
		"what happens when the health check fails: "+join(FailureModes())+" (default block)")

	// A pair, both defaulting false, rather than one flag defaulting true
	// (docs/cli.md R3). pflag takes no space-separated value for a boolean, so
	// the old `--snat` default-true spelled its own negation `--snat=false` —
	// and `--snat false` parsed as the flag plus a second positional, which on
	// a one-argument command reported an argument-count error that named
	// neither snat nor the real mistake.
	c.Flags().BoolVar(&f.snat, "snat", false,
		"rewrite the source address of traffic sent to a next hop, so replies come back through this router (default)")
	c.Flags().BoolVar(&f.noSNAT, "no-snat", false,
		"leave the source address alone; the next hop must already route back to this box")
	c.MarkFlagsMutuallyExclusive("snat", "no-snat")

	c.Flags().StringVar(&f.probe, "probe", "",
		"health-check target on the far side of the exit, as address:port, e.g. 1.1.1.1:443")
	c.Flags().BoolVar(&f.noProbe, "no-probe", false,
		"stop health-checking this exit")
	c.MarkFlagsMutuallyExclusive("probe", "no-probe")

	// Registered on `set` rather than here; see setStatsCommand.
	c.Flags().StringVar(&f.interval, "probe-interval", "", "how often to health-check, e.g. 30s")
	c.Flags().StringVar(&f.timeout, "probe-timeout", "", "how long one health check may take, e.g. 5s")

	cli.EnumFlag(c, "ipv6", list(IPv6Modes())...)
	cli.EnumFlag(c, "on-failure", list(FailureModes())...)
}

// list is join's sibling: the same vocabulary, as values rather than prose.
func list[T ~string](values []T) []string {
	out := make([]string, len(values))
	for i, v := range values {
		out[i] = string(v)
	}
	return out
}

func (f *exitFlags) apply(c *cobra.Command, e *Exit, existing bool) error {
	changed := c.Flags().Changed

	forms := 0
	for _, set := range []bool{changed("interface"), changed("next-hop"), f.blocked} {
		if set {
			forms++
		}
	}
	switch {
	case forms > 1:
		return fmt.Errorf("an exit has one form: give --interface, --next-hop or --blocked, not more than one")
	case forms == 0 && !existing:
		return fmt.Errorf("a new exit needs a form: --interface, --next-hop or --blocked")
	}

	if forms == 1 {
		// Switching form clears the other form's fields rather than leaving
		// them behind, where they would read as still being in force.
		e.Via = Via{}
		switch {
		case changed("interface"):
			e.Via.Kind, e.Via.Interface = ViaInterface, f.iface
		case changed("next-hop"):
			hop, err := netip.ParseAddr(strings.TrimSpace(f.nextHop))
			if err != nil {
				return fmt.Errorf("--next-hop: %q is not an address", f.nextHop)
			}
			e.Via.Kind, e.Via.NextHop = ViaNextHop, &hop
		case f.blocked:
			e.Via.Kind = ViaBlocked
		}
	}
	if changed("dev") {
		e.Via.Dev = f.dev
	}

	if changed("ipv6") {
		mode := IPv6Mode(f.ipv6)
		if !mode.Valid() {
			return fmt.Errorf("--ipv6: unknown value %q (want %s)", f.ipv6, join(IPv6Modes()))
		}
		e.IPv6 = mode
	}
	if changed("on-failure") {
		mode := FailureMode(f.onFailure)
		if !mode.Valid() {
			return fmt.Errorf("--on-failure: unknown value %q (want %s)", f.onFailure, join(FailureModes()))
		}
		e.OnFailure = mode
	}
	switch {
	case changed("snat"):
		snat := f.snat
		e.SNAT = &snat
	case changed("no-snat"):
		snat := !f.noSNAT
		e.SNAT = &snat
	}

	switch {
	case changed("no-probe") && f.noProbe:
		// An explicit flag rather than `--probe ""` (docs/cli.md R4): an empty
		// string is what a shell variable that failed to expand looks like, so
		// the old spelling made "clear the health check" the thing a typo does.
		e.Probe = nil
	case changed("probe"):
		target, err := netip.ParseAddrPort(strings.TrimSpace(f.probe))
		if err != nil {
			return fmt.Errorf("--probe: %q is not an address:port, e.g. 1.1.1.1:443", f.probe)
		}
		if e.Probe == nil {
			e.Probe = &Probe{}
		}
		e.Probe.Target = target
	}
	for _, d := range []struct {
		flag  string
		value string
		into  *Duration
	}{
		{"probe-interval", f.interval, nil},
		{"probe-timeout", f.timeout, nil},
	} {
		if !changed(d.flag) {
			continue
		}
		if e.Probe == nil {
			return fmt.Errorf("--%s needs --probe as well; there is nothing to time otherwise", d.flag)
		}
		if d.flag == "probe-interval" {
			d.into = &e.Probe.Interval
		} else {
			d.into = &e.Probe.Timeout
		}
		if err := d.into.UnmarshalText([]byte(d.value)); err != nil {
			return fmt.Errorf("--%s: %v", d.flag, err)
		}
	}
	return nil
}

func rmExitCommand() *cobra.Command {
	c := &cobra.Command{
		Use:   "exit <name>",
		Short: "Remove an exit",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			// References to the name are deliberately not cleaned up; see the
			// route's own comment. A network still pointing at the deleted exit
			// is a validation error naming both ends, which beats silently
			// re-pointing somebody's phones at the modem.
			return send(c, http.MethodDelete, exitEndpoint(args[0]), nil)
		},
	}
	c.ValidArgsFunction = cli.CompleteArgs(exitNames)
	return c
}

func rmViaCommand() *cobra.Command {
	c := &cobra.Command{
		Use:   "via <network>",
		Short: "Stop overriding a network's exit, so it follows the box-wide setting",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			return send(c, http.MethodDelete, assignmentEndpoint(args[0]), nil)
		},
	}
	c.ValidArgsFunction = cli.CompleteArgs(assignedNetworks)
	return c
}

func rmClientCommand() *cobra.Command {
	return &cobra.Command{
		Use:     "client <mac>",
		Aliases: []string{"device"},
		Short:   "Return one client to its network's way out",
		Args:    cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			mac, err := core.NormalizeMAC(args[0])
			if err != nil {
				return err
			}
			return send(c, http.MethodDelete, clientEndpoint(mac), nil)
		},
		ValidArgsFunction: cli.CompleteArgs(assignedClients),
	}
}

// ---------------------------------------------------------------- lifecycle

func enableCommand() *cobra.Command {
	return verb("enable", "Apply routing policy and port forwards", func(c *cobra.Command) {
		c.Args = cobra.NoArgs
		c.RunE = func(c *cobra.Command, _ []string) error {
			return patchConfig(c, map[string]any{"enabled": true})
		}
	})
}

func disableCommand() *cobra.Command {
	c := verb("disable", "Remove routing policy and port forwards, keeping them configured", func(c *cobra.Command) {
		c.Args = cobra.NoArgs
		c.RunE = func(c *cobra.Command, _ []string) error {
			return patchConfig(c, map[string]any{"enabled": false})
		}
	})
	c.Long = "Stop programming this module into the kernel, keeping every setting.\n\n" +
		"This is one switch over two tables. As well as removing the routing\n" +
		"policy, it **closes every port you have forwarded in** — the forwards\n" +
		"stay configured and stop being programmed. docs/gateway.md §0 has why\n" +
		"there is one switch rather than two.\n\n" +
		"--dry-run shows what would come down before you commit to it."
	return c
}

func statusCommand() *cobra.Command {
	return verb("status", "Show each exit's health, what uses it, and drift", func(c *cobra.Command) {
		c.Args = cobra.NoArgs
		c.RunE = func(c *cobra.Command, _ []string) error {
			if err := cli.ReadOnly(c); err != nil {
				return err
			}
			var status statusView
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

// ---------------------------------------------------------------- plumbing

// send is the shape every change shares: one request naming the thing to change,
// then print what it did.
//
// It is one request and not three. This used to read the whole config, edit it
// here, and put it back — which meant the rule being applied lived in this
// process, so the WebUI had to carry its own copy, and the apply lock only
// covered the write half of a read-modify-write. Now the daemon does all three
// steps under the lock and this only says which change it wants.
//
// Applying happens on return, with no staged commit (design.md §5.1), so the
// diff and the impact are printed either way — the operator sees what happened
// rather than only that something did. That is also why every request here
// carries confirm=true: the disruptive gate exists for the WebUI, which can put
// the question to somebody and wait. A command that returned "this would be
// disruptive, run it again" would be a staged commit by another name, and §5.1
// says this surface does not have one. `--dry-run` is how you look first.
func send(c *cobra.Command, method, path string, body any) error {
	if err := cli.ValidateOutput(c); err != nil {
		return err
	}
	ctx, client := ctxOf(c), cli.ClientFor(c)

	// A dry run asks what would happen and stops. On this module that question
	// is worth more than on most: the difference between an edit nobody
	// notices and one that disconnects the machine you are typing on is a
	// single line of the plan (§5.1's lockout row).
	if cli.DryRun(c) {
		var plan planView
		if err := client.Do(ctx, method, path+"?dry_run=true", body, &plan); err != nil {
			return err
		}
		if cli.IsJSON(c) {
			return cli.JSON(c.OutOrStdout(), plan)
		}
		return writePlanText(c.OutOrStdout(), plan, true)
	}

	var result applyResponse
	if err := client.Do(ctx, method, path+"?confirm=true", body, &result); err != nil {
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

// patchConfig changes named scalar fields and leaves the rest alone.
//
// Scalars only, and that is the whole division: RFC 7386 merges an object key by
// key but replaces an array wholesale, so `enabled`, `default` and `stats` are
// served correctly here while `exits` and `interfaces` need the item routes.
func patchConfig(c *cobra.Command, fields map[string]any) error {
	return send(c, http.MethodPatch, configEndpoint, fields)
}

// exitEndpoint and assignmentEndpoint name one item.
func exitEndpoint(name string) string {
	return core.APIPrefix + "/" + ModuleName + "/exits/" + url.PathEscape(name)
}

func assignmentEndpoint(iface string) string {
	return core.APIPrefix + "/" + ModuleName + "/assignments/" + url.PathEscape(iface)
}

func clientEndpoint(mac string) string {
	return core.APIPrefix + "/" + ModuleName + "/clients/" + url.PathEscape(mac)
}

// unknownExit names the exits that do exist, because "no exit called X" is only
// half an answer when the reason is usually a typo.
//
// The phrasing is cli.UnknownObject's rather than this module's: three modules
// had four ways to say this, and the operator reading two of them side by side
// could not tell whether they meant the same thing (docs/cli.md R8).
func unknownExit(cfg *Config, name string) error {
	return cli.UnknownObject("exit", name, "olr gateway add exit", names(cfg.Exits))
}

func names(exits []Exit) []string {
	out := make([]string, 0, len(exits))
	for _, e := range exits {
		out = append(out, e.Name)
	}
	return out
}

// ---------------------------------------------------------------- completion

// exitNames and assignedNetworks answer "which ones are there?" for the shell
// (docs/cli.md R7). Both read through olrd like every other read, so both go
// quiet rather than erroring when it is down.

func exitNames(c *cobra.Command) ([]string, error) {
	cfg, err := loadConfig(c)
	if err != nil {
		return nil, err
	}
	return names(cfg.Exits), nil
}

func assignedNetworks(c *cobra.Command) ([]string, error) {
	cfg, err := loadConfig(c)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(cfg.Interfaces))
	for _, a := range cfg.Interfaces {
		out = append(out, a.Interface)
	}
	return out, nil
}

func assignedClients(c *cobra.Command) ([]string, error) {
	cfg, err := loadConfig(c)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(cfg.Clients))
	for _, a := range cfg.Clients {
		out = append(out, a.MAC)
	}
	return out, nil
}

package nat

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
)

// Every command here is a client of olrd (design.md §6.1). None of them touches
// the kernel directly, and on this module that is not merely a convention: a CLI
// that programmed nftables itself would be a second writer of the one table this
// module exists to own, holding a lock olrd cannot see.

// Commands are the port-forwarding subcommands, for `gateway` to mount under
// its own verbs.
//
// No `cli.NewModule` here any more and no verbs of this package's own: `olr
// firewall` is gone, and a forward is addressed the way docs/cli.md §1 lays the
// surface out — `olr gateway <verb> forward [<name>]`, with the object to the
// right of the verb. The verbs themselves stay `gateway`'s, which is what keeps
// one module to one command tree.
//
// There is no `enable` or `disable` here either. Port forwarding does not have
// a switch of its own: `gateway.Config.Enabled` governs both of the module's
// tables, and docs/gateway.md §0 records the sharp edge that follows — turning
// the module off closes every forwarded port.
type Commands struct {
	// Show, Add, Set, Rm and Status are mounted under the verb they are named
	// for.
	Show   []*cobra.Command
	Add    []*cobra.Command
	Set    []*cobra.Command
	Rm     []*cobra.Command
	Status []*cobra.Command
}

// Subcommands returns the tree this package contributes to `olr gateway`.
func Subcommands() Commands {
	return Commands{
		Show:   []*cobra.Command{showForwardsCommand(), showForwardCommand()},
		Add:    []*cobra.Command{forwardCommand("add")},
		Set:    []*cobra.Command{forwardCommand("set")},
		Rm:     []*cobra.Command{rmForwardCommand()},
		Status: []*cobra.Command{statusForwardsCommand()},
	}
}

// Endpoints these commands call. Spelled once so a rename cannot leave half of
// them pointing at the old path.
const (
	forwardsEndpoint = core.APIPrefix + "/" + Module + "/forwards"
	planEndpoint     = forwardsEndpoint + "/plan"
	statusEndpoint   = forwardsEndpoint + "/status"
)

func ctxOf(c *cobra.Command) context.Context {
	if ctx := c.Context(); ctx != nil {
		return ctx
	}
	return context.Background()
}

// loadForwards reads the stored forwards.
//
// A Config is built around them so that Find, Normalize and the printers keep
// working unchanged — but `Enabled` and `Egress` are *not* filled in, because
// they are `gateway`'s and this route does not publish them. Nothing on the
// client side reads them; if something ever does, it asks `gateway`.
func loadForwards(c *cobra.Command) (Config, error) {
	var body forwardsBody
	if err := cli.ClientFor(c).Get(ctxOf(c), forwardsEndpoint, &body); err != nil {
		return Config{}, err
	}
	return Config{Forwards: body.Forwards}, nil
}

// ---------------------------------------------------------------- show

// showForwardsCommand and showForwardCommand are the list/detail pair
// docs/cli.md R6 requires.
func showForwardsCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "forwards",
		Short: "List the ports being forwarded in",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			if err := cli.ValidateOutput(c); err != nil {
				return err
			}
			// --dry-run here is the drift question: plan stored intent against
			// reality and print what does not match (design.md §5.4). It is on
			// the list command rather than on a whole-module `show`, because
			// `olr gateway show` is the routing half's and answers about the
			// other table.
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

			cfg, err := loadForwards(c)
			if err != nil {
				return err
			}
			if cli.IsJSON(c) {
				return cli.JSON(c.OutOrStdout(), cfg.Forwards)
			}
			return writeForwardsText(c.OutOrStdout(), cfg)
		},
	}
}

func showForwardCommand() *cobra.Command {
	c := &cobra.Command{
		Use:   "forward <name>",
		Short: "Show one port forward in full",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			if err := cli.ReadOnly(c); err != nil {
				return err
			}
			cfg, err := loadForwards(c)
			if err != nil {
				return err
			}
			f, ok := cfg.Find(args[0])
			if !ok {
				return unknownForward(&cfg, args[0])
			}
			if cli.IsJSON(c) {
				return cli.JSON(c.OutOrStdout(), f)
			}
			return writeForwardText(c.OutOrStdout(), f)
		},
	}
	c.ValidArgsFunction = cli.CompleteArgs(forwardNames)
	return c
}

// ---------------------------------------------------------------- add / set / rm

// forwardCommand builds `add forward` and `set forward`, which differ only in
// which of "it already exists" and "it does not exist yet" is the mistake.
//
// Two commands rather than one that does both, matching internal/dhcp's pools.
// The distinction is worth the duplication because it is the one the operator
// already has in their head: `add` on something that exists is almost always a
// name collision they want to hear about, and `set` on something that does not
// is almost always a typo.
func forwardCommand(mode string) *cobra.Command {
	var f forwardFlags

	short := "Send a port arriving from outside to a device here"
	if mode == "set" {
		short = "Change an existing port forward"
	}

	c := &cobra.Command{
		Use:   "forward <name>",
		Short: short,
		Long: "Send connections arriving on one of this router's ports to a client on your\n" +
			"network instead.\n\n" +
			"  olr gateway add forward web   --in wan0 --port 8080 --to 192.168.1.10:80\n" +
			"  olr gateway add forward games --in wan0 --protocol both --port 27015 \\\n" +
			"                                 --to 192.168.1.20:27015\n\n" +
			"A range of ports can only go to the same range — the kernel keeps each\n" +
			"connection's own port when the ranges match and picks an arbitrary one when\n" +
			"they do not, so a shifted range delivers to ports nobody chose.\n\n" +
			"Forwards work from inside your own network too, which needs the router to\n" +
			"stand in as the source of those connections. That means the device cannot\n" +
			"tell your own machines apart; --no-hairpin turns it off.\n\n" +
			"IPv6 is not forwarded. There is no NAT in IPv6 — a device inside already has\n" +
			"a reachable address — so it is a firewall permission rather than a\n" +
			"translation, and olr cannot open an IPv6 port to one device yet.",
		Args: cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			name := args[0]

			// The one read before the write, because the flags are a patch:
			// `set forward web --port 9090` means "keep everything else", and
			// f.apply needs the current forward to keep it onto. Only the flags
			// are resolved here — Upsert, the slot it preserves, and validation
			// all still happen on the daemon's side, under the lock, in the
			// single request below.
			cfg, err := loadForwards(c)
			if err != nil {
				return err
			}
			fwd, exists := cfg.Find(name)
			switch {
			case mode == "add" && exists:
				return fmt.Errorf(
					"there is already a forward called %q; use `olr gateway set forward %s` to change it",
					name, name)
			case mode == "set" && !exists:
				return unknownForward(&cfg, name)
			}

			fwd.Name = name
			if err := f.apply(c, &fwd, exists); err != nil {
				return err
			}
			return send(c, http.MethodPut, forwardEndpoint(name), fwd)
		},
	}
	f.register(c)
	if mode == "set" {
		// Only `set` addresses an existing forward; `add` is naming a new one,
		// and completing it from what already exists would suggest exactly the
		// names the command is about to refuse (docs/cli.md R7).
		c.ValidArgsFunction = cli.CompleteArgs(forwardNames)
	}
	return c
}

type forwardFlags struct {
	in        string
	protocol  string
	port      string
	to        string
	hairpin   bool
	noHairpin bool
}

func (f *forwardFlags) register(c *cobra.Command) {
	c.Flags().StringVar(&f.in, "in", "",
		"the interface connections arrive on, e.g. wan0")
	c.Flags().StringVar(&f.protocol, "protocol", "",
		"which traffic to forward: "+join(Protocols())+" (default tcp)")
	c.Flags().StringVar(&f.port, "port", "",
		"the port they arrive on, e.g. 8080 or 30000-30010")
	c.Flags().StringVar(&f.to, "to", "",
		"where to send them, as address:port, e.g. 192.168.1.10:80")

	// A pair, both defaulting false, rather than one flag defaulting true
	// (docs/cli.md R3). pflag takes no space-separated value for a boolean, so a
	// `--hairpin` defaulting true would spell its own negation `--hairpin=false`
	// — and `--hairpin false` parses as the flag plus a second positional, which
	// on a one-argument command reports an argument-count error naming neither
	// hairpin nor the real mistake.
	c.Flags().BoolVar(&f.hairpin, "hairpin", false,
		"make the forward work from inside your own network too (default)")
	c.Flags().BoolVar(&f.noHairpin, "no-hairpin", false,
		"only accept it from outside; devices here must use the internal address")
	c.MarkFlagsMutuallyExclusive("hairpin", "no-hairpin")

	cli.EnumFlag(c, "protocol", list(Protocols())...)
}

// list is join's sibling: the same vocabulary, as values rather than prose.
func list[T ~string](values []T) []string {
	out := make([]string, len(values))
	for i, v := range values {
		out[i] = string(v)
	}
	return out
}

func (f *forwardFlags) apply(c *cobra.Command, fwd *Forward, existing bool) error {
	changed := c.Flags().Changed

	if changed("in") {
		fwd.In = strings.TrimSpace(f.in)
	}
	if changed("protocol") {
		p := Protocol(strings.TrimSpace(f.protocol))
		if !p.Valid() {
			return fmt.Errorf("--protocol: unknown value %q (want %s)", f.protocol, join(Protocols()))
		}
		fwd.Protocol = p
	}
	if changed("port") {
		var r PortRange
		if err := r.UnmarshalText([]byte(f.port)); err != nil {
			return fmt.Errorf("--port: %v", err)
		}
		fwd.Port = r
	}
	if changed("to") {
		to, err := netip.ParseAddrPort(strings.TrimSpace(f.to))
		if err != nil {
			return fmt.Errorf("--to: %q is not an address:port, e.g. 192.168.1.10:80", f.to)
		}
		fwd.To = to
	}
	switch {
	case changed("hairpin"):
		on := f.hairpin
		fwd.Hairpin = &on
	case changed("no-hairpin"):
		on := !f.noHairpin
		fwd.Hairpin = &on
	}

	// Checked here rather than left to the daemon only because the message can
	// name the flags. Validate says the same things against field paths, which
	// is what the WebUI and an agent get; this is the same rule spelled for
	// somebody at a prompt.
	if !existing {
		var missing []string
		if fwd.In == "" {
			missing = append(missing, "--in")
		}
		if !fwd.Port.Valid() {
			missing = append(missing, "--port")
		}
		if !fwd.To.IsValid() {
			missing = append(missing, "--to")
		}
		if len(missing) > 0 {
			return fmt.Errorf("a new forward needs %s", strings.Join(missing, ", "))
		}
	}
	return nil
}

func rmForwardCommand() *cobra.Command {
	c := &cobra.Command{
		Use:   "forward <name>",
		Short: "Stop forwarding a port",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			return send(c, http.MethodDelete, forwardEndpoint(args[0]), nil)
		},
	}
	c.ValidArgsFunction = cli.CompleteArgs(forwardNames)
	return c
}

// ---------------------------------------------------------------- status

func statusForwardsCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "forwards",
		Short: "Show whether anything has arrived through each forward",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
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
		},
	}
}

// ---------------------------------------------------------------- plumbing

// send is the shape every change shares: one request naming the thing to change,
// then print what it did.
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

func forwardEndpoint(name string) string {
	return forwardsEndpoint + "/" + url.PathEscape(name)
}

// unknownForward names the forwards that do exist, because "no forward called X"
// is only half an answer when the reason is usually a typo.
//
// The phrasing is cli.UnknownObject's rather than this module's (docs/cli.md
// R8).
func unknownForward(cfg *Config, name string) error {
	return cli.UnknownObject("forward", name, "olr gateway add forward", names(cfg.Forwards))
}

func names(forwards []Forward) []string {
	out := make([]string, 0, len(forwards))
	for _, f := range forwards {
		out = append(out, f.Name)
	}
	return out
}

// ---------------------------------------------------------------- completion

// forwardNames answers "which ones are there?" for the shell (docs/cli.md R7).
// It reads through olrd like every other read, so it goes quiet rather than
// erroring when olrd is down.
func forwardNames(c *cobra.Command) ([]string, error) {
	cfg, err := loadForwards(c)
	if err != nil {
		return nil, err
	}
	return names(cfg.Forwards), nil
}

package remote

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/spf13/cobra"

	"github.com/open-linux-router/open-linux-router/internal/cli"
	"github.com/open-linux-router/open-linux-router/internal/core"
)

// Command returns the module's command tree. Mounted explicitly by cmd/olr.
func Command() *cobra.Command {
	return cli.NewModule("remote", "Get back into your network from outside it",
		showCommand(),
		setCommand(),
		addCommand(),
		rmCommand(),
		statusCommand(),
		cli.FixCommand(ModuleName, "remote access's"),
		enableCommand(),
		disableCommand(),
	)
}

// Endpoints this module's commands call. Spelled once so a rename cannot leave
// half the commands pointing at the old path.
const (
	base           = core.APIPrefix + "/" + ModuleName
	configEndpoint = base + "/config"
	statusEndpoint = base + "/status"

	wgConfigEndpoint = base + "/wireguard/config"
	wgPlanEndpoint   = base + "/wireguard/plan"
	wgPeersEndpoint  = base + "/wireguard/peers"
)

// peerEndpoint is the item route for one device.
//
// Escaped, because a name arrives from an operator's argument and reaches a URL
// path. A peer called `../config` should fail as a validation error, not as a
// request to somewhere else.
func peerEndpoint(name string) string {
	return wgPeersEndpoint + "/" + url.PathEscape(name)
}

func ctxOf(c *cobra.Command) context.Context {
	if ctx := c.Context(); ctx != nil {
		return ctx
	}
	return context.Background()
}

// verb wraps cli.Verb so the shared vocabulary check still runs (design.md
// §6.1: a verb outside the vocabulary panics at startup).
func verb(name, short string, build func(*cobra.Command)) *cobra.Command {
	c := cli.Verb(name, short)
	c.RunE = nil
	build(c)
	return c
}

// waysIn is the object vocabulary, for the commands that name one.
var waysIn = []string{"wireguard"}

// ---------------------------------------------------------------- show

func showCommand() *cobra.Command {
	c := verb("show", "Show remote-access configuration", func(c *cobra.Command) {
		c.Args = cobra.NoArgs
		c.RunE = func(c *cobra.Command, _ []string) error {
			if err := cli.ReadOnly(c); err != nil {
				return err
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
		showWireGuardCommand(),
		showPeersCommand(),
		showPeerCommand(),
	)
	return c
}

func loadConfig(c *cobra.Command) (Config, error) {
	var cfg Config
	if err := cli.ClientFor(c).Get(ctxOf(c), configEndpoint, &cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func showWireGuardCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "wireguard",
		Short: "Show the tunnel in full",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			if err := cli.ValidateOutput(c); err != nil {
				return err
			}
			// --dry-run on a show is the drift question (design.md §5.4).
			if cli.DryRun(c) {
				var plan planView
				if err := cli.ClientFor(c).Post(ctxOf(c), wgPlanEndpoint, nil, &plan); err != nil {
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
				return cli.JSON(c.OutOrStdout(), cfg.WireGuard)
			}
			return writeWireGuardText(c.OutOrStdout(), cfg)
		},
	}
}

func showPeersCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "peers",
		Short: "List the devices that may dial in",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			if err := cli.ReadOnly(c); err != nil {
				return err
			}
			resp, err := loadPeers(c)
			if err != nil {
				return err
			}
			if cli.IsJSON(c) {
				return cli.JSON(c.OutOrStdout(), resp.Peers)
			}
			return writePeersText(c.OutOrStdout(), resp.Peers)
		},
	}
}

// showPeerCommand is the detail half docs/cli.md R6 requires: a module that can
// list an object has to be able to show one.
func showPeerCommand() *cobra.Command {
	c := &cobra.Command{
		Use:   "peer <name>",
		Short: "Show one device in full",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			if err := cli.ReadOnly(c); err != nil {
				return err
			}
			resp, err := loadPeers(c)
			if err != nil {
				return err
			}
			want := normalizeName(args[0])
			for _, p := range resp.Peers {
				if p.Name != want {
					continue
				}
				if cli.IsJSON(c) {
					return cli.JSON(c.OutOrStdout(), p)
				}
				return writePeerText(c.OutOrStdout(), p)
			}
			return unknownPeer(resp.Peers, args[0])
		},
	}
	c.ValidArgsFunction = cli.CompleteArgs(peerNamesFor)
	return c
}

func loadPeers(c *cobra.Command) (peersResponse, error) {
	var resp peersResponse
	if err := cli.ClientFor(c).Get(ctxOf(c), wgPeersEndpoint, &resp); err != nil {
		return peersResponse{}, err
	}
	return resp, nil
}

// ---------------------------------------------------------------- set

func setCommand() *cobra.Command {
	var endpoint string

	c := verb("set", "Set how devices reach this box, or what a device is called", func(c *cobra.Command) {
		c.Args = cobra.NoArgs
		c.Long = "Set the address your devices dial from outside.\n\n" +
			"This is shared by every way in, which is why it is here rather than\n" +
			"on one of them — and why changing it invalidates every configuration\n" +
			"and link already handed out. It is also the one thing olr cannot work\n" +
			"out for itself: if a name already follows this router's address, use\n" +
			"it — `olr dial show` lists the ones olr keeps current.\n\n" +
			"Give the host alone. A port here could only be right for one way in;\n" +
			"use --public-port on the one whose forwarded port differs.\n\n" +
			"`olr remote set wireguard` changes the tunnel's own settings, and\n" +
			"`olr remote set peer` gives one device a different name."

		c.Flags().StringVar(&endpoint, "endpoint", "",
			"public name or address your devices dial")

		c.RunE = func(c *cobra.Command, _ []string) error {
			if !c.Flags().Changed("endpoint") {
				return fmt.Errorf("nothing to set; see `olr remote set --help`")
			}
			return sendEndpoint(c, map[string]any{"endpoint": endpoint})
		}
	})

	c.AddCommand(setWireGuardCommand(), setPeerCommand())
	return c
}

// setPeerCommand renames a device.
//
// `set` rather than a verb of its own: the name is a configuration field, and
// the shared vocabulary has a word for changing one (design.md §3.2 rule 4). It
// is deliberately not spelled `rm` + `add` — that would allocate a new address
// and a new key, and invalidate the file already on the device, for a change
// the device never hears about.
func setPeerCommand() *cobra.Command {
	var name, display string
	var newKey bool

	c := &cobra.Command{
		Use:   "peer <name>",
		Short: "Rename a device, label it, or give it a new configuration",
		Args:  cobra.ExactArgs(1),
		Long: "Change what a device is called, keeping its address and its key.\n\n" +
			"A name is olr's label, not part of the tunnel — WireGuard knows a\n" +
			"device by its key. So the configuration already on the device keeps\n" +
			"working, and nothing has to be re-issued or re-imported.\n\n" +
			"A local name added with `olr dns` does not follow: that name is `dns`'s\n" +
			"stored intent, and this module does not write another module's.\n\n" +
			"--name has to stay a DNS label — letters, digits and hyphens — because\n" +
			"it is the name every command takes and the one `olr dns` would be told\n" +
			"to answer. --display-name is what a person reads instead, so it takes\n" +
			"whatever you would write: \"Li's Macbook Pro\". Empty puts the name back.\n\n" +
			"--new-key is the other question entirely: it replaces a configuration\n" +
			"file you have lost. The device gets a new key pair and the file that\n" +
			"matches it, printed once like the first one, and the file it replaces\n" +
			"stops working. Its name, address and label stay as they are.",
		Example: "  olr remote set peer phone --name handset\n" +
			"  olr remote set peer handset --display-name \"Li's Macbook Pro\"\n" +
			"  olr remote set peer phone --new-key > phone.conf",
		RunE: func(c *cobra.Command, args []string) error {
			if newKey {
				// Two answers to two different questions in one command would
				// make the file and the rename succeed or fail together.
				if c.Flags().Changed("name") || c.Flags().Changed("display-name") {
					return fmt.Errorf(
						"--new-key issues a configuration and cannot be combined with --name or --display-name; " +
							"run them as two commands")
				}
				return sendTunnel(c, "POST", peerEndpoint(args[0])+"/reissue", nil)
			}
			patch := map[string]string{}
			if c.Flags().Changed("name") {
				patch["name"] = name
			}
			if c.Flags().Changed("display-name") {
				patch["display_name"] = display
			}
			if len(patch) == 0 {
				return fmt.Errorf("nothing to set; see `olr remote set peer --help`")
			}
			return sendTunnel(c, "PATCH", peerEndpoint(args[0]), patch)
		},
	}

	c.Flags().StringVar(&name, "name", "", "the device's name: letters, digits and hyphens")
	c.Flags().StringVar(&display, "display-name", "",
		"what to call it where a person reads it; anything you would write, or empty for the name")
	c.Flags().BoolVar(&newKey, "new-key", false,
		"give the device a new key pair and print the configuration that matches it, once")
	c.ValidArgsFunction = cli.CompleteArgs(peerNamesFor)
	return c
}

func setWireGuardCommand() *cobra.Command {
	var (
		subnet     string
		address    string
		noAddress  bool
		iface      string
		port       uint16
		publicPort uint16
		raw        string
		clearRaw   bool
	)

	c := &cobra.Command{
		Use:   "wireguard",
		Short: "Set the tunnel's settings",
		Args:  cobra.NoArgs,
		Long: "Set the tunnel's settings: the network dial-in devices get addresses\n" +
			"on, and which port to listen on.\n\n" +
			"Changing the network renumbers every device, so every configuration\n" +
			"already on a phone stops working. olr reports that as a disruptive\n" +
			"change; --dry-run shows it before you commit to it.",
		RunE: func(c *cobra.Command, _ []string) error {
			if c.Flags().NFlag() == 0 {
				return fmt.Errorf("nothing to set; see `olr remote set wireguard --help`")
			}
			patch := map[string]any{}
			if c.Flags().Changed("subnet") {
				patch["subnet"] = subnet
			}
			if c.Flags().Changed("address") {
				patch["address"] = address
			}
			if noAddress {
				patch["address"] = nil
			}
			if c.Flags().Changed("interface") {
				patch["interface"] = iface
			}
			if c.Flags().Changed("port") {
				patch["listen_port"] = port
			}
			if c.Flags().Changed("public-port") {
				patch["public_port"] = publicPort
			}
			if c.Flags().Changed("raw-wireguard-conf") {
				patch["raw_wireguard_conf"] = raw
			}
			if clearRaw {
				patch["raw_wireguard_conf"] = ""
			}
			return sendTunnel(c, "PATCH", wgConfigEndpoint, patch)
		},
	}

	c.Flags().StringVar(&subnet, "subnet", "",
		"network dial-in devices get addresses on, e.g. "+DefaultSubnet.String())
	c.Flags().StringVar(&address, "address", "", "this box's own address on that network")
	c.Flags().BoolVar(&noAddress, "no-address", false,
		"use the first address in the network, which is the default")
	c.Flags().StringVar(&iface, "interface", "",
		"kernel interface name for the tunnel (default "+DefaultInterface+")")
	c.Flags().Uint16Var(&port, "port", 0,
		fmt.Sprintf("UDP port to listen on (default %d)", DefaultListenPort))
	c.Flags().Uint16Var(&publicPort, "public-port", 0,
		"port devices dial, if a router in front forwards a different one")
	c.Flags().StringVar(&raw, "raw-wireguard-conf", "",
		"extra WireGuard configuration, appended verbatim")
	c.Flags().BoolVar(&clearRaw, "no-raw-wireguard-conf", false,
		"remove the extra WireGuard configuration")

	c.MarkFlagsMutuallyExclusive("address", "no-address")
	c.MarkFlagsMutuallyExclusive("raw-wireguard-conf", "no-raw-wireguard-conf")
	return c
}

// ---------------------------------------------------------------- add / rm

func addCommand() *cobra.Command {
	c := verb("add", "Let a device dial in", func(*cobra.Command) {})
	c.AddCommand(addPeerCommand())
	return c
}

func addPeerCommand() *cobra.Command {
	var (
		routes    string
		display   string
		publicKey string
	)

	c := &cobra.Command{
		Use:   "peer <name>",
		Short: "Let a device dial in over the tunnel",
		Args:  cobra.ExactArgs(1),
		Long: "Let a device dial in over the tunnel, and print the configuration to\n" +
			"import on it.\n\n" +
			"olr generates the device's key pair, keeps the public half and gives\n" +
			"you the rest. It is printed once and cannot be shown again: the\n" +
			"private key is stored nowhere, so a lost file has to be replaced —\n" +
			"`olr remote set peer <name> --new-key`.\n\n" +
			"Only the tunnel has devices. The proxy has one link for everybody —\n" +
			"see `olr remote show link`.\n\n" +
			"Examples:\n" +
			"  olr remote add peer phone > phone.conf\n" +
			"  olr remote add peer laptop --routes everything\n" +
			"  olr remote add peer work --public-key <key generated on the device>",
		RunE: func(c *cobra.Command, args []string) error {
			body := peerBody{Routes: RouteScope(routes), DisplayName: display, PublicKey: publicKey}
			return sendTunnel(c, "PUT", peerEndpoint(args[0]), body)
		},
	}

	c.Flags().StringVar(&routes, "routes", "",
		fmt.Sprintf("what the device sends through the tunnel: %s (default %s)",
			joinScopes(), RouteHome))
	c.Flags().StringVar(&display, "display-name", "",
		"what to call it where a person reads it; anything you would write, or empty for the name")
	c.Flags().StringVar(&publicKey, "public-key", "",
		"public key generated on the device itself, if you would rather olr never held the private one")
	cli.EnumFlag(c, "routes", scopeNames()...)
	return c
}

func rmCommand() *cobra.Command {
	c := verb("rm", "Stop a device dialling in", func(*cobra.Command) {})
	c.AddCommand(rmPeerCommand())
	return c
}

func rmPeerCommand() *cobra.Command {
	c := &cobra.Command{
		Use:   "peer <name>",
		Short: "Stop a device dialling in",
		Args:  cobra.ExactArgs(1),
		Long: "Stop a device dialling in over the tunnel.\n\n" +
			"Its access ends immediately and its configuration stops working for\n" +
			"good — there is no disabling a peer, because a WireGuard peer is a\n" +
			"key and removing the key is the whole of revoking it.",
		RunE: func(c *cobra.Command, args []string) error {
			return sendTunnel(c, "DELETE", peerEndpoint(args[0]), nil)
		},
	}
	c.ValidArgsFunction = cli.CompleteArgs(peerNamesFor)
	return c
}

// ---------------------------------------------------------------- status

func statusCommand() *cobra.Command {
	return verb("status", "Show whether each way in is working", func(c *cobra.Command) {
		c.Args = cobra.NoArgs
		c.RunE = func(c *cobra.Command, _ []string) error {
			if err := cli.ReadOnly(c); err != nil {
				return err
			}
			var resp moduleStatus
			if err := cli.ClientFor(c).Get(ctxOf(c), statusEndpoint, &resp); err != nil {
				return err
			}
			if cli.IsJSON(c) {
				return cli.JSON(c.OutOrStdout(), resp)
			}
			return writeStatusText(c.OutOrStdout(), resp)
		}
	})
}

// ---------------------------------------------------------------- enable

func enableCommand() *cobra.Command { return wayInCommand("enable", true) }

func disableCommand() *cobra.Command { return wayInCommand("disable", false) }

// wayInCommand builds `olr remote enable|disable wireguard`.
//
// The object is required rather than defaulted. "Turn on remote access" stopped
// being a sentence with one meaning the moment there were two ways in — the
// tunnel puts a device inside the network and the proxy explicitly does not —
// and picking one for the operator would be choosing which they meant.
func wayInCommand(name string, on bool) *cobra.Command {
	short := map[bool]string{
		true:  "Start accepting connections from outside",
		false: "Stop accepting connections from outside",
	}[on]

	return verb(name, short, func(c *cobra.Command) {
		c.Use = name + " <way>"
		c.Args = cobra.ExactArgs(1)
		c.ValidArgs = waysIn
		c.Long = map[bool]string{
			true:  "Start accepting WireGuard connections. OLR generates the tunnel key on first enable.",
			false: "Stop accepting WireGuard connections, keeping the saved key and devices.",
		}[on]

		c.RunE = func(c *cobra.Command, args []string) error {
			switch args[0] {
			case "wireguard":
				return sendTunnel(c, "PATCH", wgConfigEndpoint, map[string]any{"enabled": on})
			default:
				return cli.UnknownObject("way in", args[0], "", waysIn)
			}
		}
	})
}

// ---------------------------------------------------------------- shared
//
// Three send helpers, one per response shape.
//
// Three rather than one because the module has three write surfaces with three
// different answers: the tunnel returns a plan over kernel lines and sometimes a
// client configuration, the proxy returns a plan over files, and the shared
// endpoint returns an impact with no plan at all. A single helper would take an
// `any` and type-switch on it, which is these three branches with the types
// erased on the way past.
//
// What they share is the contract: applying happens on return with no staged
// commit (design.md §5.1), so every request carries confirm=true and
// `--dry-run` is how you look first. The disruptive gate exists for the WebUI,
// which has somebody to ask.

func sendTunnel(c *cobra.Command, method, path string, body any) error {
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

	var result tunnelApplyResponse
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
	// "Nothing to do" is suppressed when there is something for the operator to
	// do: narrowing a device's routes changes no kernel state at all, and a
	// line saying the configuration is already applied would be the opposite of
	// what the note under it is about to say.
	if result.Peer == nil || !result.Plan.Empty {
		if err := writePlanText(c.OutOrStdout(), result.Plan, false); err != nil {
			return err
		}
	} else if err := writeWarnings(c.OutOrStdout(), result.Plan.Warnings); err != nil {
		return err
	}
	writePeerResultText(c.OutOrStdout(), result.Peer)
	return nil
}

func sendProxy(c *cobra.Command, method, path string, body any) error {
	if err := cli.ValidateOutput(c); err != nil {
		return err
	}
	ctx, client := ctxOf(c), cli.ClientFor(c)

	if cli.DryRun(c) {
		var plan proxyPlanView
		if err := client.Do(ctx, method, path+"?dry_run=true", body, &plan); err != nil {
			return err
		}
		if cli.IsJSON(c) {
			return cli.JSON(c.OutOrStdout(), plan)
		}
		return writeProxyPlanText(c.OutOrStdout(), plan, true)
	}

	var result proxyApplyResponse
	if err := client.Do(ctx, method, path+"?confirm=true", body, &result); err != nil {
		writeStepsText(c.ErrOrStderr(), result.Steps)
		return err
	}
	if cli.IsJSON(c) {
		return cli.JSON(c.OutOrStdout(), result)
	}
	if err := writeProxyPlanText(c.OutOrStdout(), result.Plan, false); err != nil {
		return err
	}
	if result.Plan.PasswordGenerated {
		// A change olr made on the operator's behalf, said in the same breath
		// as the change they asked for (design.md §5.6).
		fmt.Fprintln(c.OutOrStdout(),
			"\nA new password was generated, so every device needs a new link:\n"+
				"    olr remote show link")
	}
	return nil
}

func sendEndpoint(c *cobra.Command, body any) error {
	if err := cli.ValidateOutput(c); err != nil {
		return err
	}
	ctx, client := ctxOf(c), cli.ClientFor(c)

	path := configEndpoint + "?confirm=true"
	if cli.DryRun(c) {
		path = configEndpoint + "?dry_run=true"
	}

	var resp endpointResponse
	if err := client.Do(ctx, "PATCH", path, body, &resp); err != nil {
		return err
	}
	if cli.IsJSON(c) {
		return cli.JSON(c.OutOrStdout(), resp)
	}
	return writeEndpointText(c.OutOrStdout(), resp, cli.DryRun(c))
}

func unknownPeer(peers []peerView, name string) error {
	have := make([]string, 0, len(peers))
	for _, p := range peers {
		have = append(have, p.Name)
	}
	return cli.UnknownObject("peer", name, "olr remote add peer <name>", have)
}

func peerNamesFor(c *cobra.Command) ([]string, error) {
	resp, err := loadPeers(c)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(resp.Peers))
	for _, p := range resp.Peers {
		out = append(out, p.Name)
	}
	return out, nil
}

func scopeNames() []string {
	out := make([]string, 0, len(RouteScopes()))
	for _, s := range RouteScopes() {
		out = append(out, string(s))
	}
	return out
}

func joinScopes() string { return strings.Join(scopeNames(), ", ") }

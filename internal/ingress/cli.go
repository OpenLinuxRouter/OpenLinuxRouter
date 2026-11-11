// Package ingress publishes internal services over HTTPS at a name.
//
// It owns the published domain, the certificate and the proxy, and nothing
// else. The name a published service resolves at is a record in the operator's
// own DNS (docs/ingress.md §3), and the client it points at belongs to
// `clients`, whose address is read through a view rather than copied
// (design.md §4.1).
package ingress

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"

	"github.com/open-linux-router/open-linux-router/internal/cli"
	"github.com/open-linux-router/open-linux-router/internal/core"
)

// Every command here is a client of olrd (design.md §6.1). None of them touches
// the config document, the rendered files or systemd directly: a CLI that wrote
// the system on its own would be a second writer, holding a lock olrd cannot
// see, running its own copy of the validation rules, and publishing none of the
// change events the UI listens for.

// Command returns the module's command tree. Mounted explicitly by cmd/olr.
func Command() *cobra.Command {
	return cli.NewModule("ingress", "Publish internal services at https:// names",
		showCommand(),
		setCommand(),
		addCommand(),
		rmCommand(),
		statusCommand(),
		logsCommand(),
		enableCommand(),
		disableCommand(),
	)
}

// Endpoints this module's commands call. Spelled once so a rename cannot leave
// half the commands pointing at the old path.
const (
	base              = core.APIPrefix + "/" + ModuleName
	configEndpoint    = base + "/config"
	planEndpoint      = base + "/plan"
	statusEndpoint    = base + "/status"
	servicesEndpoint  = base + "/services"
	providersEndpoint = base + "/providers"
)

// serviceEndpoint is the item route for one published service.
//
// Escaped, because a name arrives from an operator's argument and reaches a URL
// path. Publishing a service called `../config` should fail as a validation
// error, not as a request to somewhere else.
func serviceEndpoint(name string) string {
	return servicesEndpoint + "/" + url.PathEscape(name)
}

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
	c := verb("show", "Show ingress configuration", func(c *cobra.Command) {
		c.Args = cobra.NoArgs
		c.RunE = func(c *cobra.Command, _ []string) error {
			if err := cli.ValidateOutput(c); err != nil {
				return err
			}
			// --dry-run on `show` is the drift question (design.md §5.4).
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

	c.AddCommand(showServicesCommand(), showServiceCommand(), showProvidersCommand())
	return c
}

func showServicesCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "services",
		Short: "List published services",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			if err := cli.ReadOnly(c); err != nil {
				return err
			}
			resp, err := loadServices(c)
			if err != nil {
				return err
			}
			if cli.IsJSON(c) {
				return cli.JSON(c.OutOrStdout(), resp.Services)
			}
			return writeServicesText(c.OutOrStdout(), resp.Services, resp.Domain)
		},
	}
}

// showServiceCommand is the detail half docs/cli.md R6 requires: a module that
// can list an object has to be able to show one.
func showServiceCommand() *cobra.Command {
	c := &cobra.Command{
		Use:   "service <name>",
		Short: "Show one published service in full",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			if err := cli.ReadOnly(c); err != nil {
				return err
			}
			resp, err := loadServices(c)
			if err != nil {
				return err
			}
			want := normalizeName(args[0], resp.Domain)
			for _, s := range resp.Services {
				if s.Name != want {
					continue
				}
				if cli.IsJSON(c) {
					return cli.JSON(c.OutOrStdout(), s)
				}
				return writeServiceText(c.OutOrStdout(), s)
			}
			return unknownService(resp.Services, args[0])
		},
	}
	c.ValidArgsFunction = cli.CompleteArgs(serviceNames)
	return c
}

func showProvidersCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "providers",
		Short: "List the DNS providers certificates can be obtained through",
		Args:  cobra.NoArgs,
		Long: "List the DNS providers this build can write an ACME challenge record through.\n\n" +
			"The set is fixed when olr's proxy is built, because these are compiled-in\n" +
			"modules rather than plugins loaded at runtime (docs/ingress.md §5.2).",
		RunE: func(c *cobra.Command, _ []string) error {
			if err := cli.ReadOnly(c); err != nil {
				return err
			}
			var resp providersResponse
			if err := cli.ClientFor(c).Get(ctxOf(c), providersEndpoint, &resp); err != nil {
				return err
			}
			if cli.IsJSON(c) {
				return cli.JSON(c.OutOrStdout(), resp.Providers)
			}
			return writeProvidersText(c.OutOrStdout(), resp.Providers)
		},
	}
}

func loadServices(c *cobra.Command) (servicesResponse, error) {
	var resp servicesResponse
	if err := cli.ClientFor(c).Get(ctxOf(c), servicesEndpoint, &resp); err != nil {
		return servicesResponse{}, err
	}
	return resp, nil
}

// ---------------------------------------------------------------- set

func setCommand() *cobra.Command {
	var (
		domain    string
		provider  string
		token     string
		tokenFile string
		email     string
		resolvers []string
		raw       string
		clearRaw  bool
	)

	return verb("set", "Set how certificates are obtained", func(c *cobra.Command) {
		c.Args = cobra.NoArgs
		c.Long = "Set the module-level settings: the domain published names live under,\n" +
			"which DNS provider hosts it, the credential olr uses to prove the domain\n" +
			"is yours, and who the certificate authority should contact.\n\n" +
			"`--domain` is the zone you add records to, and the one the wildcard\n" +
			"certificate is for: publishing grafana under home.example.com means a\n" +
			"record you create in your own DNS and a certificate for\n" +
			"*.home.example.com. It is deliberately not the resolver's local domain\n" +
			"(`olr dns set --local-domain`), which names the clients in this house."

		c.Flags().StringVar(&domain, "domain", "", "suffix published names live under, e.g. home.example.com")
		c.Flags().StringVar(&provider, "provider", "", "DNS provider hosting your domain (see `olr ingress show providers`)")
		c.Flags().StringVar(&token, "token", "",
			"provider API credential. Prefer --token-file: a value here is recorded in your shell history")
		c.Flags().StringVar(&tokenFile, "token-file", "",
			"read the provider API credential from a file, or from standard input when given -")
		c.Flags().StringVar(&email, "email", "", "contact address for certificate-expiry warnings")
		// StringArray, not StringSlice (docs/cli.md R5): Slice splits on commas.
		c.Flags().StringArrayVar(&resolvers, "resolver", nil,
			"public resolver for the certificate propagation check, repeatable (default 1.1.1.1 and 8.8.8.8)")
		c.Flags().StringVar(&raw, "raw-caddyfile", "", "extra Caddyfile configuration, appended verbatim")
		c.Flags().BoolVar(&clearRaw, "no-raw-caddyfile", false, "remove the extra Caddyfile configuration")

		c.MarkFlagsMutuallyExclusive("token", "token-file")
		c.MarkFlagsMutuallyExclusive("raw-caddyfile", "no-raw-caddyfile")
		// Completed from the running proxy rather than from a fixed list: what
		// is legal depends on how the operator's binary was built, and a shell
		// completion that offers a name the binary lacks is a worse lie than no
		// completion at all.
		_ = c.RegisterFlagCompletionFunc("provider", cli.CompleteFlag(providerNames))

		c.RunE = func(c *cobra.Command, _ []string) error {
			if c.Flags().NFlag() == 0 {
				return fmt.Errorf("nothing to set; see `olr ingress set --help`")
			}

			secret, err := readToken(c, token, tokenFile)
			if err != nil {
				return err
			}

			// A merge patch of only what was asked for. `certificate` is an
			// object so RFC 7386 merges into it key by key, which is exactly
			// what "change the provider and leave the credential alone" needs —
			// and why the services list gets item routes instead (http.go).
			cert := map[string]any{}
			if c.Flags().Changed("provider") {
				cert["provider"] = provider
			}
			if secret != "" {
				cert["provider_token"] = secret
			}
			if c.Flags().Changed("email") {
				cert["acme_email"] = email
			}
			if c.Flags().Changed("resolver") {
				cert["resolvers"] = resolvers
			}

			patch := map[string]any{}
			if c.Flags().Changed("domain") {
				patch["domain"] = domain
			}
			if len(cert) > 0 {
				patch["certificate"] = cert
			}
			if c.Flags().Changed("raw-caddyfile") {
				patch["raw_caddyfile"] = raw
			}
			if clearRaw {
				patch["raw_caddyfile"] = ""
			}
			return send(c, "PATCH", configEndpoint, patch)
		}
	})
}

// readToken gets the credential from wherever the operator put it.
//
// `--token-file -` reads standard input, which is the form that keeps a
// credential out of both the shell history and the process table — `olr ingress
// set --token-file - < token` and `pass show … | olr ingress set --token-file -`
// both work. `--token` stays because refusing it outright would push people to
// paste the value into a config file instead, which is worse.
func readToken(c *cobra.Command, token, tokenFile string) (string, error) {
	switch {
	case token != "":
		return token, nil
	case tokenFile == "":
		return "", nil
	case tokenFile == "-":
		data, err := io.ReadAll(c.InOrStdin())
		if err != nil {
			return "", fmt.Errorf("reading the credential from standard input: %w", err)
		}
		return strings.TrimSpace(string(data)), nil
	default:
		data, err := os.ReadFile(tokenFile)
		if err != nil {
			return "", fmt.Errorf("reading the credential: %w", err)
		}
		return strings.TrimSpace(string(data)), nil
	}
}

// ---------------------------------------------------------------- add / rm

type upstreamFlags struct {
	client        string
	host          string
	port          uint16
	scheme        string
	headers       []string
	removeHeaders []string
}

func (f *upstreamFlags) register(c *cobra.Command) {
	c.Flags().StringVar(&f.client, "client", "", "client to publish, by its name in the client list")
	c.Flags().StringVar(&f.host, "host", "", "address or hostname to publish, for a target that is not a client")
	c.Flags().Uint16Var(&f.port, "port", 0, "port the service listens on")
	c.Flags().StringVar(&f.scheme, "scheme", "", fmt.Sprintf("how to reach it: %s (default http)", joinSchemes()))
	c.Flags().StringArrayVar(&f.headers, "header", nil, "set an upstream request header as 'Name: value', repeatable")
	c.Flags().StringArrayVar(&f.removeHeaders, "remove-header", nil, "remove an upstream request header, repeatable")

	c.MarkFlagsMutuallyExclusive("client", "host")
	cli.EnumFlag(c, "scheme", schemeNames()...)
}

func (f *upstreamFlags) apply(u *Upstream, c *cobra.Command) {
	if c.Flags().Changed("client") {
		u.Client, u.Host = f.client, ""
	}
	if c.Flags().Changed("host") {
		u.Host, u.Client = f.host, ""
	}
	if c.Flags().Changed("port") {
		u.Port = f.port
	}
	if c.Flags().Changed("scheme") {
		u.Scheme = Scheme(f.scheme)
	}
	for _, entry := range f.headers {
		name, value, _ := strings.Cut(entry, ":")
		u.RequestHeaders = append(u.RequestHeaders, RequestHeader{Name: strings.TrimSpace(name), Value: strings.TrimPrefix(value, " ")})
	}
	for _, name := range f.removeHeaders {
		u.RequestHeaders = append(u.RequestHeaders, RequestHeader{Name: strings.TrimSpace(name), Remove: true})
	}
}

func addCommand() *cobra.Command {
	var flags upstreamFlags
	var linkPath, icon string

	c := verb("add", "Publish a service at a name", func(c *cobra.Command) {
		c.Use = "add <name>"
		c.Args = cobra.ExactArgs(1)
		c.Long = "Publish an internal service so it is reachable at\n" +
			"https://<name>.<your published domain>.\n\n" +
			"The certificate is already arranged — one wildcard covers every\n" +
			"published name. The DNS record is yours: point <name>.<domain> at\n" +
			"this router in your own zone, and note that a private answer there\n" +
			"needs the zone listed in `olr dns set --private-domain`.\n\n" +
			"Examples:\n" +
			"  olr ingress add grafana --client nuc --port 3000\n" +
			"  olr ingress add nas --client synology --port 5001 --scheme https\n" +
			"  olr ingress add hello --host 127.0.0.1 --port 8000\n" +
			"  olr ingress add mi --host 172.16.1.163 --port 80 --header 'Host: 172.16.1.163'"
		flags.register(c)
		c.Flags().StringVar(&linkPath, "link-path", "", "page path to open and use for its icon (e.g. /ui); does not change proxy routing")
		c.Flags().StringVar(&icon, "icon", "", "custom icon: theSVG ID (thesvg:grafana) or PNG/JPEG/WebP data URL; empty uses the site favicon")
		c.RunE = func(c *cobra.Command, args []string) error {
			for _, entry := range flags.headers {
				if !strings.Contains(entry, ":") {
					return fmt.Errorf("--header %q: use 'Name: value'", entry)
				}
			}
			// One request to the item route, not load-splice-save. The daemon
			// holds the lock across the whole edit that way, and the rule that
			// reduces `grafana` and `grafana.home.example.com` to one entry lives
			// there rather than here (http.go).
			s := Service{Name: args[0], LinkPath: linkPath, Icon: icon}
			flags.apply(&s.Upstream, c)
			return send(c, "PUT", serviceEndpoint(args[0]), s)
		}
	})
	return c
}

func rmCommand() *cobra.Command {
	c := verb("rm", "Stop publishing a service", func(c *cobra.Command) {
		c.Use = "rm <name>"
		c.Args = cobra.ExactArgs(1)
		c.Long = "Stop publishing a service.\n\n" +
			"The name stops answering immediately, which olr reports as a\n" +
			"disruptive change: anyone with the URL open loses it."
		c.RunE = func(c *cobra.Command, args []string) error {
			return send(c, "DELETE", serviceEndpoint(args[0]), nil)
		}
	})
	c.ValidArgsFunction = cli.CompleteArgs(serviceNames)
	return c
}

// ---------------------------------------------------------------- status

func statusCommand() *cobra.Command {
	return verb("status", "Show whether the proxy and its certificate are healthy", func(c *cobra.Command) {
		c.Args = cobra.NoArgs
		c.RunE = func(c *cobra.Command, _ []string) error {
			if err := cli.ReadOnly(c); err != nil {
				return err
			}
			var resp statusResponse
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

func logsCommand() *cobra.Command {
	var (
		lines  int
		follow bool
	)

	c := verb("logs", "Show the proxy's log output", func(c *cobra.Command) {
		c.Args = cobra.NoArgs
		c.Long = "Show the proxy's log output.\n\n" +
			"olr stores no logs of its own — journald already does that\n" +
			"(design.md §3.4), so this streams from the journal rather than\n" +
			"reimplementing it.\n\n" +
			"This is where a certificate that will not issue explains itself."
		c.RunE = func(c *cobra.Command, _ []string) error {
			// The one command here that does not go through olrd, because there
			// is nothing for olrd to add: the logs are journald's and the unit
			// name is a constant.
			journalctl, err := exec.LookPath("journalctl")
			if err != nil {
				return fmt.Errorf("journalctl is not available on this system: %w", err)
			}
			args := []string{"-u", Caddy{}.Unit(), "-n", fmt.Sprint(lines)}
			if follow {
				args = append(args, "-f")
			}
			cmd := exec.CommandContext(ctxOf(c), journalctl, args...)
			cmd.Stdout, cmd.Stderr = c.OutOrStdout(), c.ErrOrStderr()
			return cmd.Run()
		}
	})
	c.Flags().IntVarP(&lines, "lines", "n", 50, "number of lines to show")
	c.Flags().BoolVarP(&follow, "follow", "f", false, "keep streaming new lines")
	return c
}

func enableCommand() *cobra.Command {
	return verb("enable", "Start publishing services", func(c *cobra.Command) {
		c.Args = cobra.NoArgs
		c.RunE = func(c *cobra.Command, _ []string) error {
			return send(c, "PATCH", configEndpoint, map[string]any{"enabled": true})
		}
	})
}

func disableCommand() *cobra.Command {
	return verb("disable", "Stop publishing services", func(c *cobra.Command) {
		c.Args = cobra.NoArgs
		c.Long = "Stop the proxy. Every published name stops answering, and the\n" +
			"configuration is kept so that enabling again needs no retyping."
		c.RunE = func(c *cobra.Command, _ []string) error {
			return send(c, "PATCH", configEndpoint, map[string]any{"enabled": false})
		}
	})
}

// ---------------------------------------------------------------- shared

// send is the shape every change shares: one request naming the thing to
// change, then print what it did.
//
// Applying happens on return, with no staged commit (design.md §5.1), so the
// diff and the impact are printed either way. That is also why every request
// here carries confirm=true: the disruptive gate exists for the WebUI, which can
// put the question to somebody and wait. A command that returned "this would be
// disruptive, run it again" would be a staged commit by another name, and §5.1
// says this surface does not have one. `--dry-run` is how you look first.
//
// One thing specific to this module: nothing here has to know a credential
// exists. `olr ingress set --token-file` sends the value it read; every other
// command sends a patch that does not mention the field, and RFC 7386 leaves an
// unmentioned key alone.
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

func unknownService(services []serviceView, name string) error {
	if len(services) == 0 {
		return fmt.Errorf("no service named %q; nothing is published yet", name)
	}
	names := make([]string, 0, len(services))
	for _, s := range services {
		names = append(names, s.Name)
	}
	return fmt.Errorf("no service named %q; published: %s", name, strings.Join(names, ", "))
}

func serviceNames(c *cobra.Command) ([]string, error) {
	resp, err := loadServices(c)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(resp.Services))
	for _, s := range resp.Services {
		out = append(out, s.Name)
	}
	return out, nil
}

func providerNames(c *cobra.Command) ([]string, error) {
	var resp providersResponse
	if err := cli.ClientFor(c).Get(ctxOf(c), providersEndpoint, &resp); err != nil {
		return nil, err
	}
	return resp.Providers, nil
}

func schemeNames() []string {
	out := make([]string, 0, len(Schemes()))
	for _, s := range Schemes() {
		out = append(out, string(s))
	}
	return out
}

func joinSchemes() string { return strings.Join(schemeNames(), ", ") }

// Package daemon is the open-linux-router control plane, olrd.
//
// It is one resident process holding the route table, the schema, the config
// store and the apply lock (design.md §3.5). Modules live inside it; backends
// — dnsmasq and everything like it — do not, and never will. The invariant that
// decides that split is worth repeating at the top of the package it governs:
//
//	systemctl restart olrd must never drop a packet, expire a lease, or break
//	a session.
//
// A package rather than a command since the three binaries were merged: `olr`
// reaches this through `olr internal daemon`, which olrd.service invokes. The
// process is still its own, which is the only part §3.5 was ever about.
package daemon

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	sddaemon "github.com/coreos/go-systemd/v22/daemon"

	"github.com/open-linux-router/open-linux-router/internal/brandlogo"
	"github.com/open-linux-router/open-linux-router/internal/buildinfo"
	"github.com/open-linux-router/open-linux-router/internal/clients"
	"github.com/open-linux-router/open-linux-router/internal/core"
	"github.com/open-linux-router/open-linux-router/internal/devices"
	"github.com/open-linux-router/open-linux-router/internal/dhcp"
	"github.com/open-linux-router/open-linux-router/internal/dial"
	"github.com/open-linux-router/open-linux-router/internal/dns"
	"github.com/open-linux-router/open-linux-router/internal/firewall"
	"github.com/open-linux-router/open-linux-router/internal/gateway"
	"github.com/open-linux-router/open-linux-router/internal/gateway/nat"
	"github.com/open-linux-router/open-linux-router/internal/geoip"
	"github.com/open-linux-router/open-linux-router/internal/host"
	"github.com/open-linux-router/open-linux-router/internal/ingress"
	"github.com/open-linux-router/open-linux-router/internal/inspection"
	"github.com/open-linux-router/open-linux-router/internal/iptv"
	"github.com/open-linux-router/open-linux-router/internal/link"
	"github.com/open-linux-router/open-linux-router/internal/mcp"
	"github.com/open-linux-router/open-linux-router/internal/qos"
	"github.com/open-linux-router/open-linux-router/internal/remote"
	"github.com/open-linux-router/open-linux-router/internal/runtimedb"
	socksout "github.com/open-linux-router/open-linux-router/internal/socksout"
	"github.com/open-linux-router/open-linux-router/internal/system"
	"github.com/open-linux-router/open-linux-router/internal/tools"
	"github.com/open-linux-router/open-linux-router/internal/webui"
)

type options struct {
	socket        string
	listen        string
	links         string
	root          string
	tokenPath     string
	auth          bool
	tlsCert       string
	tlsKey        string
	allowedHosts  string
	adminNetworks string
	logLevel      string
	version       bool
}

// Main is the control plane's entry point, reached as `olr internal daemon`.
//
// The prefix on the error is still olrd rather than olr. One binary now
// carries every role, so the name of the executable no longer says which one
// spoke — and in a journal holding all three, that is the only thing that
// does.
func Main(args []string) int {
	if err := run(args); err != nil {
		fmt.Fprintln(os.Stderr, "olrd:", err)
		return 1
	}
	return 0
}

// parseOptions turns olrd's arguments into options.
//
// Split out of run so that what the flags mean can be tested without starting a
// daemon — which matters most for the ones whose meaning has changed, where the
// question is not "does it parse" but "does an old argument still do something
// sensible".
func parseOptions(args []string, onError flag.ErrorHandling) (options, error) {
	var opts options

	// A FlagSet of our own, not the package-level CommandLine: `olr` owns
	// os.Args, and this process is reached through a subcommand, so the flags
	// to parse are the ones after it rather than all of them. ExitOnError
	// keeps the behaviour a bad flag used to get from flag.Parse().
	fs := flag.NewFlagSet("olr internal daemon", onError)
	fs.StringVar(&opts.socket, "socket", core.DefaultSocket,
		"unix socket to serve the API on")
	// On by default, which it was not until docs/system.md §1.
	//
	// The command that used to be required — `olr listen 0.0.0.0:8080` — was the
	// last wall on the first-install path: the UI did not exist until you ran
	// something nobody told you about. Every comparable management surface
	// listens on install, and design.md §7's "install alone changes nothing" was
	// an over-application of a rule about *shared* state to the one surface it
	// does not govern.
	//
	// What makes it safe is not this flag but gateUnclaimed: an unclaimed box
	// cannot be configured over the network, so serving the page before its
	// owner arrives costs nothing that can be used. `--listen ""` still turns
	// the listener off entirely, and `olr listen` still moves it.
	fs.StringVar(&opts.listen, "listen", core.DefaultListen,
		"TCP address for the WebUI and remote clients; empty turns it off")
	fs.StringVar(&opts.links, "links", "",
		"read interface facts from this JSON file instead of the kernel (development only)")
	fs.StringVar(&opts.root, "root", "",
		"prefix every configuration and state path with this directory (development only)")
	fs.StringVar(&opts.tokenPath, "token-file", core.TokenPath,
		"file holding the API token for the TCP listener")
	fs.BoolVar(&opts.auth, "auth", false,
		"require the API token on the TCP listener (off by default)")
	fs.StringVar(&opts.tlsCert, "tls-cert", "", "PEM certificate for HTTPS on the TCP listener (requires --tls-key)")
	fs.StringVar(&opts.tlsKey, "tls-key", "", "PEM private key for HTTPS on the TCP listener (requires --tls-cert)")
	// Keep accepting old startup arguments so upgrades cannot stop the daemon.
	fs.StringVar(&opts.allowedHosts, "allowed-hosts", "", "deprecated; accepted and ignored")
	fs.StringVar(&opts.adminNetworks, "admin-networks", "", "comma-separated client CIDRs allowed to reach TCP management; empty allows all sources")
	// Accepted and ignored. It named the default-on inverse of --auth, and a box
	// upgrading with it still in OLRD_ARGS must not fail to start over a flag
	// that now describes the default. OLRD_ARGS is a file operators edit by
	// hand, so an unknown-flag exit here is an outage during an upgrade.
	fs.Bool("no-auth", false, "deprecated: the TCP listener is unauthenticated unless --auth")
	fs.StringVar(&opts.logLevel, "log-level", "info", "debug|info|warn|error")
	fs.BoolVar(&opts.version, "version", false, "print version and exit")
	if err := fs.Parse(args); err != nil {
		return opts, err
	}
	return opts, nil
}

func run(args []string) error {
	opts, err := parseOptions(args, flag.ExitOnError)
	if err != nil {
		return err
	}

	if opts.version {
		fmt.Println(buildinfo.String())
		return nil
	}

	logger, newErr := newLogger(opts.logLevel)
	if newErr != nil {
		return newErr
	}
	slog.SetDefault(logger)

	// --- modules ----------------------------------------------------------
	//
	// Mounted as a literal list. The set is bounded and known at compile time,
	// so there is no registry and no Module interface to satisfy (§3.1/§3.2).

	source, err := interfaceSource(opts.links)
	if err != nil {
		return err
	}

	// One store for the whole box (core.ConfigPath). The module list is given
	// literally here for the same reason the mounts below are: the set is
	// bounded and known at compile time (§3.2), and the order it is given in is
	// the order the document is written in.
	// `link` comes first because everything else reads it: an interface has to
	// have been handed over before a pool, a resolver or an exit may name it.
	// `dial` follows it and precedes everything else, because it is the other
	// foundation module: it owns the uplink, and an uplink is the one thing on a
	// router that exists before any service does. That it currently contains only
	// dynamic DNS is a fact about how much of it is built, not about where it
	// sits (internal/dial's package comment).
	// `dhcp` then `dns` is §3.2's own literal list, and it is also the order
	// they matter in on a box being brought up: addresses first, then names.
	// `devices` follows both rather than leading them. Its identity half is a
	// foundation object that `gateway` and `qos` will reference (§4.4), but its
	// presence half reads the lease database through `dhcp`, so it is the last
	// of the three to come up.
	// `gateway` sits after them all, because an exit is only useful once
	// clients have addresses and names — and because docs/gateway.md §4 has its
	// domain half depending on `dns` owning :53, not the other way round. It
	// is one section carrying both directions across that boundary: where
	// traffic leaving a network goes, and what arriving from outside is
	// translated in to. docs/port-forwarding.md §6 records the one place the
	// two meet — a forwarded connection's reply must not be handed to an exit.
	// `remote` follows it and completes the direction: `gateway` decides what
	// crosses the boundary for a network, and `remote` how the operator
	// themselves gets back in to all of it (docs/remote-access.md §1). It reads `link`'s networks for what to push
	// into a client's tunnel, so it comes after `link` and depends on nothing
	// else.
	// `ingress` is the only one that is not networking at all
	// (docs/ingress.md §2). It reads `dns` for the suffix it publishes under and
	// `devices` for what to point at, so it cannot come up before either.
	// `firewall` is last because it is built from three of the others: the
	// networks say what is inside, and `remote` and `ingress` say which ports
	// are served to the outside (internal/daemon/firewall.go).
	store := configurationStore(core.RootedConfigPath(opts.root))
	checkStore(store, logger)

	// The consumers' windows onto link, all backed by one Facts: the kernel for
	// addresses and state, the document for adoption. Reading both per request
	// is what keeps §4.5 true — there is no cached copy of either to go stale
	// while olrd is running.
	//
	// The delegated prefix is created this early because link's facts read
	// it: networks numbered out of it resolve against it (§4.1 — dial reads
	// link, so link subscribes to dial's prefix rather than importing it).
	delegation := &dial.Delegation{Log: logger}
	facts := link.Facts{Source: source, Store: store, Delegated: delegation.Prefix}

	// Before anything reads the document as config, convert it if it was written
	// by a version that keyed pools by interface, or that spelled the network
	// `group`. It has to happen here: the first conversion needs a network per
	// pool, `dhcp` does not own networks, and this is the only place that sees
	// both modules. See migrateDocument for why it runs at startup rather than
	// on the way past every read.
	if _, err := migrateDocument(store, facts, logger); err != nil {
		return fmt.Errorf("migrating the stored configuration: %w", err)
	}

	networks := dhcpNetworkView{facts: facts}
	dnsLinks := dnsLinkView{facts: facts}
	gatewayLinks := gatewayLinkView{facts: facts}
	natLinks := natLinkView{facts: facts}
	dialLinks := dialLinkView{facts: facts}

	applier, err := dhcp.NewApplierAt(store, networks, opts.root)
	if err != nil {
		return fmt.Errorf("initialising dhcp: %w", err)
	}
	dnsApplier, err := dns.NewApplierAt(store, dnsLinks, dnsReservations{applier: applier}, opts.root)
	if err != nil {
		return fmt.Errorf("initialising dns: %w", err)
	}
	// The tunnel's address, read through `remote`: the resolver answers on it
	// because every client configuration names it, not because an operator
	// remembered to pin it (internal/dns/dialin.go).
	dnsApplier.DialIn = dnsDialIn{store: remote.Store{Store: store}}
	if opts.root != "" {
		logger.Warn("running against a relocated root; this is a development mode",
			"root", opts.root)
	}

	srv := core.New()
	recovery := &core.Recovery{Store: store, Lock: srv.ApplyLock(), Path: filepath.Join(opts.root, "/var/lib/open-linux-router/connectivity.json"), Capture: captureConnectivity}
	srv.Recovery = recovery
	runtimeDB, runtimeErr := runtimedb.Open(filepath.Join(opts.root, runtimedb.Path))
	if runtimeErr != nil {
		logger.Error("runtime database unavailable; history and sessions will be memory-only, QoS changes disabled", "error", runtimeErr)
	} else {
		defer runtimeDB.Close()
	}
	sessions := system.NewSessions()
	if runtimeDB != nil {
		restored, loadErr := restoreHistory(runtimeDB, logger, func() (*system.Sessions, error) {
			return system.OpenSessionsDB(runtimeDB, filepath.Join(opts.root, "/var/lib/open-linux-router/sessions.json"))
		})
		if loadErr != nil {
			logger.Error("could not restore sessions; existing logins invalidated", "error", loadErr)
		} else {
			sessions = restored
		}
	}

	// `link` is mounted first, matching the store's order and the order a box
	// is brought up in. It is the only module here that drives nothing: adopting
	// an interface writes a line of consent and touches neither the kernel nor a
	// daemon, which is precisely what lets §7 promise that installing olr
	// changes nothing until you say so.
	// `system` is mounted first, ahead of even `link`, because it is the module
	// that decides whether any of the others may be reached over the network at
	// all (docs/system.md §3). It drives no daemon and renders no file — the
	// thinnest module in the tree, and structurally so.
	srv.Mount(system.ModuleName, system.HTTP{
		Applier:       system.Applier{Store: store},
		Lock:          srv.ApplyLock(),
		Events:        srv.Events(),
		Sessions:      sessions,
		TokenRequired: opts.auth,
		ValidToken: func(r *http.Request) bool {
			if !opts.auth {
				return false
			}
			data, err := os.ReadFile(opts.tokenPath)
			return err == nil && core.ValidBearer(strings.TrimSpace(string(data)), r)
		},
	}.Routes(), system.Config{})

	// Hoisted into a variable rather than built inline like the modules above
	// it, because startup restores this one's kernel state as well as serving
	// it. See the restore block below.
	linkApplier := link.Applier{Store: store, Source: source, Delegated: delegation.Prefix}

	// The distribution's own network configuration, for the interfaces olr
	// has taken (internal/host). Rooted like the store, so a development run
	// against a scratch root edits the scratch root's files and signals
	// nothing. Both modules run it after every apply, because what it owns is
	// their two configs joined — hostDesired has the join.
	hostApplier := host.Applier{Root: opts.root, Runtime: runtimeDB, RuntimeError: runtimeErr}
	takeHost := func(ctx context.Context) ([]core.Step, error) {
		d, err := hostDesired(store)
		if err != nil {
			return nil, err
		}
		return hostApplier.Apply(ctx, d)
	}
	linkApplier.Host = takeHost
	linkApplier.CheckConfig = func(cfg link.Config) error {
		return hostApplier.CheckIPv6Auto(cfg.IPv6Auto)
	}

	// What follows a change to a network, and to the uplink (dependents.go).
	//
	// Variables assigned further down rather than closures built here, because
	// the modules that follow are constructed after the two that trigger them —
	// the mounts below are in the store's order, and `gateway` is last of the
	// three. `follow` is what makes that safe to read: it resolves the variable
	// when the request arrives, by which time every applier exists.
	var followNetworks, followUplink func(context.Context) []core.Step

	srv.Mount(link.ModuleName, link.HTTP{
		Applier: linkApplier,
		Lock:    srv.ApplyLock(),
		Events:  srv.Events(),
		Claims:  func() []link.Claim { return uplinkClaims(store) },
		HostFindings: func() []core.Problem {
			return hostFindings(hostApplier, store, false)
		},
		Dependents: follow(&followNetworks),
	}.Routes(), link.Config{})

	// `dial` is mounted second, matching the store's order. Like `link` it drives
	// no unit and renders no file and programs the kernel directly — an address,
	// the interface's state, and the default route in the main table — and like
	// `gateway` it has a background loop inside olrd: the publisher, which
	// re-reads each uplink address on a timer and calls a DNS provider only when
	// one moved (docs/ddns.md §6).
	//
	// The loop is reached through two function fields rather than held by the
	// module, exactly as the prober is below: it belongs to the process, so an
	// Applier that owned one could not be built by a test.
	publisher := dial.NewPublisher()
	publisher.Log = logger
	publisher.Links = dialLinks
	dialApplier := dial.Applier{Store: store, Links: dialLinks, Host: takeHost}

	srv.Mount(dial.ModuleName, dial.HTTP{
		Applier: dialApplier,
		Lock:    srv.ApplyLock(),
		Events:  srv.Events(),
		Watch: func(cfg dial.Config) {
			publisher.Watch(context.Background(), cfg)
			delegation.Watch(context.Background(), cfg)
		},
		States:     publisher.States,
		Delegation: delegation.State,
		Host: func() dial.HostStatus {
			return dial.HostStatus{
				Resolvers: hostApplier.Resolvers(),
				Findings:  hostFindings(hostApplier, store, true),
			}
		},
		Dependents: follow(&followUplink),
	}.Routes(), dial.Config{})

	srv.Mount(dhcp.ModuleName, dhcp.HTTP{
		Applier: applier,
		Lock:    srv.ApplyLock(),
		Events:  srv.Events(),
	}.Routes(), dhcp.Config{})

	srv.Mount(dns.ModuleName, dns.HTTP{
		Applier: dnsApplier,
		Lock:    srv.ApplyLock(),
		Events:  srv.Events(),
	}.Routes(), dns.Config{})

	// What the router remembers about devices that are not here: when each was
	// last heard from, and the name it last called itself. Sampled below for as
	// long as olrd runs, from the neighbour table and the lease file
	// respectively — the lease is what outlives a sleeping phone's last answer.
	names := dhcpNames{applier: applier}
	seen := clients.NewSeen("", clients.Neighbours{}, names, logger)
	if runtimeDB != nil {
		restored, loadErr := restoreHistory(runtimeDB, logger, func() (*clients.Seen, error) {
			return clients.OpenSeenDB(runtimeDB, clients.SeenPath(opts.root), clients.Neighbours{}, names, logger)
		})
		if loadErr != nil {
			logger.Error("could not restore what the router remembers about devices", "error", loadErr)
		} else {
			seen = restored
		}
	}

	// Hoisted out of the Mount call because `ingress` reads devices through it
	// too. One Applier, so both surfaces answer from the same join rather than
	// from two that could drift.
	clientsApplier := clients.Applier{
		Store: store,
		// Three presence sources, and the set is the point: leases know about
		// anything that asked for an address, ARP sees the statically-addressed
		// printer that never did (§10 decision 7), and ND is the IPv6 half ARP
		// has never had. Without ND a dual-stack client is one address on screen
		// and five in the kernel, which is also what traffic history used to be
		// keyed by.
		Presence: []clients.PresenceSource{
			dhcpPresence{applier: applier},
			clients.ARP{},
			clients.ND{},
		},
		Fixed:    dhcpFixedAddresses{applier: applier},
		Networks: dhcpNetworks{applier: applier},
		Seen:     seen,
	}
	clientsApplier.BeforeSave = func(doc *core.Document) error {
		return preserveIngressClients(doc, ingressClients{applier: clientsApplier})
	}

	srv.Mount(clients.ModuleName, clients.HTTP{
		Applier: clientsApplier,
		Lock:    srv.ApplyLock(),
		Events:  srv.Events(),
	}.Routes(), clients.Config{})

	// Remote equipment is observed on demand; inventory writes never alter it.
	srv.Mount(devices.ModuleName, devices.HTTP{
		Service: devices.NewService(store), Lock: srv.ApplyLock(), Events: srv.Events(),
	}.Routes(), devices.Config{})

	bootID, bootErr := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if bootErr != nil || strings.TrimSpace(string(bootID)) == "" {
		runtimeErr = fmt.Errorf("cannot identify current boot: %v", bootErr)
	}
	qosApplier := qos.Applier{Store: store, Engine: &qos.Engine{Store: store, Clients: clientsApplier,
		Runtime: runtimeDB, RuntimeError: runtimeErr, BootID: strings.TrimSpace(string(bootID)),
		StatePath: filepath.Join(opts.root, "/run/olr/qos.json")}}
	srv.Mount(qos.ModuleName, qos.HTTP{Applier: qosApplier, Lock: srv.ApplyLock(), Events: srv.Events()}.Routes(), qos.Config{})

	// `gateway` is the one module whose configuration lives in the kernel
	// rather than in a file some backend reads, so two things follow that the
	// others do not need: it is applied at startup (below), and a background
	// prober can change what the kernel should hold without the operator
	// touching anything.
	prober := gateway.NewProber()
	latency, err := gateway.OpenLatencyMonitor(store, srv.ApplyLock())
	if err != nil {
		return fmt.Errorf("loading Internet latency target: %w", err)
	}
	customLatency, err := gateway.OpenCustomLatencyConfig(store)
	if err != nil {
		return fmt.Errorf("loading custom latency sites: %w", err)
	}
	latency.Custom = customLatency
	customLatency.Log = logger
	if runtimeDB != nil {
		if loadErr := customLatency.RestoreHistory(runtimeDB); loadErr != nil {
			logger.Error("could not restore latency history", "error", loadErr)
		}
	}
	customLatency.ValidateExit = func(name string) error {
		cfg, err := (gateway.Applier{Store: store}).Load()
		if err != nil {
			return err
		}
		for _, exit := range cfg.Exits {
			if exit.Name == name {
				return nil
			}
		}
		return fmt.Errorf("gateway exit %q does not exist", name)
	}
	customLatency.Resolve = func(name string) (uint32, error) {
		cfg, err := (gateway.Applier{Store: store}).Load()
		if err != nil {
			return 0, err
		}
		if cfg.Enabled {
			for _, exit := range cfg.Exits {
				if exit.Name == name {
					return exit.Mark(), nil
				}
			}
		}
		return 0, fmt.Errorf("gateway exit %q is unavailable", name)
	}
	// The other half of an exit: where its clients resolve. Zero means the
	// router's own resolver, which is also what a site with no exit uses.
	customLatency.ResolveDNS = func(name string) (netip.AddrPort, error) {
		cfg, err := (gateway.Applier{Store: store}).Load()
		if err != nil {
			return netip.AddrPort{}, err
		}
		if cfg.Enabled {
			for _, exit := range cfg.Exits {
				if exit.Name != name {
					continue
				}
				if exit.DNS == nil {
					return netip.AddrPort{}, nil
				}
				switch exit.DNS.Mode {
				case "exit":
					if exit.Via.NextHop != nil {
						return netip.AddrPortFrom(*exit.Via.NextHop, 53), nil
					}
				case "custom":
					return exit.DNS.Server, nil
				}
				return netip.AddrPort{}, nil
			}
		}
		return netip.AddrPort{}, fmt.Errorf("gateway exit %q is unavailable", name)
	}
	prober.Log = logger
	gatewayApplier := gateway.Applier{
		Kernel: gateway.NewKernel(),
		Links:  gatewayLinks,
		Store:  store,
		Probes: prober,
		// The one fact the egress masquerade needs, read through `dial`. This
		// is design.md §4.1's `dial → gateway` arrow, and the first thing to
		// walk it.
		Uplink: gatewayUplink{store: store},
		// The tunnel's network, read through `remote`, for the masquerade that
		// makes peers look like this router on the networks it serves.
		DialIn: gatewayDialIn{store: remote.Store{Store: store}},
	}
	natApplier := gatewayApplier.NATApplier(nat.NewKernel(), natLinks)

	// The cascades the mounts above reference, now that everything they run
	// exists. In §4.1's order — `dhcp` before `gateway`, so that a renumbered
	// network is being served before it is being translated.
	followNetworks = func(ctx context.Context) []core.Step {
		return applyDependents(ctx, logger,
			dhcpDependent(applier), gatewayDependent(gatewayApplier), natDependent(natApplier),
			dnsDependent(dnsApplier))
	}
	// The delegated prefix's subscriber. After followNetworks, because a
	// network that moved to a new prefix is what everything built from the
	// networks has to follow.
	follower := newDelegationFollower(opts.root)
	follower.db, follower.dbError = runtimeDB, runtimeErr
	follower.delegation, follower.link, follower.writer = delegation, linkApplier, dial.NewWriter()
	follower.srv, follower.logger, follower.dependents = srv, logger, follow(&followNetworks)
	delegation.Changed = follower.changed

	followUplink = func(ctx context.Context) []core.Step {
		// Only the NAT table. The rest of what `dial` feeds is read live: the
		// routing half reads the main table's default route rather than
		// holding a copy of it, and `dns` reads the uplink's resolvers.
		return applyDependents(ctx, logger, natDependent(natApplier))
	}

	prober.OnChange = func(exit string, up bool) {
		// An exit changed state, so the routing the kernel should hold has
		// changed with it — a dead exit's traffic goes to `unreachable`, and a
		// recovered one gets its route back. Re-applying is how that lands,
		// and it goes through the same global apply lock as an operator's edit
		// (§3.6) so the two can never interleave.
		reapplyGateway(gatewayApplier, srv, logger, exit, up)
	}

	// One mount for the whole module, both halves. The NAT routes are appended
	// to the routing ones under the same prefix (gateway.HTTP.NAT), so `olr
	// routes` and the MCP tool list see one module rather than the two packages
	// that implement it.
	activity := gateway.NewActivity()
	if runtimeDB != nil {
		restored, loadErr := restoreHistory(runtimeDB, logger, func() (*gateway.Activity, error) {
			return gateway.OpenActivityDB(runtimeDB, filepath.Join(opts.root, "/var/lib/open-linux-router/activity.json"))
		})
		if loadErr != nil {
			logger.Error("could not restore activity history", "error", loadErr)
		} else {
			activity = restored
		}
	}
	// History was keyed by address until clients became the key. Fold what is
	// already stored into the clients that hold those addresses now, before the
	// sampler starts writing new rows, and make the fold durable so a restart
	// does not have to repeat it.
	activity.Owners = clientOwners(clientsApplier.Presence)
	foldCtx, stopFold := context.WithTimeout(context.Background(), 2*time.Minute)
	foldErr := activity.Adopt(foldCtx)
	stopFold()
	if foldErr != nil {
		logger.Warn("could not re-key stored activity history to clients", "error", foldErr)
	}
	places := geoip.NewCity(runtimeDB)
	defer places.Close()
	world := gateway.NewWorldTraffic(places)
	world.Enabled = func() bool { cfg, err := gatewayApplier.Load(); return err == nil && cfg.StatsOrDefault() }
	srv.Mount(gateway.ModuleName, gateway.HTTP{
		World:    world,
		Activity: activity,
		Latency:  latency,
		Applier:  gatewayApplier,
		Lock:     srv.ApplyLock(),
		Events:   srv.Events(),
		Watch: func(cfg gateway.Config) {
			prober.Watch(context.Background(), cfg)
			// Gateway and DNS share one document. Refresh the relay's routing
			// snapshot after the gateway write, not before it.
			if current, err := dnsApplier.Load(); err != nil {
				logger.Error("could not load DNS routing after gateway change", "error", err)
			} else if current.Enabled {
				if _, err := dnsApplier.Apply(context.Background(), current); err != nil {
					logger.Error("could not update DNS routing after gateway change", "error", err)
				}
			}
		},
		NAT: &nat.HTTP{
			Applier: natApplier,
			Lock:    srv.ApplyLock(),
			Events:  srv.Events(),
		},
	}.Routes(), gateway.Config{})

	// `remote` is the second module whose configuration lives in the kernel
	// rather than in a file some backend reads, so it is applied at startup for
	// the same reason `gateway` is (below). It supervises no
	// unit at all, and that is not an omission: WireGuard's data path is in the
	// kernel, so there is no process for systemd to watch
	// (docs/remote-access.md §7.1).
	// Two objects, two appliers, one document. They are separate types because
	// what they drive has nothing in common — the tunnel is kernel state with
	// no daemon, the proxy is a rendered file and a unit — and they share
	// `remote.Store` so that both read stored intent through the same code.
	remoteStore := remote.Store{Store: store}
	remoteTunnel := remote.TunnelApplier{
		Store:    remoteStore,
		Kernel:   remote.NewKernel(),
		Networks: remoteNetworks{facts: facts},
	}
	socksUnit, err := core.NewUnit(remote.SocksUnitName)
	if err != nil {
		logger.Warn("no service manager for the SOCKS5 proxy", "error", err)
	}
	remoteSocks := remote.ProxyApplier{
		Store:  remoteStore,
		Paths:  remote.RootedPaths(opts.root),
		Unit:   socksUnit,
		Object: remote.ObjectSocks,
	}

	// A peer's page is keyed by address, because a peer has no hardware address
	// anywhere else in the model either. Its traffic is therefore the one thing
	// the activity sampler records without a client behind it; everything else
	// that cannot be attributed is dropped instead of stored where no screen can
	// look. Read lazily: peers are added and removed while the sampler runs.
	activity.Names = func(context.Context) map[netip.Addr]bool {
		cfg, err := remoteTunnel.Load()
		if err != nil {
			return nil
		}
		named := make(map[netip.Addr]bool, len(cfg.WireGuard.Peers))
		for _, peer := range cfg.WireGuard.Peers {
			if peer.Address.IsValid() {
				named[peer.Address.Unmap()] = true
			}
		}
		return named
	}

	// design.md §4.1's remote → dns and remote → gateway arrows: moving the
	// tunnel changes an address the relay was rendered to answer on, and the
	// subnet the NAT table masquerades, and nothing in either dependent's own
	// intent moved — so neither can notice by itself. Re-applied under the same
	// apply lock as the tunnel edit.
	remoteTunnel.Follow = func(ctx context.Context) []core.Step {
		return applyDependents(ctx, logger, dnsDependent(dnsApplier), natDependent(natApplier))
	}

	srv.Mount(remote.ModuleName, remote.HTTP{
		Tunnel: remoteTunnel,
		Socks:  remoteSocks,
		Lock:   srv.ApplyLock(),
		Events: srv.Events(),
		Places: places,
	}.Routes(), remote.Config{})

	// `ingress` is mounted last, matching the store's order. Its two views are
	// adapted in ingress.go for the same reason the dhcp↔devices ones are: the
	// module declares what it needs and imports neither of the modules that
	// answer, so §4.1's arrows keep pointing one way.
	ingressApplier, err := ingress.NewApplierAt(store,
		ingressDNS{applier: dnsApplier}, ingressClients{applier: clientsApplier}, opts.root)
	if err != nil {
		return fmt.Errorf("initialising ingress: %w", err)
	}
	srv.Mount(ingress.ModuleName, ingress.HTTP{
		Applier: ingressApplier,
		Lock:    srv.ApplyLock(),
		Events:  srv.Events(),
	}.Routes(), ingress.Config{})

	// `firewall` is mounted last, matching the store's order. It re-applies
	// itself whenever a module it is built from announces a change
	// (followFirewall), rather than through a Dependents hook on each.
	firewallApplier := firewall.Applier{
		Store:    store,
		Kernel:   firewall.NewKernel(),
		Boundary: firewallBoundary{facts: facts, store: store},
	}
	srv.Mount(firewall.ModuleName, firewall.HTTP{
		Applier: firewallApplier,
		Lock:    srv.ApplyLock(),
		Events:  srv.Events(),
	}.Routes(), firewall.Config{})

	iptvUnit, err := core.NewUnit(iptv.UnitName)
	if err != nil {
		logger.Warn("no service manager for IPTV", "error", err)
	}
	iptvApplier := iptv.Applier{Store: store, Links: iptvLinks{facts: facts}, Unit: iptvUnit, Root: opts.root}
	srv.Mount(iptv.ModuleName, iptv.HTTP{
		Applier: iptvApplier, Lock: srv.ApplyLock(), Events: srv.Events(),
		Follow: func(ctx context.Context) error {
			c, err := firewallApplier.Load()
			if err != nil {
				return err
			}
			if len(c.Ruleset) > 0 {
				return nil
			}
			_, _, err = firewallApplier.Reapply(ctx)
			return err
		},
	}.Routes(), iptv.Config{})

	socksOutUnit, err := core.NewUnit(socksout.UnitName)
	if err != nil {
		logger.Warn("no service manager for outbound SOCKS5", "error", err)
	}
	socksApplier := socksout.Applier{Store: store, Unit: socksOutUnit, Root: opts.root}
	srv.Mount(socksout.ModuleName, socksout.HTTP{
		Applier: socksApplier, Lock: srv.ApplyLock(), Events: srv.Events(),
		Follow: func(ctx context.Context) error {
			cfg, err := gatewayApplier.Load()
			if err != nil {
				return err
			}
			if !cfg.Enabled {
				return nil
			}
			_, _, err = gatewayApplier.Apply(ctx, cfg, netip.Addr{})
			return err
		},
	}.Routes(), socksout.Config{})

	// Inspection owns an independent proxy unit. Reattachment must happen before
	// stale-rule recovery so redeploy leaves the active data path untouched.
	inspector := &inspection.Service{Enabled: opts.root == "", Managed: opts.root == "", Dir: filepath.Join(opts.root, "/var/lib/open-linux-router/inspection")}
	// The recorder samples only while inspection is on and checkpoints retained
	// per-device activity independently of the live proxy's request archive.
	//
	// Two name sources, relay first: it names every client, including the ones
	// the proxy never sees. The proxy's own observations come second and fill
	// what the relay could not name — a device that dialled an address, resolved
	// it elsewhere, or asked before the switch was on.
	inspector.Activity = inspection.NewActivity(inspection.Flows, inspection.NamesFrom(
		inspectionNames(dnsApplier.Observer),
		func(context.Context) ([]inspection.Name, error) { return inspector.Names(), nil },
	))
	if err := inspector.ConfigureActivityStorage(filepath.Join(inspector.Dir, "activity")); err != nil {
		logger.Warn("inspection activity history is unavailable", "error", err)
	}
	inspector.ExitMark = func(mac string) (uint32, error) {
		cfg, err := gatewayApplier.Load()
		if err != nil {
			return 0, err
		}
		if !cfg.Enabled {
			return 0, nil
		}
		for _, client := range cfg.Clients {
			if client.MAC == mac {
				for _, exit := range cfg.Exits {
					if exit.Name == client.Exit {
						if exit.Via.Kind == gateway.ViaBlocked {
							return 0, fmt.Errorf("client exit %q is blocked", exit.Name)
						}
						return exit.Mark(), nil
					}
				}
				return 0, fmt.Errorf("device exit %q is unavailable", client.Exit)
			}
		}
		return 0, nil
	}
	srv.Mount(inspection.ModuleName, inspector.Routes(), struct{}{})
	toolHTTP := &tools.HTTP{LANAddresses: func() ([]string, error) {
		cfg, err := linkApplier.Load()
		if err != nil {
			return nil, err
		}
		observed, err := link.Kernel()
		if err != nil {
			return nil, err
		}
		var addresses []string
		for _, network := range cfg.Networks {
			if network.IPv4 == nil {
				continue
			}
			for _, iface := range observed {
				if !iface.Up || iface.Loopback || !slices.Contains(network.Members, iface.Name) {
					continue
				}
				for _, prefix := range iface.Prefixes {
					address := prefix.Addr()
					if address.Is4() && address == network.IPv4.RouterAddr() {
						addresses = append(addresses, address.String())
					}
				}
			}
		}
		return slices.Compact(slices.Sorted(slices.Values(addresses))), nil
	}, ResolveExit: func(name string) (uint32, error) {
		cfg, err := (gateway.Applier{Store: store}).Load()
		if err != nil {
			return 0, err
		}
		if cfg.Enabled {
			for _, exit := range cfg.Exits {
				if exit.Name == name && exit.Via.Kind != gateway.ViaBlocked {
					return exit.Mark(), nil
				}
			}
		}
		return 0, fmt.Errorf("way out %q is unavailable", name)
	}, DNSLegs: func() ([]tools.DNSLeg, error) {
		// The resolvers this box can name, not a guess at one: its own, and
		// every exit whose DNS setting points somewhere else. The relay
		// renders the same list for its routes file.
		cfg, err := dnsApplier.Load()
		if err != nil {
			return nil, err
		}
		var legs []tools.DNSLeg
		if cfg.Enabled {
			legs = append(legs, tools.DNSLeg{Name: "This router", Server: dns.DefaultResolver.String(), Kind: "router"})
		}
		g, err := gatewayApplier.Load()
		if err != nil {
			return nil, err
		}
		if g.Enabled {
			for _, exit := range g.Exits {
				if exit.DNS == nil {
					continue
				}
				var server netip.AddrPort
				switch exit.DNS.Mode {
				case "exit":
					if exit.Via.NextHop != nil {
						server = netip.AddrPortFrom(*exit.Via.NextHop, 53)
					}
				case "custom":
					server = exit.DNS.Server
				}
				if server.IsValid() {
					legs = append(legs, tools.DNSLeg{Name: exit.Name, Server: server.String(), Kind: "exit"})
				}
			}
		}
		return legs, nil
	}, Locate: func(ctx context.Context, addrs []netip.Addr) (map[string]geoip.Place, error) {
		located, _ := places.LocateNow(ctx, addrs)
		return located, nil
	}}
	srv.Mount(tools.ModuleName, toolHTTP.Routes(), struct{}{})

	// --- routes -----------------------------------------------------------
	recovery.Rollback = func(ctx context.Context, from, before core.Document, runtime json.RawMessage) error {
		previousLink, err := link.FromDocument(from)
		if err != nil {
			return err
		}
		previousDial, err := dial.FromDocument(from)
		if err != nil {
			return err
		}
		previousRemote, err := remote.FromDocument(from)
		if err != nil {
			return err
		}
		if err := store.Save(before); err != nil {
			return err
		}
		var failures []error
		failures = append(failures, dialApplier.Recover(ctx, previousDial), linkApplier.Recover(ctx, previousLink), restoreConnectivity(runtime))
		repair := func(name string, fn func(context.Context) error) func(context.Context) error {
			return func(ctx context.Context) error {
				_, was := from.Raw(name)
				_, exists := before.Raw(name)
				if !was && !exists && name != firewall.ModuleName {
					return nil
				}
				return fn(ctx)
			}
		}
		failures = append(failures, reconcileConfiguration(ctx,
			repair(dhcp.ModuleName, func(ctx context.Context) error {
				c, e := applier.Load()
				if e != nil {
					return e
				}
				_, e = applier.Apply(ctx, c)
				return e
			}),
			repair(remote.ModuleName, func(ctx context.Context) error {
				return remoteTunnel.Recover(ctx, previousRemote)
			}),
			repair(remote.ModuleName, func(ctx context.Context) error {
				c, e := remoteSocks.Load()
				if e != nil {
					return e
				}
				_, e = remoteSocks.Apply(ctx, c)
				return e
			}),
			repair(dns.ModuleName, func(ctx context.Context) error {
				c, e := dnsApplier.Load()
				if e != nil {
					return e
				}
				_, e = dnsApplier.Apply(ctx, c)
				return e
			}),
			repair(socksout.ModuleName, func(ctx context.Context) error {
				c, e := socksApplier.Load()
				if e != nil {
					return e
				}
				_, e = socksApplier.Apply(ctx, c, false)
				return e
			}),
			repair(gateway.ModuleName, func(ctx context.Context) error {
				c, e := gatewayApplier.Load()
				if e != nil {
					return e
				}
				_, _, e = gatewayApplier.Apply(ctx, c, netip.Addr{})
				return e
			}),
			repair(gateway.ModuleName, func(ctx context.Context) error {
				c, e := natApplier.Load()
				if e != nil {
					return e
				}
				_, _, e = natApplier.Apply(ctx, c)
				return e
			}),
			repair(ingress.ModuleName, func(ctx context.Context) error {
				c, e := ingressApplier.Load()
				if e != nil {
					return e
				}
				_, e = ingressApplier.Apply(ctx, c)
				return e
			}),
			repair(iptv.ModuleName, func(ctx context.Context) error {
				c, e := iptvApplier.Load()
				if e != nil {
					return e
				}
				_, e = iptvApplier.Apply(ctx, c, false)
				return e
			}),
			repair(qos.ModuleName, qosApplier.Reconcile),
			repair(firewall.ModuleName, func(ctx context.Context) error { _, _, e := firewallApplier.Reapply(ctx); return e }),
		))
		if c, e := dialApplier.Load(); e == nil {
			publisher.Watch(context.Background(), c)
			delegation.Watch(context.Background(), c)
		}
		if c, e := gatewayApplier.Load(); e == nil {
			prober.Watch(context.Background(), c)
		}
		return errors.Join(failures...)
	}
	if err := recovery.Load(context.Background()); err != nil {
		logger.Error("connectivity recovery needs attention; journal retained", "error", err)
	}
	srv.Mount("recovery", recovery.Routes(), core.RecoveryRequest{})
	backup := &core.Backup{
		Store:    store,
		Recovery: recovery,
		// A backup written before the two inventories swapped names is
		// migrated here rather than refused: the sections it names still exist,
		// under the words the box now uses.
		Prepare: func(doc core.Document) (core.Document, error) {
			doc, _, err := renameVocabulary(doc)
			return doc, err
		},
		Validate: func(ctx context.Context, doc core.Document) error { return validateBackup(ctx, doc, source) },
	}
	srv.Mount("configuration", backup.Routes(), configurationSchema{})
	//
	// The API and the SPA are composed here rather than inside core, which has
	// no business knowing a UI exists. ServeMux prefers the longer pattern, so
	// /api/ wins over / without any ordering subtlety.

	top := http.NewServeMux()
	top.Handle(core.APIPrefix+"/", srv.Handler())
	top.HandleFunc("GET /download/inspection-ca.crt", inspector.PublicCA)
	top.Handle("/inspection-web/", http.StripPrefix("/inspection-web", http.HandlerFunc(inspector.WebUI)))
	// LibreSpeed's page and the byte endpoints under it live here rather than under
	// /api because the upload test posts 20 MB of raw bytes, which the API's
	// JSON-body guard exists to refuse. What authorizes them is a viewer cookie
	// minted inside the API — the same shape as /inspection-web/ above.
	top.Handle(tools.LinkTestPrefix+"/", toolHTTP.LinkTestHandler())
	// The brand marks the pickers offer are static artwork with no
	// configuration in them, so they sit outside /api for the same reason the
	// SPA does: an <img> must render without a session, and one installation
	// ships exactly what another does.
	top.Handle(brandlogo.Prefix, brandlogo.Handler())
	top.Handle(brandlogo.Prefix+"/", brandlogo.Handler())
	if !brandlogo.Built() {
		// Like the SPA, the catalogue comes from the web build; a binary
		// compiled without it still runs, and this is the one line that
		// explains why the picker is showing category pictures.
		logger.Warn("this olrd was built without the brand-logo catalogue",
			"note", "the device and site pickers fall back to category pictures; run `make web` before `make build` to include the marks")
	}
	top.Handle("/", webui.Handler())

	// The MCP surface (§6.4), composed here for the same reason the SPA is:
	// core has no business knowing an agent exists, and mcp.New takes the API
	// handler rather than the server so that it cannot reach past it.
	//
	// It is mounted under /api on purpose. ServeMux prefers the more specific
	// pattern, so this wins over /api/ above with no ordering subtlety, and
	// sitting inside the prefix means it inherits the authentication both
	// listeners already apply — the token on TCP, the socket's mode locally.
	// An MCP endpoint outside /api would be an unauthenticated admin surface.
	mcpServer, err := mcp.New(srv.Handler())
	if err != nil {
		return err
	}
	top.Handle(core.APIPrefix+"/mcp", mcpServer)
	logger.Info("serving MCP", "path", core.APIPrefix+"/mcp", "tools", len(mcpServer.Tools()))

	// Shared units innermost, so its release runs as a panic unwinds and
	// recovery still sees the panic.
	handler := core.WithLogging(core.WithRecovery(core.WithSharedUnits(top)))

	// --- listeners --------------------------------------------------------

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go recovery.Run(ctx, func(err error) { logger.Error("connectivity recovery failed; retrying", "error", err) })
	toolHTTP.QualityContext = ctx
	toolHTTP.QualityPath = filepath.Join(opts.root, "/var/lib/open-linux-router/quality.json")
	if err := toolHTTP.RestoreQuality(); err != nil {
		logger.Error("could not restore quality assessment", "error", err)
	}

	// Stored intent is put back in force here, for the modules that need it.
	//
	// Most of them need it because the kernel forgets: interface addresses,
	// nftables rules, `ip rule` entries, route tables and a WireGuard interface
	// do not survive a reboot, so without this a box would come back up with
	// its configuration intact and none of it in force — and for `remote` that
	// means an operator who is away from the box being unable to reach the box
	// they would fix it from.
	//
	// `link` goes first: dnsmasq needs router addresses inside its pools,
	// and gateway policies describe networks the box actually holds.
	// This is also the line whose absence was the bug: `link` programs
	// addresses through netlink and was never in this list, so a reboot took
	// the router's own address off the box and three modules broke downstream
	// of it. link.Applier.Restore has the rest, including why it is additive
	// where an operator's apply is not.
	//
	// `dial` comes straight after it, which is the store's order and the order
	// a box is brought up in: the two foundation modules, addresses and then
	// the way out. It is on this list for the same reason `link` is — an
	// address and a default route are kernel state and the kernel forgets both
	// — and the consequence of leaving it off would be worse, because the thing
	// missing after the reboot is how anybody reaches the box to notice.
	//
	// `dns` is here for a different reason, and it is the reason the other
	// file-rendering module is not. dnsmasq and unbound read files that survive
	// a reboot, so for years this line read "and they are the only three modules
	// that need this". What that assumed is that the file is still where the
	// unit looks for it, and an olr upgrade can move it: the release that put
	// the resolver's config inside /etc/unbound, where Debian's AppArmor profile
	// lets the daemon read it, left every already-configured box with a unit
	// pointing at a path nothing had written yet. Nothing noticed until the next
	// reboot, when unbound failed its config check and the building lost DNS.
	// So `dns` re-renders itself here, and only when the files on disk are not
	// the ones stored intent produces — startDNS has the rest.
	//
	// All of them are idempotent by construction — the plan against an
	// already-correct box is empty and nothing is written — which is what keeps
	// design.md §3.5's invariant true: `systemctl restart olrd` re-runs them and
	// disturbs no traffic.
	//
	// None of them ever fails the start. A box whose routing cannot be
	// programmed is exactly the box whose API has to come up, because the API is
	// how it gets fixed.
	//
	// `firewall` goes before all of them, and after nothing: it is built from
	// stored intent alone — network members, and the ports `remote` and
	// `ingress` serve — so it needs no restored address to be correct, and
	// every moment the box is up before it is a moment the default stance is
	// not in force. Its follower starts first so that nothing the restores
	// below announce is missed.
	go followFirewall(ctx, firewallApplier, srv, logger)
	reapplyFirewall(ctx, firewallApplier, srv, logger, "startup")
	startLink(ctx, linkApplier, logger)
	startDial(ctx, dialApplier, publisher, delegation, logger)
	startHost(ctx, srv, takeHost, logger)
	startSocksOut(ctx, socksApplier, logger)
	startGateway(ctx, gatewayApplier, prober, logger)
	startForwards(ctx, natApplier, logger)
	startIPTV(ctx, iptvApplier, logger)
	startRemote(ctx, remoteTunnel, logger)
	startDNS(ctx, dnsApplier, logger)
	// MAC classification does not depend on DHCP or addresses. Reconcile the
	// LAN placement after startup and as devices move between served networks.
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			if err := srv.ApplyLock().Do(ctx, func() error { return qosApplier.Reconcile(ctx) }); err != nil && ctx.Err() == nil {
				logger.Warn("could not apply device QoS", "error", err)
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()

	// And then keep looking. The line above converges the service half once, at
	// startup, which covers the box that comes back from a reboot or an upgrade
	// — but a unit somebody stops at noon stays stopped until olrd next
	// restarts, and on a router that is measured in days of no DNS. See
	// supervise.go for what it will and will not touch.
	go superviseBackends(ctx, []backend{
		dnsBackend(dnsApplier),
		dhcpBackend(applier),
	}, srv.ApplyLock(), superviseEvery, logger)

	seenDone := make(chan struct{})
	seenCtx, stopSeen := context.WithCancel(ctx)
	go func() { defer close(seenDone); seen.Run(seenCtx, seenEvery, seenWrite) }()
	defer func() {
		stopSeen()
		<-seenDone
		if err := seen.Flush(time.Now()); err != nil {
			logger.Warn("could not save what the router remembers about devices", "error", err)
		}
	}()
	go latency.Run(ctx)
	customLatencyCtx, stopCustomLatency := context.WithCancel(ctx)
	customLatencyDone := make(chan struct{})
	go func() {
		defer close(customLatencyDone)
		customLatency.Run(customLatencyCtx)
	}()
	defer func() {
		stopCustomLatency()
		<-customLatencyDone
	}()
	go world.Run(ctx)
	go inspector.Activity.Run(ctx)
	// Reattach without restarting the proxy before serving the device controls.
	if err := inspector.Resume(ctx); err != nil {
		logger.Error("could not resume inspection", "error", err)
	}
	go func() {
		tick := time.NewTicker(time.Minute)
		defer tick.Stop()
		for {
			if err := inspector.SweepHistory(ctx); err != nil {
				logger.Warn("could not prune inspection history", "error", err)
			}
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
			}
		}
	}()
	activityDone := make(chan struct{})
	activityCtx, stopActivity := context.WithCancel(ctx)
	go func() {
		defer close(activityDone)
		activity.Run(activityCtx, gatewayApplier)
	}()
	defer func() {
		stopActivity()
		<-activityDone
		if err := activity.Save(); err != nil {
			logger.Error("could not save activity history on exit", "error", err)
		}
	}()

	var listeners []net.Listener

	unix, err := core.ListenUnix(opts.socket)
	if err != nil {
		return err
	}
	listeners = append(listeners, unix)
	logger.Info("listening", "socket", opts.socket, "auth", "socket permissions")

	if opts.listen != "" {
		tcp, authed, err := tcpListener(opts, system.Applier{Store: store}, handler, logger, sessions)
		if err != nil {
			unix.Close()
			return err
		}
		listeners = append(listeners, tcp.listener)
		logger.Info("listening", "address", opts.listen, "auth", authed)

		// Two servers rather than one, because they do not share a handler:
		// the socket is authenticated by its file mode, the TCP listener by a
		// token. Wrapping both in the token check would break `olr` over the
		// socket for no gain.
		go serve(tcp.server, tcp.listener, logger)
	}

	server := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		// No WriteTimeout: it would cut off /api/events, which is a long-lived
		// stream by design (§6.3).
	}
	go serve(server, unix, logger)

	// Told to systemd only once every listener is bound, which is what makes
	// Type=notify worth having: `olr start` returns when the API is
	// actually answering, not when the process was forked. Outside systemd
	// NOTIFY_SOCKET is unset and this is a no-op.
	if _, err := sddaemon.SdNotify(false, sddaemon.SdNotifyReady); err != nil {
		logger.Warn("could not notify systemd of readiness", "error", err)
	}

	<-ctx.Done()
	logger.Info("shutting down")
	toolHTTP.Close()
	if err := inspector.Close(); err != nil {
		logger.Error("inspection cleanup failed", "error", err)
	}
	_, _ = sddaemon.SdNotify(false, sddaemon.SdNotifyStopping)

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err = server.Shutdown(shutdownCtx)
	for _, l := range listeners {
		l.Close()
	}
	return err
}

// How often the neighbour table is sampled, and how often what it heard is
// written down. A REACHABLE entry lasts about half a minute, so sampling
// more slowly than that would miss devices that were only briefly heard.
const (
	seenEvery = 10 * time.Second
	seenWrite = 5 * time.Minute
)

type tcpServer struct {
	server   *http.Server
	listener net.Listener
}

// The two API routes an unclaimed box answers over TCP.
//
// claimPath is how the state stops being unclaimed. accessPath is how the SPA
// finds out which screen to draw — without it the UI would have to infer
// "unclaimed" from a 409 on some unrelated route, and a box that cannot say
// what is wrong with it sends the operator back to reading JSON, which is
// where this whole line of work started. It answers two booleans and
// deliberately nothing else (system.AccessView), so serving it early costs no
// secret.
const (
	claimPath  = core.APIPrefix + "/system/access/claim"
	accessPath = core.APIPrefix + "/system/access"
)

// openWhileUnclaimed reports whether a request may proceed on an unclaimed box.
func openWhileUnclaimed(r *http.Request) bool {
	switch {
	case !strings.HasPrefix(r.URL.Path, core.APIPrefix):
		return true // the SPA and its assets
	case r.URL.Path == claimPath && r.Method == http.MethodPost:
		return true
	case r.URL.Path == accessPath && r.Method == http.MethodGet:
		return true
	default:
		return false
	}
}

// gateUnclaimed refuses to configure a box nobody has claimed.
//
// docs/system.md §3, and the reason olrd can listen from the moment it is
// installed:
//
//	An unclaimed box cannot be configured over the network.
//
// The SPA is always served, because it is the thing that offers the choice and
// it is not a secret. The claim route is always served, because it is how the
// state stops being unclaimed. Everything else under /api answers 409 until
// somebody has decided.
//
// **This lives here rather than in the UI on purpose.** A scanner does not load
// the SPA; it posts to /api/dhcp/config. Putting the rule in the onboarding
// screen would be a lock on the front door of a building with no walls.
//
// Only the TCP listener is wrapped. The unix socket is untouched, for the
// reason §6.2 gives about it being authenticated by its file mode: somebody
// with root on the box does not have to claim it in a browser to run `olr`, and
// `olr claim` exists for the operator who never opens one.
//
// The state is read per request rather than cached. It changes at most once in
// a box's life, so caching buys nothing, and a stale cache would mean either a
// claimed box still refusing work or an unclaimed one accepting it.
func gateUnclaimed(applier system.Applier, top http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if openWhileUnclaimed(r) {
			top.ServeHTTP(w, r)
			return
		}

		cfg, err := applier.Load()
		if err != nil {
			// Fail closed. A box whose configuration cannot be read is not one
			// to accept configuration changes for, and the operator's way in is
			// the socket, which this never touches.
			core.WriteError(w, http.StatusServiceUnavailable,
				"cannot read the configuration to check whether this box has been set up: "+err.Error())
			return
		}
		if cfg.Claimed() {
			top.ServeHTTP(w, r)
			return
		}

		core.WriteError(w, http.StatusConflict,
			"this router has not been set up yet. Open it in a browser to finish, "+
				"or run `sudo olr claim` on the box")
	})
}

// authenticateAPI puts the token in front of the API and nothing else.
//
// The bug this fixes is one level of mounting. BearerAuth used to wrap the
// whole mux, which includes `/` — the SPA itself. So a browser pointed at the
// listener was answered with `{"error":{"message":"missing or invalid API
// token; see /etc/open-linux-router/api-token"}}` as raw JSON, and never loaded
// the JavaScript. The SPA has had a working token prompt the whole time
// (web/src/components/layout/auth-gate.tsx probes /api/modules and asks on a
// 401), and it could not run, because the page carrying it was behind the thing
// it existed to collect. The documented flow — "browse to the UI and paste the
// token when asked" — was unreachable from a browser.
//
// Static assets are not secrets: the SPA is the same bundle for every
// installation and holds no configuration. What must be authenticated is the
// API, and that is exactly what is wrapped here.
func authenticateAPI(token string, top http.Handler) http.Handler {
	authed := core.BearerAuth(token, top)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == core.APIPrefix || strings.HasPrefix(r.URL.Path, core.APIPrefix+"/") {
			authed.ServeHTTP(w, r)
			return
		}
		top.ServeHTTP(w, r)
	})
}

// tcpListener builds the TCP half, which is the only surface that needs
// authenticating.
func tcpListener(opts options, access system.Applier, handler http.Handler, logger *slog.Logger, sessionManager ...*system.Sessions) (tcpServer, string, error) {
	authed := "none"
	sessions := system.NewSessions()
	if len(sessionManager) > 0 {
		sessions = sessionManager[0]
	}
	if (opts.tlsCert == "") != (opts.tlsKey == "") {
		return tcpServer{}, "", fmt.Errorf("--tls-cert and --tls-key must be provided together")
	}
	var tlsConfig *tls.Config
	if opts.tlsCert != "" {
		certificate, err := tls.LoadX509KeyPair(opts.tlsCert, opts.tlsKey)
		if err != nil {
			return tcpServer{}, "", fmt.Errorf("loading HTTPS credentials: %w", err)
		}
		tlsConfig = &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{certificate}}
	}

	// Claim/login remain reachable before authentication; all other API routes
	// pass both the access credential check and the once-only claim gate.
	handler = gateUnclaimed(access, handler)

	var token string
	if opts.auth {
		var err error
		token, err = core.LoadOrCreateToken(opts.tokenPath)
		if err != nil {
			return tcpServer{}, "", err
		}
		authed = "bearer token"
	} else if cfg, err := access.Load(); err == nil && !cfg.Claimed() {
		logger.Info("this box has not been set up yet; the web UI will ask",
			"address", opts.listen)
	} else if err == nil && !cfg.RequiresPassword() {
		// Stated every start. The operator chose this — an absent password is a
		// recorded decision now, not an oversight (docs/system.md §2) — but
		// "chosen once, months ago" and "still true" are different questions,
		// and the journal is where somebody looks when they are asking the
		// second one.
		logger.Warn("the web UI requires no password",
			"address", opts.listen,
			"note", "anyone who can reach this address can configure this router")
	}
	if cfg, err := access.Load(); err == nil && cfg.RequiresPassword() {
		authed = "administrator password"
	}
	handler = system.Authenticate(access, sessions, token, handler)
	handler = core.BrowserGuard(handler)
	if opts.adminNetworks != "" {
		var prefixes []netip.Prefix
		for _, value := range strings.Split(opts.adminNetworks, ",") {
			prefix, err := netip.ParsePrefix(strings.TrimSpace(value))
			if err != nil {
				return tcpServer{}, "", fmt.Errorf("invalid --admin-networks prefix: %w", err)
			}
			prefixes = append(prefixes, prefix.Masked())
		}
		next := handler
		handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			host, _, err := net.SplitHostPort(r.RemoteAddr)
			addr, parseErr := netip.ParseAddr(host)
			if err == nil && parseErr == nil {
				for _, prefix := range prefixes {
					if prefix.Contains(addr.Unmap()) {
						next.ServeHTTP(w, r)
						return
					}
				}
			}
			core.WriteError(w, 403, "client address is outside the configured management networks")
		})
	}

	l, err := core.ListenTCP(opts.listen)
	if err != nil {
		return tcpServer{}, "", err
	}
	if tlsConfig != nil {
		l = tls.NewListener(l, tlsConfig)
	}
	return tcpServer{
		server: &http.Server{
			Handler:           handler,
			ReadHeaderTimeout: 10 * time.Second,
		},
		listener: l,
	}, authed, nil
}

func serve(s *http.Server, l net.Listener, logger *slog.Logger) {
	if err := s.Serve(l); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("server stopped", "error", err)
	}
}

// checkStore reads the configuration document once at startup, to say something
// about it while there is still an operator watching.
//
// It never fails the start. A box whose config file is corrupt is exactly the
// box whose API has to come up, because the API is how it gets fixed — and each
// request reports the parse error again on its own. What this buys is that the
// error is in the journal at boot rather than only in the reply to whoever asks
// first.
func checkStore(store *core.Store, logger *slog.Logger) {
	if err := store.Protect(); err != nil {
		logger.Error("configuration permissions could not be tightened", "error", err)
	}
	doc, err := store.Load()
	if err != nil {
		logger.Error("configuration could not be read; the API will report this on every request",
			"path", store.Path(), "error", err)
		return
	}
	if unknown := doc.Unknown(); len(unknown) > 0 {
		// Preserved, not dropped: this is what an older olr sees after a newer
		// one has written a module it does not have. Saying so beats silence,
		// because the alternative reading — "my config vanished" — is the one
		// an operator would otherwise reach for.
		logger.Warn("configuration contains sections for modules this build does not have; they are preserved untouched",
			"path", store.Path(), "sections", strings.Join(unknown, ", "))
	}
	if legacy := store.LegacyPaths(); len(legacy) > 0 {
		logger.Warn("per-module configuration files found; they are read only when the document is absent and can be deleted once it exists",
			"document", store.Path(), "files", strings.Join(legacy, ", "))
	}
}

// startGateway programs stored gateway intent and starts the health probes.
func startGateway(ctx context.Context, a gateway.Applier, prober *gateway.Prober, logger *slog.Logger) {
	cfg, err := a.Load()
	if err != nil {
		logger.Error("gateway configuration could not be read; nothing was programmed",
			"error", err)
		return
	}
	if cfg.Empty() && !cfg.Enabled {
		return
	}

	result, _, err := a.Apply(ctx, cfg, netip.Addr{})
	switch {
	case err != nil && result.Plan.Blocked != "":
		// §6's refusal, which is the one failure here an operator can act on
		// directly — and the one where saying nothing would leave them
		// wondering why their exits do nothing.
		logger.Error("gateway was not applied because something else is managing it",
			"reason", result.Plan.Blocked)
	case err != nil:
		logger.Error("gateway could not be applied", "error", err,
			"steps", len(result.Steps))
	case !result.Plan.Empty():
		logger.Info("gateway applied", "changes", len(result.Plan.Changes))
	}

	prober.Watch(ctx, cfg)
}

// startLink puts the router's own addresses back after a reboot.
//
// First of the restore steps, because everything after it is written against
// the addresses this puts back — the block at the call site has the ordering
// argument, and link.Applier.Restore has the rest.
//
// The empty case is silent on purpose. A box with no networks configured is
// the overwhelmingly common state of a freshly installed olr, and logging
// "nothing to restore" on every boot of every such box would teach an operator
// to skim exactly the lines this block writes when something is wrong.
func startLink(ctx context.Context, a link.Applier, logger *slog.Logger) {
	steps, err := a.Restore(ctx)
	if err != nil {
		logger.Error("interface addressing could not be restored", "error", err,
			"steps", len(steps))
		return
	}

	var done int
	for _, s := range steps {
		if s.Done {
			done++
		}
		if s.Error != "" {
			// Per step rather than per call: a multi-interface restore that
			// half-lands is reported as what landed and what did not, the same
			// way the writer reports it to an operator (link.Writer).
			logger.Error("interface addressing step failed",
				"step", s.Description, "error", s.Error)
		}
	}
	if done > 0 {
		// Only when something was actually programmed. An idempotent restore
		// against a box that never rebooted is the normal case and says
		// nothing.
		logger.Info("interface addressing restored", "changes", done)
	}
}

// startForwards programs the stored port forwards and the egress masquerade.
//
// Simpler than startGateway because there is nothing to refuse and nothing to
// watch: this half never blocks on another program owning its table — a foreign
// filter is reported, not treated as a conflict (docs/port-forwarding.md §5.2)
// — and it has no prober to start afterwards.
//
// It runs even on a box with no forwards, because the egress masquerade is the
// other thing in this table and a LAN that reaches the internet depends on it
// coming back after a reboot (docs/gateway.md §3.9).
func startForwards(ctx context.Context, a nat.Applier, logger *slog.Logger) {
	cfg, err := a.Load()
	if err != nil {
		logger.Error("port forwarding configuration could not be read; nothing was programmed",
			"error", err)
		return
	}
	if cfg.ProgramsNothing() {
		return
	}

	result, _, err := a.Apply(ctx, cfg)
	switch {
	case err != nil:
		logger.Error("port forwards could not be applied", "error", err,
			"steps", len(result.Steps))
	case !result.Plan.Empty():
		logger.Info("port forwards applied", "changes", len(result.Plan.Changes))
	}
}

// startDNS re-renders the resolver's files when they are not the ones stored
// intent produces.
//
// The odd one out among these, and deliberately the most reluctant. The other
// three restore kernel state that a reboot always destroys, so they run their
// apply unconditionally and it is usually a no-op. This one guards first,
// because the files it writes normally do survive and rewriting them means
// restarting a resolver the building is using.
//
// What it is for is the upgrade that moves one of those files. olr's own
// release notes are the only thing that knows a rendered path changed, and the
// .deb's postinstall cannot re-render — it has no way to ask what the config
// says, and `olr enable` (which would warn) is not run on an upgrade. So the
// box comes back from an upgrade with a unit pointing at a file that does not
// exist, keeps serving from the config the running daemon already read, and
// fails at the next boot. This closes that: the first olrd start after the
// upgrade writes the file at the new path and brings the unit with it.
//
// It is also the recovery path for a rendered file deleted by hand or lost to a
// partial restore, which has the same shape and the same symptom.
//
// And it is the recovery path for the same failure in the service half: intent
// says a backend answers this network, and the backend is not running. That
// case used to be excluded here, on the grounds that a unit systemd has not got
// to yet is not olrd's business — true at boot, and it meant nothing at all
// converged a backend that was never started. `systemctl enable` does not start
// a unit, so a box configured after its last boot sat with the resolver up,
// the relay dead, and every device unable to resolve a name, with the only
// repair an edit to a configuration that was already correct. The race that
// reasoning was protecting against is now answered where it belongs: a unit in
// `activating` plans as no work (internal/dns serviceAction), so systemd keeps
// the socket and olrd acts only on a unit nobody is bringing up.
func startDNS(ctx context.Context, a dns.Applier, logger *slog.Logger) {
	cfg, err := a.Load()
	if err != nil {
		logger.Error("dns configuration could not be read; nothing was rendered",
			"error", err)
		return
	}

	// A module that is switched off renders nothing and stops its units, and
	// startup is not the place to do that: the plan for a disabled `dns` is all
	// service actions, which is exactly what this must not act on.
	if !cfg.Enabled {
		return
	}

	plan, err := a.Drift(ctx)
	if err != nil {
		// Reading the box failed, which is not the same as the box being wrong.
		// Logged and dropped: `olr dns status` asks the same question with an
		// operator watching, and failing the start over it would take the API
		// down with it.
		logger.Error("dns drift could not be read; nothing was rendered", "error", err)
		return
	}

	// The two halves, gated separately and neither one standing for the other.
	// Still not !Empty: a unit whose only pending work is `enable` changes
	// nothing that is running, and acting on it here would restart a resolver
	// the building is using to record a boot-time preference.
	if !plan.RewritesFiles() && !plan.StartsABackend() {
		return
	}

	// The one thing startup will not do on its own. Every repair above is
	// either a file nobody is reading yet or a daemon that is not running, and
	// neither can cost anybody an answer they are currently getting; a
	// disruptive plan by definition can. design.md §5.3.3 says the operator
	// hears about those before they happen, and there is nobody to tell at
	// startup — so it is left for a surface with a human in front of it.
	if plan.Impact >= dns.ImpactDisruptive {
		logger.Warn("dns is not doing what its settings say, and putting it back would cut somebody off; left for an operator",
			"reasons", strings.Join(plan.Reasons, "; "))
		return
	}

	result, err := a.Apply(ctx, cfg)
	switch {
	case err != nil:
		// Named rather than folded into a generic line, because the operator
		// reading this journal has a box that is not resolving and the reason
		// is the one thing they need. verifyServing's message says what a
		// backend exiting immediately usually means.
		logger.Error("the resolver's configuration could not be restored", "error", err,
			"steps", len(result.Steps))
	default:
		// Said as what happened rather than as "applied": a line an operator
		// finds at 3am has to name the thing that was wrong, because olrd
		// acting on its own is exactly the kind of change nobody remembers
		// making.
		logger.Info("dns was not doing what its settings say and has been put back",
			"changes", len(result.Plan.Changes), "services", len(result.Plan.Services),
			"started", plan.StartsABackend())
	}
}

// startRemote re-creates the dial-in tunnel.
//
// The same shape as startFirewall, and the same reason: kernel state does not
// survive a reboot. What is different is the stake — this is the module whose
// absence an operator discovers from outside the building, so a failure here is
// logged with the interface named rather than folded into a generic line.
func startRemote(ctx context.Context, tunnel remote.TunnelApplier, logger *slog.Logger) {
	cfg, err := tunnel.Load()
	if err != nil {
		logger.Error("remote-access configuration could not be read; nothing was restored",
			"error", err)
		return
	}
	if cfg.Empty() {
		return
	}

	// The proxy needs no restoring — its unit is enabled at boot and reads a
	// file that survives a reboot, which is the whole difference between a
	// rendered backend and kernel state. Only the tunnel is put back here.
	result, err := tunnel.Apply(ctx, cfg)
	switch {
	case err != nil && result.Plan.Blocked != "":
		// The interface name belongs to something else. The one refusal this
		// module makes, and the one an operator can act on directly.
		logger.Error("the tunnel was not created", "reason", result.Plan.Blocked)
	case err != nil:
		logger.Error("the tunnel could not be created", "error", err,
			"interface", cfg.WireGuard.InterfaceOrDefault(), "steps", len(result.Steps))
	case !result.Plan.Empty():
		logger.Info("remote access applied",
			"interface", cfg.WireGuard.InterfaceOrDefault(), "peers", len(cfg.WireGuard.Peers))
	}
}

// startDial puts the uplink back and hands the stored records to the publisher.
//
// Two halves that fail differently, which is why they are not one call.
//
// The uplink is kernel state and the kernel forgets it: an address and a
// default route are gone after a reboot, and nothing else on the box knows what
// they were. That is the same argument that put link.Applier.Restore in this
// list, and it bites harder here — what the box comes back without is its way
// out, so the operator cannot even reach the API to fix it. Restore is additive
// and never fatal; dial.Applier.Restore has the rest.
//
// The records program nothing. Their effect lives at a third party rather than
// on the box, so what starting does is begin *checking*, and the first check of
// every record publishes — the address cache is in memory and a restart empties
// it (docs/ddns.md §6), which is also how a record somebody changed by hand at
// the provider gets repaired.
func startDial(ctx context.Context, a dial.Applier, publisher *dial.Publisher, delegation *dial.Delegation, logger *slog.Logger) {
	cfg, err := a.Load()
	if err != nil {
		logger.Error("dial configuration could not be read; "+
			"the uplink will not be restored and no name will be kept current", "error", err)
		return
	}
	if cfg.Empty() {
		// Silent, like startLink's empty case: a box with no uplink and no
		// records is the ordinary state of a fresh install, and a line on every
		// boot of every such box teaches an operator to skim exactly the lines
		// this block writes when something is wrong.
		return
	}

	restoreUplink(ctx, a, cfg, logger)

	// Asking for the prefix again after a restart is not losing it: the ISP
	// keys the lease on the uplink's MAC and the fixed IAID, so it answers with
	// the one it gave, and the networks keep their addresses meanwhile.
	if cfg.Uplink.HasPD() {
		delegation.Watch(ctx, cfg)
		logger.Info("asking the ISP for a delegated prefix", "interface", cfg.Uplink.Interface)
	}

	if len(cfg.Records) > 0 {
		publisher.Watch(ctx, cfg)
		logger.Info("keeping names current", "records", len(cfg.Records))
	}
}

// restoreUplink is the kernel half of startDial.
func restoreUplink(ctx context.Context, a dial.Applier, cfg dial.Config, logger *slog.Logger) {
	if cfg.Uplink == nil {
		return
	}
	steps, err := a.Restore(ctx)
	if err != nil {
		logger.Error("the uplink could not be restored", "error", err,
			"interface", cfg.Uplink.Interface, "steps", len(steps))
		return
	}

	var done int
	for _, s := range steps {
		if s.Done {
			done++
		}
		if s.Error != "" {
			// Per step rather than per call, matching startLink: a restore that
			// half-lands is reported as what landed and what did not, the same
			// way the writer reports it to an operator.
			logger.Error("uplink step failed", "step", s.Description, "error", s.Error)
		}
	}
	if done > 0 {
		// Only when something was actually programmed. An idempotent restore
		// against a box that never rebooted is the normal case and says
		// nothing.
		logger.Info("uplink restored", "interface", cfg.Uplink.Interface, "changes", done)
	}
}

// startHost takes the interfaces olr addresses from the distribution's own
// network configuration, once, after the addresses and the uplink are back.
//
// After them, for the order dial.Applier.Apply gives: the distribution's DHCP
// client is told to stop only once olr's address and route are in place. And
// at every start rather than only on a change, so a box upgraded into this
// release — or one whose dhcpcd.conf a package upgrade rewrote — converges
// without anybody saving anything. Idempotent: on a box that already agrees it
// writes nothing and signals nobody.
func startHost(ctx context.Context, srv *core.Server, take func(context.Context) ([]core.Step, error), logger *slog.Logger) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	var steps []core.Step
	err := srv.ApplyLock().Do(ctx, func() error {
		var err error
		steps, err = take(ctx)
		return err
	})
	for _, s := range steps {
		if s.Error != "" {
			logger.Error("could not take over from the distribution", "step", s.Description, "error", s.Error)
		} else {
			logger.Info("took over from the distribution", "step", s.Description)
		}
	}
	if err != nil && len(steps) == 0 {
		logger.Error("could not take over from the distribution", "error", err)
	}
}

// reapplyGateway re-programs the kernel after an exit changed health.
func reapplyGateway(a gateway.Applier, srv *core.Server, logger *slog.Logger, exit string, up bool) {
	// Bounded, because it runs under the global apply lock and design.md §3.6
	// requires apply to be bounded rather than to wait for convergence.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err := srv.ApplyLock().Do(ctx, func() error {
		cfg, err := a.Load()
		if err != nil {
			return err
		}
		_, _, err = a.Apply(ctx, cfg, netip.Addr{})
		return err
	})
	if err != nil {
		logger.Error("could not re-route after an exit changed state",
			"exit", exit, "up", up, "error", err)
		return
	}

	// Announced so the UI re-reads. The device that just lost its internet is
	// on somebody's screen, and "no internet — Proxy is down" is only useful if
	// it appears without a refresh.
	srv.Events().Publish(core.Event{Type: core.EventApplied, Module: gateway.ModuleName})
}

func newLogger(level string) (*slog.Logger, error) {
	var l slog.Level
	if err := l.UnmarshalText([]byte(level)); err != nil {
		return nil, fmt.Errorf("invalid --log-level %q (want debug, info, warn or error)", level)
	}
	// Text to stderr: systemd captures it into the journal, which is where
	// §3.4 says logs belong. No log file of our own.
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: l})), nil
}

func startSocksOut(ctx context.Context, a socksout.Applier, logger *slog.Logger) {
	cfg, err := a.Load()
	if err != nil {
		logger.Error("SOCKS5 outbound configuration could not be read", "error", err)
		return
	}
	if _, err := a.Apply(ctx, cfg, false); err != nil {
		logger.Error("SOCKS5 outbound could not be restored", "error", err)
	}
}

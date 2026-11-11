package main

import (
	"github.com/spf13/cobra"

	"github.com/open-linux-router/open-linux-router/internal/cli"
	"github.com/open-linux-router/open-linux-router/internal/devices"
	"github.com/open-linux-router/open-linux-router/internal/dhcp"
	"github.com/open-linux-router/open-linux-router/internal/dial"
	"github.com/open-linux-router/open-linux-router/internal/dns"
	"github.com/open-linux-router/open-linux-router/internal/firewall"
	"github.com/open-linux-router/open-linux-router/internal/gateway"
	"github.com/open-linux-router/open-linux-router/internal/ingress"
	"github.com/open-linux-router/open-linux-router/internal/iptv"
	"github.com/open-linux-router/open-linux-router/internal/link"
	"github.com/open-linux-router/open-linux-router/internal/remote"
	"github.com/open-linux-router/open-linux-router/internal/socksout"
	"github.com/open-linux-router/open-linux-router/internal/system"
)

// newRoot builds the command tree an operator actually gets.
//
// This exists as a function, rather than inline in main, for one reason: the
// conformance tests in this package have to walk the *whole* tree. They used to
// walk cli.NewRoot(), which mounts the operations stubs, `daemon` and
// `version` — and not one module. So "every command has a Short", "every
// top-level command is grouped" and the rest had never once looked at dhcp,
// dns or routing, and passed for that reason while the module surfaces drifted
// apart in every direction docs/cli.md now catalogues.
//
// main and the tests call the same function so the two cannot diverge again.
func newRoot() *cobra.Command {
	root := cli.NewRoot()

	// Modules are mounted explicitly. The list is bounded, so it is a literal
	// list rather than a registry (design.md §3.2).
	root.AddCommand(
		system.Command(),
		link.Command(),
		devices.Command(),
		dial.Command(),
		dhcp.Command(),
		dns.Command(),
		gateway.Command(),
		iptv.Command(),
		socksout.Command(),
		remote.Command(),
		ingress.Command(),
		firewall.Command(),
	)

	// `adopt`, `release` and `net` are hub-level in design.md §6.1's list, but
	// they write the link module's document, so they are built there and mounted
	// here. internal/cli stays free of module imports, which is what keeps `olr
	// --help` a description of the tree rather than of the modules.
	//
	// `net` in particular is top-level rather than under `olr link` because §4.4
	// makes the network an object of its own rather than a property of an
	// interface, and §11.2 spells the acceptance criterion `olr net add iot`.
	root.AddCommand(link.OperationCommands()...)
	root.AddCommand(link.NetCommand())
	root.AddCommand(system.OperationCommands()...)
	return root
}

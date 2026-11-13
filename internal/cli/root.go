// Package cli assembles the olr command tree.
//
// The tree is static. Every command is registered in Go, so `olr --help` works
// with olrd stopped and there is nothing to fetch, cache, or invalidate.
package cli

import (
	"github.com/spf13/cobra"
)

// Command groups.
//
// The split matters: most of olr is a client of olrd's HTTP API, on equal
// footing with the WebUI and the MCP server (design.md §1). But a few commands
// manage olrd itself and so cannot go through it — they must work when the
// daemon is down. Those live in GroupService.
//
// GroupService is where design.md §6.1's lower tier became visible after the
// `olr daemon` group was flattened away. The tier is real and unchanged; what
// went is the word in front of it. An operator has one program installed and
// thinks of it as one program, so `olr start` is what they reach for — and the
// heading, not the command path, is what says these do not go through the API.
const (
	GroupModules    = "modules"
	GroupOperations = "operations"
	GroupService    = "service"
	GroupOther      = "other"
)

// DefaultSocket is olrd's control socket.
const DefaultSocket = "/run/olr/olrd.sock"

type globalOptions struct {
	socket string
	output string
	dryRun bool
}

// NewRoot returns the root command with the hub-level commands attached.
// Modules are mounted by the caller, not here — see cmd/olr/main.go.
func NewRoot() *cobra.Command {
	opts := &globalOptions{}

	root := &cobra.Command{
		Use:   "olr",
		Short: "Control an open-linux-router box",
		Long: "olr controls a Linux box running open-linux-router.\n\n" +
			"Most commands talk to the olr service over its control socket. The ones\n" +
			"under Service manage that service itself, and work without it running.",

		// Runtime failures should not dump usage; a bad invocation still does.
		SilenceUsage:      true,
		SilenceErrors:     true,
		PersistentPostRun: reportPendingRecovery,
	}

	root.PersistentFlags().StringVar(&opts.socket, FlagSocket, DefaultSocket,
		"olrd control socket")
	root.PersistentFlags().StringVarP(&opts.output, FlagOutput, "o", OutputText,
		"output format: text|json")

	// Changes apply on return (design.md §5.1), so the only way to look before
	// leaping is to ask. Global rather than per-module: "show me what this
	// would do" should mean the same thing everywhere in the tool.
	root.PersistentFlags().BoolVar(&opts.dryRun, FlagDryRun, false,
		"show what would change and exit without applying it")

	root.AddGroup(
		&cobra.Group{ID: GroupModules, Title: "Modules:"},
		&cobra.Group{ID: GroupOperations, Title: "Operations:"},
		&cobra.Group{ID: GroupService, Title: "Service:"},
		&cobra.Group{ID: GroupOther, Title: "Other:"},
	)

	// Keep cobra's built-ins out of an untitled "Additional Commands" bucket.
	// Other, not Service: `help` and `completion` work with the service stopped,
	// but so does `version`, and none of the three manages it.
	root.SetHelpCommandGroupID(GroupOther)
	root.SetCompletionCommandGroupID(GroupOther)

	root.AddCommand(operationCommands()...)
	root.AddCommand(recoveryCommand())
	root.AddCommand(backupCommand(), restoreCommand())
	root.AddCommand(statusCommand())
	root.AddCommand(auditCommand())
	root.AddCommand(serviceCommands()...)
	root.AddCommand(versionCommand())

	return root
}

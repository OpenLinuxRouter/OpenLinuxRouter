package clients

import (
	"encoding/json"
	"net/url"

	"github.com/open-linux-router/open-linux-router/internal/cli"
	"github.com/spf13/cobra"
)

const endpoint = "/api/clients"

// Command uses the same HTTP contract as the UI and generated MCP reads.
//
// `show location` is separate from `show list` for the reason the route is: it
// asks every switch that can report a forwarding table, so it costs a round
// trip per device and must not be paid for a list that the operator only wanted
// to read.
func Command() *cobra.Command {
	show := cli.Verb("show", "Inspect the client inventory")
	show.RunE = nil

	list := &cobra.Command{
		Use:   "list",
		Short: "List every client, joining identity to observed presence",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			if err := cli.ValidateOutput(c); err != nil {
				return err
			}
			var result json.RawMessage
			if err := cli.ClientFor(c).Get(c.Context(), endpoint+"/list", &result); err != nil {
				return err
			}
			return cli.JSON(c.OutOrStdout(), result)
		},
	}

	location := &cobra.Command{
		Use:   "location <mac>",
		Short: "Ask the network equipment which port a client is attached to",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			if err := cli.ValidateOutput(c); err != nil {
				return err
			}
			var result json.RawMessage
			if err := cli.ClientFor(c).Get(c.Context(), endpoint+"/"+url.PathEscape(args[0])+"/location", &result); err != nil {
				return err
			}
			return cli.JSON(c.OutOrStdout(), result)
		},
	}

	show.AddCommand(list, location)
	return cli.NewModule("clients", "Name devices and see where they are attached", show)
}

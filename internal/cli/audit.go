package cli

import (
	"fmt"
	"strconv"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// auditCommand reads the record of what changed this box.
//
// A client of the API like every other read (design.md §6.1). The record is
// written by olrd, and reading the file directly would be a second door onto
// the same data with its own permissions story — and the one that would keep
// working on a box where the socket is the only authenticated path.
func auditCommand() *cobra.Command {
	var limit int

	cmd := &cobra.Command{
		Use:   "audit",
		Short: "Show the record of changes made to this box",
		Long: "Shows every mutating request olrd accepted, refused or failed, newest first.\n\n" +
			"The record names the module, the route, the client it was attributed to, and the\n" +
			"answer the box gave. It does not keep request bodies: the ingress configuration\n" +
			"route carries a DNS credential, and an audit that stored it would be a copy of\n" +
			"the secret written to disk. The path is enough to say what was changed.\n\n" +
			"Reads are not recorded. The record lives on the box under\n" +
			"/var/lib/open-linux-router/audit and is bounded by size; it is a record of what\n" +
			"happened, not a guarantee that nothing was removed.",
		GroupID: GroupOperations,
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := ReadOnly(cmd); err != nil {
				return err
			}
			if limit < 0 {
				return fmt.Errorf("--limit must not be negative")
			}
			var resp struct {
				Entries []core.AuditEntry `json:"entries"`
				AsOf    time.Time         `json:"as_of"`
			}
			path := core.APIPrefix + "/audit?limit=" + strconv.Itoa(limit)
			if err := NewClient(Socket(cmd)).Get(cmd.Context(), path, &resp); err != nil {
				return err
			}
			if IsJSON(cmd) {
				return JSON(cmd.OutOrStdout(), resp)
			}
			if len(resp.Entries) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No changes have been recorded.")
				return nil
			}

			tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintln(tw, "WHEN\tMODULE\tMETHOD\tPATH\tRESULT\tFROM")
			for _, e := range resp.Entries {
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n",
					e.At.Local().Format("2006-01-02 15:04:05"),
					e.Module, e.Method, e.Path,
					auditResult(e), auditFrom(e))
			}
			return tw.Flush()
		},
	}

	cmd.Flags().IntVar(&limit, "limit", 50, "how many entries to show, newest first")
	return cmd
}

// auditResult renders the outcome and the status together: the word is what a
// reader scans for, and the code is what they quote when they ask why.
func auditResult(e core.AuditEntry) string {
	out := fmt.Sprintf("%s %d", e.Outcome, e.Status)
	if e.DryRun {
		out += " (dry run)"
	}
	return out
}

// auditFrom names the client, and says when the address was taken from a
// forwarded header rather than from the connection itself.
func auditFrom(e core.AuditEntry) string {
	if e.Source == "" {
		return "-"
	}
	if e.ForwardedFor != "" {
		return e.Source + " (via proxy)"
	}
	return e.Source
}

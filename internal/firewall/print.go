package firewall

import (
	"fmt"
	"io"
	"strings"
)

func writeConfigText(w io.Writer, c Config) {
	if len(c.Ruleset) > 0 {
		fmt.Fprintln(w, "Native nftables rules are stored for inet "+TableName+".")
	}
	if c.Enabled {
		fmt.Fprintln(w, "The firewall is on.")
		return
	}
	fmt.Fprintln(w, "The firewall is off.")
}

func writeStatusText(w io.Writer, st Status) {
	if st.Native {
		fmt.Fprintf(w, "Native nftables rules: enabled=%t known=%t drifted=%t\n", st.Enabled, st.Known, st.Drifted)
		if st.Problem != "" {
			fmt.Fprintln(w, st.Problem)
		}
		fmt.Fprintln(w, "Review table inet "+TableName+" for its chains and policies.")
		return
	}
	switch {
	case !st.Enabled:
		fmt.Fprintln(w, "The firewall is off: anything that can reach this router may connect to it.")
	case !st.Known:
		fmt.Fprintln(w, "The firewall is on, but the kernel's rules could not be read.")
	case st.Drifted:
		fmt.Fprintln(w, "The firewall is on, but the kernel does not hold its rules. Run `olr firewall enable` to repair.")
	default:
		fmt.Fprintln(w, "The firewall is on.")
	}
	if st.Problem != "" {
		fmt.Fprintf(w, "  %s\n", st.Problem)
	}

	fmt.Fprintf(w, "\ninside:  %s\n", orNone(strings.Join(st.Inside, ", ")))
	fmt.Fprintf(w, "path MTU: %s\n", st.PathMTU)

	fmt.Fprintln(w, "\nopen to the outside:")
	fmt.Fprintln(w, "  port forwards (each forward is its own permission)")
	for _, o := range st.Openings {
		fmt.Fprintf(w, "  %s  %s\n", o.Describe(), o.For)
	}

	if st.Enabled && st.Known {
		fmt.Fprintf(w, "\nblocked since the rules were last written: %d to this router, %d through it\n",
			st.BlockedInput, st.BlockedForward)
	}

	for _, p := range st.Problems {
		fmt.Fprintf(w, "\nproblem: %s\n", p.Message)
	}
}

func writePlanText(w io.Writer, p Plan, dryRun bool) {
	for _, warn := range p.Warnings {
		fmt.Fprintf(w, "warning: %s\n", warn.Message)
	}
	if p.Empty {
		if dryRun {
			fmt.Fprintln(w, "No change.")
		}
		return
	}
	fmt.Fprintf(w, "impact: %s\n", p.Impact)
	fmt.Fprint(w, p.Diff())
}

func orNone(s string) string {
	if s == "" {
		return "none"
	}
	return s
}

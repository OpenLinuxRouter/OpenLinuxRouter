package link

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/open-linux-router/open-linux-router/internal/cli"
	"github.com/open-linux-router/open-linux-router/internal/core"
)

// Text output for humans; -o json is the machine-readable surface. Both come
// from the same structs, so the two never disagree about what exists.

func table(w io.Writer) *tabwriter.Writer {
	return tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
}

func writeConfigText(w io.Writer, c Config) error {
	if c.Empty() {
		return cli.NoObjects(w, "adopted interfaces",
			"Hand one to olr with `olr adopt <interface>`, and see what this machine has\n"+
				"with `olr link show interfaces`.")
	}
	if len(c.Adopted) > 0 {
		fmt.Fprintf(w, "%s adopted:\n", core.Plural(len(c.Adopted), "interface"))
		for _, name := range c.Adopted {
			line := "  " + name
			if n, ok := c.NetworkFor(name); ok {
				line += "  (" + n.Name + ")"
			}
			fmt.Fprintln(w, line)
		}
	}
	if len(c.Networks) > 0 {
		fmt.Fprintf(w, "\n%s:\n", core.Plural(len(c.Networks), "network"))
		t := table(w)
		for _, n := range c.Networks {
			subnet := "no ipv4"
			if n.IPv4 != nil && n.IPv4.Subnet.IsValid() {
				subnet = fmt.Sprintf("%s, this router at %s", n.IPv4.Subnet, n.IPv4.RouterAddr())
			}
			switch {
			case n.IPv6 != nil && n.IPv6.Delegated != nil:
				subnet += fmt.Sprintf("; ipv6 /64 number %d of the delegated prefix", *n.IPv6.Delegated)
			case n.IPv6 != nil && n.IPv6.Subnet.IsValid():
				subnet += fmt.Sprintf("; %s, this router at %s", n.IPv6.Subnet, n.IPv6.RouterAddr())
			}
			fmt.Fprintf(t, "  %s\ton %s\t%s\n", n.Name,
				dashIfEmpty(strings.Join(n.Members, ",")), subnet)
		}
		if err := t.Flush(); err != nil {
			return err
		}
	}
	return nil
}

func writeInterfacesText(w io.Writer, resp listResponse) error {
	if len(resp.Interfaces) == 0 {
		return cli.NoneObserved(w, "interfaces", "")
	}

	t := table(w)
	fmt.Fprintln(t, "INTERFACE\tOLR\tNETWORK\tMTU\tSTATE\tADDRESSES")
	for _, iface := range resp.Interfaces {
		fmt.Fprintf(t, "%s\t%s\t%s\t%s\t%s\t%s\n",
			iface.Name, adoptedText(iface), dashIfEmpty(iface.Network),
			mtuText(iface), stateText(iface), addressesText(iface))
	}
	if err := t.Flush(); err != nil {
		return err
	}

	return writeProblems(w, resp.Problems)
}

// writeNetworksText is `olr net list`.
//
// The router's address gets its own column rather than being folded into the
// subnet, because those are two things an operator reads separately: "the
// network is 172.16.1.0/24" and "this box is .1 on it". The range column is the
// derived one — what `dhcp` would hand out if nobody typed a range — and is
// marked as such so nobody reads it as configuration that already exists.
func writeNetworksText(w io.Writer, resp listResponse) error {
	if len(resp.Networks) == 0 {
		return cli.NoObjects(w, "networks",
			"Create one with `olr net add lan`. Only the name is needed — the subnet,\n"+
				"this router's address on it, and the interface are all derived.")
	}

	t := table(w)
	fmt.Fprintln(t, "NETWORK\tON\tSUBNET\tTHIS ROUTER\tDERIVED RANGE")
	for _, n := range resp.Networks {
		members := dashIfEmpty(strings.Join(n.Members, ","))
		if !n.Present {
			members += " (absent)"
		}
		rng := "-"
		if n.SuggestedStart != "" {
			rng = n.SuggestedStart + "-" + n.SuggestedEnd
		}
		fmt.Fprintf(t, "%s\t%s\t%s\t%s\t%s\n",
			n.Name, members, dashIfEmpty(n.Subnet), dashIfEmpty(n.Router), rng)
	}
	if err := t.Flush(); err != nil {
		return err
	}

	return writeProblems(w, resp.Problems)
}

// writeStepsText reports what actually reached the kernel.
//
// Printed even when everything succeeded. §5.2 gives this module no rollback,
// so "what did it manage to do" is a question an operator can genuinely be left
// holding — and a list that only appears on failure is a list nobody trusts on
// success.
func writeStepsText(w io.Writer, steps []Step) error {
	if len(steps) == 0 {
		return nil
	}
	fmt.Fprintf(w, "\non the interfaces:\n")
	for _, s := range steps {
		switch {
		case s.Error != "":
			fmt.Fprintf(w, "  failed   %s: %s\n", s.Description, s.Error)
		case s.Done:
			fmt.Fprintf(w, "  done     %s\n", s.Description)
		default:
			fmt.Fprintf(w, "  skipped  %s\n", s.Description)
		}
	}
	return nil
}

func dashIfEmpty(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// adoptedText is the column an operator scans for. "yes"/"-" rather than a
// tick, because this is also what `grep` sees.
func adoptedText(iface interfaceView) string {
	if iface.Adopted {
		return "yes"
	}
	return "-"
}

// stateText collapses three booleans into the one sentence fragment that
// distinguishes them, because "up" alone cannot say "up with no cable in it" —
// which is the single most common reason a freshly configured DHCP server
// appears to do nothing.
func stateText(iface interfaceView) string {
	switch {
	case !iface.Present:
		return "absent"
	case iface.Loopback:
		return "loopback"
	case !iface.Up:
		return "down"
	case !iface.Running:
		return "up, no carrier"
	default:
		return "up"
	}
}

// mtuText is the interface's own MTU, not the path's. Seeing 1500 here beside a
// path MTU of 1492 is the comparison the clamp exists for.
func mtuText(iface interfaceView) string {
	if iface.MTU == 0 {
		return "-"
	}
	return fmt.Sprint(iface.MTU)
}

func addressesText(iface interfaceView) string {
	if len(iface.Prefixes) == 0 {
		return "-"
	}
	return strings.Join(iface.Prefixes, " ")
}

func writePlanText(w io.Writer, plan planView, dryRun bool) error {
	if plan.Empty {
		fmt.Fprintln(w, cli.NothingToDo)
		return writeWarnings(w, plan.Warnings)
	}

	verb := map[bool]string{true: "would change", false: "changed"}[dryRun]
	fmt.Fprintf(w, "%s %s:\n", core.Plural(len(plan.Changes), "change"), verb)
	for _, c := range plan.Changes {
		fmt.Fprintf(w, "  %-9s %s\n", changeVerb(c.Path, c.Kind), nameOf(c.Path))
		// Only the kernel lines get their diff printed. An adoption's diff is
		// "+ eth0", which the line above already said, but "ip addr del
		// 192.168.1.91/24 dev ens18" is the whole content of the change and the
		// one thing an operator needs to read before agreeing to it.
		if strings.HasPrefix(c.Path, "interfaces[") {
			for _, line := range strings.Split(strings.TrimRight(c.Diff, "\n"), "\n") {
				fmt.Fprintf(w, "            %s\n", line)
			}
		}
	}

	// The impact line is printed only when there is one. Adoption still moves
	// nothing on the box, and printing "impact: none" under a change that
	// genuinely has none would train an operator to skip the line on the
	// changes where it matters — which here is every address change.
	if plan.Impact != impactNone {
		fmt.Fprintf(w, "\nimpact: %s\n", impactText(plan.Impact))
	}
	return writeWarnings(w, plan.Warnings)
}

// impactText spells the impact in client terms rather than daemon terms
// (design.md §11.3 does the same for dhcp).
func impactText(impact string) string {
	switch impact {
	case impactDisruptive:
		return "disruptive — devices on this network lose the address they hold"
	case impactRestart:
		return "restart — an interface gains addressing; nothing loses an address"
	default:
		return impact
	}
}

// changeVerb names what is happening to an entry.
//
// Adoption gets adopt/release rather than create/delete: those entries are
// permissions, and "delete eth0" reads like the interface went away. Networks
// and interfaces keep the plain verbs, because there a create really does
// create something and a delete really does take it away.
func changeVerb(path, kind string) string {
	switch {
	case strings.HasPrefix(path, "adopted["):
		if kind == kindDelete {
			return "release"
		}
		return "adopt"
	case strings.HasPrefix(path, "interfaces["):
		return "configure"
	default:
		return kind
	}
}

// nameOf unwraps "adopted[eth0]" back to "eth0" for display. The path shape is
// the API's, and it is right there — a UI attaches a message to a field with
// it. It is only in a terminal that it is noise.
func nameOf(path string) string {
	if i := strings.IndexByte(path, '['); i >= 0 {
		return strings.TrimSuffix(path[i+1:], "]")
	}
	return path
}

func writeWarnings(w io.Writer, warnings []core.Problem) error {
	return writeLabelled(w, "warning", warnings)
}

func writeProblems(w io.Writer, problems []core.Problem) error {
	return writeLabelled(w, "note", problems)
}

func writeLabelled(w io.Writer, noun string, in []core.Problem) error {
	if len(in) == 0 {
		return nil
	}
	fmt.Fprintf(w, "\n%s:\n", core.Plural(len(in), noun))
	for _, p := range in {
		fmt.Fprintf(w, "  %s\n", problemText(p))
	}
	return nil
}

func problemText(p core.Problem) string {
	if p.Path == "" {
		return p.Message
	}
	return p.Path + ": " + p.Message
}

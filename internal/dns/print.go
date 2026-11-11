package dns

import (
	"fmt"
	"io"
	"net/netip"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/cli"
	"github.com/open-linux-router/open-linux-router/internal/core"
)

// Text output for humans; -o json is the machine-readable surface. Both come
// from the same structs, so the two never disagree about what exists.

func table(w io.Writer) *tabwriter.Writer {
	return tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
}

func writeConfigText(w io.Writer, c Config) error {
	state := "disabled"
	if c.Enabled {
		state = "enabled"
	}
	fmt.Fprintf(w, "DNS is %s (olr-dnsd in front of unbound)\n", state)

	fmt.Fprintf(w, "\nlistening on:  %s\n", orEveryAddress(c.Listen))
	fmt.Fprintf(w, "resolving by:  %s\n", describeUpstream(c.Upstream))
	fmt.Fprintf(w, "redirect:      %s\n", describeHijack(c.Hijack))
	fmt.Fprintf(w, "query log:     %s\n", describeQueryLog(c.QueryLog))
	if c.FilterAAAA {
		fmt.Fprintln(w, "IPv6 answers: filtered (AAAA returns no address)")
	}
	fmt.Fprintf(w, "local names:   %s\n", describeHosts(c))
	if len(c.PrivateDomains) > 0 {
		// Named on the summary line because it is the one setting whose
		// absence looks like somebody else's bug: a record in public DNS that
		// this resolver refuses to answer.
		fmt.Fprintf(w, "private zones: %s\n", strings.Join(c.PrivateDomains, ", "))
	}

	fmt.Fprintln(w)
	if err := writePoliciesText(w, c.Policies); err != nil {
		return err
	}

	if c.ExtraConf != "" {
		fmt.Fprintf(w, "\nextra unbound configuration:\n")
		for _, line := range strings.Split(c.ExtraConf, "\n") {
			fmt.Fprintf(w, "  %s\n", line)
		}
	}
	return nil
}

func writePoliciesText(w io.Writer, policies []Policy) error {
	if len(policies) == 0 {
		return cli.NoObjects(w, "policies", "Every client may look up anything.")
	}
	t := table(w)
	fmt.Fprintln(t, "POLICY\tCLIENTS\tBLOCKED\tALLOWED\tANSWERS")
	for _, p := range policies {
		clients := joinPrefixes(p.Clients)
		if clients == "" {
			// Not "-": an operator scanning this column needs to see which row
			// is the catch-all, and a dash reads as "none" rather than "all".
			clients = "everyone else"
		}
		fmt.Fprintf(t, "%s\t%s\t%s\t%s\t%s\n",
			p.Name, clients,
			core.Plural(len(p.Block), "name"),
			core.Plural(len(p.Allow), "exception"),
			p.Response.OrDefault(),
		)
	}
	return t.Flush()
}

// describeHosts is the one-line summary on `olr dns show`. It names the domain
// even when there are no hosts, because that is the question an operator is
// actually asking the first time: what do I put after the name.
func describeHosts(c Config) string {
	if len(c.Hosts) == 0 {
		return "none, under " + c.LocalDomainOrDefault()
	}
	return fmt.Sprintf("%s under %s", core.Plural(len(c.Hosts), "name"), c.LocalDomainOrDefault())
}

func writeHostsText(w io.Writer, c Config) error {
	if len(c.Hosts) == 0 {
		return cli.NoObjects(w, "local names",
			"Clients here are reachable by address only. `olr dns add host <name> <address>`.")
	}
	t := table(w)
	fmt.Fprintln(t, "NAME\tANSWERS")
	for _, h := range c.Hosts {
		// The full name rather than the stored relative one: the operator is
		// about to type this into a browser, and the thing that works there is
		// the qualified form.
		addrs := make([]string, 0, len(h.Addrs))
		for _, a := range h.Addrs {
			addrs = append(addrs, a.String())
		}
		fmt.Fprintf(t, "%s\t%s\n", c.FQDN(h.Name), orDash(strings.Join(addrs, ", ")))
	}
	return t.Flush()
}

func writeNamesListText(w io.Writer, p Policy) error {
	fmt.Fprintf(w, "policy %s\n", p.Name)
	if len(p.Block) == 0 {
		fmt.Fprintln(w, "  blocks nothing")
	}
	for _, n := range p.Block {
		fmt.Fprintf(w, "  block  %s\n", n)
	}
	for _, n := range p.Allow {
		fmt.Fprintf(w, "  allow  %s\n", n)
	}
	return nil
}

func writeStatusText(w io.Writer, status statusResponse) error {
	state := "disabled"
	if status.Enabled {
		state = "enabled"
	}
	fmt.Fprintf(w, "DNS:            %s\n", state)

	// Both backends, separately. "DNS is broken" has two very different causes
	// — the resolver behind died, or the thing owning :53 did — and only one of
	// them is fixed by looking at unbound.
	for _, s := range status.Services {
		switch {
		case s.Status == nil:
			fmt.Fprintf(w, "%-15s unknown (%s)\n", short(s.Unit)+":", s.Error)
		default:
			fmt.Fprintf(w, "%-15s %s\n", short(s.Unit)+":", describeUnit(*s.Status))
		}
	}

	// Reported on its own line, because a unit that is running now and not
	// enabled is the failure that costs nothing until the next reboot and then
	// costs the whole network its name resolution at once.
	for _, s := range status.Services {
		if s.Status == nil || s.Status.Enabled {
			continue
		}
		if !s.Status.Installed {
			fmt.Fprintf(w, "starts at boot: no — %s is not installed; reinstall the olr package\n", s.Unit)
			continue
		}
		fmt.Fprintf(w, "starts at boot: NO for %s — DNS will not come back after a reboot\n", s.Unit)
	}

	// What the relay answers on, and why. Stated even when it matches what was
	// asked for, because the failure this line exists for is an address that is
	// answered on by nobody: a dial-in client is told to use one address and
	// only two surfaces can say whether it is served (dialin.go).
	if len(status.Served) > 0 {
		fmt.Fprintf(w, "answering on:   %s\n", describeServed(status.Served))
	}

	switch {
	case status.DriftError != "":
		// Honest about not knowing, rather than reporting "no drift" because we
		// could not look.
		fmt.Fprintf(w, "drift:          unknown (%s)\n", status.DriftError)
	case status.Drifted:
		fmt.Fprintln(w, "drift:          yes — run `olr dns show --dry-run` to see it")
	default:
		fmt.Fprintln(w, "drift:          none")
	}

	// Last of the state lines and separated by a blank, because it is the only
	// one whose fix is a command rather than an olr setting — and because an
	// operator who has one of these has nothing to gain from the query counters
	// below until it is dealt with.
	core.WriteBlockersText(w, status.Blockers)
	// After the whole list rather than under each panel: one `olr dns fix`
	// clears all of them, and saying so three times would read as three
	// different things to run.
	core.WriteFixHint(w, ModuleName, status.Blockers)

	if status.StatsError != "" {
		fmt.Fprintf(w, "queries:        unknown (%s)\n", status.StatsError)
		return nil
	}
	if status.Stats == nil {
		return nil
	}
	return writeStatsText(w, *status.Stats)
}

func writeStatsText(w io.Writer, s statsView) error {
	fmt.Fprintf(w, "\nsince %s\n", s.Since.Local().Format(time.RFC1123))
	t := table(w)
	fmt.Fprintf(t, "queries\t%d\n", s.Queries)
	fmt.Fprintf(t, "blocked\t%d\n", s.Blocked)
	fmt.Fprintf(t, "failed\t%d\t(upstream did not answer)\n", s.Failed)
	if err := t.Flush(); err != nil {
		return err
	}

	// Printed even at zero. These are the two ways the numbers above can be
	// wrong, and a counter that only appears when it is non-zero teaches an
	// operator that it does not exist.
	fmt.Fprintf(w, "\nlog holds %d of %d entries\n", s.Held, s.Capacity)
	if s.Dropped > 0 || s.Unparsed > 0 {
		fmt.Fprintf(w, "not accounted for: %d observation(s) dropped under load, "+
			"%d response(s) could not be parsed.\n", s.Dropped, s.Unparsed)
		fmt.Fprintln(w, "Neither cost anybody an answer — both were relayed untouched — "+
			"but both are gaps in the log above.")
	}
	return nil
}

func writeQueriesText(w io.Writer, resp queriesResponse) error {
	if len(resp.Queries) == 0 {
		return cli.NoneObserved(w, "queries", "")
	}
	t := table(w)
	fmt.Fprintln(t, "TIME\tCLIENT\tNAME\tTYPE\tRESULT\tANSWERS")
	for _, q := range resp.Queries {
		result := q.Rcode
		if q.Blocked {
			result = "BLOCKED"
			if q.Policy != "" {
				result += " (" + q.Policy + ")"
			}
		}
		fmt.Fprintf(t, "%s\t%s\t%s\t%s\t%s\t%s\n",
			q.At.Local().Format("15:04:05"), q.Client, q.Name, q.Type, result,
			orDash(strings.Join(q.Answers, ", ")))
	}
	return t.Flush()
}

func writeNamesText(w io.Writer, resp namesResponse) error {
	if len(resp.Names) == 0 {
		return cli.NoneObserved(w, "addresses", "")
	}
	t := table(w)
	fmt.Fprintln(t, "CLIENT\tADDRESS\tNAME\tVIA\tSEEN")
	for _, n := range resp.Names {
		via := "-"
		if len(n.Chain) > 0 {
			// The tail of the chain is the organisation signal: it is why a
			// client that asked for one name shows up talking to a CDN.
			via = n.Chain[len(n.Chain)-1]
		}
		fmt.Fprintf(t, "%s\t%s\t%s\t%s\t%s\n",
			n.Client, n.Address, n.Name, via, n.LastSeen.Local().Format("01-02 15:04"))
	}
	return t.Flush()
}

// writePlanText reports a change. dryRun switches between the future and the
// past tense, because "would restart" and "restarted" are answers to different
// questions and conflating them is how an operator ends up unsure whether
// anything happened.
func writePlanText(w io.Writer, plan planView, dryRun bool) error {
	// First, above the file list: this is the part the operator did not ask
	// for. `olr dns enable` on a fresh box now chooses an address, and a
	// command that quietly decided something has to lead with it rather than
	// bury it under a diff.
	for _, line := range plan.Derived {
		if dryRun {
			fmt.Fprintf(w, "would be %s\n", line)
			continue
		}
		fmt.Fprintln(w, line)
	}

	if plan.Empty {
		fmt.Fprintln(w, cli.NothingToDo)
		return writeWarnings(w, plan.Warnings)
	}

	verb := map[bool]string{true: "would change", false: "changed"}[dryRun]
	fmt.Fprintf(w, "%s %s:\n", core.Plural(len(plan.Changes), "file"), verb)
	for _, c := range plan.Changes {
		fmt.Fprintf(w, "  %-6s %s\n", c.Kind, c.Path)
	}

	for _, s := range plan.Services {
		if s.Action != ActionNone {
			fmt.Fprintf(w, "\n%-15s %s\n", short(s.Unit)+":", describeAction(s.Action, dryRun))
		}
		if s.Enable != nil {
			fmt.Fprintf(w, "%-15s %s\n", "boot:", describeEnable(*s.Enable, dryRun))
		}
	}

	fmt.Fprintf(w, "impact:         %s\n", plan.Impact)
	for _, reason := range plan.Reasons {
		fmt.Fprintf(w, "                %s\n", reason)
	}

	if dryRun {
		fmt.Fprintln(w)
		for _, c := range plan.Changes {
			fmt.Fprintln(w, c.Diff)
		}
	}

	return writeWarnings(w, plan.Warnings)
}

func writeWarnings(w io.Writer, warnings []core.Problem) error {
	if len(warnings) == 0 {
		return nil
	}
	fmt.Fprintf(w, "\n%s:\n", core.Plural(len(warnings), "warning"))
	for _, p := range warnings {
		fmt.Fprintf(w, "  %s\n", problemText(p))
	}
	return nil
}

// writeStepsText reports which steps landed before a failure. There is no
// rollback (design.md §5.3.2), so this is the operator's starting point for
// finishing the job.
func writeStepsText(w io.Writer, steps []Step) {
	if len(steps) == 0 {
		return
	}
	fmt.Fprintln(w, "steps attempted:")
	for _, s := range steps {
		mark := "ok  "
		if !s.Done {
			mark = "FAIL"
		}
		fmt.Fprintf(w, "  [%s] %s\n", mark, s.Description)
		if s.Error != "" {
			fmt.Fprintf(w, "         %s\n", s.Error)
		}
	}
	fmt.Fprintln(w, "nothing was rolled back; fix the cause and re-run to finish.")
}

func problemText(p core.Problem) string {
	if p.Path == "" {
		return p.Message
	}
	return p.Path + ": " + p.Message
}

// describeAction words a service action for the tense the caller is in.
var actionPastTense = map[ServiceAction]string{
	ActionStart:   "started",
	ActionStop:    "stopped",
	ActionReload:  "reloaded",
	ActionRestart: "restarted",
}

func describeAction(action ServiceAction, dryRun bool) string {
	if dryRun {
		return "would " + string(action)
	}
	if past, ok := actionPastTense[action]; ok {
		return past
	}
	return string(action)
}

// describeEnable words the boot-time change, in the same two tenses.
func describeEnable(enable, dryRun bool) string {
	switch {
	case enable && dryRun:
		return "would be set to start at boot"
	case enable:
		return "set to start at boot"
	case dryRun:
		return "would no longer start at boot"
	default:
		return "no longer starts at boot"
	}
}

func describeUnit(s ServiceStatus) string {
	switch {
	case !s.Installed:
		return "not installed"
	case s.Active:
		return "running"
	case s.State != "":
		return "not running (" + s.State + ")"
	default:
		return "not running"
	}
}

func describeUpstream(u Upstream) string {
	if u.Mode.OrDefault() == ModeRecurse {
		return "recursing from the root"
	}
	if len(u.Servers) == 0 {
		return "forwarding, but no servers are configured"
	}
	out := "forwarding to " + joinAddrPorts(u.Servers)
	switch {
	case u.TLS && u.TLSName != "":
		out += " over TLS, as " + u.TLSName
	case u.TLS:
		out += " over TLS (unauthenticated: no certificate name is set)"
	}
	return out
}

func describeHijack(h Hijack) string {
	if !h.Enabled {
		return "off — a client configured with another resolver is not redirected"
	}
	out := "plaintext DNS on " + strings.Join(h.Interfaces, ", ")
	if h.BlockDoT {
		return out + "; DNS-over-TLS blocked"
	}
	return out + "; DNS-over-TLS NOT blocked"
}

func describeQueryLog(q QueryLog) string {
	if !q.Enabled {
		return "off"
	}
	return fmt.Sprintf("last %d answered queries, in memory", q.EntriesOrDefault())
}

// short trims the ".service" suffix so a status column is readable.
// describeServed spells the served set the way an operator would say it.
func describeServed(in []servedView) string {
	parts := make([]string, 0, len(in))
	for _, s := range in {
		if s.Source == servedWildcard {
			parts = append(parts, fmt.Sprintf("every address this router holds, port %d", DNSPort))
			continue
		}
		parts = append(parts, fmt.Sprintf("%s (%s)", s.Addr, s.Source))
	}
	return strings.Join(parts, ", ")
}

func short(unit string) string { return strings.TrimSuffix(unit, ".service") }

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// orEveryAddress spells out the default rather than printing a dash.
//
// A dash would read as "unset, and therefore broken" on a box that is working
// perfectly — empty is what almost every configuration should have here, and it
// means the relay answers wherever this router can be reached.
func orEveryAddress(in []netip.AddrPort) string {
	if len(in) == 0 {
		return fmt.Sprintf("every address this router holds, port %d", DNSPort)
	}
	return joinAddrPorts(in)
}

func joinAddrPorts(in []netip.AddrPort) string {
	parts := make([]string, len(in))
	for i, a := range in {
		parts[i] = a.String()
	}
	return strings.Join(parts, ", ")
}

func joinPrefixes(in []netip.Prefix) string {
	parts := make([]string, len(in))
	for i, p := range in {
		parts[i] = p.String()
	}
	return strings.Join(parts, ", ")
}

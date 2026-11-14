package ingress

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
	state := "disabled"
	if c.Enabled {
		state = "enabled"
	}
	fmt.Fprintf(w, "Ingress is %s\n", state)
	fmt.Fprintf(w, "  domain    %s\n", orDash(c.Domain))
	fmt.Fprintf(w, "  public    %s\n", publicDomainState(c))

	fmt.Fprintln(w)
	fmt.Fprintf(w, "certificate:\n")
	fmt.Fprintf(w, "  provider  %s\n", orDash(c.Certificate.Provider))
	// The stored value is `********` here, because this came from the API and
	// the API redacts. Printing "set" rather than the mask says the same thing
	// without inviting anyone to think eight asterisks is the credential.
	fmt.Fprintf(w, "  token     %s\n", tokenState(c.Certificate.Token))
	fmt.Fprintf(w, "  contact   %s\n", orDash(c.Certificate.Email))
	fmt.Fprintf(w, "  resolvers %s\n", strings.Join(c.Certificate.ResolversOrDefault(), " "))

	fmt.Fprintln(w)
	if err := writeServicesText(w, servicesOf(c), c.Domain); err != nil {
		return err
	}

	if c.ExtraConf != "" {
		fmt.Fprintf(w, "\nextra Caddyfile configuration:\n")
		for _, line := range strings.Split(c.ExtraConf, "\n") {
			fmt.Fprintf(w, "  %s\n", line)
		}
	}
	return nil
}

// publicDomainState says whether the box has a public suffix, and why it might
// be set while nothing is exposed.
func publicDomainState(c Config) string {
	if c.PublicDomain == "" {
		return "not set — every published name is reachable only on this network"
	}
	if !c.HasExposed() {
		return c.PublicDomain + " (set, but no service is exposed)"
	}
	return c.PublicDomain
}

func tokenState(token string) string {
	switch token {
	case "":
		return "not set"
	default:
		return "set"
	}
}

// servicesOf renders config services without a client view, for the places that
// have the config but not the daemon's resolution of it.
func servicesOf(c Config) []serviceView {
	out := make([]serviceView, 0, len(c.Services))
	for _, s := range c.Services {
		v := serviceView{
			Name:           s.Name,
			Exposure:       s.Expose.OrDefault(),
			AllowFrom:      s.AllowFrom,
			Client:         s.Upstream.Client,
			Host:           s.Upstream.Host,
			Port:           s.Upstream.Port,
			Scheme:         s.Upstream.Scheme.OrDefault(),
			RequestHeaders: s.Upstream.RequestHeaders,
		}
		v.URL = "https://" + qualify(s.Name, c.Domain)
		if v.Exposure.IsPublic() && c.PublicDomain != "" {
			v.PublicURL = "https://" + qualify(s.Name, c.PublicDomain)
		}
		out = append(out, v)
	}
	return out
}

func writeServicesText(w io.Writer, services []serviceView, domain string) error {
	if len(services) == 0 {
		return cli.NoObjects(w, "published services",
			"Publish one with `olr ingress add <name> --client <client> --port <port>`.")
	}

	t := table(w)
	fmt.Fprintln(t, "NAME\tURL\tPUBLIC\tTARGET\tGOES TO")
	for _, s := range services {
		fmt.Fprintf(t, "%s\t%s\t%s\t%s\t%s\n",
			s.Name, orDash(s.URL), publicCell(s), target(s), goesTo(s))
	}
	return t.Flush()
}

// publicCell is the second URL, or the reason there is not one. A dash would be
// ambiguous between "not exposed" and "the public URL is missing because the
// domain is unset"; both are worth telling apart in a list somebody scans.
func publicCell(s serviceView) string {
	if s.PublicURL == "" {
		return "— this network only"
	}
	if s.Exposure == ExposureRestricted {
		return s.PublicURL + " (restricted: " + strings.Join(s.AllowFrom, ", ") + ")"
	}
	return s.PublicURL
}

// target is what the operator typed; goesTo is where it currently resolves.
// Two columns rather than one, because "the NUC" and "192.168.1.50:3000" answer
// different questions and the second is the one that is wrong when a published
// name returns 502.
func target(s serviceView) string {
	name := s.Host
	if s.Client != "" {
		name = s.Client
	}
	if s.Scheme == SchemeHTTPS {
		return fmt.Sprintf("%s:%d (https)", name, s.Port)
	}
	return fmt.Sprintf("%s:%d", name, s.Port)
}

func goesTo(s serviceView) string {
	if s.UpstreamError != "" {
		return "— " + s.UpstreamError
	}
	return orDash(s.Upstream)
}

func writeServiceText(w io.Writer, s serviceView) error {
	t := table(w)
	fmt.Fprintf(t, "name\t%s\n", s.Name)
	fmt.Fprintf(t, "url\t%s\n", orDash(s.URL))
	fmt.Fprintf(t, "public url\t%s\n", orDash(s.PublicURL))
	fmt.Fprintf(t, "exposure\t%s\n", s.Exposure.OrDefault())
	if s.Exposure == ExposureRestricted {
		fmt.Fprintf(t, "sources\t%s\n", strings.Join(s.AllowFrom, ", "))
	}
	if s.Client != "" {
		fmt.Fprintf(t, "client\t%s\n", s.Client)
	} else {
		fmt.Fprintf(t, "host\t%s\n", s.Host)
	}
	fmt.Fprintf(t, "port\t%d\n", s.Port)
	fmt.Fprintf(t, "scheme\t%s\n", s.Scheme)
	for _, h := range s.RequestHeaders {
		if h.Remove {
			fmt.Fprintf(t, "remove header\t%s\n", h.Name)
		} else {
			fmt.Fprintf(t, "header\t%s: %s\n", h.Name, h.Value)
		}
	}
	fmt.Fprintf(t, "goes to\t%s\n", goesTo(s))
	return t.Flush()
}

func writeStatusText(w io.Writer, s statusResponse) error {
	state := "disabled"
	if s.Enabled {
		state = "enabled"
	}
	fmt.Fprintf(w, "Ingress is %s", state)
	if s.Domain != "" {
		fmt.Fprintf(w, ", publishing under *.%s", s.Domain)
	}
	if s.PublicDomain != "" {
		fmt.Fprintf(w, "; %s exposed at *.%s", core.Plural(s.Public, "service"), s.PublicDomain)
	}
	fmt.Fprintln(w)
	fmt.Fprintf(w, "%s published\n", core.Plural(s.Published, "service"))

	fmt.Fprintln(w)
	switch {
	case s.BinaryError != "":
		// First, because nothing below it can be true without a proxy, and the
		// message is the one that tells an operator how to get one.
		fmt.Fprintf(w, "proxy:       not installed\n\n%s\n\n", s.BinaryError)
	case s.Binary != "":
		fmt.Fprintf(w, "binary:      %s\n", s.Binary)
	}
	switch {
	case s.ServiceError != "":
		fmt.Fprintf(w, "proxy:       unknown (%s)\n", s.ServiceError)
	case s.Service == nil:
		fmt.Fprintf(w, "proxy:       unknown\n")
	default:
		fmt.Fprintf(w, "proxy:       %s\n", unitLine(*s.Service))
	}

	writeCertText(w, s)

	switch {
	case s.DriftError != "":
		fmt.Fprintf(w, "config:      unknown (%s)\n", s.DriftError)
	case s.Drifted:
		fmt.Fprintf(w, "config:      drifted — the box no longer matches what olr stored\n")
	default:
		fmt.Fprintf(w, "config:      matches\n")
	}

	// Warnings last and unindented, because they are the part that needs
	// acting on and the rest is context for them.
	for _, warning := range s.CertWarnings {
		fmt.Fprintf(w, "\n! %s\n", warning)
	}
	for _, warning := range s.PublicCertWarnings {
		fmt.Fprintf(w, "\n! public: %s\n", warning)
	}

	if s.Drifted && s.Drift != nil {
		fmt.Fprintln(w)
		return writePlanText(w, *s.Drift, true)
	}
	return nil
}

// writeCertText is the clock-dependent line (docs/ingress.md §8).
//
// It prints even when everything is fine, which is the point: the failure this
// guards against is one where nothing looks wrong, so a line that only appears
// when it is already too late would be a line nobody has learned to read.
func writeCertText(w io.Writer, s statusResponse) {
	switch {
	case s.CertError != "":
		fmt.Fprintf(w, "certificate: unknown (%s)\n", s.CertError)
	case !s.Certificate.Found:
		fmt.Fprintf(w, "certificate: none yet\n")
	case s.Certificate.ExpiresIn <= 0:
		fmt.Fprintf(w, "certificate: EXPIRED\n")
	default:
		note := ""
		if s.Certificate.RenewalOverdue {
			note = "  (renewal overdue)"
		}
		fmt.Fprintf(w, "certificate: valid for %s%s\n",
			core.Plural(s.Certificate.ExpiresInDays, "day"), note)
	}

	// The public certificate only exists when something is exposed, and it is
	// printed as its own line for the reason the field is its own field: it is
	// a different zone and a different failure.
	if s.PublicCertificate == nil {
		return
	}
	switch {
	case !s.PublicCertificate.Found:
		fmt.Fprintf(w, "public cert: none yet — the provider token may not cover the public zone\n")
	case s.PublicCertificate.ExpiresIn <= 0:
		fmt.Fprintf(w, "public cert: EXPIRED\n")
	default:
		note := ""
		if s.PublicCertificate.RenewalOverdue {
			note = "  (renewal overdue)"
		}
		fmt.Fprintf(w, "public cert: valid for %s%s\n",
			core.Plural(s.PublicCertificate.ExpiresInDays, "day"), note)
	}
}

func unitLine(s ProxyStatus) string {
	switch {
	case !s.Installed:
		return "not installed"
	case s.Active && s.Enabled:
		return "running, starts at boot"
	case s.Active:
		return "running, but will not start at boot"
	case s.Enabled:
		return "stopped, but set to start at boot"
	default:
		return "stopped"
	}
}

func writePlanText(w io.Writer, plan planView, dryRun bool) error {
	if plan.Empty {
		fmt.Fprintln(w, cli.NothingToDo)
		return writeWarnings(w, plan.Warnings)
	}

	verb := map[bool]string{true: "would change", false: "changed"}[dryRun]
	fmt.Fprintf(w, "%s %s:\n", core.Plural(len(plan.Changes), "file"), verb)
	for _, c := range plan.Changes {
		fmt.Fprintf(w, "  %-6s %s\n", c.Kind, c.Path)
	}

	if plan.Action != ActionNone {
		fmt.Fprintf(w, "\nservice: %s\n", describeAction(plan.Action, dryRun))
	}
	if plan.Enable != nil {
		fmt.Fprintf(w, "boot:    %s\n", describeEnable(*plan.Enable, dryRun))
	}
	fmt.Fprintf(w, "impact:  %s\n", plan.Impact)
	for _, reason := range plan.Reasons {
		fmt.Fprintf(w, "         %s\n", reason)
	}

	if dryRun {
		fmt.Fprintln(w)
		for _, c := range plan.Changes {
			fmt.Fprintln(w, c.Diff)
		}
	}

	return writeWarnings(w, plan.Warnings)
}

func describeAction(a ServiceAction, dryRun bool) string {
	would := map[bool]string{true: "would be ", false: ""}[dryRun]
	switch a {
	case ActionStart:
		return would + "started"
	case ActionStop:
		return would + "stopped"
	case ActionReload:
		// Said explicitly because it is the reassuring half of the ladder and
		// an operator deciding whether to press enter deserves to know.
		return would + "reloaded, without dropping connections"
	case ActionRestart:
		return would + "restarted, dropping connections in flight"
	}
	return string(a)
}

func describeEnable(enable, dryRun bool) string {
	would := map[bool]string{true: "would ", false: ""}[dryRun]
	if enable {
		return would + "start at boot"
	}
	return would + "not start at boot"
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
		switch {
		case s.Skipped:
			mark = "skip"
		case !s.Done:
			mark = "FAIL"
		}
		fmt.Fprintf(w, "  %s %s\n", mark, s.Description)
		if s.Error != "" {
			fmt.Fprintf(w, "       %s\n", s.Error)
		}
	}
}

func writeProvidersText(w io.Writer, providers []string) error {
	t := table(w)
	// Three to a row: the list is long enough that one per line is a screenful
	// and short enough that a table is overkill.
	for i, p := range providers {
		fmt.Fprintf(t, "%s", p)
		if i%3 == 2 || i == len(providers)-1 {
			fmt.Fprintln(t)
		} else {
			fmt.Fprint(t, "\t")
		}
	}
	if err := t.Flush(); err != nil {
		return err
	}
	fmt.Fprintf(w, "\n%s.\n", core.Plural(len(providers), "provider"))
	return nil
}

func problemText(p core.Problem) string {
	if p.Path == "" {
		return p.Message
	}
	return p.Path + ": " + p.Message
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

package ingress

import (
	"bytes"
	"fmt"
	"slices"
	"sort"
	"strings"
)

// Planning is pure and reads *observed* state rather than a cached copy of what
// we last wrote, which is what makes drift free (design.md §5.4): "have we
// drifted?" is "plan unchanged intent against the system and see if the diff is
// empty".

// Impact says what applying a change will cost, so a UI can warn instead of
// spinning (design.md §5.3.3).
//
// The ladder means something different here than it does for DHCP, because the
// thing being interrupted is different. A DHCP client retries; a browser in the
// middle of an upload does not.
type Impact int

const (
	// ImpactNone changes nothing the proxy reads.
	ImpactNone Impact = iota

	// ImpactReload is picked up by `caddy reload`. Listeners are kept and
	// in-flight requests finish, so nobody notices. This is the ordinary case
	// and covers every edit to the Caddyfile — adding a service, changing an
	// upstream, publishing a new name.
	ImpactReload

	// ImpactRestart bounces the process, which **drops every connection through
	// the proxy**.
	//
	// It is reached by two things. The first is worth stating because the file
	// that causes it looks as reloadable as any other: the provider
	// credential lives in an environment file, systemd applies EnvironmentFile=
	// when it *starts* a process, and `caddy reload` re-reads the Caddyfile
	// without re-reading the environment. So a new token that is only reloaded
	// is a new token that is not in use — the proxy keeps running with the old
	// one and nothing reports a problem until the next renewal fails, weeks
	// later. Restarting is the honest way to make the credential take effect.
	//
	// The second is a change to the admin endpoint's address, which is the
	// channel a reload travels over — see BuildPlan.
	ImpactRestart

	// ImpactDisruptive takes a published name away from whoever is using it.
	//
	// Removing a service, or disabling the module, means a URL somebody has
	// bookmarked stops answering. That is not recoverable by retrying, which is
	// what separates it from a restart.
	ImpactDisruptive
)

func (i Impact) String() string {
	switch i {
	case ImpactNone:
		return "none"
	case ImpactReload:
		return "reload"
	case ImpactRestart:
		return "restart"
	case ImpactDisruptive:
		return "disruptive"
	}
	return fmt.Sprintf("Impact(%d)", int(i))
}

// MarshalText makes Impact a JSON string.
func (i Impact) MarshalText() ([]byte, error) { return []byte(i.String()), nil }

// UnmarshalText parses that string back. The pair has to exist together, or a
// plan can be sent over the API and never decoded by a Go client of it.
func (i *Impact) UnmarshalText(text []byte) error {
	switch string(text) {
	case "none":
		*i = ImpactNone
	case "reload":
		*i = ImpactReload
	case "restart":
		*i = ImpactRestart
	case "disruptive":
		*i = ImpactDisruptive
	default:
		return fmt.Errorf("unknown impact %q (want none, reload, restart or disruptive)", text)
	}
	return nil
}

// ChangeKind is what happens to a file.
type ChangeKind string

// The file operations a plan can contain.
const (
	ChangeCreate ChangeKind = "create"
	ChangeUpdate ChangeKind = "update"
	ChangeDelete ChangeKind = "delete"
)

// Change is one file's worth of pending work.
type Change struct {
	Path   string     `json:"path"`
	Kind   ChangeKind `json:"kind"`
	Impact Impact     `json:"impact"`

	// Secret suppresses the contents everywhere this change is displayed. The
	// file is still planned, written and compared byte for byte; only its
	// display is withheld. See diff.go.
	Secret bool `json:"secret,omitempty"`

	// Before and After are the file contents, omitted from JSON because a
	// rendered config is long.
	Before []byte `json:"-"`
	After  []byte `json:"-"`
}

// ServiceAction is what the daemon needs after the files are written.
type ServiceAction string

// The service transitions a plan can ask for.
const (
	ActionNone    ServiceAction = "none"
	ActionStart   ServiceAction = "start"
	ActionStop    ServiceAction = "stop"
	ActionReload  ServiceAction = "reload"
	ActionRestart ServiceAction = "restart"
)

// Observed is the actual state of the system, read fresh.
type Observed struct {
	// Files is the current content of every file under the module's rendered
	// directory. A path absent from the map does not exist on disk.
	Files map[string][]byte

	// Running reports whether the proxy's unit is active.
	Running bool

	// EnabledAtBoot reports whether the unit would start after a reboot. Only
	// meaningful when ServiceKnown.
	EnabledAtBoot bool

	// Installed reports whether the unit file exists at all. Only meaningful
	// when ServiceKnown.
	Installed bool

	// ServiceKnown reports whether the service manager answered. "We could not
	// tell" and "it is off" are different answers and only one of them is
	// honest (design.md §3.4).
	ServiceKnown bool
}

// Served returns the names the proxy is answering for right now, read out of
// the Caddyfile on disk.
//
// This is the ingress analogue of reading the lease database: the question
// "would applying this take something away from somebody" has to be asked
// against what is actually being served, not against what we last stored. A
// config file that was hand-edited, or left behind by an older olr, is exactly
// the case where stored intent would give the wrong answer.
func (o Observed) Served(confPath string) []string {
	return servedNames(o.Files[confPath])
}

// adminDirective returns the global `admin` line of a rendered Caddyfile, or ""
// when there is none. Like servedNames it reads only our own output.
func adminDirective(data []byte) string {
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) > 0 && fields[0] == "admin" {
			return strings.Join(fields, " ")
		}
	}
	return ""
}

// servedNames extracts the published hostnames from a rendered Caddyfile.
//
// It parses our own output and only our own output — the `@svc_…` matchers the
// renderer emits, in either shape it emits them: a one-line `@svc_x host <fqdn>`
// or the block that also carries the source guard. That narrowness is the
// point: this is not a Caddyfile parser and must never grow into one. Anything
// it fails to recognise simply is not reported as served, which costs a
// disruption warning and never produces a wrong one.
//
// Only the `@svc_` matchers are read. The exposed names (`@pub_…`) are a second
// block in a second zone, and losing one of those is not what the disruption
// warning is about — a private name that stops answering is someone's open tab.
func servedNames(data []byte) []string {
	var out []string
	inMatcher := false
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) == 0 {
			continue
		}
		switch {
		case len(fields) == 2 && fields[1] == "{" && strings.HasPrefix(fields[0], "@svc_"):
			inMatcher = true
		case fields[0] == "}":
			inMatcher = false
		case inMatcher && len(fields) >= 2 && fields[0] == "host":
			out = append(out, fields[1])
			inMatcher = false
		case len(fields) == 3 && strings.HasPrefix(fields[0], "@svc_") && fields[1] == "host":
			out = append(out, fields[2])
		}
	}
	sort.Strings(out)
	return out
}

// Plan is the full answer to "what would applying this config do?".
type Plan struct {
	Backend string        `json:"backend"`
	Changes []Change      `json:"changes"`
	Action  ServiceAction `json:"action"`
	Impact  Impact        `json:"impact"`

	// Enable, when non-nil, is the boot-time state the unit has to be moved to.
	// Separate from Action because nothing a client holds changes when a unit is
	// enabled, so it never raises the impact — but it is still work, and still
	// drift when it disagrees with intent.
	Enable *bool `json:"enable,omitempty"`

	// Reasons explains the impact in the operator's terms — above all, which
	// published names a disruptive change would take away.
	Reasons []string `json:"reasons,omitempty"`

	// Validation carries warnings even on success.
	Validation Result `json:"-"`
}

// Empty reports whether applying would change nothing that matters — the drift
// check (design.md §5.4).
//
// "Nothing that matters" rather than "nothing at all": a release that reworded
// an explanation in the renderer produces a file that differs byte for byte and
// not at all in meaning, and calling that drift would make `olr status` cry wolf
// on every upgrade.
func (p Plan) Empty() bool {
	return p.Action == ActionNone && p.Enable == nil && !p.significant()
}

// significant reports whether any change would alter what the proxy reads.
func (p Plan) significant() bool {
	for _, c := range p.Changes {
		if c.Impact > ImpactNone {
			return true
		}
	}
	return false
}

// nothingToDo reports whether there is no work at all, cosmetic included. This
// is Apply's early exit, not the drift answer.
func (p Plan) nothingToDo() bool {
	return len(p.Changes) == 0 && p.Action == ActionNone && p.Enable == nil
}

// BuildPlan renders the desired config and diffs it against what is on disk and
// running.
//
// It validates first and returns the error rather than planning against a config
// that cannot be applied — the whole value of validation is that it happens
// before anything is written (design.md §5.3.1).
func BuildPlan(b Caddy, desired Config, dns DNSView, clients ClientView, obs Observed) (Plan, error) {
	result := Validate(desired, dns, clients)
	if err := result.Err(); err != nil {
		return Plan{Validation: result}, err
	}

	rendered, err := b.Render(desired, clients)
	if err != nil {
		return Plan{Validation: result}, err
	}

	plan := Plan{Backend: b.Name(), Validation: result}
	needsRestart := false

	for _, f := range rendered.Files {
		before, exists := obs.Files[f.Path]
		if exists && bytes.Equal(before, f.Data) {
			continue
		}
		kind := ChangeCreate
		if exists {
			kind = ChangeUpdate
		}

		var impact Impact
		switch {
		case exists && Canonical(before) == Canonical(f.Data):
			// Only comments moved. The file is still rewritten — stale
			// explanations in a generated file help nobody — but there is
			// nothing for the proxy to re-read, so it is neither a reason to
			// signal it nor a reason to call the box drifted. This module
			// renders more comment than configuration, so without this every
			// olr upgrade would bounce every proxy.
			impact = ImpactNone
		case f.Path == b.Paths.Env:
			// See ImpactRestart: a credential delivered by EnvironmentFile= is
			// only picked up by a process start.
			impact = ImpactRestart
			needsRestart = true
		case f.Path == b.Paths.Conf && adminDirective(before) != adminDirective(f.Data):
			// A reload is delivered over the admin endpoint, so a change to
			// where that endpoint is cannot be delivered by one: the running
			// process is not listening where the reload will knock. The case
			// that makes this real is an upgrade from an olr that rendered
			// `admin off` — without this the first edit after it fails with
			// "reload failed" and keeps failing until somebody restarts the
			// unit by hand.
			impact = ImpactRestart
			needsRestart = true
		default:
			impact = ImpactReload
		}

		plan.Changes = append(plan.Changes, Change{
			Path: f.Path, Kind: kind, Impact: impact,
			Secret: f.Secret, Before: before, After: f.Data,
		})
	}

	// Files on disk this config no longer produces. Caddy reads the file we
	// name rather than a directory, so a leftover cannot be served — but it is
	// still drift, and still something an operator should be told is there.
	wanted := rendered.Paths()
	for path := range obs.Files {
		if slices.Contains(wanted, path) {
			continue
		}
		plan.Changes = append(plan.Changes, Change{
			Path: path, Kind: ChangeDelete, Impact: ImpactReload, Before: obs.Files[path],
		})
	}

	sort.Slice(plan.Changes, func(i, j int) bool { return plan.Changes[i].Path < plan.Changes[j].Path })

	plan.Action = serviceAction(desired.Enabled, obs.Running, plan.significant(), needsRestart)
	if obs.ServiceKnown && desired.Enabled != obs.EnabledAtBoot {
		want := desired.Enabled
		plan.Enable = &want
	}

	plan.Impact, plan.Reasons = classify(b, desired, plan, obs)
	return plan, nil
}

// serviceAction decides what to do with the proxy after writing files.
func serviceAction(enabled, running, changed, needsRestart bool) ServiceAction {
	switch {
	case enabled && !running:
		return ActionStart
	case !enabled && running:
		return ActionStop
	case !enabled:
		// Already stopped. Files may still change so that enabling later is a
		// plain start, but nothing needs signalling.
		return ActionNone
	case !changed:
		return ActionNone
	case needsRestart:
		return ActionRestart
	default:
		return ActionReload
	}
}

// classify turns the file changes into one impact and an explanation.
//
// The explanation is the product here, not the number. "restart" tells an
// operator nothing they can weigh; "every connection through the proxy is
// dropped, and grafana.home.example.com stops answering" tells them whether to
// do it now or at midnight.
func classify(b Caddy, desired Config, plan Plan, obs Observed) (Impact, []string) {
	impact := ImpactNone
	for _, c := range plan.Changes {
		impact = max(impact, c.Impact)
	}

	var reasons []string

	// Names that are being served now and would not be afterwards. Compared
	// against the file on disk rather than against stored intent, so a
	// hand-edit is accounted for too.
	domain := normalizeDomain(desired.Domain)
	wanted := map[string]bool{}
	if desired.Enabled {
		for _, s := range desired.Services {
			wanted[qualify(s.Name, domain)] = true
		}
	}
	var losing []string
	for _, name := range obs.Served(b.Paths.Conf) {
		if !wanted[name] {
			losing = append(losing, name)
		}
	}
	if len(losing) > 0 {
		impact = max(impact, ImpactDisruptive)
		reasons = append(reasons, fmt.Sprintf(
			"%s stops answering: %s", pluralNames(len(losing)), strings.Join(losing, ", ")))
	}

	if plan.Action == ActionStop {
		impact = max(impact, ImpactDisruptive)
		reasons = append(reasons, "the proxy stops, so every published name stops answering")
	}

	if impact >= ImpactRestart && plan.Action == ActionRestart {
		why := "to move its admin socket, which is how later changes are reloaded"
		for _, c := range plan.Changes {
			if c.Path == b.Paths.Env && c.Impact == ImpactRestart {
				why = "to pick up the new credential"
			}
		}
		reasons = append(reasons,
			"the proxy restarts "+why+", which drops connections in flight")
	}

	return impact, reasons
}

func pluralNames(n int) string {
	if n == 1 {
		return "a published name"
	}
	return fmt.Sprintf("%d published names", n)
}

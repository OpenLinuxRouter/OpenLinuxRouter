package dhcp

import (
	"bytes"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// Planning is deliberately pure and reads *observed* state rather than a cached
// copy of what we last wrote. That is what makes drift free (design.md §5.4):
// "have we drifted?" is just "plan against unchanged intent and see if the diff
// is empty", so there is no separate health machinery to keep in step.

// Impact says what applying a change will cost, so a UI can warn instead of
// spinning (design.md §5.3.3).
type Impact int

const (
	// ImpactNone changes nothing that is running.
	ImpactNone Impact = iota
	// ImpactReload is picked up by a SIGHUP. Clients notice nothing.
	ImpactReload
	// ImpactRestart bounces the daemon. Leases survive in the database, so
	// clients keep their addresses; a request arriving during the restart is
	// retried by the client.
	ImpactRestart
	// ImpactDisruptive will take addresses away from clients that hold them.
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

// UnmarshalText parses that string back.
//
// The pair has to exist together. Impact is an int with a text encoding, so
// without this a plan can be sent but never read: any Go client of the API —
// `olr` itself, once it consumes /api/dhcp/plan rather than calling the module
// directly — fails to decode the response it was just handed.
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

	// Before and After are the file contents. Omitted from JSON by the CLI
	// unless asked for, since a rendered config is long.
	Before []byte `json:"-"`
	After  []byte `json:"-"`
}

// ServiceAction is what the daemon needs after the files are written.
type ServiceAction string

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
	// directories. A path absent from the map does not exist on disk. It must
	// include files we did not render, or a stale reservation left behind by a
	// previous config would never be noticed.
	Files map[string][]byte

	// Running reports whether the backend's unit is active.
	Running bool

	// Starting reports that systemd is already bringing the unit up.
	//
	// Not folded into Running, because they are different answers and only one
	// is a reason to act: a unit in `activating` is not serving yet, so it is
	// not running, and starting it again would be olr racing systemd for a
	// socket at every boot. Planning it as no work is what lets olrd converge
	// the service half without that race — internal/dns says the same.
	Starting bool

	// EnabledAtBoot reports whether the unit would start after a reboot.
	//
	// Observed rather than assumed, because it is drift in its own right: a
	// `systemctl disable` behind our back costs nothing until the power goes
	// out, and then costs every address on the network. Only meaningful when
	// ServiceKnown.
	EnabledAtBoot bool

	// Installed reports whether the unit file exists at all. Only meaningful
	// when ServiceKnown.
	Installed bool

	// ServiceKnown reports whether the service manager answered.
	//
	// The distinction matters for the same reason it does in verifyServing:
	// "we could not tell" and "it is off" are different answers, and treating
	// the first as the second would make every box without a system bus report
	// permanent drift.
	ServiceKnown bool

	// Leases is the current lease database, used to tell a restart from a
	// genuine disruption.
	Leases []Lease
}

// Plan is the full answer to "what would applying this config do?".
type Plan struct {
	Backend string        `json:"backend"`
	Changes []Change      `json:"changes"`
	Action  ServiceAction `json:"action"`
	Impact  Impact        `json:"impact"`

	// Enable, when non-nil, is the boot-time state the unit has to be moved to.
	//
	// Separate from Action because they are different questions and only one of
	// them is about right now. Nothing a client is holding changes when a unit
	// is enabled, so this never raises the impact — but it is still work, and
	// still drift when it disagrees with intent.
	Enable *bool `json:"enable,omitempty"`

	// Reasons explains the impact in the operator's terms — most importantly
	// which clients a disruptive change would drop.
	Reasons []string `json:"reasons,omitempty"`

	// Validation carries warnings even on success.
	Validation Result `json:"-"`
}

// Empty reports whether applying would change nothing that matters. This is the
// drift check (design.md §5.4).
//
// "Nothing that matters" rather than "nothing at all", and the difference is a
// comment: a new olr release that reworded an explanation in the renderer
// produces a file that differs byte for byte and not at all in meaning. Calling
// that drift would make `olr status` cry wolf on every upgrade, and a status
// line an operator has learned to ignore is worse than no status line — the
// hand-edit it exists to catch would go unnoticed with it.
func (p Plan) Empty() bool {
	return p.Action == ActionNone && p.Enable == nil && !p.significant()
}

// significant reports whether any change would alter what a daemon reads.
func (p Plan) significant() bool {
	for _, c := range p.Changes {
		if c.Impact > ImpactNone {
			return true
		}
	}
	return false
}

// RewritesFiles is Empty's file half on its own: would applying put something
// different in front of the daemon, ignoring whether the unit needs starting?
//
// The two halves are separated for the caller that must act on one without the
// other — olrd's background watch, which starts a server that should be running
// and deliberately does not rewrite a file somebody may be in the middle of
// editing. internal/dns splits them the same way and says why at more length.
func (p Plan) RewritesFiles() bool { return p.significant() }

// StartsABackend reports whether the plan would bring the daemon up from
// nothing. A unit systemd is already starting never appears here —
// serviceAction plans that as no work.
func (p Plan) StartsABackend() bool { return p.Action == ActionStart }

// nothingToDo reports whether there is no work at all, cosmetic included. This
// is Apply's early exit, not the drift answer: a cosmetic rewrite is still a
// file to write.
func (p Plan) nothingToDo() bool {
	return len(p.Changes) == 0 && p.Action == ActionNone && p.Enable == nil
}

// BuildPlan renders the desired config and diffs it against what is actually
// on disk and running.
//
// It validates first and returns the error rather than planning against a
// config that cannot be applied — the whole value of validation is that it
// happens before anything is written (design.md §5.3.1).
func BuildPlan(b Dnsmasq, desired Config, networks NetworkView, obs Observed, now time.Time) (Plan, error) {
	result := Validate(desired, networks)
	if err := result.Err(); err != nil {
		return Plan{Validation: result}, err
	}

	rendered, err := b.Render(desired, networks)
	if err != nil {
		return Plan{Validation: result}, err
	}

	plan := Plan{Backend: b.Name(), Validation: result}
	wantReloadOnly := true

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
		case exists && bytes.Equal(b.Canonical(before), b.Canonical(f.Data)):
			// Only comments moved. The file is still rewritten — leaving stale
			// explanations in a generated file helps nobody — but there is
			// nothing here for the daemon to re-read, so it is not a reason to
			// signal it and not a reason to call the box drifted.
			impact = ImpactNone
		case f.Reloadable:
			impact = ImpactReload
		default:
			impact = ImpactRestart
			wantReloadOnly = false
		}

		plan.Changes = append(plan.Changes, Change{
			Path: f.Path, Kind: kind, Impact: impact, Before: before, After: f.Data,
		})
	}

	// Files on disk that the current config no longer produces. Without this a
	// removed reservation would keep being served: dnsmasq reads whatever is in
	// the directory, not whatever we meant to put there.
	wanted := rendered.Paths()
	for path := range obs.Files {
		if slices.Contains(wanted, path) {
			continue
		}
		reloadable := b.reloadable(path)
		if !reloadable {
			wantReloadOnly = false
		}
		impact := ImpactRestart
		if reloadable {
			impact = ImpactReload
		}
		plan.Changes = append(plan.Changes, Change{
			Path: path, Kind: ChangeDelete, Impact: impact, Before: obs.Files[path],
		})
	}

	sort.Slice(plan.Changes, func(i, j int) bool { return plan.Changes[i].Path < plan.Changes[j].Path })

	plan.Action = serviceAction(desired.Enabled, obs.Running, obs.Starting, plan.significant(), wantReloadOnly)

	// Only when the service manager answered. Without that guard a box with no
	// system bus reads as "not enabled" and every plan against it would carry
	// an enable step that can never be satisfied.
	if obs.ServiceKnown && desired.Enabled != obs.EnabledAtBoot {
		want := desired.Enabled
		plan.Enable = &want
	}

	plan.Impact, plan.Reasons = classify(desired, networks, plan, obs, now)

	return plan, nil
}

// serviceAction decides what to do with the daemon after writing files.
func serviceAction(enabled, running, starting, changed, reloadOnly bool) ServiceAction {
	switch {
	case enabled && !running && starting:
		// systemd is already on it. Asking again is how olrd and systemd end
		// up racing for the socket at every boot.
		return ActionNone
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
	case reloadOnly:
		return ActionReload
	default:
		return ActionRestart
	}
}

// classify reduces the plan to a single impact plus the reasons behind it.
func classify(desired Config, networks NetworkView, plan Plan, obs Observed, now time.Time) (Impact, []string) {
	impact := ImpactNone
	for _, c := range plan.Changes {
		impact = max(impact, c.Impact)
	}
	switch plan.Action {
	case ActionRestart, ActionStart:
		impact = max(impact, ImpactRestart)
	case ActionReload:
		impact = max(impact, ImpactReload)
	}

	var reasons []string

	// The honest question is not "did a range field change" but "will a client
	// lose the address it is using". Answering it from the live lease database
	// is what makes `disruptive` a fact rather than a guess.
	if dropped := Dropped(desired, networks, obs.Leases, now); len(dropped) > 0 && obs.Running {
		impact = ImpactDisruptive
		reasons = append(reasons, describeDropped(desired, dropped))
	}

	if plan.Action == ActionStop {
		impact = ImpactDisruptive
		reasons = append(reasons, "the DHCP service will stop; no client will be able to renew")
	}

	// Said plainly because the consequence is invisible until it is expensive:
	// nothing looks wrong until the box reboots, and then every client on the
	// network fails to get an address at once.
	if plan.Enable != nil {
		if *plan.Enable {
			reasons = append(reasons,
				"the service is not set to start at boot, so DHCP would not come back after a reboot")
		} else {
			reasons = append(reasons, "the service will no longer start at boot")
		}
	}

	return impact, reasons
}

// Dropped returns the active leases that the desired config would stop serving.
//
// The question is deliberately "will this client keep the address it is holding
// right now", not "did a range field change". Answering it from the live lease
// database is what makes `disruptive` a fact rather than a guess (design.md
// §11.3), so the cases below are about the client's experience, not ours.
func Dropped(c Config, networks NetworkView, leases []Lease, now time.Time) []Lease {
	var dropped []Lease
	for _, l := range leases {
		if !l.Active(now) {
			continue
		}
		if !c.Enabled {
			// Nothing renews, so every live lease is lost regardless of family.
			dropped = append(dropped, l)
			continue
		}
		if !l.IP.Is4() {
			// IPv6 leases are governed by the ra field rather than by a range,
			// and dnsmasq's lease file does not record which interface a lease
			// was handed out on — so there is nothing here to compare them
			// against. Counting them as dropped would mark *every* apply on an
			// IPv6-enabled box disruptive, which would train operators to
			// ignore the word. Left for when the lease event stream (§11.5)
			// supplies the interface.
			continue
		}
		if servedBy(c, networks, l) {
			continue
		}
		dropped = append(dropped, l)
	}
	return dropped
}

// servedBy reports whether the client holding this lease keeps this address.
func servedBy(c Config, networks NetworkView, l Lease) bool {
	// A reservation matching the client's MAC is decisive and outranks every
	// range: dnsmasq will hand that client exactly the reserved address and
	// nothing else. So the lease survives only if the reservation names the
	// address the client already holds — pinning a client to a *different*
	// address moves it, which is a client losing the address it holds.
	if l.MAC != "" {
		for _, r := range c.Reservations {
			if r.MAC == l.MAC {
				return r.IP == l.IP
			}
		}
	}

	// The address is reserved for somebody else, so this holder loses it.
	for _, r := range c.Reservations {
		if r.IP == l.IP {
			return false
		}
	}

	for _, p := range c.Pools {
		info, err := networks.Network(p.Network)
		if err != nil {
			continue
		}
		// The *resolved* range, so a pool whose range is derived is compared
		// against the same addresses dnsmasq will actually be told to hand out.
		// Reading the stored fields directly would treat every derived pool as
		// covering nothing, and mark every apply disruptive.
		start, end, ok := p.Range(info)
		if ok && core.InRange(start, end, l.IP) {
			return true
		}
	}
	return false
}

func describeDropped(c Config, dropped []Lease) string {
	names := make([]string, 0, len(dropped))
	for _, l := range dropped {
		switch {
		case l.Hostname != "":
			names = append(names, fmt.Sprintf("%s (%s)", l.Hostname, l.IP))
		default:
			names = append(names, l.IP.String())
		}
	}
	sort.Strings(names)

	const show = 4
	listed := names
	suffix := ""
	if len(listed) > show {
		listed, suffix = listed[:show], fmt.Sprintf(" and %d more", len(names)-show)
	}

	verb := "will lose the address it holds"
	if len(dropped) > 1 {
		verb = "will lose the addresses they hold"
	}
	if !c.Enabled {
		return fmt.Sprintf("%d client(s) %s: %s%s", len(dropped), verb, strings.Join(listed, ", "), suffix)
	}
	return fmt.Sprintf("%d client(s) %s because no pool or reservation covers them any more: %s%s",
		len(dropped), verb, strings.Join(listed, ", "), suffix)
}

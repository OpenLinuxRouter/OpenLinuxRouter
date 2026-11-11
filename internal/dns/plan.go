package dns

import (
	"bytes"
	"fmt"
	"net/netip"
	"slices"
	"sort"
	"time"
)

// Planning is deliberately pure and reads *observed* state rather than a cached
// copy of what we last wrote. That is what makes drift free (design.md §5.4):
// "have we drifted?" is just "plan against unchanged intent and see if the diff
// is empty", so there is no separate health machinery to keep in step.

// Impact says what applying a change will cost, so a UI can warn instead of
// spinning (design.md §5.3.3). Same vocabulary as internal/dhcp's, because an
// operator should not have to learn a second one per module.
type Impact int

const (
	// ImpactNone changes nothing that is running.
	ImpactNone Impact = iota
	// ImpactReload is picked up by a SIGHUP. No query is interrupted.
	ImpactReload
	// ImpactRestart bounces a daemon. DNS is stateless and clients retry
	// unprompted, so this costs milliseconds and nobody notices — which is
	// exactly why docs/dns.md §5 concludes that owning :53 is survivable.
	ImpactRestart
	// ImpactDisruptive will stop somebody resolving. On this module that is not
	// one client losing a setting; it is a person finding the internet broken.
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
// The pair has to exist together: Impact is an int with a text encoding, so
// without this a plan can be sent but never read, and `olr dns` is a client of
// its own module's API.
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

	// Unit names the backend that reads this file, so the planner knows which
	// of the two daemons a change obliges it to signal. Writing unbound.conf
	// must not restart the relay, and vice versa.
	Unit string `json:"unit,omitempty"`

	// Before and After are the file contents, omitted from JSON because a
	// rendered config is long. The view layer surfaces the diff instead.
	Before []byte `json:"-"`
	After  []byte `json:"-"`
}

// ServiceAction is what a daemon needs after the files are written.
type ServiceAction string

const (
	ActionNone    ServiceAction = "none"
	ActionStart   ServiceAction = "start"
	ActionStop    ServiceAction = "stop"
	ActionReload  ServiceAction = "reload"
	ActionRestart ServiceAction = "restart"
)

// ServicePlan is one unit's worth of pending work.
//
// A list, where internal/dhcp has a single Action, because this module drives
// two daemons: unbound resolves and our relay owns :53. They fail differently
// and are signalled independently — editing a blocklist reloads the relay and
// must not touch the resolver at all.
type ServicePlan struct {
	Unit   string        `json:"unit"`
	Action ServiceAction `json:"action"`

	// Enable, when non-nil, is the boot-time state the unit has to be moved to.
	//
	// Separate from Action because they are different questions and only one is
	// about right now. Nothing in flight changes when a unit is enabled — but it
	// is still work, and still drift when it disagrees with intent.
	Enable *bool `json:"enable,omitempty"`
}

// UnitState is what the service manager says about one unit.
type UnitState struct {
	// Known reports whether the service manager answered at all. "We could not
	// tell" and "it is off" are different answers, and treating the first as
	// the second would make every box without a system bus report permanent
	// drift.
	Known bool

	// Running reports whether the unit is active.
	Running bool

	// Starting reports that systemd is already bringing the unit up.
	//
	// Not folded into Running, because they are different answers and only one
	// of them is a reason to act: a unit in `activating` is not serving yet, so
	// it is not running, and starting it again would be olr racing systemd for
	// a socket at every boot. Planning it as no work is what lets this module
	// converge the service half at startup without that race.
	Starting bool

	// EnabledAtBoot reports whether it would start after a reboot. Drift in its
	// own right: a `systemctl disable` behind our back costs nothing until the
	// power goes out, and then costs the whole network its name resolution.
	EnabledAtBoot bool

	// Installed reports whether the unit file exists at all.
	Installed bool
}

// Client is one address the relay has recently answered.
//
// Observed, never stored.
type Client struct {
	Addr     netip.Addr `json:"address"`
	Queries  int        `json:"queries"`
	LastSeen time.Time  `json:"last_seen"`
}

// Observed is the actual state of the system, read fresh.
type Observed struct {
	// Files is the current content of every file under the module's rendered
	// directory. A path absent from the map does not exist on disk. It must
	// include files we did not render, or a stale policy left behind by a
	// previous config would go on being enforced.
	Files map[string][]byte

	// Units is what systemd knows, keyed by unit name.
	Units map[string]UnitState
}

// Unit returns a unit's state, and whether anything is known about it.
func (o Observed) Unit(name string) UnitState { return o.Units[name] }

// Plan is the full answer to "what would applying this config do?".
type Plan struct {
	Backend  string        `json:"backend"`
	Changes  []Change      `json:"changes"`
	Services []ServicePlan `json:"services"`
	Impact   Impact        `json:"impact"`

	// Reasons explains the impact in the operator's terms — above all, who
	// stops being able to resolve.
	Reasons []string `json:"reasons,omitempty"`

	// Validation carries warnings even on success.
	Validation Result `json:"-"`
}

// Empty reports whether applying would change nothing that matters. This is the
// drift check (design.md §5.4).
//
// "Nothing that matters" rather than "nothing at all": a new olr release that
// reworded an explanation in the renderer produces a file that differs byte for
// byte and not at all in meaning. Calling that drift would make `olr status` cry
// wolf on every upgrade, and a status line an operator has learned to ignore is
// worse than none — the hand-edit it exists to catch would go unnoticed with it.
func (p Plan) Empty() bool { return len(p.Services) == 0 && !p.significant() }

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
// different in front of a daemon, ignoring whether any unit needs starting?
//
// It exists for the one caller that must tell those two halves apart. olrd
// re-renders this module at startup (internal/daemon's startDNS), and at that
// moment the service half is not its business: the backend units are enabled
// and systemd is bringing them up on its own schedule, so an olrd that beat
// unbound to the socket would see "not running", call it drift, and start a
// unit that was already starting. The file half carries no such race — a
// rendered file is either the one stored intent produces or it is not, and
// nothing but olr writes it.
func (p Plan) RewritesFiles() bool { return p.significant() }

// StartsABackend reports whether the plan would bring a daemon up from nothing.
//
// The service half of Empty, for the one caller that has to act on it alone:
// olrd converges this at startup (internal/daemon's startDNS), where "a file is
// wrong" and "the thing that answers the network is not running" are different
// enough to be gated separately. A unit systemd is already starting never
// appears here — serviceAction plans that as no work.
func (p Plan) StartsABackend() bool {
	for _, s := range p.Services {
		if s.Action == ActionStart {
			return true
		}
	}
	return false
}

// nothingToDo reports whether there is no work at all, cosmetic included. This
// is Apply's early exit, not the drift answer: a cosmetic rewrite is still a
// file to write.
func (p Plan) nothingToDo() bool { return len(p.Changes) == 0 && len(p.Services) == 0 }

// BuildPlan renders the desired config and diffs it against what is actually on
// disk and running.
//
// It validates first and returns the error rather than planning against a config
// that cannot be applied — the whole value of validation is that it happens
// before anything is written (design.md §5.3.1).
func BuildPlan(b Backend, desired Config, links LinkView, reservations ReservationView, obs Observed, now time.Time) (Plan, error) {
	result := Validate(desired, links, reservations)
	if err := result.Err(); err != nil {
		return Plan{Validation: result}, err
	}

	rendered, err := b.Render(desired, links)
	if err != nil {
		return Plan{Validation: result}, err
	}

	plan := Plan{Backend: b.Name(), Validation: result}

	// Per unit, because the two daemons are signalled independently: a policy
	// edit reloads the relay and must leave the resolver entirely alone.
	changed := map[string]bool{}
	reloadOnly := map[string]bool{}
	for _, unit := range b.Units() {
		reloadOnly[unit] = true
	}

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
			// nothing here for a daemon to re-read, so it is not a reason to
			// signal one and not a reason to call the box drifted.
			impact = ImpactNone
		case f.Reloadable:
			impact = ImpactReload
			changed[f.Unit] = true
		default:
			impact = ImpactRestart
			changed[f.Unit] = true
			reloadOnly[f.Unit] = false
		}

		plan.Changes = append(plan.Changes, Change{
			Path: f.Path, Kind: kind, Impact: impact, Unit: f.Unit,
			Before: before, After: f.Data,
		})
	}

	// Files on disk the current config no longer produces. Without this a
	// removed policy would go on being enforced: the relay reads whatever is in
	// the directory, not whatever we meant to put there.
	wanted := rendered.Paths()
	for path := range obs.Files {
		if slices.Contains(wanted, path) {
			continue
		}
		unit := b.unitFor(path)
		impact := ImpactRestart
		if b.reloadable(path) {
			impact = ImpactReload
		} else if unit != "" {
			reloadOnly[unit] = false
		}
		if unit != "" {
			changed[unit] = true
		}
		plan.Changes = append(plan.Changes, Change{
			Path: path, Kind: ChangeDelete, Impact: impact, Unit: unit,
			Before: obs.Files[path],
		})
	}

	sort.Slice(plan.Changes, func(i, j int) bool { return plan.Changes[i].Path < plan.Changes[j].Path })

	// Resolver before relay, which is the order they have to be brought up in:
	// a relay whose upstream is not answering serves SERVFAIL to the whole
	// house, so the thing behind comes up first.
	for _, unit := range b.Units() {
		state := obs.Unit(unit)
		sp := ServicePlan{
			Unit:   unit,
			Action: serviceAction(desired.Enabled, state.Running, state.Starting, changed[unit], reloadOnly[unit]),
		}
		// Only when the service manager answered. Without that guard a box with
		// no system bus reads as "not enabled" and every plan against it would
		// carry an enable step that can never be satisfied.
		if state.Known && desired.Enabled != state.EnabledAtBoot {
			want := desired.Enabled
			sp.Enable = &want
		}
		if sp.Action != ActionNone || sp.Enable != nil {
			plan.Services = append(plan.Services, sp)
		}
	}

	plan.Impact, plan.Reasons = classify(plan)

	return plan, nil
}

// serviceAction decides what to do with a daemon after writing files.
func serviceAction(enabled, running, starting, changed, reloadOnly bool) ServiceAction {
	switch {
	case enabled && !running && starting:
		// systemd is already on it. Nothing to ask for, and asking anyway is
		// how olrd and systemd end up racing for the socket at every boot.
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
func classify(plan Plan) (Impact, []string) {
	impact := ImpactNone
	for _, c := range plan.Changes {
		impact = max(impact, c.Impact)
	}

	var reasons []string
	stopping := false
	for _, s := range plan.Services {
		switch s.Action {
		case ActionRestart, ActionStart:
			impact = max(impact, ImpactRestart)
		case ActionReload:
			impact = max(impact, ImpactReload)
		case ActionStop:
			stopping = true
		}
	}

	if stopping {
		// The one place this module's impact vocabulary differs in weight from
		// dhcp's. A DHCP server going away costs nobody anything until their
		// lease expires; a resolver going away costs everybody everything
		// immediately, and does not present as "DNS is down" — it presents as
		// the internet being broken.
		impact = ImpactDisruptive
		reasons = append(reasons,
			"DNS will stop; every client on the network loses name resolution immediately, "+
				"because DHCP hands out this box as the only resolver")
	}

	for _, s := range plan.Services {
		if s.Enable == nil {
			continue
		}
		// Said plainly because the consequence is invisible until it is
		// expensive: nothing looks wrong until the box reboots, and then
		// nothing on the network can resolve a name.
		if *s.Enable {
			reasons = append(reasons, fmt.Sprintf(
				"%s is not set to start at boot, so DNS would not come back after a reboot", s.Unit))
		} else {
			reasons = append(reasons, fmt.Sprintf("%s will no longer start at boot", s.Unit))
		}
	}

	return impact, reasons
}

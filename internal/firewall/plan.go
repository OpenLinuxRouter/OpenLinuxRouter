package firewall

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// Planning compares what Render wants against what the kernel holds, read
// fresh, so drift is the plan against unchanged intent (design.md §5.4) and
// not separate machinery.

// Impact is what applying costs, in the vocabulary every module shares.
type Impact string

const (
	// ImpactNone changes nothing.
	ImpactNone Impact = "none"

	// ImpactReload replaces the table. Established connections survive it:
	// the first rule of both chains accepts them, and conntrack does not
	// forget a connection because a rule was rebuilt.
	ImpactReload Impact = "reload"

	// ImpactDisruptive is a change that will cut off the caller's own session
	// — the one case worth stopping to ask about (§5.3.3). See lockout.
	ImpactDisruptive Impact = "disruptive"
)

// Observed is the kernel's side, read fresh.
type Observed struct {
	// Known is false when the kernel could not be read at all — a non-Linux
	// build, a container without CAP_NET_ADMIN — which is a different answer
	// from "there is no table".
	Known bool

	// Lines is our table in the canonical form Desired.Lines produces.
	Lines []string

	// Blocked is each drop counter's packet count since the table was built.
	Blocked map[string]uint64

	// Traffic is each sourced rule's anonymous packet/byte counter.
	Traffic map[string]TrafficCount
}

// Change is one line that will be added or removed.
type Change struct {
	Kind string `json:"kind"`
	Line string `json:"line"`
}

// Plan is the answer to "what would applying this do?".
type Plan struct {
	Changes  []Change       `json:"changes"`
	Impact   Impact         `json:"impact"`
	Empty    bool           `json:"empty"`
	Warnings []core.Problem `json:"warnings,omitempty"`
}

// ErrNoInside is Validate's one refusal.
var ErrNoInside = errors.New("there are no networks yet, so everything is outside — " +
	"switching the firewall on now would block every connection to this router, " +
	"including this one. Create a network first")

// Validate checks that the config can be applied against the boundary as it is.
//
// One rule. With nothing inside, the ruleset accepts nothing but replies, and
// the operator's own connection to the UI is not a reply. That is not a
// setting anybody wants, so it is refused rather than warned about.
func Validate(c Config, inside []string) error {
	if c.Enabled && len(normalizeInside(inside)) == 0 {
		return ErrNoInside
	}
	return nil
}

// BuildPlan renders and diffs. via is the interface the caller reached this
// router on, empty when unknown or local (the `olr` CLI over the socket).
func BuildPlan(c Config, b Boundary, obs Observed, via string) (Plan, Desired, error) {
	inside, err := b.Inside()
	if err != nil {
		return Plan{}, Desired{}, fmt.Errorf("reading the networks: %w", err)
	}
	openings, err := b.Openings()
	if err != nil {
		return Plan{}, Desired{}, fmt.Errorf("reading what is served to the outside: %w", err)
	}
	if err := Validate(c, inside); err != nil {
		return Plan{}, Desired{}, err
	}

	var iptv IPTV
	if c.Enabled {
		iptv, err = b.IPTV()
		if err != nil {
			return Plan{}, Desired{}, fmt.Errorf("reading IPTV: %w", err)
		}
	}
	desired := RenderIPTV(c, inside, openings, iptv)
	plan := Plan{Impact: ImpactNone, Empty: true}

	if obs.Known {
		plan.Changes = diffLines(obs.Lines, desired.Lines())
		plan.Empty = len(plan.Changes) == 0
		if !plan.Empty {
			plan.Impact = ImpactReload
		}
	}

	if w, ok := lockout(desired, via); ok {
		plan.Warnings = append(plan.Warnings, w)
		// Only when something is about to change. Re-applying a table that is
		// already in force cannot cut off a session that is evidently getting
		// through it.
		if !plan.Empty || !obs.Known {
			plan.Impact = ImpactDisruptive
		}
	}
	return plan, desired, nil
}

// lockout reports whether the caller is reaching this router from outside,
// where the new ruleset will not let them back in.
//
// Their current connection survives — it is established, and that is the first
// rule — but the next one a browser opens is new, and it is dropped. So the
// page they applied from stops working a moment later, which reads as the box
// having died. design.md §5.5's guard, which would revert a change nobody
// confirms, is not built; this warning is what stands in for it.
func lockout(d Desired, via string) (core.Problem, bool) {
	if !d.Enabled || via == "" || via == "lo" || slices.Contains(d.Inside, via) {
		return core.Problem{}, false
	}
	return core.Problem{
		Path: "enabled",
		Message: fmt.Sprintf("you are connected to this router through %s, which is outside "+
			"every network — once the firewall is on, new connections from there are blocked, "+
			"and that includes this page. Continue from a client on one of your networks, or "+
			"over remote access.", via),
	}, true
}

// diffLines is a set difference in both directions, removals first.
func diffLines(have, want []string) []Change {
	var out []Change
	for _, l := range have {
		if !slices.Contains(want, l) {
			out = append(out, Change{Kind: "remove", Line: l})
		}
	}
	for _, l := range want {
		if !slices.Contains(have, l) {
			out = append(out, Change{Kind: "add", Line: l})
		}
	}
	return out
}

// Diff renders the plan the way `olr diff` shows every module's.
func (p Plan) Diff() string {
	var b strings.Builder
	for _, c := range p.Changes {
		sign := "+"
		if c.Kind == "remove" {
			sign = "-"
		}
		b.WriteString(sign + " " + c.Line + "\n")
	}
	return b.String()
}

package gateway

import (
	"fmt"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// The API's own shapes for things the module models internally.
//
// design.md §4.5: the model and the query interface are ours, always —
// including for observed things. These types exist so that "what the HTTP API
// returns" is a deliberate decision rather than a side effect of which fields
// happened to be exported, and so that a change to an internal struct does not
// silently become a breaking API change.
//
// They are hand-written on the TypeScript side too, which is the gap
// design.md §6.2 names: core reflects config structs and not response shapes,
// so this file and web/src/lib/api-types.ts have to change together.

// planView is a Plan with the diff included.
type planView struct {
	Changes []changeView  `json:"changes"`
	Impact  Impact        `json:"impact"`
	Foreign []ForeignRule `json:"foreign,omitempty"`
	Reasons []string      `json:"reasons,omitempty"`

	// Blocked is §6's refusal. A string rather than a boolean because the
	// operator has to go and find another program's configuration file, and
	// "blocked: true" does not tell them where to look.
	Blocked string `json:"blocked,omitempty"`

	// Empty is the drift answer (§5.4), precomputed so a client does not have
	// to reimplement what counts as "no change".
	Empty bool `json:"empty"`

	// Known reports whether the kernel could be read at all. Without it a
	// client cannot tell "nothing to do" from "we could not look", and those
	// need very different words on screen.
	Known bool `json:"known"`

	// Diff is the whole change as unified text, which is what makes an impact
	// actionable — "disruptive" with no diff is a scarier spinner, not an
	// explanation.
	Diff string `json:"diff,omitempty"`

	// Warnings are findings that did not block the change. Plan carries these
	// with `json:"-"` internally; they are the whole point of a preview, so
	// they are surfaced here.
	Warnings []core.Problem `json:"warnings,omitempty"`
}

type changeView struct {
	Kind ChangeKind `json:"kind"`
	Line string     `json:"line"`
}

func viewPlan(p Plan, obs Observed, desired Desired) planView {
	v := planView{
		Changes:  make([]changeView, 0, len(p.Changes)),
		Impact:   p.Impact,
		Foreign:  p.Foreign,
		Reasons:  p.Reasons,
		Blocked:  p.Blocked,
		Empty:    p.Empty(),
		Known:    obs.Known,
		Warnings: problems(p.Validation.Warnings),
	}
	for _, c := range p.Changes {
		v.Changes = append(v.Changes, changeView{Kind: c.Kind, Line: c.Line})
	}
	if obs.Known && len(p.Changes) > 0 {
		v.Diff = p.Diff(obs.Lines, desired.Lines())
	}
	return v
}

// applyResponse carries the plan and the steps alongside the stored config.
//
// Steps are present whether the apply succeeded or not: design.md §5.3.2 has no
// rollback, so a half-finished change stays half-finished and saying exactly
// which parts landed is the whole substitute for unwinding them.
type applyResponse struct {
	Plan   planView        `json:"plan"`
	Steps  []Step          `json:"steps,omitempty"`
	Config Config          `json:"config"`
	Error  *core.ErrorBody `json:"error,omitempty"`
}

// statusView is `olr gateway status`.
type statusView struct {
	Enabled bool `json:"enabled"`

	// Known reports whether the kernel answered. False on a machine that is not
	// a router, and on one where olrd lacks CAP_NET_ADMIN — two situations a
	// client should describe differently from "nothing is configured".
	Known bool `json:"known"`

	Exits       []exitStatusView       `json:"exits"`
	Assignments []assignmentStatusView `json:"assignments"`

	// Drifted is design.md §5.4: the plan against unchanged intent is not
	// empty, so the kernel disagrees with what was asked for.
	Drifted bool `json:"drifted"`

	// Foreign is §7.3's residual rule at the routing layer — somebody else's
	// rules, reported rather than hidden, because a hand-rolled setup that is
	// visible is one an operator can reason about.
	Foreign []ForeignRule `json:"foreign,omitempty"`

	// Problems carries the validation warnings for the stored config, so the
	// status screen is where a half-finished setup gets noticed.
	Problems []core.Problem `json:"problems,omitempty"`

	AsOf time.Time `json:"as_of"`
}

type exitStatusView struct {
	Name string  `json:"name"`
	Via  ViaKind `json:"via"`

	// Up and Probed together: an exit nobody probes is reported up, and saying
	// so without saying it was never checked would be claiming knowledge we do
	// not have (§5.6's third obligation — faults must not hide inside a
	// default).
	Up     bool `json:"up"`
	Probed bool `json:"probed"`

	UsedBy []string `json:"used_by,omitempty"`

	// The kernel resources this exit holds. Published because §3.2's whole
	// argument for documenting the ranges is that somebody can plan around
	// them, which they cannot do without the numbers this box actually used.
	Mark     string `json:"mark"`
	Table    int    `json:"table"`
	Priority int    `json:"priority"`
}

type assignmentStatusView struct {
	Interface string `json:"interface"`

	// Exit is the effective value and Source is which rung of the ladder set
	// it — §2.2, without which inheritance is unusable.
	Exit   string `json:"exit"`
	Source Source `json:"source"`

	Reason string `json:"reason,omitempty"`
}

func viewStatus(s Status, warnings []Problem, now time.Time) statusView {
	v := statusView{
		Enabled:     s.Enabled,
		Known:       s.Known,
		Drifted:     s.Drifted,
		Foreign:     s.Foreign,
		Problems:    problems(warnings),
		Exits:       make([]exitStatusView, 0, len(s.Exits)),
		Assignments: make([]assignmentStatusView, 0, len(s.Assignments)),
		AsOf:        now,
	}
	for _, e := range s.Exits {
		v.Exits = append(v.Exits, exitStatusView{
			Name:     e.Exit.Name,
			Via:      e.Exit.Via.Kind,
			Up:       e.Up,
			Probed:   e.Probed,
			UsedBy:   e.UsedBy,
			Mark:     markString(e.Mark),
			Table:    e.Table,
			Priority: e.Priority,
		})
	}
	for _, a := range s.Assignments {
		v.Assignments = append(v.Assignments, assignmentStatusView{
			Interface: a.Interface,
			Exit:      a.Exit,
			Source:    a.Source,
			Reason:    a.Reason,
		})
	}
	return v
}

// usageView is one client's traffic through one exit.
type usageView struct {
	Address string `json:"address"`

	// Exit is empty for the residual and explicit-direct device traffic, both
	// of which take the box's normal route, and for a conversation this router
	// served itself (§7.1): nothing classified it, because nothing forwarded it.
	Exit string `json:"exit"`

	// Unknown marks traffic still carrying a deleted exit's mark.
	Unknown bool `json:"unknown,omitempty"`

	UpBytes     uint64 `json:"up_bytes"`
	DownBytes   uint64 `json:"down_bytes"`
	UpPackets   uint64 `json:"up_packets"`
	DownPackets uint64 `json:"down_packets"`
}

// trafficView is the accounting read, with its own limits attached.
type trafficView struct {
	// Enabled is intent; Counting says whether the kernel actually has the
	// table. They differ for the moment between turning it on and applying.
	Enabled  bool `json:"enabled"`
	Counting bool `json:"counting"`

	Usage []usageView `json:"usage"`

	// Held and Capacity are how full the accounting is, in the same shape
	// dnsrelay reports its query log.
	//
	// Here for the reason §7.3 gives. A full set stops recording *new* devices
	// while going on counting the ones already in it, which is the failure mode
	// most likely to be read as "that device uses no data" rather than as "that
	// device is missing". The cap is generous for a house — roughly one element
	// per device per exit — so approaching it means something is wrong, and
	// saying which number cannot be trusted beats serving it flat.
	//
	// Held is rows returned and Capacity is the cap on *one* set, so on a
	// dual-stack network Held counts a device's v4 and v6 rows against a
	// ceiling each family has to itself. That makes the warning early rather
	// than late, which is the direction to be wrong in: it fires while the
	// numbers are still mostly right, and the alternative — a second read to
	// size each set exactly — would double the cost of the endpoint to sharpen
	// a threshold nobody should ever reach.
	Held     int `json:"held"`
	Capacity int `json:"capacity"`

	// Limits are §7.4's, carried on the response rather than written into a
	// screen.
	//
	// The doc asks for them to be printed in the UI, and putting them here
	// rather than in the SPA means the CLI and any agent reading this endpoint
	// get the same caveats — which matters, because every one of them is a
	// reason a number is *smaller* than somebody expects, and the first
	// question a surprising number produces is "is this broken?".
	Limits []string `json:"limits,omitempty"`

	AsOf time.Time `json:"as_of"`
}

// Saturated reports that the accounting set is close enough to full that rows
// are probably missing.
//
// Nine tenths rather than all of it, because the point is to warn while the
// number is still mostly right. A set that is exactly full has already been
// dropping devices for a while.
func (t trafficView) Saturated() bool {
	return t.Capacity > 0 && t.Held >= t.Capacity*9/10
}

// trafficLimits is what these numbers cannot see (§7.4).
func trafficLimits() []string {
	return []string{
		"Traffic between two devices on the same network never passes through " +
			"this router, so it is not counted. Copying to a NAS on your own " +
			"network is the usual surprise.",
		"A conversation this router serves itself — a name Ingress publishes, " +
			"its own pages — counts for the device that asked, not for the far " +
			"side: a NAS streamed through it shows on the player, not on the NAS.",
		"A device that changes address starts a new count, and IPv6 privacy " +
			"addresses rotate on purpose, so one device can appear as several.",
		"Anything behind a second router is counted as that router.",
	}
}

func viewTraffic(cfg Config, usage []Usage, counting bool, now time.Time) trafficView {
	v := trafficView{
		Enabled:  cfg.StatsOrDefault(),
		Counting: counting,
		Usage:    make([]usageView, 0, len(usage)),
		Held:     len(usage),
		Capacity: StatSetSize,
		AsOf:     now,
	}
	if counting {
		v.Limits = trafficLimits()
	}
	for _, u := range usage {
		v.Usage = append(v.Usage, usageView{
			Address:     u.Addr.String(),
			Exit:        u.Exit,
			Unknown:     u.Unknown,
			UpBytes:     u.UpBytes,
			DownBytes:   u.DownBytes,
			UpPackets:   u.UpPackets,
			DownPackets: u.DownPackets,
		})
	}
	return v
}

// markString renders a mark the way it is written everywhere else — hex, eight
// digits — rather than as a JSON number. A reader comparing it against
// `nft list ruleset` or `ip rule` output should not have to convert.
func markString(m uint32) string { return fmt.Sprintf("%#08x", m) }

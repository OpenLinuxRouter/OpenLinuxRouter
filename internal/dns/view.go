package dns

import (
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

// planView is a Plan with the diffs included.
//
// Plan omits file contents from JSON because a rendered config is long and the
// CLI does not always want it. The UI does: §5.3.3's impact classification is
// only actionable next to the lines that caused it — "disruptive" with no diff
// is a scarier spinner, not an explanation.
type planView struct {
	Backend  string        `json:"backend"`
	Changes  []changeView  `json:"changes"`
	Services []ServicePlan `json:"services"`
	Impact   Impact        `json:"impact"`

	// Reasons explains the impact in the operator's terms — above all, who
	// stops being able to resolve.
	Reasons []string `json:"reasons,omitempty"`

	// Empty is the drift answer (§5.4), precomputed so a client does not have
	// to reimplement what counts as "no change".
	Empty bool `json:"empty"`

	// Warnings are findings that did not block the change. Plan carries these
	// with `json:"-"` internally; they are the whole point of a preview, so
	// they are surfaced here.
	Warnings []core.Problem `json:"warnings,omitempty"`

	// Derived is what the module filled in for a caller who did not say — one
	// line per decision, in the operator's words ("answering DNS on
	// 192.168.1.91:53 (lan0)").
	//
	// Published rather than done quietly, which is the condition design.md §5.6
	// attaches to behaviour olr supplies by itself: a switch that turns DNS on
	// and also chooses an address has to say so, on the CLI and in the UI, in
	// the same breath as confirming the change.
	Derived []string `json:"derived,omitempty"`
}

type changeView struct {
	Path   string     `json:"path"`
	Kind   ChangeKind `json:"kind"`
	Impact Impact     `json:"impact"`
	Unit   string     `json:"unit,omitempty"`
	Diff   string     `json:"diff"`
}

func viewPlan(p Plan) planView {
	v := planView{
		Backend:  p.Backend,
		Changes:  make([]changeView, 0, len(p.Changes)),
		Services: p.Services,
		Impact:   p.Impact,
		Reasons:  p.Reasons,
		Empty:    p.Empty(),
		Warnings: problems(p.Validation.Warnings),
	}
	if v.Services == nil {
		// An absent list and an empty one mean the same thing here, and only
		// one of them makes a client check for null before iterating.
		v.Services = []ServicePlan{}
	}
	for _, c := range p.Changes {
		v.Changes = append(v.Changes, changeView{
			Path:   c.Path,
			Kind:   c.Kind,
			Impact: c.Impact,
			Unit:   c.Unit,
			Diff:   c.Diff(),
		})
	}
	return v
}

// queryView is one answered query as the API presents it.
type queryView struct {
	At     time.Time `json:"at"`
	Client string    `json:"client"`
	Name   string    `json:"name"`
	Type   string    `json:"type"`
	Rcode  string    `json:"rcode"`

	// Blocked and Policy together answer "why can this client not reach that
	// site", which is the question the query log exists for. A blocked entry
	// without the rule that blocked it would send the operator hunting.
	Blocked  bool   `json:"blocked"`
	Policy   string `json:"policy,omitempty"`
	Upstream string `json:"upstream,omitempty"`

	Answers []string `json:"answers,omitempty"`
	Chain   []string `json:"chain,omitempty"`
}

func viewQuery(q Query) queryView {
	v := queryView{
		At: q.At, Client: q.Client.String(), Name: q.Name, Type: q.Type,
		Rcode: q.Rcode, Blocked: q.Blocked, Policy: q.Policy, Upstream: q.Upstream, Chain: q.Chain,
	}
	for _, a := range q.Answers {
		v.Answers = append(v.Answers, a.String())
	}
	return v
}

// nameView is one domain→address pairing.
type nameView struct {
	Client   string    `json:"client"`
	Name     string    `json:"name"`
	Address  string    `json:"address"`
	Chain    []string  `json:"chain,omitempty"`
	LastSeen time.Time `json:"last_seen"`
}

func viewName(n Name) nameView {
	return nameView{
		Client: n.Client.String(), Name: n.Name, Address: n.Addr.String(),
		Chain: n.Chain, LastSeen: n.LastSeen,
	}
}

// statsView is the relay's account of itself, gaps included.
//
// Dropped and Unparsed are published rather than kept internal, and that is the
// point: "always show what you cannot account for". A query log that silently
// shed entries under load would be worse than none, because it would look
// complete.
type statsView struct {
	// Since is when the relay started. The log does not survive a restart, and
	// saying so beats implying a history it does not have.
	Since time.Time `json:"since"`

	Queries uint64 `json:"queries"`
	Blocked uint64 `json:"blocked"`
	Failed  uint64 `json:"failed"`

	// Dropped is observations lost because the tee was full; Unparsed is
	// responses the observer could not read. Neither cost anybody an answer —
	// both were relayed byte for byte — but both are holes in what follows.
	Dropped  uint64 `json:"dropped"`
	Unparsed uint64 `json:"unparsed"`

	Held     int `json:"held"`
	Capacity int `json:"capacity"`

	Clients []clientView `json:"clients,omitempty"`
}

type clientView struct {
	Address  string    `json:"address"`
	Queries  int       `json:"queries"`
	LastSeen time.Time `json:"last_seen"`
}

func viewStatsAPI(s Stats) statsView {
	v := statsView{
		Since: s.Since, Queries: s.Queries, Blocked: s.Blocked,
		Failed: s.Failed, Dropped: s.Dropped, Unparsed: s.Unparsed,
		Held: s.Held, Capacity: s.Capacity,
	}
	for _, c := range s.Clients {
		v.Clients = append(v.Clients, clientView{
			Address: c.Addr.String(), Queries: c.Queries, LastSeen: c.LastSeen,
		})
	}
	return v
}

// problems converts the module's validation findings into core's wire shape, so
// that every module reports a bad field the same way and a UI needs one
// renderer rather than one per module.
func problems(in []Problem) []core.Problem {
	if len(in) == 0 {
		return nil
	}
	out := make([]core.Problem, 0, len(in))
	for _, p := range in {
		out = append(out, core.Problem{Path: p.Path, Message: p.Message})
	}
	return out
}

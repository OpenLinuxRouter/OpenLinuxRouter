package ingress

import (
	"time"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// The API's own shapes for things the module models internally.
//
// design.md §4.5: the model and the query interface are ours, always. These
// types exist so that "what the HTTP API returns" is a deliberate decision
// rather than a side effect of which fields happened to be exported.

// planView is a Plan with the diffs included.
//
// Plan omits file contents from JSON because a rendered config is long and the
// CLI does not always want them. The UI does: §5.3.3's impact classification is
// only actionable next to the lines that caused it.
//
// The credential is not a special case here, and deliberately so — Change.Diff
// already withholds a secret file's contents, so this type cannot leak one by
// forgetting to. Putting the rule in the renderer of the diff rather than in
// each consumer is what makes "every surface" true rather than aspirational.
type planView struct {
	Backend string        `json:"backend"`
	Changes []changeView  `json:"changes"`
	Action  ServiceAction `json:"action"`
	Impact  Impact        `json:"impact"`

	// Enable, when non-nil, is the boot-time state the unit will be moved to.
	Enable *bool `json:"enable,omitempty"`

	// Reasons explains the impact in the operator's terms — which published
	// names stop answering, and whether connections are dropped.
	Reasons []string `json:"reasons,omitempty"`

	// Empty is the drift answer (§5.4), precomputed so a client does not have
	// to reimplement what counts as "no change".
	Empty bool `json:"empty"`

	// Warnings are findings that did not block the change.
	Warnings []core.Problem `json:"warnings,omitempty"`
}

type changeView struct {
	Path   string     `json:"path"`
	Kind   ChangeKind `json:"kind"`
	Impact Impact     `json:"impact"`
	Secret bool       `json:"secret,omitempty"`
	Diff   string     `json:"diff"`
}

func viewPlan(p Plan) planView {
	v := planView{
		Backend:  p.Backend,
		Changes:  make([]changeView, 0, len(p.Changes)),
		Action:   p.Action,
		Impact:   p.Impact,
		Enable:   p.Enable,
		Reasons:  p.Reasons,
		Empty:    p.Empty(),
		Warnings: problems(p.Validation.Warnings),
	}
	for _, c := range p.Changes {
		v.Changes = append(v.Changes, changeView{
			Path:   c.Path,
			Kind:   c.Kind,
			Impact: c.Impact,
			Secret: c.Secret,
			Diff:   c.Diff(),
		})
	}
	return v
}

// serviceView is one published service as the API reports it, with the parts
// the operator did not type filled in.
//
// The resolved address is included because it is the single most useful thing
// when a published name returns 502, and it is not in the config: the config
// names a client, and where that client is belongs to another module (§4.1).
// Reporting it here is reading it, not copying it.
type serviceView struct {
	Name string `json:"name"`

	// URL is the whole point of the module, so it is rendered rather than left
	// for a client to assemble out of a name and a domain it would have to
	// fetch separately.
	URL string `json:"url"`

	Client         string          `json:"client,omitempty"`
	Host           string          `json:"host,omitempty"`
	Port           uint16          `json:"port"`
	Scheme         Scheme          `json:"scheme"`
	RequestHeaders []RequestHeader `json:"request_headers,omitempty"`

	// Upstream is where a request actually goes right now.
	Upstream string `json:"upstream,omitempty"`

	// UpstreamError explains an upstream that could not be resolved, rather
	// than reporting an empty string and letting it read as "not configured".
	UpstreamError string `json:"upstream_error,omitempty"`
}

func viewService(s Service, domain string, clients ClientView) serviceView {
	v := serviceView{
		Name:           s.Name,
		URL:            "https://" + qualify(s.Name, domain) + s.LinkPath,
		Client:         s.Upstream.Client,
		Host:           s.Upstream.Host,
		Port:           s.Upstream.Port,
		Scheme:         s.Upstream.Scheme.OrDefault(),
		RequestHeaders: s.Upstream.RequestHeaders,
	}

	if s.Upstream.Client == "" {
		v.Upstream = s.Upstream.Target("")
		return v
	}
	info, err := clients.Client(s.Upstream.Client)
	switch {
	case err != nil:
		v.UpstreamError = err.Error()
	case !info.Addr.IsValid():
		v.UpstreamError = "the client has not been seen on the network"
	default:
		v.Upstream = s.Upstream.Target(info.Addr.String())
	}
	return v
}

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

// stamp is the freshness every observed reply carries (§4.5), so no surface can
// imply a currency it does not have.
func stamp() time.Time { return time.Now() }

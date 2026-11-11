package gateway

import (
	"context"
	"fmt"
	"net/netip"
	"sort"

	"github.com/open-linux-router/open-linux-router/internal/core"
	"github.com/open-linux-router/open-linux-router/internal/gateway/nat"
)

// Applier turns intent into kernel state.
//
// Much of the shape is internal/dhcp's, minus everything to do with a backend:
// there is no config file to render, no unit to reload and no port to check,
// because what this module drives is the kernel itself. Apply is therefore
// bounded by construction, which is what design.md §3.6 requires of anything
// holding the global apply lock — a handful of netlink calls, returning when
// they are acknowledged rather than when traffic has converged.
type Applier struct {
	// Kernel is the window onto what we program.
	Kernel Kernel

	// Links is the window onto the link module.
	Links LinkView

	// Store is core's configuration document, which owns this module's intent
	// alongside every other module's.
	Store *core.Store

	// Probes reports what the health checker currently believes. Nil means
	// every exit is treated as up, which is the right answer when nothing is
	// probing — an exit we are not watching must not be assumed dead.
	Probes HealthSource

	// Uplink is the window onto `dial`, for the one fact the egress masquerade
	// needs: which interface is the box's own way out. Nil means none, which is
	// the reference topology and writes no rule.
	Uplink UplinkView

	// DialIn is the window onto `remote`, for the one fact the tunnel's
	// masquerade needs: the network peers hold addresses on. Nil means no remote
	// access, and then no rule is written.
	DialIn DialInView
}

// NATApplier is the other half of this module, wired to the same document.
//
// Built here rather than held as a field, because the seam is two closures over
// this Applier: nat loads and saves through us, so the forwards travel inside
// gateway's config section and one module keeps one owner for one document.
func (a Applier) NATApplier(kernel nat.Kernel, links nat.LinkView) nat.Applier {
	return nat.Applier{
		Kernel: kernel,
		Links:  links,
		LoadConfig: func() (nat.Config, error) {
			cfg, err := a.Load()
			if err != nil {
				return nat.Config{}, err
			}
			return a.natOf(cfg), nil
		},
		SaveConfig: func(nc nat.Config) error {
			cfg, err := a.Load()
			if err != nil {
				return err
			}
			cfg.Forwards = nc.Forwards
			_, err = a.Save(cfg)
			return err
		},
	}
}

// natOf projects a stored config into what the nat half works on, filling in
// the two facts that come from outside this module.
//
// Both reads are tolerant of failure: a box whose uplink or networks cannot be
// read gets no egress rule rather than an error, which is the same call
// link.Applier.observeQuietly makes and for the same reason — the rules that
// need those facts can only ever *add* a rule, so losing them degrades to
// "write less" rather than to "refuse to answer".
func (a Applier) natOf(cfg Config) nat.Config {
	var uplink string
	if a.Uplink != nil {
		if name, err := a.Uplink.Uplink(); err == nil {
			uplink = name
		}
	}
	var networks []netip.Prefix
	if a.Links != nil {
		if nets, err := a.Links.Networks(); err == nil {
			networks = nets
		}
	}
	// The tunnel's network, read through `remote`. A failure writes no dial-in
	// rule, the same call the two reads above make: losing a fact can only ever
	// remove a rule, never invent one.
	var dialIn *nat.DialIn
	if a.DialIn != nil {
		if info, err := a.DialIn.DialIn(); err == nil && info.Enabled {
			dialIn = &nat.DialIn{Prefix: info.Prefix, Tunnel: info.Interface}
		}
	}
	return cfg.NAT(uplink, networks, dialIn)
}

// HealthSource is where exit health comes from.
//
// An interface rather than the prober itself, so that planning and applying can
// be tested without one running, and so that the prober can be absent entirely
// on a build with no network.
type HealthSource interface {
	// Health reports up/down per exit name. Exits it has no opinion about are
	// absent from the map and are treated as up.
	Health() Health
}

func (a Applier) health() Health {
	if a.Probes == nil {
		return nil
	}
	return a.Probes.Health()
}

// Load reads stored intent out of the configuration document.
func (a Applier) Load() (Config, error) {
	doc, err := a.Store.Load()
	if err != nil {
		return Config{}, err
	}
	return FromDocument(doc)
}

// Save validates and stores intent, returning the stored form.
//
// Read-modify-write on the shared document, so a save here cannot drop another
// module's configuration. It is safe without further locking because every
// config write in the process holds the one global apply lock (§3.6) — the
// caller takes it.
//
// Storing is deliberately separate from programming: a config that cannot
// currently be applied — because the tunnel interface has not come up yet, or
// because another tool is holding the routing table — is still a config the
// operator asked for, and losing it on the way to reporting the problem would
// make the problem worse.
func (a Applier) Save(cfg Config) (Config, error) {
	cfg.Normalize()
	if res := Validate(cfg, a.Links); !res.OK() {
		return cfg, res.Err()
	}

	doc, err := a.Store.Load()
	if err != nil {
		return cfg, err
	}
	data, err := MarshalConfig(cfg)
	if err != nil {
		return cfg, err
	}
	doc.Set(ModuleName, data)
	if err := a.Store.Save(doc); err != nil {
		return cfg, fmt.Errorf("storing configuration in %s: %w", a.Store.Path(), err)
	}
	return cfg, nil
}

// Observe reads the actual state of the system, fresh.
func (a Applier) Observe(ctx context.Context) (Observed, error) {
	return a.Kernel.Observe(ctx)
}

// Plan answers "what would applying this do?" without doing it.
//
// admin is the address the request arrived from, or the zero value for a local
// caller over the unix socket. It is what lets the plan warn that a change
// would route the operator's own connection somewhere else — §5.1's lockout
// row, and the one mistake here that cannot be undone by clicking again.
func (a Applier) Plan(ctx context.Context, cfg Config, admin netip.Addr) (Plan, Desired, error) {
	obs, err := a.Observe(ctx)
	if err != nil {
		return Plan{}, Desired{}, err
	}
	return BuildPlan(cfg, a.Links, a.health(), obs, admin)
}

// Apply stores intent and programs it.
//
// The order is store-then-program, and it matters for recovery: if programming
// fails, the intent is on disk and a later `olr gateway apply` — or the next
// boot — finishes the job, which is design.md §5.3.2's idempotent re-apply
// rather than a rollback.
func (a Applier) Apply(ctx context.Context, cfg Config, admin netip.Addr) (ApplyResult, Config, error) {
	plan, desired, err := a.Plan(ctx, cfg, admin)
	if err != nil {
		return ApplyResult{Plan: plan}, cfg, err
	}
	return a.ApplyPlanned(ctx, cfg, plan, desired)
}

// ApplyPlanned programs a plan that has already been built.
//
// Split out of Apply for the caller that has to look at the plan before deciding
// whether to act on it — the HTTP layer refuses a `disruptive` change that was
// not confirmed (§5.3.3), and it can only know that by planning first. Without
// this split, acting on that decision would mean reading the kernel a second
// time, and the second read could disagree with the one the operator was shown.
func (a Applier) ApplyPlanned(ctx context.Context, cfg Config, plan Plan, desired Desired) (ApplyResult, Config, error) {
	if plan.Blocked != "" {
		// §6: detect and refuse. We do not rewrite somebody else's config file
		// and we do not silently work around them, because the failure mode of
		// sharing the routing table is that it works until a version bump moves
		// a priority number and then some traffic quietly takes the wrong path.
		return ApplyResult{Plan: plan}, cfg, fmt.Errorf("%s", plan.Blocked)
	}

	stored, err := a.Save(cfg)
	if err != nil {
		return ApplyResult{Plan: plan}, stored, err
	}

	if plan.Empty() {
		// Nothing to program. Distinguished from "programmed successfully with
		// no steps" by there being no steps at all, which is what lets a caller
		// tell a no-op apply from a real one without re-deriving the plan.
		return ApplyResult{Plan: plan}, stored, nil
	}

	steps, err := a.Kernel.Apply(ctx, desired)
	return ApplyResult{Plan: plan, Steps: steps}, stored, err
}

// Usage is one client's traffic, with the exit named rather than marked.
type Usage struct {
	Addr netip.Addr

	// Exit is the operator's name for where this traffic went, empty for the
	// residual — traffic no assignment matched. Unknown is set when the mark
	// names a slot no exit currently holds, which happens for a moment after an
	// exit is deleted and for as long as its flows survive.
	Exit    string
	Unknown bool

	UpBytes, DownBytes     uint64
	UpPackets, DownPackets uint64
}

// Total is the bytes in both directions, which is what a list sorts by.
func (u Usage) Total() uint64 { return u.UpBytes + u.DownBytes }

// Traffic reads the accounting sets and resolves each mark to an exit name.
//
// The resolution is the whole reason this is not just a passthrough: a mark is
// a number the operator never chose and cannot look up, and §4.5 says the model
// and the query interface are ours — including for observed things.
func (a Applier) Traffic(ctx context.Context) ([]Usage, error) {
	cfg, err := a.Load()
	if err != nil {
		return nil, err
	}

	flows, err := a.Kernel.Traffic(ctx)
	if err != nil {
		return nil, err
	}

	byMark := map[uint32]string{}
	byMark[DirectMark] = ""
	for _, e := range cfg.Exits {
		byMark[e.Mark()] = e.Name
	}

	out := make([]Usage, 0, len(flows))
	for _, f := range flows {
		// A conversation the box has with itself is counted too — the local
		// chains do not know which end is a device — and 127.0.0.1 is nobody's
		// address. Dropped here rather than kept out of the kernel, because a
		// loopback guard would cost a rule on every packet to save a row that
		// has nowhere to be drawn.
		if f.Addr.IsLoopback() {
			continue
		}
		u := Usage{
			Addr:        f.Addr,
			UpBytes:     f.UpBytes,
			DownBytes:   f.DownBytes,
			UpPackets:   f.UpPackets,
			DownPackets: f.DownPackets,
		}
		switch name, ok := byMark[f.Mark]; {
		case f.Mark == 0:
			// The residual, and it is a row rather than an omission (§7.3):
			// per-exit totals only reconcile against the box total if what
			// matched nothing is visible too.
		case ok:
			u.Exit = name
		default:
			u.Unknown = true
		}
		out = append(out, u)
	}

	// Biggest first: the question this screen answers is "who is using the
	// bandwidth", and the answer is at the top.
	sort.SliceStable(out, func(i, j int) bool { return out[i].Total() > out[j].Total() })
	return out, nil
}

// Status is the module's account of itself: what is configured, what is
// effective, what is running, and what it cannot account for.
type Status struct {
	// Enabled is intent; Known is whether the kernel could be read at all.
	Enabled bool
	Known   bool

	// Exits is one row per configured exit, with its effective state.
	Exits []ExitStatus

	// Assignments is §2.2's effective value with its source, per network.
	Assignments []AssignmentStatus

	// Drifted reports whether the kernel disagrees with intent, and Plan
	// carries the detail. design.md §5.4: drift is not separate machinery, it
	// is the plan against unchanged intent.
	Drifted bool
	Plan    Plan

	// Foreign is §7.3's residual rule at the routing layer: someone else's
	// rules, reported rather than hidden, because a hand-rolled setup that is
	// visible is one somebody can reason about.
	Foreign []ForeignRule
}

// ExitStatus is one exit and what is true of it right now.
type ExitStatus struct {
	Exit Exit

	// Up is the prober's verdict. Probed says whether anybody asked.
	Up     bool
	Probed bool

	// UsedBy lists networks and client MACs assigned to it.
	UsedBy []string

	// Mark, Table and Priority are the kernel resources it holds, surfaced
	// because §3.2's whole argument for documenting the ranges is that somebody
	// can plan around them — which they cannot do without being told the
	// numbers this box actually used.
	Mark     uint32
	Table    int
	Priority int
}

// AssignmentStatus is one network's effective exit and where it came from.
type AssignmentStatus struct {
	Interface string
	Exit      string
	Source    Source

	// Reason explains a state that is not simply "via this exit" — an exit that
	// is down, or a network that falls back to the box's own route. This is
	// §2.2's *"no internet — Proxy is down"*, which is the sentence that makes
	// a failure diagnosable in the place the operator is already looking.
	Reason string
}

// GetStatus assembles the status, reading everything fresh.
func (a Applier) GetStatus(ctx context.Context) (Status, error) {
	cfg, err := a.Load()
	if err != nil {
		return Status{}, err
	}

	health := a.health()
	st := Status{Enabled: cfg.Enabled}

	obs, obsErr := a.Observe(ctx)
	if obsErr == nil {
		st.Known = obs.Known
		st.Foreign = obs.Foreign
		// The zero address: status is a read, so there is no caller whose
		// connection could be moved by it.
		plan, _, err := BuildPlan(cfg, a.Links, health, obs, netip.Addr{})
		if err == nil {
			st.Plan = plan
			st.Drifted = obs.Known && !plan.Empty()
		}
	}

	for _, e := range cfg.Exits {
		up, probed := health[e.Name]
		st.Exits = append(st.Exits, ExitStatus{
			Exit:     e,
			Up:       up || !probed,
			Probed:   probed,
			UsedBy:   cfg.UsedBy(e.Name),
			Mark:     e.Mark(),
			Table:    e.Table(),
			Priority: e.Priority(),
		})
	}

	for _, as := range cfg.Interfaces {
		name, source := cfg.Assigned(as.Interface)
		row := AssignmentStatus{Interface: as.Interface, Exit: name, Source: source}
		switch {
		case name == "":
			row.Reason = "uses the box's own connection"
		case health.Down(name):
			e, _ := cfg.Find(name)
			if e.OnFailure.OrDefault() == FailDirect {
				row.Reason = fmt.Sprintf("%s is not responding, so traffic is using the box's own connection", name)
			} else {
				row.Reason = fmt.Sprintf("no internet — %s is not responding", name)
			}
		}
		st.Assignments = append(st.Assignments, row)
	}

	return st, obsErr
}

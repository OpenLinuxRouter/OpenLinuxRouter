package gateway

import (
	"context"
	"errors"
	"net/netip"
	"sort"
	"strings"
)

// The seam between the decision layer and the kernel.
//
// design.md §10 requires netlink, nftables and systemd to sit behind an
// interface so that everything else is unit-testable off Linux, and §3.6 notes
// that this testability seam is also the privilege-separation seam if that
// split is ever taken. This module leans on it harder than the others do: dhcp
// and dns render files that can be asserted on anywhere, while what this one
// produces *is* kernel state, so without a seam there would be nothing to test
// on a machine that is not a router.

// ErrUnsupported is returned by a kernel that cannot do this at all.
var ErrUnsupported = errors.New("routing needs a Linux kernel with nftables and policy routing")

// Kernel is the window onto what this module programs.
//
// Reads and the one write. Every decision — which prefix gets which mark, what
// happens to IPv6, whether a change is disruptive — is made above this line
// against values, so an implementation of it has no policy in it at all.
type Kernel interface {
	// Observe reads the live state, fresh. Planning against observation rather
	// than against a cached copy of what we last wrote is what makes drift
	// detection free (design.md §5.4).
	Observe(ctx context.Context) (Observed, error)

	// Traffic reads the accounting sets (§7.1).
	//
	// Separate from Observe because it is asked at a different rate and for a
	// different reason: Observe answers "does the kernel match intent" on every
	// plan, while this answers "who used the bandwidth" on a screen somebody is
	// watching. Folding them together would make every apply pay for a walk of
	// every device on the network.
	Traffic(ctx context.Context) ([]Flow, error)

	// ReadRoutes reads the route table in full, every table and both families.
	//
	// Separate from Observe, which reads only the default route in each of our
	// own tables — the subset drift detection compares. This is the whole
	// table, for a page whose subject is the table itself, and it is why that
	// page can show a route olr did not write without pretending to own it.
	ReadRoutes(ctx context.Context) ([]KernelRoute, error)

	// Apply programs the desired state, returning what it managed to do.
	//
	// It returns steps alongside an error rather than instead of one. There is
	// no rollback here on purpose (design.md §5.2/§5.3.2): if a multi-step
	// change fails halfway the steps that landed stay landed and are reported,
	// and re-running finishes the job. That is more honest and more debuggable
	// than a silent revert — and on this module a revert would itself be a
	// routing change, with its own chance of failing.
	Apply(ctx context.Context, d Desired) ([]Step, error)
}

// Flow is one client's traffic through one exit, as the kernel counts it.
//
// The key is `(address, mark)`, so a device that uses two exits appears twice —
// which is the point. Mark 0 is traffic no assignment matched, and it is
// reported rather than dropped: §7.3's residual rule, applied to the place it
// is easiest to quietly omit.
type Flow struct {
	Addr netip.Addr

	// Mark is the exit's fwmark, already masked to our byte. Zero means
	// unpoliced.
	Mark uint32

	// Up is what the device sent, Down what it received. Bytes and packets
	// both, because a count of packets is what distinguishes a device that is
	// idle from one that is unreachable.
	UpBytes, UpPackets     uint64
	DownBytes, DownPackets uint64
}

// Step is one unit of work and how it went.
type Step struct {
	Description string `json:"description"`
	Done        bool   `json:"done"`
	Error       string `json:"error,omitempty"`
}

// ApplyResult is what an apply actually did.
type ApplyResult struct {
	Plan  Plan   `json:"plan"`
	Steps []Step `json:"steps"`
}

// ApplyOrder is the sequence Apply must follow, and the comment is the whole
// reason it is written down rather than left to the implementation.
//
// Build bottom-up and tear down top-down. Every intermediate state in between
// has to be one where traffic is either handled correctly or not classified at
// all — never classified toward something that does not exist yet:
//
//	routes  →  ip rules  →  nft marks        (installing)
//	nft marks  →  ip rules  →  routes        (removing)
//
// Get it backwards and there is a window where a packet is marked for a table
// whose default route has not been created, so the lookup misses, the RPDB
// falls through to `main`, and the traffic the operator asked to route goes out
// direct. It is a window of milliseconds and it is a silent leak, which is the
// combination this module works hardest to avoid.
const ApplyOrder = "routes, then ip rules, then nftables"

// StaticKernel is a Kernel backed by a slice of lines.
//
// It is what the tests use, and it is deliberately not a mock: it holds the
// same canonical lines a real kernel would report, so a test asserts on the
// plan an operator would see rather than on which methods were called.
type StaticKernel struct {
	// State is the live state, in canonical form — the same subset the real
	// kernel reports, so sysctls are *not* in here. Apply replaces it.
	State []string

	// Sysctls is the settings this module owns, by key, as the real kernel
	// reports them: the machine-wide forwarding switch and the per-interface
	// redirect settings. Apply records what it was asked to write.
	Sysctls map[string]string

	// Foreign, AllSendRedirects, RAHosts and Active fill out the rest of
	// Observed.
	Foreign          []ForeignRule
	AllSendRedirects *bool
	RAHosts          []string
	Active           []string

	// Flows is what Traffic returns.
	Flows []Flow

	// Routes is what ReadRoutes returns, and RoutesErr forces its failure
	// path.
	Routes    []KernelRoute
	RoutesErr error

	// Unknown makes Observe report that the kernel could not be read, which is
	// what a non-Linux build and an unprivileged container both look like.
	Unknown bool

	// ObserveErr and ApplyErr force the failure paths.
	ObserveErr error
	ApplyErr   error

	// FailAfter stops Apply after this many steps when ApplyErr is set, so a
	// test can assert on the half-finished state design.md §5.3.2 promises to
	// report rather than unwind.
	FailAfter int
}

// Observe implements Kernel.
func (k *StaticKernel) Observe(context.Context) (Observed, error) {
	if k.ObserveErr != nil {
		return Observed{}, k.ObserveErr
	}
	obs := Observed{
		Known:            !k.Unknown,
		Lines:            append([]string(nil), k.State...),
		Sysctls:          map[string]string{},
		Foreign:          append([]ForeignRule(nil), k.Foreign...),
		AllSendRedirects: k.AllSendRedirects,
		RAHosts:          append([]string(nil), k.RAHosts...),
	}
	for key, value := range k.Sysctls {
		obs.Sysctls[key] = value
	}
	obs.Active = parseAddrs(k.Active)
	sort.Strings(obs.Lines)
	return obs, nil
}

// Traffic implements Kernel.
func (k *StaticKernel) Traffic(context.Context) ([]Flow, error) {
	if k.Unknown {
		return nil, ErrUnsupported
	}
	return append([]Flow(nil), k.Flows...), nil
}

// ReadRoutes implements Kernel.
func (k *StaticKernel) ReadRoutes(context.Context) ([]KernelRoute, error) {
	if k.RoutesErr != nil {
		return nil, k.RoutesErr
	}
	if k.Unknown {
		return nil, ErrUnsupported
	}
	return append([]KernelRoute(nil), k.Routes...), nil
}

// Apply implements Kernel.
func (k *StaticKernel) Apply(_ context.Context, d Desired) ([]Step, error) {
	steps := []Step{
		{Description: "write routes", Done: true},
		{Description: "write ip rules", Done: true},
		{Description: "write nftables", Done: true},
	}
	if k.ApplyErr != nil {
		at := k.FailAfter
		if at < 0 || at > len(steps) {
			at = 0
		}
		steps = steps[:at+1]
		steps[at] = Step{Description: steps[at].Description, Error: k.ApplyErr.Error()}
		return steps, k.ApplyErr
	}
	// objectLines, not Lines: the real kernel reports the objects it holds, and
	// sysctls are read separately because they are settings we edit rather than
	// objects we own. A fake that conflated the two would make every plan
	// against an already-correct box report work to do.
	k.State = d.objectLines()
	sort.Strings(k.State)

	if k.Sysctls == nil {
		k.Sysctls = map[string]string{}
	}
	for _, s := range d.Sysctls {
		k.Sysctls[s.Key] = s.Value
	}
	return steps, nil
}

func parseAddrs(in []string) []netip.Addr {
	out := make([]netip.Addr, 0, len(in))
	for _, s := range in {
		if a, err := netip.ParseAddr(strings.TrimSpace(s)); err == nil {
			out = append(out, a)
		}
	}
	return out
}

//go:build linux

package gateway

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/dial"

	"github.com/google/nftables"
	"github.com/google/nftables/binaryutil"
	"github.com/google/nftables/expr"
	"github.com/google/nftables/userdata"
	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

// The kernel half, and the only file in this module that knows what netlink is.
//
// Everything above it works in values: Render decides, BuildPlan compares, and
// this translates. That is what design.md §10 asks for under test hardware, and
// it is load-bearing here in a way it is not for the file-rendering modules —
// what this module configures *is* the kernel, so without the seam there would
// be nothing testable off a router at all.
//
// There is no `exec.Command` here and there never may be (design.md §3.6): the
// sandbox in olrd.service is nearly free precisely because the process spawns
// nothing, and the first shell-out silently costs all of it.

// LinuxKernel programs nftables and the routing policy database.
type LinuxKernel struct {
	// ProcRoot prefixes /proc, for tests and for the --root development mode.
	// Empty means the real one.
	ProcRoot string
}

// NewKernel returns the kernel implementation for this platform.
func NewKernel() Kernel { return LinuxKernel{} }

// --- observation ----------------------------------------------------------

// Observe reads the live state into the same canonical lines Render produces.
//
// Every part is read fresh from the kernel. Nothing here consults a cache of
// what we last wrote, which is what makes drift detection mean something: a
// hand-run `ip rule del` has to show up the same way a hand-edited config file
// does in the other modules (design.md §5.4, §4.5).
func (k LinuxKernel) Observe(_ context.Context) (Observed, error) {
	obs := Observed{Known: true, Sysctls: map[string]string{}}

	nftLines, err := k.observeNFT()
	if err != nil {
		return Observed{}, fmt.Errorf("reading nftables: %w", err)
	}
	obs.Lines = append(obs.Lines, nftLines...)

	statConn, err := nftables.New()
	if err != nil {
		return Observed{}, fmt.Errorf("reading nftables: %w", err)
	}
	statLines, err := k.observeStat(statConn)
	statConn.CloseLasting()
	if err != nil {
		return Observed{}, fmt.Errorf("reading the accounting table: %w", err)
	}
	obs.Lines = append(obs.Lines, statLines...)

	ruleLines, foreign, err := k.observeRules()
	if err != nil {
		return Observed{}, fmt.Errorf("reading ip rules: %w", err)
	}
	obs.Lines = append(obs.Lines, ruleLines...)
	obs.Foreign = foreign

	routeLines, err := k.observeRoutes()
	if err != nil {
		return Observed{}, fmt.Errorf("reading routes: %w", err)
	}
	obs.Lines = append(obs.Lines, routeLines...)

	obs.Sysctls = k.readSysctls()
	if v, ok := k.readSysctl("net.ipv4.conf.all.send_redirects"); ok {
		on := v != "0"
		obs.AllSendRedirects = &on
	}

	obs.RAHosts = k.observeRAHosts()

	obs.Active = k.observeActive()

	return obs, nil
}

// observeNFT reads our table back.
//
// Each rule carries its own canonical line in netlink userdata, in nft's
// comment format, so this is a read rather than a reconstruction — and
// `nft list ruleset` shows the same text `olr diff` does, which is worth more
// than it costs.
//
// **The known gap, stated rather than left to be found.** A rule whose
// expressions were changed by hand while its comment was left intact reads back
// as unchanged, so that specific edit is not detected as drift. Reconstructing
// a rule from its expression list is what would close it, and that is the
// nftables reader design.md §4.2 gives to the `firewall` module for the whole
// box rather than to each module for its own table. The cost is bounded in the
// meantime: every apply replaces the table wholesale in one transaction, so a
// hand-edit is *corrected* on the next apply even while it goes unreported.
func (k LinuxKernel) observeNFT() ([]string, error) {
	conn, err := nftables.New()
	if err != nil {
		return nil, err
	}
	defer conn.CloseLasting()

	tables, err := conn.ListTablesOfFamily(nftables.TableFamilyINet)
	if err != nil {
		return nil, err
	}
	var table *nftables.Table
	for _, t := range tables {
		if t.Name == TableName {
			table = t
			break
		}
	}
	if table == nil {
		return nil, nil
	}

	out := []string{fmt.Sprintf("nft table inet %s", TableName)}

	objs, err := conn.GetObjects(table)
	if err != nil {
		return nil, err
	}
	for _, o := range objs {
		if c, ok := o.(*nftables.CounterObj); ok {
			out = append(out, fmt.Sprintf("nft counter %s", c.Name))
		}
	}

	chains, err := conn.ListChainsOfTableFamily(nftables.TableFamilyINet)
	if err != nil {
		return nil, err
	}
	for _, c := range chains {
		if c.Table == nil || c.Table.Name != TableName {
			continue
		}
		switch c.Name {
		case ClassifyChain:
			out = append(out, fmt.Sprintf("nft chain %s prerouting mangle", ClassifyChain))
		case PostroutingChain:
			out = append(out, fmt.Sprintf("nft chain %s postrouting srcnat", PostroutingChain))
		default:
			continue
		}

		rules, err := conn.GetRules(table, c)
		if err != nil {
			return nil, err
		}
		for _, r := range rules {
			if line, ok := userdata.GetString(r.UserData, userdata.TypeComment); ok {
				out = append(out, line)
			} else {
				// A rule in our table that we did not label. Reported as an
				// unrecognised line so it shows up as something to remove,
				// rather than being silently tolerated in a table we claim to
				// own outright.
				out = append(out, fmt.Sprintf("nft unrecognised rule in %s", c.Name))
			}
		}
	}

	return out, nil
}

// --- accounting (§7.1) ----------------------------------------------------

// The 32-bit register file, which the kernel numbers separately from the
// 16-byte NFT_REG_1..4 and which neither google/nftables nor x/sys/unix
// exports.
//
// Concatenation needs these: a concatenated key is a run of 4-byte components,
// so `address . mark` is written into consecutive 32-bit registers and the
// dynset is told where the run starts. Using NFT_REG_1 and NFT_REG_2 instead
// would leave a twelve-byte hole between the two components, because NFT_REG_1
// is sixteen bytes wide.
//
// NFT_REG_1 *aliases* reg32_00 through reg32_03, which is why the family check
// below can use register 1 and then overwrite it with the key: the comparison
// has already happened by then.
const (
	reg32_00 = 8
	reg32_01 = 9
	reg32_04 = 12
)

// The conntrack directions, as nft_ct_get_eval stores them for
// NFT_CT_DIRECTION. Spelled out here because x/sys/unix does not export
// nf_conntrack_common.h's enum, and they are stable kernel ABI.
const (
	ctDirOriginal = 0
	ctDirReply    = 1
)

// statTimeout ages out an entry that has seen no traffic.
//
// This is §7.5's retention window for byte counters, and the reason it exists
// is §7.4's: IPv6 privacy addresses rotate, so without it a dual-stack network
// fills the set with addresses no device answers to any more and then stops
// recording the ones it does. A device that goes quiet for a day loses its
// running total, which is acceptable because §7.5 says the counters are
// monotonic-since-load and every consumer of them wants deltas anyway.
//
// It is an element *attribute*, not an expression, which matters more than it
// looks — see readSetCounters.
const statTimeout = 24 * time.Hour

// observeStat reads back the accounting table's shape.
//
// Shape only: the sets' *contents* are traffic, read by Traffic and never
// compared against intent. A device appearing on the network is not drift.
func (k LinuxKernel) observeStat(conn *nftables.Conn) ([]string, error) {
	tables, err := conn.ListTablesOfFamily(nftables.TableFamilyINet)
	if err != nil {
		return nil, err
	}
	var table *nftables.Table
	for _, t := range tables {
		if t.Name == StatTableName {
			table = t
			break
		}
	}
	if table == nil {
		return nil, nil
	}

	out := []string{fmt.Sprintf("nft table inet %s", StatTableName)}

	chains, err := statChains(conn)
	if err != nil {
		return nil, err
	}
	for _, want := range StatChains() {
		chain, ok := chains[want.Name]
		if !ok {
			// A table missing a chain counts part of what it was built to
			// count. Reported as the table alone, which does not match what we
			// would write, so the plan shows it and applying rebuilds it —
			// rather than as a missing chain line, which would read as
			// something to add beside the chains that are already there.
			return out, nil
		}
		out = append(out, observedChainLine(chain))
		lines, err := statRuleLines(conn, table, chain, want)
		if err != nil {
			return nil, err
		}
		out = append(out, lines...)
	}

	return out, nil
}

// observedChainLine renders a chain the kernel holds in the canonical form the
// plan diffs against.
//
// Read from the chain rather than written from the spec, for the reason
// statRuleLines gives about rules: a chain moved to another hook, or hung off
// another hook by an older version, has to read back differently or the drift
// is invisible and never repaired.
func observedChainLine(c *nftables.Chain) string {
	return fmt.Sprintf("nft chain %s %s %s", c.Name, hookName(c.Hooknum), c.Type)
}

// hookName is nft's word for the hook a base chain runs on.
//
// The comparison is on the hook's value, not on the pointer. google/nftables
// exposes the known hooks as package-level *ChainHook values, and a chain read
// back from netlink carries a pointer of its own to the same number — so
// `switch hook { case nftables.ChainHookForward: }` compared two addresses and
// never matched. Every real chain then rendered as "hook 2", which read as
// drift on a box that was already correct and, because apply compares the same
// lines, could not be cleared by restoring. Dereferencing both sides is what
// makes "the kernel installed forward" and "we want forward" the same answer.
func hookName(hook *nftables.ChainHook) string {
	if hook == nil {
		return "none"
	}
	switch *hook {
	case *nftables.ChainHookForward:
		return "forward"
	case *nftables.ChainHookInput:
		return "input"
	case *nftables.ChainHookOutput:
		return "output"
	}
	return fmt.Sprintf("hook %d", *hook)
}

// hookNumber is the reverse, for building a chain.
func hookNumber(hook string) *nftables.ChainHook {
	switch hook {
	case "forward":
		return nftables.ChainHookForward
	case "input":
		return nftables.ChainHookInput
	case "output":
		return nftables.ChainHookOutput
	}
	// Callers pass the constants in StatChains, so this is a programming error
	// rather than a condition to report: a chain with no hook would be built
	// and count nothing, which is the failure this whole change is about.
	panic("gateway: unknown accounting hook " + hook)
}

// statChains finds the accounting table's chains, keyed by name.
//
// Its own function because both readers need the same view and because "absent"
// has to be a value rather than an error: asking the kernel for the rules of a
// chain that does not exist is an error, and a half-built table is a thing to
// rebuild, not a thing to fail the status endpoint over.
func statChains(conn *nftables.Conn) (map[string]*nftables.Chain, error) {
	chains, err := conn.ListChainsOfTableFamily(nftables.TableFamilyINet)
	if err != nil {
		return nil, err
	}
	out := map[string]*nftables.Chain{}
	for _, c := range chains {
		if c.Table != nil && c.Table.Name == StatTableName {
			out[c.Name] = c
		}
	}
	return out, nil
}

// statRuleLines reads the accounting rules' canonical forms back off the rules
// themselves, from the comment each one carries.
//
// Read from userdata rather than rendered from the spec, and that is the whole
// point of the function. The set *names* have not changed across versions, so a
// check that only asked "is there a set called dev_down4" would say yes to a
// table built by a version that fed it the wrong address, and the fix would
// never reach a box that had already applied once. The comment describes the
// rule, so a rule that changed shape reads back differently and shows up as
// drift — which applyStat then rebuilds.
//
// A rule whose comment we cannot read at all is reported under its set's name
// with no detail. That is still a mismatch against the line we would write, so
// it rebuilds too, which is the safe direction to fail in.
//
// Extra rules in the chain are reported as one line saying so. Nothing we write
// puts them there, and a table carrying rules from a version whose comments we
// no longer recognise is a table to replace rather than to leave counting.
func statRuleLines(conn *nftables.Conn, table *nftables.Table, chain *nftables.Chain, want StatChain) ([]string, error) {
	rules, err := conn.GetRules(table, chain)
	if err != nil {
		return nil, err
	}
	comments := map[string]bool{}
	for _, r := range rules {
		if line, ok := userdata.GetString(r.UserData, userdata.TypeComment); ok {
			comments[line] = true
		}
	}

	// The line does not name its chain, so the same rule text appears in more
	// than one chain by design: up4's ordinary rule is what forward and input
	// both install. Who compares what, then: statTableMatches reads each chain
	// separately, and the plan's diff counts lines, so two chains carrying the
	// same rule is two entries either way. A rule *moved* between two existing
	// chains is the one shape this cannot see, and applyStat's own comparison
	// is what replaces the table in that case.
	out := make([]string, 0, len(want.Rules))
	for _, r := range want.Rules {
		line := r.Line()
		if comments[line] {
			out = append(out, line)
		} else {
			out = append(out, fmt.Sprintf("nft stat set %s built by another version", r.Set.Name))
		}
	}
	if extra := len(rules) - len(want.Rules); extra > 0 {
		out = append(out, fmt.Sprintf("nft chain %s carries %d rules this version does not write", chain.Name, extra))
	}
	return out, nil
}

// statTableMatches reports whether the table on the box is the one this
// version would build, judged by the same canonical lines the plan diffs.
//
// Deliberately the same comparison the plan shows the operator rather than a
// second, private notion of "close enough": if `olr gateway plan` says the
// accounting table is drifting, applying is what stops it saying so.
func statTableMatches(conn *nftables.Conn, table *nftables.Table, want StatTable) (bool, error) {
	chains, err := statChains(conn)
	if err != nil {
		return false, err
	}
	// Exactly the chains we would write, no more and no fewer: a leftover chain
	// from a version that counted somewhere else would keep counting.
	if len(chains) != len(want.Chains) {
		return false, nil
	}
	for _, wantChain := range want.Chains {
		chain, ok := chains[wantChain.Name]
		if !ok {
			return false, nil
		}
		if observedChainLine(chain) != wantChain.Line() {
			return false, nil
		}
		have, err := statRuleLines(conn, table, chain, wantChain)
		if err != nil {
			return false, err
		}
		wantLines := ruleLines(wantChain)
		if len(have) != len(wantLines) {
			return false, nil
		}
		for i := range have {
			if have[i] != wantLines[i] {
				return false, nil
			}
		}
	}
	return true, nil
}

// ruleLines is a chain's rules' canonical forms.
func ruleLines(c StatChain) []string {
	out := make([]string, 0, len(c.Rules))
	for _, r := range c.Rules {
		out = append(out, r.Line())
	}
	return out
}

// applyStat replaces the accounting table, in its own transaction.
//
// Separate from applyNFT because the two tables have different lifetimes
// (§7.1): turning routing off must leave the counters running, and turning
// accounting off must not disturb a single route.
func (k LinuxKernel) applyStat(d Desired) error {
	conn, err := nftables.New()
	if err != nil {
		return err
	}
	defer conn.CloseLasting()

	tables, err := conn.ListTablesOfFamily(nftables.TableFamilyINet)
	if err != nil {
		return err
	}
	var existing *nftables.Table
	for _, t := range tables {
		if t.Name == StatTableName {
			existing = t
			break
		}
	}

	if !d.Stat.Enabled {
		if existing == nil {
			return nil
		}
		conn.DelTable(existing)
		return conn.Flush()
	}

	// Left alone when it is already the table this version would build. Unlike
	// olr_route, whose content changes with the configuration, this table does
	// not depend on config at all — and deleting it on every apply would throw
	// away every byte counted so far, which is the one thing an operator
	// watching a graph would notice.
	//
	// "Already the table this version would build" is the part that has to be
	// checked rather than assumed. The table is a constant *for a given
	// version*, not across them, and the earlier version of this function
	// returned here on the strength of the table merely existing. That is fine
	// until the rules inside it change, at which point every box that had ever
	// applied would keep the old ones forever and no amount of upgrading would
	// fix it. So: compare, and rebuild when it differs.
	//
	// Rebuilding resets the counters, once, on the upgrade that changes them.
	// That is the right trade against carrying wrong numbers indefinitely, and
	// it is the same event as a reboot, which §7.5 already says these counters
	// do not survive.
	if existing != nil {
		same, err := statTableMatches(conn, existing, d.Stat)
		if err != nil {
			return err
		}
		if same {
			return nil
		}
		conn.DelTable(existing)
		if err := conn.Flush(); err != nil {
			return fmt.Errorf("replacing accounting table: %w", err)
		}
	}

	table := conn.AddTable(&nftables.Table{
		Family: nftables.TableFamilyINet,
		Name:   StatTableName,
	})

	for _, s := range StatSets() {
		addr := nftables.TypeIPAddr
		if s.V6 {
			addr = nftables.TypeIP6Addr
		}
		set := &nftables.Set{
			Table:         table,
			Name:          s.Name,
			Dynamic:       true,
			Concatenation: true,
			Counter:       true,
			HasTimeout:    true,
			Timeout:       statTimeout,
			Size:          StatSetSize,
			KeyType:       nftables.MustConcatSetType(addr, nftables.TypeMark),
		}
		if err := conn.AddSet(set, nil); err != nil {
			return fmt.Errorf("creating set %s: %w", s.Name, err)
		}
	}

	policy := nftables.ChainPolicyAccept
	for _, c := range d.Stat.Chains {
		chain := conn.AddChain(&nftables.Chain{
			Name:    c.Name,
			Table:   table,
			Type:    nftables.ChainTypeFilter,
			Hooknum: hookNumber(c.Hook),
			// The same priority as everything else that filters, because this
			// counting is not a policy: no rule here can accept, drop or
			// redirect, and the chain's policy is accept. See §7.1 — an earlier
			// reading put these before the firewall so a dropped packet would
			// still be counted, which measures attempts rather than use.
			Priority: nftables.ChainPriorityFilter,
			Policy:   &policy,
		})
		for _, r := range c.Rules {
			conn.AddRule(&nftables.Rule{
				Table: table, Chain: chain,
				Exprs:    accountSetExprs(r.Set, r.DNAT),
				UserData: comment(r.Line()),
			})
		}
	}

	return conn.Flush()
}

// accountSetExprs keys each packet on the local device. After DNAT and
// before reverse SNAT, forwarded packet headers already contain that address.
//
// The same expression serves the local chains, where the header contains the
// client's address for exactly the same reason and the hook does the rest: a
// packet addressed to a socket here is keyed on what sent it, and one sent from
// a socket here is keyed on what asked for it.
func accountSetExprs(s StatSet, dnat bool) []expr.Any {
	proto := byte(unix.NFPROTO_IPV4)
	offset, length := uint32(12), uint32(4) // ip saddr
	dir := byte(ctDirOriginal)
	if s.Down {
		offset = 16 // ip daddr
		dir = ctDirReply
	}
	if dnat {
		if s.Down {
			offset, dir = 16, ctDirOriginal
		} else {
			offset, dir = 12, ctDirReply
		}
	}
	if s.V6 {
		proto, length = unix.NFPROTO_IPV6, 16
		if offset == 12 {
			offset = 8 // ip6 saddr
		} else {
			offset = 24 // ip6 daddr
		}
	}

	// The mark's component of the key sits immediately after the address, so a
	// 16-byte address pushes it from the second 32-bit register to the fifth.
	markReg := uint32(reg32_01)
	if s.V6 {
		markReg = reg32_04
	}

	status := uint32(0)
	if dnat {
		status = ctStatusDstNAT
	}

	return []expr.Any{
		// Register 1 here, deliberately: it aliases the key registers below,
		// and the comparison is finished before they are written. The same
		// applies to the direction check that follows it.
		&expr.Meta{Key: expr.MetaKeyNFPROTO, Register: 1},
		&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: []byte{proto}},

		// One byte: nft_ct_get_eval stores the direction with nft_reg_store8.
		&expr.Ct{Key: expr.CtKeyDIRECTION, Register: 1},
		&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: []byte{dir}},

		&expr.Ct{Register: 1, Key: expr.CtKeySTATUS},
		&expr.Bitwise{SourceRegister: 1, DestRegister: 1, Len: 4,
			Mask: markBytes(ctStatusDstNAT), Xor: markBytes(0)},
		&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: markBytes(status)},

		&expr.Payload{
			DestRegister: reg32_00,
			Base:         expr.PayloadBaseNetworkHeader,
			Offset:       offset,
			Len:          length,
		},

		&expr.Meta{Key: expr.MetaKeyMARK, Register: markReg},
		// Masked to our byte before it becomes part of the key. Without this a
		// mark bit set by Docker or WireGuard would split one client's traffic
		// across two elements that look unrelated (§3.2 — always match with the
		// mask).
		maskOurMarkBits(markReg),

		&expr.Dynset{
			SrcRegKey: reg32_00,
			SetName:   s.Name,
			Operation: unix.NFT_DYNSET_OP_UPDATE,
			Timeout:   statTimeout,
		},
	}
}

// Traffic reads the accounting sets.
//
// **This is the answer to docs/gateway.md §11 open decision 5** — whether
// google/nftables can read per-element stateful counters over netlink, rather
// than only set membership. It can, with one condition that is invisible from
// the API and load-bearing:
//
//	The kernel dumps a single stateful expression under NFTA_SET_ELEM_EXPR and
//	several under NFTA_SET_ELEM_EXPRESSIONS. google/nftables v0.3.0 *writes*
//	both but *decodes* only the first. Our elements carry exactly one — the
//	counter — so they come back on the path the library understands.
//
// The consequence to guard: adding a second stateful expression to one of these
// sets (a quota, say) moves the kernel to the plural attribute and this
// function silently starts returning zeroes. Not an error, not a parse failure
// — an empty counter. Anything added here has to be checked against that.
//
// Reads are non-destructive: there is no reset in the path, so the counters
// stay monotonic-since-load and callers can take deltas (§7.5).
func (k LinuxKernel) Traffic(_ context.Context) ([]Flow, error) {
	conn, err := nftables.New()
	if err != nil {
		return nil, err
	}
	defer conn.CloseLasting()

	tables, err := conn.ListTablesOfFamily(nftables.TableFamilyINet)
	if err != nil {
		return nil, err
	}
	var table *nftables.Table
	for _, t := range tables {
		if t.Name == StatTableName {
			table = t
			break
		}
	}
	if table == nil {
		// Accounting is off, or has never been applied. Not an error: the
		// caller's screen says "nothing is being counted" rather than "reading
		// failed", and those are different things.
		return nil, nil
	}

	sets, err := conn.GetSets(table)
	if err != nil {
		return nil, err
	}
	byName := map[string]*nftables.Set{}
	for _, s := range sets {
		byName[s.Name] = s
	}

	// Keyed by address and mark so the two directions of one device meet in a
	// single row.
	flows := map[string]*Flow{}
	for _, spec := range StatSets() {
		set, ok := byName[spec.Name]
		if !ok {
			continue
		}
		elements, err := conn.GetSetElements(set)
		if err != nil {
			return nil, fmt.Errorf("reading set %s: %w", spec.Name, err)
		}
		for _, e := range elements {
			addr, mark, ok := decodeStatKey(e.Key, spec)
			if !ok {
				continue
			}
			key := addr.String() + "/" + strconv.FormatUint(uint64(mark), 16)
			f, seen := flows[key]
			if !seen {
				f = &Flow{Addr: addr, Mark: mark}
				flows[key] = f
			}
			if e.Counter == nil {
				// The element exists but carries no counter. Left as zero
				// rather than dropped: the device is real and saying so with an
				// empty number is more honest than pretending it was not there.
				continue
			}
			if spec.Down {
				f.DownBytes, f.DownPackets = e.Counter.Bytes, e.Counter.Packets
			} else {
				f.UpBytes, f.UpPackets = e.Counter.Bytes, e.Counter.Packets
			}
		}
	}

	out := make([]Flow, 0, len(flows))
	for _, f := range flows {
		out = append(out, *f)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Addr != out[j].Addr {
			return out[i].Addr.Less(out[j].Addr)
		}
		return out[i].Mark < out[j].Mark
	})
	return out, nil
}

// decodeStatKey splits a concatenated `address . mark` element key.
//
// Each component is padded to four bytes, which is why a v4 key is eight bytes
// and not five. The mark is native-endian, as it is everywhere a register holds
// it; the address is wire order.
func decodeStatKey(key []byte, spec StatSet) (netip.Addr, uint32, bool) {
	width := 4
	if spec.V6 {
		width = 16
	}
	if len(key) < width+4 {
		return netip.Addr{}, 0, false
	}
	addr, ok := netip.AddrFromSlice(key[:width])
	if !ok {
		return netip.Addr{}, 0, false
	}
	mark := binaryutil.NativeEndian.Uint32(key[width : width+4])
	return addr.Unmap(), mark & MarkMask, true
}

// reservedTables are the kernel's own, which are never foreign and never ours.
func reservedTable(id int) bool {
	switch id {
	case unix.RT_TABLE_UNSPEC, unix.RT_TABLE_DEFAULT, unix.RT_TABLE_MAIN, unix.RT_TABLE_LOCAL:
		return true
	}
	return false
}

// observeRules reads the RPDB: ours as canonical lines, everybody else's as
// candidates for §6's refusal.
func (k LinuxKernel) observeRules() (lines []string, foreign []ForeignRule, err error) {
	defaults, err := k.tablesWithDefaultRoute()
	if err != nil {
		return nil, nil, err
	}

	for _, fam := range []int{netlink.FAMILY_V4, netlink.FAMILY_V6} {
		v6 := fam == netlink.FAMILY_V6
		rules, err := netlink.RuleList(fam)
		if err != nil {
			return nil, nil, err
		}
		for _, r := range rules {
			if dial.OwnsBrokerRule(r) {
				continue
			}
			if OwnsPriority(r.Priority) {
				lines = append(lines, observedRuleLine(r, v6))
				continue
			}
			if reservedTable(r.Table) || OwnsTable(r.Table) {
				continue
			}
			// §6, and the test is structural on purpose. The daemons that
			// install their own routing move their priority numbers between
			// versions, so a check that named one would pass on exactly the
			// release that broke it.
			// What makes a rule a conflict is not where it sits but what it
			// does: selecting a table that carries a default route is being a
			// second owner of "where does traffic go".
			if !defaults[r.Table] {
				continue
			}
			foreign = append(foreign, ForeignRule{
				Priority:   r.Priority,
				Family:     family(v6),
				Table:      r.Table,
				Selector:   strings.TrimSpace(r.String()),
				HasDefault: true,
			})
		}
	}
	return lines, foreign, nil
}

// observedRuleLine renders one of our own RPDB entries.
//
// Anything in our priority range that does not match a shape we install is
// rendered as unrecognised, so it diffs as a removal. That is deliberate: the
// range is documented as ours, and leaving a stray rule inside it would mean
// the kernel and the screen disagree about what is in force.
func observedRuleLine(r netlink.Rule, v6 bool) string {
	switch {
	case r.Priority == LocalPriority && r.SuppressPrefixlen == 0 && r.Table == unix.RT_TABLE_MAIN:
		return fmt.Sprintf("rule %s priority %d from all lookup main suppress_prefixlength 0",
			family(v6), r.Priority)
	case r.Mask != nil && *r.Mask == MarkMask:
		return fmt.Sprintf("rule %s priority %d fwmark %#08x/%#08x lookup %d",
			family(v6), r.Priority, r.Mark, MarkMask, r.Table)
	default:
		return fmt.Sprintf("rule %s priority %d unrecognised", family(v6), r.Priority)
	}
}

// tablesWithDefaultRoute reports which route tables carry a default route.
//
// One pass over every table in both families rather than a query per candidate:
// a box running Docker and a VPN can easily have a dozen, and the answer is the
// same shape either way.
func (k LinuxKernel) tablesWithDefaultRoute() (map[int]bool, error) {
	out := map[int]bool{}
	for _, fam := range []int{netlink.FAMILY_V4, netlink.FAMILY_V6} {
		routes, err := allRoutes(fam)
		if err != nil {
			return nil, err
		}
		for _, r := range routes {
			if isDefault(r) {
				out[r.Table] = true
			}
		}
	}
	return out, nil
}

// allRoutes lists routes across every table, which is `ip route show table
// all`. RT_TABLE_UNSPEC as the filter value is how that is spelled over
// netlink.
func allRoutes(fam int) ([]netlink.Route, error) {
	return netlink.RouteListFiltered(fam,
		&netlink.Route{Table: unix.RT_TABLE_UNSPEC}, netlink.RT_FILTER_TABLE)
}

func isDefault(r netlink.Route) bool {
	if r.Dst == nil {
		return true
	}
	ones, _ := r.Dst.Mask.Size()
	return ones == 0
}

// observeRoutes reads the default route in each of our tables.
func (k LinuxKernel) observeRoutes() ([]string, error) {
	var out []string
	for _, fam := range []int{netlink.FAMILY_V4, netlink.FAMILY_V6} {
		v6 := fam == netlink.FAMILY_V6
		routes, err := allRoutes(fam)
		if err != nil {
			return nil, err
		}
		for _, r := range routes {
			if !OwnsTable(r.Table) || !isDefault(r) {
				continue
			}
			out = append(out, observedRouteLine(r, v6))
		}
	}
	return out, nil
}

func observedRouteLine(r netlink.Route, v6 bool) string {
	// Linux assigns the IPv6 default route metric (1024) when no metric was
	// specified. Treat that read-back value as the omitted metric we rendered,
	// or an untouched route looks drifted after every apply.
	priority := r.Priority
	if v6 && priority == 1024 {
		priority = 0
	}
	if r.Type == unix.RTN_UNREACHABLE {
		line := fmt.Sprintf("route %s table %d unreachable default", family(v6), r.Table)
		if priority > 0 {
			line += fmt.Sprintf(" metric %d", priority)
		}
		return line
	}
	line := fmt.Sprintf("route %s table %d default", family(v6), r.Table)
	if r.Gw != nil {
		if a, ok := netip.AddrFromSlice(r.Gw); ok {
			line += " via " + a.Unmap().String()
		}
	}
	if r.LinkIndex > 0 {
		if link, err := netlink.LinkByIndex(r.LinkIndex); err == nil {
			line += " dev " + link.Attrs().Name
		}
	}
	if priority > 0 {
		line += fmt.Sprintf(" metric %d", priority)
	}
	return line
}

// observeActive reports which neighbours have recently exchanged traffic.
//
// This is what makes `disruptive` a fact rather than a guess (§5.3.3), and it
// is worth being precise about what it does and does not prove. REACHABLE,
// DELAY and PROBE mean the kernel has confirmed forward progress to that
// address recently; STALE is deliberately excluded, because it only means the
// entry existed once, and counting it would make every plan on a box with a
// long neighbour table claim it was about to disconnect somebody. Erring toward
// silence here is the right direction: a classification that cried wolf would
// train the operator to click through the one dialog that matters.
func (k LinuxKernel) observeActive() []netip.Addr {
	var out []netip.Addr
	for _, fam := range []int{netlink.FAMILY_V4, netlink.FAMILY_V6} {
		neighs, err := netlink.NeighList(0, fam)
		if err != nil {
			continue
		}
		for _, n := range neighs {
			if n.State&(netlink.NUD_REACHABLE|netlink.NUD_DELAY|netlink.NUD_PROBE) == 0 {
				continue
			}
			if a, ok := netip.AddrFromSlice(n.IP); ok {
				out = append(out, a.Unmap())
			}
		}
	}
	return out
}

// --- sysctls --------------------------------------------------------------

// sysctlPath turns a dotted key into its /proc/sys path.
func (k LinuxKernel) sysctlPath(key string) string {
	return k.ProcRoot + "/proc/sys/" + strings.ReplaceAll(key, ".", "/")
}

func (k LinuxKernel) readSysctl(key string) (string, bool) {
	data, err := os.ReadFile(k.sysctlPath(key))
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(string(data)), true
}

// readSysctls reads the settings this module owns: the machine-wide forwarding
// switch, and the per-interface redirect settings for every interface that has
// them.
//
// Read broadly, write narrowly (design.md §3.4): we look at every interface so
// the plan can say something true about the ones we are about to touch, and we
// only ever write to the ones an exit actually uses.
//
// Forwarding is read here rather than trusted from the last apply, and that is
// what makes it drift rather than a one-shot. Docker, libvirt and k8s all write
// the global key on startup, and the kernel's write path resets every
// per-client value with it — so a box olr left forwarding can stop forwarding
// because something else was installed, and this is the read that notices.
func (k LinuxKernel) readSysctls() map[string]string {
	out := map[string]string{}
	for _, key := range []string{ForwardingSysctl, IPv6ForwardingSysctl, "net.netfilter.nf_conntrack_acct"} {
		if v, ok := k.readSysctl(key); ok {
			out[key] = v
		}
	}
	links, err := netlink.LinkList()
	if err != nil {
		return out
	}
	for _, l := range links {
		name := l.Attrs().Name
		for _, key := range []string{
			"net.ipv4.conf." + name + ".send_redirects",
			"net.ipv4.conf." + name + ".accept_redirects",
			"net.ipv6.conf." + name + ".accept_redirects",
		} {
			if v, ok := k.readSysctl(key); ok {
				out[key] = v
			}
		}
	}
	return out
}

// observeRAHosts finds the interfaces that would lose their IPv6 default route
// the moment all.forwarding goes to 1 (Observed.RAHosts).
//
// Keyed on the route rather than on the address, because the route is what
// the kernel purges: rt6_purge_dflt_routers drops every default route whose
// protocol is `ra`, except on an interface whose accept_ra is 2. A SLAAC
// address survives the switch; the path out does not.
func (k LinuxKernel) observeRAHosts() []string {
	routes, err := netlink.RouteListFiltered(netlink.FAMILY_V6,
		&netlink.Route{Table: unix.RT_TABLE_MAIN}, netlink.RT_FILTER_TABLE)
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	for _, r := range routes {
		if r.Protocol != unix.RTPROT_RA || !isDefault(r) {
			continue
		}
		link, err := netlink.LinkByIndex(r.LinkIndex)
		if err != nil {
			continue
		}
		name := link.Attrs().Name
		if v, ok := k.readSysctl("net.ipv6.conf." + name + ".accept_ra"); ok && v == "2" {
			continue
		}
		seen[name] = true
	}
	out := make([]string, 0, len(seen))
	for name := range seen {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func (k LinuxKernel) writeSysctl(key, value string) error {
	return os.WriteFile(k.sysctlPath(key), []byte(value+"\n"), 0o644)
}

// --- apply ----------------------------------------------------------------

// Apply programs the desired state.
//
// The order is ApplyOrder's, and every intermediate state it passes through is
// one where traffic is either handled correctly or not classified at all:
//
//	add routes  →  add rules  →  replace nftables  →  drop stale rules  →  drop stale routes
//
// Additions go bottom-up so a mark never names a table that has no route yet;
// removals go top-down so a route is never withdrawn while a rule still points
// at it. Get either backwards and there is a window of milliseconds in which
// the RPDB falls through to `main` and the traffic the operator asked to route
// leaves direct — silently, which is the combination this module works hardest
// to avoid.
func (k LinuxKernel) Apply(ctx context.Context, d Desired) ([]Step, error) {
	var steps []Step

	run := func(desc string, fn func() error) error {
		if err := fn(); err != nil {
			steps = append(steps, Step{Description: desc, Error: err.Error()})
			return err
		}
		steps = append(steps, Step{Description: desc, Done: true})
		return nil
	}

	wantRoutes, err := desiredRoutes(d)
	if err != nil {
		return []Step{{Description: "prepare routes", Error: err.Error()}}, err
	}
	haveRoutes, err := k.currentRoutes()
	if err != nil {
		return []Step{{Description: "read current routes", Error: err.Error()}}, err
	}
	wantRules := desiredRules(d)
	haveRules, err := k.currentRules()
	if err != nil {
		return []Step{{Description: "read current ip rules", Error: err.Error()}}, err
	}

	addRoutes, delRoutes := diffRoutes(haveRoutes, wantRoutes)
	addRules, delRules := diffRules(haveRules, wantRules)

	if len(addRoutes) > 0 {
		if err := run(fmt.Sprintf("add %s", plural(len(addRoutes), "route")), func() error {
			return applyRoutes(addRoutes, netlink.RouteReplace)
		}); err != nil {
			return steps, err
		}
	}

	if len(addRules) > 0 {
		if err := run(fmt.Sprintf("add %s", plural(len(addRules), "ip rule")), func() error {
			return applyRules(addRules, netlink.RuleAdd)
		}); err != nil {
			return steps, err
		}
	}

	// One transaction. design.md §5.2 leans on exactly this: an nftables load
	// is a single all-or-nothing netlink batch, so no partial ruleset is ever
	// visible and there is nothing for a userspace two-phase commit to add.
	if err := run("write the nftables table", func() error { return k.applyNFT(d) }); err != nil {
		return steps, err
	}

	// Its own step and its own transaction, because it has its own lifetime
	// (§7.1) — and because a failure here must not stop the routing half from
	// being reported as landed.
	if err := run("write the accounting table", func() error { return k.applyStat(d) }); err != nil {
		return steps, err
	}

	if len(delRules) > 0 {
		if err := run(fmt.Sprintf("remove %s", plural(len(delRules), "stale ip rule")), func() error {
			return applyRules(delRules, netlink.RuleDel)
		}); err != nil {
			return steps, err
		}
	}

	if len(delRoutes) > 0 {
		if err := run(fmt.Sprintf("remove %s", plural(len(delRoutes), "stale route")), func() error {
			return applyRoutes(delRoutes, netlink.RouteDel)
		}); err != nil {
			return steps, err
		}
	}

	for _, s := range d.Sysctls {
		if v, ok := k.readSysctl(s.Key); ok && v == s.Value {
			continue
		}
		if err := run("set "+s.Key+" to "+s.Value, func() error {
			return k.writeSysctl(s.Key, s.Value)
		}); err != nil {
			return steps, err
		}
	}

	_ = ctx
	return steps, nil
}

func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return strconv.Itoa(n) + " " + noun + "s"
}

// --- routes ---------------------------------------------------------------

// desiredRoutes turns RouteSpecs into netlink routes, keyed by canonical line
// so that comparison is a string comparison and cannot disagree with the plan.
func desiredRoutes(d Desired) (map[string]*netlink.Route, error) {
	out := map[string]*netlink.Route{}
	for _, r := range d.Routes {
		route := &netlink.Route{
			Table:    r.Table,
			Protocol: unix.RTPROT_STATIC,
			Priority: r.Priority,
		}
		if r.V6 {
			route.Family = netlink.FAMILY_V6
			route.Dst = &net.IPNet{IP: net.IPv6zero, Mask: net.CIDRMask(0, 128)}
		} else {
			route.Family = netlink.FAMILY_V4
			route.Dst = &net.IPNet{IP: net.IPv4zero, Mask: net.CIDRMask(0, 32)}
		}

		switch r.Type {
		case RouteUnreachable:
			// unreachable, never blackhole (§3.7). Blackhole drops silently and
			// every connection hangs for thirty seconds; unreachable sends ICMP
			// admin-prohibited and applications fail at once. Same feature,
			// completely different experience for whoever is holding the tablet.
			route.Type = unix.RTN_UNREACHABLE
		default:
			route.Type = unix.RTN_UNICAST
			if r.Gateway != nil {
				route.Gw = net.IP(r.Gateway.AsSlice())
			}
			if r.Dev != "" {
				link, err := netlink.LinkByName(r.Dev)
				if err != nil {
					return nil, fmt.Errorf("exit %q names interface %q, which is not present: %w",
						r.Exit, r.Dev, err)
				}
				route.LinkIndex = link.Attrs().Index
			}
		}
		// Keyed by the same canonical line the plan showed the operator, so
		// "what the plan said" and "what gets programmed" cannot come apart.
		out[r.Line()] = route
	}
	return out, nil
}

func (k LinuxKernel) currentRoutes() (map[string]*netlink.Route, error) {
	out := map[string]*netlink.Route{}
	for _, fam := range []int{netlink.FAMILY_V4, netlink.FAMILY_V6} {
		routes, err := allRoutes(fam)
		if err != nil {
			return nil, err
		}
		for i, r := range routes {
			if !OwnsTable(r.Table) || !isDefault(r) {
				continue
			}
			out[observedRouteLine(r, fam == netlink.FAMILY_V6)] = &routes[i]
		}
	}
	return out, nil
}

func diffRoutes(have, want map[string]*netlink.Route) (add, del []*netlink.Route) {
	for line, r := range want {
		if _, ok := have[line]; !ok {
			add = append(add, r)
		}
	}
	for line, r := range have {
		if _, ok := want[line]; !ok {
			del = append(del, r)
		}
	}
	return add, del
}

func applyRoutes(routes []*netlink.Route, fn func(*netlink.Route) error) error {
	for _, r := range routes {
		if err := fn(r); err != nil {
			return fmt.Errorf("table %d: %w", r.Table, err)
		}
	}
	return nil
}

// --- ip rules -------------------------------------------------------------

func desiredRules(d Desired) map[string]*netlink.Rule {
	out := map[string]*netlink.Rule{}
	for _, r := range d.Rules {
		rule := netlink.NewRule()
		rule.Priority = r.Priority
		if r.V6 {
			rule.Family = netlink.FAMILY_V6
		} else {
			rule.Family = netlink.FAMILY_V4
		}

		switch r.Kind {
		case RuleLocal:
			rule.Table = unix.RT_TABLE_MAIN
			rule.SuppressPrefixlen = 0
		case RuleMark:
			rule.Table = r.Table
			rule.Mark = r.Mark
			mask := MarkMask
			// Always matched *with* the mask, never bare (§3.2). A rule
			// matching the whole fwmark would also match on Docker's or
			// WireGuard's bits, and would stop matching the moment one of them
			// set a bit of its own.
			rule.Mask = &mask
		}
		out[r.Line()] = rule
	}
	return out
}

func (k LinuxKernel) currentRules() (map[string]*netlink.Rule, error) {
	out := map[string]*netlink.Rule{}
	for _, fam := range []int{netlink.FAMILY_V4, netlink.FAMILY_V6} {
		rules, err := netlink.RuleList(fam)
		if err != nil {
			return nil, err
		}
		for i, r := range rules {
			if !OwnsPriority(r.Priority) {
				continue
			}
			rule := rules[i]
			// RuleList does not set Family on the way out, and RuleDel needs
			// it: without this every removal would be attempted against IPv4
			// and the IPv6 rules would survive forever.
			rule.Family = fam
			out[observedRuleLine(r, fam == netlink.FAMILY_V6)] = &rule
		}
	}
	return out, nil
}

func diffRules(have, want map[string]*netlink.Rule) (add, del []*netlink.Rule) {
	for line, r := range want {
		if _, ok := have[line]; !ok {
			add = append(add, r)
		}
	}
	for line, r := range have {
		if _, ok := want[line]; !ok {
			del = append(del, r)
		}
	}
	return add, del
}

func applyRules(rules []*netlink.Rule, fn func(*netlink.Rule) error) error {
	for _, r := range rules {
		if err := fn(r); err != nil {
			return fmt.Errorf("priority %d: %w", r.Priority, err)
		}
	}
	return nil
}

// --- nftables -------------------------------------------------------------

// applyNFT replaces our table wholesale, in one transaction.
//
// Delete-then-recreate rather than a rule-by-rule reconciliation, and the
// reason is that the batch is atomic: no half-built ruleset is ever visible, so
// there is no ordering problem to solve inside it and no chance of a state
// where some flows are classified by the old rules and some by the new. It also
// means a hand-edit inside our table is corrected on every apply, which is what
// bounds the observation gap documented on observeNFT.
//
// It never touches anything else. `nft flush ruleset` is banned outright
// (design.md §3.4) — it silently kills Docker, podman, libvirt and k8s
// networking — so this deletes and recreates one table, by name.
func (k LinuxKernel) applyNFT(d Desired) error {
	conn, err := nftables.New()
	if err != nil {
		return err
	}
	defer conn.CloseLasting()

	tables, err := conn.ListTablesOfFamily(nftables.TableFamilyINet)
	if err != nil {
		return err
	}
	for _, t := range tables {
		if t.Name == TableName {
			conn.DelTable(t)
			break
		}
	}

	if !d.Enabled {
		return conn.Flush()
	}

	table := conn.AddTable(&nftables.Table{
		Family: nftables.TableFamilyINet,
		Name:   TableName,
	})

	for _, name := range d.Table.Counters {
		conn.AddObject(&nftables.CounterObj{Table: table, Name: name})
	}

	policy := nftables.ChainPolicyAccept
	classify := conn.AddChain(&nftables.Chain{
		Name:     ClassifyChain,
		Table:    table,
		Type:     nftables.ChainTypeFilter,
		Hooknum:  nftables.ChainHookPrerouting,
		Priority: nftables.ChainPriorityMangle,
		Policy:   &policy,
	})

	// Rules are added in evaluation order, which is the order Desired.Lines
	// prints them in — so following a packet down `nft list ruleset` and
	// following it down `olr diff` are the same exercise.
	for _, e := range d.Table.Exits {
		conn.AddRule(&nftables.Rule{
			Table: table, Chain: classify,
			Exprs:    restoreExprs(e),
			UserData: comment(e.RestoreLine()),
		})
	}
	for _, r := range d.Table.Destinations {
		conn.AddRule(&nftables.Rule{Table: table, Chain: classify,
			Exprs: destinationExprs(r), UserData: comment(r.Line())})
	}
	for _, r := range d.Table.Clients {
		conn.AddRule(&nftables.Rule{
			Table: table, Chain: classify,
			Exprs:    deviceExprs(r),
			UserData: comment(r.Line()),
		})
	}
	for _, s := range d.Table.Sources {
		conn.AddRule(&nftables.Rule{
			Table: table, Chain: classify,
			Exprs:    sourceExprs(s),
			UserData: comment(s.Line()),
		})
	}
	for _, e := range d.Table.Exits {
		conn.AddRule(&nftables.Rule{
			Table: table, Chain: classify,
			Exprs:    accountExprs(e),
			UserData: comment(e.AccountLine()),
		})
	}
	conn.AddRule(&nftables.Rule{
		Table: table, Chain: classify,
		Exprs:    unpolicedExprs(),
		UserData: comment("nft count-unpoliced"),
	})

	if len(d.Table.SNAT) > 0 {
		natPolicy := nftables.ChainPolicyAccept
		post := conn.AddChain(&nftables.Chain{
			Name:     PostroutingChain,
			Table:    table,
			Type:     nftables.ChainTypeNAT,
			Hooknum:  nftables.ChainHookPostrouting,
			Priority: nftables.ChainPriorityNATSource,
			Policy:   &natPolicy,
		})
		for _, s := range d.Table.SNAT {
			conn.AddRule(&nftables.Rule{
				Table: table, Chain: post,
				Exprs:    snatExprs(s),
				UserData: comment(s.Line()),
			})
		}
	}

	return conn.Flush()
}

// deviceExprs matches the Ethernet source MAC before network classification.
// Direct has a reserved mark but no route rule: it falls through to main, and
// conntrack preserves the choice when an assignment changes mid-connection.
func deviceExprs(r ClientRule) []expr.Any {
	mac, _ := net.ParseMAC(r.MAC) // validated before rendering
	v6 := r.Prefix.Addr().Is6()
	proto := byte(unix.NFPROTO_IPV4)
	offset, length := uint32(12), uint32(4)
	if v6 {
		proto = unix.NFPROTO_IPV6
		offset, length = 8, 16
	}
	addr := r.Prefix.Masked().Addr().AsSlice()
	mask := net.CIDRMask(r.Prefix.Bits(), len(addr)*8)
	out := []expr.Any{
		&expr.Meta{Key: expr.MetaKeyMARK, Register: 1},
		maskOurMarkBits(1),
		&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: markBytes(0)},
		&expr.Ct{Register: 1, Key: expr.CtKeySTATUS},
		&expr.Bitwise{SourceRegister: 1, DestRegister: 1, Len: 4,
			Mask: markBytes(ctStatusDstNAT), Xor: markBytes(0)},
		&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: markBytes(0)},
		&expr.Meta{Key: expr.MetaKeyNFPROTO, Register: 1},
		&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: []byte{proto}},
		&expr.Fib{Register: 1, ResultADDRTYPE: true, FlagDADDR: true},
		&expr.Cmp{Op: expr.CmpOpNeq, Register: 1,
			Data: binaryutil.NativeEndian.PutUint32(unix.RTN_LOCAL)},
		&expr.Meta{Key: expr.MetaKeyIIFTYPE, Register: 1},
		&expr.Cmp{Op: expr.CmpOpEq, Register: 1,
			Data: binaryutil.NativeEndian.PutUint16(unix.ARPHRD_ETHER)},
		&expr.Meta{Key: expr.MetaKeyIIFNAME, Register: 1},
		&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: nulTerminated(r.Interface)},
		&expr.Payload{DestRegister: 1, Base: expr.PayloadBaseNetworkHeader, Offset: offset, Len: length},
		&expr.Bitwise{SourceRegister: 1, DestRegister: 1, Len: length,
			Mask: mask, Xor: make([]byte, length)},
		&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: addr},
		&expr.Payload{DestRegister: 1, Base: expr.PayloadBaseLLHeader, Offset: 6, Len: 6},
		&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: mac},
	}
	return append(out,
		&expr.Meta{Key: expr.MetaKeyMARK, Register: 1},
		setOurMarkBits(1, r.Mark),
		&expr.Meta{Key: expr.MetaKeyMARK, SourceRegister: true, Register: 1},
	)
}

// comment stores a line in nft's own comment format, so `nft list ruleset`
// prints it rather than showing an opaque blob.
func comment(s string) []byte {
	return userdata.AppendString(nil, userdata.TypeComment, s)
}

// markBytes and maskBytes are native-endian, because that is how the kernel
// holds a mark in a register. Addresses are the opposite — they are compared
// against wire bytes — which is the one endianness trap in this file.
func markBytes(v uint32) []byte { return binaryutil.NativeEndian.PutUint32(v) }

// setOurMarkBits builds `dst = (src & ~MarkMask) ^ value`.
//
// nftables' bitwise expression combines a register with constants, not with
// another register, so this is the only shape available — and it happens to be
// exactly the discipline §3.2 asks for. Clearing our byte and then XOR-ing a
// value that lives entirely inside it is an OR in every case that matters, and
// every bit outside the mask passes through untouched.
func setOurMarkBits(reg uint32, value uint32) *expr.Bitwise {
	return &expr.Bitwise{
		SourceRegister: reg,
		DestRegister:   reg,
		Len:            4,
		Mask:           markBytes(^MarkMask),
		Xor:            markBytes(value),
	}
}

// maskOurMarkBits builds `dst = src & MarkMask`, for the comparisons.
func maskOurMarkBits(reg uint32) *expr.Bitwise {
	return &expr.Bitwise{
		SourceRegister: reg,
		DestRegister:   reg,
		Len:            4,
		Mask:           markBytes(MarkMask),
		Xor:            markBytes(0),
	}
}

// restoreExprs: `ct mark & MarkMask == exit  →  meta mark set (mark & ~MarkMask) ^ exit`
//
// The first half of §3.4's flow stickiness. An established connection gets back
// the exit it started on, so editing an assignment does not move traffic that
// is already flowing — which is what keeps such an edit `reload` rather than
// `disruptive`.
func restoreExprs(e ExitRule) []expr.Any {
	return []expr.Any{
		&expr.Ct{Register: 1, Key: expr.CtKeyMARK},
		maskOurMarkBits(1),
		&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: markBytes(e.Mark)},
		&expr.Meta{Key: expr.MetaKeyMARK, Register: 1},
		setOurMarkBits(1, e.Mark),
		&expr.Meta{Key: expr.MetaKeyMARK, SourceRegister: true, Register: 1},
	}
}

// ctStatusDstNAT is IPS_DST_NAT from nf_conntrack_common.h — bit 5, set on a
// conntrack entry whose destination has been rewritten.
//
// Spelled out here because x/sys/unix does not export that header's flags, and
// they are stable kernel ABI.
const ctStatusDstNAT uint32 = 0x20

// sourceExprs classifies a new flow by where it came from.
//
// Four guards, each earning its place:
//
//   - `meta mark & MarkMask == 0` skips anything the restore rules already
//     decided, so a running connection is never reclassified mid-flight.
//   - `ct status & IPS_DST_NAT == 0` skips the reply leg of a connection
//     somebody port-forwarded (§3.5). Without it, port forwarding silently does
//     not work on any box that also has an exit: conntrack does not restore the
//     original source address until postrouting, so in prerouting the server's
//     reply looks exactly like an ordinary LAN machine opening an outbound
//     connection, and the policy route sends it into the proxy. The connection
//     dies half-open. See docs/port-forwarding.md §6 for the full trace.
//   - `fib daddr type != local` is what makes this **forward-only** (§3.5).
//     Traffic addressed to the router itself is not forwarded, so classifying it
//     would send the box's own replies out an exit. It is also, incidentally,
//     why a proxy running on this box cannot loop: its upstream connections are
//     locally generated and never meet this chain at all.
//   - the family check is required because this is an `inet` table, where an
//     IPv4 payload offset applied to an IPv6 packet reads whatever happens to be
//     at byte 12 of the header.
func sourceExprs(s SourceRule, matches ...expr.Any) []expr.Any {
	v6 := s.Prefix.Addr().Is6()
	proto := byte(unix.NFPROTO_IPV4)
	offset, length := uint32(12), uint32(4)
	if v6 {
		proto = unix.NFPROTO_IPV6
		offset, length = 8, 16
	}

	addr := s.Prefix.Masked().Addr().AsSlice()
	mask := net.CIDRMask(s.Prefix.Bits(), len(addr)*8)

	out := []expr.Any{
		&expr.Meta{Key: expr.MetaKeyMARK, Register: 1},
		maskOurMarkBits(1),
		&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: markBytes(0)},

		// The DNAT guard. Semantically general rather than a back door for
		// the NAT half: a connection whose destination we rewrote belongs
		// to whoever originated the translation, and its path is decided by the
		// conntrack entry rather than by our source rules.
		//
		// The declared cost is that traffic DNATed by *somebody else's* table —
		// a Docker published port, a hand-written rule — also loses its exit
		// assignment. That is the correct direction to be wrong in: an
		// unassigned connection takes the box's normal path and works, while the
		// alternative breaks it.
		&expr.Ct{Register: 1, Key: expr.CtKeySTATUS},
		&expr.Bitwise{SourceRegister: 1, DestRegister: 1, Len: 4,
			Mask: markBytes(ctStatusDstNAT), Xor: markBytes(0)},
		&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: markBytes(0)},

		&expr.Meta{Key: expr.MetaKeyNFPROTO, Register: 1},
		&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: []byte{proto}},

		&expr.Fib{Register: 1, ResultADDRTYPE: true, FlagDADDR: true},
		&expr.Cmp{Op: expr.CmpOpNeq, Register: 1,
			Data: binaryutil.NativeEndian.PutUint32(unix.RTN_LOCAL)},

		&expr.Payload{
			DestRegister: 1,
			Base:         expr.PayloadBaseNetworkHeader,
			Offset:       offset,
			Len:          length,
		},
		&expr.Bitwise{SourceRegister: 1, DestRegister: 1, Len: length,
			Mask: mask, Xor: make([]byte, length)},
		&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: addr},
	}
	out = append(out, matches...)
	return append(out,
		&expr.Meta{Key: expr.MetaKeyMARK, Register: 1},
		setOurMarkBits(1, s.Mark),
		&expr.Meta{Key: expr.MetaKeyMARK, SourceRegister: true, Register: 1},
	)
}

// accountExprs counts a packet against its exit and records the decision on the
// connection.
//
// Counting here rather than in the classify rule is what makes the number
// mean bytes rather than connections: the classify rule sees only the first
// packet of a flow, because the restore rule handles every one after it.
//
// The counter is a **named object**, not an anonymous inline counter, and §3.3
// is emphatic about the difference: re-rendering the chain zeroes an anonymous
// counter, so adding an exit would silently reset every other exit's totals.
func accountExprs(e ExitRule) []expr.Any {
	return []expr.Any{
		&expr.Meta{Key: expr.MetaKeyMARK, Register: 1},
		maskOurMarkBits(1),
		&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: markBytes(e.Mark)},
		&expr.Objref{Type: unix.NFT_OBJECT_COUNTER, Name: e.Counter},
		&expr.Ct{Register: 1, Key: expr.CtKeyMARK},
		setOurMarkBits(1, e.Mark),
		&expr.Ct{Register: 1, SourceRegister: true, Key: expr.CtKeyMARK},
	}
}

// unpolicedExprs counts what matched nothing.
//
// §7.3's residual rule: show what you cannot account for. It is what makes
// per-exit totals reconcile against the box total, and a number with a stated
// boundary beats a number nobody can trust.
func unpolicedExprs() []expr.Any {
	return []expr.Any{
		&expr.Meta{Key: expr.MetaKeyMARK, Register: 1},
		maskOurMarkBits(1),
		&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: markBytes(0)},
		&expr.Objref{Type: unix.NFT_OBJECT_COUNTER, Name: UnpolicedCounter},
	}
}

// snatExprs rewrites the source address of traffic bound for a next hop (§5.3).
func snatExprs(s SNATRule) []expr.Any {
	out := []expr.Any{
		&expr.Meta{Key: expr.MetaKeyMARK, Register: 1},
		maskOurMarkBits(1),
		&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: markBytes(s.Mark)},
	}
	if s.Dev != "" {
		out = append(out,
			&expr.Meta{Key: expr.MetaKeyOIFNAME, Register: 1},
			&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: nulTerminated(s.Dev)},
		)
	}
	return append(out, &expr.Masq{})
}

// nulTerminated is how the kernel compares an interface name: a fixed 16-byte
// buffer, NUL-padded. Comparing the bare string matches nothing.
func nulTerminated(s string) []byte {
	b := make([]byte, unix.IFNAMSIZ)
	copy(b, s)
	return b
}

//go:build linux

package firewall

import (
	"context"
	"fmt"

	"github.com/google/nftables"
	"github.com/google/nftables/binaryutil"
	"github.com/google/nftables/expr"
	"github.com/google/nftables/userdata"
	"golang.org/x/sys/unix"
)

// The kernel half, and the only file in this module that knows what netlink
// is. Like internal/gateway/nat it programs nftables in-process over
// google/nftables for automatic defaults. Native JSON tables use nft itself
// (native.go), preserving expressions without a second compiler.

// LinuxKernel programs the filter table.
type LinuxKernel struct{}

// NewKernel returns the kernel implementation for this platform.
func NewKernel() Kernel { return LinuxKernel{} }

// Observe reads our table back as the lines Desired.Lines produces.
//
// Each rule carries its line as an nft comment, so this is a read rather than
// a reconstruction, with the gap internal/gateway/nat's observeTable documents:
// a rule whose expressions were hand-edited under an intact comment is not
// reported. Every apply replaces the table, so such an edit is corrected even
// while it goes unseen.
func (LinuxKernel) Observe(context.Context) (Observed, error) {
	obs := Observed{Known: true, Blocked: map[string]uint64{}, Traffic: map[string]TrafficCount{}}

	conn, err := nftables.New()
	if err != nil {
		return Observed{}, fmt.Errorf("reading nftables: %w", err)
	}
	defer conn.CloseLasting()

	table, err := ourTable(conn)
	if err != nil {
		return Observed{}, fmt.Errorf("reading nftables: %w", err)
	}
	if table == nil {
		return obs, nil
	}
	obs.Lines = append(obs.Lines, "nft table inet "+TableName)

	objs, err := conn.GetObjects(table)
	if err != nil {
		return Observed{}, fmt.Errorf("reading nftables counters: %w", err)
	}
	for _, o := range objs {
		if c, ok := o.(*nftables.CounterObj); ok {
			obs.Lines = append(obs.Lines, "nft counter "+c.Name)
			obs.Blocked[c.Name] = c.Packets
		}
	}

	chains, err := conn.ListChainsOfTableFamily(nftables.TableFamilyINet)
	if err != nil {
		return Observed{}, fmt.Errorf("reading nftables chains: %w", err)
	}
	for _, c := range chains {
		if c.Table == nil || c.Table.Name != TableName {
			continue
		}
		obs.Lines = append(obs.Lines, "nft chain "+c.Name+" filter")
		rules, err := conn.GetRules(table, c)
		if err != nil {
			return Observed{}, fmt.Errorf("reading nftables rules: %w", err)
		}
		for _, r := range rules {
			line, ok := userdata.GetString(r.UserData, userdata.TypeComment)
			if !ok {
				// A rule in our table that we did not write. Reported so the
				// plan removes it, rather than tolerated in a table we own.
				line = "nft unrecognised rule in " + c.Name
			}
			obs.Lines = append(obs.Lines, line)
			for _, e := range r.Exprs {
				if count, ok := e.(*expr.Counter); ok {
					obs.Traffic[line] = TrafficCount{Packets: count.Packets, Bytes: count.Bytes}
				}
			}
		}
	}
	return obs, nil
}

func ourTable(conn *nftables.Conn) (*nftables.Table, error) {
	tables, err := conn.ListTablesOfFamily(nftables.TableFamilyINet)
	if err != nil {
		return nil, err
	}
	for _, t := range tables {
		if t.Name == TableName {
			return t, nil
		}
	}
	return nil, nil
}

// Apply replaces our table wholesale in one transaction: delete, then
// recreate, in a single batch the kernel applies atomically. No window exists
// in which the box is half-filtered, and nothing outside our table is touched
// (design.md §3.4 bans `nft flush ruleset`).
func (LinuxKernel) Apply(ctx context.Context, d Desired) error {
	if len(d.Ruleset) > 0 {
		return applyNative(ctx, d)
	}
	conn, err := nftables.New()
	if err != nil {
		return err
	}
	defer conn.CloseLasting()

	existing, err := ourTable(conn)
	if err != nil {
		return err
	}
	if existing != nil {
		conn.DelTable(existing)
	}
	if !d.Enabled {
		return conn.Flush()
	}

	table := conn.AddTable(&nftables.Table{Family: nftables.TableFamilyINet, Name: TableName})
	conn.AddObject(&nftables.CounterObj{Table: table, Name: InputCounter})
	conn.AddObject(&nftables.CounterObj{Table: table, Name: ForwardCounter})

	for _, spec := range []struct {
		name  string
		hook  *nftables.ChainHook
		rules []Rule
	}{
		{InputChain, nftables.ChainHookInput, d.Input},
		{ForwardChain, nftables.ChainHookForward, d.Forward},
	} {
		// `accept` policy; the final rule is the drop. See render.go for why
		// it is a counted rule and not the policy.
		policy := nftables.ChainPolicyAccept
		chain := conn.AddChain(&nftables.Chain{
			Name:     spec.name,
			Table:    table,
			Type:     nftables.ChainTypeFilter,
			Hooknum:  spec.hook,
			Priority: nftables.ChainPriorityFilter,
			Policy:   &policy,
		})
		for _, r := range spec.rules {
			exprs, err := ruleExprs(conn, table, d, r)
			if err != nil {
				return fmt.Errorf("building %q: %w", r.Line, err)
			}
			conn.AddRule(&nftables.Rule{
				Table: table, Chain: chain,
				Exprs:    exprs,
				UserData: userdata.AppendString(nil, userdata.TypeComment, r.Line),
			})
		}
	}
	return conn.Flush()
}

var (
	accept = &expr.Verdict{Kind: expr.VerdictAccept}
	drop   = &expr.Verdict{Kind: expr.VerdictDrop}
)

// ICMP types let in. v4: echo request and the three errors a host must see.
// v6 to this router: RFC 4890 §4.4's host set — errors, echo, MLD, and
// neighbour and router discovery. v6 through it: errors and echo (§4.3).
var (
	icmpTypes         = []byte{3, 8, 11, 12}
	icmpForwardTypes  = []byte{3, 8, 11, 12}
	icmpv6InputTypes  = []byte{1, 2, 3, 4, 128, 130, 131, 132, 134, 135, 136, 143}
	icmpv6ForwardType = []byte{1, 2, 3, 4, 128}
)

// tcpOptionMaxseg is the TCP option kind for the maximum segment size, from
// the IANA TCP option registry. nftables spells it `tcp option maxseg`.
const tcpOptionMaxseg = 2

// ipsDstNAT is IPS_DST_NAT from linux/netfilter/nf_conntrack_common.h: the
// connection was destination-translated, which in this box means a port
// forward matched it.
const ipsDstNAT = 1 << 5

func ruleExprs(conn *nftables.Conn, table *nftables.Table, d Desired, r Rule) ([]expr.Any, error) {
	expressions, err := baseRuleExprs(conn, table, d, r)
	if err != nil {
		return nil, err
	}
	if r.Kind == RuleMSSClamp {
		// A rewrite, not a decision: it ends in the exthdr write, has no
		// traffic source to partition, and must not be counted or given a
		// verdict that would stop the chain.
		return expressions, nil
	}
	conditions, err := sourceExprs(conn, table, d.Inside, r.Source)
	if err != nil {
		return nil, err
	}
	// Every other rule ends with its verdict. Count only after all of its
	// conditions matched and before that terminal verdict, exactly once.
	conditions = append(conditions, expressions[:len(expressions)-1]...)
	conditions = append(conditions, &expr.Counter{}, expressions[len(expressions)-1])
	return conditions, nil
}

func sourceExprs(conn *nftables.Conn, table *nftables.Table, inside []string, source TrafficSource) ([]expr.Any, error) {
	if source == SourceLocal {
		return []expr.Any{
			&expr.Meta{Key: expr.MetaKeyIIFNAME, Register: 1},
			&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: ifname("lo")},
		}, nil
	}
	if source != SourceInside && source != SourceOutside {
		return nil, fmt.Errorf("unknown traffic source %q", source)
	}
	conditions := []expr.Any{
		&expr.Meta{Key: expr.MetaKeyIIFNAME, Register: 1},
		&expr.Cmp{Op: expr.CmpOpNeq, Register: 1, Data: ifname("lo")},
	}
	if len(inside) > 0 {
		set, err := addSet(conn, table, nftables.TypeIFName, ifnameKeys(inside))
		if err != nil {
			return nil, err
		}
		conditions = append(conditions, &expr.Lookup{SourceRegister: 1, SetID: set.ID, SetName: set.Name, Invert: source == SourceOutside})
	} else if source == SourceInside {
		return nil, fmt.Errorf("inside traffic needs a trusted interface")
	}
	return conditions, nil
}

func baseRuleExprs(conn *nftables.Conn, table *nftables.Table, d Desired, r Rule) ([]expr.Any, error) {
	switch r.Kind {
	case RuleEstablished:
		return []expr.Any{
			&expr.Ct{Register: 1, Key: expr.CtKeySTATE},
			&expr.Bitwise{SourceRegister: 1, DestRegister: 1, Len: 4,
				Mask: native32(expr.CtStateBitESTABLISHED | expr.CtStateBitRELATED),
				Xor:  native32(0)},
			&expr.Cmp{Op: expr.CmpOpNeq, Register: 1, Data: native32(0)},
			accept,
		}, nil

	case RuleLoopback:
		return []expr.Any{
			&expr.Meta{Key: expr.MetaKeyIIFNAME, Register: 1},
			&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: ifname("lo")},
			accept,
		}, nil

	case RuleFromInside, RuleNotTowardInside:
		key, invert := expr.MetaKeyIIFNAME, false
		if r.Kind == RuleNotTowardInside {
			key, invert = expr.MetaKeyOIFNAME, true
		}
		set, err := addSet(conn, table, nftables.TypeIFName, ifnameKeys(d.Inside))
		if err != nil {
			return nil, err
		}
		return []expr.Any{
			&expr.Meta{Key: key, Register: 1},
			&expr.Lookup{SourceRegister: 1, SetID: set.ID, SetName: set.Name, Invert: invert},
			accept,
		}, nil

	case RuleICMP:
		return icmpExprs(conn, table, unix.NFPROTO_IPV4, unix.IPPROTO_ICMP,
			nftables.TypeICMPType, icmpTypes)

	case RuleICMPv6:
		return icmpExprs(conn, table, unix.NFPROTO_IPV6, unix.IPPROTO_ICMPV6,
			nftables.TypeICMP6Type, icmpv6InputTypes)

	case RuleForwardICMP:
		return icmpExprs(conn, table, unix.NFPROTO_IPV4, unix.IPPROTO_ICMP,
			nftables.TypeICMPType, icmpForwardTypes)

	case RuleForwardICMPv6:
		return icmpExprs(conn, table, unix.NFPROTO_IPV6, unix.IPPROTO_ICMPV6,
			nftables.TypeICMP6Type, icmpv6ForwardType)

	case RuleDHCPClient:
		return []expr.Any{
			nfproto(), nfprotoCmp(unix.NFPROTO_IPV4),
			l4proto(), l4protoCmp(unix.IPPROTO_UDP),
			portLoad(0), portCmp(67),
			portLoad(2), portCmp(68),
			accept,
		}, nil

	case RuleDHCPv6Client:
		return []expr.Any{
			nfproto(), nfprotoCmp(unix.NFPROTO_IPV6),
			l4proto(), l4protoCmp(unix.IPPROTO_UDP),
			portLoad(2), portCmp(546),
			accept,
		}, nil

	case RuleOpening:
		if r.Opening.Protocol == SixInFour {
			// ip protocol 41 and ip saddr <broker>. The family guard first, as
			// everywhere in this inet table: a v4 source address read out of a
			// v6 header is whatever bytes sit at that offset.
			from := r.Opening.From.As4()
			return []expr.Any{
				nfproto(), nfprotoCmp(unix.NFPROTO_IPV4),
				l4proto(), l4protoCmp(unix.IPPROTO_IPV6),
				&expr.Payload{DestRegister: 1, Base: expr.PayloadBaseNetworkHeader, Offset: 12, Len: 4},
				&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: from[:]},
				accept,
			}, nil
		}
		proto := byte(unix.IPPROTO_TCP)
		if r.Opening.Protocol == UDP {
			proto = unix.IPPROTO_UDP
		}
		return []expr.Any{
			l4proto(), l4protoCmp(proto),
			portLoad(2), portCmp(r.Opening.Port),
			accept,
		}, nil

	case RulePortForward:
		return []expr.Any{
			&expr.Ct{Register: 1, Key: expr.CtKeySTATUS},
			&expr.Bitwise{SourceRegister: 1, DestRegister: 1, Len: 4,
				Mask: native32(ipsDstNAT), Xor: native32(0)},
			&expr.Cmp{Op: expr.CmpOpNeq, Register: 1, Data: native32(0)},
			accept,
		}, nil

	case RuleIPTVIGMP:
		return []expr.Any{
			nfproto(), nfprotoCmp(unix.NFPROTO_IPV4),
			&expr.Meta{Key: expr.MetaKeyIIFNAME, Register: 1},
			&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: ifname(r.Downstream)},
			l4proto(), l4protoCmp(unix.IPPROTO_IGMP), accept,
		}, nil
	case RuleIPTVStream:
		return []expr.Any{
			nfproto(), nfprotoCmp(unix.NFPROTO_IPV4),
			&expr.Meta{Key: expr.MetaKeyIIFNAME, Register: 1},
			&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: ifname(r.Upstream)},
			&expr.Meta{Key: expr.MetaKeyOIFNAME, Register: 1},
			&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: ifname(r.Downstream)},
			&expr.Payload{DestRegister: 1, Base: expr.PayloadBaseNetworkHeader, Offset: 16, Len: 1},
			&expr.Bitwise{SourceRegister: 1, DestRegister: 1, Len: 1, Mask: []byte{0xf0}, Xor: []byte{0}},
			&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: []byte{0xe0}}, accept,
		}, nil
	case RuleMSSClamp:
		// tcp flags syn tcp option maxseg size set <n>. The SYN bit is set on
		// both a SYN and a SYN-ACK, so one rule clamps both directions. MSS is
		// option kind 2 and keeps its value at offset 2, after kind and length.
		return []expr.Any{
			&expr.Meta{Key: expr.MetaKeyOIFNAME, Register: 1},
			&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: ifname(r.Interface)},
			l4proto(), l4protoCmp(unix.IPPROTO_TCP),
			&expr.Payload{DestRegister: 1, Base: expr.PayloadBaseTransportHeader, Offset: 13, Len: 1},
			&expr.Bitwise{SourceRegister: 1, DestRegister: 1, Len: 1,
				Mask: []byte{0x02}, Xor: []byte{0x00}},
			&expr.Cmp{Op: expr.CmpOpNeq, Register: 1, Data: []byte{0x00}},
			&expr.Immediate{Register: 1, Data: binaryutil.BigEndian.PutUint16(uint16(r.MSS))},
			&expr.Exthdr{SourceRegister: 1, Type: tcpOptionMaxseg, Offset: 2, Len: 2,
				Op: expr.ExthdrOpTcpopt},
		}, nil

	case RuleDrop:
		return []expr.Any{
			&expr.Objref{Type: unix.NFT_OBJECT_COUNTER, Name: r.Counter},
			drop,
		}, nil
	}
	return nil, fmt.Errorf("unknown rule kind %d", r.Kind)
}

// icmpExprs matches one family's ICMP and a set of its types.
//
// The family guard comes first: this is an `inet` table, and an ICMP type
// read from an IPv6 packet's transport header by a v4 rule would be a match
// on whatever byte happened to be there.
func icmpExprs(conn *nftables.Conn, table *nftables.Table, family, proto byte,
	kind nftables.SetDatatype, types []byte) ([]expr.Any, error) {
	keys := make([][]byte, len(types))
	for i, t := range types {
		keys[i] = []byte{t}
	}
	set, err := addSet(conn, table, kind, keys)
	if err != nil {
		return nil, err
	}
	return []expr.Any{
		nfproto(), nfprotoCmp(family),
		l4proto(), l4protoCmp(proto),
		&expr.Payload{DestRegister: 1, Base: expr.PayloadBaseTransportHeader, Offset: 0, Len: 1},
		&expr.Lookup{SourceRegister: 1, SetID: set.ID, SetName: set.Name},
		accept,
	}, nil
}

// addSet adds an anonymous constant set. Anonymous sets are bound to the one
// rule that uses them, so each rule gets its own.
func addSet(conn *nftables.Conn, table *nftables.Table, kind nftables.SetDatatype, keys [][]byte) (*nftables.Set, error) {
	set := &nftables.Set{
		Table:     table,
		Anonymous: true,
		Constant:  true,
		KeyType:   kind,
	}
	elements := make([]nftables.SetElement, len(keys))
	for i, k := range keys {
		elements[i] = nftables.SetElement{Key: k}
	}
	if err := conn.AddSet(set, elements); err != nil {
		return nil, err
	}
	return set, nil
}

func nfproto() expr.Any { return &expr.Meta{Key: expr.MetaKeyNFPROTO, Register: 1} }

func nfprotoCmp(family byte) expr.Any {
	return &expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: []byte{family}}
}

func l4proto() expr.Any { return &expr.Meta{Key: expr.MetaKeyL4PROTO, Register: 1} }

func l4protoCmp(proto byte) expr.Any {
	return &expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: []byte{proto}}
}

// portLoad reads the transport source (offset 0) or destination (offset 2)
// port, which TCP and UDP both keep in the same place.
func portLoad(offset uint32) expr.Any {
	return &expr.Payload{DestRegister: 1, Base: expr.PayloadBaseTransportHeader, Offset: offset, Len: 2}
}

func portCmp(port uint16) expr.Any {
	return &expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: binaryutil.BigEndian.PutUint16(port)}
}

// native32 is how conntrack state and status bits are compared: host order,
// unlike every address and port on the wire.
func native32(v uint32) []byte { return binaryutil.NativeEndian.PutUint32(v) }

// ifname is how the kernel compares an interface name: a fixed 16-byte,
// NUL-padded buffer. Comparing the bare string matches nothing.
func ifname(s string) []byte {
	b := make([]byte, unix.IFNAMSIZ)
	copy(b, s)
	return b
}

func ifnameKeys(names []string) [][]byte {
	out := make([][]byte, len(names))
	for i, n := range names {
		out[i] = ifname(n)
	}
	return out
}

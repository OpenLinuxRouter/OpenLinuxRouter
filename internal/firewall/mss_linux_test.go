//go:build linux

package firewall

import (
	"bytes"
	"testing"

	"github.com/google/nftables"
	"github.com/google/nftables/binaryutil"
	"github.com/google/nftables/expr"
)

// The MSS rule is the one expression shape the module writes into a packet
// rather than matching one, and the field it writes is easy to get subtly
// wrong: the value is big-endian, and it belongs at offset 2 of an option
// whose kind is 2.
func TestMSSClampExpressions(t *testing.T) {
	conn, err := nftables.New()
	if err != nil {
		t.Fatal(err)
	}
	table := conn.AddTable(&nftables.Table{Family: nftables.TableFamilyINet, Name: TableName})

	exprs, err := ruleExprs(conn, table, Desired{}, Rule{
		Kind: RuleMSSClamp, Interface: "ens18", MSS: 1452,
	})
	if err != nil {
		t.Fatal(err)
	}
	// No counter and no verdict: the rule rewrites and lets the chain continue.
	if _, ok := exprs[len(exprs)-1].(*expr.Exthdr); !ok {
		t.Fatalf("last expression = %T, want the exthdr write", exprs[len(exprs)-1])
	}
	var (
		immediate *expr.Immediate
		exthdr    *expr.Exthdr
	)
	for _, e := range exprs {
		switch v := e.(type) {
		case *expr.Immediate:
			immediate = v
		case *expr.Exthdr:
			exthdr = v
		}
	}
	if immediate == nil || !bytes.Equal(immediate.Data, binaryutil.BigEndian.PutUint16(1452)) {
		t.Fatalf("MSS immediate = %+v, want big-endian 1452", immediate)
	}
	if exthdr == nil || exthdr.Op != expr.ExthdrOpTcpopt || exthdr.Type != 2 ||
		exthdr.Offset != 2 || exthdr.Len != 2 {
		t.Fatalf("MSS exthdr = %+v, want tcpopt kind 2, offset 2, length 2", exthdr)
	}
}

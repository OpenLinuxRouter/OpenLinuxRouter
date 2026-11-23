package link

import (
	"bytes"
	"strings"
	"testing"
)

// The terminal gets the drops and nothing else: seven cumulative counters
// would bury the state column that explains most broken interfaces, and
// -o json is where a consumer that wants to difference them goes.
func TestInterfacesTablePrintsDropsAndNothingElse(t *testing.T) {
	rx, tx := uint64(12), uint64(3)
	packets := uint64(987654)
	resp := listResponse{Interfaces: []interfaceView{{
		Name:      "lan0",
		Present:   true,
		Up:        true,
		Running:   true,
		RXPackets: &packets,
		RXDropped: &rx,
		TXDropped: &tx,
	}}}

	var out bytes.Buffer
	if err := writeInterfacesText(&out, resp); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	if !strings.Contains(text, "DROPS") {
		t.Errorf("the table has no drops column:\n%s", text)
	}
	if !strings.Contains(text, "12/3") {
		t.Errorf("the table does not name the drops:\n%s", text)
	}
	if strings.Contains(text, "987654") {
		t.Errorf("a packet counter reached the terminal:\n%s", text)
	}
}

func TestDropsTextKeepsAbsentDistinctFromZero(t *testing.T) {
	zero := uint64(0)
	seven := uint64(7)
	for _, tc := range []struct {
		name string
		rx   *uint64
		tx   *uint64
		want string
	}{
		{"neither read", nil, nil, "-"},
		{"rx absent", nil, &seven, "-/7"},
		{"tx absent", &seven, nil, "7/-"},
		{"observed zeros", &zero, &zero, "0/0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := dropsText(interfaceView{RXDropped: tc.rx, TXDropped: tc.tx})
			if got != tc.want {
				t.Errorf("dropsText = %q, want %q", got, tc.want)
			}
		})
	}
}

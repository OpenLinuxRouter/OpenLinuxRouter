package qos

import (
	"context"
	"encoding/json"
	"fmt"
	"net/netip"
	"strings"
	"testing"

	"github.com/open-linux-router/open-linux-router/internal/clients"
	"github.com/open-linux-router/open-linux-router/internal/core"
	"github.com/open-linux-router/open-linux-router/internal/link"
)

type deviceFixture []clients.Resolved

func (d deviceFixture) List(context.Context) ([]clients.Resolved, []clients.Problem, error) {
	return d, nil, nil
}

func testEngine(t *testing.T) *Engine {
	t.Helper()
	store := core.NewStore(t.TempDir()+"/config.json", ModuleName, link.ModuleName)
	doc, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(link.Config{Networks: []link.Network{{Name: "Home", Members: []string{"lan0"}, IPv4: &link.NetworkIPv4{Subnet: netip.MustParsePrefix("192.168.1.0/24")}}}})
	doc.Set(link.ModuleName, b)
	if err := store.Save(doc); err != nil {
		t.Fatal(err)
	}
	return &Engine{Store: store, StatePath: t.TempDir() + "/state.json", Devices: deviceFixture{{MAC: "aa:bb:cc:dd:ee:ff", Network: "Home"}}}
}

func TestPoliciesActivateWithoutSwitchAndFollowMAC(t *testing.T) {
	e := testEngine(t)
	c := Config{Devices: []Device{{MAC: "aa:bb:cc:dd:ee:ff", Priority: High, DownloadMbps: 3, UploadMbps: 2}}}
	queues, pending, err := e.desired(context.Background(), c)
	if err != nil || len(queues) != 1 || len(pending) != 0 {
		t.Fatalf("plan: %+v %v %v", queues, pending, err)
	}
	var commands []string
	e.Run = func(_ context.Context, tool string, args ...string) ([]byte, error) {
		commands = append(commands, tool+" "+strings.Join(args, " "))
		return []byte("[]"), nil
	}
	if err := e.shape(context.Background(), "lan0", "dst_mac", 0, true, c.Devices); err != nil {
		t.Fatal(err)
	}
	if err := e.shape(context.Background(), "ifb0", "src_mac", 0, false, c.Devices); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(commands, "\n")
	for _, want := range []string{"ceil 3.000000mbit prio 0", "ceil 2.000000mbit prio 0", "dst_mac aa:bb:cc:dd:ee:ff", "src_mac aa:bb:cc:dd:ee:ff", "protocol all", "skip_hw"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %q in %s", want, joined)
		}
	}
	if strings.Contains(joined, "mark") || strings.Contains(joined, "192.168") {
		t.Fatal("classifier must not depend on IP or change routing marks")
	}
	c.SetDevice(Device{MAC: c.Devices[0].MAC, Priority: Normal})
	if c.Requested() {
		t.Fatal("reset must remove policy without a switch")
	}
}

func TestAmbiguousOrMissingPlacementIsPending(t *testing.T) {
	e := testEngine(t)
	e.Devices = deviceFixture{}
	c := Config{Devices: []Device{{MAC: "aa:bb:cc:dd:ee:ff", UploadMbps: 2}}}
	qs, pending, err := e.desired(context.Background(), c)
	if err != nil || len(qs) != 0 || len(pending) != 1 {
		t.Fatalf("%+v %v %v", qs, pending, err)
	}
}

func TestForeignRootAndIngressAreRefusedBeforeWrites(t *testing.T) {
	for _, foreign := range []string{"root", "ingress", "offload"} {
		t.Run(foreign, func(t *testing.T) {
			e := testEngine(t)
			var mutations []string
			e.Run = func(_ context.Context, tool string, args ...string) ([]byte, error) {
				cmd := strings.Join(args, " ")
				if strings.Contains(cmd, "replace") || strings.Contains(cmd, " add ") || strings.Contains(cmd, " del ") {
					mutations = append(mutations, cmd)
				}
				if tool == "nft" && foreign == "offload" {
					return []byte(`{"nftables":[{"flowtable":{}}]}`), nil
				}
				if tool == "tc" && strings.Contains(cmd, "qdisc show") {
					if foreign == "root" {
						return []byte(`[{"root":true,"kind":"cake","handle":"1:"}]`), nil
					}
					return []byte(`[{"kind":"ingress","handle":"ffff:"}]`), nil
				}
				if strings.Contains(cmd, "filter show") {
					return []byte(`[{"pref":1,"kind":"flower"}]`), nil
				}
				return []byte("[]"), nil
			}
			err := e.Apply(context.Background(), Config{Devices: []Device{{MAC: "aa:bb:cc:dd:ee:ff", UploadMbps: 2}}})
			if err == nil || len(mutations) > 0 {
				t.Fatalf("err=%v mutations=%v", err, mutations)
			}
		})
	}
}

func TestNoOpRestartPreservesQueuesAndDetectsDrift(t *testing.T) {
	e := testEngine(t)
	c := Config{Devices: []Device{{MAC: "aa:bb:cc:dd:ee:ff", UploadMbps: 2}}}
	qs, _, err := e.desired(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	e.Run = func(_ context.Context, tool string, args ...string) ([]byte, error) {
		if tool != "nft" && args[0] != "-j" {
			return nil, fmt.Errorf("unexpected mutation: %v", args)
		}
		return []byte("[]"), nil
	}
	snapshot, err := e.snapshot(context.Background(), qs)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.saveState(runtimeState{Queues: qs, Snapshot: snapshot}); err != nil {
		t.Fatal(err)
	}
	if err := e.Apply(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	if !e.Status(context.Background(), c).Active {
		t.Fatal("matching running state should be active")
	}
	e.Run = func(context.Context, string, ...string) ([]byte, error) { return []byte("[{\"drift\":true}]"), nil }
	if e.Status(context.Background(), c).Active {
		t.Fatal("drift must never be reported active")
	}
}

func TestTrafficCountersDoNotTriggerQueueRebuild(t *testing.T) {
	a, err := stableJSON([]byte(`[{"kind":"matchall","options":{"actions":[{"kind":"mirred","to_dev":"olrq1","stats":{"bytes":12},"installed":30,"used":2,"index":4}]}}]`))
	if err != nil {
		t.Fatal(err)
	}
	b, err := stableJSON([]byte(`[{"kind":"matchall","options":{"actions":[{"kind":"mirred","to_dev":"olrq1","stats":{"bytes":99999},"installed":90,"used":0,"index":8}]}}]`))
	if err != nil {
		t.Fatal(err)
	}
	if string(a) != string(b) {
		t.Fatalf("traffic changed configuration hash: %s != %s", a, b)
	}
}

func TestDefaultMultiqueueNICCanBeShaped(t *testing.T) {
	e := &Engine{Run: func(context.Context, string, ...string) ([]byte, error) {
		return []byte(`[{"root":true,"kind":"mq","handle":"0:"},{"kind":"fq_codel","handle":"0:","parent":"0:1"}]`), nil
	}}
	if err := e.checkRoot(context.Background(), "eth0"); err != nil {
		t.Fatal(err)
	}
	e.Run = func(context.Context, string, ...string) ([]byte, error) {
		return []byte(`[{"root":true,"kind":"mq","handle":"1:"}]`), nil
	}
	if err := e.checkRoot(context.Background(), "eth0"); err == nil {
		t.Fatal("explicitly configured mq must remain foreign")
	}
}

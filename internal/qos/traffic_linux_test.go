package qos

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// Explicit opt-in: namespace mutation is never performed by ordinary go test.
func TestKernelTraffic(t *testing.T) {
	if os.Getenv("OLR_QOS_INTEGRATION") != "1" {
		t.Skip("set OLR_QOS_INTEGRATION=1 on a privileged Linux host")
	}
	if os.Getenv("OLR_QOS_CHILD") != "1" {
		binary, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command("unshare", "--net", "--mount", "--", binary, "-test.run=^TestKernelTraffic$", "-test.v")
		cmd.Env = append(os.Environ(), "OLR_QOS_CHILD=1")
		b, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("isolated traffic test: %v\n%s", err, b)
		}
		t.Log(string(b))
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	run := func(tool string, args ...string) []byte {
		t.Helper()
		b, err := exec.CommandContext(ctx, tool, args...).CombinedOutput()
		if err != nil {
			t.Fatalf("%s %v: %v\n%s", tool, args, err, b)
		}
		return b
	}
	for _, name := range []string{"olr-qos-client", "olr-qos-server"} {
		run("ip", "netns", "add", name)
		t.Cleanup(func() { _ = exec.Command("ip", "netns", "del", name).Run() })
		run("ip", "-n", name, "link", "set", "lo", "up")
	}
	run("ip", "link", "set", "lo", "up")
	for _, pair := range []struct{ iface, peer, ns, router, host, router6, host6 string }{
		{"qlan", "client0", "olr-qos-client", "192.0.2.1/24", "192.0.2.2/24", "fd00:1::1/64", "fd00:1::2/64"},
		{"qwan", "server0", "olr-qos-server", "198.51.100.1/24", "198.51.100.2/24", "fd00:2::1/64", "fd00:2::2/64"},
	} {
		run("ip", "link", "add", pair.iface, "type", "veth", "peer", "name", pair.peer)
		run("ip", "link", "set", pair.peer, "netns", pair.ns)
		run("ip", "addr", "add", pair.router, "dev", pair.iface)
		run("ip", "-6", "addr", "add", pair.router6, "dev", pair.iface, "nodad")
		run("ip", "link", "set", pair.iface, "up")
		run("ip", "-n", pair.ns, "addr", "add", pair.host, "dev", pair.peer)
		run("ip", "-n", pair.ns, "-6", "addr", "add", pair.host6, "dev", pair.peer, "nodad")
		run("ip", "-n", pair.ns, "link", "set", pair.peer, "up")
		run("ip", "-n", pair.ns, "route", "add", "default", "via", strings.Split(pair.router, "/")[0])
		run("ip", "-n", pair.ns, "-6", "route", "add", "default", "via", strings.Split(pair.router6, "/")[0])
	}
	run("ip", "-n", "olr-qos-client", "link", "set", "client0", "address", "aa:bb:cc:dd:ee:ff")
	for _, path := range []string{"/proc/sys/net/ipv4/ip_forward", "/proc/sys/net/ipv6/conf/all/forwarding"} {
		if err := os.WriteFile(path, []byte("1"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	run("nft", "add", "table", "ip", "qos_test")
	run("nft", "add", "chain", "ip", "qos_test", "nat", "{", "type", "nat", "hook", "postrouting", "priority", "100", ";", "}")
	run("nft", "add", "rule", "ip", "qos_test", "nat", "oifname", "qwan", "masquerade")
	e := &Engine{}
	q := queue{Interface: "qlan", IFB: ifbName("qlan"), Clients: []Client{{MAC: "aa:bb:cc:dd:ee:ff", Priority: High, DownloadMbps: 4, UploadMbps: 2}}}
	if err := e.install(ctx, q); err != nil {
		t.Fatal(err)
	}
	measure := func(address string, reverse bool) float64 {
		t.Helper()
		server := exec.CommandContext(ctx, "ip", "netns", "exec", "olr-qos-server", "iperf3", "-s", "-1", "-D")
		if b, err := server.CombinedOutput(); err != nil {
			t.Fatalf("server: %v %s", err, b)
		}
		args := []string{"netns", "exec", "olr-qos-client", "iperf3", "-c", address, "-t", "5", "-O", "1", "-J", "--connect-timeout", "3000"}
		if reverse {
			args = append(args, "-R")
		}
		var result struct {
			End struct {
				Received struct {
					BPS float64 `json:"bits_per_second"`
				} `json:"sum_received"`
			} `json:"end"`
			Error string `json:"error"`
		}
		if err := json.Unmarshal(run("ip", args...), &result); err != nil {
			t.Fatal(err)
		}
		if result.Error != "" {
			t.Fatal(result.Error)
		}
		return result.End.Received.BPS / 1e6
	}
	for _, address := range []string{"198.51.100.2", "fd00:2::2"} {
		for _, reverse := range []bool{false, true} {
			limit := 2.0
			if reverse {
				limit = 4
			}
			got := measure(address, reverse)
			if got < limit*0.65 || got > limit*1.15 {
				t.Fatalf("address=%s reverse=%v got %.2f Mbps; want ceiling %.2f", address, reverse, got, limit)
			}
			t.Logf("%s reverse=%v: %.2f Mbps", address, reverse, got)
		}
	}
	if err := e.remove(ctx, q); err != nil {
		t.Fatal(err)
	}
	if got := measure("198.51.100.2", false); got < 8 {
		t.Fatalf("reset still limited: %.2f Mbps", got)
	}
}

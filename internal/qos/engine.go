package qos

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/open-linux-router/open-linux-router/internal/clients"
	"github.com/open-linux-router/open-linux-router/internal/core"
	"github.com/open-linux-router/open-linux-router/internal/link"
	"github.com/open-linux-router/open-linux-router/internal/runtimedb"
)

// Shape on the LAN side: Ethernet identities survive NAT, VPN routing and
// address changes here. Upload queues run on a private IFB before routing;
// download queues run on LAN egress. No packet or connection mark is changed.
const rootHandle = "4f4c:"
const redirectPref = "20290"

type DeviceView interface {
	List(context.Context) ([]clients.Resolved, []clients.Problem, error)
}

type Engine struct {
	Store        *core.Store
	Clients      DeviceView
	Run          func(context.Context, string, ...string) ([]byte, error)
	StatePath    string
	Runtime      *runtimedb.Store
	BootID       string
	RuntimeError error
	mu           sync.Mutex
}

type queue struct {
	Interface string   `json:"interface"`
	IFB       string   `json:"ifb"`
	Clients   []Client `json:"clients"`
	Download  float64  `json:"download"`
	Upload    float64  `json:"upload"`
}

type runtimeState struct {
	Queues   []queue `json:"queues"`
	Snapshot string  `json:"snapshot"`
}

func (e *Engine) run(ctx context.Context, name string, args ...string) ([]byte, error) {
	if e.Run != nil {
		return e.Run(ctx, name, args...)
	}
	b, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(b)))
	}
	return b, nil
}

func (e *Engine) path() string {
	if e.StatePath != "" {
		return e.StatePath
	}
	return "/run/olr/qos.json"
}

func (e *Engine) readState() (runtimeState, error) {
	var s runtimeState
	if e.RuntimeError != nil {
		return s, e.RuntimeError
	}
	if e.Runtime != nil {
		err := e.Runtime.Import("qos", e.path(), func(data []byte, tx *sql.Tx) error {
			if err := json.Unmarshal(data, &s); err != nil {
				return err
			}
			return runtimedb.PutQoS(tx, e.BootID, data)
		})
		if err != nil {
			return s, err
		}
		data, err := e.Runtime.QoS(e.BootID)
		if err != nil || data == nil {
			return runtimeState{}, err
		}
		err = json.Unmarshal(data, &s)
		return s, err
	}
	b, err := os.ReadFile(e.path())
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	err = json.Unmarshal(b, &s)
	return s, err
}

func ifbName(iface string) string {
	sum := sha256.Sum256([]byte(iface))
	return "olrq" + hex.EncodeToString(sum[:5])
}

func (e *Engine) desired(ctx context.Context, c Config) ([]queue, []string, error) {
	if !c.Requested() {
		return nil, nil, nil
	}
	doc, err := e.Store.Load()
	if err != nil {
		return nil, nil, err
	}
	lc, err := link.FromDocument(doc)
	if err != nil {
		return nil, nil, err
	}
	if e.Clients == nil {
		return nil, nil, fmt.Errorf("client presence is unavailable")
	}
	list, problems, err := e.Clients.List(ctx)
	if err != nil {
		return nil, nil, err
	}
	if len(problems) != 0 {
		return nil, nil, fmt.Errorf("client presence is incomplete: %s", problems[0].Message)
	}
	byMAC := map[string]clients.Resolved{}
	for _, d := range list {
		byMAC[d.MAC] = d
	}
	byInterface := map[string]*queue{}
	var pending []string
	for _, d := range c.Clients {
		if !d.Requested() {
			continue
		}
		seen := byMAC[d.MAC]
		var candidates []string
		for _, n := range lc.Networks {
			for _, iface := range n.Members {
				if seen.Network == n.Name || (seen.Presence != nil && slices.Contains(seen.Presence.Interfaces, iface)) {
					candidates = append(candidates, iface)
				}
			}
		}
		slices.Sort(candidates)
		candidates = slices.Compact(candidates)
		if len(candidates) != 1 {
			pending = append(pending, d.MAC)
			continue
		}
		iface := candidates[0]
		q := byInterface[iface]
		if q == nil {
			q = &queue{Interface: iface, IFB: ifbName(iface), Download: c.DownloadMbps, Upload: c.UploadMbps}
			byInterface[iface] = q
		}
		q.Clients = append(q.Clients, d)
	}
	var queues []queue
	for _, q := range byInterface {
		queues = append(queues, *q)
	}
	slices.SortFunc(queues, func(a, b queue) int { return strings.Compare(a.Interface, b.Interface) })
	return queues, pending, nil
}

func (e *Engine) inspect(ctx context.Context, iface string) ([]map[string]any, error) {
	b, err := e.run(ctx, "tc", "-j", "qdisc", "show", "dev", iface)
	if err != nil {
		return nil, err
	}
	var out []map[string]any
	err = json.Unmarshal(b, &out)
	return out, err
}

func (e *Engine) checkRoot(ctx context.Context, iface string) error {
	qs, err := e.inspect(ctx, iface)
	if err != nil {
		return err
	}
	for _, q := range qs {
		if q["root"] != true {
			continue
		}
		if q["handle"] == rootHandle && q["kind"] == "htb" {
			continue
		}
		// A zero root handle identifies an unconfigured kernel default, including
		// mq on physical multiqueue NICs. Explicitly assigned handles are foreign.
		if q["handle"] == "0:" && (q["kind"] == "noqueue" || q["kind"] == "pfifo_fast" || q["kind"] == "fq_codel" || q["kind"] == "fq" || q["kind"] == "mq") {
			continue
		}
		return fmt.Errorf("%s has a foreign root queue (%v %v)", iface, q["kind"], q["handle"])
	}
	return nil
}

func (e *Engine) snapshot(ctx context.Context, queues []queue) (string, error) {
	h := sha256.New()
	for _, q := range queues {
		for _, iface := range []string{q.Interface, q.IFB} {
			for _, object := range []string{"qdisc", "class", "filter"} {
				args := []string{"-j", object, "show", "dev", iface}
				if object == "filter" {
					args = append(args, "parent", rootHandle)
				}
				b, err := e.run(ctx, "tc", args...)
				if err != nil {
					return "", err
				}
				stable, err := stableJSON(b)
				if err != nil {
					return "", err
				}
				h.Write(stable)
			}
		}
		b, err := e.run(ctx, "tc", "-j", "filter", "show", "dev", q.Interface, "ingress")
		if err != nil {
			return "", err
		}
		stable, err := stableJSON(b)
		if err != nil {
			return "", err
		}
		h.Write(stable)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Ignore counters and action timestamps when comparing queue configurations.
func stableJSON(b []byte) ([]byte, error) {
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		return nil, err
	}
	var strip func(any)
	strip = func(value any) {
		switch x := value.(type) {
		case map[string]any:
			for _, key := range []string{"stats", "stats2", "xstats", "used", "installed", "lastuse", "firstused", "ref", "bind", "index", "bytes", "packets", "drops", "overlimits", "requeues", "backlog", "qlen"} {
				delete(x, key)
			}
			for _, child := range x {
				strip(child)
			}
		case []any:
			for _, child := range x {
				strip(child)
			}
		}
	}
	strip(v)
	return json.Marshal(v)
}

func equalQueues(a, b []queue) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}

func (e *Engine) Status(ctx context.Context, c Config) Status {
	e.mu.Lock()
	defer e.mu.Unlock()
	s := Status{Enabled: c.Requested()}
	qs, pending, err := e.desired(ctx, c)
	s.Pending = pending
	if err == nil && len(qs) > 0 {
		err = e.checkOffload(ctx)
	}
	if err == nil {
		var stored runtimeState
		stored, err = e.readState()
		if err == nil && !equalQueues(qs, stored.Queues) {
			err = fmt.Errorf("saved policies have not been applied")
		}
		if err == nil && len(qs) > 0 {
			var observed string
			observed, err = e.snapshot(ctx, qs)
			if err == nil && observed != stored.Snapshot {
				err = fmt.Errorf("traffic control state differs from saved policies")
			}
		}
	}
	if err != nil {
		s.Reason = err.Error()
		return s
	}
	s.Active = len(qs) > 0
	if len(pending) > 0 {
		s.Reason = "Some clients are waiting for an unambiguous LAN interface"
	}
	if s.Active && (c.DownloadMbps == 0 || c.UploadMbps == 0) {
		for _, d := range c.Clients {
			if d.Priority != Normal && d.Priority != "" {
				s.Reason = "Speed limits apply; priority schedules the LAN queues. Set measured link rates to manage upstream congestion."
				break
			}
		}
	}
	if !c.Requested() && err == nil {
		s.Reason = "No client policies configured"
	}
	return s
}

func (e *Engine) Apply(ctx context.Context, c Config) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	qs, _, err := e.desired(ctx, c)
	if err != nil {
		return err
	}
	previous, err := e.readState()
	if err != nil {
		return err
	}
	if len(qs) > 0 {
		if err := e.checkOffload(ctx); err != nil {
			return err
		}
	}
	if equalQueues(qs, previous.Queues) {
		if len(qs) == 0 {
			return nil
		}
		snapshot, err := e.snapshot(ctx, qs)
		if err == nil && snapshot == previous.Snapshot {
			return nil
		}
	}
	// Inspect every target before writing any of them.
	for _, q := range qs {
		if err := e.checkRoot(ctx, q.Interface); err != nil {
			return err
		}
		if err := e.checkIngress(ctx, q); err != nil {
			return err
		}
		if _, err := e.run(ctx, "ip", "-j", "link", "show", "dev", q.IFB); err == nil {
			b, err := e.run(ctx, "ip", "-j", "-d", "link", "show", "dev", q.IFB)
			if err != nil {
				return err
			}
			var links []struct {
				Alias string `json:"ifalias"`
				Info  struct {
					Kind string `json:"info_kind"`
				} `json:"linkinfo"`
			}
			if err := json.Unmarshal(b, &links); err != nil {
				return err
			}
			if len(links) != 1 || links[0].Alias != "olr:qos:"+q.Interface || links[0].Info.Kind != "ifb" {
				return fmt.Errorf("%s is not an olr-owned IFB", q.IFB)
			}
			if err := e.checkRoot(ctx, q.IFB); err != nil {
				return err
			}
		}
	}
	// Record ownership before execution so a partial failure remains recoverable.
	owned := append([]queue{}, previous.Queues...)
	for _, q := range qs {
		if !slices.ContainsFunc(owned, func(old queue) bool { return old.Interface == q.Interface }) {
			owned = append(owned, q)
		}
	}
	if err := e.saveState(runtimeState{Queues: owned}); err != nil {
		return err
	}
	for _, q := range previous.Queues {
		if !slices.ContainsFunc(qs, func(want queue) bool { return want.Interface == q.Interface }) {
			if err := e.remove(ctx, q); err != nil {
				return err
			}
		}
	}
	for _, q := range qs {
		if err := e.install(ctx, q); err != nil {
			return err
		}
	}
	snapshot, err := e.snapshot(ctx, qs)
	if err != nil {
		return err
	}
	return e.saveState(runtimeState{Queues: qs, Snapshot: snapshot})
}

func (e *Engine) checkOffload(ctx context.Context) error {
	b, err := e.run(ctx, "nft", "-j", "list", "ruleset")
	if err != nil {
		return fmt.Errorf("cannot check flow offload: %w", err)
	}
	if strings.Contains(string(b), `"flowtable"`) || strings.Contains(string(b), `"flow"`) {
		return fmt.Errorf("flow offload may bypass QoS; remove flowtables before applying client policies")
	}
	return nil
}

func (e *Engine) checkIngress(ctx context.Context, q queue) error {
	qs, err := e.inspect(ctx, q.Interface)
	if err != nil {
		return err
	}
	for _, item := range qs {
		if item["kind"] != "ingress" && item["kind"] != "clsact" {
			continue
		}
		b, err := e.run(ctx, "tc", "-j", "filter", "show", "dev", q.Interface, "ingress")
		if err != nil {
			return err
		}
		var filters []map[string]any
		if err := json.Unmarshal(b, &filters); err != nil {
			return err
		}
		for _, f := range filters {
			if f["pref"] != float64(20290) || f["kind"] != "matchall" || !strings.Contains(string(b), q.IFB) {
				return fmt.Errorf("%s has foreign ingress filters; refusing to redirect traffic", q.Interface)
			}
		}
	}
	return nil
}

func (e *Engine) saveState(s runtimeState) error {
	if e.RuntimeError != nil {
		return e.RuntimeError
	}
	if e.Runtime != nil {
		b, err := json.Marshal(s)
		if err != nil {
			return err
		}
		return e.Runtime.SaveQoS(e.BootID, b)
	}
	if err := os.MkdirAll(filepath.Dir(e.path()), 0700); err != nil {
		return err
	}
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	tmp := e.path() + ".tmp"
	if err := os.WriteFile(tmp, b, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, e.path())
}

func (e *Engine) install(ctx context.Context, q queue) error {
	if _, err := e.run(ctx, "ip", "link", "show", "dev", q.IFB); err != nil {
		if _, err := e.run(ctx, "ip", "link", "add", "name", q.IFB, "type", "ifb"); err != nil {
			return err
		}
		if _, err := e.run(ctx, "ip", "link", "set", "dev", q.IFB, "alias", "olr:qos:"+q.Interface); err != nil {
			return err
		}
	}
	if _, err := e.run(ctx, "ip", "link", "set", "dev", q.IFB, "up"); err != nil {
		return err
	}
	for _, direction := range []struct {
		iface, field string
		rate         float64
		download     bool
	}{{q.Interface, "dst_mac", q.Download, true}, {q.IFB, "src_mac", q.Upload, false}} {
		if err := e.shape(ctx, direction.iface, direction.field, direction.rate, direction.download, q.Clients); err != nil {
			return err
		}
	}
	qs, err := e.inspect(ctx, q.Interface)
	if err != nil {
		return err
	}
	hasIngress := false
	for _, item := range qs {
		if item["kind"] == "ingress" || item["kind"] == "clsact" {
			hasIngress = true
		}
	}
	if !hasIngress {
		if _, err := e.run(ctx, "tc", "qdisc", "add", "dev", q.Interface, "handle", "ffff:", "ingress"); err != nil {
			return err
		}
	}
	b, err := e.run(ctx, "tc", "-j", "filter", "show", "dev", q.Interface, "ingress")
	if err != nil {
		return err
	}
	var filters []map[string]any
	if err := json.Unmarshal(b, &filters); err != nil {
		return err
	}
	for _, f := range filters {
		if f["pref"] != float64(20290) {
			return fmt.Errorf("%s has foreign ingress filters; refusing to redirect traffic", q.Interface)
		}
		if f["kind"] != "matchall" || !strings.Contains(string(b), q.IFB) {
			return fmt.Errorf("%s has a foreign ingress filter at priority %s", q.Interface, redirectPref)
		}
	}
	_, err = e.run(ctx, "tc", "filter", "replace", "dev", q.Interface, "ingress", "protocol", "all", "pref", redirectPref, "handle", "1", "matchall", "skip_hw", "action", "mirred", "egress", "redirect", "dev", q.IFB)
	return err
}

func rate(v float64) string { return strconv.FormatFloat(v, 'f', 6, 64) + "mbit" }

func (e *Engine) shape(ctx context.Context, iface, field string, total float64, download bool, policies []Client) error {
	if total == 0 {
		total = 100000
	} // No configured aggregate ceiling.
	commands := [][]string{
		{"qdisc", "replace", "dev", iface, "root", "handle", rootHandle, "htb", "default", "2"},
		{"class", "replace", "dev", iface, "parent", rootHandle, "classid", "4f4c:1", "htb", "rate", rate(total), "ceil", rate(total)},
		{"class", "replace", "dev", iface, "parent", "4f4c:1", "classid", "4f4c:2", "htb", "rate", "1kbit", "ceil", rate(total), "prio", "1", "quantum", "1514"},
		{"qdisc", "replace", "dev", iface, "parent", "4f4c:2", "handle", "2:", "fq_codel"},
	}
	// Delete the owned root to remove stale classes and filters after edits.
	// No-op applies are caught above and preserve the existing queues.
	qs, err := e.inspect(ctx, iface)
	if err != nil {
		return err
	}
	for _, q := range qs {
		if q["root"] == true && q["handle"] == rootHandle {
			if _, err := e.run(ctx, "tc", "qdisc", "del", "dev", iface, "root", "handle", rootHandle); err != nil {
				return err
			}
		}
	}
	for i, d := range policies {
		id := strconv.FormatInt(int64(i+16), 16)
		ceiling := d.UploadMbps
		if download {
			ceiling = d.DownloadMbps
		}
		if ceiling == 0 || ceiling > total {
			ceiling = total
		}
		priority := "1"
		if d.Priority == High {
			priority = "0"
		} else if d.Priority == Low {
			priority = "2"
		}
		commands = append(commands,
			[]string{"class", "replace", "dev", iface, "parent", "4f4c:1", "classid", "4f4c:" + id, "htb", "rate", "1kbit", "ceil", rate(ceiling), "prio", priority, "quantum", "1514"},
			[]string{"qdisc", "replace", "dev", iface, "parent", "4f4c:" + id, "handle", id + ":", "fq_codel"},
			[]string{"filter", "replace", "dev", iface, "parent", rootHandle, "protocol", "all", "pref", strconv.Itoa(i + 16), "flower", "skip_hw", field, d.MAC, "classid", "4f4c:" + id},
		)
	}
	for _, args := range commands {
		if _, err := e.run(ctx, "tc", args...); err != nil {
			return err
		}
	}
	return nil
}

func (e *Engine) remove(ctx context.Context, q queue) error {
	// Only delete roots carrying our reserved handle, never an operator's root.
	for _, iface := range []string{q.Interface, q.IFB} {
		qs, err := e.inspect(ctx, iface)
		if err != nil {
			return err
		}
		for _, item := range qs {
			if item["root"] == true && item["handle"] == rootHandle && item["kind"] == "htb" {
				if _, err := e.run(ctx, "tc", "qdisc", "del", "dev", iface, "root", "handle", rootHandle); err != nil {
					return err
				}
			}
		}
	}
	b, err := e.run(ctx, "tc", "-j", "filter", "show", "dev", q.Interface, "ingress", "pref", redirectPref)
	if err != nil {
		return err
	}
	if strings.Contains(string(b), q.IFB) {
		if _, err := e.run(ctx, "tc", "filter", "del", "dev", q.Interface, "ingress", "pref", redirectPref, "handle", "1", "matchall"); err != nil {
			return err
		}
	}
	// Keep the empty ingress attachment and IFB: deleting shared attachments or
	// a link another administrator is now using would be destructive.
	return nil
}

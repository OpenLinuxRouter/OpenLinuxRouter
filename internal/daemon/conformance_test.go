package daemon

import (
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"strings"
	"testing"

	"github.com/open-linux-router/open-linux-router/internal/cli"
	"github.com/open-linux-router/open-linux-router/internal/core"
	"github.com/open-linux-router/open-linux-router/internal/devices"
	"github.com/open-linux-router/open-linux-router/internal/dhcp"
	"github.com/open-linux-router/open-linux-router/internal/dial"
	"github.com/open-linux-router/open-linux-router/internal/dns"
	"github.com/open-linux-router/open-linux-router/internal/firewall"
	"github.com/open-linux-router/open-linux-router/internal/gateway"
	"github.com/open-linux-router/open-linux-router/internal/gateway/nat"
	"github.com/open-linux-router/open-linux-router/internal/ingress"
	"github.com/open-linux-router/open-linux-router/internal/inspection"
	"github.com/open-linux-router/open-linux-router/internal/iptv"
	"github.com/open-linux-router/open-linux-router/internal/link"
	"github.com/open-linux-router/open-linux-router/internal/network"
	"github.com/open-linux-router/open-linux-router/internal/qos"
	"github.com/open-linux-router/open-linux-router/internal/remote"
	socksout "github.com/open-linux-router/open-linux-router/internal/socksout"
	"github.com/open-linux-router/open-linux-router/internal/system"
	"github.com/open-linux-router/open-linux-router/internal/tools"
)

// Rules the API surface holds to, in the form docs/cli.md argues for: executable
// rather than remembered.
//
// The assertions live in this package for the reason commit 6132524 records
// about the CLI's — internal/core cannot import the modules that import it, so a
// test that wants to see every module's real surface has to sit where they are
// all already in scope. That commit also supplies the cautionary tale: the CLI's
// conformance tests walked a tree the modules were never mounted into, so they
// passed for a year while three surfaces drifted apart.
//
// Routes() is called on a zero-valued HTTP struct. It reads none of its fields —
// it only takes method values — so no applier, socket or kernel is needed to ask
// a module what it serves.
func moduleRoutes() map[string][]core.Route {
	return map[string][]core.Route{
		system.ModuleName:  system.HTTP{}.Routes(),
		link.ModuleName:    link.HTTP{}.Routes(),
		dial.ModuleName:    dial.HTTP{}.Routes(),
		dhcp.ModuleName:    dhcp.HTTP{}.Routes(),
		dns.ModuleName:     dns.HTTP{}.Routes(),
		devices.ModuleName: devices.HTTP{}.Routes(),
		network.ModuleName: network.HTTP{}.Routes(),
		// Both halves of gateway, because the NAT package's routes are mounted
		// under the same prefix and are as much of the module's surface as the
		// routing ones (gateway.HTTP.NAT).
		gateway.ModuleName:    gateway.HTTP{NAT: &nat.HTTP{}}.Routes(),
		remote.ModuleName:     remote.HTTP{}.Routes(),
		qos.ModuleName:        qos.HTTP{}.Routes(),
		ingress.ModuleName:    ingress.HTTP{}.Routes(),
		firewall.ModuleName:   firewall.HTTP{}.Routes(),
		inspection.ModuleName: (&inspection.Service{}).Routes(),
		iptv.ModuleName:       iptv.HTTP{}.Routes(),
		socksout.ModuleName:   socksout.HTTP{}.Routes(),
		tools.ModuleName:      (&tools.HTTP{}).Routes(),
	}
}

// The list above is a second copy of main's, so the first rule is that it is not
// allowed to be a stale one.
//
// This is the assertion that would have caught the CLI's failure at the time
// rather than a year later: it reads what main actually mounts instead of
// trusting that the test was updated alongside it.
func TestEveryMountedModuleIsChecked(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "daemon.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}

	var mounted []string
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		fun, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || fun.Sel.Name != "Mount" || len(call.Args) == 0 {
			return true
		}
		// The first argument is always <module>.ModuleName.
		if arg, ok := call.Args[0].(*ast.SelectorExpr); ok {
			if pkg, ok := arg.X.(*ast.Ident); ok {
				mounted = append(mounted, pkg.Name)
			}
		}
		return true
	})

	if len(mounted) == 0 {
		t.Fatal("found no srv.Mount calls in main.go; this test has stopped reading what it thinks it reads")
	}

	checked := make([]string, 0, len(moduleRoutes()))
	for name := range moduleRoutes() {
		checked = append(checked, name)
	}
	sort.Strings(mounted)
	sort.Strings(checked)

	if strings.Join(mounted, " ") != strings.Join(checked, " ") {
		t.Errorf("main.go mounts [%s] but the conformance tests check [%s]; add the new module to moduleRoutes",
			strings.Join(mounted, " "), strings.Join(checked, " "))
	}
}

// R1 — every route says what it does.
//
// core.RouteTable panics on an empty summary, so this cannot ship broken. It is
// asserted anyway because the panic says "a route has no summary" and this says
// which, and because the summary is what an agent reads to choose a tool: a
// route described badly is a tool called wrongly.
func TestEveryRouteHasASummary(t *testing.T) {
	for module, routes := range moduleRoutes() {
		for _, rt := range routes {
			if strings.TrimSpace(rt.Summary) == "" {
				t.Errorf("%s %s has no summary", module, rt.Pattern())
			}
			if !strings.HasSuffix(rt.Summary, ".") {
				t.Errorf("%s %s: summary should be a sentence ending in a full stop: %q",
					module, rt.Pattern(), rt.Summary)
			}
		}
	}
}

// R2 — every route that only reads is published as a tool.
//
// Stated this way round on purpose. A rule saying "these routes are tools"
// needs editing whenever a module gains one, and the failure mode of forgetting
// is silent: the route works, and the agent simply never learns it exists. A
// rule saying every read *must* be published fails loudly instead, and an
// intentional exception has to be argued for here.
func TestEveryReadRouteIsPublishedAsATool(t *testing.T) {
	exceptions := map[string]bool{
		// The socks5:// link carries credentials and must not enter tool transcripts.
		"remote GET /socks5/link": true,
		// Image bytes are for the browser, not a JSON tool response. The
		// structured service list remains available to agents.
		"ingress GET /services/{name}/icon": true,
		// Binary image bytes are for the UI; the latency JSON already names sites and outcomes.
		"gateway GET /latency/sites/{name}/icon": true,
		// Full native mitmproxy flows are binary downloads. The structured
		// history list and request/response previews are published as tools.
		"inspection GET /history/flow": true,
		// Not a read: it starts one site's probe rather than answering from
		// stored state. The card's measure-now control is a UI action, and the
		// latency GET remains the tool that reads the outcomes.
		"gateway POST /latency/sites/{name}/measure": true,
		// The page's Test all, with the same reasoning: it starts probes and
		// changes no configuration.
		"gateway POST /latency/measure": true,
	}

	for module, routes := range moduleRoutes() {
		for _, rt := range routes {
			if rt.Mutating || rt.Tool != "" || exceptions[module+" "+rt.Pattern()] {
				continue
			}
			t.Errorf("%s %s reads but publishes no tool; give it a Tool or make the case for the exception here",
				module, rt.Pattern())
		}
	}
}

// R3 — no write is published yet.
//
// Increment 1 is read-only, and the reason is §6.2's gate: dhcp, dns and devices
// apply on the first request, with no way for a disruptive change to stop and
// ask. Publishing a write before that exists would give an agent a tool that can
// drop the LAN, with nothing between the model and the change but a tool name in
// an approval dialog. Delete this test in the same commit that lands the gate.
func TestNoMutatingRouteIsPublishedYet(t *testing.T) {
	for module, routes := range moduleRoutes() {
		for _, rt := range routes {
			if rt.Mutating && rt.Tool != "" {
				t.Errorf("%s %s is a write published as tool %q, before the disruptive gate exists",
					module, rt.Pattern(), rt.Tool)
			}
		}
	}
}

// R4 — a tool's first word is a shared verb.
//
// The same vocabulary the CLI is held to (internal/cli/verbs.go). `olr dhcp show
// leases` and `dhcp_show_leases` are one operation named once; a tool that
// invented "get" or "fetch" would be the drift that design.md §3.2 rule 4 exists
// to prevent, arriving through the one surface nobody reads by hand.
func TestToolVerbsComeFromTheSharedVocabulary(t *testing.T) {
	allowed := make(map[string]bool)
	for _, v := range cli.Verbs() {
		allowed[v] = true
	}

	for module, routes := range moduleRoutes() {
		for _, rt := range routes {
			if rt.Tool == "" {
				continue
			}
			verb := strings.Fields(rt.Tool)[0]
			if !allowed[verb] {
				t.Errorf("%s %s uses the verb %q, which is not in %v",
					module, rt.Pattern(), verb, cli.Verbs())
			}
		}
	}
}

// R5 — no two routes produce the same tool name.
//
// A duplicate would not fail anywhere else: the tool map would simply keep
// whichever was built last, and one route would become unreachable through MCP
// while still answering over HTTP.
func TestToolNamesAreUnique(t *testing.T) {
	seen := map[string]string{}
	for module, routes := range moduleRoutes() {
		for _, rt := range routes {
			if rt.Tool == "" {
				continue
			}
			name := module + "_" + strings.ReplaceAll(rt.Tool, " ", "_")
			if first, dup := seen[name]; dup {
				t.Errorf("%s is produced by both %s and %s %s", name, first, module, rt.Pattern())
			}
			seen[name] = module + " " + rt.Pattern()
		}
	}
}

// R6 — internal/mcp reaches the modules only through the API.
//
// This is the invariant the whole package doc rests on, and the import graph is
// what enforces it. If internal/mcp ever imports a module directly it gains a
// path to the system that skips validation, skips the global apply lock, and
// publishes no event — and it stops being an equal client of the API in the
// sense design.md §1 means. Nothing else would notice: the code would compile
// and the tools would work.
func TestMCPImportsNoModule(t *testing.T) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, "../mcp", nil, parser.ImportsOnly)
	if err != nil {
		t.Fatal(err)
	}
	if len(pkgs) == 0 {
		t.Fatal("parsed no packages; this test is not reading internal/mcp")
	}

	forbidden := []string{"internal/link", "internal/dhcp", "internal/dns", "internal/devices", "internal/gateway", "internal/dnsrelay"}

	for _, pkg := range pkgs {
		for path, file := range pkg.Files {
			// The package's own tests mount modules from core, not from a
			// module package, so they are held to the same rule as the code.
			for _, imp := range file.Imports {
				for _, bad := range forbidden {
					if strings.Contains(imp.Path.Value, bad) {
						t.Errorf("%s imports %s; the MCP surface must reach modules only through the API handler",
							path, imp.Path.Value)
					}
				}
			}
		}
	}
}

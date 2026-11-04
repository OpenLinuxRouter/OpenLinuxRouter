package core

import (
	"fmt"
	"net/http"
)

// A module's HTTP surface, declared as data.
//
// Every module used to call mux.HandleFunc inline inside its Handler, which
// made the surface unenumerable: http.ServeMux has no introspection, so nothing
// outside a module could ask what routes it serves. That cost nothing while the
// only readers were the WebUI and the CLI — both hand-written against routes
// they already know, both maintained by somebody who can read the file.
//
// It stops being free at the third reader. design.md §6.4 wants MCP tools
// *generated* rather than written, and §9 milestone 5 owes an OpenAPI document;
// neither can be derived from a surface that can only be discovered by reading
// Go. So the table is the source and the mux is built from it, which is the
// same move §3.2 rule 3 already makes for config structs: declare it once, and
// let every surface be a projection of that declaration rather than a second
// copy maintained in parallel.
//
// What this deliberately does *not* do is describe responses. Config structs
// are reflected (schema.go); read and plan shapes are not, and §6.2 names that
// as an open gap. A Route says what you may call and what a request body must
// look like — not what comes back.

// BodyShape names the schema projection a request body is validated against.
//
// It is the projection and not a Go type because the schema is what every
// generated surface consumes: an MCP tool's inputSchema is the projection
// verbatim, so naming the projection here is naming the tool's arguments.
type BodyShape string

const (
	// BodyNone is a route that takes no body.
	BodyNone BodyShape = ""

	// BodyFull is a complete config document — PUT, and the file on disk.
	BodyFull BodyShape = "full"

	// BodyRelaxed is a partial one — PATCH, `olr set`, and a plan of a change
	// that names only the fields it means to alter.
	BodyRelaxed BodyShape = "relaxed"
)

// QueryParam is one query-string parameter a route accepts.
//
// Declared because a generated surface has no other way to learn it. The route
// table is the only description of a module's inputs that exists outside its Go
// source: an MCP tool built from a route with no declared parameters is a tool
// that cannot pass ?limit=, and dns's query log without a limit is the whole
// log. Path parameters need no equivalent — they are visible in Path.
type QueryParam struct {
	Name string `json:"name"`

	// Type is the JSON Schema type: "integer", "boolean" or "string".
	Type string `json:"type"`

	// Summary says what the parameter does, and reaches an agent as the
	// property description.
	Summary string `json:"summary"`

	// Required reports whether callers must supply this query parameter.
	Required bool `json:"required,omitempty"`
}

// Route is one route a module serves.
type Route struct {
	// Method and Path make the ServeMux pattern, minus the /api/<module>
	// prefix core strips before the module ever sees it: "GET", "/leases".
	Method string `json:"method"`
	Path   string `json:"path"`

	// Summary is one line saying what the route answers or does.
	//
	// Required, and required for a reason that is not tidiness: this is the
	// text an agent reads to decide whether to call the route at all. A tool
	// whose description is empty is a tool the model will either ignore or
	// guess at, and guessing is the failure mode with a router on the other
	// end. RouteTable panics on a route without one.
	Summary string `json:"summary"`

	// Tool is this route's name in the shared surface vocabulary, spelled the
	// way the CLI spells it: "show", "show leases", "status", "plan". The MCP
	// server joins the words with underscores, so `olr dhcp show leases` and
	// the tool `dhcp_show_leases` are visibly the same thing said twice rather
	// than two things that happen to overlap.
	//
	// Empty means the route is deliberately not published as a tool.
	Tool string `json:"tool,omitempty"`

	// Body is the projection a request body must satisfy.
	Body BodyShape `json:"body,omitempty"`

	// Query is the query-string parameters the route accepts.
	Query []QueryParam `json:"query,omitempty"`

	// Mutating reports whether calling this can change the box.
	//
	// Read from, among other places, the MCP tool annotations: readOnlyHint on
	// everything without it, destructiveHint on everything with it.
	Mutating bool `json:"mutating,omitempty"`

	// Handler serves the route. Never published — it is the one field here
	// that is behaviour rather than description.
	Handler http.HandlerFunc `json:"-"`
}

// Pattern is the route as ServeMux spells it.
func (r Route) Pattern() string { return r.Method + " " + r.Path }

// RouteTable builds a ServeMux from a module's routes.
//
// It panics on a malformed table — a missing summary, a missing handler, a
// duplicate pattern. Every one of those is a programming error fixed at the
// keyboard and none of them is a runtime condition, so the same reasoning
// applies as to Mount panicking on a duplicate module name: fail at startup,
// where it is unmissable, rather than serving a surface that is quietly wrong.
func RouteTable(routes []Route) *http.ServeMux {
	mux := http.NewServeMux()
	seen := make(map[string]bool, len(routes))

	for _, rt := range routes {
		switch {
		case rt.Method == "" || rt.Path == "":
			panic(fmt.Sprintf("core: route %q is missing a method or path", rt.Summary))
		case rt.Summary == "":
			panic("core: route " + rt.Pattern() + " has no summary")
		case rt.Handler == nil:
			panic("core: route " + rt.Pattern() + " has no handler")
		case seen[rt.Pattern()]:
			panic("core: route " + rt.Pattern() + " is declared twice")
		}
		seen[rt.Pattern()] = true
		mux.HandleFunc(rt.Pattern(), rt.Handler)
	}
	return mux
}

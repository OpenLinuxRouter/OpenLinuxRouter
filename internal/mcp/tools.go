package mcp

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// Turning the published API surface into tools.
//
// Nothing here is a list of olr's modules or routes. The input is whatever
// /api/routes and /api/schema said, so a module that adds a route gets a tool
// without this file being touched — which is the only version of design.md
// §6.4's "generated from the same schema" that stays true after the second time
// somebody is in a hurry.

// The argument key a request body travels under.
//
// Bodies are nested under one key rather than spread across the top level, and
// the alternative is worth naming because it looks tidier: a plan tool whose
// arguments *are* the config document. That breaks the moment a route has both
// a body and a parameter — every mutating route on `gateway` already carries
// dry_run and confirm — because a config field called "confirm" and the query
// parameter called "confirm" would then be the same argument. Nesting costs one
// level of JSON and removes the whole class.
const configKey = "config"

// tool is one MCP tool, in the shape tools/list publishes.
type tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
	Annotations *annotations    `json:"annotations,omitempty"`

	// route is how a call is served, and is not published.
	route routeCall `json:"-"`
}

// annotations are the hints a client uses to decide how much ceremony a call
// deserves — whether to ask the operator first, mostly.
type annotations struct {
	Title           string `json:"title,omitempty"`
	ReadOnlyHint    bool   `json:"readOnlyHint,omitempty"`
	DestructiveHint bool   `json:"destructiveHint,omitempty"`
}

// routeCall is what calling a tool turns into.
type routeCall struct {
	Method string
	Path   string
	Body   core.BodyShape
	Query  []core.QueryParam
}

// discovery is the two published documents this is built from, decoded.
type discovery struct {
	Routes map[string][]routeDoc
	Schema map[string]projections
}

type routeDoc struct {
	Method   string            `json:"method"`
	Path     string            `json:"path"`
	Summary  string            `json:"summary"`
	Tool     string            `json:"tool"`
	Body     core.BodyShape    `json:"body"`
	Query    []core.QueryParam `json:"query"`
	Mutating bool              `json:"mutating"`
}

// projections holds a module's two schema projections as raw JSON.
//
// Raw, and that is the point of the type existing at all. These bytes are
// core.Reflect's output and they become a tool's inputSchema unchanged —
// decoding them into some schema type here and re-encoding would put a second
// implementation of JSON Schema between the struct tag and the agent, which is
// exactly the translation layer design.md §8 picked one dialect to avoid.
type projections struct {
	Full    json.RawMessage `json:"full"`
	Relaxed json.RawMessage `json:"relaxed"`
}

// buildTools derives the tool list, sorted by name so that tools/list is stable
// across restarts and a client diffing it sees only real changes.
func buildTools(d discovery) ([]tool, error) {
	var tools []tool

	for module, routes := range d.Routes {
		for _, rt := range routes {
			// No tool word means the route is deliberately unpublished. Today
			// that is every mutating route: writes arrive with the disruptive
			// gate (§6.2), which dhcp, dns and devices do not yet implement,
			// and publishing a tool that can drop the LAN without one would be
			// the wrong thing to ship first.
			if rt.Tool == "" {
				continue
			}

			schema, err := inputSchema(rt, d.Schema[module])
			if err != nil {
				return nil, fmt.Errorf("%s %s: %w", rt.Method, rt.Path, err)
			}

			tools = append(tools, tool{
				Name:        toolName(module, rt.Tool),
				Description: rt.Summary,
				InputSchema: schema,
				Annotations: &annotations{
					Title:           strings.ToUpper(module[:1]) + module[1:] + ": " + rt.Tool,
					ReadOnlyHint:    !rt.Mutating,
					DestructiveHint: rt.Mutating,
				},
				route: routeCall{
					Method: rt.Method,
					Path:   rt.Path,
					Body:   rt.Body,
					Query:  rt.Query,
				},
			})
		}
	}

	sort.Slice(tools, func(i, j int) bool { return tools[i].Name < tools[j].Name })
	return tools, nil
}

// toolName joins a module and its surface vocabulary into one identifier.
//
// `olr dhcp show leases` and `dhcp_show_leases` are the same words in the same
// order, because they are the same operation and an operator who has read
// either should recognise the other (docs/cli.md's whole argument).
func toolName(module, verb string) string {
	return module + "_" + strings.ReplaceAll(verb, " ", "_")
}

// inputSchema builds a tool's arguments from its route.
//
// Path and query parameters become top-level properties; a request body becomes
// the nested "config" property, carrying the module's own projection verbatim.
func inputSchema(rt routeDoc, proj projections) (json.RawMessage, error) {
	properties := map[string]json.RawMessage{}
	var required []string

	for _, segment := range strings.Split(rt.Path, "/") {
		name, _ := pathParameter(segment)
		if name == "" {
			continue
		}
		properties[name] = json.RawMessage(`{"type":"string","minLength":1}`)
		required = append(required, name)
	}

	for _, q := range rt.Query {
		if _, exists := properties[q.Name]; exists {
			return nil, fmt.Errorf("query parameter %q conflicts with a path parameter", q.Name)
		}
		encoded, err := json.Marshal(map[string]string{
			"type":        q.Type,
			"description": q.Summary,
		})
		if err != nil {
			return nil, err
		}
		properties[q.Name] = encoded
		if q.Required {
			required = append(required, q.Name)
		}
	}

	if rt.Body != core.BodyNone {
		if _, exists := properties[configKey]; exists {
			return nil, fmt.Errorf("parameter %q conflicts with the request body argument", configKey)
		}
	}

	switch rt.Body {
	case core.BodyNone:
	case core.BodyFull:
		if len(proj.Full) == 0 {
			return nil, fmt.Errorf("route wants the full projection but the module published none")
		}
		properties[configKey] = proj.Full
	case core.BodyRelaxed:
		if len(proj.Relaxed) == 0 {
			return nil, fmt.Errorf("route wants the relaxed projection but the module published none")
		}
		properties[configKey] = proj.Relaxed
	default:
		return nil, fmt.Errorf("unknown body shape %q", rt.Body)
	}

	// Query parameters are optional unless declared required. Bodies remain
	// optional: POST /plan with no body plans the stored configuration. Path
	// parameters always identify the route and must be supplied.
	schema := map[string]any{
		"type":       "object",
		"properties": properties,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	return json.Marshal(schema)
}

package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

func TestPathParametersAreRequiredStringProperties(t *testing.T) {
	for _, tt := range []struct {
		name string
		path string
		want []string
	}{
		{"multiple", "/api/widgets/{id}/ports/{port}", []string{"id", "port"}},
		{"catch-all", "/api/widgets/{id}/addresses/{address...}", []string{"id", "address"}},
		{"end marker", "/api/widgets/{$}", nil},
		{"static", "/api/widgets/list", nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			tools, err := buildTools(discovery{
				Routes: map[string][]routeDoc{"widgets": {{
					Method: http.MethodPost, Path: tt.path, Tool: "show plan",
					Body:  core.BodyRelaxed,
					Query: []core.QueryParam{{Name: "limit", Type: "integer"}},
				}}},
				Schema: map[string]projections{"widgets": {Relaxed: json.RawMessage(`{"type":"object"}`)}},
			})
			if err != nil {
				t.Fatal(err)
			}
			var schema struct {
				Properties map[string]struct {
					Type      string `json:"type"`
					MinLength int    `json:"minLength"`
				} `json:"properties"`
				Required []string `json:"required"`
			}
			if err := json.Unmarshal(tools[0].InputSchema, &schema); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(schema.Required, tt.want) {
				t.Errorf("required = %v, want %v", schema.Required, tt.want)
			}
			for _, name := range tt.want {
				if p := schema.Properties[name]; p.Type != "string" || p.MinLength != 1 {
					t.Errorf("path property %s = %+v, want nonempty string", name, p)
				}
			}
			if schema.Properties["limit"].Type != "integer" || schema.Properties[configKey].Type != "object" {
				t.Errorf("query or body projection lost: %+v", schema.Properties)
			}
			if len(schema.Properties) != len(tt.want)+2 {
				t.Errorf("unexpected properties: %+v", schema.Properties)
			}
		})
	}
}

func TestRequiredQueryParametersAreProjectedFromPublishedRoutes(t *testing.T) {
	for _, tt := range []struct {
		name string
		path string
		want []string
	}{
		{"query only", "/lookup", []string{"mac"}},
		{"path and query", "/devices/{id}/lookup", []string{"id", "mac"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			api := core.New()
			api.Mount("widgets", []core.Route{{
				Method: http.MethodGet, Path: tt.path, Tool: "show lookup",
				Summary: "Look up a device address.",
				Query: []core.QueryParam{
					{Name: "mac", Type: "string", Required: true},
					{Name: "vlan", Type: "integer"},
				},
				Handler: func(w http.ResponseWriter, _ *http.Request) {
					core.WriteJSON(w, http.StatusOK, struct{}{})
				},
			}}, struct{}{})
			s, err := New(api.Handler())
			if err != nil {
				t.Fatal(err)
			}
			var schema struct {
				Properties map[string]struct {
					Type string `json:"type"`
				} `json:"properties"`
				Required []string `json:"required"`
			}
			if err := json.Unmarshal(s.listed[0].InputSchema, &schema); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(schema.Required, tt.want) {
				t.Errorf("required = %v, want %v", schema.Required, tt.want)
			}
			if schema.Properties["mac"].Type != "string" || schema.Properties["vlan"].Type != "integer" {
				t.Errorf("query schemas lost: %+v", schema.Properties)
			}
		})
	}
}

func TestPathParameterSchemaRejectsArgumentNameConflicts(t *testing.T) {
	for _, rt := range []routeDoc{
		{Path: "/widgets/{id}", Query: []core.QueryParam{{Name: "id", Type: "integer"}}},
		{Path: "/widgets/{config}", Body: core.BodyRelaxed},
	} {
		if _, err := inputSchema(rt, projections{Relaxed: json.RawMessage(`{"type":"object"}`)}); err == nil {
			t.Errorf("inputSchema accepted conflicting arguments in %+v", rt)
		}
	}
}

func TestRequestCombinesPathQueryAndBodyArguments(t *testing.T) {
	s := &Server{}
	target := tool{route: routeCall{
		Method: http.MethodPost, Path: "/api/widgets/devices/{id}/ports/{port}",
		Body:  core.BodyRelaxed,
		Query: []core.QueryParam{{Name: "detail", Type: "string"}},
	}}
	req, err := s.request(context.Background(), target, map[string]json.RawMessage{
		"id":      json.RawMessage(`"switch/west"`),
		"port":    json.RawMessage(`"ether 1"`),
		"detail":  json.RawMessage(`"full?#"`),
		configKey: json.RawMessage(`{"name":"spline"}`),
		"ignored": json.RawMessage(`"unused"`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if req.Method != http.MethodPost || req.URL.Host != "olr" {
		t.Errorf("request = %s %s", req.Method, req.URL)
	}
	if got, want := req.URL.EscapedPath(), "/api/widgets/devices/switch%2Fwest/ports/ether%201"; got != want {
		t.Errorf("escaped path = %q, want %q", got, want)
	}
	if got, want := req.URL.RawQuery, "detail=full%3F%23"; got != want {
		t.Errorf("query = %q, want %q", got, want)
	}
	body, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != `{"name":"spline"}` || req.Header.Get("Content-Type") != "application/json" {
		t.Errorf("body = %s, content type = %q", body, req.Header.Get("Content-Type"))
	}
}

func TestEscapedPathArgumentsReachThePublishedHandler(t *testing.T) {
	api := core.New()
	api.Mount("widgets", []core.Route{{
		Method: http.MethodGet, Path: "/devices/{id}/snapshot", Tool: "show snapshot",
		Summary: "Show a device snapshot.",
		Handler: func(w http.ResponseWriter, r *http.Request) {
			core.WriteJSON(w, http.StatusOK, map[string]string{
				"id": r.PathValue("id"), "path": r.URL.EscapedPath(), "query": r.URL.RawQuery,
			})
		},
	}}, struct{}{})
	s, err := New(api.Handler())
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct{ value, escaped string }{
		{"switch/west", "switch%2Fwest"},
		{"//", "%2F%2F"},
		{"../other", "..%2Fother"},
		{"a/../../b", "a%2F..%2F..%2Fb"},
		{"switch ?#%", "switch%20%3F%23%25"},
		{"%2F", "%252F"},
		{"%2e%2e", "%252e%252e"},
		{"switch/{id}", "switch%2F%7Bid%7D"},
		{"路由器", "%E8%B7%AF%E7%94%B1%E5%99%A8"},
	} {
		t.Run(tt.value, func(t *testing.T) {
			got := toolResultOf(t, call(t, s, "tools/call", map[string]any{
				"name": "widgets_show_snapshot", "arguments": map[string]any{"id": tt.value},
			}))
			if got.IsError {
				t.Fatalf("call failed: %s", got.Content[0].Text)
			}
			var seen map[string]string
			if err := json.Unmarshal([]byte(got.Content[0].Text), &seen); err != nil {
				t.Fatal(err)
			}
			if seen["id"] != tt.value {
				t.Errorf("handler received id %q, want %q", seen["id"], tt.value)
			}
			if want := "/devices/" + tt.escaped + "/snapshot"; seen["path"] != want {
				t.Errorf("handler path = %q, want %q", seen["path"], want)
			}
			if seen["query"] != "" {
				t.Errorf("path argument introduced query %q", seen["query"])
			}
		})
	}
}

func TestInvalidPathArgumentsFailBeforeCallingTheAPI(t *testing.T) {
	for _, tt := range []struct {
		name string
		raw  json.RawMessage
	}{
		{"missing", nil},
		{"empty", json.RawMessage(`""`)},
		{"null", json.RawMessage(`null`)},
		{"number", json.RawMessage(`42`)},
		{"boolean", json.RawMessage(`true`)},
		{"array", json.RawMessage(`["id"]`)},
		{"object", json.RawMessage(`{"id":"other"}`)},
		{"invalid JSON", json.RawMessage(`{`)},
		{"single slash", json.RawMessage(`"/"`)},
		{"dot", json.RawMessage(`"."`)},
		{"dot-dot", json.RawMessage(`".."`)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			called := false
			s := &Server{api: http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true })}
			args := map[string]json.RawMessage{}
			if tt.raw != nil {
				args["id"] = tt.raw
			}
			got := s.call(context.Background(), tool{route: routeCall{
				Method: http.MethodGet, Path: "/api/widgets/{id}/snapshot",
			}}, args)
			if !got.IsError || !strings.Contains(got.Content[0].Text, "id") {
				t.Errorf("invalid path argument did not produce a named tool error: %+v", got)
			}
			if called {
				t.Error("invalid path argument reached the API")
			}
		})
	}
}

func TestCatchAllPathArguments(t *testing.T) {
	for _, tt := range []struct{ value, want string }{
		{"192.0.2.1/24", "/api/widgets/addresses/192.0.2.1/24"},
		{"a b/c?#%", "/api/widgets/addresses/a%20b/c%3F%23%25"},
	} {
		raw, err := json.Marshal(tt.value)
		if err != nil {
			t.Fatal(err)
		}
		got, err := requestPath("/api/widgets/addresses/{address...}", map[string]json.RawMessage{"address": raw})
		if err != nil || got != tt.want {
			t.Errorf("requestPath(%q) = %q, %v; want %q", tt.value, got, err, tt.want)
		}
	}
	for _, value := range []string{"", ".", "..", "a/../b", "a/./b", "/a", "a/", "a//b"} {
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := requestPath("/api/widgets/addresses/{address...}", map[string]json.RawMessage{"address": raw}); err == nil {
			t.Errorf("catch-all accepted empty or traversal segments in %q", value)
		}
	}
}

func TestEndMarkerConstructsATrailingSlash(t *testing.T) {
	got, err := requestPath("/api/widgets/{$}", nil)
	if err != nil || got != "/api/widgets/" {
		t.Errorf("end marker path = %q, %v", got, err)
	}
}

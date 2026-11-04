package network

import (
	"encoding/json"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/open-linux-router/open-linux-router/internal/core"
	"github.com/open-linux-router/open-linux-router/internal/mcp"
)

func TestPublishedNetworkToolsReachTheSameDeviceAPI(t *testing.T) {
	service, adapter := testService(t)
	api := core.New()
	api.Mount(ModuleName, HTTP{Service: service, Lock: api.ApplyLock(), Events: api.Events()}.Routes(), Config{})
	server, err := mcp.New(api.Handler())
	if err != nil {
		t.Fatal(err)
	}
	list := httptest.NewRecorder()
	server.ServeHTTP(list, httptest.NewRequest("POST", "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)))
	var catalog struct {
		Result struct {
			Tools []struct {
				Name        string `json:"name"`
				InputSchema struct {
					Required []string `json:"required"`
				} `json:"inputSchema"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(list.Body.Bytes(), &catalog); err != nil {
		t.Fatal(err)
	}
	required := map[string][]string{}
	for _, tool := range catalog.Result.Tools {
		required[tool.Name] = tool.InputSchema.Required
	}
	for name, want := range map[string][]string{"network_show_snapshot": {"id"}, "network_show_fdb": {"id", "mac"}, "network_show_operation": {"id", "kind", "mac", "target"}} {
		got := slices.Clone(required[name])
		slices.Sort(got)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s required=%v want=%v", name, got, want)
		}
	}
	for _, request := range []string{
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"network_show_snapshot","arguments":{"id":"ap"}}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"network_show_operation","arguments":{"id":"ap","kind":"client.disconnect","target":"wlan0","mac":"02:aa:bb:cc:dd:ee"}}}`,
	} {
		w := httptest.NewRecorder()
		server.ServeHTTP(w, httptest.NewRequest("POST", "/mcp", strings.NewReader(request)))
		if w.Code != 200 || strings.Contains(w.Body.String(), `"isError":true`) || strings.Contains(w.Body.String(), `"error":`) {
			t.Fatalf("tool failed %d %s", w.Code, w.Body)
		}
	}
	if adapter.calls != 0 {
		t.Fatal("published tool executed a remote mutation")
	}
}

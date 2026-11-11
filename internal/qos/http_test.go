package qos

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

func TestDevicePolicyRoutesStoreIntentButNeverClaimActive(t *testing.T) {
	a := Applier{Store: core.NewStore(t.TempDir()+"/olr.json", ModuleName)}
	h := HTTP{Applier: a, Lock: core.NewLock(), Events: core.NewEvents()}
	request := func(method, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, path, bytes.NewBufferString(body))
		w := httptest.NewRecorder()
		core.RouteTable(h.Routes()).ServeHTTP(w, r)
		return w
	}
	mac := "aa:bb:cc:dd:ee:ff"
	if w := request("PUT", "/clients/"+mac, `{"priority":"high","download_mbps":20}`); w.Code != 200 {
		t.Fatalf("save: %d %s", w.Code, w.Body)
	}
	var saved struct {
		Status Status `json:"status"`
	}
	w := request("GET", "/status", "")
	if err := json.Unmarshal(w.Body.Bytes(), &saved.Status); err != nil {
		t.Fatal(err)
	}
	if saved.Status.Active {
		t.Fatal("unenforced QoS reported active")
	}
	c, err := a.Load()
	if err != nil || c.Client(mac).DownloadMbps != 20 {
		t.Fatalf("policy not stored: %+v %v", c, err)
	}
	if w := request("PUT", "/config", `{"enabled":true,"download_mbps":100,"upload_mbps":20}`); w.Code != http.StatusOK {
		t.Fatalf("legacy enabled field: %d %s", w.Code, w.Body)
	}
	if w := request("DELETE", "/clients/"+mac, ""); w.Code != 200 {
		t.Fatalf("reset: %d %s", w.Code, w.Body)
	}
	c, _ = a.Load()
	if len(c.Clients) != 0 {
		t.Fatalf("reset retained %+v", c.Clients)
	}
}

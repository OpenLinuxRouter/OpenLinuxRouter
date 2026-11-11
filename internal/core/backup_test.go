package core

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func backupJSON(body string) string { return `{"$schema":"` + SchemaURL + `",` + body + `}` }

func TestBackupRejectsAmbiguousAndIncompatibleDocuments(t *testing.T) {
	s := tempStore(t)
	for _, body := range []string{
		`null`, `[]`, `{}`, `{"$schema":"future"}`, backupJSON(`"future":{}`),
		backupJSON(`"link":null`), backupJSON(`"link":{},"link":{}`),
		backupJSON(`"link":{"adopted":[],"adopted":["eth0"]}`),
		backupJSON(`"dhcp":{"password":"********"}`), backupJSON(`"link":{}`) + `{}`,
	} {
		if _, err := s.ParseBackup([]byte(body), nil); err == nil {
			t.Fatalf("accepted unsafe backup: %s", body)
		}
	}
}

func TestBackupExportPreservesSecretsAndUint64(t *testing.T) {
	r := testRecovery(t)
	d, _ := r.Store.Load()
	d.Set("dhcp", []byte(`{"token":"keep-secret","counter":18446744073709551615}`))
	if err := r.Store.Save(d); err != nil {
		t.Fatal(err)
	}
	b := &Backup{Store: r.Store, Recovery: r}
	w := httptest.NewRecorder()
	b.export(w, httptest.NewRequest("GET", "/config", nil))
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal(w.Code, w.Header())
	}
	if !strings.Contains(w.Body.String(), "keep-secret") || !strings.Contains(w.Body.String(), "18446744073709551615") {
		t.Fatal("export lost credentials or integer precision")
	}
	parsed, err := r.Store.ParseBackup(w.Body.Bytes(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if ConfigETag(parsed.keys) != w.Header().Get("ETag") {
		t.Fatal("export revision does not match document")
	}
}

func TestBackupRestorePreviewConfirmationAndFailureRecovery(t *testing.T) {
	for _, scenario := range []string{"preview", "unconfirmed", "invalid", "conflict", "apply", "failure"} {
		t.Run(scenario, func(t *testing.T) {
			r := testRecovery(t)
			before, _ := os.ReadFile(r.Store.Path())
			applies := 0
			r.Rollback = func(ctx context.Context, from, desired Document, runtime json.RawMessage) error {
				if _, err := os.Stat(r.Path); err != nil {
					t.Fatal("intent was replaced before recovery was durable")
				}
				applies++
				if err := r.Store.Save(desired); err != nil {
					return err
				}
				if scenario == "failure" && applies == 1 {
					return errors.New("backend failed after writing")
				}
				return nil
			}
			b := &Backup{Store: r.Store, Recovery: r, Validate: func(context.Context, Document) error {
				if scenario == "invalid" {
					return errors.New("bad reference")
				}
				return nil
			}}
			query := "?confirm=true"
			if scenario == "preview" {
				query = "?dry_run=true"
			}
			if scenario == "unconfirmed" {
				query = ""
			}
			req := httptest.NewRequest("POST", "/restore"+query, strings.NewReader(backupJSON(`"link":{"adopted":["eth0"]}`)))
			if scenario == "conflict" {
				req.Header.Set("If-Match", `"stale"`)
			}
			w := httptest.NewRecorder()
			b.restore(w, req)
			want := map[string]int{"preview": 200, "unconfirmed": 409, "invalid": 422, "conflict": 412, "apply": 200, "failure": 500}[scenario]
			if w.Code != want {
				t.Fatalf("status=%d body=%s", w.Code, w.Body)
			}
			after, _ := os.ReadFile(r.Store.Path())
			if scenario == "apply" {
				if applies != 1 || r.View().State != "pending" || string(before) == string(after) {
					t.Fatal("restore did not start a connectivity trial")
				}
			} else {
				if string(before) != string(after) || r.View().State != "idle" {
					t.Fatal("failed or preview restore changed installed intent", r.View())
				}
				if scenario == "failure" && applies != 2 {
					t.Fatal("failed apply was not reverted")
				}
				if scenario != "failure" && applies != 0 {
					t.Fatal("preview executed apply")
				}
			}
		})
	}
}

func TestBackupRoutesInheritAuthenticationAndMutationGuard(t *testing.T) {
	r := testRecovery(t)
	b := &Backup{Store: r.Store, Recovery: r, Validate: func(context.Context, Document) error { return nil }}
	s := New()
	s.Recovery = r
	s.Mount("configuration", b.Routes(), struct{}{})
	h := BearerAuth("test-token", s.Handler())
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		path := "/api/configuration/config"
		if method == http.MethodPost {
			path = "/api/configuration/restore?confirm=true"
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(method, path, strings.NewReader(backupJSON(`"link":{}`))))
		if w.Code != 401 {
			t.Fatal("configuration secrets bypassed authentication")
		}
	}
}

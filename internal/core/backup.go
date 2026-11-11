package core

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

// Backup exchanges the same complete JSON document used on disk. It never
// exports redacted API projections or mixes runtime history into operator intent.
type Backup struct {
	Store    *Store
	Recovery *Recovery
	Validate func(context.Context, Document) error

	// Prepare rewrites a document between parsing and validation. It exists for
	// migrations that move a whole subtree from one module key to another:
	// ParseBackup rejects unknown sections, and a box restored from a backup
	// written before the move would otherwise be refused rather than upgraded.
	// The migrated document is what gets validated and stored.
	Prepare func(Document) (Document, error)
}

func (b *Backup) Routes() []Route {
	return []Route{
		{Method: "GET", Path: "/config", Tool: "show config", Summary: "Download complete olr.json, including credentials. Store it securely.", Handler: b.export},
		{Method: "POST", Path: "/restore", Summary: "Validate and restore a complete olr.json. Requires confirmation and starts a connectivity trial.", Body: BodyFull, Query: ApplyQuery(), Mutating: true, Handler: b.restore},
	}
}

func (b *Backup) export(w http.ResponseWriter, req *http.Request) {
	doc, err := b.Store.Load()
	if err != nil {
		WriteError(w, 500, err.Error())
		return
	}
	data, err := b.Store.encode(doc)
	if err != nil {
		WriteError(w, 500, err.Error())
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Disposition", `attachment; filename="olr.json"`)
	w.Header().Set("ETag", ConfigETag(doc.keys))
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(200)
	_, _ = w.Write(data)
}

// ParseBackup rejects ambiguous documents, unknown sections and incompatible
// schema versions before a complete replacement can discard live configuration.
func (s *Store) ParseBackup(data []byte, prepare func(Document) (Document, error)) (Document, error) {
	if len(data) > MaxBodyBytes {
		return Document{}, errors.New("configuration backup exceeds 4 MiB")
	}
	if err := uniqueJSON(json.NewDecoder(bytes.NewReader(data))); err != nil {
		return Document{}, err
	}
	var root map[string]json.RawMessage
	if err := json.Unmarshal(data, &root); err != nil || root == nil {
		return Document{}, errors.New("backup must be a complete JSON object")
	}
	var schema string
	if err := json.Unmarshal(root[schemaKey], &schema); err != nil || schema != SchemaURL {
		return Document{}, fmt.Errorf("unsupported or missing $schema; expected %s", SchemaURL)
	}
	doc, err := s.decode(data)
	if err != nil {
		return Document{}, err
	}
	if prepare != nil {
		doc, err = prepare(doc)
		if err != nil {
			return Document{}, err
		}
	}
	if len(doc.Unknown()) > 0 {
		return Document{}, fmt.Errorf("unsupported configuration sections: %v", doc.Unknown())
	}
	var values any
	if err := json.Unmarshal(data, &values); err != nil {
		return Document{}, err
	}
	if redactedBackup(values) {
		return Document{}, errors.New("backup contains redacted credentials; export the complete olr.json instead of a module API response")
	}
	for name, raw := range doc.keys {
		if len(bytes.TrimSpace(raw)) == 0 || bytes.TrimSpace(raw)[0] != '{' {
			return Document{}, fmt.Errorf("%s must be a configuration object", name)
		}
	}
	return doc, nil
}

func redactedBackup(value any) bool {
	switch v := value.(type) {
	case map[string]any:
		for key, child := range v {
			if child == "********" && (strings.Contains(key, "token") || strings.Contains(key, "password") || strings.Contains(key, "key") || strings.Contains(key, "secret") || key == "callback_url" || key == "hash") {
				return true
			}
			if redactedBackup(child) {
				return true
			}
		}
	case []any:
		for _, child := range v {
			if redactedBackup(child) {
				return true
			}
		}
	}
	return false
}

func uniqueJSON(dec *json.Decoder) error {
	var walk func() error
	walk = func() error {
		token, err := dec.Token()
		if err != nil {
			return err
		}
		delim, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		switch delim {
		case '{':
			seen := map[string]bool{}
			for dec.More() {
				token, err := dec.Token()
				if err != nil {
					return err
				}
				key, ok := token.(string)
				if !ok || seen[key] {
					return errors.New("backup contains duplicate JSON object keys")
				}
				seen[key] = true
				if err := walk(); err != nil {
					return err
				}
			}
		case '[':
			for dec.More() {
				if err := walk(); err != nil {
					return err
				}
			}
		default:
			return errors.New("invalid JSON delimiter")
		}
		_, err = dec.Token()
		return err
	}
	if err := walk(); err != nil {
		return err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return errors.New("backup must contain exactly one JSON document")
	}
	return nil
}

func (b *Backup) restore(w http.ResponseWriter, req *http.Request) {
	data, err := ReadBody(w, req)
	if err != nil {
		WriteError(w, 400, err.Error())
		return
	}
	doc, err := b.Store.ParseBackup(data, b.Prepare)
	if err != nil {
		WriteError(w, 400, err.Error())
		return
	}
	gate, err := ParseApplyGate(req)
	if err != nil {
		WriteError(w, 400, err.Error())
		return
	}
	status := 200
	var response any
	err = b.Recovery.Lock.Do(req.Context(), func() error {
		if b.Validate == nil || b.Recovery.Rollback == nil {
			return errors.New("configuration recovery is unavailable")
		}
		if err := b.Validate(req.Context(), doc); err != nil {
			status = 422
			return err
		}
		modules := make([]string, 0, len(doc.keys))
		for name := range doc.keys {
			modules = append(modules, name)
		}
		sort.Strings(modules)
		current, err := b.Store.Load()
		if err != nil {
			return err
		}
		response = map[string]any{"modules": modules, "revision": ConfigETag(current.keys), "impact": "disruptive", "warning": "Replaces all settings, including administrator access and stored credentials. Interface names must match this machine. External TLS files, startup options and runtime history are not included."}
		if gate.DryRun {
			return nil
		}
		if !gate.Confirm {
			status = 409
			return ErrApplyHeld
		}
		if !MatchConfig(w, req, current.keys) {
			response = nil
			return nil
		}
		if err := b.Recovery.Begin(); err != nil {
			status = 409
			return err
		}
		ctx, cancel := context.WithTimeout(context.WithoutCancel(req.Context()), 60*time.Second)
		defer cancel()
		if err := b.Recovery.Rollback(ctx, current, doc, nil); err != nil {
			status = 500
			revertCtx, stop := context.WithTimeout(context.WithoutCancel(req.Context()), time.Minute)
			defer stop()
			return errors.Join(fmt.Errorf("restoring configuration: %w", err), b.Recovery.revert(revertCtx))
		}
		response = b.Recovery.View()
		return nil
	})
	if err != nil {
		if status == 200 {
			status = 500
		}
		WriteError(w, status, err.Error())
		return
	}
	if response != nil {
		WriteJSON(w, status, response)
	}
}

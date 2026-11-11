package core

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// The configuration store: one JSON file describing the whole box.
//
// design.md §3.2 rule 1 originally gave each module "its own file under
// /etc/open-linux-router/". The namespace half of that rule is intact — it is
// now the top-level key — but the file is shared, and that is a deliberate
// departure with three arguments behind it.
//
//   - **One artefact for the whole box.** The file can be reviewed in a diff,
//     committed to git, or copied to a second machine. Reconstructing that from
//     seven files scattered across a directory is strictly worse for every one
//     of those.
//   - **Multi-module changes become atomic.** Writing N files leaves a window
//     where link.json is new and dhcp.json is old; a crash in that window is
//     unrecoverable state nobody asked for. One rename cannot half-happen.
//   - **It matches the lock we already have.** §3.6 mandates one *global* apply
//     lock, not one per module. A single lock guarding N files was always the
//     odd pairing; a single lock guarding one file is not.
//
// What does not change: intent stays a plain JSON file (§10 — "SSH in and read
// the config" is the recovery path on a router, and a database is the wrong
// thing to meet at that moment), and each module still parses only its own
// subtree with its own strictness.

// ConfigPath is where the whole box's intent lives.
const ConfigPath = "/etc/open-linux-router/olr.json"

// SchemaURL is the published schema for that file.
//
// JSON has no comments, so this key stands in for the ownership header every
// generated file carries (design.md §3.4), and has the side benefit of making
// the file validate in an editor.
const SchemaURL = "https://open-linux-router.org/schema/v1/olr.json"

// schemaKey is the document key holding SchemaURL. It is ours to write, so it
// is stripped on load and re-stamped on save rather than round-tripped.
const schemaKey = "$schema"

// RootedConfigPath is ConfigPath relocated under root.
//
// An empty root gives the real one. A non-empty root is the development escape
// hatch that lets olrd run as an ordinary user against a scratch directory; see
// the --root flag on olrd, and nothing else should ever set it.
func RootedConfigPath(root string) string {
	return filepath.Join(root, ConfigPath)
}

// Document is the whole box's intent, with each module's subtree left unparsed.
//
// Unparsed is the point. Core has no business knowing what a dhcp pool is, and
// keeping the subtree as bytes means a module's own decoder — with its own
// strictness — is still the only thing that ever interprets it.
type Document struct {
	// keys holds every top-level key except schemaKey, module or not.
	keys map[string]json.RawMessage

	// unknown lists the keys that belong to no mounted module, in sorted
	// order. Carried on the document rather than recomputed so that a caller
	// which wants to warn about them does not have to know the module list.
	unknown []string
}

// Raw returns a module's subtree, or false if the document has none.
func (d Document) Raw(module string) ([]byte, bool) {
	raw, ok := d.keys[module]
	if !ok {
		return nil, false
	}
	return raw, true
}

// Delete removes a module's subtree. Migrations use it after moving a subtree
// to the key the module has since taken; leaving the old key behind would keep
// a second copy of the same intent, and ParseBackup would reject it as unknown.
func (d *Document) Delete(module string) {
	delete(d.keys, module)
}

// Set replaces a module's subtree.
func (d *Document) Set(module string, raw []byte) {
	if d.keys == nil {
		d.keys = map[string]json.RawMessage{}
	}
	d.keys[module] = append(json.RawMessage(nil), raw...)
}

// DecodeStrict checks a complete candidate against the composition root's
// declared configuration types, including unknown fields inside modules.
func (d Document) DecodeStrict(dst any) error {
	data, err := json.Marshal(d.keys)
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	return decoder.Decode(dst)
}

// Unknown lists top-level keys belonging to no mounted module.
//
// These are preserved verbatim rather than rejected, and the reason is a
// downgrade: a box running an older olr must not destroy the configuration of a
// module it has not heard of just by saving an unrelated change. Strictness
// still applies *inside* a module's subtree, where a typo'd key is a real
// mistake with a real owner to report it to.
func (d Document) Unknown() []string { return d.unknown }

// Store reads and writes the configuration document.
//
// It holds no cached copy. §10 requires that `kill -9 olrd` followed by a
// restart change no answer the API gives, and a store that served config from
// memory would be the most obvious way to break that. Every Load is a fresh
// read; there is no mutable state here, so concurrent Loads need no lock and
// writes are already serialised by the global apply lock (§3.6).
type Store struct {
	path string

	// modules is the mounted module list in mount order, which is also the
	// order they are written in. Mount order is dependency order (§3.2's
	// literal list is link, dial, dhcp, …), so the file reads top to bottom the
	// way the box is actually brought up.
	modules []string
}

// NewStore returns a store over path, told which modules are mounted.
//
// The module list is passed explicitly rather than read back off the Server,
// for the same reason §3.2 mounts modules as a literal list: the set is bounded
// and known at compile time, so a registry would be machinery serving nothing.
func NewStore(path string, modules ...string) *Store {
	return &Store{path: path, modules: append([]string(nil), modules...)}
}

// Path is where the document lives, for error messages that have to name it.
func (s *Store) Path() string { return s.path }

// Protect tightens existing intent files, including legacy files which may
// still contain credentials after migration. It does not rewrite their data.
func (s *Store) Protect() error {
	for _, path := range append([]string{s.path}, s.LegacyPaths()...) {
		if err := os.Chmod(path, 0o600); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("protecting %s: %w", path, err)
		}
	}
	return nil
}

// Load reads the document. A missing file is not an error — it means the box
// has never been configured, which is exactly what a fresh install looks like.
func (s *Store) Load() (Document, error) {
	data, err := os.ReadFile(s.path)
	switch {
	case os.IsNotExist(err):
		// No document yet. A previous olr may have left per-module files, so
		// look for those before concluding the box is unconfigured.
		return s.loadLegacy()
	case err != nil:
		return Document{}, fmt.Errorf("reading %s: %w", s.path, err)
	}

	doc, err := s.decode(data)
	if err != nil {
		return Document{}, fmt.Errorf("%s: %w", s.path, err)
	}
	return doc, nil
}

// decode parses a document, dropping the schema key and classifying the rest.
func (s *Store) decode(data []byte) (Document, error) {
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(data, &keys); err != nil {
		return Document{}, fmt.Errorf("parsing configuration: %w", err)
	}
	delete(keys, schemaKey)

	doc := Document{keys: keys}
	known := make(map[string]bool, len(s.modules))
	for _, m := range s.modules {
		known[m] = true
	}
	for k := range keys {
		if !known[k] {
			doc.unknown = append(doc.unknown, k)
		}
	}
	sort.Strings(doc.unknown)
	return doc, nil
}

// Save writes the document, stamping the current schema URL.
func (s *Store) Save(d Document) error {
	data, err := s.encode(d)
	if err != nil {
		return err
	}
	// The document contains provider tokens and password hashes. Backends
	// receive their own rendered files; none needs to read this as another user.
	if err := WriteFileAtomic(s.path, data, 0o600); err != nil {
		return fmt.Errorf("writing %s: %w", s.path, err)
	}
	return nil
}

// encode renders the document: schema first, then modules in mount order, then
// anything unrecognised, sorted.
//
// The document is assembled compact and indented in one pass at the end. Doing
// it that way means a subtree's own indentation — whatever the module happened
// to produce — cannot leak into the file, so the output depends only on the
// values and the order, which is what makes the byte comparison downstream
// meaningful.
func (s *Store) encode(d Document) ([]byte, error) {
	var compact bytes.Buffer
	compact.WriteByte('{')

	schema, err := json.Marshal(SchemaURL)
	if err != nil {
		return nil, err
	}
	compact.WriteString(`"` + schemaKey + `":`)
	compact.Write(schema)

	for _, name := range s.writeOrder(d) {
		key, err := json.Marshal(name)
		if err != nil {
			return nil, err
		}
		compact.WriteByte(',')
		compact.Write(key)
		compact.WriteByte(':')
		if err := json.Compact(&compact, d.keys[name]); err != nil {
			return nil, fmt.Errorf("encoding %s configuration: %w", name, err)
		}
	}
	compact.WriteByte('}')

	var out bytes.Buffer
	if err := json.Indent(&out, compact.Bytes(), "", "  "); err != nil {
		return nil, err
	}
	out.WriteByte('\n')
	return out.Bytes(), nil
}

// writeOrder lists the document's keys: mounted modules in mount order, then
// unrecognised keys sorted.
func (s *Store) writeOrder(d Document) []string {
	out := make([]string, 0, len(d.keys))
	for _, m := range s.modules {
		if _, ok := d.keys[m]; ok {
			out = append(out, m)
		}
	}
	rest := make([]string, 0, len(d.keys))
	known := make(map[string]bool, len(s.modules))
	for _, m := range s.modules {
		known[m] = true
	}
	for k := range d.keys {
		if !known[k] {
			rest = append(rest, k)
		}
	}
	sort.Strings(rest)
	return append(out, rest...)
}

// loadLegacy folds pre-single-file per-module configs into a document.
//
// Before the store existed each module owned /etc/open-linux-router/<name>.json
// and wrote it itself. This reads those when the document is absent, so an
// upgrade does not silently come up with an unconfigured box. Nothing is
// written here and the old files are left alone — the fold is in memory, and
// the document appears on disk the next time something saves.
//
// Deliberately conditional on the document being absent entirely: once
// olr.json exists it is the only truth, and a stale dhcp.json beside it is
// ignored rather than merged, so there is never a question of which one won.
//
// This is a migration shim with a known expiry. It can be deleted once there
// is no installed version old enough to have written per-module files.
func (s *Store) loadLegacy() (Document, error) {
	doc := Document{keys: map[string]json.RawMessage{}}
	dir := filepath.Dir(s.path)

	for _, name := range s.modules {
		path := filepath.Join(dir, name+".json")
		data, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return Document{}, fmt.Errorf("reading %s: %w", path, err)
		}

		// The old per-module files carried their own "$schema"; the key is now
		// the document's, and a module's own decoder would reject it as an
		// unknown field. Strip it here rather than teaching every module to
		// tolerate a key it no longer owns.
		var keys map[string]json.RawMessage
		if err := json.Unmarshal(data, &keys); err != nil {
			return Document{}, fmt.Errorf("parsing %s: %w", path, err)
		}
		delete(keys, schemaKey)

		raw, err := json.Marshal(keys)
		if err != nil {
			return Document{}, fmt.Errorf("converting %s: %w", path, err)
		}
		doc.keys[name] = raw
	}

	return doc, nil
}

// LegacyPaths lists the per-module files loadLegacy would read, for a caller
// that wants to tell the operator they are now ignored. Deleted with the shim.
func (s *Store) LegacyPaths() []string {
	dir := filepath.Dir(s.path)
	var out []string
	for _, name := range s.modules {
		path := filepath.Join(dir, name+".json")
		if _, err := os.Stat(path); err == nil {
			out = append(out, path)
		}
	}
	return out
}

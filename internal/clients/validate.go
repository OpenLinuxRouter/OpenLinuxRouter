package clients

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// Problem is one validation finding, addressed by a JSON-ish path so a UI can
// attach it to the field that caused it. Mirrors internal/dhcp's shape, and is
// converted to core.Problem at the view boundary, so a UI needs one renderer
// for every module's complaints rather than one per module.
type Problem struct {
	Path    string
	Message string
}

func (p Problem) String() string {
	if p.Path == "" {
		return p.Message
	}
	return p.Path + ": " + p.Message
}

// Result separates the fatal from the merely suspect, as internal/dhcp does.
type Result struct {
	Errors   []Problem
	Warnings []Problem
}

func (r *Result) errorf(path, format string, args ...any) {
	r.Errors = append(r.Errors, Problem{Path: path, Message: fmt.Sprintf(format, args...)})
}

// OK reports whether the config can be stored.
func (r Result) OK() bool { return len(r.Errors) == 0 }

// Err collapses the errors into one, or nil.
func (r Result) Err() error {
	if r.OK() {
		return nil
	}
	msgs := make([]error, len(r.Errors))
	for i, p := range r.Errors {
		msgs[i] = errors.New(p.String())
	}
	return fmt.Errorf("invalid devices configuration:\n  %w", errors.Join(msgs...))
}

// modelSlug is the accepted shape of a model identifier: lower-case segments of
// letters, digits, dot, underscore or dash, joined by at most one slash — for
// example "synology/ds224plus".
//
// Strict on purpose. A model selects an image asset, so this value becomes part
// of a filename or URL; "../../etc" or a bare ".." reaching that code later is a
// path traversal that would be nobody's obvious fault. Validating the shape at
// the boundary means the asset layer never has to be suspicious of it.
var modelSlug = regexp.MustCompile(`^[a-z0-9]+(?:[._-][a-z0-9]+)*(?:/[a-z0-9]+(?:[._-][a-z0-9]+)*)?$`)

// Validate checks stored identity. It is pure: no files, no netlink, no root —
// which is what lets the whole rule set be tested without a network
// (design.md §5.3.1).
func Validate(c Config) Result {
	var r Result

	seen := map[string]int{}
	for i, d := range c.Clients {
		path := fmt.Sprintf("clients[%d]", i)

		mac, err := core.NormalizeMAC(d.MAC)
		switch {
		case strings.TrimSpace(d.MAC) == "":
			r.errorf(path+".mac", "a device needs a MAC address; it is the identity")
		case err != nil:
			r.errorf(path+".mac", "%v", err)
		default:
			if first, dup := seen[mac]; dup {
				r.errorf(path+".mac",
					"%s is already described at clients[%d]; a client is keyed by MAC, so it appears once",
					mac, first)
			} else {
				seen[mac] = i
			}
		}

		if !d.Category.Valid() {
			r.errorf(path+".category", "unknown category %q; valid values are %s",
				d.Category, joinCategories())
		}

		if n := utf8.RuneCountInString(d.Name); n > MaxNameLen {
			r.errorf(path+".name", "name is %d characters; the limit is %d", n, MaxNameLen)
		}
		if bad, ok := hasControlChar(d.Name); ok {
			// A newline in a name would break every single-line rendering of it,
			// including the CLI's table and a log message quoting it.
			r.errorf(path+".name", "name contains a control character (%q)", bad)
		}

		if d.Model != "" {
			switch {
			case utf8.RuneCountInString(d.Model) > MaxModelLen:
				r.errorf(path+".model", "model is longer than %d characters", MaxModelLen)
			case !modelSlug.MatchString(d.Model):
				r.errorf(path+".model",
					"model %q is not a valid identifier; use lower-case letters, digits, "+
						"dot, underscore or dash, optionally as vendor/model, e.g. synology/ds224plus",
					d.Model)
			}
		}

		if err := d.Icon.Validate(); err != nil {
			r.errorf(path+".icon", "%v", err)
		}

		if n := utf8.RuneCountInString(d.Notes); n > MaxNotesLen {
			r.errorf(path+".notes", "notes are %d characters; the limit is %d", n, MaxNotesLen)
		}

		if d.Group != "" {
			if _, ok := c.FindGroup(d.Group); !ok {
				r.errorf(path+".group", "there is no group called %q; %s", d.Group, knownGroups(c))
			}
		}
	}

	validateGroups(c, &r)
	return r
}

// validateGroups checks the group list itself.
//
// Names are compared without case for uniqueness. "IoT" and "iot" as two groups
// is not a distinction anybody means, and on a map they would read as the same
// box drawn twice — but a client's reference is still matched exactly, so the
// stored spelling is the one that counts.
func validateGroups(c Config, r *Result) {
	seen := map[string]int{}
	for i, g := range c.Groups {
		path := fmt.Sprintf("groups[%d].name", i)
		switch n := utf8.RuneCountInString(g.Name); {
		case g.Name == "":
			r.errorf(path, "a group needs a name")
			continue
		case n > MaxGroupNameLen:
			r.errorf(path, "name is %d characters; the limit is %d", n, MaxGroupNameLen)
		}
		if bad, ok := hasControlChar(g.Name); ok {
			r.errorf(path, "name contains a control character (%q)", bad)
		}
		key := strings.ToLower(g.Name)
		if first, dup := seen[key]; dup {
			r.errorf(path, "%q is already a group at groups[%d]; group names must differ by more than case",
				g.Name, first)
			continue
		}
		seen[key] = i
	}

	for i, g := range c.Groups {
		if g.Parent == "" {
			continue
		}
		path := fmt.Sprintf("groups[%d].parent", i)
		if _, ok := c.FindGroup(g.Parent); !ok {
			r.errorf(path, "there is no group called %q to put %q inside; %s", g.Parent, g.Name, knownGroups(c))
			continue
		}
		if inCycle(c, g.Name) {
			r.errorf(path, "%q cannot sit inside %q: that would put it inside itself", g.Name, g.Parent)
			continue
		}
		if depth := len(c.Ancestors(g.Name)) + 1; depth > MaxGroupDepth {
			r.errorf(path, "%q would be %d levels deep; groups nest at most %d levels",
				g.Name, depth, MaxGroupDepth)
		}
	}
}

// inCycle reports whether walking up from a group ever comes back to a group
// already passed. Ancestors stops at exactly that repeat, so the walk ended on
// a cycle when the last group it reached — the group itself, if the chain is
// empty — still has a parent that exists. A group that is its own parent is
// the one-step case of the same thing.
func inCycle(c Config, name string) bool {
	last := name
	if chain := c.Ancestors(name); len(chain) > 0 {
		last = chain[len(chain)-1]
	}
	top, _ := c.FindGroup(last)
	if top.Parent == "" {
		return false
	}
	_, exists := c.FindGroup(top.Parent)
	return exists
}

// knownGroups finishes an unknown-group message with the names that would
// have worked, so the fix is on the same line as the complaint.
func knownGroups(c Config) string {
	if len(c.Groups) == 0 {
		return "no groups exist yet — create one first"
	}
	names := make([]string, len(c.Groups))
	for i, g := range c.Groups {
		names[i] = fmt.Sprintf("%q", g.Name)
	}
	return "groups are " + strings.Join(names, ", ")
}

func hasControlChar(s string) (rune, bool) {
	for _, r := range s {
		// Tab included: it is a control character that would misalign a table.
		if unicode.IsControl(r) {
			return r, true
		}
	}
	return 0, false
}

func joinCategories() string {
	out := make([]string, 0, len(categories))
	for _, c := range categories {
		out = append(out, string(c))
	}
	return strings.Join(out, ", ")
}

// problems converts this module's findings into core's wire shape, so that
// every module reports a bad field the same way (see internal/dhcp/view.go).
func problems(in []Problem) []core.Problem {
	if len(in) == 0 {
		return nil
	}
	out := make([]core.Problem, 0, len(in))
	for _, p := range in {
		out = append(out, core.Problem{Path: p.Path, Message: p.Message})
	}
	return out
}

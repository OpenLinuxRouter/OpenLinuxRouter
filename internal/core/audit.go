package core

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// The box's record of what changed it.
//
// Every other observation in olr answers "what is true now". This one answers
// "how did it get like this": which client called which mutating route, when,
// from where, and whether the box accepted it. It exists because the questions
// that matter later — *who published this service to the internet*, *why is the
// firewall off* — are unanswerable from state alone, and because a change that
// is allowed to happen without a record is a change nobody will ever own.
//
// **A file, not the journal.** docs/install.md says olr keeps no log files of
// its own, and that principle is right for logs: journald already rotates,
// ships and correlates them, and a second logging system is a second thing to
// get wrong. This is not a log in that sense. It is a bounded, structured
// record that a client reads back over the API — `olr audit` — and that must
// still be there when journald is volatile, when the operator's logging config
// changed, and when the question is asked three months later. The precedent is
// the connectivity journal (docs/configuration-safety.md): mode 0600 under
// /var/lib, owned by the box, read back through the API rather than by hand.
//
// Two deliberate omissions:
//
//   - **No request bodies.** The body of `PUT /api/ingress/config` carries the
//     DNS provider token. An audit that stored it would be a copy of the
//     credential, written to disk, retained for months. The path already names
//     the object (`/api/ingress/services/rustfs`), which is the fact worth
//     keeping.
//   - **No tamper evidence.** Root can edit the file. Saying otherwise would
//     be a promise the design cannot keep; what the record buys is that a
//     change is *seen*, not that it is *proven*.

const (
	// AuditFile is the current segment. Older ones are `changes.jsonl.1`,
	// `.2`, … newest first.
	AuditFile = "changes.jsonl"

	// auditMaxBytes is when the current segment is rolled. At roughly 200
	// bytes an entry that is about twenty thousand changes — years of a home
	// router, and small enough that reading it back is cheap.
	auditMaxBytes int64 = 4 << 20

	// auditSegments is how many rolled files are kept besides the current
	// one, so the record spans at most auditMaxBytes × (auditSegments+1).
	auditSegments = 4
)

// AuditEntry is one change, as the record keeps it.
type AuditEntry struct {
	// At is when the request started, not when it finished: the question is
	// always "when did somebody try this", and a slow apply must not move the
	// answer later than the decision.
	At time.Time `json:"at"`

	// Module is the module whose route was called, taken from the mount point
	// rather than parsed out of the path.
	Module string `json:"module"`

	Method string `json:"method"`

	// Path is absolute, with the concrete parameters, so a reader can tell
	// which service was published without also holding the route table.
	Path string `json:"path"`

	// Source is the client address the change is attributed to.
	//
	// When the request arrived over loopback — which is the normal case for
	// the WebUI, because it is reached through ingress at
	// `olr.<domain>` — this is the last X-Forwarded-For hop instead of
	// 127.0.0.1. The immediate peer is checked first, so a request that did
	// not come through a local proxy keeps its own address and cannot borrow
	// one from a header.
	Source string `json:"source,omitempty"`

	// ForwardedFor is the header as received, when there was one. Kept beside
	// the attribution rather than folded into it, because the header is
	// asserted by whoever connected and only trustworthy as far as the hop
	// that appended to it.
	ForwardedFor string `json:"forwarded_for,omitempty"`

	UserAgent string `json:"user_agent,omitempty"`

	// Status is the HTTP response the change produced. 2xx means the box
	// accepted it, 4xx that it refused (validation, confirmation, stale
	// config), 5xx that it failed after accepting.
	Status int `json:"status"`

	// Outcome is Status in the three words a reader actually thinks in.
	Outcome string `json:"outcome"`

	// DryRun marks a mutation that was planned rather than applied. Without
	// it the record would claim a change that the operator only previewed.
	DryRun bool `json:"dry_run,omitempty"`

	DurationMS int64 `json:"duration_ms,omitempty"`
}

// Audit appends entries and reads them back.
//
// A nil *Audit is usable and does nothing, so a caller never has to ask
// whether the record is switched on.
type Audit struct {
	dir  string
	max  int64
	keep int
	mu   sync.Mutex
}

// NewAudit returns a record rooted at dir.
func NewAudit(dir string) *Audit {
	return &Audit{dir: dir, max: auditMaxBytes, keep: auditSegments}
}

// Dir is where the record lives, for a status page or a bug report.
func (a *Audit) Dir() string {
	if a == nil {
		return ""
	}
	return a.dir
}

// Record appends one entry.
//
// A failure is reported and swallowed. An audit that can fail a change is an
// audit that can take the router down — and the alternative to a missing line
// is not a lost change, it is a change the operator was told succeeded or
// refused, which the response already says.
func (a *Audit) Record(e AuditEntry) {
	if a == nil {
		return
	}
	if e.At.IsZero() {
		e.At = time.Now()
	}
	if e.Outcome == "" {
		e.Outcome = auditOutcome(e.Status)
	}
	line, err := json.Marshal(e)
	if err != nil {
		slog.Warn("audit entry not encoded", "error", err)
		return
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	if err := os.MkdirAll(a.dir, 0o700); err != nil {
		slog.Warn("audit directory unavailable", "dir", a.dir, "error", err)
		return
	}
	a.rotateLocked()

	path := filepath.Join(a.dir, AuditFile)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		slog.Warn("audit entry not written", "path", path, "error", err)
		return
	}
	defer f.Close()
	if _, err := f.Write(append(line, '\n')); err != nil {
		slog.Warn("audit entry not written", "path", path, "error", err)
	}
}

// rotateLocked rolls the current segment when it has grown past the limit.
//
// Rename is atomic on one filesystem, so a reader never sees a half-written
// segment under the current name; the worst case is that one entry lands in
// the segment that was just rolled.
func (a *Audit) rotateLocked() {
	path := filepath.Join(a.dir, AuditFile)
	info, err := os.Stat(path)
	if err != nil || info.Size() < a.max {
		return
	}
	// Oldest first, so each rename frees the name the next one needs. Renaming
	// onto an existing file replaces it, which is how the oldest segment is
	// dropped without a separate delete.
	for i := a.keep - 1; i >= 1; i-- {
		from := fmt.Sprintf("%s.%d", path, i)
		to := fmt.Sprintf("%s.%d", path, i+1)
		if _, err := os.Stat(from); err == nil {
			_ = os.Rename(from, to)
		}
	}
	_ = os.Rename(path, path+".1")
}

// Read returns the newest entries first, up to limit.
//
// Reads walk segments from newest to oldest until the limit is met, so a
// caller asking for ten entries does not pay for the whole history.
func (a *Audit) Read(limit int) ([]AuditEntry, error) {
	if a == nil {
		return nil, nil
	}
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}

	var out []AuditEntry
	for segment := 0; segment <= a.keep && len(out) < limit; segment++ {
		path := filepath.Join(a.dir, AuditFile)
		if segment > 0 {
			path += "." + strconv.Itoa(segment)
		}
		entries, err := readAuditSegment(path)
		if os.IsNotExist(err) {
			// No such segment yet: a box that has just started, or a history
			// shorter than the limit. Either way there is nothing older.
			break
		}
		if err != nil {
			return out, err
		}
		for i := len(entries) - 1; i >= 0 && len(out) < limit; i-- {
			out = append(out, entries[i])
		}
	}
	return out, nil
}

// readAuditSegment decodes one file. A malformed line is skipped rather than
// failing the read: a record that refuses to be read because one write was
// torn is worse than a gap the reader can see.
func readAuditSegment(path string) ([]AuditEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var out []AuditEntry
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var e AuditEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			continue
		}
		out = append(out, e)
	}
	return out, scanner.Err()
}

// auditSource attributes a request to an address.
//
// A local proxy (ingress) is the reason for the loopback branch: the WebUI is
// usually reached through Caddy, so the TCP peer is 127.0.0.1 and the client
// only exists in X-Forwarded-For. Caddy appends to that header rather than
// replacing it, so the last hop is the one it observed; earlier entries are
// whatever the client sent.
func auditSource(r *http.Request) (source, forwarded string) {
	host := r.RemoteAddr
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	// The unix socket has no peer address at all, and reports a name rather
	// than an address. Recording that as a "source" would read as a client.
	if net.ParseIP(host) == nil {
		return "", ""
	}
	forwarded = r.Header.Get("X-Forwarded-For")
	if ip := net.ParseIP(host); ip.IsLoopback() && forwarded != "" {
		parts := strings.Split(forwarded, ",")
		return strings.TrimSpace(parts[len(parts)-1]), forwarded
	}
	return host, ""
}

// auditOutcome is a status code in the words the record uses.
func auditOutcome(status int) string {
	switch {
	case status >= 500:
		return "failed"
	case status >= 400:
		return "refused"
	default:
		return "succeeded"
	}
}

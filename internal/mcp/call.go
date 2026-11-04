package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// Executing a tool call, which is one request against olrd's own API handler.

// toolResult is what tools/call answers with.
type toolResult struct {
	Content []content `json:"content"`

	// IsError marks a call the *tool* refused, as opposed to one the protocol
	// refused. The distinction is not pedantry: a JSON-RPC error is handled by
	// the client and never reaches the model, so an invalid pool prefix
	// reported that way would leave the agent knowing only that something went
	// wrong, with no idea what to fix. Reported here it is text the model reads
	// and can act on.
	IsError bool `json:"isError,omitempty"`
}

type content struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func textResult(text string, isError bool) toolResult {
	return toolResult{
		Content: []content{{Type: "text", Text: text}},
		IsError: isError,
	}
}

// call runs one tool against the API.
func (s *Server) call(ctx context.Context, t tool, args map[string]json.RawMessage) toolResult {
	req, err := s.request(ctx, t, args)
	if err != nil {
		return textResult(err.Error(), true)
	}

	rec := newRecorder()
	s.api.ServeHTTP(rec, req)

	body := rec.body.Bytes()
	if rec.code >= 300 {
		return textResult(describeFailure(rec.code, body), true)
	}

	// Passed through exactly as the API produced them. An agent asking what
	// the box thinks and an operator running curl against the same route get
	// the same bytes, which is the property that makes the two surfaces
	// checkable against each other.
	if len(bytes.TrimSpace(body)) == 0 {
		return textResult("The request succeeded and returned nothing.", false)
	}
	return textResult(string(body), false)
}

// request turns a tool call into an HTTP request against the API.
func (s *Server) request(ctx context.Context, t tool, args map[string]json.RawMessage) (*http.Request, error) {
	query := url.Values{}
	for _, p := range t.route.Query {
		raw, ok := args[p.Name]
		if !ok {
			continue
		}
		value, err := queryValue(raw)
		if err != nil {
			return nil, fmt.Errorf("the argument %s could not be read: %w", p.Name, err)
		}
		query.Set(p.Name, value)
	}

	var body io.Reader
	if t.route.Body != core.BodyNone {
		if raw, ok := args[configKey]; ok && len(bytes.TrimSpace(raw)) > 0 {
			body = bytes.NewReader(raw)
		}
	}

	target, err := requestPath(t.route.Path, args)
	if err != nil {
		return nil, err
	}
	if encoded := query.Encode(); encoded != "" {
		target += "?" + encoded
	}

	// The host is a placeholder. This request never reaches a socket: it is
	// handed straight to olrd's own handler, in this process. That is what
	// makes the MCP surface an API client in fact and not only in intent —
	// there is no path from here to a module that does not cross the same
	// validation, the same apply lock, and the same event publication that the
	// WebUI and the CLI cross.
	req, err := http.NewRequestWithContext(ctx, t.route.Method, "http://olr"+target, body)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req, nil
}

// queryValue renders one JSON argument as a query-string value.
//
// A string argument is unquoted rather than re-encoded, because ?limit="50" is
// not what any handler parses. Everything else is passed through as written.
func queryValue(raw json.RawMessage) (string, error) {
	var decoded any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return "", err
	}
	switch v := decoded.(type) {
	case string:
		return v, nil
	case bool:
		return strconv.FormatBool(v), nil
	case float64:
		// Rendered without an exponent or a trailing .0, so that an integer
		// argument arrives at the handler as the integer it was written as.
		return strconv.FormatFloat(v, 'f', -1, 64), nil
	default:
		return string(raw), nil
	}
}

// describeFailure turns a refused request into something a model can act on.
//
// The addressed problems matter more here than anywhere else. An agent told
// only "invalid dhcp configuration" will retry the same document; told
// "pools.0.range_end: outside the pool's subnet" it fixes the field. Those are
// the same bytes the WebUI attaches to a form field and the CLI prints under
// its error — one envelope, read three ways (core.ErrorBody).
func describeFailure(status int, body []byte) string {
	var envelope struct {
		Error core.ErrorBody `json:"error"`
	}
	if err := json.Unmarshal(body, &envelope); err == nil && envelope.Error.Message != "" {
		return "The request was refused (HTTP " + strconv.Itoa(status) + "): " + envelope.Error.String()
	}

	// Not our envelope. Say what little there is rather than inventing a
	// reason for it.
	text := strings.TrimSpace(string(body))
	if text == "" {
		text = http.StatusText(status)
	}
	return "The request failed (HTTP " + strconv.Itoa(status) + "): " + text
}

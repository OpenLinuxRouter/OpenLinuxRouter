package mcp

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

// pathParameter reads a named ServeMux wildcard. {$} marks a trailing slash,
// rather than an argument supplied by the caller.
func pathParameter(segment string) (name string, rest bool) {
	if !strings.HasPrefix(segment, "{") || !strings.HasSuffix(segment, "}") || segment == "{$}" {
		return "", false
	}
	name = strings.TrimSuffix(strings.TrimPrefix(segment, "{"), "}")
	name, rest = strings.CutSuffix(name, "...")
	return name, rest
}

// requestPath substitutes arguments without letting a single-segment wildcard
// introduce more URL segments. Catch-alls explicitly accept multiple segments;
// each is escaped separately and must survive ServeMux's path cleaning.
func requestPath(template string, args map[string]json.RawMessage) (string, error) {
	segments := strings.Split(template, "/")
	for i, segment := range segments {
		if segment == "{$}" {
			segments[i] = ""
			continue
		}
		name, rest := pathParameter(segment)
		if name == "" {
			continue
		}
		raw, ok := args[name]
		if !ok {
			return "", fmt.Errorf("the path argument %s is required", name)
		}
		var value string
		if err := json.Unmarshal(raw, &value); err != nil {
			return "", fmt.Errorf("the path argument %s must be a string: %w", name, err)
		}
		if value == "" {
			return "", fmt.Errorf("the path argument %s must not be empty", name)
		}
		// ServeMux treats a decoded "/" segment as its trailing-slash marker,
		// even when it was escaped, and cannot match it to a single wildcard.
		if !rest && value == "/" {
			return "", fmt.Errorf("the path argument %s must not be a single slash", name)
		}
		parts := []string{value}
		if rest {
			parts = strings.Split(value, "/")
		}
		for j, part := range parts {
			if part == "" || part == "." || part == ".." {
				return "", fmt.Errorf("the path argument %s must not contain empty or dot path segments", name)
			}
			parts[j] = url.PathEscape(part)
		}
		segments[i] = strings.Join(parts, "/")
	}
	return strings.Join(segments, "/"), nil
}

package codegraph

import "strings"

// extractNodeName extracts the symbol name from an AGE node string.
//
// AGE serialises nodes as either bare names or JSON-like vertex strings, e.g.:
//
//	{"id":42, "label":"Symbol", "properties":{"name":"FooBar", ...}}
//
// The function looks for a `"name":` JSON key first; when not found it returns
// the raw trimmed string if it contains no braces (already a plain name).
func extractNodeName(raw string) string {
	raw = strings.TrimSpace(raw)
	const nameKey = `"name":`
	idx := strings.Index(raw, nameKey)
	if idx >= 0 {
		rest := strings.TrimSpace(raw[idx+len(nameKey):])
		if len(rest) > 0 && rest[0] == '"' {
			rest = rest[1:]
			end := strings.Index(rest, `"`)
			if end > 0 {
				return rest[:end]
			}
		}
	}
	// Plain name — no braces.
	if !strings.ContainsAny(raw, "{}") && raw != "" {
		return raw
	}
	return ""
}

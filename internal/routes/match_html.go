package routes

import (
	"strings"

	"github.com/anatolykoptev/go-code/internal/parser/preproc"
)

// HTMLMatcher extracts htmx HTTP routes from HTML/Go-template files.
// It operates on the ORIGINAL src bytes (not StripGoTemplate output) so that
// Go template actions embedded in hx-* attribute values are preserved for
// normalisation by normalizeHtmxPath.
type HTMLMatcher struct{}

func init() {
	m := &HTMLMatcher{}
	Register(m)
}

// Language returns the language identifier for this matcher.
func (h *HTMLMatcher) Language() string { return "html" }

// Match scans HTML source for htmx URL-emitting attributes and returns
// client-side routes with Side="client" and Framework="htmx".
func (h *HTMLMatcher) Match(source []byte) []Route {
	refs := preproc.ScanHtmxRefs(source)
	out := make([]Route, 0, len(refs))
	for _, r := range refs {
		out = append(out, Route{
			Method:    r.Method,
			Path:      normalizeHtmxPath(r.URL),
			RawPath:   r.URL,
			Framework: "htmx",
			Side:      "client",
			Line:      uint32(r.StartLine), //nolint:gosec // line numbers are small positive ints
		})
	}
	return out
}

// normalizeHtmxPath normalises an htmx attribute URL to a canonical path
// suitable for cross-stack route matching. It performs two transformations:
//
//  1. Go template actions ({{...}}) are replaced with "*". This makes
//     htmx "/admin/hunt/job/{{.ID}}/rate" equivalent to the Go-server-side
//     mux pattern "/admin/hunt/job/{id}/rate", which NormalizePath turns into
//     "/admin/hunt/job/*/rate" via its {param}→* rule.
//
//  2. The resulting string is passed to NormalizePath, which handles
//     {param}→*, :param→*, scheme-strip, leading-slash, and double-slash
//     cleanup — the same normalisation applied to server-side Go routes.
//
// Query strings: the "?" portion is preserved as-is; template actions inside
// query strings (e.g. "?page={{add .Page 1}}") are also collapsed to "*".
// This aligns with the convention that server-side query parameters are not
// matched by route patterns.
func normalizeHtmxPath(raw string) string {
	// Step 1: replace every {{...}} span with *.
	//
	// Strategy: scan for "{{", then scan forward counting brace depth until
	// the matching "}}". This correctly handles nested braces inside template
	// actions (e.g. {{if .X}}...{{end}} is treated as one opaque action by the
	// attr walker, but query params like {{add .Page 1}} contain no inner
	// braces so simple scanning works; we use depth counting for correctness).
	var sb strings.Builder
	sb.Grow(len(raw))
	i := 0
	for i < len(raw) {
		if i+1 < len(raw) && raw[i] == '{' && raw[i+1] == '{' {
			// Found opening {{. Scan for closing }}.
			j := i + 2
			depth := 1
			for j < len(raw) {
				if j+1 < len(raw) && raw[j] == '{' && raw[j+1] == '{' {
					depth++
					j += 2
					continue
				}
				if j+1 < len(raw) && raw[j] == '}' && raw[j+1] == '}' {
					depth--
					j += 2
					if depth == 0 {
						break
					}
					continue
				}
				j++
			}
			// Emit a single * for the entire {{...}} span.
			sb.WriteByte('*')
			i = j
			continue
		}
		sb.WriteByte(raw[i])
		i++
	}

	// Step 2: pass through NormalizePath to handle {param}→*, :param→*,
	// scheme-strip, leading-slash, double-slash, and trailing-slash cleanup.
	return NormalizePath(sb.String())
}

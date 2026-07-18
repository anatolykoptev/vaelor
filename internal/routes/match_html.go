package routes

import (
	"strings"

	"github.com/anatolykoptev/vaelor/internal/parser/preproc"
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
			Handler:   r.EnclosingTemplate, // {{define "X"}} scope → FETCHES edge in AGE graph
		})
	}
	return out
}

// normalizeHtmxPath normalises an htmx attribute URL to a canonical path
// suitable for cross-stack route matching. It performs two transformations:
//
//  1. Go template actions ({{...}}) are replaced with "*". Block actions
//     ({{if}}, {{range}}, {{with}}, {{block}}, {{define}}) and everything up
//     to their matching {{end}} are collapsed to a SINGLE "*" so that
//     /x/{{if .Y}}foo{{else}}bar{{end}}/y → /x/*/y (not /x/*foo*bar*/y).
//     Atomic actions ({{.X}}, {{$x}}, {{add .Page 1}}) collapse to one "*".
//
//  2. The resulting string is passed to NormalizePath, which handles
//     {param}→*, :param→*, scheme-strip, leading-slash, and double-slash
//     cleanup — the same normalisation applied to server-side Go routes.
//
// Query strings: the "?" portion is preserved as-is; template actions inside
// query strings (e.g. "?page={{add .Page 1}}") are also collapsed to "*".
// This aligns with the convention that server-side query parameters are not
// matched by route patterns. Phase B must use path-prefix comparison when
// matching htmx routes against server-side patterns.
func normalizeHtmxPath(raw string) string {
	// Step 1: block-aware collapse of {{...}} spans.
	//
	// When we encounter a block-opening action (if/range/with/block/define),
	// scanBlockEnd advances past the entire balanced block (including nested
	// blocks) and returns the position after the closing {{end}}. The whole
	// range collapses to one "*". Atomic actions collapse individually.
	var sb strings.Builder
	sb.Grow(len(raw))
	i := 0
	for i < len(raw) {
		if i+1 < len(raw) && raw[i] == '{' && raw[i+1] == '{' {
			// Scan to closing }} of this single action.
			actionEnd := scanActionClose(raw, i+2)
			keyword := extractActionKeyword(raw[i+2 : actionEnd-2])
			if isBlockOpenKeyword(keyword) {
				// Block action: consume until the matching {{end}}, collapse all to *.
				i = scanBlockEnd(raw, actionEnd)
			} else {
				// Atomic action: collapse this single {{...}} to *.
				i = actionEnd
			}
			sb.WriteByte('*')
			continue
		}
		sb.WriteByte(raw[i])
		i++
	}

	// Step 2: pass through NormalizePath to handle {param}→*, :param→*,
	// scheme-strip, leading-slash, double-slash, and trailing-slash cleanup.
	return NormalizePath(sb.String())
}

// scanActionClose returns the position immediately after the closing "}}" of
// the action that starts at raw[from:]. from should point to the first byte
// after the opening "{{". Handles nested "{{" depth so that actions containing
// literal braces (rare in htmx URLs) are handled safely.
func scanActionClose(raw string, from int) int {
	depth := 1
	j := from
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
				return j
			}
			continue
		}
		j++
	}
	return len(raw)
}

// extractActionKeyword returns the first whitespace-delimited word from an
// action body (the text between "{{" and "}}"). Used to detect block keywords.
func extractActionKeyword(body string) string {
	body = strings.TrimSpace(body)
	// Strip leading trim marker "-".
	body = strings.TrimPrefix(body, "-")
	body = strings.TrimSpace(body)
	sp := strings.IndexAny(body, " \t\n\r")
	if sp < 0 {
		return body
	}
	return body[:sp]
}

// isBlockOpenKeyword reports whether kw is a Go template block-opening keyword
// that requires a matching {{end}}.
func isBlockOpenKeyword(kw string) bool {
	switch kw {
	case "if", "range", "with", "block", "define":
		return true
	}
	return false
}

// scanBlockEnd scans raw starting at from (which should be immediately after
// the closing "}}" of an opening block action) and returns the position
// immediately after the matching "{{end}}". Nested blocks are handled via a
// depth counter so that {{if}}...{{if}}...{{end}}...{{end}} is consumed fully.
func scanBlockEnd(raw string, from int) int {
	depth := 1 // we are already inside one open block
	i := from
	for i < len(raw) {
		if i+1 < len(raw) && raw[i] == '{' && raw[i+1] == '{' {
			actionEnd := scanActionClose(raw, i+2)
			keyword := extractActionKeyword(raw[i+2 : actionEnd-2])
			if isBlockOpenKeyword(keyword) {
				depth++
			} else if keyword == "end" {
				depth--
				if depth == 0 {
					return actionEnd
				}
			}
			i = actionEnd
			continue
		}
		i++
	}
	return len(raw)
}

package preproc

import "bytes"

// HtmxRef records a single URL-emitting hx-* attribute found in an HTML file.
// The URL field contains the ORIGINAL bytes from src (Go template actions such
// as {{.ID}} are preserved for downstream normalisation by the routes layer).
type HtmxRef struct {
	Method            string // "GET", "POST", "PUT", "DELETE", or "PATCH"
	URL               string // raw attribute value including any {{...}} actions
	StartLine         int    // 1-based line of the opening tag
	EndLine           int    // 1-based line of the attribute value end (same as StartLine for single-line)
	EnclosingTemplate string // name from the nearest enclosing {{define "X"}} block; "" if at top level
}

// hxMethodAttrs maps hx-* attribute name suffixes to their HTTP methods.
// Only URL-emitting verbs are listed — hx-on:*, hx-target, hx-swap, etc. are
// intentionally excluded.
var hxMethodAttrs = []struct {
	suffix []byte // e.g. []byte("hx-get")
	method string
}{
	{[]byte("hx-get"), "GET"},
	{[]byte("hx-post"), "POST"},
	{[]byte("hx-put"), "PUT"},
	{[]byte("hx-delete"), "DELETE"},
	{[]byte("hx-patch"), "PATCH"},
}

// ScanHtmxRefs scans src for htmx URL-emitting attributes (hx-get, hx-post,
// hx-put, hx-delete, hx-patch) and returns one HtmxRef per occurrence.
//
// Contract:
//   - src MUST be the original file bytes, NOT the cleaned output of
//     StripGoTemplate. The walker needs raw bytes so that Go template actions
//     ({{.ID}}, {{add .Page 1}}) inside attribute values are preserved for
//     the route normaliser in the routes layer.
//   - <script> and <style> block contents are skipped (same as scanTemplateRefs).
//   - HTML comments <!-- ... --> are skipped.
//   - hx-on:* / hx-on::* attributes are not URL-emitting and are ignored.
//   - Both double-quoted and single-quoted attribute values are handled.
func ScanHtmxRefs(src []byte) []HtmxRef {
	w := &htmxWalker{
		src:   src,
		skips: collectSkipRanges(src),
		line:  1,
	}
	var refs []HtmxRef
	for w.i < len(src) {
		if ref, ok := w.step(); ok {
			refs = append(refs, ref)
		}
	}
	return refs
}

// htmxWalker holds the mutable byte-walker state for ScanHtmxRefs.
//
// defineStack tracks open {{define "X"}} blocks AND non-define block keywords
// ({{range}}/{{if}}/{{with}}/{{block}}) so {{end}} pops correctly even when
// blocks nest inside a define. Non-define blocks push "" so the pop accounting
// matches Go template's flat block grammar. currentTemplate walks the stack
// top→bottom returning the first non-empty name, preserving "innermost define
// wins" semantics while letting nested non-define blocks transparently inherit
// the enclosing define — the dominant real-world htmx pattern:
//
//	{{define "list"}}{{range .Items}}<button hx-get="/x">{{end}}{{end}}
type htmxWalker struct {
	src         []byte
	skips       []skipRange // opaque type from astro.go
	i           int
	line        int
	defineStack []string
}

// currentTemplate returns the innermost define name, or "" if at top level.
func (w *htmxWalker) currentTemplate() string {
	for k := len(w.defineStack) - 1; k >= 0; k-- {
		if w.defineStack[k] != "" {
			return w.defineStack[k]
		}
	}
	return ""
}

// step advances the walker by one logical unit and returns an HtmxRef when one
// is found at the current position. The walker guarantees w.i advances on every
// call so the outer loop always terminates.
func (w *htmxWalker) step() (HtmxRef, bool) {
	b := w.src[w.i]

	if b == '\n' {
		w.line++
		w.i++
		return HtmxRef{}, false
	}
	if inSkipRanges(w.i, w.skips) {
		w.i++
		return HtmxRef{}, false
	}
	if w.skipComment() {
		return HtmxRef{}, false
	}
	if isGoTemplateStart(w.src, w.i) {
		w.advanceTemplateAction()
		return HtmxRef{}, false
	}
	if b != 'h' || !isAttrBoundary(w.src, w.i) {
		w.i++
		return HtmxRef{}, false
	}
	return w.matchAttr()
}

// skipComment handles "<!-- ... -->". Returns true when src[w.i] is "<!--".
// On an unclosed comment, advances w.i to end-of-src so the outer loop exits.
func (w *htmxWalker) skipComment() bool {
	if !bytes.HasPrefix(w.src[w.i:], []byte("<!--")) {
		return false
	}
	end := bytes.Index(w.src[w.i:], []byte("-->"))
	if end < 0 {
		w.i = len(w.src)
		return true
	}
	advance := end + 3
	for _, c := range w.src[w.i : w.i+advance] {
		if c == '\n' {
			w.line++
		}
	}
	w.i += advance
	return true
}

// advanceTemplateAction processes one Go template action starting at w.src[w.i].
// It updates w.defineStack and advances w.i past the closing "}}".
// On an unclosed action, w.i is advanced by 2 (past "{{").
func (w *htmxWalker) advanceTemplateAction() {
	actionEnd := findActionClose(w.src, w.i+2)
	if actionEnd < 0 {
		w.i += 2
		return
	}
	body := actionBody(w.src, w.i, actionEnd)
	keyword, name := parseActionKeyword(body)
	switch keyword {
	case "define", "range", "if", "with", "block":
		// Push a new scope. Only "define" carries a meaningful name;
		// other block keywords push "" so that {{end}} pops correctly
		// when nested inside a define.
		if keyword == "define" {
			w.defineStack = append(w.defineStack, name)
		} else {
			w.defineStack = append(w.defineStack, "")
		}
	case "end":
		if len(w.defineStack) > 0 {
			w.defineStack = w.defineStack[:len(w.defineStack)-1]
		}
	}
	for _, c := range w.src[w.i:actionEnd] {
		if c == '\n' {
			w.line++
		}
	}
	w.i = actionEnd
}

// matchAttr tries to match an hx-METHOD attribute at w.src[w.i].
// On a match it advances w.i past the value and returns (ref, true).
// On no match it advances w.i by 1 and returns (HtmxRef{}, false).
func (w *htmxWalker) matchAttr() (HtmxRef, bool) {
	for _, attr := range hxMethodAttrs {
		if !bytes.HasPrefix(w.src[w.i:], attr.suffix) {
			continue
		}
		j := w.i + len(attr.suffix)

		// Must be followed by '=' (optionally with whitespace).
		for j < len(w.src) && (w.src[j] == ' ' || w.src[j] == '\t') {
			j++
		}
		if j >= len(w.src) || w.src[j] != '=' {
			continue
		}
		j++ // skip '='

		for j < len(w.src) && (w.src[j] == ' ' || w.src[j] == '\t') {
			j++
		}
		if j >= len(w.src) {
			continue
		}

		url, newJ, startLine, endLine := extractAttrValue(w.src, j, w.line)
		w.line = endLine
		w.i = newJ
		return HtmxRef{
			Method:            attr.method,
			URL:               url,
			StartLine:         startLine,
			EndLine:           endLine,
			EnclosingTemplate: w.currentTemplate(),
		}, url != ""
	}
	w.i++
	return HtmxRef{}, false
}

// extractAttrValue reads an attribute value starting at src[j] (the character
// immediately after '=' and any whitespace). Handles both quoted
// (single/double) and unquoted values. Returns the raw value string (which
// may contain {{...}} Go template actions), the position just past the
// consumed value, and the start/end line numbers of the value.
func extractAttrValue(src []byte, j, line int) (url string, newJ, startLine, endLine int) {
	if src[j] != '"' && src[j] != '\'' {
		// Unquoted attribute value — read until whitespace or '>'.
		valStart := j
		for j < len(src) && src[j] != ' ' && src[j] != '\t' && src[j] != '>' && src[j] != '\n' {
			j++
		}
		return string(src[valStart:j]), j, line, line
	}

	// Quoted value: find closing quote, preserving {{...}} actions.
	quote := src[j]
	j++
	valStart := j
	startLine = line
	endLine = line
	for j < len(src) {
		c := src[j]
		if c == '\n' {
			endLine++
			line++
			j++
			continue
		}
		if c == quote {
			break
		}
		// Skip {{...}} actions as opaque spans (no need to count braces
		// since the closing quote terminates the value, not braces).
		j++
	}
	url = string(src[valStart:j])
	if j < len(src) {
		j++ // consume closing quote
	}
	return url, j, startLine, endLine
}

// isGoTemplateStart reports whether src[i:] begins with "{{".
func isGoTemplateStart(src []byte, i int) bool {
	return src[i] == '{' && i+1 < len(src) && src[i+1] == '{'
}

// isAttrBoundary reports whether position i is a valid HTML attribute start:
// the preceding byte must be whitespace or '<' (first-attr position).
// Returns true for i==0 (start of file).
func isAttrBoundary(src []byte, i int) bool {
	if i == 0 {
		return true
	}
	prev := src[i-1]
	return prev == ' ' || prev == '\t' || prev == '\n' || prev == '\r' || prev == '<'
}

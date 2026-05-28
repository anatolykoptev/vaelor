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
func ScanHtmxRefs(src []byte) []HtmxRef { //nolint:gocognit // byte-walker inherently sequential; matches scanTemplateRefs pattern in astro_refs.go
	skips := collectSkipRanges(src)

	// defineStack tracks open {{define "X"}} blocks so we can stamp the
	// enclosing template name onto each HtmxRef.  Go html/template does not
	// allow nested define blocks, but we use a stack to be resilient; only
	// the innermost name (top of stack) is recorded.
	var defineStack []string

	// currentTemplate returns the innermost open define name, or "" at top level.
	currentTemplate := func() string {
		if len(defineStack) == 0 {
			return ""
		}
		return defineStack[len(defineStack)-1]
	}

	var refs []HtmxRef
	i := 0
	line := 1

	for i < len(src) {
		b := src[i]

		// Track line numbers.
		if b == '\n' {
			line++
			i++
			continue
		}

		// Skip bytes inside <script>/<style> blocks.
		if inSkipRanges(i, skips) {
			i++
			continue
		}

		// HTML comment: <!-- ... -->
		if bytes.HasPrefix(src[i:], []byte("<!--")) {
			end := bytes.Index(src[i:], []byte("-->"))
			if end < 0 {
				break
			}
			advance := end + 3
			for _, c := range src[i : i+advance] {
				if c == '\n' {
					line++
				}
			}
			i += advance
			continue
		}

		// Go template action: {{...}}
		// Track {{define "X"}} and {{end}} to maintain the enclosing-template
		// scope stack.  We process this BEFORE the hx-* check so that a
		// {{define}} on the same line as an hx-* attribute is handled first.
		// Note: skip-ranges have already been checked above; {{define}} inside
		// a <script> block is therefore ignored (correct, it can't happen in
		// valid Go templates anyway).
		if b == '{' && i+1 < len(src) && src[i+1] == '{' {
			actionEnd := findActionClose(src, i+2)
			if actionEnd < 0 {
				// Unclosed action — skip past the opening "{{" and continue.
				i += 2
				continue
			}
			body := actionBody(src, i, actionEnd)
			keyword, name := parseActionKeyword(body)
			switch keyword {
			case "define", "range", "if", "with", "block":
				// Push a new scope.  Only "define" carries a meaningful name;
				// other block keywords push "" so that {{end}} pops correctly
				// if they are nested inside a define.
				if keyword == "define" {
					defineStack = append(defineStack, name)
				} else {
					defineStack = append(defineStack, "")
				}
			case "end":
				if len(defineStack) > 0 {
					defineStack = defineStack[:len(defineStack)-1]
				}
			}
			// Count newlines consumed by the action span.
			for _, c := range src[i:actionEnd] {
				if c == '\n' {
					line++
				}
			}
			i = actionEnd
			continue
		}

		// Look for the start of an hx-* attribute. We only care about bytes
		// that could start "hx-" so fast-path everything else.
		if b != 'h' {
			i++
			continue
		}

		// Left-boundary guard: 'h' must sit at attribute-slot position.
		// Previous byte must be whitespace or '<' (very-first-attr position).
		// Without this guard, occurrences inside other attribute values fire:
		//   <input name="hx-get=test">     → emits GET "test\""
		//   <button onclick="x.hx-get=1">  → emits GET "1\""
		// Both pollute Phase B AGE graph with malformed URLs.
		if i > 0 {
			prev := src[i-1]
			if prev != ' ' && prev != '\t' && prev != '\n' && prev != '\r' && prev != '<' {
				i++
				continue
			}
		}

		// Check for each hx-METHOD attr.
		matched := false
		for _, attr := range hxMethodAttrs {
			if !bytes.HasPrefix(src[i:], attr.suffix) {
				continue
			}
			afterAttr := i + len(attr.suffix)
			// Must be followed by '=' (optionally with whitespace).
			j := afterAttr
			for j < len(src) && (src[j] == ' ' || src[j] == '\t') {
				j++
			}
			if j >= len(src) || src[j] != '=' {
				// Not an attribute assignment — could be a prefix of another attr
				// (unlikely but guard it).
				continue
			}
			j++ // skip '='

			// Skip optional whitespace after '='.
			for j < len(src) && (src[j] == ' ' || src[j] == '\t') {
				j++
			}
			if j >= len(src) {
				continue
			}

			// Read quoted value.
			var quote byte
			if src[j] == '"' || src[j] == '\'' {
				quote = src[j]
				j++
			} else {
				// Unquoted attribute value — read until whitespace or '>'.
				valStart := j
				for j < len(src) && src[j] != ' ' && src[j] != '\t' && src[j] != '>' && src[j] != '\n' {
					j++
				}
				url := string(src[valStart:j])
				if url != "" {
					refs = append(refs, HtmxRef{
						Method:            attr.method,
						URL:               url,
						StartLine:         line,
						EndLine:           line,
						EnclosingTemplate: currentTemplate(),
					})
				}
				// Advance i to just after attr name so we don't re-scan.
				i = j
				matched = true
				break
			}

			// Quoted value: find closing quote, preserving {{...}} actions.
			valStart := j
			startLine := line
			endLine := line
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
			url := string(src[valStart:j])
			if j < len(src) {
				j++ // consume closing quote
			}
			if url != "" {
				refs = append(refs, HtmxRef{
					Method:            attr.method,
					URL:               url,
					StartLine:         startLine,
					EndLine:           endLine,
					EnclosingTemplate: currentTemplate(),
				})
			}
			i = j
			matched = true
			break
		}

		if !matched {
			i++
		}
	}

	return refs
}

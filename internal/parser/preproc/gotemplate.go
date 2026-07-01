package preproc

import "bytes"

// GoTemplateDefine records a {{define "Name"}} ... {{end}} block found at the
// top level of a Go html/template file. StartLine and EndLine are 1-based.
type GoTemplateDefine struct {
	Name      string
	StartLine int
	EndLine   int
}

// StripGoTemplate scans src for Go html/template actions ({{...}}) and returns:
//
//   - cleaned: a copy of src where every {{...}} action span is replaced with
//     an equal number of space bytes. This preserves line/column positions for
//     any downstream byte-level scanners (htmx attribute walker, etc.).
//
//   - defines: the list of top-level {{define "Name"}} ... {{end}} blocks,
//     with 1-based start/end line numbers derived from the original src.
//
// Strategy: balanced-brace byte scanner.
//
//   - "{{" opens an action; find matching "}}". Account for trim markers "{{-"
//     and "-}}".
//   - "{{/*" ... "*/}}" is a comment block; scan content without nesting.
//   - Block actions (define, range, if, with, block) push onto a stack; the
//     matching "{{end}}" pops the stack.
//   - Top-level "{{define}}" blocks (depth 0 before pushing) are recorded.
//
// Limitations (sufficient for Wave 1):
//
//   - Strings inside actions (e.g. `{{ printf "%s" "fmt" }}`) are skipped as
//     part of the action span — their content does not affect brace counting
//     because the outer {{...}} scanner does not recurse into Go expressions.
//   - Nested pipeline braces `{{ if eq .X "{" }}` are NOT handled; in
//     practice Go template action bodies rarely contain literal braces and the
//     scanner falls back to consuming until the next "}}" at the same depth.
func StripGoTemplate(src []byte) (cleaned []byte, defines []GoTemplateDefine) { //nolint:gocognit // byte-walker inherently sequential; matches scanTemplateRefs pattern in astro_refs.go
	out := make([]byte, len(src))
	copy(out, src)

	// blockStack tracks open block actions. We only need to know the action
	// keyword and the start line of the outermost define to record its extent.
	type blockFrame struct {
		keyword   string // "define", "range", "if", "with", "block"
		startLine int    // 1-based line of the "{{define" open
		name      string // template name for define frames; "" otherwise
	}
	var stack []blockFrame

	i := 0
	for i < len(src) {
		// Look for "{{".
		idx := bytes.Index(src[i:], []byte("{{"))
		if idx < 0 {
			break
		}
		actionStart := i + idx

		// Check for comment: {{/* ... */}}
		if bytes.HasPrefix(src[actionStart:], []byte("{{/*")) {
			end := bytes.Index(src[actionStart+4:], []byte("*/}}"))
			var actionEnd int
			if end < 0 {
				actionEnd = len(src)
			} else {
				actionEnd = actionStart + 4 + end + 4
			}
			blankRange(out, actionStart, actionEnd)
			i = actionEnd
			continue
		}

		// Find the closing "}}". Account for trim-right marker "-}}".
		closeIdx := findActionClose(src, actionStart+2)
		var actionEnd int
		if closeIdx < 0 {
			actionEnd = len(src)
		} else {
			actionEnd = closeIdx
		}

		// Extract the action body (between {{ and }}).
		body := actionBody(src, actionStart, actionEnd)
		keyword, name := parseActionKeyword(body)

		startLine := lineNumber(src, actionStart)

		switch keyword {
		case "define", "range", "if", "with", "block", "template":
			if keyword != "template" {
				stack = append(stack, blockFrame{
					keyword:   keyword,
					startLine: startLine,
					name:      name,
				})
			}
		case "end":
			if len(stack) > 0 {
				top := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				// Record top-level define completions.
				if top.keyword == "define" && len(stack) == 0 {
					endLine := lineNumber(src, actionEnd-1)
					defines = append(defines, GoTemplateDefine{
						Name:      top.name,
						StartLine: top.startLine,
						EndLine:   endLine,
					})
				}
			}
		}

		blankRange(out, actionStart, actionEnd)
		i = actionEnd
	}

	return out, defines
}

// blankRange replaces out[start:end] with spaces, preserving newlines so that
// line numbers in the cleaned output match the original src.
func blankRange(out []byte, start, end int) {
	if end > len(out) {
		end = len(out)
	}
	for k := start; k < end; k++ {
		if out[k] != '\n' {
			out[k] = ' '
		}
	}
}

// lenTrimClose is the byte length of the trim-right action close marker "-}}".
const lenTrimClose = 3

// findActionClose finds the byte offset of the character immediately AFTER the
// closing "}}" of an action. src[from:] is searched, and the returned offset
// is absolute in src. Returns -1 if no closing "}}" is found.
//
// Handles:
//   - Plain "}}" — close is at the "}}" position + 2.
//   - Trim marker "-}}" — close is at "-}}" + lenTrimClose (we include the "-").
//   - Double-quoted string literals inside actions — skip them so a "}}" inside
//     a string (e.g. `{{ printf "%s" "}}x" }}`) does not close prematurely.
func findActionClose(src []byte, from int) int {
	i := from
	for i < len(src) {
		b := src[i]
		switch {
		case b == '"':
			i = skipDoubleQuoted(src, i+1)
		case b == '`':
			i = skipBacktickQuoted(src, i+1)
		case b == '}' && i+1 < len(src) && src[i+1] == '}':
			return i + 2
		case b == '-' && i+2 < len(src) && src[i+1] == '}' && src[i+2] == '}':
			return i + lenTrimClose
		default:
			i++
		}
	}
	return -1
}

// skipDoubleQuoted advances past a double-quoted string starting at from
// (i.e. from is the index AFTER the opening '"'). Returns the index of the
// first byte after the closing '"', or len(src) if unterminated.
func skipDoubleQuoted(src []byte, from int) int {
	return skipQuoted(src, from, '"', true)
}

// skipBacktickQuoted advances past a backtick-quoted string starting at from
// (i.e. from is the index AFTER the opening '`'). Returns the index of the
// first byte after the closing '`', or len(src) if unterminated.
func skipBacktickQuoted(src []byte, from int) int {
	// Go raw strings do not process backslash escapes, so escaped=false;
	// behaviour is identical to the previous inline scanner.
	return skipQuoted(src, from, '`', false)
}

// actionBody returns the trimmed text between "{{" and "}}" for keyword parsing.
// It strips leading trim markers "{{-" and trailing trim markers "-}}", then
// trims ASCII whitespace.
func actionBody(src []byte, actionStart, actionEnd int) []byte {
	if actionEnd > len(src) {
		actionEnd = len(src)
	}
	// Skip "{{"
	body := src[actionStart+2 : actionEnd]
	// Strip trailing "}}" or "-}}"
	if bytes.HasSuffix(body, []byte("-}}")) {
		body = body[:len(body)-3]
	} else if bytes.HasSuffix(body, []byte("}}")) {
		body = body[:len(body)-2]
	}
	// Strip leading "-" trim marker
	if bytes.HasPrefix(body, []byte("-")) {
		body = body[1:]
	}
	return bytes.TrimSpace(body)
}

// parseActionKeyword returns the first word (keyword) and an optional quoted
// name from an action body. Examples:
//
//	define "layout"  → ("define", "layout")
//	end              → ("end", "")
//	range .Items     → ("range", "")
//	if .Cond         → ("if", "")
func parseActionKeyword(body []byte) (keyword, name string) {
	body = bytes.TrimSpace(body)
	// Extract first word.
	sp := bytes.IndexAny(body, " \t\n\r")
	var kw []byte
	if sp < 0 {
		kw = body
	} else {
		kw = body[:sp]
		rest := bytes.TrimSpace(body[sp:])
		// Extract quoted name if present.
		if len(rest) > 0 && rest[0] == '"' {
			end := bytes.IndexByte(rest[1:], '"')
			if end >= 0 {
				name = string(rest[1 : end+1])
			}
		}
	}
	keyword = string(kw)
	return keyword, name
}

// lineNumber returns the 1-based line number of byte offset pos in src.
func lineNumber(src []byte, pos int) int {
	if pos > len(src) {
		pos = len(src)
	}
	return 1 + bytes.Count(src[:pos], []byte("\n"))
}

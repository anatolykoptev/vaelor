package preproc

import "bytes"

// scanTemplateRefs returns TemplateRef entries for every capitalised JSX-style
// opening tag in the Astro template body (after frontmatter). Skips HTML
// comments, <script>/<style> blocks, closing tags, and namespace-prefixed
// tags (e.g. <astro:fragment>).
func scanTemplateRefs(src []byte) []TemplateRef {
	templateStart := findTemplateStart(src)
	skips := collectSkipRanges(src)

	var refs []TemplateRef
	i := templateStart
	line, col := lineColAt(src, templateStart)

	for i < len(src) {
		b := src[i]

		if b == '\n' {
			line++
			col = 1
			i++
			continue
		}
		if inSkipRanges(i, skips) {
			col++
			i++
			continue
		}
		if b != '<' {
			col++
			i++
			continue
		}

		// HTML comment: <!-- ... -->
		if bytes.HasPrefix(src[i:], []byte("<!--")) {
			end := bytes.Index(src[i:], []byte("-->"))
			if end < 0 {
				break
			}
			for _, c := range src[i : i+end+3] {
				if c == '\n' {
					line++
					col = 1
				} else {
					col++
				}
			}
			i += end + 3
			continue
		}

		// Closing tag </Foo> — not a usage.
		if i+1 < len(src) && src[i+1] == '/' {
			gtIdx := bytes.IndexByte(src[i:], '>')
			if gtIdx < 0 {
				col++
				i++
				continue
			}
			for _, c := range src[i : i+gtIdx+1] {
				if c == '\n' {
					line++
					col = 1
				} else {
					col++
				}
			}
			i += gtIdx + 1
			continue
		}

		// Opening tag with capitalised first letter.
		if i+1 < len(src) && isUpperASCII(src[i+1]) {
			tagLine, tagCol := line, col
			j := i + 1
			for j < len(src) && isTagNameByte(src[j]) {
				j++
			}
			name := string(src[i+1 : j])
			if !bytes.ContainsRune([]byte(name), ':') {
				refs = append(refs, TemplateRef{Name: name, Line: uint32(tagLine), Col: uint32(tagCol)})
			}
			// Advance past tag, honouring quoted attribute values.
			k, inQ := j, byte(0)
			for k < len(src) {
				c := src[k]
				if inQ != 0 {
					if c == inQ {
						inQ = 0
					}
				} else if c == '"' || c == '\'' {
					inQ = c
				} else if c == '>' {
					k++
					break
				}
				if c == '\n' {
					line++
					col = 1
				} else {
					col++
				}
				k++
			}
			i = k
			continue
		}

		col++
		i++
	}
	return refs
}

// findTemplateStart returns the byte offset just after the closing --- line,
// or 0 if there is no frontmatter.
func findTemplateStart(src []byte) int {
	if !bytes.HasPrefix(bytes.TrimLeft(src, " \t\r\n"), []byte("---")) {
		return 0
	}
	ad := bytes.Index(src, []byte("---")) + 3
	if ad < len(src) && src[ad] == '\r' {
		ad++
	}
	if ad < len(src) && src[ad] == '\n' {
		ad++
	}
	cl := findLinePrefix(src, ad, []byte("---"))
	if cl < 0 {
		return len(src)
	}
	nl := bytes.IndexByte(src[cl:], '\n')
	if nl < 0 {
		return len(src)
	}
	return cl + nl + 1
}

// inSkipRanges reports whether i falls inside any skip range.
func inSkipRanges(i int, ranges []skipRange) bool {
	for _, r := range ranges {
		if i >= r.start && i < r.end {
			return true
		}
	}
	return false
}

// lineColAt returns the 1-based line and column of byte offset pos.
func lineColAt(src []byte, pos int) (line, col int) {
	line, col = 1, 1
	if pos > len(src) {
		pos = len(src)
	}
	for _, c := range src[:pos] {
		if c == '\n' {
			line++
			col = 1
		} else {
			col++
		}
	}
	return
}

// isUpperASCII reports whether b is [A-Z].
func isUpperASCII(b byte) bool { return b >= 'A' && b <= 'Z' }

// isTagNameByte reports whether b is a valid tag-name byte. Colon included so
// namespaced names like "astro:fragment" are read as one token.
func isTagNameByte(b byte) bool {
	return (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z') ||
		(b >= '0' && b <= '9') || b == '_' || b == '$' || b == '.' || b == '-' || b == ':'
}

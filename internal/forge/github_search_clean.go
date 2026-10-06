package forge

import (
	"path"
	"regexp"
	"strings"
	"unicode"
)

// markdownFragmentExts gates fragment cleanup to prose files where markup
// boilerplate (badges, nav links, html comments) is furniture rather than
// source. Code files keep fragments verbatim — a tag-only or punct-only line
// there is real syntax, not furniture.
var markdownFragmentExts = map[string]struct{}{
	".md": {}, ".markdown": {}, ".mdx": {}, ".mdown": {},
	".mkdn": {}, ".mkd": {}, ".rst": {},
}

var (
	// mdInlineLinkRe strips markdown inline links and images, label included.
	mdInlineLinkRe = regexp.MustCompile(`!?\[[^\]]*\]\([^)]*\)`)
	// mdRefLinkRe strips markdown reference-style links `[text][ref]`.
	mdRefLinkRe = regexp.MustCompile(`!?\[[^\]]*\]\[[^\]]*\]`)
	// htmlCommentRe strips HTML comments.
	htmlCommentRe = regexp.MustCompile(`<!--.*?-->`)
	// htmlTagRe strips HTML tags and autolinks such as `<https://…>`.
	htmlTagRe = regexp.MustCompile(`<[^>]+>`)
	// htmlEntityRe strips HTML entities such as `&nbsp;`.
	htmlEntityRe = regexp.MustCompile(`&(?:[a-zA-Z]+|#[0-9]+|#[xX][0-9a-fA-F]+);`)
	// bareURLRe strips bare URLs left outside markdown constructs.
	bareURLRe = regexp.MustCompile(`https?://\S+`)
	// queryTokenRe splits a GitHub code-search query into raw tokens,
	// keeping "quoted phrases" whole.
	queryTokenRe = regexp.MustCompile(`"[^"]*"|[^\s"]+`)
)

// searchQueryTerms extracts the plain-text terms from a GitHub code-search
// query, dropping qualifiers (`language:go`, `repo:a/b`, `path:x`) and the
// boolean operators OR/AND/NOT.
func searchQueryTerms(q string) []string {
	var terms []string
	for _, tok := range queryTokenRe.FindAllString(q, -1) {
		tok = strings.Trim(tok, `"`)
		if tok == "" || strings.Contains(tok, ":") {
			continue
		}
		switch strings.ToUpper(tok) {
		case "OR", "AND", "NOT":
			continue
		}
		terms = append(terms, strings.ToLower(strings.TrimLeft(tok, "-")))
	}
	return terms
}

// isMarkdownSearchPath reports whether a result path is a prose file whose
// fragments are safe to de-furniture.
func isMarkdownSearchPath(p string) bool {
	_, ok := markdownFragmentExts[strings.ToLower(path.Ext(p))]
	return ok
}

// hasQueryTerm reports whether the raw line contains any query term. The
// check runs against the untouched line so a term inside a link label or
// tag attribute still protects the line.
func hasQueryTerm(line string, terms []string) bool {
	low := strings.ToLower(line)
	for _, t := range terms {
		if t != "" && strings.Contains(low, t) {
			return true
		}
	}
	return false
}

// markupResidue returns the readable text left in a line after every markup
// construct is removed.
func markupResidue(line string) string {
	s := htmlCommentRe.ReplaceAllString(line, "")
	s = mdInlineLinkRe.ReplaceAllString(s, "")
	s = mdRefLinkRe.ReplaceAllString(s, "")
	s = htmlTagRe.ReplaceAllString(s, "")
	s = htmlEntityRe.ReplaceAllString(s, "")
	s = bareURLRe.ReplaceAllString(s, "")
	return s
}

// hasLetters reports whether s contains at least one letter of any script.
func hasLetters(s string) bool {
	for _, r := range s {
		if unicode.IsLetter(r) {
			return true
		}
	}
	return false
}

// sanitizeSearchFragment drops markup-furniture lines from one text-match
// fragment of a markdown-family file: badge/image rows, nav-link rows, raw
// HTML tag lines, HTML comments, bare URLs and separator rules — anything
// whose readable residue carries no letters. A line is always kept when a
// GitHub match span overlaps it, when it contains a query term, or when it
// sits inside a fenced code block.
func sanitizeSearchFragment(frag string, matches []ghMatchSpan, terms []string) string {
	lines := strings.Split(frag, "\n")
	out := make([]string, 0, len(lines))
	inFence := false
	blankPending := false
	offset := 0

	for _, line := range lines {
		lineStart := offset
		offset += len(line) + 1
		trimmed := strings.TrimSpace(line)
		if fenceMarker(trimmed) {
			inFence = !inFence
			blankPending = false
			out = append(out, line)
			continue
		}
		if inFence {
			out = append(out, line)
			continue
		}
		if trimmed == "" {
			blankPending = true
			continue
		}
		if matchedLine(lineStart, offset-1, matches) || hasQueryTerm(line, terms) || hasLetters(markupResidue(trimmed)) {
			if blankPending && len(out) > 0 {
				out = append(out, "")
			}
			blankPending = false
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}

// matchedLine reports whether any GitHub match span intersects the byte
// range [lineStart, lineEnd) of the fragment.
func matchedLine(lineStart, lineEnd int, matches []ghMatchSpan) bool {
	for _, m := range matches {
		if len(m.Indices) != 2 {
			continue
		}
		if m.Indices[0] < lineEnd && m.Indices[1] > lineStart {
			return true
		}
	}
	return false
}

// fenceMarker reports whether a trimmed line opens or closes a fenced code
// block (``` or ~~~, with optional info string).
func fenceMarker(trimmed string) bool {
	return strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~")
}

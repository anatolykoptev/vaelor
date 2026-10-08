package impact

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

const (
	// callSnippetContext is the number of source lines carried before and
	// after the call line — a snippet is at most 2*callSnippetContext+1 lines.
	callSnippetContext = 1
	// maxSnippetFileBytes bounds the file read behind one snippet. A caller
	// file larger than this (generated bundle, minified asset) is skipped
	// like an unreadable one.
	maxSnippetFileBytes = 1 << 20
	// maxSnippetLineChars caps one emitted source line so a minified
	// single-line file cannot blow up the response.
	maxSnippetLineChars = 300
)

// AttachCallSnippets fills Snippet on each caller that has a recorded call
// site (CallLine > 0) and a readable file: a few numbered lines centred on
// the call, same "%4d│" style as internal/review snippets.
//
// Work is bounded: one read per unique caller file, at most
// 2*callSnippetContext+1 lines per caller, each line capped at
// maxSnippetLineChars. Unreadable or oversized files and callers without a
// call line keep an empty Snippet — a snippet miss is never fatal.
func AttachCallSnippets(callers []AffectedSymbol, repoRoot string) (missed int) {
	cache := make(map[string][]string) // path → lines; nil = skipped/unreadable
	for i := range callers {
		if callers[i].CallLine == 0 {
			missed++
			continue
		}
		path := callers[i].File
		if !filepath.IsAbs(path) {
			path = filepath.Join(repoRoot, path)
		}
		if !withinRoot(repoRoot, path) {
			missed++
			continue
		}
		lines, seen := cache[path]
		if !seen {
			lines = readSnippetLines(path)
			cache[path] = lines
		}
		if callers[i].Snippet = snippetAround(lines, callers[i].CallLine); callers[i].Snippet == "" {
			missed++
		}
	}
	return missed
}

// withinRoot reports whether path stays under repoRoot, so a graph entry with
// a "../" path cannot make the tool read outside the checkout.
func withinRoot(repoRoot, path string) bool {
	rel, err := filepath.Rel(repoRoot, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// readSnippetLines splits a source file into lines, returning nil when the
// file is unreadable, binary, or exceeds maxSnippetFileBytes. The size bound
// is enforced on the bytes actually read, not on a prior Stat, so a file that
// grows between the two cannot slip past it.
func readSnippetLines(path string) []string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxSnippetFileBytes+1))
	if err != nil || len(data) > maxSnippetFileBytes || bytes.IndexByte(data, 0) >= 0 {
		return nil
	}
	return strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
}

// snippetAround renders at most 2*callSnippetContext+1 numbered lines centred
// on callLine (1-based); "" when the file was skipped or the line is out of
// range.
func snippetAround(lines []string, callLine uint32) string {
	if lines == nil || callLine == 0 || int(callLine) > len(lines) {
		return ""
	}
	line := int(callLine)
	start := max(line-1-callSnippetContext, 0)
	end := min(line+callSnippetContext, len(lines))
	var b strings.Builder
	for i := start; i < end; i++ {
		fmt.Fprintf(&b, "%4d│ %s\n", i+1, truncateSnippetLine(strings.TrimSuffix(lines[i], "\r")))
	}
	return b.String()
}

func truncateSnippetLine(s string) string {
	if len(s) <= maxSnippetLineChars {
		return s
	}
	cut := maxSnippetLineChars
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut-- // never split a multi-byte rune
	}
	return s[:cut] + "…"
}

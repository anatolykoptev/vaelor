package impact

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
func AttachCallSnippets(callers []AffectedSymbol, repoRoot string) {
	cache := make(map[string][]string) // path → lines; nil = skipped/unreadable
	for i := range callers {
		if callers[i].CallLine == 0 {
			continue
		}
		path := callers[i].File
		if !filepath.IsAbs(path) {
			path = filepath.Join(repoRoot, path)
		}
		lines, seen := cache[path]
		if !seen {
			lines = readSnippetLines(path)
			cache[path] = lines
		}
		callers[i].Snippet = snippetAround(lines, callers[i].CallLine)
	}
}

// readSnippetLines splits a source file into lines, returning nil when the
// file is unreadable or exceeds maxSnippetFileBytes.
func readSnippetLines(path string) []string {
	info, err := os.Stat(path)
	if err != nil || info.Size() > maxSnippetFileBytes {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	return strings.Split(string(data), "\n")
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
		fmt.Fprintf(&b, "%4d│ %s\n", i+1, truncateSnippetLine(lines[i]))
	}
	return b.String()
}

func truncateSnippetLine(s string) string {
	if len(s) <= maxSnippetLineChars {
		return s
	}
	return s[:maxSnippetLineChars] + "…"
}

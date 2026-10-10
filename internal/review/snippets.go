package review

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/anatolykoptev/vaelor/internal/fsutil"
)

// Snippet is a source code excerpt around a changed symbol.
type Snippet struct {
	File      string `json:"file"`
	Symbol    string `json:"symbol"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
	Code      string `json:"code"`
}

const (
	snippetContextBefore = 3 // lines before symbol start
	snippetContextAfter  = 1 // lines after symbol end
	maxSnippetLines      = 30
)

// ExtractSnippets reads source files and extracts code context around changed symbols.
// repoRoot is the absolute path to the repo root.
func ExtractSnippets(changed []ChangedSymbol, repoRoot string) []Snippet {
	// Group symbols by file to avoid reading the same file multiple times.
	byFile := make(map[string][]ChangedSymbol)
	for _, cs := range changed {
		rel := filepath.Join(repoRoot, cs.FileDiff.Path)
		byFile[rel] = append(byFile[rel], cs)
	}

	var snippets []Snippet
	for absPath, syms := range byFile {
		lines, err := readLines(repoRoot, absPath)
		if err != nil {
			continue
		}
		for _, cs := range syms {
			s := extractOne(lines, cs, absPath, repoRoot)
			if s.Code != "" {
				snippets = append(snippets, s)
			}
		}
	}
	return snippets
}

func extractOne(lines []string, cs ChangedSymbol, absPath, root string) Snippet {
	start := int(cs.Symbol.StartLine) - snippetContextBefore - 1 // 0-based
	if start < 0 {
		start = 0
	}
	end := int(cs.Symbol.EndLine) + snippetContextAfter // 0-based exclusive
	if end > len(lines) {
		end = len(lines)
	}
	// Clamp to maxSnippetLines.
	if end-start > maxSnippetLines {
		end = start + maxSnippetLines
	}

	// Number lines for readability.
	var b strings.Builder
	for i := start; i < end; i++ {
		fmt.Fprintf(&b, "%4d│ %s\n", i+1, lines[i])
	}

	rel, _ := filepath.Rel(root, absPath)
	return Snippet{
		File:      rel,
		Symbol:    cs.Symbol.Name,
		StartLine: start + 1,
		EndLine:   end,
		Code:      b.String(),
	}
}

// snippetFileMaxBytes bounds a source file read for snippet extraction.
const snippetFileMaxBytes = 2 << 20

func readLines(root, path string) ([]string, error) {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return nil, err
	}
	data, err := fsutil.ReadRepoFile(root, rel, snippetFileMaxBytes)
	if err != nil {
		fsutil.ReportRefusal("review.snippets", rel, err)
		return nil, err
	}
	return strings.Split(string(data), "\n"), nil
}

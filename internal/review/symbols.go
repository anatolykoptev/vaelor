package review

import (
	"path/filepath"
	"strings"

	"github.com/anatolykoptev/go-code/internal/parser"
)

// ChangeType describes how a symbol was modified.
type ChangeType string

const (
	ChangeAdded    ChangeType = "added"
	ChangeModified ChangeType = "modified"
	ChangeRemoved  ChangeType = "removed"
)

// ChangedSymbol pairs a symbol with its change type.
type ChangedSymbol struct {
	Symbol     *parser.Symbol
	ChangeType ChangeType
	FileDiff   FileDiff
}

// ChangedSymbols intersects parsed symbols with git diff line ranges.
// repoRoot is the absolute path to the repo root (symbols have absolute File paths).
func ChangedSymbols(symbols []*parser.Symbol, diffs []FileDiff, repoRoot string) []ChangedSymbol {
	byFile := make(map[string][]*parser.Symbol)
	for _, sym := range symbols {
		rel := relPath(sym.File, repoRoot)
		byFile[rel] = append(byFile[rel], sym)
	}

	var result []ChangedSymbol
	for _, diff := range diffs {
		fileSymbols := byFile[diff.Path]
		if len(fileSymbols) == 0 {
			continue
		}
		// Heuristic: new file has no removed lines and at least one added line.
		isNewFile := diff.Removed == 0 && diff.Added > 0 && len(diff.LineRanges) >= 1

		for _, sym := range fileSymbols {
			if overlaps(sym, diff.LineRanges) {
				ct := ChangeModified
				if isNewFile {
					ct = ChangeAdded
				}
				result = append(result, ChangedSymbol{
					Symbol:     sym,
					ChangeType: ct,
					FileDiff:   diff,
				})
			}
		}
	}
	return result
}

func overlaps(sym *parser.Symbol, ranges []LineRange) bool {
	for _, r := range ranges {
		if int(sym.StartLine) <= r.End && r.Start <= int(sym.EndLine) {
			return true
		}
	}
	return false
}

func relPath(absPath, root string) string {
	rel, err := filepath.Rel(root, absPath)
	if err != nil {
		return strings.TrimPrefix(absPath, root+"/")
	}
	return rel
}

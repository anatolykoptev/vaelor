package review

import (
	"github.com/anatolykoptev/vaelor/internal/langutil"
	"github.com/anatolykoptev/vaelor/internal/parser"
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

	// Flag is a non-fatal classification attached by the review pipeline when
	// graph signals indicate something noteworthy (e.g. community_move, high_surprise).
	// Empty when no flag applied.
	Flag string `json:"flag,omitempty"`
	// Note is a human-readable explanation of Flag. Empty when Flag is empty.
	Note string `json:"note,omitempty"`

	// DeadCodeScore is the CE reranker probability [0..1] that this symbol is
	// genuine dead code. Set only for ChangeRemoved symbols when a score is
	// available in code_dead_code_scores (score > 0.25).
	DeadCodeScore float32 `json:"dead_code_score,omitempty"`
	// DeadCodeNote is a human-readable explanation of the dead-code probability.
	DeadCodeNote string `json:"dead_code_note,omitempty"`
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
	return langutil.RelPath(absPath, root)
}

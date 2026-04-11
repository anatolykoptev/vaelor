package render

import (
	"strings"

	"github.com/anatolykoptev/go-code/internal/parser"
)

// buildLineOps converts replacements into per-line skip/replace/insert instructions.
func buildLineOps(lines []string, replacements []replacement) lineOps {
	ops := lineOps{
		skip:        make(map[int]bool),
		replaceWith: make(map[int]string),
		insertAfter: make(map[int]string),
	}

	totalLines := len(lines)
	for _, r := range replacements {
		if r.startLine > totalLines {
			continue
		}
		end := r.endLine
		if end > totalLines {
			end = totalLines
		}

		switch r.action {
		case actionSignatures:
			if r.signature != "" {
				ops.replaceWith[r.startLine] = r.signature
			}
			for line := r.startLine + 1; line <= end; line++ {
				ops.skip[line] = true
			}
		case actionSkeleton:
			for line := r.startLine + 1; line <= end-1; line++ {
				ops.skip[line] = true
			}
			ops.insertAfter[r.startLine] = bodyPlaceholder
		}
	}

	return ops
}

// isStructuralKind returns true for symbol kinds where the body defines
// the API surface (fields, methods in interfaces, etc.).
func isStructuralKind(kind parser.NodeKind) bool {
	switch kind {
	case parser.KindStruct, parser.KindInterface, parser.KindClass, parser.KindType:
		return true
	default:
		return false
	}
}

// isRelevant checks whether a symbol name or signature matches any of the query terms.
func isRelevant(sym *parser.Symbol, terms []string) bool {
	lowerName := strings.ToLower(sym.Name)
	lowerSig := strings.ToLower(sym.Signature)
	for _, t := range terms {
		if strings.Contains(lowerName, t) || strings.Contains(lowerSig, t) {
			return true
		}
	}
	return false
}

// Package render provides advanced source code rendering modes for LLM context.
//
// It transforms full source code into more compact representations by
// leveraging parsed symbol information. Modes include signatures-only,
// skeleton (signatures + body placeholders), and focused (full bodies
// for query-relevant symbols, signatures for the rest).
package render

import (
	"strings"

	"github.com/anatolykoptev/go-code/internal/parser"
)

// Mode controls how source code is rendered for LLM context.
type Mode string

const (
	// ModeDefault preserves the original source (existing behavior).
	ModeDefault Mode = ""

	// ModeSignatures keeps only declaration signatures, removing all bodies.
	ModeSignatures Mode = "signatures"

	// ModeSkeleton keeps signatures with "// ..." placeholders for bodies.
	ModeSkeleton Mode = "skeleton"

	// ModeFocused keeps full bodies for query-relevant symbols and
	// signatures only for the rest.
	ModeFocused Mode = "focused"
)

// Opts controls rendering behavior.
type Opts struct {
	// Mode selects the rendering strategy.
	Mode Mode

	// QueryTerms are lowercase terms used in focused mode to decide
	// which symbols get full bodies.
	QueryTerms []string
}

// bodyPlaceholder is the placeholder inserted for omitted function bodies.
const bodyPlaceholder = "    // ..."

// actionSignatures strips the body, keeping only the signature line.
const actionSignatures = "signatures"

// actionSkeleton replaces the body with a placeholder.
const actionSkeleton = "skeleton"

// replacement describes how a symbol's line range should be transformed.
type replacement struct {
	startLine int // 1-based, inclusive
	endLine   int // 1-based, inclusive
	action    string
}

// RenderFile transforms source code according to the rendering mode,
// using parsed symbol information to identify which lines belong to
// function/method bodies vs. declarations.
//
// For symbols of structural kinds (struct, interface, class, type),
// the full body is always preserved since fields ARE the API surface.
func RenderFile(source string, symbols []*parser.Symbol, opts Opts) string {
	if opts.Mode == ModeDefault || len(symbols) == 0 {
		return source
	}

	replacements := buildReplacements(symbols, opts)
	if len(replacements) == 0 {
		return source
	}

	lines := strings.Split(source, "\n")
	result := applyReplacements(lines, replacements)

	// Trim trailing newline to match original format if source didn't end with one.
	if len(result) > 0 && result[len(result)-1] == '\n' {
		if len(source) == 0 || source[len(source)-1] != '\n' {
			result = result[:len(result)-1]
		}
	}

	return result
}

// buildReplacements determines which symbol ranges need transformation.
func buildReplacements(symbols []*parser.Symbol, opts Opts) []replacement {
	var out []replacement

	for _, sym := range symbols {
		if sym.StartLine == 0 || sym.EndLine == 0 || sym.StartLine >= sym.EndLine {
			continue
		}
		if isStructuralKind(sym.Kind) {
			continue
		}

		action := symbolAction(sym, opts)
		if action == "" {
			continue
		}

		out = append(out, replacement{
			startLine: int(sym.StartLine),
			endLine:   int(sym.EndLine),
			action:    action,
		})
	}

	return out
}

// symbolAction returns the replacement action for a symbol, or "" to keep it unchanged.
func symbolAction(sym *parser.Symbol, opts Opts) string {
	switch opts.Mode {
	case ModeSignatures:
		return actionSignatures
	case ModeSkeleton:
		return actionSkeleton
	case ModeFocused:
		if isRelevant(sym, opts.QueryTerms) {
			return ""
		}
		return actionSignatures
	default:
		return ""
	}
}

// applyReplacements processes source lines according to the replacement list,
// skipping body lines and optionally inserting placeholders.
func applyReplacements(lines []string, replacements []replacement) string {
	skipLines := make(map[int]bool)
	insertAfter := make(map[int]string)

	for _, r := range replacements {
		for line := r.startLine + 1; line <= r.endLine; line++ {
			skipLines[line] = true
		}
		if r.action == actionSkeleton {
			insertAfter[r.startLine] = bodyPlaceholder
		}
	}

	var out strings.Builder
	for i, line := range lines {
		lineNum := i + 1 // 1-based
		if skipLines[lineNum] {
			continue
		}
		out.WriteString(line)
		out.WriteByte('\n')
		if placeholder, ok := insertAfter[lineNum]; ok {
			out.WriteString(placeholder)
			out.WriteByte('\n')
		}
	}

	return out.String()
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

// isRelevant checks whether a symbol name matches any of the query terms.
func isRelevant(sym *parser.Symbol, terms []string) bool {
	lower := strings.ToLower(sym.Name)
	for _, t := range terms {
		if strings.Contains(lower, t) {
			return true
		}
	}
	return false
}

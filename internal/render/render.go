// Package render provides advanced source code rendering modes for LLM context.
//
// It transforms full source code into more compact representations by
// leveraging parsed symbol information. Modes include signatures-only,
// skeleton (signatures + body placeholders), and focused (full bodies
// for query-relevant symbols, signatures for the rest).
package render

import (
	"sort"
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

	// ModeSkeleton keeps signatures with "⋮..." placeholders for bodies.
	ModeSkeleton Mode = "skeleton"

	// ModeFocused keeps full bodies for query-relevant symbols and
	// signatures only for the rest.
	ModeFocused Mode = "focused"
)

// ValidMode reports whether m is a recognized rendering mode.
func ValidMode(m string) bool {
	switch Mode(m) {
	case ModeDefault, ModeSignatures, ModeSkeleton, ModeFocused:
		return true
	default:
		return false
	}
}

// Opts controls rendering behavior.
type Opts struct {
	// Mode selects the rendering strategy.
	Mode Mode

	// QueryTerms are lowercase terms used in focused mode to decide
	// which symbols get full bodies.
	QueryTerms []string
}

// bodyPlaceholder is the placeholder inserted for omitted function bodies.
// Uses vertical ellipsis (⋮) to be visually distinct from real comments.
const bodyPlaceholder = "    ⋮..."

// actionSignatures replaces the entire symbol range with its clean signature.
const actionSignatures = "signatures"

// actionSkeleton keeps the opening line and closing line, replaces the body
// between them with a placeholder.
const actionSkeleton = "skeleton"

// replacement describes how a symbol's line range should be transformed.
type replacement struct {
	startLine int    // 1-based, inclusive
	endLine   int    // 1-based, inclusive
	action    string // actionSignatures or actionSkeleton
	signature string // clean signature text (from parser, without braces)
}

// RenderFile transforms source code according to the rendering mode,
// using parsed symbol information to identify which lines belong to
// function/method bodies vs. declarations.
//
// For symbols of structural kinds (struct, interface, class, type),
// the full body is always preserved since fields ARE the API surface.
func RenderFile(source string, symbols []*parser.Symbol, opts Opts) string {
	if opts.Mode == ModeDefault || len(symbols) == 0 || source == "" {
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
// Symbols are sorted by start line and nested symbols (fully contained
// within a parent replacement) are suppressed to avoid corrupted output.
func buildReplacements(symbols []*parser.Symbol, opts Opts) []replacement {
	var candidates []replacement

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

		candidates = append(candidates, replacement{
			startLine: int(sym.StartLine),
			endLine:   int(sym.EndLine),
			action:    action,
			signature: sym.Signature,
		})
	}

	// Sort by startLine so we can detect nesting.
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].startLine < candidates[j].startLine
	})

	// Remove nested replacements: if a child range is fully contained
	// within a parent replacement, the parent already handles those lines.
	return removeNested(candidates)
}

// removeNested filters out replacements whose range falls entirely within
// a preceding replacement's range.
func removeNested(sorted []replacement) []replacement {
	if len(sorted) == 0 {
		return sorted
	}

	out := make([]replacement, 0, len(sorted))
	out = append(out, sorted[0])

	for i := 1; i < len(sorted); i++ {
		parent := out[len(out)-1]
		child := sorted[i]
		if child.startLine >= parent.startLine && child.endLine <= parent.endLine {
			// Fully nested — skip.
			continue
		}
		out = append(out, child)
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

// lineOps holds per-line instructions built from replacements.
type lineOps struct {
	skip        map[int]bool   // lines to omit entirely (1-based)
	replaceWith map[int]string // lines to replace with different text
	insertAfter map[int]string // text to insert after a line
}

// applyReplacements processes source lines according to the replacement list.
//
// For signatures: the entire symbol range (startLine..endLine) is replaced
// with a single line showing the clean signature from the parser.
//
// For skeleton: startLine and endLine are kept (opening declaration and
// closing brace), and the body between them is replaced with a placeholder.
func applyReplacements(lines []string, replacements []replacement) string {
	ops := buildLineOps(lines, replacements)
	hasReplacements := len(replacements) > 0

	var out strings.Builder
	for i, line := range lines {
		lineNum := i + 1 // 1-based
		if ops.skip[lineNum] {
			continue
		}
		if text, ok := ops.replaceWith[lineNum]; ok {
			if hasReplacements {
				out.WriteString("│")
			}
			out.WriteString(text)
		} else {
			if hasReplacements {
				out.WriteString("│")
			}
			out.WriteString(line)
		}
		out.WriteByte('\n')
		if text, ok := ops.insertAfter[lineNum]; ok {
			out.WriteString(text)
			out.WriteByte('\n')
		}
	}

	return out.String()
}

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

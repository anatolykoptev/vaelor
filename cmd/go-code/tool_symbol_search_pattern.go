package main

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/anatolykoptev/vaelor/internal/analyze"
	"github.com/anatolykoptev/vaelor/internal/oxcodes"
	"github.com/anatolykoptev/vaelor/internal/parser"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// handleStructuralSymbolSearch routes a shape-based symbol_search query to
// ox-codes /search/structural and maps each match's enclosing-symbol
// expansion back to the same xmlSymSearchItem layout used by name-based
// queries — agents see one consistent format regardless of mode.
func handleStructuralSymbolSearch(ctx context.Context, input SymbolSearchInput, deps analyze.Deps, outputDir string) (*mcp.CallToolResult, error) {
	root, cleanup, err := resolveRoot(ctx, input.Repo, "", deps)
	if err != nil {
		return errResult(fmt.Sprintf("resolve repo: %s", err)), nil
	}
	defer cleanup()

	limit := input.Limit
	if limit <= 0 {
		limit = defaultMaxResults
	}

	result, err := deps.OxCodes.SearchStructural(ctx, oxcodes.StructuralSearchInput{
		Root:       root,
		Pattern:    input.Pattern,
		Language:   input.Language,
		MaxResults: limit,
		Expand:     "function",
	})
	if err != nil {
		return errResult(fmt.Sprintf("structural symbol search: %s", err)), nil
	}

	syms := convertStructuralToSymbols(result.Matches, root)
	if len(syms) == 0 {
		return textResult(fmt.Sprintf(
			"No symbols match pattern %q (language=%s).",
			input.Pattern, input.Language)), nil
	}

	return largeTextResult(
		formatSymbolSearchXML(input.Pattern, syms, root),
		"symbol_search",
		outputDir,
	), nil
}

// convertStructuralToSymbols turns ox-codes structural matches into
// *parser.Symbol records keyed by the enclosing function/class. Matches
// without an Expanded block are skipped (no usable name/kind); duplicate
// hits inside the same enclosing symbol are deduped.
func convertStructuralToSymbols(matches []oxcodes.SearchMatch, root string) []*parser.Symbol {
	out := make([]*parser.Symbol, 0, len(matches))
	seen := make(map[string]struct{}, len(matches))
	for _, m := range matches {
		if m.Expanded == nil {
			continue
		}
		file := m.File
		if rel, err := filepath.Rel(root, file); err == nil && !strings.HasPrefix(rel, "..") {
			file = rel
		}
		key := fmt.Sprintf("%s:%d", file, m.Expanded.LineStart)
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}

		sym := &parser.Symbol{
			Kind:      parser.NodeKind(m.Expanded.SymbolKind),
			Name:      m.Expanded.SymbolName,
			File:      file,
			StartLine: uint32(m.Expanded.LineStart),
			EndLine:   uint32(m.Expanded.LineEnd),
			Signature: firstNonEmptyLine(m.Expanded.Body),
		}
		out = append(out, sym)
	}
	return out
}

// firstNonEmptyLine returns the first non-blank line of body, trimmed.
// Used as a synthetic Signature when ox-codes returns full bodies —
// the first line of a function/class declaration is exactly what an
// agent needs to decide whether to read the rest.
func firstNonEmptyLine(body string) string {
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}

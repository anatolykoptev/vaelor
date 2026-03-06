package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/anatolykoptev/go-code/internal/codesearch"
	"github.com/anatolykoptev/go-code/internal/embeddings"
)

// runKeywordSearch runs a case-insensitive literal search for the query in the repo.
func runKeywordSearch(ctx context.Context, query, root string) []embeddings.FileLineHit {
	matches, err := codesearch.Search(ctx, codesearch.SearchInput{
		Root:          root,
		Pattern:       query,
		IsRegex:       false,
		CaseSensitive: false,
		MaxResults:    50,
		ContextLines:  0,
	})
	if err != nil || len(matches) == 0 {
		return nil
	}
	hits := make([]embeddings.FileLineHit, len(matches))
	for i, m := range matches {
		hits[i] = embeddings.FileLineHit{FilePath: m.File, Line: m.Line}
	}
	return hits
}

// formatHybridResults formats hybrid (semantic + keyword RRF-merged) results as XML.
func formatHybridResults(input SemanticSearchInput, results []embeddings.HybridResult) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "<response tool=\"semantic_search\" mode=\"hybrid\">\n")
	fmt.Fprintf(&sb, "  <query>%s</query>\n", escapeXML(input.Query))
	fmt.Fprintf(&sb, "  <repo>%s</repo>\n", escapeXML(input.Repo))
	fmt.Fprintf(&sb, "  <results count=\"%d\">\n", len(results))
	for i, r := range results {
		fmt.Fprintf(&sb, "    <result rank=\"%d\" source=\"%s\" score=\"%.4f\">\n",
			i+1, r.Source, r.RRFScore)
		fmt.Fprintf(&sb, "      <file>%s</file>\n", escapeXML(r.FilePath))
		fmt.Fprintf(&sb, "      <symbol kind=\"%s\">%s</symbol>\n",
			escapeXML(r.SymbolKind), escapeXML(r.SymbolName))
		fmt.Fprintf(&sb, "      <line>%d</line>\n", r.StartLine)
		fmt.Fprintf(&sb, "      <language>%s</language>\n", escapeXML(r.Language))
		fmt.Fprintf(&sb, "    </result>\n")
	}
	sb.WriteString("  </results>\n</response>")
	return sb.String()
}

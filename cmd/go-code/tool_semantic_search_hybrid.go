package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/anatolykoptev/go-code/internal/codesearch"
	"github.com/anatolykoptev/go-code/internal/embeddings"
	"github.com/anatolykoptev/go-code/internal/oxcodes"
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

// runScopedKeywordSearch finds keyword matches inside function bodies via ox-codes.
// More precise than full-file grep — avoids imports, comments, strings.
// Returns nil when ox-codes unavailable (caller falls back to runKeywordSearch).
func runScopedKeywordSearch(ctx context.Context, client *oxcodes.Client, query, root, language string) []embeddings.FileLineHit {
	if client == nil {
		return nil
	}
	kws := embeddings.ExtractQueryKeywords(query)
	if len(kws) == 0 {
		return nil
	}
	pattern := strings.Join(kws, "|")
	isRegex := len(kws) > 1
	resp, err := client.SearchScoped(ctx, oxcodes.ScopedSearchInput{
		Root:       root,
		Pattern:    pattern,
		Scope:      "function_bodies",
		Language:   language,
		IsRegex:    isRegex,
		MaxResults: 30,
	})
	if err != nil || resp == nil {
		return nil
	}
	hits := make([]embeddings.FileLineHit, 0, len(resp.Matches))
	for _, m := range resp.Matches {
		hits = append(hits, embeddings.FileLineHit{FilePath: m.File, Line: m.Line})
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

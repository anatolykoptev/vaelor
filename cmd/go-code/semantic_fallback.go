package main

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/anatolykoptev/go-code/internal/codegraph"
	"github.com/anatolykoptev/go-code/internal/embeddings"
)

const semanticFallbackTopK = 5

// semanticSuggest runs a semantic search as fallback when the primary tool found nothing.
// Returns formatted XML suggestions string, or empty string if unavailable or no results.
func semanticSuggest(ctx context.Context, sem *SemanticDeps, root, query, language string) string {
	if sem == nil || sem.Client == nil || sem.Store == nil {
		return ""
	}

	repoKey := codegraph.GraphNameFor(root)

	vector, err := sem.Client.EmbedQuery(ctx, query)
	if err != nil {
		slog.Debug("semantic fallback: embed failed", slog.Any("error", err))
		return ""
	}

	results, err := sem.Store.Search(ctx, vector, embeddings.SearchOpts{
		RepoKey:  repoKey,
		Language: language,
		TopK:     semanticFallbackTopK,
	})
	if err != nil || len(results) == 0 {
		return ""
	}

	return formatSemanticSuggestions(results)
}

// formatSemanticSuggestions formats semantic search results as XML suggestions block.
func formatSemanticSuggestions(results []embeddings.SearchResult) string {
	var sb strings.Builder
	sb.WriteString("\n<semantic_suggestions>\n")
	sb.WriteString("  <hint>No exact matches found. These symbols are semantically similar to your query:</hint>\n")
	for i, r := range results {
		fmt.Fprintf(&sb, "  <suggestion rank=\"%d\" distance=\"%.4f\">\n", i+1, r.Distance)
		fmt.Fprintf(&sb, "    <symbol kind=\"%s\">%s</symbol>\n",
			escapeXML(r.SymbolKind), escapeXML(r.SymbolName))
		fmt.Fprintf(&sb, "    <file line=\"%d\">%s</file>\n", r.StartLine, escapeXML(r.FilePath))
		sb.WriteString("  </suggestion>\n")
	}
	sb.WriteString("</semantic_suggestions>")
	return sb.String()
}

package main

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/anatolykoptev/go-code/internal/codegraph"
	"github.com/anatolykoptev/go-code/internal/embeddings"
)

const (
	semanticFallbackTopK = 5

	// semanticFallbackEmbedTimeout caps the EmbedQuery call inside semanticSuggest.
	// The fallback is best-effort "did you mean…?" — a dead/slow embed server
	// must degrade to empty suggestions, NOT consume the parent tool's full budget.
	// Root-cause: 2026-05-27 understand-timeout incident (embed.krolik.tools 504).
	semanticFallbackEmbedTimeout = 5 * time.Second
)

// semanticSuggest runs a semantic search as fallback when the primary tool found nothing.
// Returns formatted XML suggestions string, or empty string if unavailable or no results.
func semanticSuggest(ctx context.Context, sem *SemanticDeps, root, query, language string) string {
	if sem == nil || sem.Client == nil || sem.Store == nil {
		return ""
	}

	repoKey := codegraph.GraphNameFor(root)

	// Bound the embed call to a short sub-context: the fallback is best-effort,
	// so a dead embed server should degrade to no suggestions rather than
	// blocking the entire tool budget. DeadlineExceeded is handled by the
	// existing slog.Debug below.
	embedCtx, cancel := context.WithTimeout(ctx, semanticFallbackEmbedTimeout)
	defer cancel()

	vector, err := sem.Client.EmbedQuery(embedCtx, query)
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

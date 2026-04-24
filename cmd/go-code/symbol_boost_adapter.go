package main

import (
	"context"

	"github.com/anatolykoptev/go-code/internal/analyze"
	"github.com/anatolykoptev/go-code/internal/embeddings"
)

// symbolBoostAdapter wraps *embeddings.Store to satisfy analyze.SymbolNameSearcher.
// It bridges the embeddings.SearchResult type to analyze.SymbolHit.
type symbolBoostAdapter struct {
	store *embeddings.Store
}

// SearchBySymbolName delegates to the embeddings store and maps results to SymbolHit.
func (a *symbolBoostAdapter) SearchBySymbolName(
	ctx context.Context,
	repoKey string,
	keywords []string,
	language string,
	limit int,
) ([]analyze.SymbolHit, error) {
	results, err := a.store.SearchBySymbolName(ctx, repoKey, keywords, language, limit)
	if err != nil {
		return nil, err
	}
	hits := make([]analyze.SymbolHit, len(results))
	for i, r := range results {
		hits[i] = analyze.SymbolHit{FilePath: r.FilePath}
	}
	return hits, nil
}

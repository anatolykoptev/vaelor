package codegraph

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/anatolykoptev/go-code/internal/graphx"
)

// defaultTopK is the fallback result count when TopPageRank is called with k<=0.
const defaultTopK = 20

// Compile-time interface satisfaction checks.
var _ graphx.Analytics = (*analyticsAdapter)(nil)
var _ graphx.CrossRefs = (*crossRefsAdapter)(nil)

// analyticsAdapter bridges *Store to graphx.Analytics.
type analyticsAdapter struct {
	store *Store
}

// NewAnalyticsAdapter wraps a Store as a graphx.Analytics.
// The returned value is safe to use concurrently; it inherits the pool's concurrency.
func NewAnalyticsAdapter(s *Store) graphx.Analytics {
	return &analyticsAdapter{store: s}
}

// Symbol returns the pagerank, community, and surprise signals for a single
// symbol. Returns Signals{Found:false} and nil error when the graph has no
// snapshot or the symbol is absent.
func (a *analyticsAdapter) Symbol(ctx context.Context, repoKey, symbolName, file string) (graphx.Signals, error) {
	if a.store == nil {
		return graphx.Signals{}, nil
	}

	graphName := GraphNameFor(repoKey)
	cypher := fmt.Sprintf(
		"MATCH (s:Symbol {name: '%s', file: '%s'}) RETURN s.pagerank, s.community",
		escapeCypher(symbolName), escapeCypher(file),
	)

	rows, err := a.store.ExecCypher(ctx, graphName, cypher, 2)
	if err != nil {
		if isGraphMissingError(err) {
			slog.Debug("codegraph.analyticsAdapter.Symbol: graph absent",
				slog.String("repo", repoKey), slog.String("symbol", symbolName))
			return graphx.Signals{}, nil
		}
		return graphx.Signals{}, fmt.Errorf("analytics symbol query: %w", err)
	}
	if len(rows) == 0 {
		return graphx.Signals{}, nil
	}

	row := rows[0]
	pr := atofSafe(row[0])
	community := strings.Trim(row[1], `"`)

	return graphx.Signals{
		PageRank:  pr,
		Community: community,
		Found:     true,
	}, nil
}

// TopPageRank returns the k symbols with the highest pagerank in the repo,
// ordered descending. Returns nil, nil when the graph has no snapshot.
func (a *analyticsAdapter) TopPageRank(ctx context.Context, repoKey string, k int) ([]graphx.Signal, error) {
	if a.store == nil {
		return nil, nil
	}
	if k <= 0 {
		k = defaultTopK
	}

	graphName := GraphNameFor(repoKey)
	cypher := fmt.Sprintf(
		"MATCH (s:Symbol) WHERE s.pagerank IS NOT NULL RETURN s.name, s.file, s.pagerank ORDER BY s.pagerank DESC LIMIT %d",
		k,
	)

	rows, err := a.store.ExecCypher(ctx, graphName, cypher, 3)
	if err != nil {
		if isGraphMissingError(err) {
			slog.Debug("codegraph.analyticsAdapter.TopPageRank: graph absent",
				slog.String("repo", repoKey))
			return nil, nil
		}
		return nil, fmt.Errorf("analytics top pagerank query: %w", err)
	}

	signals := make([]graphx.Signal, 0, len(rows))
	for _, row := range rows {
		name := strings.Trim(row[0], `"`)
		file := strings.Trim(row[1], `"`)
		pr := atofSafe(row[2])
		signals = append(signals, graphx.Signal{
			Symbol:  graphx.SymbolRef{Name: name, File: file},
			Signals: graphx.Signals{PageRank: pr, Found: true},
		})
	}
	return signals, nil
}

// isGraphMissingError returns true if the error indicates a missing AGE graph.
// PostgreSQL SQLSTATE 3F000 = "invalid schema name" (raised by AGE when the
// graph does not exist yet).
func isGraphMissingError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "3F000") ||
		strings.Contains(msg, "does not exist") ||
		strings.Contains(msg, "invalid schema name")
}

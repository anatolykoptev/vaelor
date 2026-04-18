package codegraph

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/anatolykoptev/go-code/internal/graphx"
)

// crossRefsAdapter bridges *Store to graphx.CrossRefs.
type crossRefsAdapter struct {
	store *Store
}

// NewCrossRefsAdapter wraps a Store as a graphx.CrossRefs.
func NewCrossRefsAdapter(s *Store) graphx.CrossRefs {
	return &crossRefsAdapter{store: s}
}

// HandlesRoute returns the HTTP Route served by a handler symbol.
// Returns Route{}, false, nil when absent or when the graph has no snapshot.
func (c *crossRefsAdapter) HandlesRoute(ctx context.Context, repoKey, symbolName, file string) (graphx.Route, bool, error) {
	if c.store == nil {
		return graphx.Route{}, false, nil
	}

	graphName := GraphNameFor(repoKey)
	cypher := fmt.Sprintf(
		"MATCH (s:Symbol {name: '%s', file: '%s'})-[:HANDLES]->(r:Route) RETURN r.method, r.path",
		escapeCypher(symbolName), escapeCypher(file),
	)

	rows, err := c.store.ExecCypher(ctx, graphName, cypher, 2)
	if err != nil {
		if isGraphMissingError(err) {
			slog.Debug("codegraph.crossRefsAdapter.HandlesRoute: graph absent",
				slog.String("repo", repoKey), slog.String("symbol", symbolName))
			return graphx.Route{}, false, nil
		}
		return graphx.Route{}, false, fmt.Errorf("crossrefs handles route query: %w", err)
	}
	if len(rows) == 0 {
		return graphx.Route{}, false, nil
	}

	row := rows[0]
	route := graphx.Route{
		Method: strings.Trim(row[0], `"`),
		Path:   strings.Trim(row[1], `"`),
	}
	return route, true, nil
}

// FetchedBy returns the frontend (or upstream) symbols that issue HTTP
// requests to the given route. Returns nil, nil when none are found.
func (c *crossRefsAdapter) FetchedBy(ctx context.Context, repoKey string, route graphx.Route) ([]graphx.SymbolRef, error) {
	if c.store == nil {
		return nil, nil
	}

	graphName := GraphNameFor(repoKey)
	cypher := fmt.Sprintf(
		"MATCH (client:Symbol)-[:FETCHES]->(r:Route {method: '%s', path: '%s'}) RETURN client.name, client.file",
		escapeCypher(route.Method), escapeCypher(route.Path),
	)

	rows, err := c.store.ExecCypher(ctx, graphName, cypher, 2)
	if err != nil {
		if isGraphMissingError(err) {
			slog.Debug("codegraph.crossRefsAdapter.FetchedBy: graph absent",
				slog.String("repo", repoKey))
			return nil, nil
		}
		return nil, fmt.Errorf("crossrefs fetched by query: %w", err)
	}

	refs := make([]graphx.SymbolRef, 0, len(rows))
	for _, row := range rows {
		refs = append(refs, graphx.SymbolRef{
			Name: strings.Trim(row[0], `"`),
			File: strings.Trim(row[1], `"`),
		})
	}
	return refs, nil
}

// TestedBy returns test functions that directly cover the given production symbol.
// Returns nil, nil when no test edges are recorded.
func (c *crossRefsAdapter) TestedBy(ctx context.Context, repoKey, symbolName, file string) ([]graphx.SymbolRef, error) {
	if c.store == nil {
		return nil, nil
	}

	graphName := GraphNameFor(repoKey)
	cypher := fmt.Sprintf(
		"MATCH (test:Symbol)-[:TESTED_BY]->(s:Symbol {name: '%s', file: '%s'}) RETURN test.name, test.file",
		escapeCypher(symbolName), escapeCypher(file),
	)

	rows, err := c.store.ExecCypher(ctx, graphName, cypher, 2)
	if err != nil {
		if isGraphMissingError(err) {
			slog.Debug("codegraph.crossRefsAdapter.TestedBy: graph absent",
				slog.String("repo", repoKey), slog.String("symbol", symbolName))
			return nil, nil
		}
		return nil, fmt.Errorf("crossrefs tested by query: %w", err)
	}

	refs := make([]graphx.SymbolRef, 0, len(rows))
	for _, row := range rows {
		refs = append(refs, graphx.SymbolRef{
			Name: strings.Trim(row[0], `"`),
			File: strings.Trim(row[1], `"`),
		})
	}
	return refs, nil
}

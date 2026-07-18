package codegraph

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/anatolykoptev/vaelor/internal/analyze"
	"github.com/anatolykoptev/vaelor/internal/graphx"
)

// defaultTopK is the fallback result count when TopPageRank is called with k<=0.
const defaultTopK = 20

// Compile-time interface satisfaction checks.
var _ graphx.Analytics = (*analyticsAdapter)(nil)
var _ graphx.CrossRefs = (*crossRefsAdapter)(nil)

// analyticsAdapter bridges *Store to graphx.Analytics.
type analyticsAdapter struct {
	store    *Store
	mappings map[string]string // host->container prefix mappings from PATH_MAPPINGS
}

// NewAnalyticsAdapter wraps a Store as a graphx.Analytics.
// mappings converts []analyze.PathMapping (External->Internal) into a simple
// map used by repoKeyToHostPath for host<->container path rewriting.
// The returned value is safe to use concurrently; it inherits the pool's concurrency.
func NewAnalyticsAdapter(s *Store, mappings []analyze.PathMapping) graphx.Analytics {
	m := make(map[string]string, len(mappings))
	for _, pm := range mappings {
		m[pm.External] = pm.Internal
	}
	return &analyticsAdapter{store: s, mappings: m}
}

// Symbol returns the pagerank, community, and surprise signals for a single
// symbol. Returns Signals{Found:false} and nil error when the graph has no
// snapshot or the symbol is absent.
func (a *analyticsAdapter) Symbol(ctx context.Context, repoKey, symbolName, file string) (graphx.Signals, error) {
	if a.store == nil {
		return graphx.Signals{}, nil
	}

	graphName := GraphNameFor(repoKey)
	relFile := toRelativeFile(repoKey, file, a.mappings)

	// Preflight: avoid postgres ERROR logs for repos that were never indexed.
	// The IsGraphMissingError guard below remains as a race fallback.
	if err := a.store.EnsureGraphExistsForRead(ctx, graphName); err != nil {
		if errors.Is(err, ErrGraphNotIndexed) {
			recordGraphMissing("adapter_symbol")
			slog.Debug("codegraph.analyticsAdapter.Symbol: graph absent (preflight)",
				slog.String("repo", repoKey), slog.String("symbol", symbolName))
			return graphx.Signals{}, nil
		}
		return graphx.Signals{}, err
	}

	cypher := fmt.Sprintf(
		"MATCH (s:Symbol {name: '%s', file: '%s'}) RETURN s.pagerank, s.community, s.surprise LIMIT 1",
		escapeCypher(symbolName), escapeCypher(relFile),
	)

	const symbolQueryCols = 3
	rows, err := a.store.ExecCypher(ctx, graphName, cypher, symbolQueryCols)
	if err != nil {
		if IsGraphMissingError(err) {
			a.store.existsCache.Forget(graphName)
			recordGraphMissing("adapter_symbol")
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
	surprise := atofSafe(row[2]) // 0 when NULL (graph built without surprise index)

	return graphx.Signals{
		PageRank:  pr,
		Community: community,
		Surprise:  surprise,
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

	// Preflight: avoid postgres ERROR logs for repos that were never indexed.
	// The IsGraphMissingError guard below remains as a race fallback.
	if err := a.store.EnsureGraphExistsForRead(ctx, graphName); err != nil {
		if errors.Is(err, ErrGraphNotIndexed) {
			recordGraphMissing("adapter_callers")
			slog.Debug("codegraph.analyticsAdapter.TopPageRank: graph absent (preflight)",
				slog.String("repo", repoKey))
			return nil, nil
		}
		return nil, err
	}

	cypher := fmt.Sprintf(
		"MATCH (s:Symbol) WHERE s.pagerank IS NOT NULL RETURN s.name, s.file, s.pagerank ORDER BY s.pagerank DESC LIMIT %d",
		k,
	)

	rows, err := a.store.ExecCypher(ctx, graphName, cypher, 3)
	if err != nil {
		if IsGraphMissingError(err) {
			a.store.existsCache.Forget(graphName)
			recordGraphMissing("adapter_callers")
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

// repoKeyToHostPath rewrites a repo key using the first matching prefix in
// mappings (External->Internal). Returns repoKey unchanged when no prefix matches.
// This replaces the former hardcoded host-path substitution.
func repoKeyToHostPath(repoKey string, mappings map[string]string) string {
	for src, dst := range mappings {
		if strings.HasPrefix(repoKey, src) {
			return dst + strings.TrimPrefix(repoKey, src)
		}
	}
	return repoKey
}

// toRelativeFile normalises an incoming file path into the repo-root-relative
// form AGE actually stores.
//
// Tool callers typically pass absolute container paths
// ("/host/src/repo/crates/x/y.rs"), but AGE stores Symbol.file as relative
// ("crates/x/y.rs"). We try three strategies in order:
//  1. filepath.Rel(repoKey, file) -- works when both sides share the same prefix.
//  2. filepath.Rel(repoKeyToHostPath(repoKey, mappings), file) -- host<->container
//     rewrite driven by PATH_MAPPINGS (e.g. /path/to/repos:/host).
//  3. Give up and return the input unchanged -- the Cypher lookup will miss,
//     which is the safe failure mode (Found=false, no incorrect enrichment).
func toRelativeFile(repoKey, file string, mappings map[string]string) string {
	if file == "" || !filepath.IsAbs(file) {
		return file
	}
	if rel, err := filepath.Rel(repoKey, file); err == nil && !strings.HasPrefix(rel, "..") {
		return rel
	}
	containerKey := repoKeyToHostPath(repoKey, mappings)
	if containerKey != repoKey {
		if rel, err := filepath.Rel(containerKey, file); err == nil && !strings.HasPrefix(rel, "..") {
			return rel
		}
	}
	return file
}

package embeddings

import (
	"context"
	"fmt"
	"os"
	"testing"

	pgvector "github.com/pgvector/pgvector-go"
)

// TestSearchBySymbolName_MultiKeywordPrefilter is the #643 regression test.
//
// The old implementation joined query keywords into one string and ran a
// single pg_trgm similarity() > 0.1 prefilter. Joining dilutes trigram
// overlap: for a multi-word NL query a short symbol name scores below the
// threshold, the candidate set comes back empty, and BM25F silently falls
// back to grep. The fix ORs a per-keyword similarity check via unnest.
//
// Fixture discipline: BOTH the symbol name and its file path are asserted
// below 0.1 similarity to the JOINED string up front (measured: 0.023 and
// 0.032 vs 'connect postgres dsn audit trail migrate'), so the fixture
// provably exercises the bug — a candidate the old query would also have
// found cannot detect the fix. The per-keyword match comes through
// similarity('db','dsn') = 0.167.
func TestSearchBySymbolName_MultiKeywordPrefilter(t *testing.T) {
	pool := testPool(t)
	s := NewStore(pool)
	ctx := context.Background()
	if err := s.EnsureSchema(ctx); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}

	repoKey := fmt.Sprintf("test_bm25f_%d", os.Getpid())
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM public.code_embeddings WHERE repo_key = $1", repoKey)
	})

	const (
		sym     = "db"
		path    = "internal/store/pool.go"
		joinedQ = "connect postgres dsn audit trail migrate"
	)
	keywords := []string{"connect", "postgres", "dsn", "audit", "trail", "migrate"}

	_, err := pool.Exec(ctx, `
		INSERT INTO public.code_embeddings
		    (repo_key, file_path, symbol_name, symbol_kind, language, start_line, embedding)
		VALUES ($1, $2, $3, 'function', 'go', 10, $4)`,
		repoKey, path, sym, pgvector.NewVector(makeVec()))
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Fixture validity gate: the OLD joined-string prefilter must NOT have
	// matched this row on either field, otherwise the test cannot detect
	// the regression (a >0.1 fixture is green under both implementations).
	for field, val := range map[string]string{"symbol_name": sym, "file_path": path} {
		var joinedSim float64
		if err := pool.QueryRow(ctx,
			`SELECT similarity($1, $2)`, val, joinedQ).Scan(&joinedSim); err != nil {
			t.Fatalf("similarity probe %s: %v", field, err)
		}
		if joinedSim >= 0.1 {
			t.Fatalf("fixture degenerate: similarity(%s=%q, joined) = %f >= 0.1 — "+
				"the old query would have found it anyway", field, val, joinedSim)
		}
	}

	results, err := s.SearchBySymbolName(ctx, repoKey, keywords, "", 10)
	if err != nil {
		t.Fatalf("SearchBySymbolName: %v", err)
	}
	found := false
	for _, r := range results {
		if r.SymbolName == sym {
			found = true
		}
	}
	if !found {
		t.Fatalf("per-keyword prefilter missed seeded symbol %q — "+
			"multi-word NL query still falls through to grep fallback", sym)
	}
}

package embeddings

import (
	"context"
	"testing"

	dto "github.com/prometheus/client_model/go"
)

// --- helpers ---

// bm25FailCounterValue reads the current value of bm25QueryFailTotal for stage.
func bm25FailCounterValue(stage string) float64 {
	c := bm25QueryFailTotal.WithLabelValues(stage)
	m := &dto.Metric{}
	if err := c.Write(m); err != nil {
		return 0
	}
	if m.Counter != nil {
		return m.Counter.GetValue()
	}
	return 0
}

// bm25EmptyQueryCounterValue reads the current value of bm25EmptyQueryTotal.
func bm25EmptyQueryCounterValue() float64 {
	m := &dto.Metric{}
	if err := bm25EmptyQueryTotal.Write(m); err != nil {
		return 0
	}
	if m.Counter != nil {
		return m.Counter.GetValue()
	}
	return 0
}

// --- Test A: BM25F ranking correctness — exact-identifier candidate beats partial match ---
//
// TestBM25Search_SymbolFieldBoostDrivesRanking verifies that the symbol×5 field
// boost in Document construction overrides path-field scoring to produce correct ranking.
//
// Design uses two candidates that BOTH pass the trigram prefilter for query "parseConf":
//   - "parseConfig" in "logic.go"  — symbol matches "parse"/"config" via SplitIdentifier
//     (Symbol×5 hits); path "logic.go" has NO query token (path scores 0).
//   - "getResult"   in "parseutil.go" — symbol "getresult" has NO "parse" or "conf" token
//     (symbol scores 0); path "parseutil.go" DOES contain "parse" (path×3 hit).
//
// With correct Symbols wiring:
//
//	BM25F("parseConfig") ≈ WeightSymbol×2 hits × IDF ≫ BM25F("getResult") ≈ WeightPath×1 hit × IDF
//	→ parseConfig ranks first.
//
// Falsification (Symbols: nil):
//
//	"parseConfig" → symbol scores 0, path "logic.go" scores 0 → total = 0.
//	"getResult"   → symbol scores 0, path "parseutil.go" hits "parse" → total > 0.
//	→ "getResult" ranks first — wrong answer. hits[0].SymbolName != "parseConfig" → RED.
//
// This proves the Symbols field wiring is load-bearing: without it, path-only scoring
// produces the wrong winner.
func TestBM25Search_SymbolFieldBoostDrivesRanking(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	store := NewStore(pool)

	if err := store.EnsureSchema(ctx); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}

	const repo = "test/bm25-symbol-field-boost"
	_ = store.DeleteRepo(ctx, repo)
	t.Cleanup(func() { _ = store.DeleteRepo(ctx, repo) })

	records := []EmbeddingRecord{
		{
			// TARGET: symbol "parseConfig" in a path with no query tokens.
			// SplitIdentifier("parseConfig") = ["parse","config"]; query "parseConf"
			// tokenizes to ["parseconf","parse","conf"]. "parse" hits symbol field ×5.
			RepoKey:    repo,
			FilePath:   "logic.go",
			SymbolName: "parseConfig",
			SymbolKind: "function",
			Language:   "go",
			StartLine:  10,
			Embedding:  makeVec(0.1),
		},
		{
			// DECOY: symbol "getResult" in a path that contains "parse".
			// SplitIdentifier("getResult") = ["get","result"]; no "parse"/"conf" hits on symbol.
			// Path "parseutil.go" contains "parse" → path field ×3 hit.
			// With Symbols nil: decoy WINS (path×3 > 0). With Symbols populated: target WINS.
			RepoKey:    repo,
			FilePath:   "parseutil.go",
			SymbolName: "getResult",
			SymbolKind: "function",
			Language:   "go",
			StartLine:  20,
			Embedding:  makeVec(0.2),
		},
	}
	if err := store.Upsert(ctx, records); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	hits, err := store.BM25Search(ctx, repo, "parseConf", "go", 10)
	if err != nil {
		t.Fatalf("BM25Search: %v", err)
	}

	// Both candidates must appear (both trigram-match: "parseConfig" matches
	// "parseConf" directly; "parseutil.go" contains "parse" which trigram-matches).
	if len(hits) < 2 {
		t.Fatalf("expected ≥2 hits (both candidates pass trigram prefilter), got %d: %+v", len(hits), hits)
	}

	// parseConfig must rank first: symbol×5 field boost (["parse","config"] subwords hit)
	// dominates decoy's path×3 hit ("parseutil.go" path). If Symbols field is nil in
	// Document construction, getResult wins via path scoring — this assertion goes RED.
	if hits[0].SymbolName != "parseConfig" {
		t.Errorf("expected parseConfig at rank 1 (symbol×5 field boost > path×3 decoy), got %q at rank 1; full ranking: %+v", hits[0].SymbolName, hits)
	}
}

// --- Test B: candidate-set scoping — BM25F sees only prefilter candidates ---
//
// TestBM25Search_CandidateScoping seeds three symbols in a repo.
// Only two have names that trigram-match the query "loadDatabase"; the third
// ("unrelatedWidget") has no matching tokens. We assert that:
//  1. The two matching candidates appear in results.
//  2. The unrelated symbol does NOT appear (confirming BM25F is scoped to
//     the trigram prefilter output, not the full table).
//
// Falsification: if BM25Search were to bypass SearchBySymbolName and scan
// the full table (e.g. by passing all symbols to NewBM25F regardless of prefilter),
// "unrelatedWidget" would potentially appear in results. This test goes RED
// whenever BM25F sees symbols outside the candidate set.
//
// Note: the prefilter may not return "unrelatedWidget" even if we forgot the
// limit — the trigram similarity threshold enforces the exclusion at SQL level.
// The test verifies the combined pipeline (prefilter + BM25F) excludes it.
func TestBM25Search_CandidateScoping(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	store := NewStore(pool)

	if err := store.EnsureSchema(ctx); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}

	const repo = "test/bm25-candidate-scoping"
	_ = store.DeleteRepo(ctx, repo)
	t.Cleanup(func() { _ = store.DeleteRepo(ctx, repo) })

	records := []EmbeddingRecord{
		{
			RepoKey:    repo,
			FilePath:   "db/loader.go",
			SymbolName: "loadDatabase",
			SymbolKind: "function",
			Language:   "go",
			StartLine:  5,
			Embedding:  makeVec(0.1),
		},
		{
			RepoKey:    repo,
			FilePath:   "db/connect.go",
			SymbolName: "openDatabaseConnection",
			SymbolKind: "function",
			Language:   "go",
			StartLine:  15,
			Embedding:  makeVec(0.2),
		},
		{
			RepoKey:    repo,
			FilePath:   "ui/widget.go",
			SymbolName: "unrelatedWidget",
			SymbolKind: "function",
			Language:   "go",
			StartLine:  30,
			Embedding:  makeVec(0.3),
		},
	}
	if err := store.Upsert(ctx, records); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	hits, err := store.BM25Search(ctx, repo, "loadDatabase", "go", 10)
	if err != nil {
		t.Fatalf("BM25Search: %v", err)
	}

	// Verify unrelatedWidget is NOT in results.
	for _, h := range hits {
		if h.SymbolName == "unrelatedWidget" {
			t.Errorf("unrelatedWidget must not appear: BM25F scoped to trigram prefilter, not full table")
		}
	}

	// Verify at least the exact-match candidate appears.
	found := false
	for _, h := range hits {
		if h.SymbolName == "loadDatabase" {
			found = true
		}
	}
	if !found {
		t.Errorf("loadDatabase not in results: expected it as an exact-match candidate; got %+v", hits)
	}
}

// --- Test C: empty/stopword query → empty non-fatal, counter bumped ---
//
// TestBM25Search_EmptyQuery_ReturnsNilNoDBHit asserts that a query producing
// zero tokens (empty string) returns (nil, nil) without touching the DB and
// increments bm25EmptyQueryTotal.
//
// Falsification: remove the `if len(terms) == 0 { return nil, nil }` gate in
// BM25Search → the function proceeds to SearchBySymbolName with an empty keyword
// list, which returns nil per its own guard, but the bm25EmptyQueryTotal counter
// is NOT bumped — the counter assertion goes RED. Alternatively, if the empty-term
// guard is removed AND SearchBySymbolName is called with empty keywords on a pool
// with no connection (nil pool), it panics.
func TestBM25Search_EmptyQuery_ReturnsNilNoDBHit(t *testing.T) {
	// nil pool: any DB call would panic, proving no DB I/O occurs.
	store := &Store{}

	before := bm25EmptyQueryCounterValue()

	hits, err := store.BM25Search(context.Background(), "any-repo", "", "go", 10)
	if err != nil {
		t.Errorf("empty query must be non-fatal, got err=%v", err)
	}
	if hits != nil {
		t.Errorf("expected nil hits for empty query, got %v", hits)
	}

	after := bm25EmptyQueryCounterValue()
	if delta := after - before; delta != 1 {
		t.Errorf("bm25_empty_query counter: expected delta 1, got %g (before=%g after=%g)", delta, before, after)
	}
}

// TestBM25Search_FetchFailure_BumpsCounterAndReturnsNil asserts that when the
// underlying SearchBySymbolName returns an error (simulated via a closed/nil
// pool and a non-empty term list), BM25Search:
//   - returns (nil, nil) — non-fatal
//   - bumps bm25QueryFailTotal{stage="fetch"}
//
// Falsification: remove the `bm25QueryFailTotal.WithLabelValues("fetch").Inc()`
// call from the err branch in BM25Search → counter delta stays 0 and the test
// goes RED.
func TestBM25Search_FetchFailure_BumpsCounterAndReturnsNil(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	store := NewStore(pool)

	if err := store.EnsureSchema(ctx); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}

	// Use a closed pool to force a DB error on the candidate fetch.
	pool.Close()

	before := bm25FailCounterValue("fetch")

	// "getUserName" has real tokens so lextoken.Tokenize returns non-empty.
	hits, err := store.BM25Search(ctx, "nonexistent-repo", "getUserName", "go", 10)
	if err != nil {
		t.Errorf("fetch failure must be swallowed (non-fatal), got err=%v", err)
	}
	if hits != nil {
		t.Errorf("expected nil hits on fetch failure, got %v", hits)
	}

	after := bm25FailCounterValue("fetch")
	if delta := after - before; delta != 1 {
		t.Errorf("fetch counter: expected delta 1, got %g (before=%g after=%g)", delta, before, after)
	}
}

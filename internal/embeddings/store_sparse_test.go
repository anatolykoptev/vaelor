package embeddings

import (
	"context"
	"testing"

	"github.com/anatolykoptev/go-kit/sparse"
	dto "github.com/prometheus/client_model/go"
)

// --- helpers ---

// sparseQueryCounterValue reads the current value of sparseQueryFailTotal for stage.
func sparseQueryCounterValue(stage string) float64 {
	c := sparseQueryFailTotal.WithLabelValues(stage)
	m := &dto.Metric{}
	if err := c.Write(m); err != nil {
		return 0
	}
	if m.Counter != nil {
		return m.Counter.GetValue()
	}
	return 0
}

// staticSparseEmbedder is a SparseEmbedder that always returns a fixed vector.
// Used in unit tests to control the query-side embed path without a real HTTP server.
type staticSparseEmbedder struct {
	vec sparse.SparseVector
	err error
}

func (s *staticSparseEmbedder) EmbedSparse(_ context.Context, texts []string) ([]sparse.SparseVector, error) {
	if s.err != nil {
		return nil, s.err
	}
	out := make([]sparse.SparseVector, len(texts))
	for i := range out {
		out[i] = s.vec
	}
	return out, nil
}
func (s *staticSparseEmbedder) EmbedSparseQuery(ctx context.Context, text string) (sparse.SparseVector, error) {
	if s.err != nil {
		return sparse.SparseVector{}, s.err
	}
	return s.vec, nil
}
func (s *staticSparseEmbedder) VocabSize() int { return sparseDim }
func (s *staticSparseEmbedder) Close() error   { return nil }

// --- HNSW index DDL tests (require live DB) ---

// TestEnsureSchema_SparseHNSWIndex asserts that after EnsureSchema the
// code_embeddings_sparse_hnsw index exists with the expected access method and
// operator class. Re-running EnsureSchema must be a no-op (idempotent).
//
// Falsification: remove the CREATE INDEX line from schemaSQL → query returns
// 0 rows and t.Fatalf fires (red).
func TestEnsureSchema_SparseHNSWIndex(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	store := NewStore(pool)

	if err := store.EnsureSchema(ctx); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	// Idempotency: run again — must not error.
	if err := store.EnsureSchema(ctx); err != nil {
		t.Fatalf("EnsureSchema second run: %v", err)
	}

	const q = `
SELECT indexname, indexdef
FROM pg_indexes
WHERE schemaname = 'public'
  AND tablename  = 'code_embeddings'
  AND indexname  = 'code_embeddings_sparse_hnsw'`

	var indexname, indexdef string
	if err := pool.QueryRow(ctx, q).Scan(&indexname, &indexdef); err != nil {
		t.Fatalf("sparse HNSW index missing after EnsureSchema: %v", err)
	}
	// Verify it uses hnsw access method and sparsevec_ip_ops operator class.
	for _, want := range []string{"hnsw", "sparsevec_ip_ops"} {
		if indexdef == "" {
			t.Errorf("empty indexdef for code_embeddings_sparse_hnsw")
		}
		if !containsSubstring(indexdef, want) {
			t.Errorf("indexdef missing %q; got: %s", want, indexdef)
		}
	}
}

func containsSubstring(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && stringContains(s, sub))
}

func stringContains(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// --- SearchSparse gating tests (no DB required) ---

// TestSearchSparse_NilClient_ReturnsEmpty asserts that when sparseClient is nil,
// SearchSparse returns (nil, nil) without touching the DB.
//
// Falsification: remove the `if sparseClient == nil { return nil, nil }` guard
// from SearchSparse → the function will attempt to embed with a nil client, panic
// or return an error, and this test goes RED.
func TestSearchSparse_NilClient_ReturnsEmpty(t *testing.T) {
	store := &Store{} // no pool — would panic if a DB call were issued
	hits, err := store.SearchSparse(context.Background(), "find something", nil, SearchOpts{})
	if err != nil {
		t.Errorf("expected nil error for nil client, got %v", err)
	}
	if hits != nil {
		t.Errorf("expected nil hits for nil client, got %v", hits)
	}
}

// TestSearchSparse_EmptyQueryVector_ReturnsEmpty asserts that a query whose
// sparse embedding is fully degenerate (all-stopword query → empty literal)
// causes SearchSparse to return (nil, nil) without a DB round-trip.
//
// Falsification: remove the `if lit == "" { return nil, nil }` guard in
// SearchSparse → the function proceeds to EnsureSchema on a nil pool, panics,
// and the test goes RED.
func TestSearchSparse_EmptyQueryVector_ReturnsEmpty(t *testing.T) {
	// Static embedder returns an all-zero vector → SanitizeAndFormat returns "".
	emb := &staticSparseEmbedder{
		vec: sparse.SparseVector{
			Indices: []uint32{30522}, // OOB → stripped by sanitize
			Values:  []float32{0.0},  // zero-weight → stripped by sanitize
		},
	}
	store := &Store{} // nil pool: would panic if DB reached
	hits, err := store.SearchSparse(context.Background(), "stopwords only", emb, SearchOpts{})
	if err != nil {
		t.Errorf("expected nil error for empty query vector, got %v", err)
	}
	if hits != nil {
		t.Errorf("expected nil hits for empty query vector, got %v", hits)
	}
}

// TestSearchSparse_EmbedFailure_BumpsCounter verifies that an EmbedSparseQuery
// failure increments gocode_sparse_query_failures_total{stage="embed"} and
// returns (nil, nil) — non-fatal, search continues as dense+keyword only.
//
// Falsification: remove the sparseQueryFailTotal.WithLabelValues("embed").Inc()
// line from SearchSparse → counter delta stays 0, test goes RED.
func TestSearchSparse_EmbedFailure_BumpsCounter(t *testing.T) {
	emb := &staticSparseEmbedder{err: errInjected}
	store := &Store{} // nil pool: must not reach DB

	before := sparseQueryCounterValue("embed")
	hits, err := store.SearchSparse(context.Background(), "any query", emb, SearchOpts{})
	after := sparseQueryCounterValue("embed")

	if err != nil {
		t.Errorf("embed failure must be swallowed (non-fatal), got err=%v", err)
	}
	if hits != nil {
		t.Errorf("expected nil hits on embed failure, got %v", hits)
	}
	if delta := after - before; delta != 1 {
		t.Errorf("embed counter: expected delta 1, got %g (before=%g after=%g)", delta, before, after)
	}
}

// errInjected is a sentinel error for test injection.
var errInjected = stringer("injected sparse embed error")

type stringer string

func (s stringer) Error() string { return string(s) }

// --- Query-side sanitization test (no DB) ---

// TestSearchSparse_QuerySidePrune verifies that SanitizeAndFormatSparseVector is
// applied to the query vector before the DB call, capping it to sparseMaxTerms.
// Here we confirm the cap indirectly: if the embedder returns >256 terms, the
// resulting literal must have exactly 256 terms (the formatted string has 256 colons).
//
// This is a pure unit test — it calls SanitizeAndFormatSparseVector directly
// on a >256-term vector to confirm the literal is within the HNSW cap.
//
// Falsification: remove the top-K prune block from SanitizeAndFormatSparseVector
// → the count assertion goes RED (nnz == 400 != 256).
func TestSearchSparse_QuerySidePrune(t *testing.T) {
	const n = 400 // > sparseMaxTerms (256)
	indices := make([]uint32, n)
	values := make([]float32, n)
	for i := range n {
		indices[i] = uint32(i)
		values[i] = 1.0 + float32(i)*0.001
	}
	qvec := sparse.SparseVector{Indices: indices, Values: values}
	lit := SanitizeAndFormatSparseVector(qvec, sparseDim)

	if lit == "" {
		t.Fatal("expected non-empty literal for 400-term vector")
	}
	// Count colons — one per "idx:val" entry.
	nnz := 0
	for _, c := range lit {
		if c == ':' {
			nnz++
		}
	}
	if nnz != sparseMaxTerms {
		t.Errorf("query vector: expected %d terms after prune, got %d", sparseMaxTerms, nnz)
	}
}

// --- Integration: SearchSparse with real seeded DB row ---

// TestSearchSparse_ReturnsRankedResults seeds two rows with known sparse vectors,
// queries with a vector that should match one more than the other, and asserts
// ordering. Uses the live gocode DB (skipped when DATABASE_URL not set).
//
// Falsification: swap the ORDER BY direction (DESC → ASC) in SearchSparse or
// remove the IS NOT NULL filter → row ordering flips or un-seeded rows appear,
// and the first-result assertion goes RED.
func TestSearchSparse_ReturnsRankedResults(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	store := NewStore(pool)

	if err := store.EnsureSchema(ctx); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}

	const repo = "test/sparse-retrieval"
	// Clean up before and after.
	_ = store.DeleteRepo(ctx, repo)
	t.Cleanup(func() { _ = store.DeleteRepo(ctx, repo) })

	// Seed two dense rows first (Upsert requires dense embedding).
	records := []EmbeddingRecord{
		{
			RepoKey: repo, FilePath: "alpha.go", SymbolName: "AlphaFunc",
			SymbolKind: "function", Language: "go",
			Embedding: makeVec(0.1),
		},
		{
			RepoKey: repo, FilePath: "beta.go", SymbolName: "BetaFunc",
			SymbolKind: "function", Language: "go",
			Embedding: makeVec(0.2),
		},
	}
	if err := store.Upsert(ctx, records); err != nil {
		t.Fatalf("upsert dense: %v", err)
	}

	// Write sparse vectors via UpdateSparseEmbeddingsBatch.
	// AlphaFunc: strong token 100 (weight=0.9), weak token 200 (weight=0.1).
	// BetaFunc:  weak token 100 (weight=0.1), strong token 300 (weight=0.9).
	alphaVec := sparse.SparseVector{Indices: []uint32{100, 200}, Values: []float32{0.9, 0.1}}
	betaVec := sparse.SparseVector{Indices: []uint32{100, 300}, Values: []float32{0.1, 0.9}}

	sparseBatch := []SparseUpdate{
		{RepoKey: repo, FilePath: "alpha.go", SymbolName: "AlphaFunc", Literal: SanitizeAndFormatSparseVector(alphaVec, sparseDim)},
		{RepoKey: repo, FilePath: "beta.go", SymbolName: "BetaFunc", Literal: SanitizeAndFormatSparseVector(betaVec, sparseDim)},
	}
	if err := store.UpdateSparseEmbeddingsBatch(ctx, sparseBatch); err != nil {
		t.Fatalf("write sparse batch: %v", err)
	}

	// Query: strong weight on token 100. IP with AlphaFunc = 0.9*0.9 + 0.1*0.1 = 0.82 (< 0)
	// IP with BetaFunc = 0.9*0.1 = 0.09 (<#> returns negative: -0.82 < -0.09)
	// ORDER BY neg_ip ASC → AlphaFunc first.
	queryVec := sparse.SparseVector{Indices: []uint32{100}, Values: []float32{0.9}}
	emb := &staticSparseEmbedder{vec: queryVec}

	hits, err := store.SearchSparse(ctx, "alpha related", emb, SearchOpts{RepoKey: repo, TopK: 10})
	if err != nil {
		t.Fatalf("SearchSparse: %v", err)
	}
	if len(hits) < 2 {
		t.Fatalf("expected ≥2 hits, got %d", len(hits))
	}
	// AlphaFunc must rank first — it has the higher inner product with the query.
	if hits[0].SymbolName != "AlphaFunc" {
		t.Errorf("expected AlphaFunc first (higher IP), got %s", hits[0].SymbolName)
	}
	if hits[1].SymbolName != "BetaFunc" {
		t.Errorf("expected BetaFunc second, got %s", hits[1].SymbolName)
	}
}

// TestSearchSparse_IsNullSkipsUnseededRows asserts that rows without a
// sparse_embedding are not returned by SearchSparse.
//
// Falsification: remove the IS NOT NULL filter from the WHERE clause in
// SearchSparse → the un-seeded row might appear in results (undefined order
// with NULL scores), and if present this test would pass vacuously. We verify
// strictly: seed one row with NULL sparse (dense only) and one with a sparse
// vector; assert only the sparse-seeded row is returned.
func TestSearchSparse_IsNullSkipsUnseededRows(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	store := NewStore(pool)

	if err := store.EnsureSchema(ctx); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}

	const repo = "test/sparse-null-skip"
	_ = store.DeleteRepo(ctx, repo)
	t.Cleanup(func() { _ = store.DeleteRepo(ctx, repo) })

	// Two rows — only one gets a sparse vector.
	records := []EmbeddingRecord{
		{RepoKey: repo, FilePath: "x.go", SymbolName: "WithSparse", SymbolKind: "function", Language: "go", Embedding: makeVec(0.1)},
		{RepoKey: repo, FilePath: "y.go", SymbolName: "NoSparse", SymbolKind: "function", Language: "go", Embedding: makeVec(0.2)},
	}
	if err := store.Upsert(ctx, records); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	sparseLit := SanitizeAndFormatSparseVector(
		sparse.SparseVector{Indices: []uint32{50}, Values: []float32{1.0}},
		sparseDim,
	)
	if err := store.UpdateSparseEmbeddingsBatch(ctx, []SparseUpdate{
		{RepoKey: repo, FilePath: "x.go", SymbolName: "WithSparse", Literal: sparseLit},
	}); err != nil {
		t.Fatalf("write sparse: %v", err)
	}

	emb := &staticSparseEmbedder{
		vec: sparse.SparseVector{Indices: []uint32{50}, Values: []float32{1.0}},
	}
	hits, err := store.SearchSparse(ctx, "query", emb, SearchOpts{RepoKey: repo, TopK: 10})
	if err != nil {
		t.Fatalf("SearchSparse: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("expected exactly 1 result (IS NOT NULL filter), got %d: %+v", len(hits), hits)
	}
	if hits[0].SymbolName != "WithSparse" {
		t.Errorf("expected WithSparse, got %s", hits[0].SymbolName)
	}
}

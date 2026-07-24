package ranking

import (
	"math"
	"testing"
)

func TestBM25F_EmptyCorpus(t *testing.T) {
	t.Parallel()
	scorer := NewBM25F(nil)

	doc := Document{Path: "main.go", Symbols: []string{"main"}}
	score := scorer.Score("main", doc)

	if score != 0 {
		t.Errorf("expected score 0 for empty corpus, got %f", score)
	}

	score = scorer.ScoreTerms([]string{"main", "handler"}, doc)
	if score != 0 {
		t.Errorf("expected ScoreTerms 0 for empty corpus, got %f", score)
	}
}

func TestBM25F_SingleDocument(t *testing.T) {
	t.Parallel()
	docs := []Document{
		{Path: "handler.go", Symbols: []string{"HandleRequest", "ServeHTTP"}},
	}
	scorer := NewBM25F(docs)

	score := scorer.Score("handle", docs[0])
	if score <= 0 {
		t.Errorf("expected positive score for matching term, got %f", score)
	}

	// Non-matching term should return 0.
	score = scorer.Score("database", docs[0])
	if score != 0 {
		t.Errorf("expected 0 for non-matching term, got %f", score)
	}
}

func TestBM25F_SymbolWeightHigherThanPath(t *testing.T) {
	t.Parallel()
	docs := []Document{
		{
			Path:    "file_a.go",
			Symbols: []string{"AuthHandler"},
		},
		{
			Path:    "auth/file_b.go",
			Symbols: []string{"main"},
		},
	}
	scorer := NewBM25F(docs)

	symbolScore := scorer.ScoreTerms([]string{"auth"}, docs[0])
	pathScore := scorer.ScoreTerms([]string{"auth"}, docs[1])

	if symbolScore <= pathScore {
		t.Errorf("symbol match (%f) should score higher than path-only match (%f)", symbolScore, pathScore)
	}
}

func TestBM25F_PathMatchWeighted(t *testing.T) {
	t.Parallel()
	docs := []Document{
		{
			Path:    "auth/handler.go",
			Symbols: []string{"main"},
		},
		{
			Path:    "utils/helper.go",
			Symbols: []string{"main"},
		},
	}
	scorer := NewBM25F(docs)

	pathScore := scorer.ScoreTerms([]string{"auth"}, docs[0])
	noMatchScore := scorer.ScoreTerms([]string{"auth"}, docs[1])

	if pathScore <= noMatchScore {
		t.Errorf("path match (%f) should score higher than no match (%f)", pathScore, noMatchScore)
	}
}

func TestBM25F_MultipleTerms(t *testing.T) {
	t.Parallel()
	docs := []Document{
		{
			Path:    "auth_handler.go",
			Symbols: []string{"AuthHandler", "ValidateToken"},
		},
		{
			Path:    "auth_only.go",
			Symbols: []string{"AuthHandler"},
		},
	}

	scorer := NewBM25F(docs)

	bothTerms := scorer.ScoreTerms([]string{"auth", "token"}, docs[0])
	singleTerm := scorer.ScoreTerms([]string{"auth"}, docs[0])

	if bothTerms <= singleTerm {
		t.Errorf("matching both terms (%f) should score higher than single term (%f)", bothTerms, singleTerm)
	}
}

func TestBM25F_IDF_CommonTermLowerScore(t *testing.T) {
	t.Parallel()
	// "main" appears in all 3 docs (common), "auth" appears in only 1 (rare).
	docs := []Document{
		{
			Path:    "handler.go",
			Symbols: []string{"main", "AuthMiddleware"},
		},
		{
			Path:    "server.go",
			Symbols: []string{"main"},
		},
		{
			Path:    "config.go",
			Symbols: []string{"main"},
		},
	}
	scorer := NewBM25F(docs)

	// For the first doc, "auth" (rare) should score higher than "main" (common).
	authScore := scorer.Score("auth", docs[0])
	mainScore := scorer.Score("main", docs[0])

	if authScore <= mainScore {
		t.Errorf("rare term 'auth' (%f) should score higher than common term 'main' (%f)", authScore, mainScore)
	}
}

func TestBM25FMatchesDocComment(t *testing.T) {
	t.Parallel()
	docs := []Document{
		{Path: "a.go", Symbols: []string{"Foo"}, Docs: []string{"retries the request with exponential backoff"}},
		{Path: "b.go", Symbols: []string{"Bar"}, Docs: []string{"unrelated helper"}},
	}
	scorer := NewBM25F(docs)

	a := scorer.ScoreTerms([]string{"retry", "backoff"}, docs[0])
	b := scorer.ScoreTerms([]string{"retry", "backoff"}, docs[1])

	if a <= b {
		t.Errorf("doc-comment match must score higher: a=%f b=%f", a, b)
	}
}

// TestBM25F_SamePathCandidatesScoredAgainstOwnDocument is the B1 regression
// test. It reproduces the per-symbol candidate shape from
// internal/embeddings/store_bm25.go (BM25Search): multiple Documents sharing
// the SAME Path, each carrying distinct symbol tokens.
//
// Before the fix, ScoreTerms resolved the document via findDocIndex(path) — a
// linear scan returning the FIRST match. With identical paths, every candidate
// resolved to index 0 (Config's doc), so SparseConfig was scored against
// Config's tokens — losing its rightful symbol-match boost. All same-path
// candidates received Config's score (a tie), not their own.
//
// After the fix, ScoreTerms computes TF directly from the passed doc's own
// fields (corpus-wide IDF/avgdl only), so each candidate is scored against its
// own tokens. SparseConfig (symbol hit, WeightSymbol=5) must outrank Config
// (doc-comment hit, WeightDoc=2); RRFWeights (no "sparse" token) must score 0.
//
// Falsification: revert the fix → findDocIndex returns 0 for all three → all
// scored against Config's doc → scoreSparse == scoreConfig (tie) → the
// `scoreSparse > scoreConfig` assertion goes RED.
func TestBM25F_SamePathCandidatesScoredAgainstOwnDocument(t *testing.T) {
	t.Parallel()
	// Three candidates from the SAME file (config.go), distinct symbol tokens.
	// "sparse" appears in Config's Docs (WeightDoc=2) and SparseConfig's symbol
	// (WeightSymbol=5), but NOT in RRFWeights at all.
	docs := []Document{
		{Path: "config.go", Symbols: []string{"Config"}, Docs: []string{"sparse config helper"}},
		{Path: "config.go", Symbols: []string{"RRFWeights"}},
		{Path: "config.go", Symbols: []string{"SparseConfig"}},
	}
	scorer := NewBM25F(docs)

	scoreConfig := scorer.ScoreTerms([]string{"sparse"}, docs[0])
	scoreRRF := scorer.ScoreTerms([]string{"sparse"}, docs[1])
	scoreSparse := scorer.ScoreTerms([]string{"sparse"}, docs[2])

	// SparseConfig's symbol contains "sparse" (×5) — must outrank Config's
	// doc-comment-only match (×2). With the bug, both equal Config's score.
	if scoreSparse <= scoreConfig {
		t.Errorf("same-path: SparseConfig (symbol hit) must outrank Config "+
			"(doc-only hit): sparse=%f config=%f rrf=%f",
			scoreSparse, scoreConfig, scoreRRF)
	}
	// RRFWeights has no "sparse" token in any field — must score exactly 0.
	// With the bug, it inherits Config's doc-comment score (>0) — RED.
	if scoreRRF != 0 {
		t.Errorf("same-path: RRFWeights has no 'sparse' token, must score 0, "+
			"got %f (bug: scored against Config's doc)", scoreRRF)
	}
	// Config's own score must be positive (doc-comment match).
	if scoreConfig <= 0 {
		t.Errorf("same-path: Config doc-comment match must score >0, got %f",
			scoreConfig)
	}
}

// TestBM25F_UniquePathRankingUnchanged locks the repo_analyze call-site
// behavior (internal/analyze/rank.go: each Document has a UNIQUE path, one doc
// per file). The fix computes TF from the passed doc's own fields instead of a
// path lookup; for unique-path docs the passed doc IS the corpus doc, so TF,
// DL, and the final score are identical to the pre-fix path-lookup path within
// floating-point tolerance (a different accumulation order shifts the last few
// ULPs but never the ranking). This test asserts golden scores within tolerance
// so any real drift in the scoring math is caught.
func TestBM25F_UniquePathRankingUnchanged(t *testing.T) {
	t.Parallel()
	docs := []Document{
		{Path: "handler.go", Symbols: []string{"HandleRequest"}, Docs: []string{"handles http requests"}},
		{Path: "server.go", Symbols: []string{"Serve"}},
		{Path: "config.go", Symbols: []string{"LoadConfig"}, Docs: []string{"loads sparse config"}},
	}
	scorer := NewBM25F(docs)

	// Golden scores captured from the pre-fix path-lookup implementation
	// (findDocIndex resolved correctly for unique paths → identical to the
	// post-fix per-doc-TF path). Any change to the scoring math breaks these.
	tests := []struct {
		name     string
		terms    []string
		doc      Document
		expected float64
	}{
		{"handler symbol+doc match", []string{"handle"}, docs[0], 1.9156335442461112},
		{"server no match", []string{"handle"}, docs[1], 0},
		{"config sparse doc match", []string{"sparse"}, docs[2], 1.3220805686109927},
	}
	for _, tc := range tests {
		got := scorer.ScoreTerms(tc.terms, tc.doc)
		// Tolerance, not exact equality: the fix computes the same value via a
		// different accumulation order (per-doc TF inline vs the old docDL
		// array), which can differ by a few ULPs (~1e-15). That is far below
		// any score gap that affects ranking, so a tight tolerance both locks
		// the scoring math and tolerates float re-association.
		if math.Abs(got-tc.expected) > 1e-9 {
			t.Errorf("%s: expected %.16f, got %.16f (delta %g)", tc.name, tc.expected, got, got-tc.expected)
		}
	}
}

func TestBM25FDocWeightLowerThanSymbol(t *testing.T) {
	t.Parallel()
	// A symbol-name match should out-rank a doc-comment-only match
	// because symbols carry stronger signal (WeightSymbol=5.0 > WeightDoc=2.0).
	docs := []Document{
		{Path: "a.go", Symbols: []string{"Retry"}, Docs: []string{"unrelated"}},
		{Path: "b.go", Symbols: []string{"Helper"}, Docs: []string{"retry logic here"}},
	}
	scorer := NewBM25F(docs)

	a := scorer.ScoreTerms([]string{"retry"}, docs[0])
	b := scorer.ScoreTerms([]string{"retry"}, docs[1])

	if a <= b {
		t.Errorf("symbol match (a=%f) must out-rank doc-only match (b=%f)", a, b)
	}
}

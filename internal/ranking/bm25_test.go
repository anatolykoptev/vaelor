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

// TestBM25F_PathAndSymbolEqualWeight locks the Zoekt equal-boost field-weight
// change (WeightPath 3 → 5, == WeightSymbol). With equal field weights, a doc
// whose only "auth" hit is in the Path field and a doc whose only "auth" hit is
// in the Symbols field — both with the same token count and thus the same
// weighted document length — score identically.
//
// Falsification: revert WeightPath to 3.0 → pathScore's TF is 3, symbolScore's
// TF is 5 → pathScore < symbolScore → the equality assertion goes RED.
func TestBM25F_PathAndSymbolEqualWeight(t *testing.T) {
	t.Parallel()
	docs := []Document{
		{
			Path:    "file_a.go",
			Symbols: []string{"Auth"},
		},
		{
			Path:    "auth/file_b.go",
			Symbols: []string{"main"},
		},
	}
	scorer := NewBM25F(docs)

	symbolScore := scorer.ScoreTerms([]string{"auth"}, docs[0])
	pathScore := scorer.ScoreTerms([]string{"auth"}, docs[1])

	// Both docs: one "auth" token (×5), one single-token symbol entry
	// (dl = 1×5 + WeightPath = 10), df=2, N=2 → identical IDF, TF, and DL →
	// identical scores.
	if math.Abs(symbolScore-pathScore) > 1e-9 {
		t.Errorf("equal field weights: symbol match (%f) should equal path-only match (%f)", symbolScore, pathScore)
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
// per file). The per-doc-TF fix computes TF from the passed doc's own fields
// instead of a path lookup; for unique-path docs the passed doc IS the corpus
// doc. The golden scores below reflect the tokenized BM25F math (exact-token TF
// via lextoken.SplitIdentifier + compact compound) with Zoekt equal field
// weights (WeightPath = WeightSymbol = 5). Any drift in the scoring math breaks
// these.
func TestBM25F_UniquePathRankingUnchanged(t *testing.T) {
	t.Parallel()
	docs := []Document{
		{Path: "handler.go", Symbols: []string{"HandleRequest"}, Docs: []string{"handles http requests"}},
		{Path: "server.go", Symbols: []string{"Serve"}},
		{Path: "config.go", Symbols: []string{"LoadConfig"}, Docs: []string{"loads sparse config"}},
	}
	scorer := NewBM25F(docs)

	// Golden scores captured from the tokenized BM25F implementation
	// (exact-token TF, WeightPath=5). Any change to the scoring math breaks these.
	tests := []struct {
		name     string
		terms    []string
		doc      Document
		expected float64
	}{
		{"handler symbol+doc match", []string{"handle"}, docs[0], 1.6858002786139050},
		{"server no match", []string{"handle"}, docs[1], 0},
		{"config sparse doc match", []string{"sparse"}, docs[2], 1.2693084450739991},
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

// --- Phase E: tokenized BM25F term matching (issue #661) ---

// TestBM25F_TokenizedCrossConventionMatch verifies that a query "parse_config"
// matches a symbol "parseConfig" and vice versa, via the shared subword
// (parse, config) and compact (parseconfig) tokens produced by symmetric
// index/query tokenization.
//
// Falsification: revert to substring matching (strings.Contains on lowercased
// raw fields without tokenization). "parse_config" lowercased is
// "parse_config"; strings.Contains("parseconfig", "parse_config") is FALSE
// (the underscore is not in "parseconfig"), so the cross-convention match is
// lost and the score drops to 0 → RED.
func TestBM25F_TokenizedCrossConventionMatch(t *testing.T) {
	t.Parallel()
	docs := []Document{
		{Path: "logic.go", Symbols: []string{"parseConfig"}},
	}
	scorer := NewBM25F(docs)

	snakeQuery := scorer.ScoreTerms([]string{"parse_config"}, docs[0])
	camelQuery := scorer.ScoreTerms([]string{"parseConfig"}, docs[0])

	if snakeQuery <= 0 {
		t.Errorf("tokenized: query \"parse_config\" must match symbol \"parseConfig\", got %f", snakeQuery)
	}
	if camelQuery <= 0 {
		t.Errorf("tokenized: query \"parseConfig\" must match symbol \"parseConfig\", got %f", camelQuery)
	}
	// Both queries produce the same token set {parse, config, parseconfig} →
	// identical scores (symmetric tokenization).
	if math.Abs(snakeQuery-camelQuery) > 1e-9 {
		t.Errorf("tokenized: snake and camel queries should score equally, got snake=%f camel=%f", snakeQuery, camelQuery)
	}
}

// TestBM25F_SubstringFalseMatchNoLongerInflatesTF verifies that a substring-only
// false match the OLD code accepted — query "serial" matching "Deserializer"
// mid-word (strings.Contains("deserializer", "serial") == true) — no longer
// inflates TF under exact-token matching. "serial" is not a token of
// "Deserializer" (which tokenizes to {deserializer}).
//
// Falsification: revert to substring matching → strings.Contains("deserializer",
// "serial") is TRUE → TF > 0 → score > 0 → the "score == 0" assertion goes RED.
func TestBM25F_SubstringFalseMatchNoLongerInflatesTF(t *testing.T) {
	t.Parallel()
	docs := []Document{
		{Path: "serde.go", Symbols: []string{"Deserializer"}},
	}
	scorer := NewBM25F(docs)

	score := scorer.ScoreTerms([]string{"serial"}, docs[0])
	if score != 0 {
		t.Errorf("tokenized: \"serial\" is not a token of \"Deserializer\", must score 0, got %f", score)
	}
}

// TestBM25F_ExactTokenRanksTraitAboveMethod is the core Rust relevance fix from
// issue #661. Given docs with symbols "Deserialize" (the trait) and
// "deserialize_u32" (a method), a query "Deserialize" must score the exact trait
// strictly higher. Under exact-token matching both contain the token
// "deserialize" (TF equal), but the trait doc is shorter (fewer tokens → smaller
// weighted DL → less length-normalization penalty), so it wins.
//
// Falsification: revert to substring matching → "deserialize" is a substring of
// BOTH "deserialize" and "deserialize_u32"; the method's longer name still
// contains "deserialize" so both match, but substring TF counts the method's
// swarm of deserialize_* siblings equally, collapsing the trait's advantage →
// the strict "trait > method" assertion goes RED (tie or reversed).
func TestBM25F_ExactTokenRanksTraitAboveMethod(t *testing.T) {
	t.Parallel()
	docs := []Document{
		{Path: "trait.go", Symbols: []string{"Deserialize"}},
		{Path: "impl.go", Symbols: []string{"deserialize_u32"}},
	}
	scorer := NewBM25F(docs)

	trait := scorer.ScoreTerms([]string{"Deserialize"}, docs[0])
	method := scorer.ScoreTerms([]string{"Deserialize"}, docs[1])

	if trait <= method {
		t.Errorf("exact-token: trait Deserialize (%f) must rank strictly above method deserialize_u32 (%f)", trait, method)
	}
}

// TestBM25F_CompactCompoundFallback verifies that a cross-convention compact
// query "getusername" still matches a symbol "get_user_name" via the compact
// compound token "getusername" indexed alongside the subwords.
//
// Falsification: remove the compact-compound emission (emitCompact=false for
// symbols) → "get_user_name" tokenizes to {get, user, name} only; query
// "getusername" tokenizes to {getusername}; no shared token → score 0 → RED.
func TestBM25F_CompactCompoundFallback(t *testing.T) {
	t.Parallel()
	docs := []Document{
		{Path: "user.go", Symbols: []string{"get_user_name"}},
	}
	scorer := NewBM25F(docs)

	score := scorer.ScoreTerms([]string{"getusername"}, docs[0])
	if score <= 0 {
		t.Errorf("compact compound: query \"getusername\" must match symbol \"get_user_name\", got %f", score)
	}
}

// TestBM25F_WholeIdentifierEmission verifies that an exact whole-identifier
// query matches the whole-identifier token emitted alongside subwords. The
// symbol "Deserialize" emits the whole token "deserialize" (== its compact form
// for a single camelCase word); a query "deserialize" matches it exactly.
//
// Falsification: drop the whole-word token emission (only emit subwords) →
// "Deserialize" → SplitIdentifier → ["deserializer"]? No: SplitCamelCase of
// "Deserialize" yields ["deserialize"]; the whole token IS that subword here.
// To make this test independently falsifiable, use a symbol whose subword split
// differs from the whole identifier: "XMLParser" → subwords {xml, parser},
// whole {xmlparser}. A query "xmlparser" matches only via the whole token.
// Revert whole-token emission → "xmlparser" not in {xml, parser} → score 0 → RED.
func TestBM25F_WholeIdentifierEmission(t *testing.T) {
	t.Parallel()
	docs := []Document{
		{Path: "xml.go", Symbols: []string{"XMLParser"}},
	}
	scorer := NewBM25F(docs)

	score := scorer.ScoreTerms([]string{"xmlparser"}, docs[0])
	if score <= 0 {
		t.Errorf("whole-identifier: query \"xmlparser\" must match symbol \"XMLParser\" via whole token, got %f", score)
	}
}

// TestBM25F_LowPriorityFilePenalty verifies that a match in a test file has its
// TF divided by lowPriorityFilePenalty (5), so an equal symbol match in a
// non-test file scores strictly higher than the same match in a *_test.go file.
//
// Falsification: remove the lowPriorityFilePenalty division in
// computeWeightedTF → both docs have identical TF (5), identical DL, identical
// IDF → equal scores → the strict "normal > test" assertion goes RED.
func TestBM25F_LowPriorityFilePenalty(t *testing.T) {
	t.Parallel()
	docs := []Document{
		{Path: "handler.go", Symbols: []string{"Auth"}},
		{Path: "handler_test.go", Symbols: []string{"Auth"}},
	}
	scorer := NewBM25F(docs)

	normal := scorer.ScoreTerms([]string{"auth"}, docs[0])
	testFile := scorer.ScoreTerms([]string{"auth"}, docs[1])

	if normal <= testFile {
		t.Errorf("lowPriorityFilePenalty: normal-file match (%f) must outrank test-file match (%f)", normal, testFile)
	}
	// The test-file TF is divided by 5; with identical IDF/DL structure the
	// test-file score must be strictly lower, not equal.
	if testFile <= 0 {
		t.Errorf("lowPriorityFilePenalty: test-file match must still score >0 (TF divided, not zeroed), got %f", testFile)
	}
}

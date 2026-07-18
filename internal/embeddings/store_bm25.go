package embeddings

import (
	"context"
	"log/slog"
	"sort"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/anatolykoptev/vaelor/internal/lextoken"
	"github.com/anatolykoptev/vaelor/internal/ranking"
)

// BM25F arm metrics. Pre-touched at 0 so /metrics always exposes all label
// values regardless of whether any query has been issued (metrics-first rule).
//
//   - gocode_bm25_query_duration_seconds — arm latency histogram (seconds).
//   - gocode_bm25_candidates_scored      — candidate-set size per query.
//   - gocode_bm25_query_failures_total{stage} — failures by stage:
//     "fetch"  — trigram/ILIKE candidate SQL failed
//     "score"  — internal scoring error (should never fire; defensive)
//   - gocode_bm25_empty_query_total — empty-term guard (no DB I/O issued).
var (
	bm25QueryDuration = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "gocode_bm25_query_duration_seconds",
			Help:    "BM25F lexical arm end-to-end latency (seconds), including candidate fetch and scoring.",
			Buckets: []float64{0.001, 0.005, 0.010, 0.025, 0.050, 0.100, 0.250},
		},
	)
	bm25CandidatesScored = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "gocode_bm25_candidates_scored",
			Help:    "Number of candidate symbols scored per BM25F query (trigram prefilter set size).",
			Buckets: []float64{0, 5, 10, 25, 50, 100, 200, 500},
		},
	)
	bm25QueryFailTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "gocode_bm25_query_failures_total",
			Help: "Total BM25F lexical-arm failures by stage (fetch, score).",
		},
		[]string{"stage"},
	)
	bm25EmptyQueryTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "gocode_bm25_empty_query_total",
			Help: "Total BM25F queries short-circuited due to empty term set (no DB I/O).",
		},
	)
)

func init() {
	// Pre-touch label values.
	bm25QueryFailTotal.WithLabelValues("fetch").Add(0)
	bm25QueryFailTotal.WithLabelValues("score").Add(0)
}

// BM25Search retrieves the top-K symbols most relevant to queryText via BM25F
// scoring over a trigram-prefiltered candidate set.
//
// Protocol:
//  1. Tokenize queryText (lextoken.Tokenize — identifier-aware, no stopword filter).
//     Empty result → return (nil, nil) with no DB I/O (mirrors SearchSparse gate).
//  2. Fetch candidates: reuse SearchBySymbolName (trigram similarity SQL, ILIKE
//     fallback) to pull candidate rows whose symbol_name/file_path match the terms.
//     Candidate set is bounded by the existing SQL LIMIT (not the full table).
//  3. Build []ranking.Document from candidates:
//     Path    = file_path  (BM25F Path field, ×3 weight)
//     Symbols = [symbol_name] + lextoken.SplitIdentifier(symbol_name) (×5 weight)
//     Docs    = nil  (no body column in code_embeddings; see plan §strategy-a)
//  4. Score each candidate via ranking.NewBM25F + ScoreTerms; sort DESC; take topK.
//  5. Map to []KeywordHit{FilePath, SymbolName, Line} — the Keyword-slot contract
//     consumed by MergeRRF (rrf.go:81).
//
// Corpus note: BM25F's IDF and avgDL are computed over the candidate set, not the
// global corpus (local-IDF approximation — standard for candidate-reranking pipelines
// fused via RRF). See plan §strategy-a for the trade-off rationale.
//
// Gating: empty terms or candidate-fetch failure → (nil, nil) non-fatal; caller
// falls back to grep (Phase 4 wiring). DB errors bump gocode_bm25_query_failures_total.
func (s *Store) BM25Search(ctx context.Context, repoKey, queryText, language string, topK int) ([]KeywordHit, error) {
	timer := prometheus.NewTimer(bm25QueryDuration)
	defer timer.ObserveDuration()

	// Step 1: tokenize — identifier-aware (CamelCase + snake_case splitting).
	terms := lextoken.Tokenize(queryText)
	if len(terms) == 0 {
		bm25EmptyQueryTotal.Inc()
		return nil, nil
	}

	// Step 2: fetch candidates via the existing trigram prefilter.
	// SearchBySymbolName issues the pg_trgm similarity SQL with ILIKE fallback;
	// it is already on the hot path and bounded by the SQL LIMIT.
	limit := topK * candidateFanout
	if limit < minCandidateLimit {
		limit = minCandidateLimit
	}
	candidates, err := s.SearchBySymbolName(ctx, repoKey, terms, language, limit)
	if err != nil {
		bm25QueryFailTotal.WithLabelValues("fetch").Inc()
		slog.Warn("bm25 search: candidate fetch failed", slog.String("repo", repoKey), slog.Any("error", err))
		return nil, nil //nolint:nilerr // non-fatal: P4 caller falls back to grep
	}
	if len(candidates) == 0 {
		bm25CandidatesScored.Observe(0)
		return nil, nil
	}

	bm25CandidatesScored.Observe(float64(len(candidates)))

	// Step 3: build ranking.Document corpus from candidates.
	// Symbol field carries both the raw symbol_name AND its identifier-split
	// subwords so that a query "parseConfig" matches "parse_config" and vice versa.
	// Doc field is nil — code_embeddings has no body column (body_hash BIGINT only).
	docs := make([]ranking.Document, len(candidates))
	for i, c := range candidates {
		subwords := lextoken.SplitIdentifier(c.SymbolName)
		symbols := make([]string, 0, 1+len(subwords))
		symbols = append(symbols, c.SymbolName)
		symbols = append(symbols, subwords...)
		docs[i] = ranking.Document{
			Path:    c.FilePath,
			Symbols: symbols,
			Docs:    nil,
		}
	}

	// Step 4: BM25F score each candidate, sort DESC.
	scorer := ranking.NewBM25F(docs)
	type scored struct {
		hit   KeywordHit
		score float64
	}
	scoreds := make([]scored, len(candidates))
	for i, c := range candidates {
		sc := scorer.ScoreTerms(terms, docs[i])
		scoreds[i] = scored{
			hit: KeywordHit{
				FilePath:   c.FilePath,
				SymbolName: c.SymbolName,
				Line:       c.StartLine,
			},
			score: sc,
		}
	}
	sort.Slice(scoreds, func(i, j int) bool {
		return scoreds[i].score > scoreds[j].score
	})

	// Step 5: take topK, map to []KeywordHit.
	n := len(scoreds)
	if topK > 0 && n > topK {
		n = topK
	}
	hits := make([]KeywordHit, n)
	for i := range n {
		hits[i] = scoreds[i].hit
	}
	return hits, nil
}

// candidateFanout controls how many candidates we fetch per topK result.
// Fetching topK*10 candidates gives BM25F a sufficiently large reranking pool
// while keeping the trigram SQL result small (bounded by SQL LIMIT in SearchBySymbolName).
const candidateFanout = 10

// minCandidateLimit is the floor on the SQL LIMIT passed to SearchBySymbolName
// so single-result queries still get a meaningful candidate pool.
const minCandidateLimit = 50

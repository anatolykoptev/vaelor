package main

import (
	"context"
	"log/slog"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/anatolykoptev/vaelor/internal/codesearch"
	"github.com/anatolykoptev/vaelor/internal/embeddings"
	"github.com/anatolykoptev/vaelor/internal/oxcodes"
)

// Keyword-arm observability (BM25F P4).
//
// gocode_keyword_arm_active{arm} — gauge published at startup by publishKeywordArm.
// gocode_keyword_arm_total{arm}  — counter incremented per query by the arm that served it.
//
// arm label values:
//   - "grep"           — scoped or full-file grep served the keyword slot.
//   - "bm25f"          — BM25F served the keyword slot.
//   - "bm25f_fallback" — BM25F was requested but errored/empty; grep served instead.
var (
	keywordArmActive = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "gocode_keyword_arm_active",
			Help: "Active keyword retrieval arm (1 = active arm). Published at startup.",
		},
		[]string{"arm"},
	)
	keywordArmTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "gocode_keyword_arm_total",
			Help: "Total semantic_search queries served by keyword arm (grep|bm25f|bm25f_fallback).",
		},
		[]string{"arm"},
	)
)

func init() {
	// Pre-touch all label values so /metrics always exposes every arm counter.
	keywordArmTotal.WithLabelValues("grep").Add(0)
	keywordArmTotal.WithLabelValues("bm25f").Add(0)
	keywordArmTotal.WithLabelValues("bm25f_fallback").Add(0)
	keywordArmActive.WithLabelValues("grep").Set(0)
	keywordArmActive.WithLabelValues("bm25f").Set(0)
	keywordArmActive.WithLabelValues("bm25f_fallback").Set(0)
}

// publishKeywordArm sets the gocode_keyword_arm_active gauge at startup so ops
// can see which arm is live without issuing a query. Mirrors PublishRRFWeights.
func publishKeywordArm(arm string) {
	keywordArmActive.WithLabelValues("grep").Set(0)
	keywordArmActive.WithLabelValues("bm25f").Set(0)
	keywordArmActive.WithLabelValues("bm25f_fallback").Set(0)
	keywordArmActive.WithLabelValues(arm).Set(1)
}

// runKeywordArm runs the flag-selected keyword retriever for the Keyword slot of
// MergeRRF. It returns either:
//
//   - (fileHits, nil) when the grep arm runs — fileHits must be resolved to
//     []KeywordHit via Store.MatchKeywordHits by the caller.
//   - (nil, kwHits)   when the bm25f arm runs (or falls back) — kwHits are
//     already []KeywordHit and bypass MatchKeywordHits.
//
// The caller (handleSemanticHits) MUST check which return is non-nil and wire
// accordingly into MergeRRF.
//
// Arm selection (deps.KeywordArm):
//
//	"grep"  (default) → scoped grep (ox-codes) with full-file grep fallback.
//	                     Byte-identical to pre-P4 behavior (dark-launch invariant).
//	"bm25f"           → BM25F over trigram-prefiltered candidates.
//	                     On error or empty result → falls back to grep, bumps
//	                     gocode_keyword_arm_total{arm="bm25f_fallback"}.
func runKeywordArm(
	ctx context.Context,
	deps SemanticDeps,
	query, repoKey, root, language string,
	topK int,
) (fileHits []embeddings.FileLineHit, kwHits []embeddings.KeywordHit) {
	// bm25searcher: test seam wins over Store (allows spy injection without live DB).
	var bm25 bm25Searcher
	if deps.bm25searcher != nil {
		bm25 = deps.bm25searcher
	} else if deps.Store != nil {
		bm25 = deps.Store
	}

	if deps.KeywordArm == keywordArmBM25F && bm25 != nil {
		hits, err := bm25.BM25Search(ctx, repoKey, query, language, topK)
		if err != nil {
			// BM25Search already logged + counted at the store level; here we
			// bump the arm-level fallback counter and warn for ops visibility.
			slog.Warn("keyword arm: bm25f failed, falling back to grep",
				slog.String("repo", repoKey),
				slog.Any("error", err),
			)
		}
		if len(hits) > 0 {
			keywordArmTotal.WithLabelValues("bm25f").Inc()
			return nil, hits
		}
		// Empty result (nil,nil from BM25Search) or error — fall through to grep.
		keywordArmTotal.WithLabelValues("bm25f_fallback").Inc()
	}

	// Grep arm (default, or bm25f fallback).
	var grepHits []embeddings.FileLineHit
	if scopedHits := runScopedKeywordSearch(ctx, deps.OxCodes, query, root, language); len(scopedHits) > 0 {
		grepHits = scopedHits
	} else {
		grepHits = runKeywordSearch(ctx, query, root)
	}
	keywordArmTotal.WithLabelValues("grep").Inc()
	return grepHits, nil
}

// runKeywordSearch runs a case-insensitive literal search for the query in the repo.
func runKeywordSearch(ctx context.Context, query, root string) []embeddings.FileLineHit {
	matches, err := codesearch.Search(ctx, codesearch.SearchInput{
		Root:          root,
		Pattern:       query,
		IsRegex:       false,
		CaseSensitive: false,
		MaxResults:    50,
		ContextLines:  0,
	})
	if err != nil || len(matches) == 0 {
		return nil
	}
	hits := make([]embeddings.FileLineHit, len(matches))
	for i, m := range matches {
		hits[i] = embeddings.FileLineHit{FilePath: m.File, Line: m.Line}
	}
	return hits
}

// runScopedKeywordSearch finds keyword matches inside function bodies via ox-codes.
// More precise than full-file grep — avoids imports, comments, strings.
// Returns nil when ox-codes unavailable (caller falls back to runKeywordSearch).
func runScopedKeywordSearch(ctx context.Context, client *oxcodes.Client, query, root, language string) []embeddings.FileLineHit {
	if client == nil || language == "" {
		return nil
	}
	kws := embeddings.ExtractQueryKeywords(query)
	if len(kws) == 0 {
		return nil
	}
	pattern := strings.Join(kws, "|")
	isRegex := len(kws) > 1
	resp, err := client.SearchScoped(ctx, oxcodes.ScopedSearchInput{
		Root:       root,
		Pattern:    pattern,
		Scope:      "function_bodies",
		Language:   language,
		IsRegex:    isRegex,
		MaxResults: 30,
	})
	if err != nil || resp == nil {
		return nil
	}
	hits := make([]embeddings.FileLineHit, 0, len(resp.Matches))
	for _, m := range resp.Matches {
		hits = append(hits, embeddings.FileLineHit{FilePath: m.File, Line: m.Line})
	}
	return hits
}

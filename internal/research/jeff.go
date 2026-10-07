package research

import (
	"context"
	"log/slog"
	"sort"
	"time"
)

// Jeff (gliformer) topicality arbitration for seed files — the fix for
// hub domination (issue #834). Structural signals (PageRank boost, import
// degree) are query-agnostic: config.go / cache/redis.go accumulate
// centrality and top every query because their content contains most
// keywords. BM25F and embeddings cannot see "this file is generic
// infrastructure" — keyword overlap with an infra file IS real. Jeff
// judges the file's ROLE against the query: implementation of the topic
// vs shared wiring — and flagged files lose an explicit penalty that
// erases the structural boost, instead of silently topping every answer.

const (
	// jeffTopFiles bounds the noul question count in one Ask — latency
	// grows ~linearly with label count (measured 3.58s at 15 files
	// edge→kisol, ≈0.24s/file). 10 keeps the call ≈2.4s typical with
	// headroom inside jeffCallTimeout when the shared model box is busy.
	jeffTopFiles = 10
	// jeffMinSeeds skips arbitration when the seed set is too small to
	// hide a hub inside.
	jeffMinSeeds = 4
	// jeffInfraPenalty subtracts just above the max PageRank file boost
	// (+0.3 in step 3.6): a file whose score was purely structural drops
	// out of seeds entirely, while a topically-scored hub keeps a
	// residual rank instead of disappearing.
	jeffInfraPenalty = 0.35
	// jeffTopicalThreshold is the noul boundary: below 0.5 the scorer
	// judges the file generic infrastructure for this query.
	jeffTopicalThreshold = 0.5
	// jeffCallTimeout bounds the single batched Ask — 10 files take ≈2.4s
	// typical; 5s leaves margin for a busy kisol and matches the client's
	// transport headroom.
	jeffCallTimeout = 5 * time.Second
)

// JeffScorer is the minimal interface the research pipeline needs from a
// jeff System One service. Implementations wrap go-kit/jeff's batched Ask.
// Satisfied by the cmd/vaelor adapter; nil disables arbitration entirely
// (cold path stays byte-identical).
type JeffScorer interface {
	// ScoreTopical returns, per file path, the probability that the file
	// implements functionality relevant to query rather than shared
	// infrastructure (config, caches, wiring, utils). Missing keys mean
	// "unscored" and are treated as topical.
	ScoreTopical(ctx context.Context, query string, files []string) (map[string]float64, error)
}

// applyJeffArbitration penalizes seed scores for files jeff judges as
// shared infrastructure rather than topical implementations of the query.
// Runs after all score sources (BM25F + semantic RRF + trgm + PageRank
// boost) and before the seed set/prune so the demotion propagates to both
// the seed ordering and the token-budget MMR pick. Best-effort: any scorer
// error leaves seedScores untouched.
func applyJeffArbitration(ctx context.Context, deps Deps, query string, seedScores map[string]float64) {
	if deps.JeffScorer == nil || len(seedScores) < jeffMinSeeds {
		return
	}

	// Only the files that could crowd the top need arbitration — sort by
	// score descending and take jeffTopFiles.
	top := topSeedFiles(seedScores, jeffTopFiles)

	jctx, cancel := context.WithTimeout(ctx, jeffCallTimeout)
	defer cancel()
	topical, err := deps.JeffScorer.ScoreTopical(jctx, query, top)
	if err != nil {
		slog.Warn("research: jeff arbitration failed — keeping structural order",
			slog.Any("error", err))
		return
	}

	demoted := 0
	for _, f := range top {
		p, ok := topical[f]
		if !ok {
			continue // unscored → keep score as-is
		}
		if p < jeffTopicalThreshold {
			seedScores[f] -= jeffInfraPenalty
			demoted++
		}
	}
	slog.Info("research: jeff arbitration",
		slog.Int("scored", len(topical)),
		slog.Int("demoted", demoted),
		slog.Any("topicality", topical))
}

// topSeedFiles returns the n highest-scored seed paths, score descending.
func topSeedFiles(seedScores map[string]float64, n int) []string {
	paths := make([]string, 0, len(seedScores))
	for p := range seedScores {
		paths = append(paths, p)
	}
	sort.Slice(paths, func(i, j int) bool {
		if seedScores[paths[i]] == seedScores[paths[j]] {
			return paths[i] < paths[j] // deterministic candidate set on ties
		}
		return seedScores[paths[i]] > seedScores[paths[j]]
	})
	if len(paths) > n {
		paths = paths[:n]
	}
	return paths
}

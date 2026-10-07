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
// answers a single choice question — "which of these files implements the
// query topic" — and the softmax forces separation where independent
// yes/no probabilities collapse (live probe: noul compressed to
// 0.4-0.5 with inversions; choice spread 0.44 topical vs 0.04 infra).
// Files jeff ranks below the uniform share lose a penalty proportional
// to the shortfall — erasing the structural boost, not the file.

const (
	// jeffTopFiles bounds the option count in the single choice question —
	// latency grows with option count (measured 0.78s at 10 options
	// edge→kisol). 10 keeps the call ~1s typical.
	jeffTopFiles = 10
	// jeffMinSeeds skips arbitration when the seed set is too small to
	// hide a hub inside.
	jeffMinSeeds = 4
	// jeffInfraPenalty scales the demotion for files jeff ranks below the
	// uniform share of the choice distribution. It tops out just above
	// the max PageRank file boost (+0.3 in step 3.6): a file whose score
	// was purely structural drops out of seeds entirely, while a
	// topically-scored hub keeps a residual rank instead of disappearing.
	jeffInfraPenalty = 0.35
	// jeffCallTimeout bounds the single batched Ask — a 10-option choice
	// takes ≈0.8s typical edge→kisol; 5s leaves margin for a busy shared
	// model box and matches the client's transport headroom.
	jeffCallTimeout = 5 * time.Second
)

// JeffScorer is the minimal interface the research pipeline needs from a
// jeff System One service. Implementations wrap go-kit/jeff's batched Ask
// with a single choice question — the returned per-file probabilities are
// shares of a softmax over the candidates (they sum to ~1), NOT
// independent topicality probabilities. Satisfied by the cmd/vaelor
// adapter; nil disables arbitration entirely (cold path stays
// byte-identical).
type JeffScorer interface {
	// ScoreTopical returns, per file path, the share of the choice
	// distribution — how strongly jeff ranks the file as the topical
	// implementation versus the other candidates. Missing keys mean
	// "unscored" and are treated as topical.
	ScoreTopical(ctx context.Context, query string, files []string) (map[string]float64, error)
}

// applyJeffArbitration penalizes seed scores for files jeff's choice
// distribution ranks below the uniform share — weaker-than-average picks
// for "which file implements the query", the hub signature. Runs after
// all score sources (BM25F + semantic RRF + trgm + PageRank boost) and
// before the seed set/prune so the demotion propagates to both the seed
// ordering and the token-budget MMR pick. Best-effort: any scorer error
// leaves seedScores untouched.
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

	// Choice probabilities are softmax shares over the candidate set, so
	// the uniform share 1/len(top) is the "jeff has no opinion" line:
	// files below it are ranked weaker-than-average at being topical for
	// THIS query — the infra signature. Penalty scales with the shortfall
	// (p→0 pays the full jeffInfraPenalty), so a mildly-below-uniform file
	// loses little while a clear hub loses its whole structural boost.
	// A compressed/no-signal answer (all shares ≈ uniform) demotes ~0 —
	// uncertainty must not reshuffle the structural order.
	uniform := 1.0 / float64(len(top))
	demoted := 0
	for _, f := range top {
		p, ok := topical[f]
		if !ok || p >= uniform {
			continue // unscored or at-or-above uniform → keep score as-is
		}
		penalty := jeffInfraPenalty * (uniform - p) / uniform
		seedScores[f] -= penalty
		demoted++
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

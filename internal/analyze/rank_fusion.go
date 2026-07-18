package analyze

import (
	"sort"
	"sync"

	"github.com/anatolykoptev/go-kit/rerank"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/anatolykoptev/vaelor/internal/ranking"
)

// FusionMode selects the multi-signal combination strategy used by
// prioritizeFilesWithScores.
type FusionMode string

const (
	// FusionModeMinmax is the legacy default: ranking.FusionRank with min-max
	// normalization + weighted sum. Preserved for byte-identical fallback while
	// the offline harness validates the rrf path.
	FusionModeMinmax FusionMode = "minmax"

	// FusionModeRRF routes the three signals through rerank.WeightedRRF
	// (Cormack-Clarke 2009 with per-list weights). Rank-based fusion is immune
	// to score-scale drift across corpora — see plan §Stream 3.
	FusionModeRRF FusionMode = "rrf"

	// rrfK matches the package-level k used by go-kit/rerank elsewhere.
	rrfK = 60
)

// FusionConfig is the runtime knob controlling rank.go's signal combiner.
// Default = byte-identical legacy minmax. The rrf mode is opt-in; the default
// flip is gated on offline-harness evidence (plan open question §2).
type FusionConfig struct {
	Mode           FusionMode
	WeightBM25     float64
	WeightPageRank float64
	WeightSeed     float64
}

// DefaultFusionConfig returns the byte-identical-fallback configuration.
// Weights are unused in minmax mode (the legacy ranking.FusionRank path uses
// its own const weights), but are kept here so callers see one struct shape
// regardless of mode.
func DefaultFusionConfig() FusionConfig {
	return FusionConfig{
		Mode: FusionModeMinmax,
		// Defaults align with plan §Stream 3 telemetry: pagerank centrality is
		// semantic-heavier than text relevance; exact-match seed boost is
		// auxiliary. Weights only matter when Mode == FusionModeRRF.
		WeightBM25:     1.0,
		WeightPageRank: 1.5,
		WeightSeed:     0.5,
	}
}

// fusionMu guards the package-level fusion config. Wired once at startup from
// cmd/go-code.loadConfig; tests swap it via SetFusionConfig.
var (
	fusionMu  sync.RWMutex
	fusionCfg = DefaultFusionConfig()
)

// SetFusionConfig overrides the package-level fusion configuration. Called
// once at startup; tests use it to exercise the rrf path.
func SetFusionConfig(cfg FusionConfig) {
	fusionMu.Lock()
	fusionCfg = cfg
	fusionMu.Unlock()
	publishFusionModeGauge(cfg.Mode)
}

// currentFusionConfig returns a snapshot of the active config under the read
// lock so request paths cannot race with SetFusionConfig.
func currentFusionConfig() FusionConfig {
	fusionMu.RLock()
	defer fusionMu.RUnlock()
	return fusionCfg
}

// gocode_analyze_fusion_mode is a startup gauge (1 for active mode, 0 for
// inactive). Operators grep /metrics to confirm the deployed mode matches
// config without rerunning loadConfig.
var fusionModeGauge = promauto.NewGaugeVec(
	prometheus.GaugeOpts{
		Name: "gocode_analyze_fusion_mode",
		Help: "Active fusion mode in prioritizeFilesWithScores (1=active, 0=inactive).",
	},
	[]string{"mode"},
)

func publishFusionModeGauge(active FusionMode) {
	for _, m := range []FusionMode{FusionModeMinmax, FusionModeRRF} {
		v := 0.0
		if m == active {
			v = 1
		}
		fusionModeGauge.WithLabelValues(string(m)).Set(v)
	}
}

// init publishes the default-mode gauge so /metrics has a value before the
// first SetFusionConfig call (services that never call SetFusionConfig fall
// through to legacy minmax behavior — the gauge must reflect that).
func init() {
	publishFusionModeGauge(fusionCfg.Mode)
}

// fuseSignals combines the three per-file score maps into a single fused
// score map according to cfg.Mode. minmax preserves byte-identical legacy
// behavior; rrf routes through rerank.WeightedRRF over rank-only lists.
func fuseSignals(
	cfg FusionConfig,
	bm25Scores, prScores, exactScores map[string]float64,
) map[string]float64 {
	if cfg.Mode == FusionModeRRF {
		bm25Ranked := rankByScore(bm25Scores)
		prRanked := rankByScore(prScores)
		exactRanked := rankByScore(exactScores)
		fused := rerank.WeightedRRF(rrfK,
			[]float64{cfg.WeightBM25, cfg.WeightPageRank, cfg.WeightSeed},
			bm25Ranked, prRanked, exactRanked,
		)
		out := make(map[string]float64, len(fused))
		for _, f := range fused {
			out[f.ID] = f.Score
		}
		return out
	}

	// Legacy path: ranking.FusionRank with the original const weights.
	// Coexists for one release per plan §Open question 5; do not delete.
	// SA1019 expected: this is the deprecated default-mode path on purpose.
	return ranking.FusionRank([]ranking.Signal{ //nolint:staticcheck // SA1019: legacy default-mode path
		{Name: "bm25", Weight: weightBM25, Scores: bm25Scores},
		{Name: "pagerank", Weight: weightPR, Scores: prScores},
		{Name: "exact", Weight: weightExact, Scores: exactScores},
	})
}

// rankByScore returns IDs sorted descending by score with a stable tie-break
// (lexicographic on ID) so RRF input is deterministic across runs.
func rankByScore(scores map[string]float64) []string {
	keys := make([]string, 0, len(scores))
	for k := range scores {
		keys = append(keys, k)
	}
	sort.SliceStable(keys, func(i, j int) bool {
		if scores[keys[i]] != scores[keys[j]] {
			return scores[keys[i]] > scores[keys[j]]
		}
		return keys[i] < keys[j]
	})
	return keys
}

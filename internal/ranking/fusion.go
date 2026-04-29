package ranking

import "math"

// Signal represents a single ranking signal with weight and per-file scores.
type Signal struct {
	Name   string
	Weight float64
	Scores map[string]float64
}

// FusionRank combines ranking signals into a single score per file.
// Each signal is min-max normalized to [0,1] before weighted combination.
//
// Deprecated: prefer rerank.WeightedRRF (rank-based fusion) over min-max +
// weighted sum. Min-max is outlier-sensitive and not scale-invariant across
// corpora — see plan 2026-04-29-go-code-retrieval-quality-lift.md §Stream 3.
// Kept as the default ANALYZE_RANK_FUSION_MODE=minmax path until the offline
// harness validates rrf with no per-repo regression >2%; remove next sprint
// after the default flip.
func FusionRank(signals []Signal) map[string]float64 {
	if len(signals) == 0 {
		return map[string]float64{}
	}
	files := make(map[string]struct{})
	for _, sig := range signals {
		for f := range sig.Scores {
			files[f] = struct{}{}
		}
	}
	result := make(map[string]float64, len(files))
	for _, sig := range signals {
		normalized := normalizeMinMax(sig.Scores)
		for file := range files {
			result[file] += normalized[file] * sig.Weight
		}
	}
	return result
}

func normalizeMinMax(scores map[string]float64) map[string]float64 {
	if len(scores) == 0 {
		return map[string]float64{}
	}
	minVal, maxVal := math.Inf(1), math.Inf(-1)
	for _, v := range scores {
		if v < minVal {
			minVal = v
		}
		if v > maxVal {
			maxVal = v
		}
	}
	rng := maxVal - minVal
	normalized := make(map[string]float64, len(scores))
	if rng == 0 {
		return normalized // all zeros — no signal
	}
	for f, v := range scores {
		normalized[f] = (v - minVal) / rng
	}
	return normalized
}

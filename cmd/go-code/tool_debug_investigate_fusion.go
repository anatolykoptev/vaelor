// cmd/go-code/tool_debug_investigate_fusion.go
package main

import (
	"math"
	"sort"
	"time"

	"github.com/anatolykoptev/go-code/internal/investigate"
	"github.com/anatolykoptev/go-code/internal/ranking"
)

// fusionSignalNames lists the 5 signals used by runFusionRank.
// Preserved as constants for consistent key naming in SignalBreakdown.
const (
	fusionSigMetricAnomaly = "metric_anomaly"
	fusionSigRecency       = "recency"
	fusionSigComplexity    = "complexity"
	fusionSigImpact        = "impact"
	fusionSigHistorical    = "historical"
)

// runFusionRank applies multi-signal fusion (Phase γ.D) to a ranked
// hypothesis slice. It uses ranking.FusionRank (min-max + weighted sum)
// with the following signal weights:
//
//	metric_anomaly  0.40  — AnomalyScore (already 0..1)
//	recency         0.20  — git commits in last 30d, capped at 30
//	complexity      0.15  — SymbolBody.ErrorExits (top-1 only), log-normalized
//	impact          0.15  — Impact.DirectCallers (top-3 only), log-normalized
//	historical      0.10  — binary 1.0 if Subject matches a historical incident
//
// Dead-code hypotheses are already dropped before this call (γ.B). No
// separate dead-code signal is needed.
//
// Key: h.Subject (stable identifier per hypothesis; empty file/line ok).
//
// Returns a new slice sorted by FusedScore descending. Each hypothesis has
// FusedScore and SignalBreakdown set. If len(hyps) == 0, returns nil.
//
// recentCommits: map[file]commitCount from gitutil.CommitsSince (may be nil).
// historicalSubjects: set of Subject strings from HistoricalIncidents (may be nil).
func runFusionRank(
	hyps []investigate.Hypothesis,
	recentCommits map[string]int,
	historicalSubjects map[string]bool,
) []investigate.Hypothesis {
	if len(hyps) == 0 {
		return nil
	}

	// Build per-signal score maps keyed by Subject.
	anomalyScores := make(map[string]float64, len(hyps))
	recencyScores := make(map[string]float64, len(hyps))
	complexityScores := make(map[string]float64, len(hyps))
	impactScores := make(map[string]float64, len(hyps))
	historicalScores := make(map[string]float64, len(hyps))

	for _, h := range hyps {
		key := h.Subject

		// metric_anomaly: AnomalyScore is already 0..1.
		anomalyScores[key] = h.AnomalyScore

		// recency: commits in last 30d for this file, capped at 30.
		if recentCommits != nil && h.File != "" {
			c := recentCommits[h.File]
			recencyScores[key] = math.Min(float64(c)/30.0, 1.0)
		} else {
			recencyScores[key] = 0.0
		}

		// complexity: log-normalized ErrorExits from SymbolBody (top-1 gets it set).
		if h.SymbolBody != nil && h.SymbolBody.ErrorExits > 0 {
			complexityScores[key] = math.Log1p(float64(h.SymbolBody.ErrorExits)) / math.Log1p(10)
		} else {
			complexityScores[key] = 0.0
		}

		// impact: log-normalized DirectCallers from Impact (top-3 get it set).
		if h.Impact != nil && h.Impact.DirectCallers > 0 {
			impactScores[key] = math.Log1p(float64(h.Impact.DirectCallers)) / math.Log1p(100)
		} else {
			impactScores[key] = 0.0
		}

		// historical: binary 1.0 if subject appears in historical incidents.
		if historicalSubjects != nil && historicalSubjects[key] {
			historicalScores[key] = 1.0
		} else {
			historicalScores[key] = 0.0
		}
	}

	signals := []ranking.Signal{
		{Name: fusionSigMetricAnomaly, Weight: 0.40, Scores: anomalyScores},
		{Name: fusionSigRecency, Weight: 0.20, Scores: recencyScores},
		{Name: fusionSigComplexity, Weight: 0.15, Scores: complexityScores},
		{Name: fusionSigImpact, Weight: 0.15, Scores: impactScores},
		{Name: fusionSigHistorical, Weight: 0.10, Scores: historicalScores},
	}

	fused := ranking.FusionRank(signals)

	// Rebuild per-signal normalized scores for breakdown (re-apply normalizeMinMax logic).
	// FusionRank does not expose per-signal normalized values, so we compute them inline.
	// Note on duplication with ranking.FusionRank:
	// We compute per-signal normalized scores locally for SignalBreakdown because
	// ranking.FusionRank doesn't expose its internal normalized values. Both paths use
	// min-max normalization — if the package's algorithm changes (e.g. softmax migration
	// per the deprecation note), TestFusionRankAndBreakdownAgree catches the divergence
	// by asserting fused_score == sum(breakdown[i] * weight[i]).
	normalizedSignals := make([]map[string]float64, len(signals))
	for i, sig := range signals {
		normalizedSignals[i] = normalizeSignalScores(sig.Scores)
	}

	// Attach FusedScore and SignalBreakdown to each hypothesis.
	out := make([]investigate.Hypothesis, len(hyps))
	copy(out, hyps)
	for i := range out {
		key := out[i].Subject
		out[i].FusedScore = fused[key]
		out[i].SignalBreakdown = map[string]float64{
			fusionSigMetricAnomaly: normalizedSignals[0][key],
			fusionSigRecency:       normalizedSignals[1][key],
			fusionSigComplexity:    normalizedSignals[2][key],
			fusionSigImpact:        normalizedSignals[3][key],
			fusionSigHistorical:    normalizedSignals[4][key],
		}
	}

	// Sort by FusedScore descending (stable: equal scores preserve input order).
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].FusedScore > out[j].FusedScore
	})

	return out
}

// normalizeSignalScores applies min-max normalization to a score map.
// Returns empty map for all-equal input (rng==0), matching ranking.FusionRank behavior.
//
// This mirrors the unexported normalizeMinMax in the ranking package. The duplication
// is intentional: FusionRank does not expose per-signal normalized values, and we need
// them for SignalBreakdown. If ranking ever changes its normalization algorithm,
// TestFusionRankAndBreakdownAgree will catch the divergence.
func normalizeSignalScores(scores map[string]float64) map[string]float64 {
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
	out := make(map[string]float64, len(scores))
	if rng == 0 {
		return out // all zeros
	}
	for k, v := range scores {
		out[k] = (v - minVal) / rng
	}
	return out
}

// historicalSubjectsFromIncidents builds a lookup set of Subject strings
// from HistoricalIncidents. The Subject in the incident is the Symbol field
// (closest match to Hypothesis.Subject from the learnings store).
func historicalSubjectsFromIncidents(incidents []investigate.HistoricalIncident) map[string]bool {
	if len(incidents) == 0 {
		return nil
	}
	set := make(map[string]bool, len(incidents))
	for _, inc := range incidents {
		if inc.Symbol != "" {
			set[inc.Symbol] = true
		}
	}
	return set
}

// recentChangeForHypothesis builds a RecentChange for the top-1 hypothesis
// given the raw diff. Since is formatted as "YYYY-MM-DD" 30d ago.
func recentChangeForHypothesis(file, diff string) *investigate.RecentChange {
	if diff == "" {
		return nil
	}
	since := time.Now().AddDate(0, 0, -30).Format("2006-01-02")
	return &investigate.RecentChange{
		File:  file,
		Since: since,
		Diff:  diff,
	}
}

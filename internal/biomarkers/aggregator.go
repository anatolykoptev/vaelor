package biomarkers

import (
	"context"
	"fmt"
	"math"
)

const (
	// weightSumTolerance is the allowed slop on Σweights == 1.0; floating
	// point math accumulates small errors when weight values are written
	// as decimals.
	weightSumTolerance = 0.001
	// scoreSpan maps a weighted [0,1] sum onto the integer health-score
	// [1, 10] range via 1 + round(scoreSpan * weighted).
	scoreSpan = 9.0
)

// Aggregator combines biomarker scores into a 1-10 file score.
type Aggregator struct {
	reg     *Registry
	weights map[string]float64 // biomarker name → weight in [0,1], sum to 1
}

// NewAggregator builds an aggregator. weights must sum to 1.0 (±weightSumTolerance)
// and every weight name must have a registered biomarker; otherwise
// NewAggregator panics — these are programmer errors caught at init.
func NewAggregator(reg *Registry, weights map[string]float64) *Aggregator {
	var sum float64
	for _, w := range weights {
		sum += w
	}
	if math.Abs(sum-1.0) > weightSumTolerance {
		panic(fmt.Sprintf("biomarkers: weights sum %.3f, want 1.0", sum))
	}
	for name := range weights {
		if reg.Get(name) == nil {
			panic(fmt.Sprintf("biomarkers: weight name %q has no registered biomarker", name))
		}
	}
	return &Aggregator{reg: reg, weights: weights}
}

// ScoreFile runs every registered biomarker on the file, combines them
// by weight, and returns a 1-10 score with per-biomarker reasons.
func (a *Aggregator) ScoreFile(ctx context.Context, repoRoot, relPath string) (FileHealth, error) {
	fs := FileHealth{
		Path:    relPath,
		Reasons: make(map[string]string),
		Raw:     make(map[string]float64),
	}
	var weighted float64
	for name, w := range a.weights {
		bm := a.reg.Get(name)
		if bm == nil {
			continue
		}
		s, reason, err := bm.Score(ctx, repoRoot, relPath)
		if err != nil {
			return fs, fmt.Errorf("%s: %w", name, err)
		}
		fs.Raw[name] = s
		if reason != "" {
			fs.Reasons[name] = reason
		}
		weighted += s * w
	}
	if weighted < 0 {
		weighted = 0
	}
	if weighted > 1 {
		weighted = 1
	}
	fs.Score = 1 + int(math.Round(scoreSpan*weighted))
	return fs, nil
}

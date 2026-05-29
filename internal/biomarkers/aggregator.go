package biomarkers

import (
	"context"
	"fmt"
	"math"
)

// Aggregator combines biomarker scores into a 1-10 file score.
type Aggregator struct {
	reg     *Registry
	weights map[string]float64 // biomarker name → weight in [0,1], sum to 1
}

// NewAggregator builds an aggregator. weights must sum to 1.0 (±0.001);
// otherwise NewAggregator panics — this is a programmer error caught at
// init.
func NewAggregator(reg *Registry, weights map[string]float64) *Aggregator {
	var sum float64
	for _, w := range weights {
		sum += w
	}
	if math.Abs(sum-1.0) > 0.001 { //nolint:mnd
		panic(fmt.Sprintf("biomarkers: weights sum %.3f, want 1.0", sum))
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
	fs.Score = 1 + int(math.Round(9.0*weighted)) //nolint:mnd
	return fs, nil
}

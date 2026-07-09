package analyze

import (
	"reflect"
	"sort"
	"testing"
)

// TestFuseSignals_MinmaxModeUnchanged asserts the legacy path still produces
// the exact byte-identical output of ranking.FusionRank for the same inputs.
// Captures a golden ordering pre-change so any future tweak that drifts the
// minmax path will fail this test.
func TestFuseSignals_MinmaxModeUnchanged(t *testing.T) {
	t.Parallel()
	bm25 := map[string]float64{"a.go": 10, "b.go": 5, "c.go": 1}
	pr := map[string]float64{"a.go": 0.1, "b.go": 0.5, "c.go": 0.9}
	exact := map[string]float64{"a.go": 2, "b.go": 0, "c.go": 0}

	cfg := FusionConfig{Mode: FusionModeMinmax}
	got := fuseSignals(cfg, bm25, pr, exact)

	// Manual replication of legacy weighted-minmax sum with const weights
	// (weightBM25=0.5, weightPR=0.3, weightExact=0.2):
	// a: bm25=1.0 pr=0.0 exact=1.0  → 0.5 + 0.0 + 0.2 = 0.7
	// b: bm25=4/9 pr=0.5 exact=0.0  → 0.5*4/9 + 0.3*0.5 + 0 ≈ 0.3722
	// c: bm25=0.0 pr=1.0 exact=0.0  → 0.0 + 0.3 + 0 = 0.3
	want := map[string]float64{
		"a.go": 0.5*1.0 + 0.3*0.0 + 0.2*1.0,
		"b.go": 0.5*(4.0/9.0) + 0.3*0.5 + 0.2*0.0,
		"c.go": 0.5*0.0 + 0.3*1.0 + 0.2*0.0,
	}

	for k, v := range want {
		if absDiff(got[k], v) > 1e-9 {
			t.Errorf("minmax fuse[%s] = %g, want %g (drift from legacy FusionRank)", k, got[k], v)
		}
	}

	// Top-1 must be a.go under default constants.
	if topByScore(got) != "a.go" {
		t.Errorf("minmax top = %q, want a.go", topByScore(got))
	}
}

// TestFuseSignals_RRFMode_DefaultWeights checks that with default weights and
// a doc that's #1 in all three signal lists, that doc is the fused top result.
func TestFuseSignals_RRFMode_DefaultWeights(t *testing.T) {
	t.Parallel()
	// "winner.go" tops every list; the others vary.
	bm25 := map[string]float64{"winner.go": 100, "x.go": 10, "y.go": 1}
	pr := map[string]float64{"winner.go": 0.9, "x.go": 0.4, "y.go": 0.1}
	exact := map[string]float64{"winner.go": 5, "x.go": 1, "y.go": 0}

	cfg := DefaultFusionConfig()
	cfg.Mode = FusionModeRRF
	got := fuseSignals(cfg, bm25, pr, exact)

	if top := topByScore(got); top != "winner.go" {
		t.Errorf("rrf top = %q, want winner.go (winner ranks #1 in all 3 lists)", top)
	}

	// y.go ranks last in all 3 lists → must be lowest fused score.
	if got["y.go"] >= got["x.go"] {
		t.Errorf("rrf: y.go (%g) should rank below x.go (%g)", got["y.go"], got["x.go"])
	}
}

// TestFuseSignals_RRFMode_PageRankHeavier verifies that bumping the PageRank
// weight makes the top-PageRank doc win even when BM25 disagrees.
func TestFuseSignals_RRFMode_PageRankHeavier(t *testing.T) {
	t.Parallel()
	// pr_winner is #1 in PageRank but #3 in BM25/exact.
	// bm_winner is #1 in BM25/exact but #3 in PageRank.
	bm25 := map[string]float64{"bm_winner.go": 100, "mid.go": 50, "pr_winner.go": 1}
	pr := map[string]float64{"pr_winner.go": 0.9, "mid.go": 0.5, "bm_winner.go": 0.1}
	exact := map[string]float64{"bm_winner.go": 5, "mid.go": 2, "pr_winner.go": 0}

	cfg := FusionConfig{
		Mode:           FusionModeRRF,
		WeightBM25:     1.0,
		WeightPageRank: 5.0, // strongly bumped
		WeightSeed:     1.0,
	}
	got := fuseSignals(cfg, bm25, pr, exact)

	if top := topByScore(got); top != "pr_winner.go" {
		t.Errorf("rrf top = %q, want pr_winner.go (PageRank weight=5 should dominate)", top)
	}
}

// TestRankByScore_DeterministicTieBreak guards the lex tie-break used to make
// RRF input reproducible across runs (Go map iteration order is randomized).
func TestRankByScore_DeterministicTieBreak(t *testing.T) {
	t.Parallel()
	scores := map[string]float64{"b.go": 1, "a.go": 1, "c.go": 1}
	got := rankByScore(scores)
	want := []string{"a.go", "b.go", "c.go"} // alphabetic on score-tie
	if !reflect.DeepEqual(got, want) {
		t.Errorf("rankByScore tie-break = %v, want %v", got, want)
	}
}

// --- helpers ---

func topByScore(scores map[string]float64) string {
	type kv struct {
		k string
		v float64
	}
	pairs := make([]kv, 0, len(scores))
	for k, v := range scores {
		pairs = append(pairs, kv{k, v})
	}
	sort.SliceStable(pairs, func(i, j int) bool { return pairs[i].v > pairs[j].v })
	if len(pairs) == 0 {
		return ""
	}
	return pairs[0].k
}

func absDiff(a, b float64) float64 {
	if a > b {
		return a - b
	}
	return b - a
}

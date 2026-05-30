package federate

import (
	"math"

	"github.com/anatolykoptev/go-kit/score"
)

// chi-square df=1 critical values for the significance label.
//
//	3.84 → p<0.05, 6.63 → p<0.01, 10.83 → p<0.001
var (
	g2Thresholds = []float64{3.84, 6.63, 10.83}
	g2Labels     = []string{"weak", "moderate", "strong", "very_strong"}
)

// xlnx returns x·ln(x), defined as 0 at x=0 (the limit).
func xlnx(x float64) float64 {
	if x <= 0 {
		return 0
	}
	return x * math.Log(x)
}

// logLikelihoodG2 computes Dunning's log-likelihood ratio G² for the 2×2
// contingency of two files' window co-occurrence:
//
//	             B present      B absent
//	A present    co             winA-co
//	A absent     winB-co        n-winA-winB+co
//
// G² measures departure from independence AND grows with sample size, so a
// rare coincidence (co=2 of n=200) scores near-zero while a genuine, well-
// supported coupling scores high. Returns 0 for degenerate inputs.
func logLikelihoodG2(co, winA, winB, n int) float64 {
	if n <= 0 || winA <= 0 || winB <= 0 || co <= 0 {
		return 0
	}
	a := float64(co)
	b := float64(winA - co)
	c := float64(winB - co)
	d := float64(n - winA - winB + co)
	if d < 0 {
		return 0
	}
	nf := float64(n)
	g2 := 2 * (xlnx(a) + xlnx(b) + xlnx(c) + xlnx(d) +
		xlnx(nf) -
		xlnx(float64(winA)) - xlnx(float64(n-winA)) -
		xlnx(float64(winB)) - xlnx(float64(n-winB)))
	if g2 < 0 || math.IsNaN(g2) { // floating-point guard
		return 0
	}
	return g2
}

// significanceLabel maps a G² value to a chi-square-derived label.
func significanceLabel(g2 float64) string {
	return score.Bucket(g2, g2Thresholds, g2Labels)
}

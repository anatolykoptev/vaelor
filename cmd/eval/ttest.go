// Package main — eval harness for go-code retrieval quality.
//
// This file: paired Student's t-test with a stdlib-only Student's t two-tailed
// p-value. Implementation mirrors Numerical Recipes §6.4 (regularized
// incomplete beta via continued fraction, Abramowitz-Stegun 26.5.8/26.5.20).
//
// Why no gonum: harness is a single-binary tool; pulling gonum into go-code's
// vendor tree forces a `go mod vendor` round-trip that risks the recurring
// tree-sitter PHP-header strip. ~80 LOC of pure-Go beta is cheaper.
package main

import (
	"fmt"
	"math"
)

// pairedTTest returns (deltaMean, pValue) for the paired sample (a, b).
//
// Each pair (a_i, b_i) yields d_i = a_i - b_i. The test statistic is
// t = mean(d) / (sd(d) / sqrt(n)). The reported p-value is two-tailed.
//
// Edge cases:
//   - len(a) != len(b): returns NaN deltas + 1.0 p (caller should detect by
//     checking deltaMean for NaN).
//   - n < 2 or zero variance: deltaMean is reported (could be 0), p = 1.0.
//   - identical inputs collapse to p = 1.0 (no evidence of difference).
func pairedTTest(a, b []float64) (deltaMean, p float64) {
	if len(a) != len(b) {
		return math.NaN(), 1.0
	}
	n := len(a)
	if n < 2 {
		if n == 1 {
			return a[0] - b[0], 1.0
		}
		return 0, 1.0
	}

	// Compute mean of differences.
	var sum float64
	for i := 0; i < n; i++ {
		sum += a[i] - b[i]
	}
	mean := sum / float64(n)

	// Sample variance (Bessel-corrected).
	var ss float64
	for i := 0; i < n; i++ {
		d := (a[i] - b[i]) - mean
		ss += d * d
	}
	variance := ss / float64(n-1)
	if variance <= 0 {
		// All differences identical → degenerate test.
		if mean == 0 {
			return 0, 1.0
		}
		return mean, 0.0
	}
	stderr := math.Sqrt(variance / float64(n))
	t := mean / stderr
	df := float64(n - 1)
	return mean, studentTTwoTailed(t, df)
}

// studentTTwoTailed returns Pr(|T| ≥ |t|) for T ~ Student-t(df).
//
// Uses the identity Pr(|T| ≥ t) = I_x(df/2, 1/2) where x = df/(df+t²) and I is
// the regularized incomplete beta. Accurate to ~1e-10 for all df ≥ 1.
func studentTTwoTailed(t, df float64) float64 {
	if math.IsNaN(t) || math.IsInf(t, 0) {
		return 0
	}
	// 2.0 = "halve df" per the standard Student-t identity (see Numerical
	// Recipes §6.4); 0.5 = the second beta parameter for the t-distribution.
	const (
		halve     = 2.0
		betaShape = 0.5
	)
	x := df / (df + t*t)
	return regIncompleteBeta(df/halve, betaShape, x)
}

// regIncompleteBeta returns I_x(a, b), the regularized incomplete beta
// function. Translation of Numerical Recipes' betai (§6.4) using a continued
// fraction for stability — the canonical implementation; see also AS 63.
func regIncompleteBeta(a, b, x float64) float64 {
	if x <= 0 {
		return 0
	}
	if x >= 1 {
		return 1
	}
	// Symmetry transformation when x is in the slow-convergence half.
	bt := math.Exp(logGamma(a+b) - logGamma(a) - logGamma(b) +
		a*math.Log(x) + b*math.Log(1.0-x))
	if x < (a+1.0)/(a+b+2.0) {
		return bt * betaCF(a, b, x) / a
	}
	return 1.0 - bt*betaCF(b, a, 1.0-x)/b
}

// betaCF evaluates the continued fraction for the incomplete beta function.
// Lentz's algorithm with iteration cap of 200 — converges in ≪ 50 in practice.
func betaCF(a, b, x float64) float64 {
	const (
		maxIter = 200
		eps     = 3e-16
		fpmin   = 1e-30
	)
	qab := a + b
	qap := a + 1.0
	qam := a - 1.0
	c := 1.0
	d := 1.0 - qab*x/qap
	if math.Abs(d) < fpmin {
		d = fpmin
	}
	d = 1.0 / d
	h := d

	for m := 1; m <= maxIter; m++ {
		mf := float64(m)
		m2 := 2.0 * mf
		// Even step.
		aa := mf * (b - mf) * x / ((qam + m2) * (a + m2))
		d = 1.0 + aa*d
		if math.Abs(d) < fpmin {
			d = fpmin
		}
		c = 1.0 + aa/c
		if math.Abs(c) < fpmin {
			c = fpmin
		}
		d = 1.0 / d
		h *= d * c
		// Odd step.
		aa = -(a + mf) * (qab + mf) * x / ((a + m2) * (qap + m2))
		d = 1.0 + aa*d
		if math.Abs(d) < fpmin {
			d = fpmin
		}
		c = 1.0 + aa/c
		if math.Abs(c) < fpmin {
			c = fpmin
		}
		d = 1.0 / d
		del := d * c
		h *= del
		if math.Abs(del-1.0) < eps {
			break
		}
	}
	return h
}

// logGamma wraps math.Lgamma for the harness (sign is always +1 for a > 0).
func logGamma(x float64) float64 {
	lg, _ := math.Lgamma(x)
	return lg
}

// formatDelta renders the human-readable "+0.034 (p=0.012)" string used in
// Report.Delta. mean and p must be plain floats (not NaN); caller is expected
// to coerce edge cases upstream.
func formatDelta(mean, p float64) string {
	sign := "+"
	if mean < 0 {
		sign = ""
	}
	return fmt.Sprintf("%s%.4f (p=%.4f)", sign, mean, p)
}

// computeDelta runs the paired t-test on each metric and returns the
// formatted DeltaBlock. Queries with errors on either side are skipped (paired
// requires both to be present — the test is on the same query, run twice).
//
// Pairing strategy: queries are matched by (repo, query) string.
func computeDelta(baseline, candidate []QueryResult) *DeltaBlock {
	type pair struct {
		bl, cn QueryResult
	}
	idx := make(map[string]QueryResult, len(baseline))
	for _, r := range baseline {
		if r.Error != "" {
			continue
		}
		idx[r.Repo+"|"+r.Query] = r
	}

	var pairs []pair
	for _, r := range candidate {
		if r.Error != "" {
			continue
		}
		bl, ok := idx[r.Repo+"|"+r.Query]
		if !ok {
			continue
		}
		pairs = append(pairs, pair{bl, r})
	}
	if len(pairs) < 2 {
		return &DeltaBlock{
			NDCG10:   "n/a (insufficient paired samples)",
			Recall10: "n/a (insufficient paired samples)",
			Recall20: "n/a (insufficient paired samples)",
			MRR:      "n/a (insufficient paired samples)",
		}
	}

	cn := func(g func(QueryResult) float64) ([]float64, []float64) {
		ca := make([]float64, len(pairs))
		ba := make([]float64, len(pairs))
		for i, p := range pairs {
			ca[i] = g(p.cn)
			ba[i] = g(p.bl)
		}
		return ca, ba
	}

	cnNDCG, baNDCG := cn(func(r QueryResult) float64 { return r.NDCG10 })
	cnR10, baR10 := cn(func(r QueryResult) float64 { return r.Recall10 })
	cnR20, baR20 := cn(func(r QueryResult) float64 { return r.Recall20 })
	cnMRR, baMRR := cn(func(r QueryResult) float64 { return r.MRR })
	cnLat, baLat := cn(func(r QueryResult) float64 { return r.LatencyMS })

	dn, pn := pairedTTest(cnNDCG, baNDCG)
	dr10, pr10 := pairedTTest(cnR10, baR10)
	dr20, pr20 := pairedTTest(cnR20, baR20)
	dm, pm := pairedTTest(cnMRR, baMRR)
	dlat, plat := pairedTTest(cnLat, baLat)

	return &DeltaBlock{
		NDCG10:    formatDelta(dn, pn),
		Recall10:  formatDelta(dr10, pr10),
		Recall20:  formatDelta(dr20, pr20),
		MRR:       formatDelta(dm, pm),
		LatencyMS: formatLatencyDelta(dlat, plat),
	}
}

// formatLatencyDelta renders the latency delta as "+X.XXXXms (p=Y.YYYY)".
// Latency deltas are in milliseconds; the sign convention is candidate −
// baseline (positive = candidate is slower).
func formatLatencyDelta(meanMS, p float64) string {
	sign := "+"
	if meanMS < 0 {
		sign = ""
	}
	return fmt.Sprintf("%s%.4fms (p=%.4f)", sign, meanMS, p)
}

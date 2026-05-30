package federate

import "math"

// wilsonZ is the z-score for a 95% Wilson confidence interval (hard-coded —
// it never changes for this ranking use).
const wilsonZ = 1.96

// idf returns the inverse-document-frequency weight of a file given how many
// windows it was touched in (winCount) out of n total windows. A file touched
// in a large fraction of windows is "stop-word"-like (CHANGELOG, lockfile) and
// carries near-zero coupling information → low idf. idf(N,N)=0. Returns 0 for
// degenerate inputs.
func idf(winCount, n int) float64 {
	if winCount <= 0 || n <= 0 || winCount > n {
		return 0
	}
	return math.Log(float64(n) / float64(winCount))
}

// wilsonLowerBound returns the lower bound of the Wilson score interval for a
// binomial proportion pos/n at confidence z. It balances the observed
// proportion against the uncertainty from how few observations support it, so
// a thin co=2/n=2 (p̂=1.0) is demoted far below a well-supported co=40/n=45.
// Evan Miller, "How Not To Sort By Average Rating" (2009). Returns 0 for n<=0.
func wilsonLowerBound(pos, n int, z float64) float64 {
	if n <= 0 {
		return 0
	}
	phat := float64(pos) / float64(n)
	nf := float64(n)
	z2 := z * z
	num := phat + z2/(2*nf) - z*math.Sqrt((phat*(1-phat)+z2/(4*nf))/nf)
	den := 1 + z2/nf
	lb := num / den
	if lb < 0 || math.IsNaN(lb) {
		return 0
	}
	return lb
}

// couplingScore is the composite cross-repo coupling rank key: the Wilson lower
// bound on the directional confidence (co over the rarer file's window count),
// down-weighted by the IDF of both files so ubiquitous-file pairs collapse.
//
//	score = wilsonLowerBound(co, min(winA,winB), z) · sqrt( idf(winA,n) · idf(winB,n) )
func couplingScore(co, winA, winB, n int) float64 {
	minWin := winA
	if winB < minWin {
		minWin = winB
	}
	wlb := wilsonLowerBound(co, minWin, wilsonZ)
	return wlb * math.Sqrt(idf(winA, n)*idf(winB, n))
}

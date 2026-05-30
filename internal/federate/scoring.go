package federate

import "math"

// wilsonZ is the z-score for a 95% Wilson confidence interval (hard-coded).
const wilsonZ = 1.96

// ubiquityPct is the window-fraction (percent) above which a file is treated as
// a "stop-word" carrying no coupling signal — a CHANGELOG/lockfile touched in
// nearly every window. Set high (85%) on purpose: genuine couplings are often
// between ACTIVE files touched in 60-70% of windows, so a lower threshold
// (e.g. 50%) would wrongly drop real signal. Only near-universal files filter.
const ubiquityPct = 85

// wilsonLowerBound returns the lower bound of the Wilson score interval for a
// binomial proportion pos/n at confidence z. It balances the observed
// proportion against how few observations support it, so a thin co=2/n=2
// (p̂=1.0) is demoted far below a well-supported co=40/n=45.
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

// isUbiquitous reports whether a file touched in winCount of n windows is too
// common to carry coupling signal (changes in > ubiquityPct% of windows). Used
// as a binary pre-filter to drop CHANGELOG/lockfile noise without penalizing
// the merely-active files that genuine couplings involve.
func isUbiquitous(winCount, n int) bool {
	if n <= 0 {
		return false
	}
	return winCount*100 > n*ubiquityPct
}

package federate

import (
	"math"
	"testing"
)

func TestLogLikelihoodG2_Independence(t *testing.T) {
	// A in 5/10, B in 4/10, co=2 = expected under independence (0.5·0.4·10). G²≈0.
	g2 := logLikelihoodG2(2, 5, 4, 10)
	if g2 > 0.01 {
		t.Fatalf("independent pair → G²≈0, got %.4f", g2)
	}
}

func TestLogLikelihoodG2_PerfectCoupling(t *testing.T) {
	g2 := logLikelihoodG2(8, 8, 8, 20) // both in the same 8 of 20 windows
	if g2 < 10 {
		t.Fatalf("tight 8/20 coupling → high G², got %.4f", g2)
	}
}

func TestLogLikelihoodG2_RareCoincidenceIsWeak(t *testing.T) {
	rare := logLikelihoodG2(2, 2, 2, 200)       // raw lift = 100, but only 2 samples
	genuine := logLikelihoodG2(10, 12, 12, 200) // well-supported coupling
	if rare >= genuine {
		t.Fatalf("G² must rank genuine(%.2f) above rare coincidence(%.2f)", genuine, rare)
	}
}

func TestLogLikelihoodG2_ZeroCells(t *testing.T) {
	if g2 := logLikelihoodG2(0, 3, 4, 10); g2 < 0 || math.IsNaN(g2) {
		t.Fatalf("co=0 → finite non-negative G², got %.4f", g2)
	}
	if g2 := logLikelihoodG2(5, 5, 5, 5); math.IsNaN(g2) || math.IsInf(g2, 0) {
		t.Fatalf("saturated → finite G², got %.4f", g2)
	}
}

func TestSignificanceLabel(t *testing.T) {
	cases := []struct {
		g2   float64
		want string
	}{
		{1.0, "weak"},
		{5.0, "moderate"},
		{8.0, "strong"},
		{20.0, "very_strong"},
	}
	for _, c := range cases {
		if got := significanceLabel(c.g2); got != c.want {
			t.Errorf("significanceLabel(%.1f) = %q, want %q", c.g2, got, c.want)
		}
	}
}

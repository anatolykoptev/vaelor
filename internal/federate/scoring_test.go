package federate

import (
	"math"
	"testing"
)

func almostEqual(a, b float64) bool { return math.Abs(a-b) < 0.02 }

func TestIDF_UbiquitousVsRare(t *testing.T) {
	ubiquitous := idf(60, 100)
	rare := idf(4, 100)
	if ubiquitous >= rare {
		t.Fatalf("rare file must have higher idf: rare=%.3f ubiq=%.3f", rare, ubiquitous)
	}
	if !almostEqual(ubiquitous, math.Log(100.0/60.0)) {
		t.Fatalf("idf(60,100) = %.3f, want %.3f", ubiquitous, math.Log(100.0/60.0))
	}
	if got := idf(100, 100); got != 0 {
		t.Fatalf("idf(N,N) must be 0, got %.3f", got)
	}
}

func TestIDF_Degenerate(t *testing.T) {
	if got := idf(0, 100); got != 0 {
		t.Fatalf("idf(0,_) = %.3f, want 0", got)
	}
	if got := idf(5, 0); got != 0 {
		t.Fatalf("idf(_,0) = %.3f, want 0", got)
	}
}

func TestWilsonLowerBound_PenalizesThinSupport(t *testing.T) {
	thin := wilsonLowerBound(2, 2, wilsonZ)
	loose := wilsonLowerBound(8, 10, wilsonZ)
	strong := wilsonLowerBound(40, 45, wilsonZ)
	if !(thin < loose && loose < strong) {
		t.Fatalf("Wilson-LB must order thin<loose<strong: thin=%.3f loose=%.3f strong=%.3f", thin, loose, strong)
	}
	if math.Abs(thin-0.34) > 0.05 {
		t.Fatalf("wilsonLowerBound(2,2) = %.3f, want ≈0.34", thin)
	}
	if math.Abs(strong-0.77) > 0.05 {
		t.Fatalf("wilsonLowerBound(40,45) = %.3f, want ≈0.77", strong)
	}
}

func TestWilsonLowerBound_Degenerate(t *testing.T) {
	if got := wilsonLowerBound(0, 0, wilsonZ); got != 0 {
		t.Fatalf("wilsonLowerBound(0,0) must be 0, got %.3f", got)
	}
	if got := wilsonLowerBound(5, 3, wilsonZ); math.IsNaN(got) {
		t.Fatalf("wilsonLowerBound(5,3) must be finite, got NaN")
	}
}

func TestCouplingScore_DemotesUbiquitous(t *testing.T) {
	n := 100
	genuine := couplingScore(8, 10, 10, n)
	noise := couplingScore(8, 10, 60, n)
	if noise >= genuine {
		t.Fatalf("ubiquitous-partner pair must score lower: genuine=%.4f noise=%.4f", genuine, noise)
	}
}

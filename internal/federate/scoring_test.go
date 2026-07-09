package federate

import (
	"math"
	"testing"
)

func TestWilsonLowerBound_PenalizesThinSupport(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
	if got := wilsonLowerBound(0, 0, wilsonZ); got != 0 {
		t.Fatalf("wilsonLowerBound(0,0) must be 0, got %.3f", got)
	}
	if got := wilsonLowerBound(5, 3, wilsonZ); math.IsNaN(got) {
		t.Fatalf("wilsonLowerBound(5,3) must be finite, got NaN")
	}
}

func TestIsUbiquitous(t *testing.T) {
	t.Parallel()
	// CHANGELOG in every window → ubiquitous.
	if !isUbiquitous(15, 15) {
		t.Fatal("file in 100% of windows must be ubiquitous")
	}
	if !isUbiquitous(18, 20) { // 90%
		t.Fatal("file in 90% of windows must be ubiquitous")
	}
	// An active genuine file (60-70%) must NOT be filtered.
	if isUbiquitous(10, 15) { // 67%
		t.Fatal("file in 67% of windows must NOT be ubiquitous (active genuine file)")
	}
	if isUbiquitous(2, 15) { // rare
		t.Fatal("rare file must not be ubiquitous")
	}
	if isUbiquitous(5, 0) {
		t.Fatal("n=0 guard")
	}
}

package main

import (
	"testing"

	"github.com/anatolykoptev/go-code/internal/compare"
)

// TestConvertArchMetrics_Approximate verifies that convertArchMetrics omits
// MaxCallDepth, InterfaceRatio, and CommunityCount when Approximate=true, and
// sets the Approximate attr so machine consumers can distinguish approximate
// results from real zeros.
//
// Red-on-revert: if the Approximate branch is removed from convertArchMetrics
// (or if MaxCallDepth/InterfaceRatio are unconditionally copied), the
// Approximate=true case will have MaxCallDepth=5/InterfaceRatio=0.8 set on the
// XML struct, violating the "uncomputed fields must be absent" invariant.
func TestConvertArchMetrics_Approximate(t *testing.T) {
	m := &compare.ArchMetrics{
		PackageCount:      10,
		CrossPkgCallRatio: 0.3,
		Approximate:       true,
		Hint:              compare.HintApproxArchMetrics,
		// These fields must NOT appear in the XML output when Approximate=true.
		MaxCallDepth:   5,
		InterfaceRatio: 0.8,
		CommunityCount: 3,
	}

	got := convertArchMetrics(m)

	if !got.Approximate {
		t.Error("Approximate = false, want true")
	}
	if got.MaxCallDepth != 0 {
		t.Errorf("MaxCallDepth = %d, want 0 (uncomputed field must be omitted when Approximate)", got.MaxCallDepth)
	}
	if got.InterfaceRatio != 0 {
		t.Errorf("InterfaceRatio = %f, want 0 (uncomputed field must be omitted when Approximate)", got.InterfaceRatio)
	}
	if got.CommunityCount != 0 {
		t.Errorf("CommunityCount = %d, want 0 (uncomputed field must be omitted when Approximate)", got.CommunityCount)
	}
	if got.PackageCount != 10 {
		t.Errorf("PackageCount = %d, want 10", got.PackageCount)
	}
	if got.Hint != compare.HintApproxArchMetrics {
		t.Errorf("Hint = %q, want HintApproxArchMetrics", got.Hint)
	}
}

// TestConvertArchMetrics_Full verifies that when Approximate=false, all fields
// including MaxCallDepth and InterfaceRatio are copied to the XML struct.
func TestConvertArchMetrics_Full(t *testing.T) {
	m := &compare.ArchMetrics{
		PackageCount:      20,
		CommunityCount:    4,
		CrossPkgCallRatio: 0.5,
		MaxCallDepth:      7,
		InterfaceRatio:    0.6,
	}

	got := convertArchMetrics(m)

	if got.Approximate {
		t.Error("Approximate = true, want false for full AGE metrics")
	}
	if got.MaxCallDepth != 7 {
		t.Errorf("MaxCallDepth = %d, want 7", got.MaxCallDepth)
	}
	if got.InterfaceRatio != 0.6 {
		t.Errorf("InterfaceRatio = %f, want 0.6", got.InterfaceRatio)
	}
	if got.CommunityCount != 4 {
		t.Errorf("CommunityCount = %d, want 4", got.CommunityCount)
	}
}

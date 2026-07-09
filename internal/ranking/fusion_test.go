package ranking

import (
	"math"
	"testing"
)

func TestFusionRank_SingleSignal(t *testing.T) {
	t.Parallel()
	signals := []Signal{
		{Name: "bm25", Weight: 1.0, Scores: map[string]float64{"a.go": 10.0, "b.go": 5.0, "c.go": 0.0}},
	}
	result := FusionRank(signals)
	if result["a.go"] < result["b.go"] || result["b.go"] < result["c.go"] {
		t.Errorf("ordering wrong: a=%f b=%f c=%f", result["a.go"], result["b.go"], result["c.go"])
	}
}

func TestFusionRank_TwoSignals_Balanced(t *testing.T) {
	t.Parallel()
	signals := []Signal{
		{Name: "bm25", Weight: 0.5, Scores: map[string]float64{"a.go": 10.0, "b.go": 0.0}},
		{Name: "pr", Weight: 0.5, Scores: map[string]float64{"a.go": 0.0, "b.go": 1.0}},
	}
	result := FusionRank(signals)
	if math.Abs(result["a.go"]-result["b.go"]) > 0.01 {
		t.Errorf("expected equal scores, got a=%f b=%f", result["a.go"], result["b.go"])
	}
}

func TestFusionRank_Empty(t *testing.T) {
	t.Parallel()
	if len(FusionRank(nil)) != 0 {
		t.Error("expected empty")
	}
}

func TestFusionRank_ConstantSignal(t *testing.T) {
	t.Parallel()
	signals := []Signal{
		{Name: "flat", Weight: 1.0, Scores: map[string]float64{"a.go": 5.0, "b.go": 5.0}},
	}
	result := FusionRank(signals)
	if result["a.go"] != 0 || result["b.go"] != 0 {
		t.Errorf("constant signal should produce 0, got a=%f b=%f", result["a.go"], result["b.go"])
	}
}

func TestNormalizeMinMax(t *testing.T) {
	t.Parallel()
	n := normalizeMinMax(map[string]float64{"a": 10, "b": 5, "c": 0})
	if n["a"] != 1.0 || n["c"] != 0.0 || math.Abs(n["b"]-0.5) > 0.01 {
		t.Errorf("bad normalization: %v", n)
	}
}

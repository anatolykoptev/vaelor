package biomarkers

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type stubBM struct {
	name string
	s    float64
	r    string
}

func (b stubBM) Name() string                                                  { return b.name }
func (b stubBM) Score(_ context.Context, _, _ string) (float64, string, error) { return b.s, b.r, nil }

func TestAggregator_AllZeroIsOne(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	r.Register(stubBM{name: "prior_defect", s: 0})
	r.Register(stubBM{name: "churn_risk", s: 0})
	agg := NewAggregator(r, map[string]float64{"prior_defect": 0.6, "churn_risk": 0.4})
	fs, err := agg.ScoreFile(context.Background(), "/repo", "foo.go")
	if err != nil {
		t.Fatal(err)
	}
	if fs.Score != 1 {
		t.Fatalf("zero biomarkers → score 1, got %d", fs.Score)
	}
}

func TestAggregator_AllMaxIsTen(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	r.Register(stubBM{name: "prior_defect", s: 1, r: "all the defects"})
	r.Register(stubBM{name: "churn_risk", s: 1, r: "all the churn"})
	agg := NewAggregator(r, map[string]float64{"prior_defect": 0.6, "churn_risk": 0.4})
	fs, _ := agg.ScoreFile(context.Background(), "/repo", "f.go")
	if fs.Score != 10 {
		t.Fatalf("max biomarkers → score 10, got %d", fs.Score)
	}
	if fs.Reasons["prior_defect"] != "all the defects" {
		t.Fatalf("missing reason: %#v", fs.Reasons)
	}
}

func TestAggregator_WeightsApplied(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	r.Register(stubBM{name: "prior_defect", s: 1})
	r.Register(stubBM{name: "churn_risk", s: 0})
	agg := NewAggregator(r, map[string]float64{"prior_defect": 0.6, "churn_risk": 0.4})
	fs, _ := agg.ScoreFile(context.Background(), "/repo", "f.go")
	// weighted = 1*0.6 + 0*0.4 = 0.6 → 1 + round(9*0.6) = 1 + 5 = 6
	if fs.Score != 6 {
		t.Fatalf("expected 6 (0.6 weighted), got %d", fs.Score)
	}
}

// TestAggregator_BadWeightSumPanics guards the load-bearing invariant
// that NewAggregator rejects weight maps that don't sum to 1.0.
func TestAggregator_BadWeightSumPanics(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	r.Register(stubBM{name: "prior_defect"})
	r.Register(stubBM{name: "churn_risk"})
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on weight sum != 1.0")
		}
	}()
	NewAggregator(r, map[string]float64{"prior_defect": 0.6, "churn_risk": 0.5}) // sum=1.1
}

// TestAggregator_UnknownWeightKeyPanics guards against silent score-drop
// when a weight name has no registered biomarker (e.g. a typo).
func TestAggregator_UnknownWeightKeyPanics(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	r.Register(stubBM{name: "prior_defect"})
	// Note: churn_risk NOT registered.
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on unknown weight key")
		}
	}()
	NewAggregator(r, map[string]float64{"prior_defect": 0.6, "churn_risk": 0.4})
}

// TestAggregator_BiomarkerErrorWrapsName ensures ScoreFile's error
// preserves the biomarker name so callers can identify the failing one.
func TestAggregator_BiomarkerErrorWrapsName(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	r.Register(failBM{name: "boom"})
	agg := NewAggregator(r, map[string]float64{"boom": 1.0})
	_, err := agg.ScoreFile(context.Background(), "/repo", "f.go")
	if err == nil {
		t.Fatal("expected error from failing biomarker")
	}
	if !strings.Contains(err.Error(), "boom:") {
		t.Fatalf("error must wrap biomarker name, got %q", err)
	}
}

// failBM is a test double that always errors.
type failBM struct{ name string }

func (b failBM) Name() string { return b.name }
func (b failBM) Score(_ context.Context, _, _ string) (float64, string, error) {
	return 0, "", errors.New("synthetic failure")
}

package biomarkers

import (
	"context"
	"testing"
)

type stubBM struct {
	name string
	s    float64
	r    string
}

func (b stubBM) Name() string                                                   { return b.name }
func (b stubBM) Score(_ context.Context, _, _ string) (float64, string, error) { return b.s, b.r, nil }

func TestAggregator_AllZeroIsOne(t *testing.T) {
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

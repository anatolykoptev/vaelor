package compare

import (
	"testing"

	"github.com/anatolykoptev/go-code/internal/parser"
)

func TestComputeHotspots(t *testing.T) {
	churn := map[string]ChurnStats{
		"a.go": {Commits: 20, Additions: 500, Deletions: 200},
		"b.go": {Commits: 2, Additions: 50, Deletions: 10},
		"c.go": {Commits: 10, Additions: 200, Deletions: 100},
		"d.go": {Commits: 1, Additions: 10, Deletions: 5},
	}

	fileComplexity := map[string]float64{
		"a.go": 12.0,
		"b.go": 2.0,
		"c.go": 8.0,
		"d.go": 15.0,
	}

	hotspots := ComputeHotspots(churn, fileComplexity)

	if len(hotspots) == 0 {
		t.Fatal("expected hotspots, got none")
	}

	// a.go has highest churn AND high complexity -> should be top hotspot.
	if hotspots[0].File != "a.go" {
		t.Errorf("top hotspot = %q, want a.go", hotspots[0].File)
	}

	// Scores should be in [0, 1].
	for _, h := range hotspots {
		if h.Score < 0 || h.Score > 1 {
			t.Errorf("hotspot %q score = %.2f, want [0, 1]", h.File, h.Score)
		}
	}

	// Should be sorted by score descending.
	for i := 1; i < len(hotspots); i++ {
		if hotspots[i].Score > hotspots[i-1].Score {
			t.Errorf("hotspots not sorted: [%d].Score=%.2f > [%d].Score=%.2f",
				i, hotspots[i].Score, i-1, hotspots[i-1].Score)
		}
	}
}

func TestComputeHotspots_Empty(t *testing.T) {
	hotspots := ComputeHotspots(nil, nil)
	if len(hotspots) != 0 {
		t.Errorf("expected empty hotspots, got %d", len(hotspots))
	}
}

func TestPercentileRank(t *testing.T) {
	values := []float64{1, 2, 3, 4, 5}
	if p := percentileRank(5, values); p != 1.0 {
		t.Errorf("percentileRank(5) = %.2f, want 1.0", p)
	}
	if p := percentileRank(1, values); p < 0.1 || p > 0.3 {
		t.Errorf("percentileRank(1) = %.2f, want ~0.2", p)
	}
}

func TestClassifyRisk(t *testing.T) {
	tests := []struct {
		score  float64
		expect string
	}{
		{0.90, "critical"},
		{0.81, "critical"},
		{0.70, "high"},
		{0.64, "high"},
		{0.50, "moderate"},
		{0.36, "moderate"},
		{0.20, ""},
		{0.0, ""},
	}

	for _, tt := range tests {
		got := classifyRisk(tt.score)
		if got != tt.expect {
			t.Errorf("classifyRisk(%.2f) = %q, want %q", tt.score, got, tt.expect)
		}
	}
}

func TestFileComplexityFromSnapshot(t *testing.T) {
	snap := &RepoSnapshot{
		Symbols: []*parser.Symbol{
			{Name: "Simple", Kind: parser.KindFunction, File: "a.go", Body: "func Simple() { return 1 }"},
			{Name: "Complex", Kind: parser.KindFunction, File: "a.go", Body: "func Complex() { if a { } if b && c { } for i := range x { } }"},
			{Name: "Other", Kind: parser.KindFunction, File: "b.go", Body: "func Other() { return 1 }"},
		},
	}

	fc := FileComplexityFromSnapshot(snap)

	if len(fc) != 2 {
		t.Fatalf("got %d files, want 2", len(fc))
	}

	// a.go has two functions: Simple (cc=1) and Complex (cc=5). Average = 3.0
	aCC := fc["a.go"]
	if aCC < 2 || aCC > 5 {
		t.Errorf("a.go complexity = %.1f, want ~3.0", aCC)
	}

	// b.go has one function: Other (cc=1)
	bCC := fc["b.go"]
	if bCC != 1 {
		t.Errorf("b.go complexity = %.1f, want 1", bCC)
	}
}

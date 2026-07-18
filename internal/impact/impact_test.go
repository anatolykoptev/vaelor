package impact

import (
	"context"
	"testing"

	"github.com/anatolykoptev/vaelor/internal/callgraph"
	"github.com/anatolykoptev/vaelor/internal/parser"
)

// TestAnalyze_DirectCallers builds A->B->C and verifies that analyzing C
// finds B as a direct caller and A as a transitive caller.
func TestAnalyze_DirectCallers(t *testing.T) {
	t.Parallel()
	symA := &parser.Symbol{Name: "A", Kind: parser.KindFunction, File: "/src/a.go", StartLine: 1, EndLine: 10}
	symB := &parser.Symbol{Name: "B", Kind: parser.KindFunction, File: "/src/b.go", StartLine: 1, EndLine: 10}
	symC := &parser.Symbol{Name: "C", Kind: parser.KindFunction, File: "/src/c.go", StartLine: 1, EndLine: 10}

	cg := &callgraph.CallGraph{
		Symbols: []*parser.Symbol{symA, symB, symC},
		Edges: []callgraph.CallEdge{
			{Caller: symA, Callee: symB, CalleeName: "B"},
			{Caller: symB, Callee: symC, CalleeName: "C"},
		},
	}

	result := Analyze(context.Background(), cg, "C", Options{MaxDepth: 5})

	if !result.Found {
		t.Fatal("expected symbol to be found")
	}
	if len(result.DirectCallers) != 1 {
		t.Fatalf("expected 1 direct caller, got %d", len(result.DirectCallers))
	}
	if result.DirectCallers[0].Name != "B" {
		t.Errorf("expected direct caller B, got %s", result.DirectCallers[0].Name)
	}
	if len(result.TransitiveCallers) != 1 {
		t.Fatalf("expected 1 transitive caller, got %d", len(result.TransitiveCallers))
	}
	if result.TransitiveCallers[0].Name != "A" {
		t.Errorf("expected transitive caller A, got %s", result.TransitiveCallers[0].Name)
	}
	if result.TotalAffected != 2 {
		t.Errorf("expected 2 total affected, got %d", result.TotalAffected)
	}
}

// TestAnalyze_NotFound verifies that a non-existent symbol returns blast radius "none".
func TestAnalyze_NotFound(t *testing.T) {
	t.Parallel()
	cg := &callgraph.CallGraph{
		Symbols: []*parser.Symbol{
			{Name: "Existing", Kind: parser.KindFunction, File: "/src/main.go", StartLine: 1, EndLine: 10},
		},
	}

	result := Analyze(context.Background(), cg, "NonExistent", Options{MaxDepth: 5})

	if result.Found {
		t.Error("expected Found=false for non-existent symbol")
	}
	if result.BlastRadius != "none" {
		t.Errorf("expected blast radius 'none', got %q", result.BlastRadius)
	}
}

// TestClassifyBlastRadius is a table test for the classification thresholds.
func TestClassifyBlastRadius(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		callers  int
		packages int
		want     string
	}{
		{"no callers", 0, 0, "none"},
		{"few callers few packages", 3, 1, "low"},
		{"at low threshold", 5, 2, "low"},
		{"many callers few packages", 10, 3, "medium"},
		{"at medium threshold", 20, 5, "medium"},
		{"many callers many packages", 25, 8, "high"},
		{"few callers many packages", 3, 6, "high"},
		{"many callers few packages over medium", 21, 3, "high"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifyBlastRadius(tt.callers, tt.packages)
			if got != tt.want {
				t.Errorf("classifyBlastRadius(%d, %d) = %q, want %q", tt.callers, tt.packages, got, tt.want)
			}
		})
	}
}

// TestAnalyze_ConfidenceDegrades verifies that confidence drops with distance.
func TestAnalyze_ConfidenceDegrades(t *testing.T) {
	t.Parallel()
	// Build chain: A -> B -> C -> D -> target
	target := &parser.Symbol{Name: "target", Kind: parser.KindFunction, File: "/src/target.go", StartLine: 1, EndLine: 10}
	symD := &parser.Symbol{Name: "D", Kind: parser.KindFunction, File: "/src/d.go", StartLine: 1, EndLine: 10}
	symC := &parser.Symbol{Name: "C", Kind: parser.KindFunction, File: "/src/c.go", StartLine: 1, EndLine: 10}
	symB := &parser.Symbol{Name: "B", Kind: parser.KindFunction, File: "/src/b.go", StartLine: 1, EndLine: 10}
	symA := &parser.Symbol{Name: "A", Kind: parser.KindFunction, File: "/src/a.go", StartLine: 1, EndLine: 10}

	cg := &callgraph.CallGraph{
		Symbols: []*parser.Symbol{symA, symB, symC, symD, target},
		Edges: []callgraph.CallEdge{
			{Caller: symD, Callee: target, CalleeName: "target"},
			{Caller: symC, Callee: symD, CalleeName: "D"},
			{Caller: symB, Callee: symC, CalleeName: "C"},
			{Caller: symA, Callee: symB, CalleeName: "B"},
		},
	}

	result := Analyze(context.Background(), cg, "target", Options{MaxDepth: 10})

	if !result.Found {
		t.Fatal("expected symbol to be found")
	}

	// Direct caller D at distance 1 -> confidence 1.0
	if len(result.DirectCallers) != 1 {
		t.Fatalf("expected 1 direct caller, got %d", len(result.DirectCallers))
	}
	if result.DirectCallers[0].Confidence != 1.0 {
		t.Errorf("direct caller confidence = %f, want 1.0", result.DirectCallers[0].Confidence)
	}

	// Transitive callers: C (dist 2, conf 0.8), B (dist 3, conf 0.6), A (dist 4, conf 0.4)
	if len(result.TransitiveCallers) != 3 {
		t.Fatalf("expected 3 transitive callers, got %d", len(result.TransitiveCallers))
	}

	// Verify confidence decreases with distance.
	prevConf := 1.1
	for _, tc := range result.TransitiveCallers {
		if tc.Confidence >= prevConf {
			t.Errorf("confidence should decrease: %s has confidence %f >= previous %f",
				tc.Name, tc.Confidence, prevConf)
		}
		prevConf = tc.Confidence
	}
}

// TestAnalyze_MaxDepthLimits verifies that MaxDepth restricts traversal.
func TestAnalyze_MaxDepthLimits(t *testing.T) {
	t.Parallel()
	// Chain: A -> B -> C -> target
	target := &parser.Symbol{Name: "target", Kind: parser.KindFunction, File: "/src/target.go", StartLine: 1, EndLine: 10}
	symC := &parser.Symbol{Name: "C", Kind: parser.KindFunction, File: "/src/c.go", StartLine: 1, EndLine: 10}
	symB := &parser.Symbol{Name: "B", Kind: parser.KindFunction, File: "/src/b.go", StartLine: 1, EndLine: 10}
	symA := &parser.Symbol{Name: "A", Kind: parser.KindFunction, File: "/src/a.go", StartLine: 1, EndLine: 10}

	cg := &callgraph.CallGraph{
		Symbols: []*parser.Symbol{symA, symB, symC, target},
		Edges: []callgraph.CallEdge{
			{Caller: symC, Callee: target, CalleeName: "target"},
			{Caller: symB, Callee: symC, CalleeName: "C"},
			{Caller: symA, Callee: symB, CalleeName: "B"},
		},
	}

	// MaxDepth=1 should only find direct caller C.
	result := Analyze(context.Background(), cg, "target", Options{MaxDepth: 1})

	if result.TotalAffected != 1 {
		t.Errorf("expected 1 affected with MaxDepth=1, got %d", result.TotalAffected)
	}
	if len(result.DirectCallers) != 1 || result.DirectCallers[0].Name != "C" {
		t.Errorf("expected direct caller C with MaxDepth=1")
	}
	if len(result.TransitiveCallers) != 0 {
		t.Errorf("expected no transitive callers with MaxDepth=1, got %d", len(result.TransitiveCallers))
	}
}

// TestAnalyze_MultipleDirectCallers verifies fan-in from multiple callers.
func TestAnalyze_MultipleDirectCallers(t *testing.T) {
	t.Parallel()
	target := &parser.Symbol{Name: "target", Kind: parser.KindFunction, File: "/src/target.go", StartLine: 1, EndLine: 10}
	caller1 := &parser.Symbol{Name: "caller1", Kind: parser.KindFunction, File: "/pkg1/a.go", StartLine: 1, EndLine: 10}
	caller2 := &parser.Symbol{Name: "caller2", Kind: parser.KindFunction, File: "/pkg2/b.go", StartLine: 1, EndLine: 10}
	caller3 := &parser.Symbol{Name: "caller3", Kind: parser.KindFunction, File: "/pkg3/c.go", StartLine: 1, EndLine: 10}

	cg := &callgraph.CallGraph{
		Symbols: []*parser.Symbol{target, caller1, caller2, caller3},
		Edges: []callgraph.CallEdge{
			{Caller: caller1, Callee: target, CalleeName: "target"},
			{Caller: caller2, Callee: target, CalleeName: "target"},
			{Caller: caller3, Callee: target, CalleeName: "target"},
		},
	}

	result := Analyze(context.Background(), cg, "target", Options{MaxDepth: 5})

	if result.TotalAffected != 3 {
		t.Errorf("expected 3 total affected, got %d", result.TotalAffected)
	}
	if len(result.DirectCallers) != 3 {
		t.Errorf("expected 3 direct callers, got %d", len(result.DirectCallers))
	}
	if len(result.AffectedPackages) != 3 {
		t.Errorf("expected 3 affected packages, got %d", len(result.AffectedPackages))
	}
}

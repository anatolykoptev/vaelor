package impact

import (
	"context"
	"testing"

	"github.com/anatolykoptev/vaelor/internal/callgraph"
)

// TestImpactAnalysis_AstroComponent builds a CallGraph with a UsesIndex
// representing 3 Astro files that each render Breadcrumbs.astro, and verifies
// that Analyze returns Found=true, 3 DirectCallers, and TotalAffected=3.
func TestImpactAnalysis_AstroComponent(t *testing.T) {
	t.Parallel()
	target := "src/components/Breadcrumbs.astro"
	callers := []string{
		"src/pages/Home.astro",
		"src/pages/About.astro",
		"src/layouts/Base.astro",
	}

	cg := &callgraph.CallGraph{
		UsesIndex: map[string][]string{
			target: callers,
		},
	}

	result := Analyze(context.Background(), cg, target, Options{MaxDepth: 5})

	if !result.Found {
		t.Fatal("expected Found=true for Astro component in UsesIndex")
	}
	if len(result.DirectCallers) != 3 {
		t.Errorf("expected 3 DirectCallers, got %d", len(result.DirectCallers))
	}
	if result.TotalAffected != 3 {
		t.Errorf("expected TotalAffected=3, got %d", result.TotalAffected)
	}
	// Verify each caller appears exactly once in DirectCallers.
	seen := make(map[string]bool)
	for _, c := range result.DirectCallers {
		seen[c.Name] = true
	}
	for _, caller := range callers {
		if !seen[caller] {
			t.Errorf("expected caller %q in DirectCallers", caller)
		}
	}
}

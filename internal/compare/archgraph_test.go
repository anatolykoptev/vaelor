package compare

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestCollectArchMetrics_NilStore(t *testing.T) {
	result := CollectArchMetrics(context.Background(), nil, "test")
	if result != nil {
		t.Error("expected nil for nil store")
	}
}

// TestCollectArchMetrics_Integration requires DATABASE_URL and an indexed graph.
// Skip in CI — tested manually via code_compare MCP tool.
func TestCollectArchMetrics_Integration(t *testing.T) {
	t.Skip("requires DATABASE_URL and indexed graph")
}

// TestFallbackArchMetrics verifies that FallbackArchMetrics returns non-zero
// PackageCount when run against the go-code repo itself.  We navigate two
// directories up from the test file's location to reach the repo root.
func TestFallbackArchMetrics(t *testing.T) {
	if testing.Short() {
		t.Skip("heavy integration test; runs in the nightly full suite (make test)")
	}
	// Locate the repo root relative to this test file's directory.
	// internal/compare → ../../ = repo root.
	repoRoot, err := filepath.Abs("../../")
	if err != nil {
		t.Fatalf("filepath.Abs: %v", err)
	}
	// Sanity-check: go.mod must exist at the root.
	if _, err := os.Stat(filepath.Join(repoRoot, "go.mod")); err != nil {
		t.Fatalf("go.mod not found at %s: %v", repoRoot, err)
	}

	ctx := context.Background()
	got := FallbackArchMetrics(ctx, repoRoot)
	if got == nil {
		t.Fatal("FallbackArchMetrics returned nil")
	}
	if got.PackageCount == 0 {
		t.Errorf("PackageCount = 0, want > 0 for repo at %s", repoRoot)
	}
	// CrossPkgCallRatio must be in [0,1].
	if got.CrossPkgCallRatio < 0 || got.CrossPkgCallRatio > 1 {
		t.Errorf("CrossPkgCallRatio = %f, want in [0, 1]", got.CrossPkgCallRatio)
	}
	t.Logf("FallbackArchMetrics: packages=%d crossPkgRatio=%.3f",
		got.PackageCount, got.CrossPkgCallRatio)
}

// TestFallbackArchMetrics_InvalidRoot verifies that FallbackArchMetrics does
// not panic on a root path that does not exist. The ingest walk returns no
// error for a nonexistent path (it simply finds no files), so the function
// returns a non-nil zero-valued struct rather than nil.
//
// FallbackArchMetrics returns nil only when root == "" or when BuildFromRepo
// itself returns an error (e.g. context cancelled before ingest, parse panic).
func TestFallbackArchMetrics_InvalidRoot(t *testing.T) {
	ctx := context.Background()
	got := FallbackArchMetrics(ctx, "/nonexistent/path/that/cannot/exist")
	if got == nil {
		t.Fatal("FallbackArchMetrics returned nil for nonexistent root, want non-nil empty struct (ingest does not error on missing path)")
	}
	// Expect zero metrics — no packages found in a nonexistent path.
	if got.PackageCount != 0 {
		t.Errorf("PackageCount = %d, want 0 for nonexistent root", got.PackageCount)
	}
}

// TestFallbackArchMetrics_EmptyRoot verifies that FallbackArchMetrics returns
// nil when root is empty string.
func TestFallbackArchMetrics_EmptyRoot(t *testing.T) {
	ctx := context.Background()
	got := FallbackArchMetrics(ctx, "")
	if got != nil {
		t.Errorf("FallbackArchMetrics(\"\") = %+v, want nil", got)
	}
}

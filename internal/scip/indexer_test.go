package scip_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	gocodescip "github.com/anatolykoptev/vaelor/internal/scip"
)

func TestRunIndexer_MissingBinary(t *testing.T) {
	t.Parallel()
	cfg := gocodescip.IndexerConfig{
		Name: "nonexistent-scip-indexer-xyz",
		Args: nil,
	}
	_, err := gocodescip.RunIndexer(context.Background(), cfg, t.TempDir())
	if err == nil {
		t.Fatal("expected error for missing binary, got nil")
	}
}

func TestRunIndexerSafe_ReadOnlyDir(t *testing.T) {
	t.Parallel()
	// Create a read-only source directory with a Go file.
	srcDir := t.TempDir()

	goMod := []byte("module example.com/rotest\n\ngo 1.21\n")
	if err := os.WriteFile(filepath.Join(srcDir, "go.mod"), goMod, 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	mainGo := []byte("package main\n\nfunc main() {}\n")
	if err := os.WriteFile(filepath.Join(srcDir, "main.go"), mainGo, 0o644); err != nil {
		t.Fatalf("write main.go: %v", err)
	}

	// Make the directory read-only.
	if err := os.Chmod(srcDir, 0o555); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(srcDir, 0o755) })

	cfg := gocodescip.IndexerConfig{
		Name: "nonexistent-scip-indexer-xyz", // intentionally missing
		Args: nil,
	}

	// RunIndexerSafe should detect read-only dir, copy to temp, then fail on
	// the missing binary — not on a write-permission error.
	_, err := gocodescip.RunIndexerSafe(context.Background(), cfg, srcDir)
	if err == nil {
		t.Fatal("expected error for missing binary, got nil")
	}
	// Error must mention the binary name, not a permission problem.
	if !contains(err.Error(), "nonexistent-scip-indexer-xyz") {
		t.Errorf("unexpected error (expected binary-not-found): %v", err)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr ||
		len(s) > 0 && func() bool {
			for i := 0; i <= len(s)-len(substr); i++ {
				if s[i:i+len(substr)] == substr {
					return true
				}
			}
			return false
		}())
}

// TestRunIndexer_ScipGo was removed: scip-go is no longer installed in the
// Docker image because Go analysis is handled by internal/goanalysis (go/types).

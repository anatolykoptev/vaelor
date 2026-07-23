package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/anatolykoptev/go-kit/llm"
	"github.com/anatolykoptev/vaelor/internal/compare"
)

// TestCompareRepos_TinyDeadline_ReturnsPartial verifies the #566 fix at the
// CompareRepos level: a tiny deadline produces a partial result (not a hard
// error), and the result is returned before the deadline window closes.
// RED-on-revert: remove the ctx.Err() checks in matchExact/CompareRepos and
// this either returns a hard error or takes far longer than 2s.
func TestCompareRepos_TinyDeadline_ReturnsPartial(t *testing.T) {
	root := writeCompareFixture(t)

	// 5ms deadline — enough for ingest to start but not for the full
	// parse+match pipeline. Burn it before entering CompareRepos.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()
	time.Sleep(10 * time.Millisecond)

	t0 := time.Now()
	result, err := compare.CompareRepos(ctx, compare.CompareInput{
		RootA: root,
		RootB: root,
		Query: "test tiny deadline",
		Opts:  compare.SnapshotOpts{Language: "go"},
	}, llm.NoOp{})
	elapsed := time.Since(t0)

	if err != nil {
		t.Fatalf("CompareRepos returned error on expired ctx (want partial): %v", err)
	}
	if result == nil {
		t.Fatal("CompareRepos returned nil result on expired ctx")
	}
	if !result.Partial {
		t.Fatal("result.Partial = false, want true (ctx deadline expired)")
	}
	if elapsed > 2*time.Second {
		t.Fatalf("CompareRepos took %s on expired ctx, want < 2s", elapsed)
	}
}

// TestCompareRepos_FastPath_NoPartialFooter verifies the fast path (generous
// deadline) returns a full result with Partial=false.
func TestCompareRepos_FastPath_NoPartialFooter(t *testing.T) {
	root := writeCompareFixture(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result, err := compare.CompareRepos(ctx, compare.CompareInput{
		RootA: root,
		RootB: root,
		Query: "test fast path",
		Opts:  compare.SnapshotOpts{Language: "go"},
	}, llm.NoOp{})
	if err != nil {
		t.Fatalf("CompareRepos fast path: %v", err)
	}
	if result == nil {
		t.Fatal("nil result on fast path")
	}
	if result.Partial {
		t.Fatalf("fast path must not be partial, got Partial=true")
	}
}

// writeCompareFixture creates a temp Go repo for CompareRepos tests.
func writeCompareFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/demo\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n\nfunc main() {}\nfunc helper() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

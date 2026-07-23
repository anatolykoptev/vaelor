package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/anatolykoptev/vaelor/internal/analyze"
	"github.com/anatolykoptev/vaelor/internal/ranking"
)

// TestHandleExplore_TinyDeadline_ReturnsPartialWithFooter verifies #534: under
// a tiny injected soft deadline, explore returns a PARTIAL result with a footer
// naming what was truncated — never a bare error, never a session-killing hard
// timeout. RED-on-revert: remove the SoftDeadline / softCtx.Err() guard in
// handleExplore and this test gets a hard error (errResult) instead of a
// partial footer.
func TestHandleExplore_TinyDeadline_ReturnsPartialWithFooter(t *testing.T) {
	root := t.TempDir()
	writeExploreFixture(t, root)

	// Inject a 1ms soft deadline via the parent ctx — SoftDeadline will take
	// min(parent, 25s) = 1ms. By the time resolveRoot returns (local path,
	// ctx-agnostic) and buildExploreOutput enters explore.Run, the ctx is
	// already expired; ingest bails and the handler renders a partial result.
	deadlined, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()
	time.Sleep(2 * time.Millisecond) // burn the deadline

	deps := analyze.Deps{}
	res, err := handleExplore(deadlined, ExploreInput{Repo: root}, deps)
	if err != nil {
		t.Fatalf("handleExplore returned error on expired ctx (want partial result): %v", err)
	}
	if res == nil || res.IsError {
		t.Fatalf("want non-error partial result, got %+v", res)
	}
	got := textContentOf(t, res)
	if !strings.Contains(got, "partial: true") {
		t.Fatalf("partial result must contain 'partial: true', got:\n%s", got)
	}
	if !strings.Contains(got, "soft deadline") {
		t.Fatalf("partial footer must name the soft deadline, got:\n%s", got)
	}
}

// TestHandleExplore_FastPath_NoFooter verifies the fast path (deadline not
// reached) returns the full JSON result with NO partial footer — byte-identical
// to the pre-change output.
func TestHandleExplore_FastPath_NoFooter(t *testing.T) {
	root := t.TempDir()
	writeExploreFixture(t, root)

	deps := analyze.Deps{}
	res, err := handleExplore(context.Background(), ExploreInput{Repo: root}, deps)
	if err != nil {
		t.Fatalf("handleExplore: %v", err)
	}
	if res == nil || res.IsError {
		t.Fatalf("want non-error result, got %+v", res)
	}
	got := textContentOf(t, res)
	if strings.Contains(got, "partial: true") {
		t.Fatalf("fast path must NOT contain a partial footer, got:\n%s", got)
	}
	if !strings.Contains(got, `"file_count"`) {
		t.Fatalf("fast path must contain explore JSON fields, got:\n%s", got)
	}
}

// TestLouvainWeightedCtx_FastPath_IdenticalToNonCtx verifies the ctx-aware
// variant produces the same result as the original on a non-canceled ctx —
// the fast path is byte-identical.
func TestLouvainWeightedCtx_FastPath_IdenticalToNonCtx(t *testing.T) {
	graph := map[string]map[string]int{
		"a": {"b": 3, "c": 1},
		"b": {"a": 3, "c": 2},
		"c": {"a": 1, "b": 2, "d": 1},
		"d": {"c": 1, "e": 3},
		"e": {"d": 3, "f": 2},
		"f": {"e": 2},
	}
	gotCtx := ranking.LouvainWeightedCtx(context.Background(), graph)
	gotNonCtx := ranking.LouvainWeighted(graph)
	if len(gotCtx) != len(gotNonCtx) {
		t.Fatalf("ctx vs non-ctx result length mismatch: %d vs %d", len(gotCtx), len(gotNonCtx))
	}
	for k, v := range gotCtx {
		if gotNonCtx[k] != v {
			t.Fatalf("community mismatch for %q: ctx=%d non-ctx=%d", k, v, gotNonCtx[k])
		}
	}
}

// TestLouvainWeightedCtx_CanceledCtx_ReturnsNil verifies that a canceled ctx
// makes LouvainWeightedCtx bail promptly with nil instead of running all
// passes. RED-on-revert: remove the ctx.Err() checks and this either returns
// a non-nil result or takes far longer.
func TestLouvainWeightedCtx_CanceledCtx_ReturnsNil(t *testing.T) {
	graph := map[string]map[string]int{
		"a": {"b": 3, "c": 1},
		"b": {"a": 3, "c": 2},
		"c": {"a": 1, "b": 2, "d": 1},
		"d": {"c": 1, "e": 3},
		"e": {"d": 3, "f": 2},
		"f": {"e": 2},
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got := ranking.LouvainWeightedCtx(ctx, graph)
	if got != nil {
		t.Fatalf("canceled ctx must return nil, got %v", got)
	}
}

func writeExploreFixture(t *testing.T, root string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/demo\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n\nfunc main() {}\nfunc helper() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

package review

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestDeltaReview_AnalysisRootUsesWorktreeTree is the core #583 guard: when
// head points at a feature branch whose tree contains a symbol that does NOT
// exist on the current checkout, DeltaReview must build the call graph from
// AnalysisRoot (the worktree at head) so that symbol appears in
// changed_symbols. Before the fix, AnalysisRoot was ignored and the call
// graph was built from Root (the main checkout), so the feature-only symbol
// was missing from changed_symbols — impacted analysis reflected the wrong
// tree.
//
// Falsification: revert the AnalysisRoot wiring in DeltaReview (make it
// always use Root) and this test fails — FeatureOnly disappears from
// changed_symbols because the main checkout's call graph has no such symbol.
func TestDeltaReview_AnalysisRootUsesWorktreeTree(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test.com",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test.com")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s: %s", args, err, out)
		}
	}
	run("init")
	run("config", "user.email", "test@test.com")
	run("config", "user.name", "test")

	// main: a base file with a caller.
	if err := os.WriteFile(filepath.Join(dir, "main.go"),
		[]byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-m", "initial")

	// feature branch: add a feature-only file with a unique symbol.
	run("checkout", "-b", "feature")
	if err := os.WriteFile(filepath.Join(dir, "feature_only.go"),
		[]byte("package main\n\n// FeatureOnly exists only on the feature branch.\nfunc FeatureOnly() string {\n\treturn \"feature\"\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-m", "add feature-only symbol")
	// Return the warm checkout to main.
	run("checkout", "main")

	// Precondition: feature_only.go is NOT on the main checkout's tree.
	if _, err := os.Stat(filepath.Join(dir, "feature_only.go")); !os.IsNotExist(err) {
		t.Fatalf("precondition: main checkout must not have feature_only.go")
	}

	// Build an isolated worktree at the feature branch (mirror review_pr).
	wt, err := CreatePRWorktree(context.Background(), dir, "feature")
	if err != nil {
		t.Fatalf("CreatePRWorktree: %v", err)
	}
	defer wt.Cleanup()

	// The worktree must carry the feature-only file.
	if _, err := os.Stat(filepath.Join(wt.Path, "feature_only.go")); err != nil {
		t.Fatalf("worktree missing feature_only.go: %v", err)
	}

	// DeltaReview with Root=main checkout, AnalysisRoot=worktree, Head=feature.
	// The diff (base..feature) resolves refs against Root; the call graph is
	// built from AnalysisRoot so FeatureOnly is parsed and intersects the diff.
	result, err := DeltaReview(context.Background(), DeltaInput{
		Root:         dir,
		AnalysisRoot: wt.Path,
		Base:         "main",
		Head:         "feature",
		Depth:        2,
	})
	if err != nil {
		t.Fatalf("DeltaReview: %v", err)
	}

	found := false
	for _, cs := range result.ChangedSymbols {
		if cs.Symbol != nil && cs.Symbol.Name == "FeatureOnly" {
			found = true
			break
		}
	}
	if !found {
		var names []string
		for _, cs := range result.ChangedSymbols {
			if cs.Symbol != nil {
				names = append(names, cs.Symbol.Name)
			}
		}
		t.Fatalf("FeatureOnly missing from changed_symbols (got %v) — AnalysisRoot was not used for the call graph", strings.Join(names, ", "))
	}
}

// TestDeltaReview_AnalysisRootEmptyFallsBackToRoot verifies the fast path:
// when AnalysisRoot is empty, DeltaReview builds the call graph from Root
// (unchanged pre-#583 behaviour).
func TestDeltaReview_AnalysisRootEmptyFallsBackToRoot(t *testing.T) {
	t.Parallel()
	dir := setupGitRepoWithSymbols(t)
	result, err := DeltaReview(context.Background(), DeltaInput{
		Root:  dir,
		Base:  "HEAD~1",
		Depth: 3,
		// AnalysisRoot intentionally empty — must default to Root.
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ChangedFiles) == 0 {
		t.Error("expected changed files on the fast path")
	}
}

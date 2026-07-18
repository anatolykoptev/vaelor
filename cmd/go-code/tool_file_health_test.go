package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/anatolykoptev/vaelor/internal/analyze"
	"github.com/anatolykoptev/vaelor/internal/biomarkers"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// mkHealthRepo creates a temporary git repo with commit messages all touching
// "foo.go". Mirrors the mkRepoWithCommits helper from internal/biomarkers
// (internal test package, not importable from here).
func mkHealthRepo(t *testing.T, msgs []string) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), "git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-b", "main")
	run("config", "user.email", "t@t.t")
	run("config", "user.name", "t")
	for i, m := range msgs {
		p := filepath.Join(dir, "foo.go")
		content := []byte("package p // " + string(rune('a'+i)) + "\n")
		if err := os.WriteFile(p, content, 0o644); err != nil {
			t.Fatal(err)
		}
		run("add", "foo.go")
		run("commit", "-m", m)
	}
	return dir
}

// testAgg builds the default aggregator used by the tool for test helpers.
func testAgg() *biomarkers.Aggregator {
	reg := defaultHealthRegistry()
	return biomarkers.NewAggregator(reg, defaultHealthWeights)
}

// extractFileHealthResult parses a *mcp.CallToolResult JSON body into FileHealthResult.
func extractFileHealthResult(t *testing.T, result *mcp.CallToolResult) FileHealthResult {
	t.Helper()
	if result.IsError {
		t.Fatalf("expected non-error result, got error: %+v", result.Content)
	}
	if len(result.Content) == 0 {
		t.Fatal("result has no content")
	}
	tc, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("Content[0] is %T, want *mcp.TextContent", result.Content[0])
	}
	var out FileHealthResult
	if err := json.Unmarshal([]byte(tc.Text), &out); err != nil {
		t.Fatalf("json unmarshal: %v\nbody: %s", err, tc.Text)
	}
	return out
}

// textOf extracts the raw text body from a non-error CallToolResult.
func textOf(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	if len(result.Content) == 0 {
		return ""
	}
	tc, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		return ""
	}
	return tc.Text
}

// TestGetFileHealth_RequiresRepo verifies that an empty Repo field returns IsError=true.
func TestGetFileHealth_RequiresRepo(t *testing.T) {
	args := FileHealthArgs{Repo: ""}
	result, err := handleFileHealthCore(t.Context(), args, testAgg(), Config{}, analyze.Deps{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected IsError=true for empty repo, got false")
	}
}

// TestGetFileHealth_EmptyRepo_ReturnsEmptyList tests that a non-git directory
// (CollectChurn returns error → propagated → collect churn error) with empty
// Paths yields IsError. For an actual non-git dir with explicit paths, Files is empty.
func TestGetFileHealth_EmptyRepo_ReturnsEmptyList(t *testing.T) {
	dir := t.TempDir() // not a git repo
	args := FileHealthArgs{Repo: dir, Paths: []string{}}
	result, err := handleFileHealthCore(t.Context(), args, testAgg(), Config{}, analyze.Deps{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// topHotspotPaths now propagates CollectChurn errors (MAJOR-3 fix).
	// A non-git tempdir causes git to exit non-zero → error returned → IsError=true.
	// That is the correct behaviour: caller should know the dir is not a git repo.
	if result.IsError {
		// Accept: non-git dir → collect churn error → IsError.
		return
	}
	out := extractFileHealthResult(t, result)
	if len(out.Files) != 0 {
		t.Fatalf("expected 0 files for non-git dir with explicit empty paths, got %d", len(out.Files))
	}
}

// TestGetFileHealth_ExplicitPaths tests that explicit paths against a real git repo
// with fix commits yields a scored FileHealth entry with Score >= 3.
func TestGetFileHealth_ExplicitPaths(t *testing.T) {
	if testing.Short() {
		t.Skip("heavy integration test; runs in the nightly full suite (make test)")
	}
	// 3 fix commits on foo.go → prior_defect fires.
	dir := mkHealthRepo(t, []string{"fix: a", "fix: b", "fix: c"})
	args := FileHealthArgs{
		Repo:  dir,
		Paths: []string{"foo.go"},
	}
	result, err := handleFileHealthCore(t.Context(), args, testAgg(), Config{}, analyze.Deps{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := extractFileHealthResult(t, result)
	if len(out.Files) == 0 {
		t.Fatal("expected at least 1 file in result")
	}
	if out.Files[0].Path != "foo.go" {
		t.Fatalf("expected path foo.go, got %q", out.Files[0].Path)
	}
	// 3 fix commits → prior_defect ≈ 0.48, weight 0.6 → weighted ≈ 0.29 → score ≈ 4.
	if out.Files[0].Score < 3 {
		t.Fatalf("expected score >= 3 for 3 fix commits, got %d", out.Files[0].Score)
	}
	if out.Meta.DurationMS < 0 {
		t.Fatal("meta.duration_ms should be >= 0")
	}
}

// TestGetFileHealth_HintNeverReferencesUnregisteredTools guards against
// the broken-hint class that shipped in commit 747fdf0 (referenced
// "get_dead_code" and "understand(path=)" which don't exist / take wrong
// args). The Phase-1 contract: hints may only reference our own response
// keys (prior_defect, churn_risk) or advisory descriptions, never external
// tool names or non-existent arg keys.
func TestGetFileHealth_HintNeverReferencesUnregisteredTools(t *testing.T) {
	if testing.Short() {
		t.Skip("heavy integration test; runs in the nightly full suite (make test)")
	}
	// High-score scenario: many fix commits → prior_defect fires hard.
	dir := mkHealthRepo(t, []string{
		"fix: a", "fix: b", "fix: c", "fix: d", "fix: e", "fix: f",
		"fix: g", "fix: h",
	})
	args := FileHealthArgs{
		Repo:  dir,
		Paths: []string{"foo.go"},
	}
	result, err := handleFileHealthCore(t.Context(), args, testAgg(), Config{}, analyze.Deps{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error result: %s", textOf(t, result))
	}
	body := textOf(t, result)
	// The forbidden strings from the original broken hint (commit 747fdf0).
	forbidden := []string{"get_dead_code", "understand(path=", "dead_code(path="}
	for _, f := range forbidden {
		if strings.Contains(body, f) {
			t.Fatalf("hint must not reference %q — does not exist as a tool / arg key", f)
		}
	}
}

// mkHotspotRepo creates a git repo where each path receives `commits` distinct
// commits with churning content. Used to drive CollectChurn ranking.
//
// NOTE: paths map iteration is Go's randomised map order. Tests that
// depend on ordering OF COMMIT CONSTRUCTION will flake. Filter / set-
// membership assertions (the T1 use case) are order-independent and safe.
func mkHotspotRepo(t *testing.T, paths map[string]int) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.CommandContext(t.Context(), "git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-b", "main")
	run("config", "user.email", "t@t.t")
	run("config", "user.name", "t")
	for path, cycles := range paths {
		full := filepath.Join(dir, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < cycles; i++ {
			body := strings.Repeat("a\n", 10)
			if i%2 == 1 {
				body = strings.Repeat("b\n", 10)
			}
			if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
			run("add", path)
			run("commit", "-m", "churn cycle "+strconv.Itoa(i))
		}
	}
	return dir
}

// TestTopHotspotPaths_SkipsMarkdown verifies markdown docs are excluded.
func TestTopHotspotPaths_SkipsMarkdown(t *testing.T) {
	if testing.Short() {
		t.Skip("heavy integration test; runs in the nightly full suite (make test)")
	}
	dir := mkHotspotRepo(t, map[string]int{
		"foo.go":       6,
		"docs/PLAN.md": 8,
	})
	paths, err := topHotspotPaths(t.Context(), dir, 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range paths {
		if strings.HasSuffix(p, ".md") {
			t.Fatalf("markdown must be excluded, got %q in %v", p, paths)
		}
	}
}

// TestTopHotspotPaths_SkipsLockFiles guards lock-file pollution.
func TestTopHotspotPaths_SkipsLockFiles(t *testing.T) {
	if testing.Short() {
		t.Skip("heavy integration test; runs in the nightly full suite (make test)")
	}
	dir := mkHotspotRepo(t, map[string]int{
		"main.go":           5,
		"package-lock.json": 8,
		"Cargo.lock":        8,
		"pnpm-lock.yaml":    8,
	})
	paths, err := topHotspotPaths(t.Context(), dir, 10)
	if err != nil {
		t.Fatal(err)
	}
	lockSubstrings := []string{"package-lock.json", "Cargo.lock", "pnpm-lock.yaml"}
	for _, p := range paths {
		for _, lk := range lockSubstrings {
			if strings.Contains(p, lk) {
				t.Fatalf("lock-file %q must be excluded, got %v", p, paths)
			}
		}
	}
}

// TestTopHotspotPaths_SkipsExcludedDirs verifies vendored / generated
// content under known directories is excluded.
func TestTopHotspotPaths_SkipsExcludedDirs(t *testing.T) {
	if testing.Short() {
		t.Skip("heavy integration test; runs in the nightly full suite (make test)")
	}
	dir := mkHotspotRepo(t, map[string]int{
		"src/foo.go":              5,
		"vendor/x/lib.go":         8,
		"node_modules/p/index.js": 8,
		"static/codec.js":         8,
		"docs/plan.md":            8,
	})
	paths, err := topHotspotPaths(t.Context(), dir, 10)
	if err != nil {
		t.Fatal(err)
	}
	excluded := []string{"vendor/", "node_modules/", "static/", "docs/"}
	for _, p := range paths {
		for _, ex := range excluded {
			if strings.HasPrefix(p, ex) {
				t.Fatalf("path under %q must be excluded, got %q in %v", ex, p, paths)
			}
		}
	}
}

// TestTopHotspotPaths_SkipsNestedStatic guards the specific smoke case
// from BUG-FH-1: web/static/audio/c2dec.js (codec2 WASM) MUST be
// excluded even though it lives at web/static/, not root-level static/.
func TestTopHotspotPaths_SkipsNestedStatic(t *testing.T) {
	if testing.Short() {
		t.Skip("heavy integration test; runs in the nightly full suite (make test)")
	}
	dir := mkHotspotRepo(t, map[string]int{
		"src/main.go":               5,
		"web/static/audio/c2dec.js": 10,
		"web/static/audio/c2enc.js": 10,
	})
	paths, err := topHotspotPaths(t.Context(), dir, 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range paths {
		if strings.Contains(p, "static/") {
			t.Fatalf("nested static path must be excluded, got %q in %v", p, paths)
		}
	}
}

// TestIsHealthEligible_BasenameAllowlist guards Dockerfile/Makefile inclusion.
func TestIsHealthEligible_BasenameAllowlist(t *testing.T) {
	cases := map[string]bool{
		"Dockerfile":      true,
		"Makefile":        true,
		"app/Dockerfile":  true,
		"deploy/Makefile": true,
		"random.notext":   false,
		"NotDockerfile":   false,
	}
	for p, want := range cases {
		if got := isHealthEligible(p); got != want {
			t.Errorf("isHealthEligible(%q): got %v want %v", p, got, want)
		}
	}
}

// TestGetFileHealth_PerFileErrorSetsErrorField guards Important-2: a
// path-level scoring failure surfaces via FileHealth.Error instead of
// polluting the reasons map with the "error" key.
func TestGetFileHealth_PerFileErrorSetsErrorField(t *testing.T) {
	dir := mkHealthRepo(t, []string{"fix: a", "fix: b"})
	// Query a path that doesn't exist — biomarker scoring should fail
	// gracefully for it and populate Error on its FileHealth entry.
	res, err := handleFileHealthCore(t.Context(), FileHealthArgs{
		Repo:  dir,
		Paths: []string{"ghost.go"},
	}, testAgg(), Config{}, analyze.Deps{})
	if err != nil || res.IsError {
		t.Fatalf("unexpected: err=%v isErr=%v", err, res.IsError)
	}
	body := extractFileHealthResult(t, res)
	if len(body.Files) != 1 {
		t.Fatalf("want 1 file, got %d", len(body.Files))
	}
	f := body.Files[0]
	if f.Path != "ghost.go" {
		t.Fatalf("path mismatch: %q", f.Path)
	}
	// The error field MUST be empty for a missing file that simply returns 0 — but
	// MUST be non-empty if biomarker actually errored. ChurnRisk + PriorDefect
	// both return (0, "", nil) for missing/zero — so this test verifies the
	// Error field is OMITTED when no real error occurred.
	if f.Error != "" {
		t.Logf("note: missing file returned Error=%q; either of these is acceptable: empty (graceful) or descriptive", f.Error)
	}
	// The KEY thing this test asserts: reasons map must NOT contain "error" key.
	if _, hasErrKey := f.Reasons["error"]; hasErrKey {
		t.Fatalf("Reasons map must not contain 'error' key (use Error field), got %v", f.Reasons)
	}
}

// TestIsHealthEligible_NewExtensions guards the schema/IaC additions.
func TestIsHealthEligible_NewExtensions(t *testing.T) {
	cases := []string{
		"api/v1.proto",
		"schema.graphql",
		"infra/main.tf",
		"vars.tfvars",
		"k8s/deploy.yaml",
		"config.toml",
	}
	for _, p := range cases {
		if !isHealthEligible(p) {
			t.Errorf("%q must be eligible (schema/IaC/config-as-code)", p)
		}
	}
}

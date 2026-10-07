package callgraph

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestDiscoverGoRepos_FiltersNonGoDirs builds a tmp tree with three
// subdirs — only one carries go.mod — and asserts only that one is returned.
func TestDiscoverGoRepos_FiltersNonGoDirs(t *testing.T) {
	tmp := t.TempDir()

	goRepo := filepath.Join(tmp, "alpha")
	if err := os.MkdirAll(goRepo, 0o755); err != nil {
		t.Fatalf("mkdir goRepo: %v", err)
	}
	if err := os.WriteFile(filepath.Join(goRepo, "go.mod"), []byte("module alpha\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}

	// Non-Go subdir (no go.mod).
	if err := os.MkdirAll(filepath.Join(tmp, "beta"), 0o755); err != nil {
		t.Fatalf("mkdir beta: %v", err)
	}
	// Non-directory entry at top level should be skipped.
	if err := os.WriteFile(filepath.Join(tmp, "stray.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write stray.txt: %v", err)
	}

	got := discoverGoRepos([]string{tmp})
	if len(got) != 1 || got[0] != goRepo {
		t.Fatalf("discoverGoRepos = %v; want [%s]", got, goRepo)
	}
}

// TestDiscoverGoRepos_TrimsAndIgnoresEmpty verifies whitespace trimming and
// empty-entry skipping in the comma-split AUTO_INDEX_DIRS contract.
func TestDiscoverGoRepos_TrimsAndIgnoresEmpty(t *testing.T) {
	tmp := t.TempDir()
	repo := filepath.Join(tmp, "r")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repo, "go.mod"), []byte("module r\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	got := discoverGoRepos([]string{"  " + tmp + "  ", "", "/no/such/dir"})
	if len(got) != 1 || got[0] != repo {
		t.Fatalf("discoverGoRepos = %v; want [%s]", got, repo)
	}
}

// TestEagerWarmRepos_DispatchesPerRepo stubs the prewarm function and asserts
// it is invoked once per discovered Go repo, and that the started/completed
// counters move in lockstep when warmups succeed. It also verifies that the
// cap=2 parallelism limit is never exceeded.
func TestEagerWarmRepos_DispatchesPerRepo(t *testing.T) {
	tmp := t.TempDir()
	// Use 5 repos so the cap=2 semaphore is actually exercised.
	repoNames := []string{"a", "b", "c", "d", "e"}
	for _, name := range repoNames {
		dir := filepath.Join(tmp, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", name, err)
		}
		if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module "+name+"\n"), 0o644); err != nil {
			t.Fatalf("write go.mod %s: %v", name, err)
		}
	}

	// Add vendor/ to each repo so the dispatch selects -mod=vendor.
	for _, name := range repoNames {
		if err := os.MkdirAll(filepath.Join(tmp, name, "vendor"), 0o755); err != nil {
			t.Fatalf("mkdir vendor %s: %v", name, err)
		}
	}

	var (
		mu            sync.Mutex
		called        []string
		modFlags      []string
		count         atomic.Int64
		concurrent    atomic.Int32
		maxConcurrent atomic.Int32
	)
	orig := prewarmRepoFn
	prewarmRepoFn = func(_ context.Context, root, modFlag string) ([]string, error) {
		count.Add(1)
		mu.Lock()
		called = append(called, root)
		modFlags = append(modFlags, modFlag)
		mu.Unlock()

		// Track peak concurrency.
		cur := concurrent.Add(1)
		defer concurrent.Add(-1)
		for {
			m := maxConcurrent.Load()
			if cur <= m || maxConcurrent.CompareAndSwap(m, cur) {
				break
			}
		}

		time.Sleep(20 * time.Millisecond)
		return nil, nil
	}
	t.Cleanup(func() { prewarmRepoFn = orig })

	EagerWarmRepos(context.Background(), []string{tmp})

	if got := count.Load(); got != int64(len(repoNames)) {
		t.Fatalf("prewarmRepoFn calls = %d; want %d", got, len(repoNames))
	}
	sort.Strings(called)
	want := make([]string, len(repoNames))
	for i, name := range repoNames {
		want[i] = filepath.Join(tmp, name)
	}
	for i := range want {
		if called[i] != want[i] {
			t.Fatalf("called[%d]=%s; want %s", i, called[i], want[i])
		}
	}
	if got := maxConcurrent.Load(); got > eagerWarmParallelism {
		t.Fatalf("parallelism cap violated: max concurrent = %d, want <= %d", got, eagerWarmParallelism)
	}
	for _, mf := range modFlags {
		if mf != "-mod=vendor" {
			t.Fatalf("vendored repo got modFlag %q; want -mod=vendor", mf)
		}
	}
}

// TestEagerWarmRepos_ModFlagSelection verifies the per-repo -mod selection:
// a vendored repo is warmed with -mod=vendor, a non-vendored repo is ATTEMPTED
// with -mod=mod (module proxy) instead of being skipped — the redesign of
// issue #736 removed the skipped_no_vendor gate that left 23/39 repos cold.
func TestEagerWarmRepos_ModFlagSelection(t *testing.T) {
	tmp := t.TempDir()

	// Repo A: has vendor/ → -mod=vendor.
	repoA := filepath.Join(tmp, "with-vendor")
	if err := os.MkdirAll(filepath.Join(repoA, "vendor"), 0o755); err != nil {
		t.Fatalf("mkdir vendor: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoA, "go.mod"), []byte("module a\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}

	// Repo B: no vendor/ → attempted with -mod=mod (no longer skipped).
	repoB := filepath.Join(tmp, "no-vendor")
	if err := os.MkdirAll(repoB, 0o755); err != nil {
		t.Fatalf("mkdir repoB: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoB, "go.mod"), []byte("module b\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}

	var outcomes []string
	var mu sync.Mutex
	origRecord := recordEagerWarmFn
	recordEagerWarmFn = func(outcome string) {
		mu.Lock()
		outcomes = append(outcomes, outcome)
		mu.Unlock()
		eagerWarmTotal.WithLabelValues(outcome).Inc()
	}
	t.Cleanup(func() { recordEagerWarmFn = origRecord })

	var flagsMu sync.Mutex
	flags := map[string]string{}
	origWarm := prewarmRepoFn
	prewarmRepoFn = func(_ context.Context, root, modFlag string) ([]string, error) {
		flagsMu.Lock()
		flags[root] = modFlag
		flagsMu.Unlock()
		return nil, nil
	}
	t.Cleanup(func() { prewarmRepoFn = origWarm })

	EagerWarmRepos(context.Background(), []string{tmp})

	mu.Lock()
	got := append([]string(nil), outcomes...)
	mu.Unlock()

	// Both repos attempt: 2× started + 2× completed, no skipped/failed.
	if len(got) != 4 {
		t.Fatalf("expected 4 outcomes (2× started + 2× completed); got %v", got)
	}
	for _, o := range got {
		if o == "failed" || o == "skipped_no_vendor" {
			t.Fatalf("unexpected outcome %q; both repos must be attempted (issue #736)", o)
		}
	}
	flagsMu.Lock()
	defer flagsMu.Unlock()
	if flags[repoA] != "-mod=vendor" {
		t.Fatalf("vendored repo got %q; want -mod=vendor", flags[repoA])
	}
	if flags[repoB] != "-mod=mod" {
		t.Fatalf("non-vendored repo got %q; want -mod=mod (module proxy warm, issue #736)", flags[repoB])
	}
}

// TestEagerWarmRepos_PartialOutcome asserts that a prewarm returning errored
// packages (tolerated -e failures: cgo, broken deps) records "partial" — NOT
// "completed" — and emits a WARN naming the repo, so an operator can tell a
// silently-cold repo apart from a fully warm one (issue #736).
func TestEagerWarmRepos_PartialOutcome(t *testing.T) {
	tmp := t.TempDir()
	repo := filepath.Join(tmp, "cgo-repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repo, "go.mod"), []byte("module cgo\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}

	var outcomes []string
	var mu sync.Mutex
	origRecord := recordEagerWarmFn
	recordEagerWarmFn = func(outcome string) {
		mu.Lock()
		outcomes = append(outcomes, outcome)
		mu.Unlock()
		eagerWarmTotal.WithLabelValues(outcome).Inc()
	}
	t.Cleanup(func() { recordEagerWarmFn = origRecord })

	origWarm := prewarmRepoFn
	prewarmRepoFn = func(_ context.Context, _ string, _ string) ([]string, error) { return []string{"a", "b", "c"}, nil }
	t.Cleanup(func() { prewarmRepoFn = origWarm })

	buf := captureDefaultSlog(t)
	EagerWarmRepos(context.Background(), []string{tmp})

	mu.Lock()
	got := append([]string(nil), outcomes...)
	mu.Unlock()

	found := false
	for _, o := range got {
		if o == "partial" {
			found = true
		}
		if o == "completed" {
			t.Fatalf("partial warm recorded as completed; outcomes=%v", got)
		}
	}
	if !found {
		t.Fatalf("expected partial outcome for 3 errored packages; got %v", got)
	}
	if !strings.Contains(buf.String(), "level=WARN") || !strings.Contains(buf.String(), repo) {
		t.Fatalf("expected WARN naming the repo for a partial warm; log:\n%s", buf.String())
	}
}

// TestEagerWarmRepos_EmptyDirsNoOp asserts that the warm path is a no-op
// when AUTO_INDEX_DIRS is empty (the gating env var is empty) — no goroutine
// dispatched, no metric increment.
func TestEagerWarmRepos_EmptyDirsNoOp(t *testing.T) {
	var calls atomic.Int64
	orig := prewarmRepoFn
	prewarmRepoFn = func(_ context.Context, _ string, _ string) ([]string, error) {
		calls.Add(1)
		return nil, nil
	}
	t.Cleanup(func() { prewarmRepoFn = orig })

	EagerWarmRepos(context.Background(), nil)
	EagerWarmRepos(context.Background(), []string{"", "  "})

	if got := calls.Load(); got != 0 {
		t.Fatalf("prewarmRepoFn called %d times on empty dirs; want 0", got)
	}
}

// captureDefaultSlog redirects slog.Default to a buffer-backed handler for the
// duration of the test. The returned *bytes.Buffer accumulates all log lines.
// The original default logger is restored via t.Cleanup.
func captureDefaultSlog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	h := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	orig := slog.Default()
	slog.SetDefault(slog.New(h))
	t.Cleanup(func() { slog.SetDefault(orig) })
	return &buf
}

// TestRunGoListPrewarm_NoWarnLog asserts that runGoListPrewarm emits zero
// WARN-level log lines regardless of outcome: it is a pure executor — the
// EagerWarmRepos caller owns operator-visible logging.
func TestRunGoListPrewarm_NoWarnLog(t *testing.T) {
	buf := captureDefaultSlog(t)

	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "go.mod"), []byte("module example.com/test\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o600); err != nil {
		t.Fatalf("write main.go: %v", err)
	}
	// Real `go list` — returns errored-package count, must not log.
	_, _ = runGoListPrewarm(context.Background(), tmp, "-mod=mod")

	if got := buf.String(); strings.Contains(got, "level=WARN") {
		t.Fatalf("runGoListPrewarm emitted WARN; log output:\n%s", got)
	}
}

// TestRunGoListPrewarm_ToleratesBrokenDeps is the -e contract test: a module
// whose dependency cannot resolve must NOT fail the whole run — the other
// packages still get export data. Before -e this was a hard failure for the
// entire repo (issue #736: one cgo package froze vaelor's warm forever).
func TestRunGoListPrewarm_ToleratesBrokenDeps(t *testing.T) {
	tmp := t.TempDir()
	gomod := "module example.com/broken\n\ngo 1.22\n"
	if err := os.WriteFile(filepath.Join(tmp, "go.mod"), []byte(gomod), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	// A package importing a module that does not resolve offline →
	// per-package .Error with -e, tolerated; main package still exports.
	if err := os.WriteFile(filepath.Join(tmp, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o600); err != nil {
		t.Fatalf("write main.go: %v", err)
	}
	sub := filepath.Join(tmp, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatalf("mkdir sub: %v", err)
	}
	broken := "package sub\n\nimport _ \"example.com/definitely-missing-module/pkg\"\n"
	if err := os.WriteFile(filepath.Join(sub, "sub.go"), []byte(broken), 0o600); err != nil {
		t.Fatalf("write sub.go: %v", err)
	}

	errored, err := runGoListPrewarm(context.Background(), tmp, "-mod=mod")
	if err != nil {
		t.Fatalf("go list -e must not fail the run on a per-package error: %v", err)
	}
	if len(errored) == 0 {
		t.Fatal("expected >=1 errored package (the broken import); -e may be missing, turning per-package failures silent")
	}
}

// TestEagerWarmRepos_FailedWarmLogsWarn asserts the operator-visible contract
// of issue #736: a repo whose prewarm fails records "failed" AND logs WARN
// naming the repo — the previous Debug-level logging is exactly how a repo
// silently stayed cold across every deploy.
func TestEagerWarmRepos_FailedWarmLogsWarn(t *testing.T) {
	tmp := t.TempDir()
	repo := filepath.Join(tmp, "will-fail")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repo, "go.mod"), []byte("module f\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}

	var outcomes []string
	var mu sync.Mutex
	origRecord := recordEagerWarmFn
	recordEagerWarmFn = func(outcome string) {
		mu.Lock()
		outcomes = append(outcomes, outcome)
		mu.Unlock()
		eagerWarmTotal.WithLabelValues(outcome).Inc()
	}
	t.Cleanup(func() { recordEagerWarmFn = origRecord })

	origWarm := prewarmRepoFn
	prewarmRepoFn = func(_ context.Context, _ string, _ string) ([]string, error) {
		return nil, fmt.Errorf("go list exploded")
	}
	t.Cleanup(func() { prewarmRepoFn = origWarm })

	buf := captureDefaultSlog(t)
	EagerWarmRepos(context.Background(), []string{tmp})

	mu.Lock()
	got := append([]string(nil), outcomes...)
	mu.Unlock()

	failed := 0
	for _, o := range got {
		if o == "failed" {
			failed++
		}
	}
	if failed != 1 {
		t.Fatalf("expected 1 failed outcome; got %v", got)
	}
	if !strings.Contains(buf.String(), "level=WARN") || !strings.Contains(buf.String(), repo) {
		t.Fatalf("expected WARN naming the failing repo; log:\n%s", buf.String())
	}
}

// TestEagerWarmRepos_BrokenVendorSymlinkEmitsFailed asserts that a broken
// symlink at vendor/ is NOT treated the same as a missing directory. os.Stat
// follows symlinks; a dangling symlink returns an error that is NOT os.IsNotExist,
// so the EagerWarmRepos goroutine must record "failed" and emit a WARN so the
// operator can investigate — it must NOT warm with -mod=mod over a broken
// vendor.
func TestEagerWarmRepos_BrokenVendorSymlinkEmitsFailed(t *testing.T) {
	tmp := t.TempDir()
	repo := filepath.Join(tmp, "broken-vendor")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repo, "go.mod"), []byte("module example.com/broken\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	// Create a dangling symlink at vendor/ (points to a path that does not exist).
	// os.Stat follows symlinks, so this returns an error that is NOT os.IsNotExist.
	if err := os.Symlink(filepath.Join(repo, "nonexistent_target"), filepath.Join(repo, "vendor")); err != nil {
		t.Skipf("symlink creation failed (likely OS restriction): %v", err)
	}

	var outcomes []string
	var mu sync.Mutex
	origRecord := recordEagerWarmFn
	recordEagerWarmFn = func(outcome string) {
		mu.Lock()
		outcomes = append(outcomes, outcome)
		mu.Unlock()
		eagerWarmTotal.WithLabelValues(outcome).Inc()
	}
	t.Cleanup(func() { recordEagerWarmFn = origRecord })

	var calls atomic.Int64
	origWarm := prewarmRepoFn
	prewarmRepoFn = func(_ context.Context, _ string, modFlag string) ([]string, error) {
		calls.Add(1)
		if modFlag == "-mod=vendor" {
			t.Error("broken vendor symlink must NOT warm with -mod=vendor")
		}
		return nil, nil
	}
	t.Cleanup(func() { prewarmRepoFn = origWarm })

	buf := captureDefaultSlog(t)
	EagerWarmRepos(context.Background(), []string{tmp})

	mu.Lock()
	got := append([]string(nil), outcomes...)
	mu.Unlock()

	// Must record "failed" and never dispatch the prewarm.
	if calls.Load() != 0 {
		t.Fatalf("prewarm dispatched %d times on broken vendor symlink; want 0", calls.Load())
	}
	found := false
	for _, o := range got {
		if o == "failed" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected failed outcome for broken vendor symlink; got %v", got)
	}
	// Operator must be warned.
	if !strings.Contains(buf.String(), "level=WARN") {
		t.Fatalf("expected WARN log for broken vendor symlink; log:\n%s", buf.String())
	}
}

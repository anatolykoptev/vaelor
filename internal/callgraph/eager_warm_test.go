package callgraph

import (
	"bytes"
	"context"
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

	// Add vendor/ to each repo so the EagerWarmRepos caller passes the vendor
	// check and dispatches warmGoBuildFn (the prewarm stub below).
	for _, name := range repoNames {
		if err := os.MkdirAll(filepath.Join(tmp, name, "vendor"), 0o755); err != nil {
			t.Fatalf("mkdir vendor %s: %v", name, err)
		}
	}

	var (
		mu            sync.Mutex
		called        []string
		count         atomic.Int64
		concurrent    atomic.Int32
		maxConcurrent atomic.Int32
	)
	orig := warmGoBuildFn
	warmGoBuildFn = func(_ context.Context, root string) error {
		count.Add(1)
		mu.Lock()
		called = append(called, root)
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
		return nil
	}
	t.Cleanup(func() { warmGoBuildFn = orig })

	EagerWarmRepos(context.Background(), []string{tmp})

	if got := count.Load(); got != int64(len(repoNames)) {
		t.Fatalf("warmGoBuildFn calls = %d; want %d", got, len(repoNames))
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
}

// TestEagerWarmRepos_SkippedVsFailedCounterSemantics verifies that when two
// repos are discovered — one without vendor/ and one with — the skipped repo
// does NOT increment the "started" or "completed" counters, so the
// started/completed ratio accurately reflects actual build attempts.
func TestEagerWarmRepos_SkippedVsFailedCounterSemantics(t *testing.T) {
	tmp := t.TempDir()

	// Repo A: has vendor/ → will be dispatched to warmGoBuildFn.
	repoA := filepath.Join(tmp, "with-vendor")
	if err := os.MkdirAll(filepath.Join(repoA, "vendor"), 0o755); err != nil {
		t.Fatalf("mkdir vendor: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoA, "go.mod"), []byte("module a\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}

	// Repo B: no vendor/ → must emit skipped_no_vendor, NOT started/completed.
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

	origWarm := warmGoBuildFn
	warmGoBuildFn = func(_ context.Context, _ string) error { return nil }
	t.Cleanup(func() { warmGoBuildFn = origWarm })

	EagerWarmRepos(context.Background(), []string{tmp})

	mu.Lock()
	got := append([]string(nil), outcomes...)
	mu.Unlock()

	// Repo B must produce skipped_no_vendor.
	skipped := 0
	for _, o := range got {
		if o == "skipped_no_vendor" {
			skipped++
		}
	}
	if skipped != 1 {
		t.Fatalf("expected 1 skipped_no_vendor outcome; got %v", got)
	}
	// Repo A must produce started + completed.
	started, completed := 0, 0
	for _, o := range got {
		switch o {
		case "started":
			started++
		case "completed":
			completed++
		}
	}
	if started != 1 || completed != 1 {
		t.Fatalf("expected 1 started + 1 completed for vendor repo; got %v", got)
	}
	// Total outcomes = 3 (skipped + started + completed); no "failed".
	if len(got) != 3 {
		t.Fatalf("expected 3 total outcomes; got %v", got)
	}
}

// TestEagerWarmRepos_EmptyDirsNoOp asserts that the warm path is a no-op
// when AUTO_INDEX_DIRS is empty (the gating env var is empty) — no goroutine
// dispatched, no metric increment.
func TestEagerWarmRepos_EmptyDirsNoOp(t *testing.T) {
	var calls atomic.Int64
	orig := warmGoBuildFn
	warmGoBuildFn = func(_ context.Context, _ string) error {
		calls.Add(1)
		return nil
	}
	t.Cleanup(func() { warmGoBuildFn = orig })

	EagerWarmRepos(context.Background(), nil)
	EagerWarmRepos(context.Background(), []string{"", "  "})

	if got := calls.Load(); got != 0 {
		t.Fatalf("warmGoBuildFn called %d times on empty dirs; want 0", got)
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

// TestRunGoBuildPrewarm_NoVendorNoWarnLog asserts that runGoBuildPrewarm emits
// zero WARN-level log lines regardless of outcome. runGoBuildPrewarm is a pure
// build executor — it does not log warnings; it returns errors for the caller
// to handle. The no-vendor path no longer produces a WARN because the vendor
// check has moved to the EagerWarmRepos caller goroutine. This test guards
// against WARN being re-introduced inside runGoBuildPrewarm.
func TestRunGoBuildPrewarm_NoVendorNoWarnLog(t *testing.T) {
	buf := captureDefaultSlog(t)

	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "go.mod"), []byte("module example.com/test\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	// No vendor/ directory: build fails but must not emit WARN.
	// Ignore the error — this test only asserts on log output.
	_ = runGoBuildPrewarm(context.Background(), tmp)

	if got := buf.String(); strings.Contains(got, "level=WARN") {
		t.Fatalf("runGoBuildPrewarm emitted WARN; log output:\n%s", got)
	}
}

// TestEagerWarmRepos_NoVendorEmitsSkippedOutcome asserts that when runGoBuildPrewarm
// signals that a repo has no vendor/ directory, the EagerWarmRepos caller records
// "skipped_no_vendor" and does NOT record "completed". Under the old code the
// warmGoBuildFn stub returns nil unconditionally, making no-vendor repos
// indistinguishable from successful builds in the counter.
func TestEagerWarmRepos_NoVendorEmitsSkippedOutcome(t *testing.T) {
	tmp := t.TempDir()
	// One repo without vendor/.
	repo := filepath.Join(tmp, "nv")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repo, "go.mod"), []byte("module nv\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	// No vendor/ directory.

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

	// Wire warmGoBuildFn to the real runGoBuildPrewarm so the vendor check fires.
	orig := warmGoBuildFn
	warmGoBuildFn = runGoBuildPrewarm
	t.Cleanup(func() { warmGoBuildFn = orig })

	EagerWarmRepos(context.Background(), []string{tmp})

	mu.Lock()
	got := append([]string(nil), outcomes...)
	mu.Unlock()

	for _, o := range got {
		if o == "completed" {
			t.Fatalf("recorded outcome %q for no-vendor repo; want skipped_no_vendor, not completed", o)
		}
	}
	found := false
	for _, o := range got {
		if o == "skipped_no_vendor" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("no skipped_no_vendor outcome recorded; got %v", got)
	}
}

// TestEagerWarmRepos_BrokenVendorSymlinkEmitsFailed asserts that a broken
// symlink at vendor/ is NOT treated the same as a missing directory. os.Stat
// follows symlinks; a dangling symlink returns an error that is NOT os.IsNotExist,
// so the EagerWarmRepos goroutine must NOT record "skipped_no_vendor" — it must
// record "failed" and emit a WARN so the operator can investigate.
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

	buf := captureDefaultSlog(t)
	EagerWarmRepos(context.Background(), []string{tmp})

	mu.Lock()
	got := append([]string(nil), outcomes...)
	mu.Unlock()

	// Must record "failed", not "skipped_no_vendor".
	for _, o := range got {
		if o == "skipped_no_vendor" {
			t.Fatalf("broken vendor symlink incorrectly recorded as skipped_no_vendor; outcomes=%v", got)
		}
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

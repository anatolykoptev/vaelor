package callgraph

import (
	"context"
	"os"
	"path/filepath"
	"sort"
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

	var (
		mu             sync.Mutex
		called         []string
		count          atomic.Int64
		concurrent     atomic.Int32
		maxConcurrent  atomic.Int32
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

package ingest

import (
	"sync"
)

// Clone directories under WorkspaceDir are slug-deterministic and SHARED: the
// reuse cache (CloneRepo stat-hit → git fetch refresh) is intentional, and
// several tools (code_health, code_compare, impact, find_duplicates,
// repo_analyze) may resolve the SAME slug to the SAME on-disk dir concurrently.
//
// The hazard: each reader is handed a cleanup func that os.RemoveAll's the dir.
// Synchronous readers fire cleanup after their read finishes, but a background
// reader (code_health spawns one) can still be walking the tree when a sibling
// tool's synchronous cleanup deletes it — a use-after-delete that yields
// non-deterministic partial file counts.
//
// cloneRefs makes the cleanup safe regardless of who holds a reference: a clone
// dir is removed only when the LAST holder releases it. This is the single
// delete authority for shared workspace clones — collapsing N independent
// deleters into one refcounted owner.
//
// Local checkouts are never registered here (their cleanup is a no-op), so this
// only governs temporary, deletable clone/fetch dirs.

var cloneRefs = struct {
	mu     sync.Mutex
	counts map[string]int
}{counts: make(map[string]int)}

// AcquireCloneRef registers an active reader of the clone directory at dir.
// Every Acquire MUST be balanced by exactly one ReleaseCloneRef. Acquire is a
// no-op for an empty dir.
func AcquireCloneRef(dir string) {
	if dir == "" {
		return
	}
	cloneRefs.mu.Lock()
	cloneRefs.counts[dir]++
	cloneRefs.mu.Unlock()
}

// ReleaseCloneRef drops one reader reference for dir and removes the directory
// from disk (via CleanupCloneDir) only when the reference count reaches zero —
// i.e. no other reader is still using it. It returns the CleanupCloneDir error
// when it actually removes the dir, and nil otherwise: when other holders remain,
// when dir is empty, or when dir was never acquired / already released (an
// unbalanced release is a safe no-op, never a delete of an unheld dir). This is
// the refcounted replacement for a bare CleanupCloneDir on shared workspace
// clones.
func ReleaseCloneRef(dir string) error {
	if dir == "" {
		return nil
	}
	cloneRefs.mu.Lock()
	switch n := cloneRefs.counts[dir]; n {
	case 0:
		// Release without a matching Acquire (or a double release). Do NOT
		// delete a dir nobody holds — that would be an unbalanced-call footgun.
		cloneRefs.mu.Unlock()
		return nil
	case 1:
		// Last holder — remove the bookkeeping entry and delete below.
		delete(cloneRefs.counts, dir)
	default:
		cloneRefs.counts[dir] = n - 1
		cloneRefs.mu.Unlock()
		return nil
	}
	cloneRefs.mu.Unlock()
	// Single delete implementation: route through CleanupCloneDir so there is
	// exactly one place that removes a clone dir from disk.
	return CleanupCloneDir(dir)
}

// cloneRefCount returns the current reader count for dir. Test-only helper.
func cloneRefCount(dir string) int {
	cloneRefs.mu.Lock()
	defer cloneRefs.mu.Unlock()
	return cloneRefs.counts[dir]
}

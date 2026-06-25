package main

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// TestSpawnHealthBuild_CleanupAfterCompute proves the core ownership-transfer
// invariant that fixes the use-after-delete race: spawnHealthBuild must run the
// clone cleanup ONLY AFTER the background compute has finished reading the tree
// — never while it is still walking. Pre-fix, the handler's deferred cleanup
// fired the instant it returned the synchronous "computing" response, deleting
// the clone mid-walk.
func TestSpawnHealthBuild_CleanupAfterCompute(t *testing.T) {
	// Stand in for a resolved clone dir.
	clone := filepath.Join(t.TempDir(), "owner_repo")
	if err := os.MkdirAll(clone, 0o750); err != nil {
		t.Fatalf("mkdir clone: %v", err)
	}

	var (
		computeStarted   = make(chan struct{})
		releaseCompute   = make(chan struct{})
		dirAliveInWalk   atomic.Bool
		cleanupRan       atomic.Bool
		cleanupAfterWalk atomic.Bool
		computeDoneFirst atomic.Bool
	)

	cleanup := func() {
		// Cleanup must observe that compute has already completed.
		cleanupAfterWalk.Store(computeDoneFirst.Load())
		cleanupRan.Store(true)
		_ = os.RemoveAll(clone)
	}

	compute := func(_ context.Context) error {
		close(computeStarted)
		<-releaseCompute // hold the goroutine inside compute (simulating the walk)
		// While we are still "reading", the clone MUST still exist.
		if _, err := os.Stat(clone); err == nil {
			dirAliveInWalk.Store(true)
		}
		computeDoneFirst.Store(true)
		return nil
	}

	done := make(chan struct{})
	// Observe goroutine completion deterministically (fires after cleanup).
	prev := healthBuildDone
	t.Cleanup(func() { healthBuildDone = prev })
	healthBuildDone = func(string) { close(done) }

	spawnHealthBuild("owner/repo", cleanup, compute)

	// Compute is running — cleanup must NOT have fired yet.
	select {
	case <-computeStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("compute never started")
	}
	if cleanupRan.Load() {
		t.Fatal("cleanup ran while compute was still in progress (use-after-delete)")
	}
	if _, err := os.Stat(clone); err != nil {
		t.Fatalf("clone dir already gone before compute finished: %v", err)
	}

	// Let compute finish; the goroutine then runs cleanup, then healthBuildDone.
	close(releaseCompute)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("background build never completed")
	}

	if !dirAliveInWalk.Load() {
		t.Error("clone dir was missing during the compute walk")
	}
	if !cleanupRan.Load() {
		t.Error("cleanup never ran after the build finished")
	}
	if !cleanupAfterWalk.Load() {
		t.Error("cleanup ran but NOT after compute completed (ordering violation)")
	}
	if _, err := os.Stat(clone); !os.IsNotExist(err) {
		t.Errorf("clone dir not removed after build: stat err = %v", err)
	}
}

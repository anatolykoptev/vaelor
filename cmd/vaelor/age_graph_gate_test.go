package main

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/anatolykoptev/vaelor/internal/codegraph"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

// TestEnsureAgeGraphOrStatus_Cold_ReturnsBuildingAndStartsBackground proves that
// a non-fresh AGE graph short-circuits with a building status and spawns a
// background IndexRepo without blocking the caller.
//
// The test instruments every seam closure with a called flag and asserts each
// seam was actually invoked by the production code. This closes the
// synthetic-green gap where a future change could bypass the seam variables
// (e.g. call codegraph.CacheStatus directly) and the test would still pass
// because it only asserted on downstream side effects.
func TestEnsureAgeGraphOrStatus_Cold_ReturnsBuildingAndStartsBackground(t *testing.T) {
	origCacheStatus := ageGraphCacheStatus
	origIndexRepo := ageGraphIndexRepo
	origMemGuard := ageGraphMemGuardWatchdog
	defer func() {
		ageGraphCacheStatus = origCacheStatus
		ageGraphIndexRepo = origIndexRepo
		ageGraphMemGuardWatchdog = origMemGuard
	}()

	var cacheStatusCalled atomic.Bool
	ageGraphCacheStatus = func(context.Context, *codegraph.Store, string) (bool, error) {
		cacheStatusCalled.Store(true)
		return false, nil
	}
	indexStarted := make(chan struct{}, 1)
	indexDone := make(chan struct{})
	var indexRepoCalled atomic.Bool
	ageGraphIndexRepo = func(context.Context, *codegraph.Store, string, bool, codegraph.IndexConfig) (*codegraph.GraphMeta, error) {
		indexRepoCalled.Store(true)
		indexStarted <- struct{}{}
		<-indexDone
		return nil, nil
	}
	var memGuardCalled atomic.Bool
	ageGraphMemGuardWatchdog = func(context.Context, context.CancelFunc) {
		memGuardCalled.Store(true)
	}

	root := t.TempDir()
	repoKey := codegraph.GraphNameFor(root)
	defer buildingRepos.Delete(repoKey)

	var gotStatus, gotMessage string
	builder := func(status, message string) *mcp.CallToolResult {
		gotStatus = status
		gotMessage = message
		return textResult(fmt.Sprintf("status=%s message=%s", status, message))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	fresh, res := ensureAgeGraphOrStatus(ctx, "test_tool", nil, root, repoKey, false, codegraph.IndexConfig{}, builder)

	// cacheStatus is called synchronously — must be set by now.
	if !cacheStatusCalled.Load() {
		t.Error("ageGraphCacheStatus seam was NOT called: production code bypassed the cache-status seam")
	}

	if fresh {
		t.Fatalf("expected fresh=false for cold repo, got true")
	}
	if res == nil {
		t.Fatal("expected non-nil status result for cold repo")
	}
	if gotStatus != "building" {
		t.Errorf("expected status 'building', got %q", gotStatus)
	}
	if !strings.Contains(gotMessage, "retry") {
		t.Errorf("expected retry hint in message, got %q", gotMessage)
	}

	select {
	case <-indexStarted:
		// Background IndexRepo was launched as expected.
	case <-time.After(2 * time.Second):
		t.Fatal("background IndexRepo was not started")
	}

	// memGuard and indexRepo run inside the background goroutine — wait for
	// both flags to be set (memGuard is launched before indexRepo).
	if !waitForFlag(&memGuardCalled, 2*time.Second) {
		t.Error("ageGraphMemGuardWatchdog seam was NOT called: production code bypassed the mem-guard seam")
	}
	if !indexRepoCalled.Load() {
		t.Error("ageGraphIndexRepo seam was NOT called: production code bypassed the index-repo seam")
	}

	close(indexDone)
}

// TestEnsureAgeGraphOrStatus_Fresh_ReturnsNil proves that a fresh graph lets the
// caller continue synchronously.
func TestEnsureAgeGraphOrStatus_Fresh_ReturnsNil(t *testing.T) {
	origCacheStatus := ageGraphCacheStatus
	defer func() { ageGraphCacheStatus = origCacheStatus }()

	var cacheStatusCalled atomic.Bool
	ageGraphCacheStatus = func(context.Context, *codegraph.Store, string) (bool, error) {
		cacheStatusCalled.Store(true)
		return true, nil
	}

	root := t.TempDir()
	fresh, res := ensureAgeGraphOrStatus(context.Background(), "test_tool", nil, root, "key", false, codegraph.IndexConfig{}, nil)
	if !cacheStatusCalled.Load() {
		t.Error("ageGraphCacheStatus seam was NOT called: production code bypassed the cache-status seam")
	}
	if !fresh {
		t.Fatalf("expected fresh=true, got false")
	}
	if res != nil {
		t.Fatalf("expected nil status for fresh graph, got %v", res)
	}
}

// TestEnsureAgeGraphOrStatus_Cold_IncrementsColdReturnMetric proves the helper
// bumps gocode_tool_cold_return_total{tool,status} exactly once per cold return.
func TestEnsureAgeGraphOrStatus_Cold_IncrementsColdReturnMetric(t *testing.T) {
	origCacheStatus := ageGraphCacheStatus
	origIndexRepo := ageGraphIndexRepo
	origMemGuard := ageGraphMemGuardWatchdog
	defer func() {
		ageGraphCacheStatus = origCacheStatus
		ageGraphIndexRepo = origIndexRepo
		ageGraphMemGuardWatchdog = origMemGuard
	}()

	var cacheStatusCalled atomic.Bool
	ageGraphCacheStatus = func(context.Context, *codegraph.Store, string) (bool, error) {
		cacheStatusCalled.Store(true)
		return false, nil
	}
	var indexRepoCalled atomic.Bool
	ageGraphIndexRepo = func(context.Context, *codegraph.Store, string, bool, codegraph.IndexConfig) (*codegraph.GraphMeta, error) {
		indexRepoCalled.Store(true)
		return nil, nil
	}
	var memGuardCalled atomic.Bool
	ageGraphMemGuardWatchdog = func(context.Context, context.CancelFunc) {
		memGuardCalled.Store(true)
	}

	root := t.TempDir()
	repoKey := codegraph.GraphNameFor(root)
	defer buildingRepos.Delete(repoKey)

	label := toolColdReturnTotal.WithLabelValues("ensure_test", "building")
	before := testutil.ToFloat64(label)

	ensureAgeGraphOrStatus(context.Background(), "ensure_test", nil, root, repoKey, false, codegraph.IndexConfig{}, func(_, _ string) *mcp.CallToolResult {
		return textResult("building")
	})

	// cacheStatus is synchronous — must be set immediately.
	if !cacheStatusCalled.Load() {
		t.Error("ageGraphCacheStatus seam was NOT called: production code bypassed the cache-status seam")
	}

	after := testutil.ToFloat64(label)
	if after != before+1 {
		t.Errorf("cold_return metric: got %v, want %v", after, before+1)
	}

	// indexRepo and memGuard run in the background goroutine — wait for both.
	if !waitForFlag(&indexRepoCalled, 2*time.Second) {
		t.Error("ageGraphIndexRepo seam was NOT called: production code bypassed the index-repo seam")
	}
	if !waitForFlag(&memGuardCalled, 2*time.Second) {
		t.Error("ageGraphMemGuardWatchdog seam was NOT called: production code bypassed the mem-guard seam")
	}
}

// waitForFlag polls an atomic.Bool until it is true or the timeout elapses.
// Returns true if the flag was set, false on timeout.
func waitForFlag(flag *atomic.Bool, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if flag.Load() {
			return true
		}
		time.Sleep(time.Millisecond)
	}
	return flag.Load()
}

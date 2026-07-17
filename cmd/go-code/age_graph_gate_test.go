package main

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/anatolykoptev/go-code/internal/codegraph"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

// TestEnsureAgeGraphOrStatus_Cold_ReturnsBuildingAndStartsBackground proves that
// a non-fresh AGE graph short-circuits with a building status and spawns a
// background IndexRepo without blocking the caller.
func TestEnsureAgeGraphOrStatus_Cold_ReturnsBuildingAndStartsBackground(t *testing.T) {
	origCacheStatus := ageGraphCacheStatus
	origIndexRepo := ageGraphIndexRepo
	origMemGuard := ageGraphMemGuardWatchdog
	defer func() {
		ageGraphCacheStatus = origCacheStatus
		ageGraphIndexRepo = origIndexRepo
		ageGraphMemGuardWatchdog = origMemGuard
	}()

	ageGraphCacheStatus = func(context.Context, *codegraph.Store, string) (bool, error) { return false, nil }
	indexStarted := make(chan struct{}, 1)
	indexDone := make(chan struct{})
	ageGraphIndexRepo = func(context.Context, *codegraph.Store, string, bool, codegraph.IndexConfig) (*codegraph.GraphMeta, error) {
		indexStarted <- struct{}{}
		<-indexDone
		return nil, nil
	}
	ageGraphMemGuardWatchdog = func(context.Context, context.CancelFunc) {}

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
	close(indexDone)
}

// TestEnsureAgeGraphOrStatus_Fresh_ReturnsNil proves that a fresh graph lets the
// caller continue synchronously.
func TestEnsureAgeGraphOrStatus_Fresh_ReturnsNil(t *testing.T) {
	origCacheStatus := ageGraphCacheStatus
	defer func() { ageGraphCacheStatus = origCacheStatus }()

	ageGraphCacheStatus = func(context.Context, *codegraph.Store, string) (bool, error) { return true, nil }

	root := t.TempDir()
	fresh, res := ensureAgeGraphOrStatus(context.Background(), "test_tool", nil, root, "key", false, codegraph.IndexConfig{}, nil)
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

	ageGraphCacheStatus = func(context.Context, *codegraph.Store, string) (bool, error) { return false, nil }
	ageGraphIndexRepo = func(context.Context, *codegraph.Store, string, bool, codegraph.IndexConfig) (*codegraph.GraphMeta, error) {
		return nil, nil
	}
	ageGraphMemGuardWatchdog = func(context.Context, context.CancelFunc) {}

	root := t.TempDir()
	repoKey := codegraph.GraphNameFor(root)
	defer buildingRepos.Delete(repoKey)

	label := toolColdReturnTotal.WithLabelValues("ensure_test", "building")
	before := testutil.ToFloat64(label)

	ensureAgeGraphOrStatus(context.Background(), "ensure_test", nil, root, repoKey, false, codegraph.IndexConfig{}, func(_, _ string) *mcp.CallToolResult {
		return textResult("building")
	})

	after := testutil.ToFloat64(label)
	if after != before+1 {
		t.Errorf("cold_return metric: got %v, want %v", after, before+1)
	}
}

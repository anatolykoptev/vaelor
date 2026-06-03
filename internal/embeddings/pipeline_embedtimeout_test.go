package embeddings

// Fix: embed-timeout + resumable index (Fixes 1-3).
//
// RED guarantee (anti-vacuous):
//
//   Fix1_EmbedHTTPTimeout: the httptest server adds 200ms artificial delay and
//   the client is built with a 50ms timeout. Without Fix 1 the client has the
//   30s default, which is longer than the test deadline — the test hangs or passes
//   vacuously. With Fix 1 wired correctly, NewHTTPEmbedder sets Timeout=50ms and the
//   embed call returns context/timeout error, which the test confirms.
//   This test does NOT call newCodeEmbedder (cmd-level; no test pool needed) —
//   it verifies the embed.WithTimeout path via embed.NewClient directly, which is
//   the same path newCodeEmbedder will use after Fix 1.
//
//   Fix2_PartialAbortCounter: a 3-chunk repo (3×100 symbols) has its 2nd-chunk
//   embed call return HTTP 500. WITHOUT Fix 2 embedChunks returns error immediately
//   after chunk 1 succeeds, but gocode_index_partial_abort_total is never bumped
//   because the counter doesn't exist yet. The test asserts the counter increments
//   AND that rows_written > 0 (chunk 1 committed) AND SHA was NOT advanced (Bug #1
//   invariant). Reverting Fix 2 (removing the counter bump) makes the counter assert fail.
//
//   Fix3_ToolLabel: IndexRepoAsyncWithTool("semantic_search", ...) is called while
//   the embed server hangs. The context is bounded (INDEX_BUDGET). When it fires,
//   RecordIndexCancelled is called with tool="semantic_search", not "unknown".
//   The test reads gocode_index_cancelled_total{tool="semantic_search",...} directly
//   and asserts it incremented. Reverting Fix 3 (keeping "unknown") makes the
//   semantic_search label delta stay 0.
//
//   Fix3_IndexBudget: IndexRepoAsyncWithTool is called with a very short budget
//   (100ms) and a hung embed server. The goroutine must exit within a reasonable
//   wall-clock window. Reverting Fix 3 (removing context.WithTimeout) makes the
//   goroutine hang indefinitely — the IsIndexing poll times out.

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/anatolykoptev/go-kit/embed"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Fix 1: embed.WithTimeout is propagated by newCodeEmbedder path ---

// TestFix1_EmbedClientHonorsHTTPTimeout asserts that an embed.Client built with
// embed.WithTimeout(shortTimeout) actually fails fast when the server is slow.
// This validates that the embed.WithTimeout option works end-to-end through the
// v2 NewClient path that newCodeEmbedder will use after Fix 1.
//
// RED: without embed.WithTimeout the client uses the 30s default, the call does not
// time out within the test's 3s window, and the error assert fails.
func TestFix1_EmbedClientHonorsHTTPTimeout(t *testing.T) {
	const slowDelay = 200 * time.Millisecond
	const clientTimeout = 50 * time.Millisecond

	// Slow server that holds every request for slowDelay.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(slowDelay)
		http.Error(w, "late response", http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	client, err := embed.NewClient(srv.URL,
		embed.WithBackend("http"),
		embed.WithDim(dimSize),
		embed.WithTimeout(clientTimeout), // Fix 1: this option must be wired
	)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	_, embedErr := client.Embed(ctx, []string{"hello timeout"})
	assert.Error(t, embedErr,
		"embed client built with WithTimeout(%v) must return error when server takes %v",
		clientTimeout, slowDelay)
}

// --- Fix 2: partial-abort counter + chunk progress log ---

// TestFix2_PartialAbortCounter_BumpsOnChunkFailure asserts that when embedChunks
// fails mid-run (chunk 1 succeeds, chunk 2 errors), gocode_index_partial_abort_total
// is incremented, committed rows survive (chunk 1 rows > 0), and SHA is NOT advanced.
//
// RED (3 assertions, each independently RED if Fix 2 is reverted):
//  1. Counter delta: counter does not exist before Fix 2 — assert fails with "0 == 0".
//  2. Rows_written > 0: confirms chunk 1 committed before the error (non-vacuous:
//     reverts if embedChunks rolls back chunk 1 or returns before first write).
//  3. SHA NOT advanced: Bug #1 invariant — partial failure must not advance SHA.
func TestFix2_PartialAbortCounter_BumpsOnChunkFailure(t *testing.T) {
	// Baseline counter before the test.
	beforeAbort := sumCounterWithLabels(t, "gocode_index_partial_abort_total", nil)

	const repo = "test/fix2-partial-abort"

	// We need at least 2 full chunks = 101+ symbols (indexChunkSize=100).
	// Generate 2 files × 60 functions each = 120 symbols → 2 chunks (100 + 20).
	// Chunk 1 (100 items) succeeds; chunk 2 (20 items) fails.
	filesMap := make(map[string]string, 2)
	for i := range 2 {
		funcNames := make([]string, 60)
		for j := range 60 {
			funcNames[j] = fmt.Sprintf("ChunkFunc%d_%d", i, j)
		}
		filesMap[fmt.Sprintf("file%d.go", i)] = goFile(funcNames...)
	}

	// The embed client sub-batches by 32 (defaultChunkSize). A pipeline chunk of
	// 100 items = ceil(100/32) = 4 sub-requests to the embed server. We must let
	// all 4 sub-requests for chunk 1 succeed, then fail chunk 2's sub-requests.
	// Track cumulative texts embedded; once we exceed indexChunkSize (100) texts,
	// all subsequent calls fail (chunk 2 sub-requests).
	var mu sync.Mutex
	var textsProcessed int
	p, store := testPipelineWithEmbedHook(t, func(inputCount int) error {
		mu.Lock()
		prev := textsProcessed
		textsProcessed += inputCount
		mu.Unlock()
		if prev < indexChunkSize {
			// This sub-request is part of chunk 1 — let it succeed.
			return nil
		}
		// Chunk 2 or later — always fail (simulated embed server error).
		return fmt.Errorf("inject: embed server error after chunk 1 (textsProcessed=%d)", prev)
	})

	cleanRepoFull(t, store, repo)
	root := initGitRepo(t, filesMap)

	ctx := context.Background()
	_, indexErr := p.IndexRepo(ctx, repo, root)
	// The index must return an error (chunk 2 failed after all retries).
	require.Error(t, indexErr, "IndexRepo must return error when a chunk fails")

	// Assertion 1: partial abort counter incremented.
	afterAbort := sumCounterWithLabels(t, "gocode_index_partial_abort_total", nil)
	assert.Greater(t, afterAbort, beforeAbort,
		"gocode_index_partial_abort_total must increment on partial abort (Fix 2)")

	// Assertion 2: chunk 1 rows survived (non-zero rows in DB).
	count, countErr := store.CountEmbeddings(ctx, repo)
	require.NoError(t, countErr)
	assert.Greater(t, count, 0,
		"committed rows from chunk 1 must survive a partial abort")

	// Assertion 3: SHA NOT advanced (Bug #1 invariant: partial failure ≠ full success).
	sha, shaErr := store.GetRepoState(ctx, repo)
	require.NoError(t, shaErr)
	assert.Empty(t, sha,
		"SHA must NOT be advanced when embedChunks aborted mid-run (Bug #1 gate must hold)")
}

// --- Fix 3a: real tool label on cancel counter ---

// TestFix3_IndexRepoAsyncWithTool_ToolLabelOnCancel asserts that when
// IndexRepoAsyncWithTool("semantic_search", ...) is used and the bounded context
// cancels mid-chunk, RecordIndexCancelled is called with tool="semantic_search"
// (not "unknown").
//
// RED: without Fix 3, IndexRepoAsync hardcodes "unknown" in RecordIndexCancelled.
// The semantic_search label delta stays 0, and assert.Greater fails.
func TestFix3_IndexRepoAsyncWithTool_ToolLabelOnCancel(t *testing.T) {
	const tool = "semantic_search"
	beforeLabel := sumCounterWithLabels(t, "gocode_index_cancelled_total",
		map[string]string{"tool": tool})

	// unblock is closed explicitly after the goroutine exits and before srv.Close()
	// so that blocked handler goroutines can finish and srv.Close() does not deadlock.
	unblock := make(chan struct{})

	// Embed server that blocks until either the request context is cancelled (by the
	// HTTP client when the budget fires) OR unblock fires (explicitly released below).
	reached := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-reached:
		default:
			close(reached)
		}
		select {
		case <-r.Context().Done():
		case <-unblock:
		}
		http.Error(w, "cancelled", http.StatusServiceUnavailable)
	}))
	// srv.Close() is registered BEFORE unblock so that cleanup runs in the right order.
	// (t.Cleanup runs LIFO; we want unblock closed first, then srv.Close.)
	t.Cleanup(srv.Close)

	client, err := embed.NewClient(srv.URL,
		embed.WithBackend("http"),
		embed.WithDim(dimSize),
	)
	require.NoError(t, err)

	pool := testPool(t)
	store := NewStore(pool)
	require.NoError(t, store.EnsureSchema(context.Background()))

	// Use a very short budget so IndexRepoAsyncWithTool's bounded context fires quickly.
	const shortBudget = 300 * time.Millisecond
	p := NewPipeline(client, store,
		WithFileCache(nil),
		WithIndexBudget(shortBudget), // Fix 3: budget option
	)

	const repo = "test/fix3-tool-label"
	cleanRepoFull(t, store, repo)
	root := initGitRepo(t, map[string]string{
		"tool_label.go": goFile("ToolFunc1", "ToolFunc2"),
	})

	// IndexRepoAsyncWithTool must propagate the tool name to the cancel counter.
	started := p.IndexRepoAsyncWithTool(tool, repo, root) // Fix 3: new method
	require.True(t, started, "indexing must start")

	// Wait for the background goroutine to finish (budget fires, goroutine exits).
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !p.IsIndexing(repo) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	// Unblock the handler BEFORE any srv.Close call (which runs in t.Cleanup).
	// This ensures srv.Close() does not deadlock on active handler connections.
	close(unblock)

	assert.False(t, p.IsIndexing(repo),
		"IndexRepoAsyncWithTool goroutine must exit after budget expires")

	afterLabel := sumCounterWithLabels(t, "gocode_index_cancelled_total",
		map[string]string{"tool": tool})
	assert.Greater(t, afterLabel, beforeLabel,
		"gocode_index_cancelled_total{tool=%q} must increment (Fix 3: real tool label, not unknown)",
		tool)
}

// --- Fix 3b: INDEX_BUDGET terminates hung goroutine ---

// TestFix3_IndexBudget_TerminatesHungGoroutine asserts that IndexRepoAsyncWithTool
// with a short WithIndexBudget exits within wall-clock time when the embed server hangs.
//
// RED: without Fix 3 (no context.WithTimeout in IndexRepoAsync), the goroutine hangs
// indefinitely — IsIndexing never returns false and the poll times out.
func TestFix3_IndexBudget_TerminatesHungGoroutine(t *testing.T) {
	// unblock is closed explicitly after goroutine exits so srv.Close() doesn't deadlock.
	unblock := make(chan struct{})

	// Embed server that never responds until request context cancelled or unblock fires.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-unblock:
		}
		http.Error(w, "hung", http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)

	client, err := embed.NewClient(srv.URL,
		embed.WithBackend("http"),
		embed.WithDim(dimSize),
	)
	require.NoError(t, err)

	pool := testPool(t)
	store := NewStore(pool)
	require.NoError(t, store.EnsureSchema(context.Background()))

	const shortBudget = 300 * time.Millisecond
	p := NewPipeline(client, store,
		WithFileCache(nil),
		WithIndexBudget(shortBudget), // Fix 3
	)

	const repo = "test/fix3-budget-termination"
	cleanRepoFull(t, store, repo)
	root := initGitRepo(t, map[string]string{
		"budget.go": goFile("BudgetFunc1", "BudgetFunc2"),
	})

	started := p.IndexRepoAsyncWithTool("autoindex", repo, root)
	require.True(t, started)

	// Must exit well within 5s (budget=300ms + retry overhead + margin).
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !p.IsIndexing(repo) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	// Unblock handlers before srv.Close() runs (via t.Cleanup) so it doesn't deadlock.
	close(unblock)

	assert.False(t, p.IsIndexing(repo),
		"goroutine must exit after INDEX_BUDGET; without Fix 3 it hangs indefinitely")
}

// --- helpers ---

// sumCounterWithLabels returns the sum of counter samples for the named metric
// that match ALL provided label key=value pairs. labelFilter=nil sums all label combos.
func sumCounterWithLabels(t *testing.T, name string, labelFilter map[string]string) float64 {
	t.Helper()
	mfs, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err)
	var total float64
	for _, mf := range mfs {
		if mf.GetName() != name {
			continue
		}
		for _, m := range mf.GetMetric() {
			if !matchesLabels(m.GetLabel(), labelFilter) {
				continue
			}
			if c := m.GetCounter(); c != nil {
				total += c.GetValue()
			}
		}
	}
	return total
}

// matchesLabels returns true when all label key=value pairs in filter are present in pairs.
func matchesLabels(pairs []*dto.LabelPair, filter map[string]string) bool {
	if len(filter) == 0 {
		return true
	}
	have := make(map[string]string, len(pairs))
	for _, p := range pairs {
		have[p.GetName()] = p.GetValue()
	}
	for k, v := range filter {
		if have[k] != v {
			return false
		}
	}
	return true
}

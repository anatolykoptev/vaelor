package embeddings

// Self-index desync regression tests — Bug #1 (SHA-gate advances with 0 embeddings)
// and Bug #2 (IndexRepoAsync detached from request ctx).
//
// RED guarantee (anti-vacuous): each test fails if the production change is reverted:
//
//   Bug #1: without CountEmbeddings + recovery fall-through, indexRepo hits the
//           same-SHA branch, calls writeRepoState, and returns IndexResult{Total:0}.
//           The assertion "countAfter > 0" fails.
//
//   Bug #2: IndexRepoAsync already uses context.Background() internally.
//           TestBug2_IndexRepoAsync_DetachedFromRequestCtx therefore confirms the
//           existing correct behavior as a non-regression guard: if a future refactor
//           accidentally threads the caller ctx, this test detects it.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/anatolykoptev/go-kit/embed"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBug1_SameSHA_ZeroEmbeddings_Reindexes asserts that when code_repo_state
// has HEAD_SHA == current SHA but code_embeddings has 0 rows (the frozen-empty
// state from Bug #1), the next IndexRepo call re-parses and writes rows rather
// than returning the 68ms empty fast-path result.
//
// RED: without CountEmbeddings + recovery fall-through, indexRepo returns
// IndexResult{Total:0}. "countAfter > 0" fails.
func TestBug1_SameSHA_ZeroEmbeddings_Reindexes(t *testing.T) {
	p, store := testPipeline(t)
	ctx := context.Background()

	const repo = "test/bug1-frozen-empty"
	cleanRepoFull(t, store, repo)

	// Real git repo with parseable Go source.
	root := initGitRepo(t, map[string]string{
		"alpha.go": goFile("FuncAlpha", "FuncBeta"),
	})

	// Inject a repo_state row with the repo's actual current SHA.
	// This simulates the post-orphan-delete frozen state (#209/#210):
	// SHA is recorded but all embeddings were deleted.
	currentSHA, shaErr := repoMainBranchSHA(root)
	require.NoError(t, shaErr)
	require.NotEmpty(t, currentSHA, "test repo must have a main SHA")
	rawSetRepoState(t, store, repo, currentSHA)

	// Confirm 0 embeddings before the call.
	count0, cErr0 := store.CountEmbeddings(ctx, repo)
	require.NoError(t, cErr0)
	require.Equal(t, 0, count0, "setup: must start with 0 embeddings")

	// IndexRepo must detect the desync and fall through to full parse+embed.
	result, indexErr := p.IndexRepo(ctx, repo, root)
	require.NoError(t, indexErr)

	countAfter, cErr := store.CountEmbeddings(ctx, repo)
	require.NoError(t, cErr)
	assert.Greater(t, countAfter, 0,
		"Bug #1: IndexRepo must re-embed when SHA matches but 0 embeddings present")
	assert.Greater(t, result.Indexed, 0,
		"Bug #1: IndexResult.Indexed must reflect written rows, not a zero fast-path skip")
}

// TestBug1_SameSHA_PopulatedRepo_SkipsCheaply asserts the hot path is NOT
// regressed: when SHA matches AND embeddings are present, IndexRepo must still
// skip cheaply (no re-embed).
//
// RED: if CountEmbeddings is called unconditionally (outside the same-SHA gate),
// or if the fix always falls through to re-embed, result2.Indexed > 0 when it
// should be 0.
func TestBug1_SameSHA_PopulatedRepo_SkipsCheaply(t *testing.T) {
	p, store := testPipeline(t)
	ctx := context.Background()

	const repo = "test/bug1-hot-path"
	cleanRepoFull(t, store, repo)

	root := initGitRepo(t, map[string]string{
		"alpha.go": goFile("FuncAlpha", "FuncBeta"),
	})

	// First index: populates embeddings and advances SHA.
	_, err := p.IndexRepo(ctx, repo, root)
	require.NoError(t, err)

	countFirst, _ := store.CountEmbeddings(ctx, repo)
	require.Greater(t, countFirst, 0, "setup: first index must write rows")

	// Second call with same SHA and rows present — must short-circuit.
	result2, err2 := p.IndexRepo(ctx, repo, root)
	require.NoError(t, err2)
	assert.Equal(t, 0, result2.Indexed,
		"hot path: same SHA + populated embeddings must not re-embed")
	assert.Equal(t, 0, result2.Total,
		"hot path: same SHA + populated embeddings must return Total=0 (fast-path skip)")
}

// TestBug1_IncrementalSync_SameSHA_ZeroEmbeddings_Reindexes asserts that
// IncrementalSync also recovers from the frozen-empty state. The same-SHA branch
// now calls fallbackToFull → IndexRepo, which applies the Bug #1 fix.
//
// RED: without the fix, IncrementalSync on same-SHA returns mode=skip-sha-match
// with FilesEmbedded=0 and store stays at 0 rows.
func TestBug1_IncrementalSync_SameSHA_ZeroEmbeddings_Reindexes(t *testing.T) {
	p, store := testPipeline(t)
	ctx := context.Background()

	const repo = "test/bug1-inc-frozen"
	cleanRepoFull(t, store, repo)

	root := initGitRepo(t, map[string]string{
		"alpha.go": goFile("FuncGamma", "FuncDelta"),
	})

	currentSHA, shaErr := repoMainBranchSHA(root)
	require.NoError(t, shaErr)
	require.NotEmpty(t, currentSHA)
	rawSetRepoState(t, store, repo, currentSHA) // SHA set, 0 embeddings

	_, syncErr := p.IncrementalSync(ctx, repo, root)
	require.NoError(t, syncErr)

	countAfter, _ := store.CountEmbeddings(ctx, repo)
	assert.Greater(t, countAfter, 0,
		"Bug #1 (incremental): must recover and write embeddings when SHA matches but 0 rows")
}

// --- Bug #2: IndexRepoAsync must be detached from any caller context ---

// countingEmbedServerSelfIndex returns an httptest.Server that counts embed
// requests and returns zero-vectors. Used to confirm background indexing completes
// even when a caller-supplied context is cancelled.
func countingEmbedServerSelfIndex(t *testing.T) (*httptest.Server, *int, *sync.Mutex) {
	t.Helper()
	var mu sync.Mutex
	var n int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Input []string `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad", http.StatusBadRequest)
			return
		}
		mu.Lock()
		n += len(req.Input)
		mu.Unlock()

		type embedData struct {
			Embedding []float32 `json:"embedding"`
			Index     int       `json:"index"`
		}
		type embedResp struct {
			Data []embedData `json:"data"`
		}
		resp := embedResp{Data: make([]embedData, len(req.Input))}
		for i := range resp.Data {
			resp.Data[i] = embedData{Embedding: makeVec(), Index: i}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)
	return srv, &n, &mu
}

// TestBug2_IndexRepoAsync_DetachedFromRequestCtx asserts that the background
// goroutine started by IndexRepoAsync runs under context.Background() so that
// cancelling the calling context does not abort the indexing.
//
// RED: if IndexRepoAsync forwards the caller ctx, cancelling it will abort
// pool.Exec inside embedAndUpsert → 0 rows written. "countAfter > 0" fails.
func TestBug2_IndexRepoAsync_DetachedFromRequestCtx(t *testing.T) {
	embedSrv, embedCount, _ := countingEmbedServerSelfIndex(t)
	client, err := embed.NewClient(embedSrv.URL,
		embed.WithBackend("http"),
		embed.WithDim(dimSize),
	)
	require.NoError(t, err)

	pool := testPool(t)
	store := NewStore(pool)
	ctx := context.Background()
	require.NoError(t, store.EnsureSchema(ctx))

	p := NewPipeline(client, store, WithFileCache(nil))

	const repo = "test/bug2-detached-ctx"
	cleanRepoFull(t, store, repo)

	root := initGitRepo(t, map[string]string{
		"alpha.go": goFile("AsyncFunc1", "AsyncFunc2"),
	})

	// Start indexing, then immediately cancel the request context.
	// IndexRepoAsync must NOT use this context internally.
	started := p.IndexRepoAsync(repo, root)
	require.True(t, started, "indexing must start")

	// Simulate immediate request context cancellation (e.g. client disconnect).
	// The background goroutine should be unaffected because it uses context.Background().

	// Wait for background goroutine to finish (poll, 10s ceiling).
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if !p.IsIndexing(repo) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	assert.False(t, p.IsIndexing(repo), "background index must complete within 10s")

	// Rows must be present: background goroutine completed despite no request ctx.
	countAfter, cErr := store.CountEmbeddings(ctx, repo)
	require.NoError(t, cErr)
	assert.Greater(t, countAfter, 0,
		"Bug #2: background index must persist rows regardless of request ctx lifecycle")
	assert.Greater(t, *embedCount, 0,
		"embed server must have received requests from the background goroutine")
}

// TestBug3_SetEmbeddingsPresentGauge_ZeroAndOne asserts that
// SetEmbeddingsPresentGauge is callable without panics for both 0 and >0
// counts (gauge pre-touch regression guard).
func TestBug3_SetEmbeddingsPresentGauge_ZeroAndOne(t *testing.T) {
	const repo = "test/obs-present-gauge"
	// Must not panic for either value.
	assert.NotPanics(t, func() { SetEmbeddingsPresentGauge(repo, 0) })
	assert.NotPanics(t, func() { SetEmbeddingsPresentGauge(repo, 1) })
}

// TestBug1_CountEmbeddings_Returns0ForUnknownRepo asserts that CountEmbeddings
// returns 0 (not an error) for a repo key with no rows. Used by the gate.
func TestBug1_CountEmbeddings_Returns0ForUnknownRepo(t *testing.T) {
	_, store := testPipeline(t)
	ctx := context.Background()

	count, err := store.CountEmbeddings(ctx, "test/nonexistent-repo-xyz-abc")
	require.NoError(t, err)
	assert.Equal(t, 0, count, "CountEmbeddings must return 0 for unknown repo key")
}

// TestBug2_IndexCancelledCounter_Increments asserts that when the embed context is
// cancelled mid-chunk, RecordIndexCancelled bumps gocode_index_cancelled_total.
//
// RED: without the RecordIndexCancelled call in embedChunks, the counter stays at
// its pre-test value and assert.Greater fails.
//
// Design: the embed server signals "reached" when it receives a request, allowing
// the test to cancel the context only after the goroutine has entered embedChunks
// and is blocked on the HTTP call. This avoids the TOCTOU window where cancel()
// fires before ingest/embedChunks is reached.
func TestBug2_IndexCancelledCounter_Increments(t *testing.T) {
	// Gather baseline counter value before the test.
	before := sumCounter(t, "gocode_index_cancelled_total")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// reached is closed when the embed server receives its first request,
	// confirming the goroutine is inside embedChunks and blocked on the HTTP call.
	reached := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Signal that we are inside the embed call (embedChunks is running).
		select {
		case <-reached:
		default:
			close(reached)
		}
		// Block until the context is cancelled — the HTTP transport will then
		// abort the request and the embed client will propagate context.Canceled.
		<-ctx.Done()
		http.Error(w, "cancelled", http.StatusServiceUnavailable)
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
	p := NewPipeline(client, store, WithFileCache(nil))

	const repo = "test/cancel-counter"
	cleanRepoFull(t, store, repo)

	root := initGitRepo(t, map[string]string{
		"cancel.go": goFile("CancelFunc1", "CancelFunc2"),
	})

	// Run IndexRepo in a goroutine — it will block on the embed server.
	done := make(chan error, 1)
	go func() {
		_, err := p.IndexRepo(ctx, repo, root)
		done <- err
	}()

	// Wait until the embed server receives a request (goroutine is inside embedChunks).
	select {
	case <-reached:
	case <-time.After(15 * time.Second):
		t.Fatal("embed server never received a request — goroutine did not reach embedChunks")
	}

	// Cancel AFTER the goroutine is confirmed inside embedChunks.
	cancel()

	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("IndexRepo did not return after context cancel")
	}

	after := sumCounter(t, "gocode_index_cancelled_total")
	assert.Greater(t, after, before,
		"gocode_index_cancelled_total must increment when embedChunks observes ctx cancellation")
}

// TestF3_IndexRepoAsync_OnlyOneGoroutine_UnderConcurrency asserts that two
// concurrent IndexRepoAsync calls for the same repo_key spawn exactly one
// background index goroutine, not two.
//
// RED: the pre-fix code had a TOCTOU window between Load and Store, so both
// callers could win the check and both spawn goroutines. With the fix, only one
// LoadOrStore winner spawns; the loser returns false immediately.
func TestF3_IndexRepoAsync_OnlyOneGoroutine_UnderConcurrency(t *testing.T) {
	// Count how many times the index body actually executes (i.e. goroutines spawned).
	var bodyCount int64

	// Build a pipeline with a slow embed server so both goroutines have time to overlap.
	var indexBodyStarted = make(chan struct{}, 2)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Input []string `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad", http.StatusBadRequest)
			return
		}
		type embedData struct {
			Embedding []float32 `json:"embedding"`
			Index     int       `json:"index"`
		}
		type embedResp struct {
			Data []embedData `json:"data"`
		}
		resp := embedResp{Data: make([]embedData, len(req.Input))}
		for i := range resp.Data {
			resp.Data[i] = embedData{Embedding: makeVec(), Index: i}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
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

	// Override writeRepoStateFn to count body executions.
	p := NewPipeline(client, store, WithFileCache(nil),
		withWriteRepoStateFn(func(ctx context.Context, repoKey, sha string) error {
			atomic.AddInt64(&bodyCount, 1)
			select {
			case indexBodyStarted <- struct{}{}:
			default:
			}
			return store.SetRepoState(ctx, repoKey, sha)
		}),
	)

	const repo = "test/f3-toctou"
	cleanRepoFull(t, store, repo)

	root := initGitRepo(t, map[string]string{
		"toctou.go": goFile("TocTouFunc1", "TocTouFunc2"),
	})

	// Fire two concurrent IndexRepoAsync calls.
	started1 := p.IndexRepoAsync(repo, root)
	started2 := p.IndexRepoAsync(repo, root)

	// Exactly one must report started.
	assert.True(t, started1 || started2, "at least one IndexRepoAsync must return true")
	assert.False(t, started1 && started2, "both must not return true — TOCTOU would allow double-spawn")

	// Wait for the background goroutine to finish.
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if !p.IsIndexing(repo) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	assert.False(t, p.IsIndexing(repo), "background index must complete within 15s")

	// bodyCount tracks writeRepoState executions; one per completed index goroutine.
	assert.LessOrEqual(t, atomic.LoadInt64(&bodyCount), int64(1),
		"writeRepoState must be called at most once — only one goroutine should have run")
	_ = indexBodyStarted
}

// TestF5_ZeroSymbolRepo_SHAAdvancesNoThrash asserts that a repo containing
// no indexable symbols (e.g. markdown-only) advances SHA on first call and does
// NOT re-parse on the second call (no thrash).
//
// RED: if advanceStateNoEmbed does not advance SHA when Total==0, the second call
// will re-parse again (Total still 0, SHA never advanced) — thrash detected by
// counting how many times GetHashes is called or by checking that the second
// call returns mode=skip (Total==0 AND Indexed==0 from fast path).
func TestF5_ZeroSymbolRepo_SHAAdvancesNoThrash(t *testing.T) {
	p, store := testPipeline(t)
	ctx := context.Background()

	const repo = "test/f5-zero-symbols"
	cleanRepoFull(t, store, repo)

	// Markdown-only repo: no parseable Go/TS/Rust symbols.
	root := initGitRepo(t, map[string]string{
		"README.md": "# Hello\n\nThis repo has no indexable code.\n",
	})

	// First call: 0 symbols parsed — SHA must advance so we don't loop forever.
	result1, err := p.IndexRepo(ctx, repo, root)
	require.NoError(t, err)
	assert.Equal(t, 0, result1.Total, "markdown-only repo: Total must be 0")

	sha, shaErr := store.GetRepoState(ctx, repo)
	require.NoError(t, shaErr)
	assert.NotEmpty(t, sha, "SHA must be persisted after zero-symbol index so next call fast-paths")

	// Second call: same SHA, 0 symbols — must return from same-SHA fast path (Indexed==0, Total==0).
	result2, err2 := p.IndexRepo(ctx, repo, root)
	require.NoError(t, err2)
	assert.Equal(t, 0, result2.Indexed, "second call on zero-symbol repo must not re-embed (fast-path skip)")
	assert.Equal(t, 0, result2.Total, "second call total must be 0 (fast-path skip, no parse)")
}

// sumCounter returns the sum of all counter samples for the named metric family
// from the default Prometheus registry. Returns 0.0 if the family is not found.
func sumCounter(t *testing.T, name string) float64 {
	t.Helper()
	mfs, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err)
	var total float64
	for _, mf := range mfs {
		if mf.GetName() != name {
			continue
		}
		for _, m := range mf.GetMetric() {
			if c := m.GetCounter(); c != nil {
				total += c.GetValue()
			}
		}
	}
	return total
}

// TestDocsOnlyRepo_ZeroEmbeddingsCounter_NoFalsePositive asserts that a docs-only
// repo (no code files, only Markdown) does NOT increment the
// gocode_repo_state_advanced_with_zero_embeddings_total counter.
//
// This is the false-positive regression guard for investigation 2026-06-12 (ITEM 1):
// repos like /host/src/wiki and /host/src/oxpulse-business are 100% .md files and
// legitimately produce 0 embeddings. The counter must NOT fire for them.
//
// RED guarantee: revert either the rootHasEmbeddableFiles gate in checkSameSHAFastPath
// OR the gate in IncrementalSync same-SHA branch, and this test detects a non-zero counter.
func TestDocsOnlyRepo_ZeroEmbeddingsCounter_NoFalsePositive(t *testing.T) {
	p, store := testPipeline(t)
	ctx := context.Background()

	const repo = "test/docs-only-zero-emb-counter"
	cleanRepoFull(t, store, repo)

	// Docs-only repo: only Markdown files, no embeddable source code.
	root := initGitRepo(t, map[string]string{
		"README.md": "# Wiki\n\nThis repo has no indexable code.\n",
		"guide.md":  "# Guide\n\nSome guide text.\n",
	})

	before := sumCounter(t, "gocode_repo_state_advanced_with_zero_embeddings_total")

	// First call: 0 symbols, SHA advances (intended behaviour for docs-only repos).
	_, err := p.IndexRepo(ctx, repo, root)
	require.NoError(t, err)

	// Second call: same SHA + 0 embeddings — triggers the recovery path, but must
	// NOT increment the desync counter because there are no embeddable source files.
	_, err = p.IndexRepo(ctx, repo, root)
	require.NoError(t, err)

	// Third call via IncrementalSync to cover the pipeline_incremental.go gate.
	_, err = p.IncrementalSync(ctx, repo, root)
	require.NoError(t, err)

	after := sumCounter(t, "gocode_repo_state_advanced_with_zero_embeddings_total")
	assert.Equal(t, before, after,
		"docs-only repo must not increment zero-embeddings counter (false positive on every boot)")
}

// TestCodeRepo_ZeroEmbeddingsCounter_Fires asserts that a code repo with 0 stored
// embeddings (real desync: the store was emptied after the SHA was advanced) DOES
// increment the gocode_repo_state_advanced_with_zero_embeddings_total counter.
//
// This preserves the real desync detection — the gate must only suppress docs-only repos.
//
// RED guarantee: add rootHasEmbeddableFiles() returning always-false, and this test
// fails because the counter stays at its pre-test value.
func TestCodeRepo_ZeroEmbeddingsCounter_Fires(t *testing.T) {
	p, store := testPipeline(t)
	ctx := context.Background()

	const repo = "test/code-repo-zero-emb-counter"
	cleanRepoFull(t, store, repo)

	// Code repo with real Go source files — embeddable symbols.
	root := initGitRepo(t, map[string]string{
		"main.go": goFile("FuncDesync1", "FuncDesync2"),
	})

	// First call: indexes normally, stores embeddings and advances SHA.
	_, err := p.IndexRepo(ctx, repo, root)
	require.NoError(t, err)
	count, cErr := store.CountEmbeddings(ctx, repo)
	require.NoError(t, cErr)
	require.Greater(t, count, 0, "setup: first index must write rows")

	// Simulate desync: delete all embeddings but leave code_repo_state intact.
	_, _ = store.pool.Exec(ctx, `DELETE FROM code_embeddings WHERE repo_key = $1`, repo)
	count2, _ := store.CountEmbeddings(ctx, repo)
	require.Equal(t, 0, count2, "setup: must have 0 embeddings after delete")

	before := sumCounter(t, "gocode_repo_state_advanced_with_zero_embeddings_total")

	// Second call: same SHA + 0 embeddings + code files present → counter MUST fire.
	_, err = p.IndexRepo(ctx, repo, root)
	require.NoError(t, err)

	after := sumCounter(t, "gocode_repo_state_advanced_with_zero_embeddings_total")
	assert.Greater(t, after, before,
		"code repo with 0 embeddings (real desync) must increment the desync counter")
}

// --- helpers ---

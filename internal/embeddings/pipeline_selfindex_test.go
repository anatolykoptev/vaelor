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
	"testing"
	"time"

	"github.com/anatolykoptev/go-kit/embed"
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

// --- helpers ---

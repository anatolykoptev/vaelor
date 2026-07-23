package embeddings

// Single-flight regression tests for the sync IndexRepo path (#589).
//
// The residual TOCTOU: a synchronous IndexRepo previously bypassed the per-repoKey
// single-flight slot the async path (IndexRepoAsyncWithTool) uses, so two concurrent
// first-indexers for one repoKey could race. The loser's compensating DeleteRepo
// (WHERE repo_key=$1) wiped the winner's just-committed embeddings in the gap
// between the winner's embed-commit and its state-write → inverted orphan (state
// row, zero embeddings). The fix routes the sync path through the SAME claimIndexSlot
// so two indexers for one repoKey never run concurrently.
//
// All cases are DB-gated (skip without PR_TEST_DATABASE_URL; run in CI) — they need
// pgvector + a real Postgres store to exercise embedChunks → writeRepoState ordering.
//
// Falsification: revert IndexRepo to call indexRepoWithTool directly (bypassing
// claimIndexSlot) and the loser enters indexRepoWithTool concurrently → its
// compensating DeleteRepo fires (deleteCalls > 0) and wipes the winner's embeddings
// (CountEmbeddings == 0, inverted orphan) → assertions RED.

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/anatolykoptev/go-kit/embed"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSyncIndexRepo_SingleFlight_LoserDoesNotWipeWinner asserts that two
// concurrent sync IndexRepo calls for the same repoKey never run
// indexRepoWithTool concurrently: the loser returns immediately with an empty
// result (single-flight), so its compensating DeleteRepo never fires and the
// winner's embeddings survive — no inverted orphan.
//
// The loser is forced to fail writeRepoState (the trigger for compensate). With
// the fix the loser never reaches writeRepoState (it returns at the single-flight
// check), so deleteCalls stays 0 and the winner's rows survive.
//
// Falsifiable: revert IndexRepo to bypass claimIndexSlot → both goroutines enter
// indexRepoWithTool → the loser's writeRepoState fails → compensateFirstIndexOrphan
// fires (deleteCalls >= 1) and wipes the winner's committed embeddings →
// CountEmbeddings == 0 (inverted orphan) and deleteCalls == 0 assertion REDS.
//
// Determinism: the winner is launched first and blocks inside writeRepoState
// (AFTER embedChunks committed its rows) until the loser has returned. This
// guarantees the loser runs while the winner's embeddings are committed and its
// state row is not yet written — the exact inverted-orphan window. Without the
// fix the loser's compensate (RepoStateExists=false) deletes the committed rows;
// with the fix the loser never enters indexRepoWithTool.
func TestSyncIndexRepo_SingleFlight_LoserDoesNotWipeWinner(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	const repo = "test/sync-singleflight-inverted-orphan"
	store := testStore(t)
	cleanRepoFull(t, store, repo)

	srv := fakeEmbedServer(t)
	defer srv.Close()
	client, err := embed.NewClient(srv.URL,
		embed.WithBackend("http"),
		embed.WithDim(dimSize),
	)
	require.NoError(t, err)

	// Coordination: the winner blocks in writeRepoState (after committing
	// embeddings) until the loser has returned, forcing the loser to run inside
	// the inverted-orphan window.
	winnerStateEntered := make(chan struct{})
	winnerRelease := make(chan struct{})
	loserDone := make(chan struct{})

	var stateCalls int64
	writeFn := func(c context.Context, repoKey, sha, sourcePath string) error {
		n := atomic.AddInt64(&stateCalls, 1)
		if n == 1 {
			// Winner: signal that embeddings are committed and we hold the slot,
			// then block until the loser has returned (compensated or skipped).
			close(winnerStateEntered)
			select {
			case <-winnerRelease:
			case <-c.Done():
				return c.Err()
			}
			return store.SetRepoStateWithPath(c, repoKey, sha, "", sourcePath)
		}
		// Loser (n >= 2): fail to trigger compensateFirstIndexOrphan.
		return errors.New("injected loser writeRepoState failure")
	}

	var deleteCalls int64
	p := NewPipeline(client, store, "", WithFileCache(nil),
		withWriteRepoStateFn(writeFn),
		withDeleteRepoFn(func(c context.Context, rk string) error {
			atomic.AddInt64(&deleteCalls, 1)
			return store.DeleteRepo(c, rk)
		}),
	)

	root := initGitRepo(t, map[string]string{
		"src.go": goFile("SyncSFA", "SyncSFB"),
	})

	// Launch the winner first; it claims the slot and blocks in writeRepoState
	// after committing its embeddings.
	var wg sync.WaitGroup
	var winnerErr, loserErr error
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, winnerErr = p.IndexRepo(ctx, repo, root)
	}()
	<-winnerStateEntered // winner's embeddings are committed; it holds the slot.

	go func() {
		defer wg.Done()
		defer close(loserDone)
		_, loserErr = p.IndexRepo(ctx, repo, root)
	}()

	// Wait for the loser to return (with the fix it returns immediately at the
	// single-flight check; without the fix it runs through to compensate). Bound
	// the wait so a bug does not hang the test.
	select {
	case <-loserDone:
	case <-time.After(5 * time.Second):
	}
	// Release the winner so its writeRepoState completes and it writes the state row.
	close(winnerRelease)
	wg.Wait()

	// With the fix the loser returns an empty result + no error (single-flight
	// skip); without the fix it runs concurrently and surfaces the injected
	// writeRepoState failure.
	assert.NoError(t, loserErr, "loser must not error — it skipped at the single-flight check")

	// Winner must succeed and persist a state row.
	require.NoError(t, winnerErr, "winner's index must complete successfully")
	stored, gErr := store.GetRepoState(ctx, repo)
	require.NoError(t, gErr)
	assert.NotEqual(t, "", stored, "winner must persist a state row")

	// Core assertion: the winner's embeddings survive — no inverted orphan.
	count, cErr := store.CountEmbeddings(ctx, repo)
	require.NoError(t, cErr)
	assert.Greater(t, count, 0,
		"winner's embeddings must survive — no inverted orphan (#589); revert the single-flight and the loser's compensate wipes them")

	// The loser must NOT have compensated: single-flight prevents it from running
	// indexRepoWithTool at all. Revert the fix → deleteCalls >= 1.
	assert.Equal(t, int64(0), atomic.LoadInt64(&deleteCalls),
		"loser must not compensate — single-flight prevents concurrent index (revert fix → deleteCalls>0)")

	// Only the winner reached writeRepoState (stateCalls==1). Revert the fix and
	// the loser also reaches it (stateCalls >= 2, plus its retry).
	assert.Equal(t, int64(1), atomic.LoadInt64(&stateCalls),
		"only the winner must reach writeRepoState — the loser returns at the single-flight check")
}

// TestSyncIndexRepo_SingleFlight_LoserReturnsEmptyResult asserts the observable
// contract of the loser: it returns a non-nil empty IndexResult with no error
// (the concurrent winner is performing the index). This is the sync analog of the
// async path returning false when the slot is taken.
//
// Falsifiable: if the loser instead ran indexRepoWithTool concurrently, its
// result would carry Indexed>0 or an error from the injected writeRepoState
// failure — the empty-result + no-error assertion REDS.
func TestSyncIndexRepo_SingleFlight_LoserReturnsEmptyResult(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	const repo = "test/sync-singleflight-loser-empty"
	store := testStore(t)
	cleanRepoFull(t, store, repo)

	srv := fakeEmbedServer(t)
	defer srv.Close()
	client, err := embed.NewClient(srv.URL,
		embed.WithBackend("http"),
		embed.WithDim(dimSize),
	)
	require.NoError(t, err)

	// Winner blocks in writeRepoState until released, holding the slot open so
	// the loser is guaranteed to observe the slot as taken.
	winnerStateEntered := make(chan struct{})
	winnerRelease := make(chan struct{})
	var stateCalls int64
	writeFn := func(c context.Context, repoKey, sha, sourcePath string) error {
		if atomic.AddInt64(&stateCalls, 1) == 1 {
			close(winnerStateEntered)
			select {
			case <-winnerRelease:
			case <-c.Done():
				return c.Err()
			}
			return store.SetRepoStateWithPath(c, repoKey, sha, "", sourcePath)
		}
		return store.SetRepoStateWithPath(c, repoKey, sha, "", sourcePath)
	}
	p := NewPipeline(client, store, "", WithFileCache(nil),
		withWriteRepoStateFn(writeFn),
	)

	root := initGitRepo(t, map[string]string{
		"src.go": goFile("SyncSFLoserA"),
	})

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, _ = p.IndexRepo(ctx, repo, root)
	}()
	<-winnerStateEntered

	var loserResult *IndexResult
	var loserErr error
	go func() {
		defer wg.Done()
		loserResult, loserErr = p.IndexRepo(ctx, repo, root)
	}()
	// Give the loser a moment to return (it returns immediately with the fix).
	time.Sleep(200 * time.Millisecond)
	close(winnerRelease)
	wg.Wait()

	require.NotNil(t, loserResult, "loser must return a non-nil result")
	assert.NoError(t, loserErr, "loser must not surface an error — the winner is handling the repoKey")
	assert.Equal(t, 0, loserResult.Indexed,
		"loser must return an empty result — it did not run indexRepoWithTool (single-flight)")
	assert.Equal(t, 0, loserResult.Total,
		"loser must return Total=0 — it did not parse/embed (single-flight)")
}

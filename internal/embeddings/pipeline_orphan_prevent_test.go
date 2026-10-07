package embeddings

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/anatolykoptev/go-kit/embed"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// firstIndexOrphanTests exercises the retry+compensate fix that stops the
// dominant orphan source: on a FIRST index (no prior code_repo_state row), a
// writeRepoState failure (or embedChunks partial failure) previously left
// embeddings persisted with no state row → orphan. The fix retries
// writeRepoState once, then — on first index only — rolls back the just-written
// embeddings via DeleteRepo and returns the error so the caller retries.
//
// All cases are DB-gated (skip without PR_TEST_DATABASE_URL; run in CI).
// Assertions use call-counters on the injected deleteRepo seam and DB state
// (CountEmbeddings / GetRepoState), never log-string matching.
//
// Falsification: removing the compensate (deleteRepo call) makes the first-index
// cases RED — DeleteRepo call-count stays 0 and embeddings survive (orphan
// left). Removing the retry makes the transient case RED.

// seamedPipeline builds a Pipeline over the given store + embed server URL with
// injectable writeRepoState and a deleteRepo spy that counts calls AND delegates
// to the real store (so the rollback actually mutates DB state, letting tests
// assert no-orphan via CountEmbeddings). Returns the call counter.
func seamedPipeline(t *testing.T, store *Store, embedURL string, writeFn func(ctx context.Context, repoKey, sha, sourcePath string) error) (*Pipeline, *int64) {
	t.Helper()
	client, err := embed.NewClient(embedURL,
		embed.WithBackend("http"),
		embed.WithDim(dimSize),
	)
	require.NoError(t, err)

	var deleteCalls int64
	p := NewPipeline(client, store, "",
		WithFileCache(nil),
		withWriteRepoStateFn(writeFn),
		withDeleteRepoFn(func(ctx context.Context, repoKey string) error {
			atomic.AddInt64(&deleteCalls, 1)
			return store.DeleteRepo(ctx, repoKey)
		}),
	)
	return p, &deleteCalls
}

func deleteCalls(p *int64) int { return int(atomic.LoadInt64(p)) }

// TestFirstIndexOrphan_WriteRepoStateFailsTwice_Compensates: first index,
// writeRepoState fails on both the initial attempt and the retry. The fix must
// roll back the just-written embeddings (DeleteRepo called) and return an error
// so the caller retries. Net: no orphan — either state is written, or
// embeddings are rolled back.
//
// Falsifiable: remove the compensate (deleteRepo call) → deleteCalls==0 and
// CountEmbeddings>0 (orphan left) → both assertions RED.
func TestFirstIndexOrphan_WriteRepoStateFailsTwice_Compensates(t *testing.T) {
	ctx := context.Background()
	const repo = "test/orphan-prevent-fail-twice"
	store := testStore(t)
	cleanRepoFull(t, store, repo)

	srv := fakeEmbedServer(t)
	defer srv.Close()

	var writeCalls int64
	failAlways := func(_ context.Context, _, _, _ string) error {
		atomic.AddInt64(&writeCalls, 1)
		return errors.New("injected persistent write failure")
	}
	p, delCalls := seamedPipeline(t, store, srv.URL, failAlways)

	root := initGitRepo(t, map[string]string{
		"src.go": goFile("OrphanPreventA", "OrphanPreventB"),
	})

	before := readCounter(t, "gocode_orphan_prevented_total")

	_, err := p.IndexRepo(ctx, repo, root)

	require.Error(t, err, "first-index writeRepoState failure must propagate, not be swallowed")
	assert.Equal(t, int64(2), atomic.LoadInt64(&writeCalls),
		"writeRepoState must be retried exactly once (2 calls) before compensating")
	assert.Equal(t, 1, deleteCalls(delCalls),
		"compensating DeleteRepo must fire on first-index writeRepoState failure (revert the fix → 0)")

	count, cErr := store.CountEmbeddings(ctx, repo)
	require.NoError(t, cErr)
	assert.Equal(t, 0, count, "embeddings must be rolled back — no orphan rows left behind")

	stored, gErr := store.GetRepoState(ctx, repo)
	require.NoError(t, gErr)
	assert.Equal(t, "", stored, "no state row must be written on first-index failure")

	after := readCounter(t, "gocode_orphan_prevented_total")
	assert.Greater(t, after, before, "gocode_orphan_prevented_total must increment when compensate fires")
}

// TestFirstIndexOrphan_WriteRepoStateFailsOnceThenSucceeds_NoCompensate: first
// index, writeRepoState fails once then succeeds on the retry. The fix must NOT
// compensate (no delete), return no error, and persist the state row.
//
// Falsifiable: remove the retry → writeCalls==1, error returned, state row
// absent → assertions RED.
func TestFirstIndexOrphan_WriteRepoStateFailsOnceThenSucceeds_NoCompensate(t *testing.T) {
	ctx := context.Background()
	const repo = "test/orphan-prevent-fail-then-succeed"
	store := testStore(t)
	cleanRepoFull(t, store, repo)

	srv := fakeEmbedServer(t)
	defer srv.Close()

	var writeCalls int64
	failOnce := func(ctx context.Context, repoKey, sha, sourcePath string) error {
		n := atomic.AddInt64(&writeCalls, 1)
		if n == 1 {
			return errors.New("injected transient write failure")
		}
		return store.SetRepoStateWithPath(ctx, repoKey, sha, "", sourcePath)
	}
	p, delCalls := seamedPipeline(t, store, srv.URL, failOnce)

	root := initGitRepo(t, map[string]string{
		"src.go": goFile("RetrySucceedA"),
	})

	_, err := p.IndexRepo(ctx, repo, root)
	require.NoError(t, err, "transient write failure retried successfully must not surface an error")

	assert.Equal(t, int64(2), atomic.LoadInt64(&writeCalls),
		"writeRepoState must be called twice (fail + retry-success)")
	assert.Equal(t, 0, deleteCalls(delCalls),
		"compensating DeleteRepo must NOT fire when the retry succeeds")

	stored, gErr := store.GetRepoState(ctx, repo)
	require.NoError(t, gErr)
	assert.NotEqual(t, "", stored, "state row must be persisted after retry-success")

	count, cErr := store.CountEmbeddings(ctx, repo)
	require.NoError(t, cErr)
	assert.Greater(t, count, 0, "embeddings must survive when state is written")
}

// TestReindexOrphan_WriteRepoStateFails_NoCompensate: re-index (state row
// already exists), writeRepoState fails. Current behavior must be preserved —
// the swallowed failure leaves a stale row, NOT an orphan, so NO compensating
// delete and NO error returned.
//
// Falsifiable: if the fix wrongly compensates on re-index, deleteCalls>0 and
// embeddings are gone → assertions RED.
func TestReindexOrphan_WriteRepoStateFails_NoCompensate(t *testing.T) {
	ctx := context.Background()
	const repo = "test/orphan-prevent-reindex-no-compensate"
	store := testStore(t)
	cleanRepoFull(t, store, repo)

	srv := fakeEmbedServer(t)
	defer srv.Close()

	var writeCalls int64
	failAlways := func(_ context.Context, _, _, _ string) error {
		atomic.AddInt64(&writeCalls, 1)
		return errors.New("injected persistent write failure on reindex")
	}
	p, delCalls := seamedPipeline(t, store, srv.URL, failAlways)

	root := initGitRepo(t, map[string]string{
		"src.go": goFile("ReindexSym"),
	})

	// Seed a PRIOR state row with a DIFFERENT sha so checkSameSHAFastPath falls
	// through to the full index path (firstIndex=false) rather than skipping.
	rawSetRepoState(t, store, repo, "stale-sha-different-from-current")

	_, err := p.IndexRepo(ctx, repo, root)
	require.NoError(t, err,
		"re-index writeRepoState failure must NOT propagate (stale row is acceptable, not an orphan)")

	assert.Equal(t, int64(2), atomic.LoadInt64(&writeCalls),
		"writeRepoState retried once even on re-index (retry applies to both paths)")
	assert.Equal(t, 0, deleteCalls(delCalls),
		"compensating DeleteRepo must NOT fire on re-index (state row exists → stale, not orphan)")

	count, cErr := store.CountEmbeddings(ctx, repo)
	require.NoError(t, cErr)
	assert.Greater(t, count, 0, "re-index embeddings must survive a swallowed writeRepoState failure")
}

// TestFirstIndexOrphan_EmbedChunksPartialFail_Compensates: first index,
// embedChunks commits the first chunk then fails on the second. The partial
// embeddings persist with no state row → orphan. The fix must roll them back
// (DeleteRepo called) and return the error.
//
// Falsifiable: remove the compensate on the embedChunks path → deleteCalls==0
// and CountEmbeddings>0 (partial orphan left) → assertions RED.
func TestFirstIndexOrphan_EmbedChunksPartialFail_Compensates(t *testing.T) {
	ctx := context.Background()
	const repo = "test/orphan-prevent-embed-partial-fail"
	store := testStore(t)
	cleanRepoFull(t, store, repo)

	// > indexChunkSize (100) symbols so embedChunks spans 2 chunks; the hook
	// fails on the 2nd embed request so chunk 1 commits (rows written) and
	// chunk 2 aborts → partial embeddings with no state row = orphan.
	const symCount = indexChunkSize + 10
	names := make([]string, symCount)
	for i := range names {
		names[i] = fmt.Sprintf("PartialFailFunc%04d", i)
	}
	root := initGitRepo(t, map[string]string{
		"src.go": goFile(names...),
	})

	var embedReqs int64
	hook := func(inputCount int) error {
		if atomic.AddInt64(&embedReqs, 1) >= 2 {
			return errors.New("injected embed failure on second chunk")
		}
		return nil
	}
	srv := fakeEmbedServerWithHook(t, hook)
	defer srv.Close()

	// writeRepoState delegates to the real store (it should never be reached
	// because embedChunks fails first; if it were, success is the safe default).
	writeFn := func(ctx context.Context, repoKey, sha, sourcePath string) error {
		return store.SetRepoStateWithPath(ctx, repoKey, sha, "", sourcePath)
	}
	p, delCalls := seamedPipeline(t, store, srv.URL, writeFn)

	_, err := p.IndexRepo(ctx, repo, root)
	require.Error(t, err, "first-index embedChunks partial failure must propagate after compensate")

	assert.Equal(t, 1, deleteCalls(delCalls),
		"compensating DeleteRepo must fire on first-index embedChunks partial failure (revert the fix → 0)")

	count, cErr := store.CountEmbeddings(ctx, repo)
	require.NoError(t, cErr)
	assert.Equal(t, 0, count,
		"partial embeddings must be rolled back — no orphan rows left behind")

	stored, gErr := store.GetRepoState(ctx, repo)
	require.NoError(t, gErr)
	assert.Equal(t, "", stored, "no state row must be written when embedChunks fails on first index")
}

// TestCompensate_DeadParentCtx_StillRollsBack is the issue #739 fix test —
// DB-free (both store seams are injected). The compensation was previously
// invoked with the ALREADY-EXPIRED parent ctx, so RepoStateExists and
// deleteRepo failed on contact precisely when they were needed (a deadline
// mid-embed is the trigger case). The fix must run the probe + delete on a
// detached, short-budgeted ctx: injected fakes honor ctx.Err() like the real
// pgx store does, so a regression back to the inherited ctx fails the delete
// and yields orphanLeftInPlace → both assertions RED.
func TestCompensate_DeadParentCtx_StillRollsBack(t *testing.T) {
	var probed, deleted int64
	p := NewPipeline(nil, NewStore(nil), "",
		withRepoStateExistsFn(func(ctx context.Context, _ string) (bool, error) {
			atomic.AddInt64(&probed, 1)
			if err := ctx.Err(); err != nil {
				return false, err
			}
			return false, nil
		}),
		withDeleteRepoFn(func(ctx context.Context, _ string) error {
			atomic.AddInt64(&deleted, 1)
			return ctx.Err()
		}),
	)

	// The trigger case: parent ctx is already dead when compensate runs.
	dead, cancel := context.WithCancel(context.Background())
	cancel()

	got := p.compensateFirstIndexOrphan(dead, "r/739", "embedChunks")

	if got != orphanRolledBack {
		t.Errorf("outcome = %q, want %q — the compensation must run on its own budget even when the parent ctx is expired (issue #739)", got, orphanRolledBack)
	}
	if atomic.LoadInt64(&probed) == 0 || atomic.LoadInt64(&deleted) == 0 {
		t.Errorf("probe=%d delete=%d — both must fire on a detached live ctx", probed, deleted)
	}
}

// TestCompensate_DeleteFails_OutcomeSaysLeftInPlace pins the honest-outcome
// contract: when the rollback delete fails, the outcome is left-in-place, so
// the caller's error can no longer claim "rolled back" for a failure it did
// not observe (issue #739 part 2 — message/counter disagreement).
func TestCompensate_DeleteFails_OutcomeSaysLeftInPlace(t *testing.T) {
	p := NewPipeline(nil, NewStore(nil), "",
		withRepoStateExistsFn(func(context.Context, string) (bool, error) { return false, nil }),
		withDeleteRepoFn(func(context.Context, string) error { return errors.New("delete blew up") }),
	)
	if got := p.compensateFirstIndexOrphan(context.Background(), "r/739", "embedChunks"); got != orphanLeftInPlace {
		t.Errorf("outcome = %q, want %q — a failed delete must NOT report rolled back", got, orphanLeftInPlace)
	}
}

// TestCompensate_ConcurrentIndexer_Superseded pins the race-guard outcome:
// when the re-check sees a state row (a concurrent indexer won), the
// embeddings are kept and the outcome says so — no delete, no "rolled back".
func TestCompensate_ConcurrentIndexer_Superseded(t *testing.T) {
	var deleted int64
	p := NewPipeline(nil, NewStore(nil), "",
		withRepoStateExistsFn(func(context.Context, string) (bool, error) { return true, nil }),
		withDeleteRepoFn(func(context.Context, string) error {
			atomic.AddInt64(&deleted, 1)
			return nil
		}),
	)
	if got := p.compensateFirstIndexOrphan(context.Background(), "r/739", "embedChunks"); got != orphanSuperseded {
		t.Errorf("outcome = %q, want %q", got, orphanSuperseded)
	}
	if atomic.LoadInt64(&deleted) != 0 {
		t.Error("delete must NOT fire when a concurrent indexer committed the state row")
	}
}

// TestFirstIndexVerdict covers the fail-closed classification WITHOUT a DB —
// the dangerous branches (lookup error, empty-SHA re-index surfacing as a
// present row) that a compensating delete must never treat as a first index.
func TestFirstIndexVerdict(t *testing.T) {
	errBoom := errors.New("boom")
	cases := []struct {
		name       string
		prevErr    error
		stateExist bool
		existErr   error
		want       bool
	}{
		{"true first index: no row, no errors", nil, false, nil, true},
		{"re-index: row present", nil, true, nil, false},
		{"lookup error on GetRepoState → not first index", errBoom, false, nil, false},
		{"error on existence probe → not first index", nil, false, errBoom, false},
		{"present row wins even with nil errors", nil, true, nil, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := firstIndexVerdict(c.prevErr, c.stateExist, c.existErr); got != c.want {
				t.Fatalf("firstIndexVerdict(%v,%v,%v)=%v want %v",
					c.prevErr, c.stateExist, c.existErr, got, c.want)
			}
		})
	}
}

// TestCompensate_ProbeFails_NoDelete pins the fail-closed contract: when the
// state-existence PROBE itself errors, the rows may belong to a concurrent
// indexer's committed state — the delete must NOT fire and the outcome must
// say left-in-place, not rolled back (issue #739).
func TestCompensate_ProbeFails_NoDelete(t *testing.T) {
	var deleted int64
	p := NewPipeline(nil, NewStore(nil), "",
		withRepoStateExistsFn(func(context.Context, string) (bool, error) {
			return false, errors.New("probe exploded")
		}),
		withDeleteRepoFn(func(context.Context, string) error {
			atomic.AddInt64(&deleted, 1)
			return nil
		}),
	)
	if got := p.compensateFirstIndexOrphan(context.Background(), "r/739", "embedChunks"); got != orphanLeftInPlace {
		t.Errorf("outcome = %q, want %q", got, orphanLeftInPlace)
	}
	if atomic.LoadInt64(&deleted) != 0 {
		t.Error("delete must NOT fire when the existence probe cannot prove orphanhood")
	}
}

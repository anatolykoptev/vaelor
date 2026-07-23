package embeddings

// DB-gated tests for the code_repo_state ON DELETE CASCADE trigger (#588).
//
// A FK with ON DELETE CASCADE is deliberately NOT used: the embed-first write
// order (embedChunks commits code_embeddings BEFORE writeRepoState commits the
// state row) would make a FK reject the first-index INSERT. The trigger gives
// the cascade guarantee (state-row delete → embeddings delete) without enforcing
// INSERT. See cascadeDeleteFnSQL / ensureCascadeTrigger for the full rationale.
//
// All cases are DB-gated (skip without PR_TEST_DATABASE_URL; run in CI) — they
// need pgvector + a real Postgres store to exercise the trigger.
//
// Falsification: drop the ensureCascadeTrigger call (or the trigger) and the
// schema-drift test REDS (trigger absent after EnsureSchema) and the cascade
// test REDS (embeddings survive a state-row delete).

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// cascadeTriggerExists queries pg_trigger for the cascade trigger. Used by the
// schema-drift test to verify the trigger is present after EnsureSchema.
func cascadeTriggerExists(t *testing.T, store *Store) bool {
	t.Helper()
	var one int
	err := store.pool.QueryRow(context.Background(),
		`SELECT 1 FROM pg_trigger
		  WHERE tgrelid = 'public.code_repo_state'::regclass
		    AND tgname = 'trg_code_repo_state_cascade'
		    AND NOT tgisinternal`).Scan(&one)
	if err != nil {
		return false
	}
	return true
}

// dropCascadeTrigger removes the trigger + backing function for test setup so
// EnsureSchema's idempotent install path can be exercised from a clean state.
func dropCascadeTrigger(t *testing.T, store *Store) {
	t.Helper()
	ctx := context.Background()
	_, err := store.pool.Exec(ctx,
		`DROP TRIGGER IF EXISTS trg_code_repo_state_cascade ON public.code_repo_state`)
	require.NoError(t, err, "drop trigger for test setup")
	_, err = store.pool.Exec(ctx,
		`DROP FUNCTION IF EXISTS public.fn_cascade_delete_embeddings()`)
	require.NoError(t, err, "drop function for test setup")
}

// TestCascadeTrigger_InstalledByEnsureSchema is the schema-drift guard: after
// EnsureSchema the cascade trigger MUST exist. If the trigger is dropped (schema
// drift, a manual migration, or a partial rollback), a subsequent EnsureSchema
// MUST recreate it. Reds if ensureCascadeTrigger is removed or the catalog guard
// wrongly skips a missing trigger.
func TestCascadeTrigger_InstalledByEnsureSchema(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	// Force the cold path: drop the trigger + function, then clear the schemaDone
	// latch so EnsureSchema re-runs runEnsureSchema (which installs the trigger).
	dropCascadeTrigger(t, store)
	require.False(t, cascadeTriggerExists(t, store),
		"setup: trigger must be absent after explicit drop")
	store.schemaDone.Store(false)

	require.NoError(t, store.EnsureSchema(ctx),
		"EnsureSchema must succeed and reinstall the cascade trigger")
	assert.True(t, cascadeTriggerExists(t, store),
		"cascade trigger must exist after EnsureSchema (schema-drift guard; revert ensureCascadeTrigger → RED)")

	// Idempotency: a second EnsureSchema (fresh latch) must not error and must
	// leave the trigger present (catalog guard short-circuits, no double-create).
	store.schemaDone.Store(false)
	require.NoError(t, store.EnsureSchema(ctx))
	assert.True(t, cascadeTriggerExists(t, store),
		"cascade trigger must still exist after a second EnsureSchema (idempotent)")
}

// TestCascadeTrigger_StateDeleteCascadesEmbeddings asserts the trigger's runtime
// behavior: deleting a code_repo_state row cascades to its code_embeddings rows
// without an explicit DELETE on code_embeddings. This is the DB-level guarantee
// that a state-row delete never leaves orphaned embeddings behind.
//
// Falsifiable: drop the trigger (or ensureCascadeTrigger) and the state-row
// delete leaves embeddings behind → CountEmbeddings > 0 assertion REDS.
func TestCascadeTrigger_StateDeleteCascadesEmbeddings(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	const repo = "test/cascade-trigger-state-delete"

	// Seed: a state row + embeddings for the repo.
	require.NoError(t, store.SetRepoState(ctx, repo, "sha-cascade", ""))
	insertSymbols(t, store, repo, "cascade.go", []string{"CascadeA", "CascadeB"})

	count, cErr := store.CountEmbeddings(ctx, repo)
	require.NoError(t, cErr)
	require.Equal(t, 2, count, "setup: two embeddings must be present")

	// Delete ONLY the state row; the trigger must cascade to embeddings.
	_, err := store.pool.Exec(ctx,
		`DELETE FROM public.code_repo_state WHERE repo_key = $1`, repo)
	require.NoError(t, err, "delete state row")

	countAfter, cErrAfter := store.CountEmbeddings(ctx, repo)
	require.NoError(t, cErrAfter)
	assert.Equal(t, 0, countAfter,
		"embeddings must be cascaded by the state-row delete trigger (revert trigger → RED: embeddings survive)")

	// Cleanup: embeddings already gone via cascade; ensure no state row lingers.
	_, _ = store.pool.Exec(ctx, `DELETE FROM public.code_repo_state WHERE repo_key = $1`, repo)
}

// TestCascadeTrigger_WipeRepoStillCleansBoth asserts the trigger does NOT break
// WipeRepo, which deletes code_embeddings first then code_repo_state inside one
// transaction. The trigger fires on the state delete and issues a DELETE against
// code_embeddings that affects 0 rows (already deleted) — no double-delete, no
// conflict. Both tables must be empty after WipeRepo.
//
// Falsifiable: if the trigger conflicted with WipeRepo's explicit embeddings
// DELETE (e.g. raised on a row-level lock or double-delete), WipeRepo would
// error → the no-error assertion REDS.
func TestCascadeTrigger_WipeRepoStillCleansBoth(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	const repo = "test/cascade-trigger-wiperepo"

	require.NoError(t, store.SetRepoState(ctx, repo, "sha-wipe", ""))
	insertSymbols(t, store, repo, "wipe.go", []string{"WipeA", "WipeB"})

	count, cErr := store.CountEmbeddings(ctx, repo)
	require.NoError(t, cErr)
	require.Equal(t, 2, count, "setup: two embeddings present before WipeRepo")

	require.NoError(t, store.WipeRepo(ctx, repo),
		"WipeRepo must succeed with the cascade trigger installed (no double-delete conflict)")

	countAfter, cErrAfter := store.CountEmbeddings(ctx, repo)
	require.NoError(t, cErrAfter)
	assert.Equal(t, 0, countAfter, "embeddings must be gone after WipeRepo")

	stored, gErr := store.GetRepoState(ctx, repo)
	require.NoError(t, gErr)
	assert.Equal(t, "", stored, "state row must be gone after WipeRepo")
}

// TestCascadeTrigger_FirstIndexUnaffected asserts the trigger (which only fires
// on state-row DELETE) does NOT interfere with the embed-first write order: a
// first index inserts embeddings before the state row exists, then writes the
// state row. No INSERT enforcement means this succeeds. This is the reason a FK
// was rejected in favor of the trigger.
//
// Falsifiable: if a FK were used instead, the embedChunks INSERT would fail
// (no parent state row) → IndexRepo errors and CountEmbeddings == 0 → REDS.
func TestCascadeTrigger_FirstIndexUnaffected(t *testing.T) {
	p, store := testPipeline(t) // EnsureSchema runs → cascade trigger installed
	ctx := context.Background()
	const repo = "test/cascade-trigger-firstindex"
	cleanRepoFull(t, store, repo)

	root := initGitRepo(t, map[string]string{
		"src.go": goFile("CascadeFirstIdxA", "CascadeFirstIdxB"),
	})

	_, err := p.IndexRepo(ctx, repo, root)
	require.NoError(t, err,
		"first index must succeed with the cascade trigger installed (embed-first write order unaffected)")

	count, cErr := store.CountEmbeddings(ctx, repo)
	require.NoError(t, cErr)
	assert.Greater(t, count, 0, "first-index embeddings must be persisted (trigger must not block INSERT)")

	stored, gErr := store.GetRepoState(ctx, repo)
	require.NoError(t, gErr)
	assert.NotEqual(t, "", stored, "state row must be written after first index")
}

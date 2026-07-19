package embeddings

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWipeRepo_DeletesBothTables verifies that WipeRepo atomically deletes rows
// from BOTH code_embeddings and code_repo_state for the given repo_key. This is
// the critical-path invariant: a half-purged state must never be committed.
//
// Anti-tautology: the test inserts rows into both tables, calls WipeRepo, then
// verifies BOTH tables are empty for that repo_key. If WipeRepo only deleted
// code_embeddings (like DeleteRepo does), the code_repo_state assertion fails.
func TestWipeRepo_DeletesBothTables(t *testing.T) {
	if testing.Short() {
		t.Skip("requires postgres")
	}
	store := testStore(t)
	ctx := context.Background()
	const repoKey = "test/wipe-both-tables"
	cleanRepoFull(t, store, repoKey)

	// Bootstrap: insert a repo_state row and an embedding row.
	require.NoError(t, store.SetRepoState(ctx, repoKey, "sha1", "code-rank-embed"))
	insertSymbols(t, store, repoKey, "file.go", []string{"alpha"})

	// Sanity: both tables have rows for this repo.
	sha, err := store.GetRepoState(ctx, repoKey)
	require.NoError(t, err)
	require.Equal(t, "sha1", sha)

	syms, err := store.GetSymbolsForFile(ctx, repoKey, "file.go")
	require.NoError(t, err)
	require.Len(t, syms, 1)

	// Wipe.
	require.NoError(t, store.WipeRepo(ctx, repoKey))

	// Both tables must now be empty for this repo.
	sha2, err := store.GetRepoState(ctx, repoKey)
	require.NoError(t, err)
	assert.Empty(t, sha2, "WipeRepo must delete code_repo_state row")

	syms2, err := store.GetSymbolsForFile(ctx, repoKey, "file.go")
	require.NoError(t, err)
	assert.Empty(t, syms2, "WipeRepo must delete code_embeddings rows")
}

// TestWipeRepo_IdempotentOnMissingRepo verifies that WipeRepo does not error
// when the repo_key has no rows in either table. Deletion of zero rows is a
// valid no-op; the transaction still commits successfully.
func TestWipeRepo_IdempotentOnMissingRepo(t *testing.T) {
	if testing.Short() {
		t.Skip("requires postgres")
	}
	store := testStore(t)
	ctx := context.Background()
	const repoKey = "test/wipe-missing-repo"
	cleanRepoFull(t, store, repoKey)

	// No rows inserted — WipeRepo must succeed (zero rows deleted is fine).
	require.NoError(t, store.WipeRepo(ctx, repoKey))

	sha, err := store.GetRepoState(ctx, repoKey)
	require.NoError(t, err)
	assert.Empty(t, sha)
}

// TestWipeRepo_DoesNotAffectOtherRepos verifies that WipeRepo only deletes rows
// for the specified repo_key — other repos' data must remain intact. This
// guards against a WHERE-clause regression that could wipe the wrong rows.
func TestWipeRepo_DoesNotAffectOtherRepos(t *testing.T) {
	if testing.Short() {
		t.Skip("requires postgres")
	}
	store := testStore(t)
	ctx := context.Background()
	const target = "test/wipe-target"
	const other = "test/wipe-other"
	cleanRepoFull(t, store, target)
	cleanRepoFull(t, store, other)

	// Insert data for both repos.
	require.NoError(t, store.SetRepoState(ctx, target, "shaT", "code-rank-embed"))
	require.NoError(t, store.SetRepoState(ctx, other, "shaO", "code-rank-embed"))
	insertSymbols(t, store, target, "file.go", []string{"target_fn"})
	insertSymbols(t, store, other, "file.go", []string{"other_fn"})

	// Wipe only the target repo.
	require.NoError(t, store.WipeRepo(ctx, target))

	// Target: both tables empty.
	shaT, err := store.GetRepoState(ctx, target)
	require.NoError(t, err)
	assert.Empty(t, shaT, "target repo_state must be deleted")

	symsT, err := store.GetSymbolsForFile(ctx, target, "file.go")
	require.NoError(t, err)
	assert.Empty(t, symsT, "target embeddings must be deleted")

	// Other repo: both tables must still have their rows.
	shaO, err := store.GetRepoState(ctx, other)
	require.NoError(t, err)
	assert.Equal(t, "shaO", shaO, "other repo_state must survive")

	symsO, err := store.GetSymbolsForFile(ctx, other, "file.go")
	require.NoError(t, err)
	require.Len(t, symsO, 1, "other embeddings must survive")
	assert.Equal(t, "other_fn", symsO[0].SymbolName)
}

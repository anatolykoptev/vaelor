package embeddings

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestInvalidateRepoIfModelChanged_PurgesOnMismatch: when stored embed_model
// differs from active model, all embeddings for that repo are purged and
// GetRepoState returns "" (no row). Guards the model-fingerprint invariant:
// a model swap must never result in mixed-space vectors in code_embeddings.
//
// Anti-tautology: test would pass vacuously if InvalidateRepoIfModelChanged
// was a no-op. Reverts are caught because GetRepoState returns "" only when
// the code_repo_state row is deleted.
func TestInvalidateRepoIfModelChanged_PurgesOnMismatch(t *testing.T) {
	if testing.Short() {
		t.Skip("requires postgres")
	}
	store := testStore(t)
	ctx := context.Background()
	const repoKey = "test/invalidate-model-mismatch"
	cleanRepoFull(t, store, repoKey)

	// Bootstrap: index with jina-code-v2.
	require.NoError(t, store.SetRepoState(ctx, repoKey, "sha1", "jina-code-v2"))

	// Sanity: repo state exists.
	sha, err := store.GetRepoState(ctx, repoKey)
	require.NoError(t, err)
	require.Equal(t, "sha1", sha)

	// Simulate model switch to code-rank-embed.
	purged, err := store.InvalidateRepoIfModelChanged(ctx, repoKey, "code-rank-embed")
	require.NoError(t, err)
	assert.True(t, purged, "mismatch (jina→coderank) must trigger a purge")

	// After purge: GetRepoState must return "" (no row) so next IncrementalSync
	// runs a full bootstrap instead of a SHA-skipping no-op.
	sha2, err := store.GetRepoState(ctx, repoKey)
	require.NoError(t, err)
	assert.Empty(t, sha2, "purge must delete code_repo_state row so next index is a full bootstrap")
}

// TestInvalidateRepoIfModelChanged_NoOpOnMatch: when stored embed_model equals
// the active model, no purge should happen. Guards the short-circuit path.
func TestInvalidateRepoIfModelChanged_NoOpOnMatch(t *testing.T) {
	if testing.Short() {
		t.Skip("requires postgres")
	}
	store := testStore(t)
	ctx := context.Background()
	const repoKey = "test/invalidate-model-match"
	cleanRepoFull(t, store, repoKey)

	require.NoError(t, store.SetRepoState(ctx, repoKey, "sha99", "code-rank-embed"))

	purged, err := store.InvalidateRepoIfModelChanged(ctx, repoKey, "code-rank-embed")
	require.NoError(t, err)
	assert.False(t, purged, "same model must not trigger a purge")

	// State row must still exist with original SHA.
	sha, err := store.GetRepoState(ctx, repoKey)
	require.NoError(t, err)
	assert.Equal(t, "sha99", sha, "GetRepoState must return original SHA when no purge occurred")
}

// TestInvalidateRepoIfModelChanged_NoOpOnMissingRow: no prior index → no row
// to purge → must return (false, nil) gracefully.
func TestInvalidateRepoIfModelChanged_NoOpOnMissingRow(t *testing.T) {
	if testing.Short() {
		t.Skip("requires postgres")
	}
	store := testStore(t)
	ctx := context.Background()
	const repoKey = "test/invalidate-no-row"
	cleanRepoFull(t, store, repoKey)

	purged, err := store.InvalidateRepoIfModelChanged(ctx, repoKey, "code-rank-embed")
	require.NoError(t, err)
	assert.False(t, purged, "missing row must not trigger a purge and must not error")
}

// TestSetRepoState_StoresModel: SetRepoState must persist the embed_model
// alongside head_sha. This guards the fingerprint storage path — if model is
// not stored, InvalidateRepoIfModelChanged will always see "" and skip purges.
func TestSetRepoState_StoresModel(t *testing.T) {
	if testing.Short() {
		t.Skip("requires postgres")
	}
	store := testStore(t)
	ctx := context.Background()
	const repoKey = "test/set-repo-state-stores-model"
	cleanRepoFull(t, store, repoKey)

	require.NoError(t, store.SetRepoState(ctx, repoKey, "sha42", "code-rank-embed"))

	// Verify via direct query that the model was stored.
	var storedModel string
	err := store.pool.QueryRow(ctx,
		`SELECT embed_model FROM public.code_repo_state WHERE repo_key = $1`, repoKey).
		Scan(&storedModel)
	require.NoError(t, err)
	assert.Equal(t, "code-rank-embed", storedModel,
		"SetRepoState must write embed_model to code_repo_state")
}

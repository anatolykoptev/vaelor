package embeddings

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestListRepoKeys_ReturnsKnownRepos is the regression guard for the
// gocode_repo_state_advanced_with_zero_embeddings_total boot-warm fix
// (2026-07-01 metrics audit): cmd/go-code register.go calls ListRepoKeys at
// boot to pre-touch the counter for every repo already on record, so
// increase() has something to subtract from on a repo's first desync after a
// restart. ListRepoKeys must return every repo_key present in
// code_repo_state, not just the most recently indexed one.
//
// RED before the fix: ListRepoKeys does not exist (compile error) / an
// incomplete implementation that only returns one row fails the
// "both keys present" assertion.
func TestListRepoKeys_ReturnsKnownRepos(t *testing.T) {
	_, store := testPipeline(t)
	ctx := context.Background()

	const repoA = "test/list-repo-keys-a"
	const repoB = "test/list-repo-keys-b"
	cleanRepoFull(t, store, repoA)
	cleanRepoFull(t, store, repoB)

	rawSetRepoState(t, store, repoA, "sha-a")
	rawSetRepoState(t, store, repoB, "sha-b")

	keys, err := store.ListRepoKeys(ctx)
	require.NoError(t, err)

	assert.Contains(t, keys, repoA, "ListRepoKeys must include repos registered via SetRepoState")
	assert.Contains(t, keys, repoB, "ListRepoKeys must include repos registered via SetRepoState")
}

// TestListRepoKeys_ExcludesDeletedRepo asserts that a repo removed via
// cleanRepoFull's DELETE (mirroring DeleteRepo's code_repo_state cleanup)
// is not returned — ListRepoKeys reflects the CURRENT registry, not a
// historical superset that would pre-touch stale label combinations forever.
func TestListRepoKeys_ExcludesDeletedRepo(t *testing.T) {
	_, store := testPipeline(t)
	ctx := context.Background()

	const repo = "test/list-repo-keys-deleted"
	cleanRepoFull(t, store, repo)
	rawSetRepoState(t, store, repo, "sha-transient")

	keys, err := store.ListRepoKeys(ctx)
	require.NoError(t, err)
	assert.Contains(t, keys, repo, "setup: repo must be present before delete")

	// Remove the code_repo_state row directly (same statement cleanRepoFull's
	// t.Cleanup uses), then confirm it drops out of the list.
	_, err = store.pool.Exec(ctx, `DELETE FROM code_repo_state WHERE repo_key = $1`, repo)
	require.NoError(t, err)

	keysAfter, err := store.ListRepoKeys(ctx)
	require.NoError(t, err)
	assert.NotContains(t, keysAfter, repo, "ListRepoKeys must not return a deleted repo_key")
}

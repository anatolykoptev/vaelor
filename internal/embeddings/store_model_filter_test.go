package embeddings

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Per-row embed_model is part of the read contract (#837): rows stamped by a
// different embedding model occupy a foreign vector space and must never be
// served — drift is unservable by construction, not just detected by the
// state-row guard. A repo's PK (repo_key, file_path, symbol_name) cannot hold
// two models for one symbol (upsert overwrites embed_model), so the mixed-space
// fixture simulates an interrupted reindex: old-model rows in one file,
// new-model rows in another.

// TestSearch_ModelFilter_HidesForeignSpace: with Model set, Search returns only
// rows stamped with that model; empty Model keeps the legacy unfiltered mode.
func TestSearch_ModelFilter_HidesForeignSpace(t *testing.T) {
	if testing.Short() {
		t.Skip("requires postgres")
	}
	store := testStore(t)
	ctx := context.Background()
	const repoKey = "test/model-filter-space"
	cleanRepoFull(t, store, repoKey)

	require.NoError(t, store.Upsert(ctx, []EmbeddingRecord{
		{RepoKey: repoKey, FilePath: "old.go", SymbolName: "OldSym", SymbolKind: "function",
			Language: "go", EmbedModel: "jina-code-v2", Embedding: makeVec(1)},
		{RepoKey: repoKey, FilePath: "new.go", SymbolName: "NewSym", SymbolKind: "function",
			Language: "go", EmbedModel: "code-rank-embed", Embedding: makeVec(1)},
	}))

	// Model filter → only the current-space row. Without the filter the
	// identical foreign-space vector would tie at distance 0.
	res, err := store.Search(ctx, makeVec(1), SearchOpts{RepoKey: repoKey, Model: "code-rank-embed"})
	require.NoError(t, err)
	require.Len(t, res, 1)
	assert.Equal(t, "NewSym", res[0].SymbolName)

	// A model that never indexed this repo sees nothing — the repo reads as
	// unindexed, which is what triggers reindexing upstream.
	res, err = store.Search(ctx, makeVec(1), SearchOpts{RepoKey: repoKey, Model: "future-model"})
	require.NoError(t, err)
	assert.Empty(t, res)

	// Empty Model → no filter (legacy/test mode): both spaces visible.
	res, err = store.Search(ctx, makeVec(1), SearchOpts{RepoKey: repoKey})
	require.NoError(t, err)
	assert.Len(t, res, 2)
}

// TestFindSimilarPairs_SameSpaceOnly: the O(n²) self-join must never pair rows
// across embedding spaces — cross-model distances are meaningless. The fixture
// uses byte-identical vectors stamped with different models, so without the
// a.embed_model = b.embed_model clause the cross-model pair would top the list.
func TestFindSimilarPairs_SameSpaceOnly(t *testing.T) {
	if testing.Short() {
		t.Skip("requires postgres")
	}
	store := testStore(t)
	ctx := context.Background()
	const repoKey = "test/model-filter-pairs"
	cleanRepoFull(t, store, repoKey)

	vec := makeVec(1, 1, 1)
	require.NoError(t, store.Upsert(ctx, []EmbeddingRecord{
		{RepoKey: repoKey, FilePath: "a1.go", SymbolName: "SymA1", SymbolKind: "function",
			EmbedModel: "model-a", Embedding: vec},
		{RepoKey: repoKey, FilePath: "a2.go", SymbolName: "SymA2", SymbolKind: "function",
			EmbedModel: "model-a", Embedding: vec},
		{RepoKey: repoKey, FilePath: "b1.go", SymbolName: "SymB1", SymbolKind: "function",
			EmbedModel: "model-b", Embedding: vec}, // identical vector, foreign space
	}))

	pairs, err := store.FindSimilarPairs(ctx, SimilarPairOpts{RepoKey: repoKey, Threshold: 0.9})
	require.NoError(t, err)
	require.Len(t, pairs, 1, "cross-space pairs must be excluded; only SymA1/SymA2 share a space")
	assert.ElementsMatch(t, []string{"SymA1", "SymA2"},
		[]string{pairs[0].SymbolA, pairs[0].SymbolB})
}

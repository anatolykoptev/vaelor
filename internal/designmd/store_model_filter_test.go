package designmd

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Per-row embed_model is part of the read contract (#839): rows stamped by a
// different embedding model occupy a foreign vector space and must never be
// served. The (brand, section) PK collapses same-key rows across models, so
// the foreign-space fixture uses distinct brands — matching the real-world
// scenario where two models each index a partially-overlapping corpus.

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping pgvector integration test")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("pgxpool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func unitVec1024(idx int) []float32 {
	v := make([]float32, dimSize)
	v[idx] = 1
	return v
}

// TestSearch_ModelFilter_HidesForeignSpace: with a model set, Search returns
// only same-space rows; GetHashes is scoped the same way so the indexer
// re-embeds (and re-stamps) foreign rows instead of hash-skipping them
// forever. Mutation check: removing either WHERE embed_model predicate turns
// this test red — Search returns the foreign row / GetHashes exposes it.
func TestSearch_ModelFilter_HidesForeignSpace(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	old := NewStore(pool, "e5-old")
	require := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	require(old.EnsureSchema(ctx))
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM design_embeddings WHERE brand LIKE '__test_mf__%'")
	})

	require(old.Upsert(ctx, []Record{
		{Brand: "__test_mf__a", Section: "sec", FilePath: "a/DESIGN.md", Embedding: unitVec1024(0)},
	}))

	// Same corpus key seen under a different model is invisible to Search…
	fresh := NewStore(pool, "e5-new")
	res, err := fresh.Search(ctx, unitVec1024(0), 10)
	require(err)
	for _, r := range res {
		if r.Brand == "__test_mf__a" {
			t.Fatalf("foreign-space row returned by Search: %+v", r)
		}
	}

	// …and invisible to GetHashes, so the next index run re-embeds it under
	// the active model instead of hash-skipping.
	hashes, err := fresh.GetHashes(ctx)
	require(err)
	if _, ok := hashes["__test_mf__a:sec"]; ok {
		t.Fatal("foreign-space row visible to GetHashes — hash-skip would leave it stale forever")
	}
	oldHashes, err := old.GetHashes(ctx)
	require(err)
	if _, ok := oldHashes["__test_mf__a:sec"]; !ok {
		t.Fatal("same-space row missing from GetHashes")
	}

	// Re-indexing under the new model re-stamps the row and makes it visible.
	require(fresh.Upsert(ctx, []Record{
		{Brand: "__test_mf__a", Section: "sec", FilePath: "a/DESIGN.md", Embedding: unitVec1024(0)},
	}))
	res, err = fresh.Search(ctx, unitVec1024(0), 10)
	require(err)
	found := false
	for _, r := range res {
		if r.Brand == "__test_mf__a" {
			found = true
		}
	}
	if !found {
		t.Fatal("re-stamped row not returned by Search under the new model")
	}
}

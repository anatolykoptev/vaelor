package embeddings

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	pgvector "github.com/pgvector/pgvector-go"
)

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func makeVec(vals ...float32) []float32 {
	v := make([]float32, dimSize)
	copy(v, vals)
	return v
}

func TestSearch_DistanceThreshold(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	store := NewStore(pool)

	if err := store.EnsureSchema(ctx); err != nil {
		t.Fatalf("schema: %v", err)
	}

	// Clean up test data.
	const repo = "test/distance-threshold"
	defer func() {
		_ = store.DeleteRepo(ctx, repo)
	}()
	_ = store.DeleteRepo(ctx, repo)

	// Insert 3 embeddings with known vectors.
	records := []EmbeddingRecord{
		{RepoKey: repo, FilePath: "a.go", SymbolName: "Close", SymbolKind: "function", Language: "go", Embedding: makeVec(1, 0, 0)},
		{RepoKey: repo, FilePath: "b.go", SymbolName: "Far1", SymbolKind: "function", Language: "go", Embedding: makeVec(0, 1, 0)},
		{RepoKey: repo, FilePath: "c.go", SymbolName: "Far2", SymbolKind: "function", Language: "go", Embedding: makeVec(0, 0, 1)},
	}
	if err := store.Upsert(ctx, records); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	// Query close to v1.
	query := makeVec(0.9, 0.1, 0)

	// Without distance filter — should return all 3.
	all, err := store.Search(ctx, query, SearchOpts{RepoKey: repo, TopK: 10})
	if err != nil {
		t.Fatalf("search all: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("expected 3 results without filter, got %d", len(all))
	}

	// With MaxDistance=0.5 — only the close vector should pass.
	filtered, err := store.Search(ctx, query, SearchOpts{RepoKey: repo, TopK: 10, MaxDistance: 0.5})
	if err != nil {
		t.Fatalf("search filtered: %v", err)
	}
	if len(filtered) != 1 {
		for _, r := range all {
			t.Logf("  %s distance=%.4f", r.SymbolName, r.Distance)
		}
		t.Fatalf("expected 1 result with MaxDistance=0.5, got %d", len(filtered))
	}
	if filtered[0].SymbolName != "Close" {
		t.Errorf("expected Close, got %s", filtered[0].SymbolName)
	}

	// Exact match should pass a very strict threshold.
	none, err := store.Search(ctx, pgvector.NewVector(makeVec(0, 1, 0)).Slice(), SearchOpts{
		RepoKey: repo, TopK: 10, MaxDistance: 0.01,
	})
	if err != nil {
		t.Fatalf("search strict: %v", err)
	}
	if len(none) != 1 {
		t.Errorf("expected 1 exact match with MaxDistance=0.01, got %d", len(none))
	}
}

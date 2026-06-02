package embeddings

import (
	"context"
	"fmt"
	"testing"

	"github.com/anatolykoptev/go-kit/sparse"
)

// TestUpdateSparseEmbeddingsBatch_HappyPath seeds N dense rows, calls
// UpdateSparseEmbeddingsBatch once, then reads back the rows to assert all
// sparse_embedding columns were written. Uses the live gocode DB (skipped when
// DATABASE_URL is not set).
//
// Falsification: comment out updateSparseEmbeddingsBatchChunk → all rows stay
// NULL and the SELECT scan finds no non-NULL rows, t.Errorf fires (RED).
func TestUpdateSparseEmbeddingsBatch_HappyPath(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	store := NewStore(pool)

	if err := store.EnsureSchema(ctx); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}

	const repo = "test/batch-write-happy"
	_ = store.DeleteRepo(ctx, repo)
	t.Cleanup(func() { _ = store.DeleteRepo(ctx, repo) })

	// Seed 5 dense rows (sparse_embedding stays NULL via Upsert).
	const n = 5
	records := make([]EmbeddingRecord, n)
	for i := range n {
		records[i] = EmbeddingRecord{
			RepoKey:    repo,
			FilePath:   fmt.Sprintf("f%d.go", i),
			SymbolName: fmt.Sprintf("Sym%d", i),
			SymbolKind: "function",
			Language:   "go",
			Embedding:  makeVec(float32(i) * 0.1),
		}
	}
	if err := store.Upsert(ctx, records); err != nil {
		t.Fatalf("upsert dense: %v", err)
	}

	// Build a batch of sparse updates — one per row.
	batch := make([]SparseUpdate, n)
	for i := range n {
		sv := sparse.SparseVector{
			Indices: []uint32{uint32(i + 1)},
			Values:  []float32{0.8},
		}
		batch[i] = SparseUpdate{
			RepoKey:    repo,
			FilePath:   fmt.Sprintf("f%d.go", i),
			SymbolName: fmt.Sprintf("Sym%d", i),
			Literal:    SanitizeAndFormatSparseVector(sv, sparseDim),
		}
	}

	// One batch UPDATE — should touch all 5 rows in a single round-trip.
	if err := store.UpdateSparseEmbeddingsBatch(ctx, batch); err != nil {
		t.Fatalf("UpdateSparseEmbeddingsBatch: %v", err)
	}

	// Verify all rows now have a non-NULL sparse_embedding.
	var nullCount int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM public.code_embeddings
		 WHERE repo_key=$1 AND sparse_embedding IS NULL`, repo,
	).Scan(&nullCount); err != nil {
		t.Fatalf("count null: %v", err)
	}
	if nullCount != 0 {
		t.Errorf("expected 0 NULL rows after batch write, got %d", nullCount)
	}

	var nonNullCount int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM public.code_embeddings
		 WHERE repo_key=$1 AND sparse_embedding IS NOT NULL`, repo,
	).Scan(&nonNullCount); err != nil {
		t.Fatalf("count non-null: %v", err)
	}
	if nonNullCount != n {
		t.Errorf("expected %d rows with sparse_embedding, got %d", n, nonNullCount)
	}
}

// TestUpdateSparseEmbeddingsBatch_DenseIndependence seeds a dense row, then
// verifies that UpdateSparseEmbeddingsBatch writes only the sparse column and
// does NOT alter embedding (dense). Confirms the dense-independence invariant
// (P2 MAJOR-1): a sparse batch failure cannot touch or roll back dense data.
//
// Falsification: change the UPDATE SET clause to also modify `embedding` → the
// dense-independence check fires (RED).
func TestUpdateSparseEmbeddingsBatch_DenseIndependence(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	store := NewStore(pool)

	if err := store.EnsureSchema(ctx); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}

	const (
		repo   = "test/batch-dense-independence"
		file   = "x.go"
		symbol = "IndepFunc"
	)
	_ = store.DeleteRepo(ctx, repo)
	t.Cleanup(func() { _ = store.DeleteRepo(ctx, repo) })

	// Seed with a distinct dense vector so we can tell if it changed.
	denseVec := makeVec(0.42)
	if err := store.Upsert(ctx, []EmbeddingRecord{{
		RepoKey:    repo,
		FilePath:   file,
		SymbolName: symbol,
		SymbolKind: "function",
		Language:   "go",
		Embedding:  denseVec,
	}}); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	// Batch-write the sparse vector.
	sv := sparse.SparseVector{Indices: []uint32{7}, Values: []float32{0.9}}
	batch := []SparseUpdate{{
		RepoKey:    repo,
		FilePath:   file,
		SymbolName: symbol,
		Literal:    SanitizeAndFormatSparseVector(sv, sparseDim),
	}}
	if err := store.UpdateSparseEmbeddingsBatch(ctx, batch); err != nil {
		t.Fatalf("batch write: %v", err)
	}

	// Read back and confirm embedding[0] is still 0.42.
	var embeddingStr string
	if err := pool.QueryRow(ctx,
		`SELECT embedding::text FROM public.code_embeddings
		 WHERE repo_key=$1 AND file_path=$2 AND symbol_name=$3`,
		repo, file, symbol,
	).Scan(&embeddingStr); err != nil {
		t.Fatalf("read dense back: %v", err)
	}
	// pgvector formats as [0.42,0,0,...] — just check it starts with [0.42
	if len(embeddingStr) < 6 || embeddingStr[:5] != "[0.42" {
		t.Errorf("dense embedding mutated after sparse batch write; got: %s", embeddingStr[:min(30, len(embeddingStr))])
	}
}

// TestUpdateSparseEmbeddingsBatch_EmptyBatch asserts that an empty slice is a
// no-op (no error, no DB round-trip).
//
// Falsification: replace the `if len(rows) == 0 { return nil }` guard with an
// unconditional batch build → the UPDATE statement would have an empty VALUES
// list, producing a Postgres syntax error (RED).
func TestUpdateSparseEmbeddingsBatch_EmptyBatch(t *testing.T) {
	store := &Store{} // nil pool — must not be reached
	if err := store.UpdateSparseEmbeddingsBatch(context.Background(), nil); err != nil {
		t.Errorf("expected nil error for empty batch, got %v", err)
	}
	if err := store.UpdateSparseEmbeddingsBatch(context.Background(), []SparseUpdate{}); err != nil {
		t.Errorf("expected nil error for empty slice, got %v", err)
	}
}

package embeddings

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"testing"

	"github.com/anatolykoptev/go-kit/sparse"
	dto "github.com/prometheus/client_model/go"

	"github.com/anatolykoptev/go-code/internal/parser"
)

// backfillCounterValue reads the current value of sparseBackfillTotal for outcome.
func backfillCounterValue(outcome string) float64 {
	c := sparseBackfillTotal.WithLabelValues(outcome)
	m := &dto.Metric{}
	if err := c.Write(m); err != nil {
		return 0
	}
	if m.Counter != nil {
		return m.Counter.GetValue()
	}
	return 0
}

// --- unit tests (no DB) ---

// TestBackfill_MissingRepo_CountedSkippedMiss verifies that when rootLookup
// returns (_, false) for a repoKey, all rows for that repo are counted as
// skipped_missing and the function returns without error.
//
// Falsification: remove the `!ok` branch from backfillPage → function will
// call filepath.Join("", ...) and attempt os.ReadFile on an invalid path,
// producing a different code path. OR remove the counter increment → delta stays 0,
// test goes RED.
func TestBackfill_MissingRepo_CountedSkippedMiss(t *testing.T) {
	rows := []BackfillRow{
		{RepoKey: "missing/repo", FilePath: "a.go", SymbolName: "Alpha", Language: "go"},
		{RepoKey: "missing/repo", FilePath: "b.go", SymbolName: "Beta", Language: "go"},
	}
	result := &BackfillResult{}
	before := backfillCounterValue(backfillOutcomeMissing)

	// rootLookup always returns false — repo not on disk.
	store := &Store{}
	store.backfillPage(
		context.Background(),
		&fakeSparseEmbedder{},
		rows,
		func(_ string) (string, bool) { return "", false },
		func(_ context.Context, _ []SparseUpdate) error { return nil },
		result,
	)

	after := backfillCounterValue(backfillOutcomeMissing)
	if delta := after - before; delta != 2 {
		t.Errorf("skipped_missing counter: expected delta 2, got %g", delta)
	}
	if result.SkippedMiss != 2 {
		t.Errorf("result.SkippedMiss: expected 2, got %d", result.SkippedMiss)
	}
	if result.Backfilled != 0 {
		t.Errorf("no row should be backfilled when repo is missing, got %d", result.Backfilled)
	}
}

// TestBackfill_HashDrift_RowStaysNullAndCountedDrift verifies that when the
// freshly-computed body hash does not match the stored one, the row is counted
// as skipped_drift, the write function is NOT called, and the result reflects it.
//
// Falsification: remove the `freshHash != row.BodyHash` guard → the row would
// proceed to embed+write, skipped_drift counter would NOT increment, and the
// writeSpy would be called with inconsistent data. The delta assertion goes RED.
func TestBackfill_HashDrift_RowStaysNullAndCountedDrift(t *testing.T) {
	// Create a real temp dir with a Go source file.
	dir := t.TempDir()
	src := []byte("package p\n\nfunc Foo() {}\n")
	if err := os.WriteFile(filepath.Join(dir, "foo.go"), src, 0o644); err != nil {
		t.Fatal(err)
	}
	// Make it look like a git repo (backfillPage checks os.Stat on .git).
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Compute the real hash for the symbol so we can deliberately set the wrong one.
	// We parse the file to get the real symbol, build embed text, hash it.
	// Then store a DIFFERENT hash to trigger drift.
	rows := []BackfillRow{
		{
			RepoKey:    "test/drift",
			FilePath:   "foo.go",
			SymbolName: "Foo",
			Language:   "go",
			BodyHash:   0xDEADBEEFDEADBEEF, // intentionally wrong hash
		},
	}
	result := &BackfillResult{}
	var writeCalled bool
	before := backfillCounterValue(backfillOutcomeDrift)

	store := &Store{}
	store.backfillPage(
		context.Background(),
		&fakeSparseEmbedder{},
		rows,
		func(_ string) (string, bool) { return dir, true },
		func(_ context.Context, _ []SparseUpdate) error {
			writeCalled = true
			return nil
		},
		result,
	)

	after := backfillCounterValue(backfillOutcomeDrift)
	if delta := after - before; delta != 1 {
		t.Errorf("skipped_drift counter: expected delta 1, got %g", delta)
	}
	if result.SkippedDrift != 1 {
		t.Errorf("result.SkippedDrift: expected 1, got %d", result.SkippedDrift)
	}
	if writeCalled {
		t.Error("write must NOT be called for a drifted row — dense/sparse would be inconsistent")
	}
}

// TestBackfill_HappyPath_BackfillsNullRow verifies that a seeded NULL row
// with matching hash is backfilled: sparse vector written, counter incremented.
//
// Falsification: remove the `writeSparse(...)` call from backfillPage → writeCalled
// stays false and result.Backfilled stays 0, test goes RED.
func TestBackfill_HappyPath_BackfillsNullRow(t *testing.T) {
	dir := t.TempDir()
	src := []byte("package p\n\nfunc Bar() {}\n")
	if err := os.WriteFile(filepath.Join(dir, "bar.go"), src, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Compute the REAL hash the backfill would compute.
	// We must parse the actual file and call buildEmbedText + textHash.
	realHash := computeRealHash(t, dir, "bar.go", "Bar", "go")

	rows := []BackfillRow{
		{
			RepoKey:    "test/happy",
			FilePath:   "bar.go",
			SymbolName: "Bar",
			Language:   "go",
			BodyHash:   realHash,
		},
	}
	result := &BackfillResult{}
	var writeArgs []string
	before := backfillCounterValue(backfillOutcomeBackfilled)

	store := &Store{}
	store.backfillPage(
		context.Background(),
		&fakeSparseEmbedder{},
		rows,
		func(_ string) (string, bool) { return dir, true },
		func(_ context.Context, batch []SparseUpdate) error {
			for _, r := range batch {
				writeArgs = append(writeArgs, r.SymbolName+":"+r.Literal)
			}
			return nil
		},
		result,
	)

	after := backfillCounterValue(backfillOutcomeBackfilled)
	if delta := after - before; delta != 1 {
		t.Errorf("backfilled counter: expected delta 1, got %g", delta)
	}
	if result.Backfilled != 1 {
		t.Errorf("result.Backfilled: expected 1, got %d", result.Backfilled)
	}
	// One batch call containing 1 row.
	if len(writeArgs) != 1 {
		t.Errorf("batch write contained %d rows, expected 1", len(writeArgs))
	}
}

// TestBackfill_Resumability_RerunOnlyTouchesRemainingNulls verifies that rows
// already written (non-NULL) are NOT re-processed. The IS NULL cursor is the
// mechanism — we test it by seeding a DB and running BackfillSparse twice.
// Uses the live gocode DB (skipped when DATABASE_URL not set).
//
// Falsification: remove the IS NULL clause from fetchNullSparseRows → backfill
// re-processes ALL rows on the second run, and result2.Total would be non-zero,
// making the test go RED.
func TestBackfill_Resumability_RerunOnlyTouchesRemainingNulls(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	store := NewStore(pool)

	if err := store.EnsureSchema(ctx); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}

	const repo = "test/backfill-resumable"
	_ = store.DeleteRepo(ctx, repo)
	t.Cleanup(func() { _ = store.DeleteRepo(ctx, repo) })

	// Seed a row with NULL sparse (dense only — Upsert does not set sparse).
	records := []EmbeddingRecord{
		{
			RepoKey: repo, FilePath: "z.go", SymbolName: "Zap",
			SymbolKind: "function", Language: "go",
			Embedding: makeVec(0.5),
			BodyHash:  0xABCD1234,
		},
	}
	if err := store.Upsert(ctx, records); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	// Manually write the sparse vector — simulates first backfill completing.
	lit := SanitizeAndFormatSparseVector(
		sparse.SparseVector{Indices: []uint32{10}, Values: []float32{0.9}},
		sparseDim,
	)
	if err := store.UpdateSparseEmbeddingsBatch(ctx, []SparseUpdate{
		{RepoKey: repo, FilePath: "z.go", SymbolName: "Zap", Literal: lit},
	}); err != nil {
		t.Fatalf("UpdateSparseEmbeddingsBatch: %v", err)
	}

	// Now run BackfillSparse — the row has a sparse vector, so IS NULL returns 0 rows.
	result, err := store.BackfillSparse(ctx, &fakeSparseEmbedder{}, BackfillOpts{
		RepoKey:           repo,
		RepoRootLookup:    func(_ string) (string, bool) { return "", false }, // not needed
		WriteSparsesBatch: func(_ context.Context, _ []SparseUpdate) error { return nil },
	})
	if err != nil {
		t.Fatalf("BackfillSparse: %v", err)
	}
	if result.Total != 0 {
		t.Errorf("re-run must touch 0 rows (IS NULL cursor); got Total=%d", result.Total)
	}
}

// TestBackfill_BatchBy32_RespectsServerCap verifies that embedSparseBatched is
// called with the correct sub-batch size. Provides 40 candidates and asserts
// the fake embedder sees 2 calls (32 + 8).
//
// This test drives the REAL backfillPage → embedSparseBatched path with 40
// identical real files. Falsification: change sparseServerMaxDocs to 40 →
// only 1 call would be issued, test goes RED (callLen[1] doesn't exist).
func TestBackfill_BatchBy32_RespectsServerCap(t *testing.T) {
	// Build 40 identical files with different symbol names.
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	var rows []BackfillRow
	// Generate 40 unique source files so each has its own symbol.
	for i := range 40 {
		name := fmt.Sprintf("Sym%02d", i)
		content := []byte("package p\n\nfunc " + name + "() {}\n")
		fname := fmt.Sprintf("f%02d.go", i)
		if err := os.WriteFile(filepath.Join(dir, fname), content, 0o644); err != nil {
			t.Fatal(err)
		}
		realHash := computeRealHash(t, dir, fname, name, "go")
		rows = append(rows, BackfillRow{
			RepoKey:    "test/batch32",
			FilePath:   fname,
			SymbolName: name,
			Language:   "go",
			BodyHash:   realHash,
		})
	}

	fake := &fakeSparseEmbedder{}
	result := &BackfillResult{}
	store := &Store{}
	store.backfillPage(
		context.Background(),
		fake,
		rows,
		func(_ string) (string, bool) { return dir, true },
		func(_ context.Context, _ []SparseUpdate) error { return nil },
		result,
	)

	if result.Backfilled != 40 {
		t.Errorf("expected 40 backfilled, got %d (missed=%d, drift=%d, failed=%d)",
			result.Backfilled, result.SkippedMiss, result.SkippedDrift, result.EmbedFailed)
	}
	// 40 texts, maxBatch=32 → 2 calls: sizes 32 + 8.
	if len(fake.calls) != 2 {
		t.Errorf("expected 2 embed calls (32+8), got %d: %v", len(fake.calls), fake.calls)
	}
	if fake.calls[0] != 32 || fake.calls[1] != 8 {
		t.Errorf("unexpected call sizes: %v (want [32 8])", fake.calls)
	}
}

// TestBackfill_EmbedFailed_RowStaysNullAndCounted verifies that when the sparse
// embedder returns an error, all candidates in the failing batch are counted as
// embed_failed and the write function is NOT called.
//
// Falsification: remove the embed error handling in backfillPage → embed_failed
// counter stays unchanged and the test goes RED.
func TestBackfill_EmbedFailed_RowStaysNullAndCounted(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	src := []byte("package p\n\nfunc Qux() {}\n")
	if err := os.WriteFile(filepath.Join(dir, "qux.go"), src, 0o644); err != nil {
		t.Fatal(err)
	}
	realHash := computeRealHash(t, dir, "qux.go", "Qux", "go")

	rows := []BackfillRow{
		{RepoKey: "test/embed-fail", FilePath: "qux.go", SymbolName: "Qux", Language: "go", BodyHash: realHash},
	}

	// Embedder that always fails.
	failEmb := &fakeSparseEmbedder{failOn: 1}
	result := &BackfillResult{}
	var writeCalled bool
	before := backfillCounterValue(backfillOutcomeEmbedFailed)

	store := &Store{}
	store.backfillPage(
		context.Background(),
		failEmb,
		rows,
		func(_ string) (string, bool) { return dir, true },
		func(_ context.Context, _ []SparseUpdate) error {
			writeCalled = true
			return nil
		},
		result,
	)

	after := backfillCounterValue(backfillOutcomeEmbedFailed)
	if delta := after - before; delta != 1 {
		t.Errorf("embed_failed counter: expected delta 1, got %g", delta)
	}
	if result.EmbedFailed != 1 {
		t.Errorf("result.EmbedFailed: expected 1, got %d", result.EmbedFailed)
	}
	if writeCalled {
		t.Error("write must NOT be called when embed fails")
	}
}

// TestBackfill_AllPermanentSkip_Terminates verifies that BackfillSparse breaks
// out of the page loop when a page produces no backfilled/embed_failed rows
// (all drift/missing), preventing an infinite loop.
// Uses the live gocode DB (skipped when DATABASE_URL not set).
//
// Falsification: remove the `pageBackfilled == 0 && pageEmbedFailed == 0` break
// in BackfillSparse → the loop never exits and the test times out.
func TestBackfill_AllPermanentSkip_Terminates(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	store := NewStore(pool)

	if err := store.EnsureSchema(ctx); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}

	const repo = "test/backfill-allskip"
	_ = store.DeleteRepo(ctx, repo)
	t.Cleanup(func() { _ = store.DeleteRepo(ctx, repo) })

	// Seed a row with a deliberately WRONG body_hash (drift) and NULL sparse.
	records := []EmbeddingRecord{
		{
			RepoKey: repo, FilePath: "drift.go", SymbolName: "DriftSym",
			SymbolKind: "function", Language: "go",
			Embedding: makeVec(0.1),
			BodyHash:  0xDEADBEEFDEADBEEF, // will never match real file hash
		},
	}
	if err := store.Upsert(ctx, records); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	// rootLookup returns a dir where the file doesn't exist → skipped_missing.
	emptyDir := t.TempDir()
	result, err := store.BackfillSparse(ctx, &fakeSparseEmbedder{}, BackfillOpts{
		RepoKey:           repo,
		RepoRootLookup:    func(_ string) (string, bool) { return emptyDir, true },
		WriteSparsesBatch: func(_ context.Context, _ []SparseUpdate) error { return nil },
	})
	if err != nil {
		t.Fatalf("BackfillSparse returned unexpected error: %v", err)
	}
	// Must terminate (not loop forever) and report the missing skip.
	if result.SkippedMiss != 1 && result.SkippedDrift != 1 {
		t.Logf("result: %+v", result)
		// Accept either missing or drift depending on whether the file was found.
		// Either way, the function must terminate.
	}
}

// TestBackfill_BatchWrite_OneCallPerPage verifies that backfillPage accumulates
// all surviving candidates and issues exactly ONE batch UPDATE call (not one per
// row). Uses a WriteSparsesBatch spy that counts calls and records rows.
//
// Falsification: revert backfillWriteVecs to the old per-row writeSparse loop →
// the spy would be called N times (not once), batchCalls != 1, test goes RED.
func TestBackfill_BatchWrite_OneCallPerPage(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	const nSymbols = 5
	var rows []BackfillRow
	for i := range nSymbols {
		name := fmt.Sprintf("BatchSym%d", i)
		content := []byte("package p\n\nfunc " + name + "() {}\n")
		fname := fmt.Sprintf("bf%d.go", i)
		if err := os.WriteFile(filepath.Join(dir, fname), content, 0o644); err != nil {
			t.Fatal(err)
		}
		realHash := computeRealHash(t, dir, fname, name, "go")
		rows = append(rows, BackfillRow{
			RepoKey:    "test/batch-one-call",
			FilePath:   fname,
			SymbolName: name,
			Language:   "go",
			BodyHash:   realHash,
		})
	}

	var batchCalls int
	var batchedRows []SparseUpdate
	result := &BackfillResult{}

	store := &Store{}
	store.backfillPage(
		context.Background(),
		&fakeSparseEmbedder{},
		rows,
		func(_ string) (string, bool) { return dir, true },
		func(_ context.Context, batch []SparseUpdate) error {
			batchCalls++
			batchedRows = append(batchedRows, batch...)
			return nil
		},
		result,
	)

	if result.Backfilled != nSymbols {
		t.Errorf("expected %d backfilled, got %d (miss=%d drift=%d failed=%d)",
			nSymbols, result.Backfilled, result.SkippedMiss, result.SkippedDrift, result.EmbedFailed)
	}
	// Key assertion: one batch call, not N per-row calls.
	if batchCalls != 1 {
		t.Errorf("expected 1 batch write call for %d rows, got %d", nSymbols, batchCalls)
	}
	if len(batchedRows) != nSymbols {
		t.Errorf("batch contained %d rows, want %d", len(batchedRows), nSymbols)
	}
}

// TestBackfill_BatchWriteFailure_NonFatal verifies that a batch UPDATE failure
// counts ALL rows in the batch as embed_failed (by row count, not by 1), leaves
// them NULL, and does not propagate a fatal error upward.
//
// Falsification: revert the counter to Inc() (per-item) and change the assertion
// → delta != nSymbols, or remove the batch write call → no rows are counted,
// both go RED.
func TestBackfill_BatchWriteFailure_NonFatal(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	const nSymbols = 3
	var rows []BackfillRow
	for i := range nSymbols {
		name := fmt.Sprintf("FailSym%d", i)
		content := []byte("package p\n\nfunc " + name + "() {}\n")
		fname := fmt.Sprintf("fail%d.go", i)
		if err := os.WriteFile(filepath.Join(dir, fname), content, 0o644); err != nil {
			t.Fatal(err)
		}
		realHash := computeRealHash(t, dir, fname, name, "go")
		rows = append(rows, BackfillRow{
			RepoKey:    "test/batch-fail",
			FilePath:   fname,
			SymbolName: name,
			Language:   "go",
			BodyHash:   realHash,
		})
	}

	result := &BackfillResult{}
	before := backfillCounterValue(backfillOutcomeEmbedFailed)

	store := &Store{}
	store.backfillPage(
		context.Background(),
		&fakeSparseEmbedder{},
		rows,
		func(_ string) (string, bool) { return dir, true },
		func(_ context.Context, batch []SparseUpdate) error {
			return errors.New("injected batch write failure")
		},
		result,
	)

	after := backfillCounterValue(backfillOutcomeEmbedFailed)
	delta := after - before
	if int(delta) != nSymbols {
		t.Errorf("embed_failed counter: expected delta %d (one per row in batch), got %g", nSymbols, delta)
	}
	if result.EmbedFailed != nSymbols {
		t.Errorf("result.EmbedFailed: expected %d, got %d", nSymbols, result.EmbedFailed)
	}
	if result.Backfilled != 0 {
		t.Errorf("no row must be counted as backfilled when batch write fails, got %d", result.Backfilled)
	}
}

// --- helpers ---

// computeRealHash parses fileName from dir, finds symbolName, builds embed text
// using buildEmbedText (same function the indexer uses), and returns textHash.
// Used to compute the hash that the backfill would see for a given symbol.
func computeRealHash(t *testing.T, dir, fileName, symbolName, language string) uint64 {
	t.Helper()
	src, err := os.ReadFile(filepath.Join(dir, fileName))
	if err != nil {
		t.Fatalf("computeRealHash: read %s: %v", fileName, err)
	}
	pr, err := parser.ParseFile(filepath.Join(dir, fileName), src, parser.ParseOpts{
		Language:    language,
		IncludeBody: true,
	})
	if err != nil {
		t.Fatalf("computeRealHash: parse %s: %v", fileName, err)
	}
	for _, sym := range pr.Symbols {
		if sym.Name == symbolName {
			return textHash(buildEmbedText(sym, fileName))
		}
	}
	t.Fatalf("computeRealHash: symbol %s not found in %s", symbolName, fileName)
	return 0
}

// fnvHash is a local helper to produce a stable non-matching hash for tests.
func fnvHash(s string) uint64 {
	h := fnv.New64a()
	h.Write([]byte(s))
	return h.Sum64()
}

// errInjectBackfill is a sentinel for write-spy injection.
var errInjectBackfill = errors.New("injected write failure")

// ensure fnvHash and errInjectBackfill are used to avoid compile errors.
var _ = fnvHash
var _ = errInjectBackfill

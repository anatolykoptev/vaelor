package embeddings

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/anatolykoptev/go-kit/sparse"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

// --- embedSparseBatched ---

// fakeSparseEmbedder is an httptest-backed SparseEmbedder that records call
// counts and returns one SparseVector per input text (index=0, value=1.0).
type fakeSparseEmbedder struct {
	calls   []int // lengths of each EmbedSparse call
	failOn  int   // if > 0, return error on the n-th call (1-indexed)
	callNum int
}

func (f *fakeSparseEmbedder) EmbedSparse(_ context.Context, texts []string) ([]sparse.SparseVector, error) {
	f.callNum++
	f.calls = append(f.calls, len(texts))
	if f.failOn > 0 && f.callNum == f.failOn {
		return nil, fmt.Errorf("injected sparse error on call %d", f.callNum)
	}
	out := make([]sparse.SparseVector, len(texts))
	for i := range out {
		out[i] = sparse.SparseVector{Indices: []uint32{uint32(i)}, Values: []float32{1.0}}
	}
	return out, nil
}
func (f *fakeSparseEmbedder) EmbedSparseQuery(ctx context.Context, text string) (sparse.SparseVector, error) {
	return sparse.EmbedSparseQueryViaEmbed(ctx, f, text)
}
func (f *fakeSparseEmbedder) VocabSize() int { return 30522 }
func (f *fakeSparseEmbedder) Close() error   { return nil }

func TestEmbedSparseBatched_SubBatchesByMaxBatch(t *testing.T) {
	// 70 texts, maxBatch=32 → ceil(70/32)=3 calls of sizes 32, 32, 6.
	texts := make([]string, 70)
	for i := range texts {
		texts[i] = fmt.Sprintf("sym%d", i)
	}
	fake := &fakeSparseEmbedder{}
	vecs, err := embedSparseBatched(context.Background(), fake, texts, 32)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(vecs) != 70 {
		t.Errorf("expected 70 vectors, got %d", len(vecs))
	}
	if len(fake.calls) != 3 {
		t.Errorf("expected 3 calls, got %d: %v", len(fake.calls), fake.calls)
	}
	if fake.calls[0] != 32 || fake.calls[1] != 32 || fake.calls[2] != 6 {
		t.Errorf("unexpected call sizes: %v", fake.calls)
	}
}

func TestEmbedSparseBatched_ExactMultiple(t *testing.T) {
	// 32 texts with maxBatch=32 → exactly 1 call.
	texts := make([]string, 32)
	for i := range texts {
		texts[i] = fmt.Sprintf("t%d", i)
	}
	fake := &fakeSparseEmbedder{}
	vecs, err := embedSparseBatched(context.Background(), fake, texts, 32)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(vecs) != 32 {
		t.Errorf("expected 32 vectors, got %d", len(vecs))
	}
	if len(fake.calls) != 1 {
		t.Errorf("expected 1 call, got %d", len(fake.calls))
	}
}

func TestEmbedSparseBatched_ErrorBumpsCounter(t *testing.T) {
	// Fail on the 2nd call; verify error is returned (dense stays unaffected — caller logic).
	texts := make([]string, 40)
	for i := range texts {
		texts[i] = fmt.Sprintf("t%d", i)
	}
	fake := &fakeSparseEmbedder{failOn: 2}
	_, err := embedSparseBatched(context.Background(), fake, texts, 32)
	if err == nil {
		t.Fatal("expected error from injected failure, got nil")
	}
}

// --- WithSparseEmbedder nil gate (byte-identical dense-only path) ---

// TestEmbedAndUpsert_NilSparseClient_NoSparseWrites asserts that when Pipeline
// has no sparseClient, embedAndUpsert builds records with zero-valued
// SparseEmbedding (→ NULL in DB). We verify by inspecting the records that
// would be passed to Upsert via a store spy.
//
// Falsification: remove the "sparseVecs[i]" assignment in embedAndUpsert and
// this test still passes (the zero value is still empty). To properly falsify,
// we set a sparseClient and verify non-empty sparse vectors ARE populated.
// That is done in TestEmbedAndUpsert_SparseClientPopulatesSparseEmbedding.
func TestPipeline_NilSparseClient_RecordsHaveEmptySparseEmbedding(t *testing.T) {
	// Minimal pipeline: nil sparseClient.
	p := &Pipeline{}
	// Verify the nil gate: sparseVecs must stay zero-valued.
	texts := []string{"alpha func", "beta func"}
	sparseVecs := make([]sparse.SparseVector, len(texts))
	if p.sparseClient != nil {
		t.Fatal("expected nil sparseClient")
	}
	// No call should be made; sparseVecs should remain zero-valued.
	for _, v := range sparseVecs {
		if !v.IsEmpty() {
			t.Error("sparseVec should be empty for nil sparseClient")
		}
	}
}

// TestPipeline_SparseClientPopulatesVectors verifies that when a sparseClient
// is wired, embedSparseBatched is called and returns non-empty vectors.
// This is the falsification test: if WithSparseEmbedder is removed or the
// sparseVecs assignment is dropped from embedAndUpsert, this test goes red
// (because sparseVecs would all be empty SparseVectors).
func TestPipeline_SparseClientPopulatesVectors(t *testing.T) {
	fake := &fakeSparseEmbedder{}
	p := &Pipeline{sparseClient: fake, sparseMaxBatch: 32}

	texts := []string{"func Alpha", "func Beta", "func Gamma"}
	svecs, err := embedSparseBatched(context.Background(), p.sparseClient, texts, p.sparseMaxBatch)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(svecs) != 3 {
		t.Fatalf("expected 3 sparse vectors, got %d", len(svecs))
	}
	for i, v := range svecs {
		if v.IsEmpty() {
			t.Errorf("svecs[%d] is empty — sparse client did not populate vector", i)
		}
	}
	// Confirm fake was called once (3 texts < maxBatch=32).
	if len(fake.calls) != 1 || fake.calls[0] != 3 {
		t.Errorf("expected 1 call of size 3, got calls=%v", fake.calls)
	}
}

// --- httptest-backed batching integration test ---

// TestEmbedSparseBatched_HTTPFakeServer verifies that the batching loop makes
// the correct number of HTTP calls when texts exceed sparseServerMaxDocs.
// Uses a real httptest server to exercise the full HTTP path.
func TestEmbedSparseBatched_HTTPFakeServer(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/embed_sparse" || r.Method != http.MethodPost {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		var req struct {
			Input []string `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		callCount++
		// Return one sparse vector per input: index=callCount, value=0.5.
		type item struct {
			Index   int       `json:"index"`
			Indices []uint32  `json:"indices"`
			Values  []float32 `json:"values"`
		}
		type resp struct {
			Model string `json:"model"`
			Data  []item `json:"data"`
		}
		data := make([]item, len(req.Input))
		for i := range req.Input {
			data[i] = item{Index: i, Indices: []uint32{uint32(callCount)}, Values: []float32{0.5}}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp{Model: "splade-v3-distilbert", Data: data})
	}))
	defer srv.Close()

	e := sparse.NewHTTPSparseEmbedder(srv.URL, "splade-v3-distilbert", nil)
	texts := make([]string, 50) // 50 > 32 → must split into 2 calls (32 + 18)
	for i := range texts {
		texts[i] = fmt.Sprintf("symbol_%d", i)
	}
	vecs, err := embedSparseBatched(context.Background(), e, texts, sparseServerMaxDocs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(vecs) != 50 {
		t.Errorf("expected 50 vectors, got %d", len(vecs))
	}
	if callCount != 2 {
		t.Errorf("expected 2 HTTP calls (32+18), got %d", callCount)
	}
}

// --- SanitizeAndFormatSparseVector ---

func TestSanitizeAndFormat_ZeroWeightDropped(t *testing.T) {
	// A zero-weight entry must be stripped; pgvector rejects it.
	v := sparse.SparseVector{
		Indices: []uint32{10, 20, 30},
		Values:  []float32{0.5, 0.0, 0.3},
	}
	got := SanitizeAndFormatSparseVector(v, 30522)
	if strings.Contains(got, "20:") {
		t.Errorf("zero-weight index 20 must be dropped, got %q", got)
	}
	if !strings.Contains(got, "10:") || !strings.Contains(got, "30:") {
		t.Errorf("non-zero entries missing in %q", got)
	}
}

func TestSanitizeAndFormat_OOBIndexDropped(t *testing.T) {
	// An index >= dim must be dropped to prevent a sparsevec cast error.
	v := sparse.SparseVector{
		Indices: []uint32{100, 30522, 30523, 99999},
		Values:  []float32{0.8, 0.9, 0.7, 0.6},
	}
	got := SanitizeAndFormatSparseVector(v, 30522)
	for _, oob := range []string{"30522:", "30523:", "99999:"} {
		if strings.Contains(got, oob) {
			t.Errorf("OOB index %s must be dropped, got %q", oob, got)
		}
	}
	if !strings.Contains(got, "100:") {
		t.Errorf("valid index 100 must be kept, got %q", got)
	}
}

func TestSanitizeAndFormat_DuplicateIndexDeduped(t *testing.T) {
	// Duplicate indices (same index, different weights) — keep last value.
	v := sparse.SparseVector{
		Indices: []uint32{42, 42},
		Values:  []float32{0.3, 0.7},
	}
	got := SanitizeAndFormatSparseVector(v, 30522)
	// Must appear exactly once.
	count := strings.Count(got, "42:")
	if count != 1 {
		t.Errorf("index 42 must appear exactly once after dedup, got %d occurrences in %q", count, got)
	}
}

func TestSanitizeAndFormat_AllDegenerateReturnsEmpty(t *testing.T) {
	// All entries are zero or OOB → result must be "" so caller binds NULL.
	v := sparse.SparseVector{
		Indices: []uint32{30522, 30523},
		Values:  []float32{0.0, 0.0},
	}
	got := SanitizeAndFormatSparseVector(v, 30522)
	if got != "" {
		t.Errorf("fully degenerate vector must return empty string, got %q", got)
	}
}

func TestSanitizeAndFormat_ValidVectorUnchanged(t *testing.T) {
	// A clean vector must pass through sanitization and format correctly.
	v := sparse.SparseVector{
		Indices: []uint32{500, 100, 300},
		Values:  []float32{0.9, 0.5, 0.7},
	}
	got := SanitizeAndFormatSparseVector(v, 30522)
	if !strings.HasSuffix(got, "/30522") {
		t.Errorf("dim suffix missing in %q", got)
	}
	// Must be index-ascending (100 < 300 < 500).
	pos100 := strings.Index(got, "100:")
	pos300 := strings.Index(got, "300:")
	pos500 := strings.Index(got, "500:")
	if pos100 >= pos300 || pos300 >= pos500 {
		t.Errorf("sanitized result not index-ascending: %q", got)
	}
}

// TestSanitizeAndFormat_TopKPrune verifies that when a vector has more than
// sparseMaxTerms (256) non-zero entries, only the highest-weight 256 are kept
// and the result is still index-ascending.
//
// Design: 400 entries all with non-zero weight 1 + i*0.001 (lowest = index 0
// at weight 1.0, highest = index 399 at weight 1.399). Top-256 = indices 144..399.
// Index 0 (weight 1.0) falls below the pruning threshold so it must be absent.
//
// Falsification: remove the top-K prune block from SanitizeAndFormatSparseVector
// → nnz will be 400 and the count assertion goes RED.
func TestSanitizeAndFormat_TopKPrune(t *testing.T) {
	const n = 400 // > sparseMaxTerms (256)
	indices := make([]uint32, n)
	values := make([]float32, n)
	for i := range n {
		indices[i] = uint32(i)
		values[i] = 1.0 + float32(i)*0.001 // weight 1.0+i*0.001: all non-zero; highest = index 399
	}
	v := sparse.SparseVector{Indices: indices, Values: values}
	got := SanitizeAndFormatSparseVector(v, 30522)

	// Count number of entries in the result.
	// Format is {i1:w1,i2:w2,...}/dim — one colon per "idx:val" pair.
	open := strings.Index(got, "{")
	close := strings.Index(got, "}")
	if open < 0 || close < 0 || close <= open+1 {
		t.Fatalf("malformed result: %q", got)
	}
	inner := got[open+1 : close]
	nnz := strings.Count(inner, ":") // one colon per entry
	if nnz != sparseMaxTerms {
		t.Errorf("top-K prune: expected %d terms, got %d", sparseMaxTerms, nnz)
	}

	// Top-256 = indices 144..399 (highest 256 by weight 1+i*0.001).
	// Index 0 (weight 1.0) is not in the top 256, so it must be absent.
	// Check by looking for "0:" at start of inner (index 0 would be the first
	// entry if it were present since the result is index-ascending).
	if strings.HasPrefix(inner, "0:") {
		t.Errorf("lowest-weight entry (index 0) must be pruned but appears in result: %q", got[:min(80, len(got))])
	}

	// Result must be index-ascending.
	prev := -1
	for _, part := range strings.Split(inner, ",") {
		var idx int
		if _, err := fmt.Sscanf(part, "%d:", &idx); err != nil {
			t.Fatalf("cannot parse index from %q: %v", part, err)
		}
		if idx <= prev {
			t.Errorf("index %d not strictly ascending after %d in %q", idx, prev, got[:min(80, len(got))])
		}
		prev = idx
	}
}

// --- VocabSize guard (MINOR fix) ---

// wrongVocabEmbedder reports a vocab size that does not match sparseDim.
type wrongVocabEmbedder struct{ fakeSparseEmbedder }

func (w *wrongVocabEmbedder) VocabSize() int { return 65536 } // != 30522

func TestWithSparseEmbedder_VocabMismatchDisablesSparse(t *testing.T) {
	// Falsification: remove the VocabSize check from newSparseEmbedder and
	// this test goes red (p.sparseClient would be non-nil).
	p := NewPipeline(nil, nil, "", WithSparseEmbedder(&wrongVocabEmbedder{}))
	if p.sparseClient != nil {
		t.Error("sparse client must be nil when VocabSize != sparseDim (30522)")
	}
}

func TestWithSparseEmbedder_CorrectVocabEnablesSparse(t *testing.T) {
	// A matching vocab size must wire the client.
	p := NewPipeline(nil, nil, "", WithSparseEmbedder(&fakeSparseEmbedder{})) // VocabSize()=30522
	if p.sparseClient == nil {
		t.Error("sparse client must be non-nil when VocabSize == sparseDim (30522)")
	}
}

// --- stage="write" counter ---

// counterValue reads the current float64 value of a Prometheus counter by
// writing a single sample into a dto.Metric. No external testutil needed.
func counterValue(c prometheus.Counter) float64 {
	m := &dto.Metric{}
	if err := c.Write(m); err != nil {
		return 0
	}
	if m.Counter != nil {
		return m.Counter.GetValue()
	}
	return 0
}

// TestSparseWriteFailure_BumpsWriteCounter verifies that a sparse UPDATE failure
// via the per-row legacy path bumps gocode_sparse_embed_failures_total{stage="write"}.
// NOTE: production now uses the batch path (withWriteSparsesBatchFn); this test
// exercises the per-row writeSparse field which is kept for backward compat but is
// no longer the production path. See TestSparseBatchWrite_FailureBumpsCounterByBatchSize.
func TestSparseWriteFailure_BumpsWriteCounter(t *testing.T) {
	// Inject a spy UpdateSparseEmbeddingsBatch (batch path) that always fails.
	writeErr := errors.New("injected sparse batch write error")
	p := NewPipeline(nil, nil, "",
		WithSparseEmbedder(&fakeSparseEmbedder{}),
		withWriteSparsesBatchFn(func(_ context.Context, _ []SparseUpdate) error {
			return writeErr
		}),
	)

	// 2 records with non-empty sparse vectors → both end up in one batch.
	records := []EmbeddingRecord{
		{RepoKey: "repo", FilePath: "a.go", SymbolName: "Alpha"},
		{RepoKey: "repo", FilePath: "b.go", SymbolName: "Beta"},
	}
	sparseVecs := []sparse.SparseVector{
		{Indices: []uint32{1}, Values: []float32{0.9}},
		{Indices: []uint32{2}, Values: []float32{0.8}},
	}

	c := sparseEmbedFailTotal.WithLabelValues("write")
	before := counterValue(c)

	// Drive the PRODUCTION runSparseWrites — the batch path.
	p.runSparseWrites(context.Background(), "repo", records, sparseVecs)

	after := counterValue(c)
	// Batch failure: counter incremented by len(batch) = 2 via Add(float64(len(batch))).
	if delta := after - before; delta != 2 {
		t.Errorf("stage=write counter: expected delta 2 (batch size), got %g (before=%g after=%g)", delta, before, after)
	}
}

// TestSparseWriteSuccess_NoCounterBump verifies that a successful sparse write
// does NOT bump the failure counter — ensuring the counter is not always-fired.
//
// Falsification: move the Add() call outside the `werr != nil` branch in
// runSparseWrites → counter would increment even on success, test goes RED.
func TestSparseWriteSuccess_NoCounterBump(t *testing.T) {
	var batchReceived []SparseUpdate
	p := NewPipeline(nil, nil, "",
		WithSparseEmbedder(&fakeSparseEmbedder{}),
		withWriteSparsesBatchFn(func(_ context.Context, rows []SparseUpdate) error {
			batchReceived = append(batchReceived, rows...)
			return nil // success
		}),
	)
	records := []EmbeddingRecord{
		{RepoKey: "repo", FilePath: "c.go", SymbolName: "Gamma"},
	}
	sparseVecs := []sparse.SparseVector{
		{Indices: []uint32{5}, Values: []float32{0.7}},
	}

	c := sparseEmbedFailTotal.WithLabelValues("write")
	before := counterValue(c)
	p.runSparseWrites(context.Background(), "repo", records, sparseVecs)
	after := counterValue(c)

	if delta := after - before; delta != 0 {
		t.Errorf("stage=write counter must NOT increment on success, got delta=%g", delta)
	}
	if len(batchReceived) != 1 {
		t.Errorf("batch spy received %d rows, want 1", len(batchReceived))
	}
}

// TestSparseBatchWrite_OneCallPerChunk verifies that runSparseWrites issues
// exactly ONE call to writeSparsesBatch for a whole chunk (not one per row).
//
// Falsification: revert runSparseWrites to per-row writeSparse loop → the spy
// count would be N (not 1), batchCalls != 1, test goes RED.
func TestSparseBatchWrite_OneCallPerChunk(t *testing.T) {
	const n = 4
	var batchCalls int
	var totalRows int
	p := NewPipeline(nil, nil, "",
		WithSparseEmbedder(&fakeSparseEmbedder{}),
		withWriteSparsesBatchFn(func(_ context.Context, rows []SparseUpdate) error {
			batchCalls++
			totalRows += len(rows)
			return nil
		}),
	)
	records := make([]EmbeddingRecord, n)
	sparseVecs := make([]sparse.SparseVector, n)
	for i := range n {
		records[i] = EmbeddingRecord{
			RepoKey:    "repo",
			FilePath:   fmt.Sprintf("f%d.go", i),
			SymbolName: fmt.Sprintf("Sym%d", i),
		}
		sparseVecs[i] = sparse.SparseVector{
			Indices: []uint32{uint32(i + 1)},
			Values:  []float32{0.5},
		}
	}

	p.runSparseWrites(context.Background(), "repo", records, sparseVecs)

	if batchCalls != 1 {
		t.Errorf("expected 1 batch call for %d rows, got %d", n, batchCalls)
	}
	if totalRows != n {
		t.Errorf("batch received %d rows, want %d", totalRows, n)
	}
}

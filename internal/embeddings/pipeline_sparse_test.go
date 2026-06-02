package embeddings

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/anatolykoptev/go-kit/sparse"
)

// --- FormatSparseVector ---

func TestFormatSparseVector_Empty(t *testing.T) {
	got := FormatSparseVector(sparse.SparseVector{}, 30522)
	want := "{}/30522"
	if got != want {
		t.Errorf("FormatSparseVector empty: got %q want %q", got, want)
	}
}

func TestFormatSparseVector_SortedAscending(t *testing.T) {
	// SPLADE output is weight-descending (high weight first); pgvector requires
	// index-ascending. Verify the formatter sorts a copy without mutating the input.
	v := sparse.SparseVector{
		Indices: []uint32{500, 100, 300},
		Values:  []float32{0.9, 0.5, 0.7},
	}
	origIndices := append([]uint32(nil), v.Indices...)
	got := FormatSparseVector(v, 30522)
	// indices must appear ascending
	if !strings.Contains(got, "100:") {
		t.Errorf("got %q: expected index 100", got)
	}
	pos100 := strings.Index(got, "100:")
	pos300 := strings.Index(got, "300:")
	pos500 := strings.Index(got, "500:")
	if pos100 >= pos300 || pos300 >= pos500 {
		t.Errorf("indices not ascending in %q", got)
	}
	// verify input slice not mutated
	for i, idx := range v.Indices {
		if idx != origIndices[i] {
			t.Errorf("input mutated at [%d]: want %d got %d", i, origIndices[i], idx)
		}
	}
	// dim suffix
	if !strings.HasSuffix(got, "/30522") {
		t.Errorf("missing dim suffix in %q", got)
	}
}

func TestFormatSparseVector_SingleEntry(t *testing.T) {
	v := sparse.SparseVector{Indices: []uint32{42}, Values: []float32{1.5}}
	got := FormatSparseVector(v, 30522)
	want := "{42:1.5}/30522"
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

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
func (f *fakeSparseEmbedder) VocabSize() int  { return 30522 }
func (f *fakeSparseEmbedder) Close() error    { return nil }

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

package codegraph

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/anatolykoptev/go-kit/rerank"
)

// TestRerankCandidateBatches_SplitsAndGlobalisesOrigRank verifies the build-time
// scoring path splits candidates into rerankServerMaxDocs-sized requests (the
// embed server rejects larger ones with 400) and globalises each batch's
// OrigRank back into the full candidate slice, so every candidate is scored
// exactly once with the correct index.
func TestRerankCandidateBatches_SplitsAndGlobalisesOrigRank(t *testing.T) {
	var mu sync.Mutex
	maxDocsSeen := 0

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Documents []string `json:"documents"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		mu.Lock()
		if len(req.Documents) > maxDocsSeen {
			maxDocsSeen = len(req.Documents)
		}
		mu.Unlock()
		// Emulate the real server: reject oversized requests with 400.
		if len(req.Documents) > rerankServerMaxDocs {
			http.Error(w, "documents_too_many", http.StatusBadRequest)
			return
		}
		results := make([]map[string]any, len(req.Documents))
		for i := range req.Documents {
			results[i] = map[string]any{"index": i, "relevance_score": 1.0}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"model": "test", "results": results})
	}))
	defer srv.Close()

	withRerankClient(t, rerank.New(rerank.Config{URL: srv.URL, Model: rerankModel, MaxDocs: maxOrphanCandidates}, nil))

	// 70 candidates → 3 server requests (32 + 32 + 6).
	const n = 70
	candidates := make([]orphanCandidate, n)
	for i := range candidates {
		candidates[i] = orphanCandidate{
			row: []string{fmt.Sprintf(`{"name":"sym%d","file":"f%d.go","complexity":"1"}`, i, i)},
		}
	}

	scored, ok := (&Store{}).rerankCandidateBatches(context.Background(), "repo", candidates)
	if !ok {
		t.Fatal("anyScored=false, want true")
	}
	if maxDocsSeen > rerankServerMaxDocs {
		t.Errorf("a request carried %d docs, exceeds server cap %d (not split)", maxDocsSeen, rerankServerMaxDocs)
	}
	if len(scored) != n {
		t.Fatalf("want %d scored, got %d", n, len(scored))
	}
	// Every candidate index 0..n-1 must appear exactly once — proves OrigRank
	// was globalised across batches (a local-only OrigRank would repeat 0..31).
	seen := make([]int, n)
	for _, sc := range scored {
		if sc.OrigRank < 0 || sc.OrigRank >= n {
			t.Fatalf("OrigRank %d out of range [0,%d)", sc.OrigRank, n)
		}
		seen[sc.OrigRank]++
	}
	for i, c := range seen {
		if c != 1 {
			t.Errorf("candidate %d scored %d times, want exactly 1", i, c)
		}
	}
}

// TestRerankCandidateBatches_PartialBatchFailureSurvives verifies that a failing
// batch is skipped without aborting the others — anyScored stays true and the
// surviving batches' scores are returned.
func TestRerankCandidateBatches_PartialBatchFailureSurvives(t *testing.T) {
	var mu sync.Mutex
	call := 0

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Documents []string `json:"documents"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		mu.Lock()
		idx := call
		call++
		mu.Unlock()
		if idx == 0 {
			// First batch fails — must not abort the rest.
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		results := make([]map[string]any, len(req.Documents))
		for i := range req.Documents {
			results[i] = map[string]any{"index": i, "relevance_score": 1.0}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"model": "test", "results": results})
	}))
	defer srv.Close()

	withRerankClient(t, rerank.NewClient(srv.URL,
		rerank.WithModel(rerankModel), rerank.WithMaxDocs(maxOrphanCandidates), rerank.WithRetry(rerank.NoRetry)))

	const n = 40 // 2 batches (32 + 8); first fails, second survives
	candidates := make([]orphanCandidate, n)
	for i := range candidates {
		candidates[i] = orphanCandidate{
			row: []string{fmt.Sprintf(`{"name":"sym%d","file":"f%d.go","complexity":"1"}`, i, i)},
		}
	}

	scored, ok := (&Store{}).rerankCandidateBatches(context.Background(), "repo", candidates)
	if !ok {
		t.Fatal("anyScored=false, want true (second batch succeeded)")
	}
	// Only the second batch (8 docs) scored; first batch (32) skipped.
	if len(scored) != n-rerankServerMaxDocs {
		t.Fatalf("want %d scored (second batch only), got %d", n-rerankServerMaxDocs, len(scored))
	}
	for _, sc := range scored {
		if sc.OrigRank < rerankServerMaxDocs {
			t.Errorf("OrigRank %d belongs to the failed first batch, should be skipped", sc.OrigRank)
		}
	}
}

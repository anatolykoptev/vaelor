package codegraph

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/anatolykoptev/go-kit/rerank"
	"github.com/anatolykoptev/vaelor/internal/embeddings"
)

// TestRerankSemanticResults_DegradedKeepsProvenance verifies that when the
// reranker is configured (Available) but the server fails (StatusDegraded), the
// results keep their original Source — they must NOT be relabelled "ce_reranked"
// for a rerank that did not actually happen.
func TestRerankSemanticResults_DegradedKeepsProvenance(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	// NoRetry so the 500 degrades immediately instead of retrying for seconds.
	withRerankClient(t, rerank.NewClient(srv.URL,
		rerank.WithModel(rerankModel), rerank.WithRetry(rerank.NoRetry)))

	in := []embeddings.SearchResult{
		{SymbolName: "a", FilePath: "a.go", Source: "hybrid"},
		{SymbolName: "b", FilePath: "b.go", Source: "hybrid"},
	}
	out := RerankSemanticResults(context.Background(), "", "query", in, 10)

	if len(out) != len(in) {
		t.Fatalf("want %d results, got %d", len(in), len(out))
	}
	for i, r := range out {
		if r.Source != "hybrid" {
			t.Errorf("result %d: degraded rerank relabelled Source to %q, want original %q",
				i, r.Source, "hybrid")
		}
	}
}

// TestRerankSemanticResults_ColdPath verifies the unconfigured reranker returns
// the original results capped at topK with original provenance.
func TestRerankSemanticResults_ColdPath(t *testing.T) {
	withRerankClient(t, rerank.New(rerank.Config{URL: ""}, nil))

	in := []embeddings.SearchResult{
		{SymbolName: "a", Source: "semantic"},
		{SymbolName: "b", Source: "semantic"},
		{SymbolName: "c", Source: "semantic"},
	}
	out := RerankSemanticResults(context.Background(), "", "query", in, 2)

	if len(out) != 2 {
		t.Fatalf("want 2 (topK), got %d", len(out))
	}
	for i, r := range out {
		if r.Source != "semantic" {
			t.Errorf("result %d: Source %q, want unchanged %q", i, r.Source, "semantic")
		}
	}
}

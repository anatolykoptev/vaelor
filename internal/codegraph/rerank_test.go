package codegraph

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/anatolykoptev/go-kit/rerank"
)

// scoreAllServer returns a rerank server that scores every document with the
// given constant relevance, so a truncated tail (Score=0) is detectable.
func scoreAllServer(t *testing.T, score float64) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Documents []string `json:"documents"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		results := make([]map[string]any, len(req.Documents))
		for i := range req.Documents {
			results[i] = map[string]any{"index": i, "relevance_score": score}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"model": "test", "results": results})
	}))
}

// rerankRow builds an AGE-vertex-style JSON row with the given name and
// complexity, matching what formatDeadCodeDoc / parseIntField parse.
func rerankRow(name string, complexity string) []string {
	return []string{`{"name":"` + name + `","complexity":"` + complexity + `"}`}
}

// withRerankClient swaps the package-level rerankClient for the duration of a
// test and restores it afterwards.
func withRerankClient(t *testing.T, c *rerank.Client) {
	t.Helper()
	old := rerankClient
	rerankClient = c
	t.Cleanup(func() { rerankClient = old })
}

// TestRerankDeadCode_MapsScoresToRows verifies that scores returned by the
// reranker are mapped back to the correct rows via OrigRank. The fake server
// reverses relevance (last doc scores highest), so the output order must be the
// reverse of the (complexity-sorted) input.
func TestRerankDeadCode_MapsScoresToRows(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Documents []string `json:"documents"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		// Reverse relevance: doc i gets score i, so the LAST doc ranks first.
		// This forces a non-trivial reordering that proves OrigRank mapping.
		results := make([]map[string]any, len(req.Documents))
		for i := range req.Documents {
			results[i] = map[string]any{"index": i, "relevance_score": float64(i)}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model":   "test",
			"results": results,
		})
	}))
	defer srv.Close()

	withRerankClient(t, rerank.New(rerank.Config{URL: srv.URL, Model: "test"}, nil))

	// Pre-sorted by complexity DESC already, so docs are sent in this order.
	rows := [][]string{
		rerankRow("a", "30"),
		rerankRow("b", "20"),
		rerankRow("c", "10"),
	}
	got := RerankDeadCode(context.Background(), rows)

	if len(got) != 3 {
		t.Fatalf("want 3 rows, got %d", len(got))
	}
	wantNames := []string{"c", "b", "a"} // reversed by the fake server
	for i, want := range wantNames {
		gotName := extractFieldRerank(got[i][0], "name")
		if gotName != want {
			t.Errorf("row %d: want name %q, got %q", i, want, gotName)
		}
	}
}

// TestRerankDeadCode_ColdPath verifies the cold-path guarantee: with no
// reranker URL configured the client is unavailable and the original
// (complexity-sorted) order is returned unchanged.
func TestRerankDeadCode_ColdPath(t *testing.T) {
	withRerankClient(t, rerank.New(rerank.Config{URL: ""}, nil))

	rows := [][]string{
		rerankRow("a", "30"),
		rerankRow("b", "20"),
		rerankRow("c", "10"),
	}
	got := RerankDeadCode(context.Background(), rows)

	if len(got) != 3 {
		t.Fatalf("want 3 rows, got %d", len(got))
	}
	wantNames := []string{"a", "b", "c"} // unchanged complexity order
	for i, want := range wantNames {
		gotName := extractFieldRerank(got[i][0], "name")
		if gotName != want {
			t.Errorf("row %d: want name %q, got %q", i, want, gotName)
		}
	}
}

// TestRerankDeadCode_Empty verifies the empty-input fast path.
func TestRerankDeadCode_Empty(t *testing.T) {
	if got := RerankDeadCode(context.Background(), nil); got != nil {
		t.Errorf("want nil for empty input, got %v", got)
	}
}

// TestRerankClient_ScoresBuildTimeTail guards the build-time scoring path
// (ScoreDeadCodeCandidates), which sends ALL candidates — up to
// maxOrphanCandidates — not the runtime pre-filter of rerankPreFilterN. The
// shared client's MaxDocs must cover that bound; otherwise the tail beyond
// MaxDocs comes back Score=0, which ceScoreToProbability turns into a fabricated
// 0.5 probability and surfaces phantom dead code. Configured exactly like the
// production init (MaxDocs: maxOrphanCandidates).
func TestRerankClient_ScoresBuildTimeTail(t *testing.T) {
	srv := scoreAllServer(t, 1.0)
	defer srv.Close()

	c := rerank.New(rerank.Config{URL: srv.URL, Model: rerankModel, MaxDocs: maxOrphanCandidates}, nil)

	// More than the runtime pre-filter cap, to exercise the tail.
	n := rerankPreFilterN + 25
	docs := make([]rerank.Doc, n)
	for i := range docs {
		docs[i] = rerank.Doc{Text: fmt.Sprintf("doc%d", i)}
	}

	res, _ := c.RerankWithResult(context.Background(), "q", docs)
	if res == nil || res.Status != rerank.StatusOk {
		t.Fatalf("want StatusOk, got %+v", res)
	}
	if len(res.Scored) != n {
		t.Fatalf("want %d scored docs, got %d", n, len(res.Scored))
	}
	// The server scored every doc 1.0; none must be truncated to Score=0.
	for _, s := range res.Scored {
		if s.Score == 0 {
			t.Errorf("doc OrigRank=%d truncated to Score=0 — MaxDocs (%d) too small for build-time path",
				s.OrigRank, maxOrphanCandidates)
		}
	}
}

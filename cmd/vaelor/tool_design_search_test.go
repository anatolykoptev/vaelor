package main

import (
	"context"
	"testing"

	"github.com/anatolykoptev/go-kit/embed"

	"github.com/anatolykoptev/vaelor/internal/designmd"
)

// recordingQueryEmbedder captures the exact text handed to EmbedQuery.
type recordingQueryEmbedder struct {
	query string
}

var _ embed.Embedder = (*recordingQueryEmbedder)(nil)

func (r *recordingQueryEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i := range out {
		out[i] = []float32{0}
	}
	return out, nil
}

func (r *recordingQueryEmbedder) EmbedQuery(_ context.Context, text string) ([]float32, error) {
	r.query = text
	return []float32{0}, nil
}

func (r *recordingQueryEmbedder) Dimension() int { return 1024 }
func (r *recordingQueryEmbedder) Close() error   { return nil }

// emptyDesignSearch satisfies designSearcher with zero hits — enough to drive
// handleDesignSearch through the EmbedQuery boundary without a database.
type emptyDesignSearch struct{}

var _ designSearcher = emptyDesignSearch{}

func (emptyDesignSearch) Search(context.Context, []float32, int) ([]designmd.SearchResult, error) {
	return nil, nil
}

func TestHandleDesignSearch_QueryEmbeddedWithE5Prefix(t *testing.T) {
	rec := &recordingQueryEmbedder{}
	deps := DesignDeps{Client: rec, Store: emptyDesignSearch{}}

	_, err := handleDesignSearch(context.Background(),
		DesignSearchInput{Query: "dark minimal dashboard"}, deps, nil, nil, nil)
	if err != nil {
		t.Fatalf("handleDesignSearch: %v", err)
	}

	// multilingual-e5-large was trained on "query: "-prefixed retrieval text;
	// the query reaching the embedder must carry that prefix.
	if want := "query: dark minimal dashboard"; rec.query != want {
		t.Fatalf("EmbedQuery got %q, want %q", rec.query, want)
	}
}

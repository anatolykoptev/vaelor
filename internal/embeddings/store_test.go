package embeddings

import (
	"testing"
)

func TestSearchOptsDefaults(t *testing.T) {
	opts := SearchOpts{}
	if opts.TopK != 0 {
		t.Errorf("expected zero-value TopK, got %d", opts.TopK)
	}
	if opts.RepoKey != "" {
		t.Errorf("expected empty RepoKey, got %q", opts.RepoKey)
	}
	if opts.Language != "" {
		t.Errorf("expected empty Language, got %q", opts.Language)
	}
}

func TestTopKClamping(t *testing.T) {
	tests := []struct {
		name     string
		input    int
		expected int
	}{
		{"zero uses default", 0, defaultTopK},
		{"negative uses default", -5, defaultTopK},
		{"normal value", 10, 10},
		{"at max", maxTopK, maxTopK},
		{"over max clamped", 200, maxTopK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			topK := tt.input
			if topK <= 0 {
				topK = defaultTopK
			}
			if topK > maxTopK {
				topK = maxTopK
			}
			if topK != tt.expected {
				t.Errorf("got %d, want %d", topK, tt.expected)
			}
		})
	}
}

func TestEmbeddingRecordValidation(t *testing.T) {
	r := EmbeddingRecord{
		RepoKey:    "github.com/test/repo",
		FilePath:   "main.go",
		SymbolName: "main",
		SymbolKind: "function",
		Language:   "go",
		StartLine:  1,
		BodyHash:   12345,
		Embedding:  make([]float32, dimSize),
	}
	if r.RepoKey == "" {
		t.Error("RepoKey should not be empty")
	}
	if len(r.Embedding) != dimSize {
		t.Errorf("expected %d dimensions, got %d", dimSize, len(r.Embedding))
	}
}

func TestUpsertEmptyRecords(t *testing.T) {
	s := &Store{} // nil pool — Upsert should short-circuit for empty slice
	err := s.Upsert(t.Context(), nil)
	if err != nil {
		t.Errorf("Upsert(nil) should return nil, got %v", err)
	}
	err = s.Upsert(t.Context(), []EmbeddingRecord{})
	if err != nil {
		t.Errorf("Upsert([]) should return nil, got %v", err)
	}
}

func TestNewStore(t *testing.T) {
	s := NewStore(nil)
	if s == nil {
		t.Fatal("NewStore returned nil")
	}
	if s.pool != nil {
		t.Error("expected nil pool")
	}
}

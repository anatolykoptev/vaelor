package embeddings

import (
	"testing"
)

func TestSimilarPairResult(t *testing.T) {
	p := SimilarPair{
		SymbolA: "Foo", FileA: "a.go", LineA: 10,
		SymbolB: "Bar", FileB: "b.go", LineB: 20,
		Similarity: 0.97,
	}
	if p.SymbolA != "Foo" || p.Similarity != 0.97 {
		t.Errorf("unexpected SimilarPair: %+v", p)
	}
}

func TestSimilarPairOptsDefaults(t *testing.T) {
	opts := SimilarPairOpts{RepoKey: "test"}
	if opts.effectiveThreshold() != defaultSimilarityThreshold {
		t.Errorf("default threshold = %f, want %f", opts.effectiveThreshold(), defaultSimilarityThreshold)
	}
	if opts.effectiveLimit() != defaultSimilarLimit {
		t.Errorf("default limit = %d, want %d", opts.effectiveLimit(), defaultSimilarLimit)
	}
}

func TestSimilarPairOptsCustom(t *testing.T) {
	opts := SimilarPairOpts{RepoKey: "test", Threshold: 0.95, Limit: 30}
	if opts.effectiveThreshold() != 0.95 {
		t.Errorf("threshold = %f, want 0.95", opts.effectiveThreshold())
	}
	if opts.effectiveLimit() != 30 {
		t.Errorf("limit = %d, want 30", opts.effectiveLimit())
	}
}

func TestSimilarPairOptsMaxLimit(t *testing.T) {
	opts := SimilarPairOpts{RepoKey: "test", Limit: 999}
	if opts.effectiveLimit() != maxSimilarLimit {
		t.Errorf("limit = %d, want %d (capped)", opts.effectiveLimit(), maxSimilarLimit)
	}
}

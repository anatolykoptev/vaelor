package ranking

import (
	"testing"
)

func TestBM25F_EmptyCorpus(t *testing.T) {
	scorer := NewBM25F(nil)

	doc := Document{Path: "main.go", Symbols: []string{"main"}}
	score := scorer.Score("main", doc)

	if score != 0 {
		t.Errorf("expected score 0 for empty corpus, got %f", score)
	}

	score = scorer.ScoreTerms([]string{"main", "handler"}, doc)
	if score != 0 {
		t.Errorf("expected ScoreTerms 0 for empty corpus, got %f", score)
	}
}

func TestBM25F_SingleDocument(t *testing.T) {
	docs := []Document{
		{Path: "handler.go", Symbols: []string{"HandleRequest", "ServeHTTP"}},
	}
	scorer := NewBM25F(docs)

	score := scorer.Score("handle", docs[0])
	if score <= 0 {
		t.Errorf("expected positive score for matching term, got %f", score)
	}

	// Non-matching term should return 0.
	score = scorer.Score("database", docs[0])
	if score != 0 {
		t.Errorf("expected 0 for non-matching term, got %f", score)
	}
}

func TestBM25F_SymbolWeightHigherThanPath(t *testing.T) {
	docs := []Document{
		{
			Path:    "file_a.go",
			Symbols: []string{"AuthHandler"},
		},
		{
			Path:    "auth/file_b.go",
			Symbols: []string{"main"},
		},
	}
	scorer := NewBM25F(docs)

	symbolScore := scorer.ScoreTerms([]string{"auth"}, docs[0])
	pathScore := scorer.ScoreTerms([]string{"auth"}, docs[1])

	if symbolScore <= pathScore {
		t.Errorf("symbol match (%f) should score higher than path-only match (%f)", symbolScore, pathScore)
	}
}

func TestBM25F_PathMatchWeighted(t *testing.T) {
	docs := []Document{
		{
			Path:    "auth/handler.go",
			Symbols: []string{"main"},
		},
		{
			Path:    "utils/helper.go",
			Symbols: []string{"main"},
		},
	}
	scorer := NewBM25F(docs)

	pathScore := scorer.ScoreTerms([]string{"auth"}, docs[0])
	noMatchScore := scorer.ScoreTerms([]string{"auth"}, docs[1])

	if pathScore <= noMatchScore {
		t.Errorf("path match (%f) should score higher than no match (%f)", pathScore, noMatchScore)
	}
}

func TestBM25F_MultipleTerms(t *testing.T) {
	docs := []Document{
		{
			Path:    "auth_handler.go",
			Symbols: []string{"AuthHandler", "ValidateToken"},
		},
		{
			Path:    "auth_only.go",
			Symbols: []string{"AuthHandler"},
		},
	}

	scorer := NewBM25F(docs)

	bothTerms := scorer.ScoreTerms([]string{"auth", "token"}, docs[0])
	singleTerm := scorer.ScoreTerms([]string{"auth"}, docs[0])

	if bothTerms <= singleTerm {
		t.Errorf("matching both terms (%f) should score higher than single term (%f)", bothTerms, singleTerm)
	}
}

func TestBM25F_IDF_CommonTermLowerScore(t *testing.T) {
	// "main" appears in all 3 docs (common), "auth" appears in only 1 (rare).
	docs := []Document{
		{
			Path:    "handler.go",
			Symbols: []string{"main", "AuthMiddleware"},
		},
		{
			Path:    "server.go",
			Symbols: []string{"main"},
		},
		{
			Path:    "config.go",
			Symbols: []string{"main"},
		},
	}
	scorer := NewBM25F(docs)

	// For the first doc, "auth" (rare) should score higher than "main" (common).
	authScore := scorer.Score("auth", docs[0])
	mainScore := scorer.Score("main", docs[0])

	if authScore <= mainScore {
		t.Errorf("rare term 'auth' (%f) should score higher than common term 'main' (%f)", authScore, mainScore)
	}
}

package ranking

import (
	"math"
	"strings"
	"sync"
)

// Field weight constants for BM25F scoring.
const (
	WeightSymbol  = 5.0 // symbol name matches are most important
	WeightPath    = 3.0 // file path matches are moderately important
	WeightContent = 1.0 // content matches are baseline importance
)

// BM25 tuning parameters.
const (
	bm25K1 = 1.2  // term frequency saturation parameter
	bm25B  = 0.75 // length normalization parameter
)

// Document represents a file for BM25F scoring.
type Document struct {
	Path    string   // relative file path
	Symbols []string // symbol names in the file
	Content string   // file content (or cleaned excerpt)
}

// BM25F implements field-weighted BM25 scoring.
type BM25F struct {
	n     int                // total number of documents in the corpus
	avgDL float64            // average document length across the corpus
	docDL []float64          // per-document weighted length
	docs  []documentInternal // preprocessed documents for fast lookup

	// Lazy document-frequency cache: computed on first query for each term.
	mu    sync.Mutex
	dfMap map[string]int
}

// documentInternal holds preprocessed lowercase data for fast matching.
type documentInternal struct {
	pathLower    string
	symbolsLower []string
	contentLower string
}

// NewBM25F creates a BM25F scorer from a corpus of documents.
func NewBM25F(docs []Document) *BM25F {
	if len(docs) == 0 {
		return &BM25F{}
	}

	b := &BM25F{
		n:     len(docs),
		docDL: make([]float64, len(docs)),
		docs:  make([]documentInternal, len(docs)),
		dfMap: make(map[string]int),
	}

	// Preprocess documents: lowercase everything once.
	var totalDL float64
	for i, doc := range docs {
		b.docs[i] = documentInternal{
			pathLower:    strings.ToLower(doc.Path),
			contentLower: strings.ToLower(doc.Content),
		}
		b.docs[i].symbolsLower = make([]string, len(doc.Symbols))
		for j, sym := range doc.Symbols {
			b.docs[i].symbolsLower[j] = strings.ToLower(sym)
		}

		// Compute weighted document length: sum of all field contributions.
		dl := float64(len(b.docs[i].symbolsLower))*WeightSymbol +
			WeightPath + // path always contributes a baseline length
			float64(wordCount(b.docs[i].contentLower))*WeightContent
		b.docDL[i] = dl
		totalDL += dl
	}

	b.avgDL = totalDL / float64(b.n)

	return b
}

// Score computes the BM25F score for a single query term against a document.
func (b *BM25F) Score(term string, doc Document) float64 {
	if b.n == 0 {
		return 0
	}

	termLower := strings.ToLower(term)

	// Find the document index.
	docIdx := b.findDocIndex(strings.ToLower(doc.Path))
	if docIdx < 0 {
		return 0
	}

	return b.scoreTermAtIndex(termLower, docIdx)
}

// ScoreTerms computes total BM25F score for multiple query terms.
func (b *BM25F) ScoreTerms(terms []string, doc Document) float64 {
	if b.n == 0 || len(terms) == 0 {
		return 0
	}

	docIdx := b.findDocIndex(strings.ToLower(doc.Path))
	if docIdx < 0 {
		return 0
	}

	var total float64
	for _, term := range terms {
		total += b.scoreTermAtIndex(strings.ToLower(term), docIdx)
	}
	return total
}

// findDocIndex returns the index of the document with the given lowercase path, or -1.
func (b *BM25F) findDocIndex(pathLower string) int {
	for i := range b.docs {
		if b.docs[i].pathLower == pathLower {
			return i
		}
	}
	return -1
}

// documentFrequency returns how many documents contain the term (substring match).
// Results are cached for repeated queries.
func (b *BM25F) documentFrequency(termLower string) int {
	b.mu.Lock()
	if df, ok := b.dfMap[termLower]; ok {
		b.mu.Unlock()
		return df
	}
	b.mu.Unlock()

	// Compute df by scanning all documents (substring match, same as scoring).
	df := 0
	for i := range b.docs {
		if documentContainsTerm(b.docs[i], termLower) {
			df++
		}
	}

	b.mu.Lock()
	b.dfMap[termLower] = df
	b.mu.Unlock()

	return df
}

// scoreTermAtIndex computes BM25F score for a single lowercase term at a given doc index.
func (b *BM25F) scoreTermAtIndex(termLower string, docIdx int) float64 {
	// Weighted term frequency across fields.
	tf := computeWeightedTF(b.docs[docIdx], termLower)
	if tf == 0 {
		return 0
	}

	df := b.documentFrequency(termLower)
	if df == 0 {
		return 0
	}

	// IDF: log((N - df + 0.5) / (df + 0.5) + 1)
	idf := math.Log((float64(b.n-df)+0.5)/(float64(df)+0.5) + 1)

	// BM25 score: idf * (tf * (k1 + 1)) / (tf + k1 * (1 - b + b * dl/avgdl))
	dl := b.docDL[docIdx]
	avgdl := b.avgDL
	if avgdl == 0 {
		avgdl = 1 // avoid division by zero
	}

	numerator := tf * (bm25K1 + 1)
	denominator := tf + bm25K1*(1-bm25B+bm25B*dl/avgdl)

	return idf * numerator / denominator
}

// computeWeightedTF computes the weighted term frequency for a term in a document.
// tf = (symbol match count * WeightSymbol) + (path match * WeightPath) + (content match count * WeightContent)
func computeWeightedTF(doc documentInternal, termLower string) float64 {
	var tf float64

	// Symbol matches: count how many symbol names contain the term.
	for _, sym := range doc.symbolsLower {
		if strings.Contains(sym, termLower) {
			tf += WeightSymbol
		}
	}

	// Path match: binary (1 or 0).
	if strings.Contains(doc.pathLower, termLower) {
		tf += WeightPath
	}

	// Content matches: count occurrences in content.
	if doc.contentLower != "" {
		tf += float64(strings.Count(doc.contentLower, termLower)) * WeightContent
	}

	return tf
}

// documentContainsTerm checks if any field in the document contains the term (substring match).
func documentContainsTerm(doc documentInternal, termLower string) bool {
	if strings.Contains(doc.pathLower, termLower) {
		return true
	}
	for _, sym := range doc.symbolsLower {
		if strings.Contains(sym, termLower) {
			return true
		}
	}
	return strings.Contains(doc.contentLower, termLower)
}

// wordCount returns an approximate word count for a string.
func wordCount(s string) int {
	return len(strings.Fields(s))
}

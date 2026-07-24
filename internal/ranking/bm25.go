package ranking

import (
	"math"
	"strings"
	"sync"
)

// Field weight constants for BM25F scoring.
const (
	WeightSymbol = 5.0 // symbol name matches are most important
	WeightPath   = 3.0 // file path matches are moderately important
	WeightDoc    = 2.0 // doc-comment matches (more verbose, less specific than symbol names)
)

// BM25 tuning parameters.
const (
	bm25K1 = 1.2  // term frequency saturation parameter
	bm25B  = 0.75 // length normalization parameter
)

// Document represents a file for BM25F scoring.
// Only Path, Symbols, and Docs are used — file content is scored via LLM, not BM25F.
type Document struct {
	Path    string   // relative file path
	Symbols []string // symbol names in the file
	Docs    []string // doc-comments / leading comments per symbol
}

// BM25F implements field-weighted BM25 scoring.
type BM25F struct {
	n     int                // total number of documents in the corpus
	avgDL float64            // average document length across the corpus
	docs  []documentInternal // preprocessed corpus for document-frequency (IDF) computation

	// Lazy document-frequency cache: computed on first query for each term.
	mu    sync.Mutex
	dfMap map[string]int
}

// documentInternal holds preprocessed lowercase data for fast matching.
type documentInternal struct {
	pathLower    string
	symbolsLower []string
	docsLower    []string // lowercased doc-comment strings
}

// NewBM25F creates a BM25F scorer from a corpus of documents.
func NewBM25F(docs []Document) *BM25F {
	if len(docs) == 0 {
		return &BM25F{}
	}

	b := &BM25F{
		n:     len(docs),
		docs:  make([]documentInternal, len(docs)),
		dfMap: make(map[string]int),
	}

	// Preprocess documents: lowercase everything once.
	var totalDL float64
	for i, doc := range docs {
		b.docs[i] = documentInternal{
			pathLower: strings.ToLower(doc.Path),
		}
		b.docs[i].symbolsLower = make([]string, len(doc.Symbols))
		for j, sym := range doc.Symbols {
			b.docs[i].symbolsLower[j] = strings.ToLower(sym)
		}
		b.docs[i].docsLower = make([]string, len(doc.Docs))
		for j, d := range doc.Docs {
			b.docs[i].docsLower[j] = strings.ToLower(d)
		}

		// Accumulate weighted document length for avgDL.
		dl := float64(len(b.docs[i].symbolsLower))*WeightSymbol +
			float64(len(b.docs[i].docsLower))*WeightDoc +
			WeightPath // path always contributes a baseline length
		totalDL += dl
	}

	b.avgDL = totalDL / float64(b.n)

	return b
}

// Score computes the BM25F score for a single query term against a document.
// TF and document length are computed from the passed doc's own fields; the
// corpus (built by NewBM25F) supplies only corpus-wide IDF and avgDL. This
// makes scoring correct when multiple candidates share the same Path (the
// per-symbol candidate shape from store_bm25.go BM25Search) — each candidate is
// scored against its own tokens, not the first same-path document in the corpus.
func (b *BM25F) Score(term string, doc Document) float64 {
	if b.n == 0 {
		return 0
	}

	di, dl := preprocessDoc(doc)
	return b.scoreTermInDoc(strings.ToLower(term), di, dl)
}

// ScoreTerms computes total BM25F score for multiple query terms.
// See Score for the per-document TF semantics.
func (b *BM25F) ScoreTerms(terms []string, doc Document) float64 {
	if b.n == 0 || len(terms) == 0 {
		return 0
	}

	di, dl := preprocessDoc(doc)

	var total float64
	for _, term := range terms {
		total += b.scoreTermInDoc(strings.ToLower(term), di, dl)
	}
	return total
}

// preprocessDoc lowercases a Document's fields and computes its weighted
// document length. This is the per-document TF/DL source for Score/ScoreTerms,
// replacing the former path-lookup (findDocIndex) which was incorrect for
// same-path candidates.
func preprocessDoc(doc Document) (documentInternal, float64) {
	di := documentInternal{
		pathLower: strings.ToLower(doc.Path),
	}
	di.symbolsLower = make([]string, len(doc.Symbols))
	for j, sym := range doc.Symbols {
		di.symbolsLower[j] = strings.ToLower(sym)
	}
	di.docsLower = make([]string, len(doc.Docs))
	for j, d := range doc.Docs {
		di.docsLower[j] = strings.ToLower(d)
	}
	dl := float64(len(di.symbolsLower))*WeightSymbol +
		float64(len(di.docsLower))*WeightDoc +
		WeightPath
	return di, dl
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

// scoreTermInDoc computes BM25F score for a single lowercase term against a
// preprocessed document with its own weighted length. TF and DL are
// per-document (from the passed doc); IDF and avgDL are corpus-wide.
func (b *BM25F) scoreTermInDoc(termLower string, doc documentInternal, dl float64) float64 {
	// Weighted term frequency across fields.
	tf := computeWeightedTF(doc, termLower)
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
	avgdl := b.avgDL
	if avgdl == 0 {
		avgdl = 1 // avoid division by zero
	}

	numerator := tf * (bm25K1 + 1)
	denominator := tf + bm25K1*(1-bm25B+bm25B*dl/avgdl)

	return idf * numerator / denominator
}

// computeWeightedTF computes the weighted term frequency for a term in a document.
// tf = (symbol match count * WeightSymbol) + (doc-comment match count * WeightDoc) + (path match * WeightPath)
func computeWeightedTF(doc documentInternal, termLower string) float64 {
	var tf float64

	// Symbol matches: count how many symbol names contain the term.
	for _, sym := range doc.symbolsLower {
		if strings.Contains(sym, termLower) {
			tf += WeightSymbol
		}
	}

	// Doc-comment matches: count how many doc strings contain the term.
	for _, d := range doc.docsLower {
		if strings.Contains(d, termLower) {
			tf += WeightDoc
		}
	}

	// Path match: binary (1 or 0).
	if strings.Contains(doc.pathLower, termLower) {
		tf += WeightPath
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
	for _, d := range doc.docsLower {
		if strings.Contains(d, termLower) {
			return true
		}
	}
	return false
}

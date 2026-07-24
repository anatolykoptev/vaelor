package ranking

import (
	"math"
	"regexp"
	"strings"
	"sync"
	"unicode"

	"github.com/anatolykoptev/vaelor/internal/langutil"
	"github.com/anatolykoptev/vaelor/internal/lextoken"
)

// Field weight constants for BM25F scoring.
//
// WeightPath matches WeightSymbol (both 5.0): Zoekt (index/score.go, PR#785)
// uses equal filename+symbol boost — filenames are as important as symbol names
// for code search.
const (
	WeightSymbol = 5.0 // symbol name matches are most important
	WeightPath   = 5.0 // file path matches are as important as symbol matches (Zoekt equal boost)
	WeightDoc    = 2.0 // doc-comment matches (more verbose, less specific than symbol names)
)

// BM25 tuning parameters.
const (
	bm25K1 = 1.2  // term frequency saturation parameter
	bm25B  = 0.75 // length normalization parameter
)

// lowPriorityFilePenalty divides the weighted TF for test/generated files,
// demoting mechanically-produced or test-only matches. Mirrors Zoekt's
// const lowPriorityFilePenalty = 5 (index/score.go), eval-validated.
const lowPriorityFilePenalty = 5.0

// minTokenLen is the minimum token length kept in a field/query term multiset.
// Matches lextoken.Tokenize's ≥3-char floor so index and query tokenization are
// symmetric and short noise tokens (e.g. "go", "id") never score.
const minTokenLen = 3

// compactRe strips every character that is not a letter or digit, producing the
// separator-free compact compound (e.g. "get_user_name" → "getusername"). This
// is the cross-convention bridge: a query "getusername" and a symbol
// "get_user_name" both yield the compact token "getusername" and match under
// exact-token matching where substring matching used to.
var compactRe = regexp.MustCompile(`[^\pL\pN]`)

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

// documentInternal holds the per-field term multisets used for exact-token TF
// and DF computation. Tokenization is symmetric with the query path
// (tokenizeForQuery) so a query "parse_config" and a symbol "parseConfig"
// produce overlapping token sets.
type documentInternal struct {
	path         string         // original path, for IsLowPriorityFile penalty gate
	pathTokens   map[string]int // term multiset for the Path field
	symbolTokens map[string]int // term multiset for the Symbols field
	docTokens    map[string]int // term multiset for the Docs field
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

	// Preprocess documents: tokenize each field once.
	var totalDL float64
	for i, doc := range docs {
		di, dl := tokenizeDocument(doc)
		b.docs[i] = di
		totalDL += dl
	}

	b.avgDL = totalDL / float64(b.n)

	return b
}

// Score computes the BM25F score for a single query term against a document.
// The term is tokenized the same way as the document fields (symmetric
// index/query tokenization), so an identifier-like term "parse_config" scores
// against a symbol "parseConfig" via the shared compact/subword tokens.
//
// TF and document length are computed from the passed doc's own fields; the
// corpus (built by NewBM25F) supplies only corpus-wide IDF and avgDL. This
// makes scoring correct when multiple candidates share the same Path (the
// per-symbol candidate shape from store_bm25.go BM25Search) — each candidate is
// scored against its own tokens, not the first same-path document in the corpus.
func (b *BM25F) Score(term string, doc Document) float64 {
	if b.n == 0 {
		return 0
	}

	di, dl := tokenizeDocument(doc)
	var total float64
	for qtok := range tokenizeForQuery(term) {
		total += b.scoreTermInDoc(qtok, di, dl)
	}
	return total
}

// ScoreTerms computes total BM25F score for multiple query terms.
// Each term is tokenized (symmetric with field tokenization) and the resulting
// query-token set is deduplicated so a token produced by several terms is not
// double-counted. See Score for the per-document TF semantics.
func (b *BM25F) ScoreTerms(terms []string, doc Document) float64 {
	if b.n == 0 || len(terms) == 0 {
		return 0
	}

	di, dl := tokenizeDocument(doc)

	qset := make(map[string]struct{})
	for _, term := range terms {
		for qtok := range tokenizeForQuery(term) {
			qset[qtok] = struct{}{}
		}
	}

	var total float64
	for qtok := range qset {
		total += b.scoreTermInDoc(qtok, di, dl)
	}
	return total
}

// tokenizeDocument tokenizes a Document's fields into per-field term multisets
// and computes its weighted document length. This is the per-document TF/DL
// source for Score/ScoreTerms, replacing the former path-lookup (findDocIndex)
// which was incorrect for same-path candidates.
//
// Field-length policy:
//   - Symbols: token-multiset occurrence count (BM25F field length = term
//     occurrences). A symbol "Deserialize" (1 token) is shorter than
//     "deserialize_u32" (3 tokens), so the exact trait wins on length
//     normalization — the core Rust relevance fix.
//   - Docs: entry count (one doc-comment string = one unit). Doc comments are
//     arbitrary-length natural language; penalizing a thorough comment would
//     make a well-documented file less relevant, which is undesirable.
//   - Path: constant WeightPath baseline (every file has a path; a token-count
//     path length would let short filenames like "a.go" artificially shrink the
//     document and distort normalization).
func tokenizeDocument(doc Document) (documentInternal, float64) {
	di := documentInternal{
		path:         doc.Path,
		pathTokens:   tokenizeField(doc.Path, true),
		symbolTokens: tokenizeSymbolField(doc.Symbols),
		docTokens:    tokenizeDocField(doc.Docs),
	}
	dl := float64(multisetCount(di.symbolTokens))*WeightSymbol +
		float64(len(doc.Docs))*WeightDoc +
		WeightPath
	return di, dl
}

// multisetCount returns the total number of term occurrences in a multiset
// (sum of counts), used as the BM25F symbol field length.
func multisetCount(m map[string]int) int {
	var n int
	for _, c := range m {
		n += c
	}
	return n
}

// documentFrequency returns how many documents contain the term (exact-token
// match against the tokenized fields). Results are cached for repeated queries.
func (b *BM25F) documentFrequency(termLower string) int {
	b.mu.Lock()
	if df, ok := b.dfMap[termLower]; ok {
		b.mu.Unlock()
		return df
	}
	b.mu.Unlock()

	// Compute df by scanning all documents (exact-token match, same as scoring).
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

// scoreTermInDoc computes BM25F score for a single lowercase query token against
// a preprocessed document with its own weighted length. TF and DL are
// per-document (from the passed doc); IDF and avgDL are corpus-wide.
func (b *BM25F) scoreTermInDoc(termLower string, doc documentInternal, dl float64) float64 {
	// Weighted term frequency across fields (tokenized, exact-token match).
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

// computeWeightedTF computes the weighted term frequency for an exact query
// token in a document. tf = Σ_field (term count in field's token multiset ×
// field weight). Test/generated files have their TF divided by
// lowPriorityFilePenalty (Zoekt index/score.go), demoting weak signals.
func computeWeightedTF(doc documentInternal, termLower string) float64 {
	var tf float64
	tf += float64(doc.symbolTokens[termLower]) * WeightSymbol
	tf += float64(doc.docTokens[termLower]) * WeightDoc
	tf += float64(doc.pathTokens[termLower]) * WeightPath
	if tf == 0 {
		return 0
	}
	if langutil.IsLowPriorityFile(doc.path) {
		tf /= lowPriorityFilePenalty
	}
	return tf
}

// documentContainsTerm checks if any field's token multiset contains the term
// (exact-token match). Used for corpus-wide document-frequency (IDF); the
// lowPriorityFilePenalty is NOT applied here — DF reflects term presence, not
// relevance weight.
func documentContainsTerm(doc documentInternal, termLower string) bool {
	if _, ok := doc.pathTokens[termLower]; ok {
		return true
	}
	if _, ok := doc.symbolTokens[termLower]; ok {
		return true
	}
	if _, ok := doc.docTokens[termLower]; ok {
		return true
	}
	return false
}

// tokenizeField builds the term multiset for a single field string:
//   - each alphanumeric word (split on non-letter/digit) is added lowercased
//     (≥ minTokenLen);
//   - lextoken.SplitIdentifier subwords of each word are added (camelCase +
//     snake_case split), reusing the canonical splitter — no new splitter here;
//   - when emitCompact is true, the separator-free lowercased compact compound
//     of the whole string is added (e.g. "get_user_name" → "getusername") so
//     cross-convention substring queries still match after the tokenized switch.
//
// Query and field share this function (via tokenizeForQuery) → symmetric
// tokenization, the core requirement from BM25F arXiv:0911.506 / Lucene /
// tantivy / Zoekt.
func tokenizeField(s string, emitCompact bool) map[string]int {
	tokens := make(map[string]int)
	words := strings.FieldsFunc(s, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	for _, w := range words {
		lower := strings.ToLower(w)
		if len(lower) >= minTokenLen {
			tokens[lower]++
		}
		// Add subwords only when they differ from the whole lowercased word —
		// otherwise a standalone word ("auth") would double-count itself
		// (word + trivial SplitIdentifier result), inflating its TF relative
		// to the same token appearing as a subword of a larger identifier.
		for _, sw := range lextoken.SplitIdentifier(w) {
			if len(sw) >= minTokenLen && sw != lower {
				tokens[sw]++
			}
		}
	}
	// The compact compound is only distinct from the per-word tokens when the
	// string actually split into multiple words (separators present). For a
	// single word the whole lowercased token already IS the compact form, so
	// skip it to avoid a word+compact double count.
	if emitCompact && len(words) > 1 {
		compact := strings.ToLower(compactRe.ReplaceAllString(s, ""))
		if len(compact) >= minTokenLen {
			tokens[compact]++
		}
	}
	return tokens
}

// tokenizeSymbolField tokenizes each symbol name with compact-compound emission
// (whole-identifier + subwords) and merges the per-symbol multisets.
func tokenizeSymbolField(symbols []string) map[string]int {
	out := make(map[string]int)
	for _, sym := range symbols {
		for tok, n := range tokenizeField(sym, true) {
			out[tok] += n
		}
	}
	return out
}

// tokenizeDocField tokenizes each doc-comment string as natural language (word
// tokens + identifier subwords, no compact compound) and merges the multisets.
func tokenizeDocField(docs []string) map[string]int {
	out := make(map[string]int)
	for _, d := range docs {
		for tok, n := range tokenizeField(d, false) {
			out[tok] += n
		}
	}
	return out
}

// tokenizeForQuery tokenizes a query term with the same rules as a symbol field
// (compact compound + subwords), guaranteeing symmetric index/query
// tokenization. A term like "parse_config" yields {parse, config, parseconfig},
// matching a symbol "parseConfig" → {parse, config, parseconfig} on all three.
func tokenizeForQuery(term string) map[string]int {
	return tokenizeField(term, true)
}

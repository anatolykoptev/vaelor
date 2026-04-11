// Package research implements code-research: multi-signal retrieval that combines
// keyword (BM25F), semantic (vector embeddings), import-DAG graph expansion, and
// token-budget pruning to produce a compact, LLM-ready context from a repository.
//
// Pipeline: seeds(BM25F+embed+RRF) → DAG expand → prune → Aider-style map
package research

import "github.com/anatolykoptev/go-code/internal/parser"

// Input holds parameters for a code-research request.
type Input struct {
	// Root is the local path to the (already-cloned) repository.
	Root string

	// Query is the natural-language or keyword query.
	Query string

	// Language limits analysis to files of this language. Optional.
	Language string

	// MaxTokens is the approximate token budget for the output map.
	// 0 uses DefaultMaxTokens.
	MaxTokens int

	// ExpandHops is the number of import-graph hops from seed files.
	// 0 uses DefaultExpandHops.
	ExpandHops int

	// IncludeBody includes full symbol bodies in the output map.
	IncludeBody bool

	// FileGlob restricts analysis to files matching this glob (e.g.
	// "internal/**", "pkg/foo/*.go"). Empty = no filter.
	FileGlob string

	// IncludeTests controls whether *_test.go / test files are indexed.
	// Default false — test files are usually noise for "how does X work"
	// queries. Set true to include them.
	IncludeTests bool
}

// DefaultMaxTokens is the default token budget (~8k tokens ≈ comfortable context).
const DefaultMaxTokens = 8000

// DefaultExpandHops is the default number of import-graph hops.
const DefaultExpandHops = 2

// Result is the output of a code-research request.
type Result struct {
	// Seeds are symbols directly matching the query (from BM25F / embed).
	Seeds []SeedSymbol

	// Graph is the DAG-expanded set of related files, ordered by relevance.
	Graph []LinkedFile

	// Map is the Aider-style compact text representation for LLM consumption.
	Map string

	// EstimatedTokens is the estimated token count of Map.
	EstimatedTokens int

	// PrunedFiles is the number of files dropped by the token-budget pruner.
	PrunedFiles int

	// Mode describes which signals were active: "full", "no-embed", "keyword-only".
	Mode string
}

// SeedSymbol is a symbol that directly matched the query.
type SeedSymbol struct {
	File   string
	Name   string
	Kind   string
	Line   int
	Score  float64 // RRF or BM25F score
	Source string  // "semantic", "keyword", or "hybrid"
}

// LinkedFile is a file reached via import-DAG expansion from a seed.
type LinkedFile struct {
	RelPath   string           // path relative to repo root
	Distance  int              // hop distance from nearest seed (0 = seed file itself)
	WhyLinked string           // human-readable explanation, e.g. "imports seed internal/parser"
	Symbols   []*parser.Symbol // symbols in this file relevant to the query
	Score     float64          // combined relevance score after decay
}

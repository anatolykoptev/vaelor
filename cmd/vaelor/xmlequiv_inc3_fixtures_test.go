package main

// Shared fixtures for the increment-3 XML-formatter migration equivalence tests
// (semantic_search, code_research, repo_analyze quick-local). These are pure
// input builders that reference no migrated-only symbol, so they compile against
// both the pre-migration and post-migration tree. The same fixture drives the
// captured pre-migration golden and the migrated formatter in the equivalence
// test, so any structural divergence surfaces as a tree diff.

import (
	"github.com/anatolykoptev/vaelor/internal/embeddings"
	"github.com/anatolykoptev/vaelor/internal/parser"
	"github.com/anatolykoptev/vaelor/internal/research"
)

// ---- semantic_search ----

func benignSemanticInput() SemanticSearchInput {
	return SemanticSearchInput{Repo: "owner/repo", Query: "jwt token validation"}
}

// benignSemanticResults exercises the two <result> shapes: one with PageRank > 0
// (pagerank attribute present) and one without (source defaults to "semantic").
// All values are benign so the pre-migration escapeXML output is well-formed.
func benignSemanticResults() []embeddings.SearchResult {
	return []embeddings.SearchResult{
		{
			FilePath:   "internal/auth/jwt.go",
			SymbolName: "ValidateToken",
			SymbolKind: "function",
			Language:   "go",
			StartLine:  42,
			Distance:   0.1234,
			Source:     "hybrid",
			PageRank:   0.987654,
		},
		{
			FilePath:   "internal/auth/keys.go",
			SymbolName: "loadKeys",
			SymbolKind: "function",
			Language:   "go",
			StartLine:  10,
			Distance:   0.5000,
			Source:     "", // -> "semantic", no pagerank attr
			PageRank:   0,
		},
	}
}

const benignSemanticStatusMessage = "Repository indexing started in the background. Please retry in 30-60 seconds."

// hostileSemanticInput carries XML-hostile characters in the user query; the
// symbol name in hostileSemanticResults is likewise hostile. The pre-migration
// formatter already escaped these via escapeXML, so its output stays
// well-formed -- this fixture proves the migrated output ROUND-TRIPS (structural
// payload), not a malformed-XML fix.
func hostileSemanticInput() SemanticSearchInput {
	return SemanticSearchInput{Repo: `a&b`, Query: `find <T> where a & b == "x"`}
}

func hostileSemanticResults() []embeddings.SearchResult {
	return []embeddings.SearchResult{
		{
			FilePath:   "internal/gen/list.go",
			SymbolName: `New<T>`,
			SymbolKind: `func<>`,
			Language:   "go",
			StartLine:  7,
			Distance:   0.25,
			Source:     "semantic",
		},
	}
}

// ---- code_research ----

const benignResearchRoot = "/ws/owner_repo"

func benignResearchInput() CodeResearchInput {
	return CodeResearchInput{Repo: "owner/repo", Query: "dag executor"}
}

// benignResearchResult exercises grouped seeds (two symbols in one file), a graph
// file (plus one graph file with no symbols that must be skipped), and a benign
// code map. Single deterministic ordering (already score-descending) keeps the
// output stable.
func benignResearchResult() *research.Result {
	return &research.Result{
		Mode:            "hybrid",
		PrunedFiles:     3,
		EstimatedTokens: 1200,
		Seeds: []research.SeedSymbol{
			{File: "/ws/owner_repo/internal/dag/exec.go", Name: "Run", Kind: "function", Line: 20, Score: 0.9876, Source: "hybrid"},
			{File: "/ws/owner_repo/internal/dag/exec.go", Name: "Executor", Kind: "struct", Line: 10, Score: 0.8000, Source: "semantic"},
			{File: "/ws/owner_repo/internal/dag/node.go", Name: "Node", Kind: "struct", Line: 5, Score: 0.7000, Source: "keyword"},
		},
		Graph: []research.LinkedFile{
			{
				RelPath:   "/ws/owner_repo/internal/dag/plan.go",
				Distance:  1,
				WhyLinked: "imports seed internal/dag",
				Score:     0.5000,
				Symbols:   []*parser.Symbol{{Kind: parser.KindFunction, Name: "Build", StartLine: 15}},
			},
			{
				RelPath:   "/ws/owner_repo/internal/dag/empty.go",
				Distance:  2,
				WhyLinked: "no symbols",
				Score:     0.1000,
				Symbols:   nil, // skipped (len == 0)
			},
		},
		Map: "internal/dag/exec.go  [seed]\n    Executor\n    Run(ctx Context) error\n",
	}
}

// hostileResearchResult puts XML-hostile characters where the pre-migration
// formatter emitted them raw: the <map> body carries a Go signature with <-chan
// and & (raw %s), and the seed path/kind/name carry <, & and > (%q attributes /
// escaped chardata). The pre-migration output is MALFORMED XML.
func hostileResearchResult() *research.Result {
	return &research.Result{
		Mode:            "hybrid",
		PrunedFiles:     0,
		EstimatedTokens: 10,
		Seeds: []research.SeedSymbol{
			{File: "/ws/owner_repo/internal/a&b.go", Name: "Send<T>", Kind: "func<>", Line: 1, Score: 0.5, Source: "sem&kw"},
		},
		Map: "func Send() <-chan T { return ch & mask }",
	}
}

// ---- repo_analyze quick-local ----

const (
	benignQuickRepo   = "myrepo"
	benignQuickTree   = "myrepo/\n  main.go\n  README.md\n"
	benignQuickReadme = "# myrepo\n\nA sample project.\n"
	// quickTreeWithCDATAClose exercises the "]]>" split guard inside CDATA.
	quickTreeWithCDATAClose = "before ]]> after"
	// hostileQuickRepo carries characters the pre-migration `repo=%q` emitted
	// unescaped (& raw, " closing the attribute early) -> malformed XML.
	hostileQuickRepo = `a & b "x"`
)

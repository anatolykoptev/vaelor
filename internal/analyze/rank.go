package analyze

import (
	"context"
	"sort"
	"strings"

	"github.com/anatolykoptev/go-code/internal/goutil"
	"github.com/anatolykoptev/go-code/internal/ingest"
	"github.com/anatolykoptev/go-code/internal/parser"
	"github.com/anatolykoptev/go-code/internal/ranking"
)

// Fusion ranking weights.
const (
	weightBM25  = 0.5
	weightPR    = 0.3
	weightExact = 0.2

	pagerankIters   = 20
	pagerankDamping = 0.85
)

// prioritizeFilesWithScores orders files by multi-signal fusion ranking:
// BM25F (0.5) + Personalized PageRank on reference graph (0.3) + exact symbol match (0.2).
// All signals are min-max normalized before combination.
func prioritizeFilesWithScores(
	root string, files []*ingest.File,
	results []fileParseResult, queryTerms []string,
) ([]*ingest.File, map[string]float64) {
	fileSymbols := buildFileSymbolMap(results)
	fileDocs := buildFileDocMap(results)

	// Signal 1: BM25F text relevance.
	docs := make([]ranking.Document, len(files))
	for i, f := range files {
		docs[i] = ranking.Document{
			Path:    f.RelPath,
			Symbols: fileSymbols[f.RelPath],
			Docs:    fileDocs[f.RelPath],
		}
	}
	scorer := ranking.NewBM25F(docs)
	bm25Scores := make(map[string]float64, len(files))
	for i, f := range files {
		bm25Scores[f.RelPath] = scorer.ScoreTerms(queryTerms, docs[i])
	}

	// Signal 2: Personalized PageRank on identifier-level reference graph.
	allSymbols, allCalls := collectSymbolsAndCalls(results)
	importEdges := buildPageRankGraph(root, results)
	refGraph := ranking.BuildRefGraph(ranking.RefGraphInput{
		Symbols: allSymbols, Calls: allCalls, ImportEdges: importEdges,
	})
	seeds := buildSeeds(fileSymbols, queryTerms)
	prScores := ranking.WeightedPersonalizedPageRank(refGraph.Adjacency(), seeds, pagerankIters, pagerankDamping)

	// Signal 3: Exact symbol name match.
	exactScores := computeExactMatchScores(fileSymbols, queryTerms)

	// Fusion: min-max normalize each signal, then weighted combination.
	fused := ranking.FusionRank([]ranking.Signal{
		{Name: "bm25", Weight: weightBM25, Scores: bm25Scores},
		{Name: "pagerank", Weight: weightPR, Scores: prScores},
		{Name: "exact", Weight: weightExact, Scores: exactScores},
	})

	return sortByScores(files, fused)
}

// collectSymbolsAndCalls aggregates symbols and call sites from all parse results.
func collectSymbolsAndCalls(results []fileParseResult) ([]*parser.Symbol, []parser.CallSite) {
	var symbols []*parser.Symbol
	var calls []parser.CallSite
	for _, pr := range results {
		if pr.result != nil {
			symbols = append(symbols, pr.result.Symbols...)
		}
		calls = append(calls, pr.calls...)
	}
	return symbols, calls
}

// buildSeeds creates a personalization vector for PageRank. Files containing
// symbols that match query terms get higher seed weights (x10 for exact, x1 for substring).
func buildSeeds(fileSymbols map[string][]string, queryTerms []string) map[string]float64 {
	if len(queryTerms) == 0 {
		return nil
	}
	termSet := make(map[string]struct{}, len(queryTerms))
	for _, t := range queryTerms {
		termSet[strings.ToLower(t)] = struct{}{}
	}
	seeds := make(map[string]float64)
	for file, symbols := range fileSymbols {
		for _, sym := range symbols {
			lower := strings.ToLower(sym)
			if _, ok := termSet[lower]; ok {
				seeds[file] += 10.0 //nolint:mnd // exact match boost (Aider uses x10)
				continue
			}
			for term := range termSet {
				if strings.Contains(lower, term) {
					seeds[file] += 1.0
				}
			}
		}
	}
	return seeds
}

// computeExactMatchScores counts exact symbol-name matches per file.
func computeExactMatchScores(fileSymbols map[string][]string, queryTerms []string) map[string]float64 {
	if len(queryTerms) == 0 {
		return nil
	}
	termSet := make(map[string]struct{}, len(queryTerms))
	for _, t := range queryTerms {
		termSet[strings.ToLower(t)] = struct{}{}
	}
	scores := make(map[string]float64)
	for file, symbols := range fileSymbols {
		for _, sym := range symbols {
			if _, ok := termSet[strings.ToLower(sym)]; ok {
				scores[file] += 1.0
			}
		}
	}
	return scores
}

// sortByScores sorts files descending by their fusion scores.
func sortByScores(files []*ingest.File, scores map[string]float64) ([]*ingest.File, map[string]float64) {
	type sf struct {
		file  *ingest.File
		score float64
	}
	scored := make([]sf, 0, len(files))
	for _, f := range files {
		scored = append(scored, sf{file: f, score: scores[f.RelPath]})
	}
	sort.SliceStable(scored, func(i, j int) bool {
		return scored[i].score > scored[j].score
	})
	out := make([]*ingest.File, len(scored))
	for i, s := range scored {
		out[i] = s.file
	}
	return out, scores
}

// buildFileSymbolMap extracts symbol names per file from parse results.
func buildFileSymbolMap(results []fileParseResult) map[string][]string {
	m := make(map[string][]string)
	for _, pr := range results {
		if pr.result == nil {
			continue
		}
		names := make([]string, 0, len(pr.result.Symbols))
		for _, sym := range pr.result.Symbols {
			names = append(names, sym.Name)
		}
		m[pr.file.RelPath] = names
	}
	return m
}

// buildFileDocMap extracts doc-comment strings per file from parse results.
// Only non-empty doc-comments are included to keep the index compact.
func buildFileDocMap(results []fileParseResult) map[string][]string {
	m := make(map[string][]string)
	for _, pr := range results {
		if pr.result == nil {
			continue
		}
		docs := make([]string, 0, len(pr.result.Symbols))
		for _, sym := range pr.result.Symbols {
			if sym.DocComment != "" {
				docs = append(docs, sym.DocComment)
			}
		}
		m[pr.file.RelPath] = docs
	}
	return m
}

// buildPageRankGraph builds a file-level graph for PageRank by lifting the
// package-level import graph. Each file inherits edges from its package:
// if package A imports package B (local), then every file in A links to every file in B.
func buildPageRankGraph(root string, results []fileParseResult) map[string][]string {
	pkgGraph := buildImportGraph(root, results, false)

	pkgFiles := make(map[string][]string)
	for _, pr := range results {
		pkg := goutil.PackageDir(root, pr.file.Path)
		pkgFiles[pkg] = append(pkgFiles[pkg], pr.file.RelPath)
	}

	graph := make(map[string][]string)
	for _, pr := range results {
		pkg := goutil.PackageDir(root, pr.file.Path)
		deps, ok := pkgGraph[pkg]
		if !ok {
			graph[pr.file.RelPath] = nil
			continue
		}
		var targets []string
		for dep := range deps {
			targets = append(targets, resolveImportToFiles(dep, pkgFiles)...)
		}
		graph[pr.file.RelPath] = targets
	}
	return graph
}

// resolveImportToFiles resolves an import path to local files using suffix matching.
func resolveImportToFiles(importPath string, pkgFiles map[string][]string) []string {
	if files, ok := pkgFiles[importPath]; ok {
		return files
	}
	for localPkg, files := range pkgFiles {
		if strings.HasSuffix(importPath, "/"+localPkg) {
			return files
		}
	}
	return nil
}

// SymbolNameSearcher is the narrow interface for pg_trgm symbol name lookup.
// *embeddings.Store satisfies this interface via an adapter in the cmd layer.
type SymbolNameSearcher interface {
	SearchBySymbolName(ctx context.Context, repoKey string, keywords []string, language string, limit int) ([]SymbolHit, error)
}

// SymbolHit is a minimal result from a symbol name search.
// Kept local so the analyze package does not depend on the embeddings package.
type SymbolHit struct {
	FilePath string
}

// BoostBySymbolNames enhances file scores by boosting files containing symbols
// whose names match query keywords via pg_trgm similarity.
// Files housing pg_trgm-matched symbols receive a symbolBoost additive boost.
// Non-fatal: returns unmodified scores on any error or when preconditions unmet.
func BoostBySymbolNames(
	ctx context.Context,
	scores map[string]float64,
	store SymbolNameSearcher,
	repoKey, query, language string,
) map[string]float64 {
	if store == nil || query == "" || repoKey == "" {
		return scores
	}
	kws := extractKeywordsForBoost(query)
	if len(kws) == 0 {
		return scores
	}
	hits, err := store.SearchBySymbolName(ctx, repoKey, kws, language, 30)
	if err != nil || len(hits) == 0 {
		return scores
	}
	boosted := make(map[string]float64, len(scores))
	for k, v := range scores {
		boosted[k] = v
	}
	const symbolBoost = 0.3
	for _, hit := range hits {
		if _, exists := boosted[hit.FilePath]; exists {
			boosted[hit.FilePath] += symbolBoost
		}
		// Files not already in scores were filtered during ingest — skip them
		// to avoid injecting results outside the analyzed file set.
	}
	return boosted
}

// extractKeywordsForBoost splits a query into meaningful keywords for symbol matching,
// removing stopwords and short tokens (min 3 chars). Returns lowercase terms.
func extractKeywordsForBoost(query string) []string {
	stopwords := map[string]bool{
		"the": true, "and": true, "for": true, "that": true, "with": true,
		"this": true, "from": true, "are": true, "not": true, "have": true,
		"function": true, "method": true, "code": true, "file": true,
		"which": true, "where": true, "when": true, "how": true, "what": true,
	}
	seen := make(map[string]bool)
	var keywords []string
	for _, word := range strings.FieldsFunc(strings.ToLower(query), func(r rune) bool {
		return !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'))
	}) {
		if len(word) >= 3 && !stopwords[word] && !seen[word] {
			seen[word] = true
			keywords = append(keywords, word)
		}
	}
	return keywords
}

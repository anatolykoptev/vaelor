package analyze

import (
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
)

// prioritizeFilesWithScores orders files by multi-signal fusion ranking:
// BM25F (0.5) + Personalized PageRank on reference graph (0.3) + exact symbol match (0.2).
// All signals are min-max normalized before combination.
func prioritizeFilesWithScores(
	root string, files []*ingest.File,
	results []fileParseResult, queryTerms []string,
) ([]*ingest.File, map[string]float64) {
	fileSymbols := buildFileSymbolMap(results)

	// Signal 1: BM25F text relevance.
	docs := make([]ranking.Document, len(files))
	for i, f := range files {
		docs[i] = ranking.Document{Path: f.RelPath, Symbols: fileSymbols[f.RelPath]}
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
	prScores := ranking.WeightedPersonalizedPageRank(refGraph.Adjacency(), seeds, 20, 0.85) //nolint:mnd

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

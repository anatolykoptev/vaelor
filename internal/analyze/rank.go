package analyze

import (
	"sort"
	"strings"

	"github.com/anatolykoptev/go-code/internal/goutil"
	"github.com/anatolykoptev/go-code/internal/ingest"
	"github.com/anatolykoptev/go-code/internal/ranking"
)

// prioritizeFilesWithScores orders files by relevance and returns both the
// sorted list and a map of relPath → combined score.
//
// BM25F weighs symbol name matches (x5) and file path matches (x3).
// PageRank propagates importance through package-level import edges, surfacing core files.
// Combined score: 70% BM25F relevance + 30% PageRank importance.
func prioritizeFilesWithScores(root string, files []*ingest.File, results []fileParseResult, queryTerms []string) ([]*ingest.File, map[string]float64) {
	fileSymbols := buildFileSymbolMap(results)
	docs := make([]ranking.Document, len(files))
	for i, f := range files {
		docs[i] = ranking.Document{
			Path:    f.RelPath,
			Symbols: fileSymbols[f.RelPath],
		}
	}
	scorer := ranking.NewBM25F(docs)

	prGraph := buildPageRankGraph(root, results)
	pageRanks := ranking.PageRank(prGraph, 20, 0.85) //nolint:mnd // standard PageRank params

	type scoredFile struct {
		file  *ingest.File
		score float64
	}

	scores := make(map[string]float64, len(files))
	scored := make([]scoredFile, 0, len(files))
	for i, f := range files {
		bm25Score := scorer.ScoreTerms(queryTerms, docs[i])
		prScore := pageRanks[f.RelPath] * 100 //nolint:mnd // normalize PageRank to BM25F magnitude
		combined := bm25Score*0.7 + prScore*0.3 //nolint:mnd // 70% relevance + 30% importance
		scores[f.RelPath] = combined
		scored = append(scored, scoredFile{file: f, score: combined})
	}

	sort.SliceStable(scored, func(i, j int) bool {
		return scored[i].score > scored[j].score
	})

	out := make([]*ingest.File, len(scored))
	for i, sf := range scored {
		out[i] = sf.file
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

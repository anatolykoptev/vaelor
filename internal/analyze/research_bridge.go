package analyze

import (
	"context"
	"fmt"

	"github.com/anatolykoptev/go-code/internal/codesearch"
	"github.com/anatolykoptev/go-code/internal/goutil"
	"github.com/anatolykoptev/go-code/internal/ingest"
	"github.com/anatolykoptev/go-code/internal/langutil"
	"github.com/anatolykoptev/go-code/internal/parser"
)

// ResearchData is the raw analysis output consumed by the research package.
// All fields use exported types so the research package can use them directly.
type ResearchData struct {
	// Root is the resolved repository root path.
	Root string

	// Files is the full list of ingested files.
	Files []*ingest.File

	// FileSymbols maps relPath → symbols for each file.
	FileSymbols map[string][]*parser.Symbol

	// FileImports maps relPath → relPaths of local files it imports.
	// Built from the package-level import graph lifted to file level.
	FileImports map[string][]string

	// PkgFiles maps package dir → relPaths of files in that package.
	PkgFiles map[string][]string

	// FusedScores maps relPath → multi-signal fused score
	// (BM25F 0.5 + Personalized PageRank 0.3 + exact-match 0.2)
	// produced by prioritizeFilesWithScores. NOT raw BM25.
	FusedScores map[string]float64

	// QueryTerms are the extracted terms used for BM25F matching.
	QueryTerms []string
}

// AnalyzeForResearch ingests and parses a repository, returning the raw data
// needed by the research pipeline (symbols, import graph, BM25 scores).
// It is analogous to AnalyzeRepo but returns structured data instead of a
// rendered result, so the research package can apply its own ranking/pruning.
func AnalyzeForResearch(ctx context.Context, root, query, language, fileGlob string, includeTests, includeBody bool, deps Deps) (*ResearchData, error) {
	var langs []string
	if language != "" {
		langs = []string{language}
	}

	ir, err := ingest.IngestRepo(ctx, ingest.IngestOpts{
		Root:         root,
		Languages:    langs,
		MaxFileBytes: deps.maxFileBytes(),
	})
	if err != nil {
		return nil, fmt.Errorf("ingest: %w", err)
	}

	if !includeTests {
		filtered := ir.Files[:0]
		for _, f := range ir.Files {
			if langutil.IsTestFile(f.RelPath) {
				continue
			}
			filtered = append(filtered, f)
		}
		ir.Files = filtered
	}

	if fileGlob != "" {
		filtered := ir.Files[:0]
		for _, f := range ir.Files {
			if codesearch.MatchFileGlob(f.RelPath, fileGlob) {
				filtered = append(filtered, f)
			}
		}
		ir.Files = filtered
	}

	parseResults := parseFilesParallel(ctx, ir.Files, includeBody, deps.ParseCache)

	// Package-level import graph (local packages only).
	pkgGraph := buildImportGraph(root, parseResults, false)

	// Map package dir → file relPaths.
	pkgFiles := make(map[string][]string, len(parseResults))
	for _, pr := range parseResults {
		pkg := goutil.PackageDir(root, pr.file.Path)
		pkgFiles[pkg] = append(pkgFiles[pkg], pr.file.RelPath)
	}

	// Lift package graph to file-level graph.
	fileImports := make(map[string][]string, len(parseResults))
	for pkg, imports := range pkgGraph {
		for _, srcRelPath := range pkgFiles[pkg] {
			for dep := range imports {
				dstFiles := resolveToFiles(dep, pkgFiles)
				fileImports[srcRelPath] = append(fileImports[srcRelPath], dstFiles...)
			}
		}
	}

	// File → symbols map.
	fileSymbols := make(map[string][]*parser.Symbol, len(parseResults))
	for _, pr := range parseResults {
		if pr.result != nil {
			fileSymbols[pr.file.RelPath] = pr.result.Symbols
		}
	}

	// Fused scores (BM25F + Personalized PageRank + exact-match).
	queryTerms := extractQueryTerms(query)
	_, fusedScores := prioritizeFilesWithScores(root, ir.Files, parseResults, queryTerms)

	// Boost via pg_trgm symbol name matching when available.
	if deps.SymbolBooster != nil && deps.RepoKeyFunc != nil {
		repoKey := deps.RepoKeyFunc(root)
		fusedScores = BoostBySymbolNames(ctx, fusedScores, deps.SymbolBooster, repoKey, query, language)
	}

	return &ResearchData{
		Root:        root,
		Files:       ir.Files,
		FileSymbols: fileSymbols,
		FileImports: fileImports,
		PkgFiles:    pkgFiles,
		FusedScores: fusedScores,
		QueryTerms:  queryTerms,
	}, nil
}

// resolveToFiles resolves an import path to local file relPaths using suffix matching.
func resolveToFiles(importPath string, pkgFiles map[string][]string) []string {
	if files, ok := pkgFiles[importPath]; ok {
		return files
	}
	for localPkg, files := range pkgFiles {
		if len(importPath) > len(localPkg) &&
			importPath[len(importPath)-len(localPkg)-1] == '/' &&
			importPath[len(importPath)-len(localPkg):] == localPkg {
			return files
		}
	}
	return nil
}

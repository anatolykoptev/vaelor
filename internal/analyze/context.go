package analyze

import (
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/anatolykoptev/go-code/internal/goutil"
	"github.com/anatolykoptev/go-code/internal/ingest"
	"github.com/anatolykoptev/go-code/internal/ranking"
)

// Depth level constants.
const (
	DepthOverview = "overview"
	DepthModule   = "module"
	DepthDeep     = "deep"
)

// ValidDepth reports whether d is a recognized analysis depth.
func ValidDepth(d string) bool {
	switch d {
	case "", DepthOverview, DepthModule, DepthDeep:
		return true
	default:
		return false
	}
}

// ContextData holds mechanically-extracted analysis data: ranking, import graph,
// scores, and file metadata. Consumed directly by the XML output layer.
type ContextData struct {
	RankedFiles  []*ingest.File     // files ordered by relevance
	FileScores   map[string]float64 // relPath → BM25F+PageRank combined score
	ImportGraph  importGraph        // package-level import adjacency
	ImportedBy   map[string]int     // relPath → imported-by count
	SymbolCounts map[string]int     // relPath → symbol count
	FileTree     string             // rendered directory tree
	QueryTerms   []string           // extracted search terms
	Languages    map[string]int     // language → file count
}

// buildContextData extracts ranking, import graph, scores, and other
// mechanical analysis data from ingest + parse results.
func buildContextData(ir *ingest.IngestResult, results []fileParseResult, query string) *ContextData {
	queryTerms := extractQueryTerms(query)
	rankedFiles, fileScores := prioritizeFilesWithScores(ir.Root, ir.Files, results, queryTerms)
	ig := buildImportGraph(ir.Root, results, false)
	importedBy := computeImportedByCounts(ir.Root, results)
	symbolCounts := computeSymbolCounts(results)
	fileTree := ingest.RenderTree(ir.Files)

	languages := make(map[string]int)
	for _, f := range ir.Files {
		if f.Language != "" {
			languages[f.Language]++
		}
	}

	return &ContextData{
		RankedFiles:  rankedFiles,
		FileScores:   fileScores,
		ImportGraph:  ig,
		ImportedBy:   importedBy,
		SymbolCounts: symbolCounts,
		FileTree:     fileTree,
		QueryTerms:   queryTerms,
		Languages:    languages,
	}
}

// computeSymbolCounts returns the number of symbols in each file (by RelPath).
func computeSymbolCounts(results []fileParseResult) map[string]int {
	counts := make(map[string]int)
	for _, pr := range results {
		if pr.result != nil {
			counts[pr.file.RelPath] = len(pr.result.Symbols)
		}
	}
	return counts
}

// computeImportedByCounts returns how many packages import the package of each file.
// Uses the package-level import graph with suffix matching for import resolution.
func computeImportedByCounts(root string, results []fileParseResult) map[string]int {
	pkgGraph := buildImportGraph(root, results, false)

	// Collect local package names.
	localPkgs := make(map[string]struct{})
	for _, pr := range results {
		localPkgs[goutil.PackageDir(root, pr.file.Path)] = struct{}{}
	}

	// Build reverse index: for each local package, how many packages import it.
	pkgImportedBy := make(map[string]int)
	for _, deps := range pkgGraph {
		for dep := range deps {
			if resolved := resolveImportToPkg(dep, localPkgs); resolved != "" {
				pkgImportedBy[resolved]++
			}
		}
	}

	// Map to files: each file gets its package's imported-by count.
	counts := make(map[string]int)
	for _, pr := range results {
		pkg := goutil.PackageDir(root, pr.file.Path)
		if n := pkgImportedBy[pkg]; n > 0 {
			counts[pr.file.RelPath] = n
		}
	}
	return counts
}

// resolveImportToPkg resolves an import path to a local package name using suffix matching.
func resolveImportToPkg(importPath string, localPkgs map[string]struct{}) string {
	if _, ok := localPkgs[importPath]; ok {
		return importPath
	}
	for pkg := range localPkgs {
		if strings.HasSuffix(importPath, "/"+pkg) {
			return pkg
		}
	}
	return ""
}

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

// nonAlphanumRe matches characters that are not letters, digits, or underscores.
var nonAlphanumRe = regexp.MustCompile(`[^\w]`)

// splitCamelCase splits a camelCase or PascalCase identifier into lowercase subwords.
func splitCamelCase(s string) []string {
	if s == "" {
		return nil
	}
	var parts []string
	runes := []rune(s)
	start := 0

	for i := 1; i < len(runes); i++ {
		if isCamelBoundary(runes, i) {
			part := strings.ToLower(string(runes[start:i]))
			if len(part) >= 2 { //nolint:mnd // minimum subword length
				parts = append(parts, part)
			}
			start = i
		}
	}

	if start < len(runes) {
		part := strings.ToLower(string(runes[start:]))
		if len(part) >= 2 { //nolint:mnd // minimum subword length
			parts = append(parts, part)
		}
	}

	return parts
}

// isCamelBoundary returns true if position i in runes is a camelCase split point.
func isCamelBoundary(runes []rune, i int) bool {
	prev, cur := runes[i-1], runes[i]

	if unicode.IsLower(prev) && unicode.IsUpper(cur) {
		return true
	}
	if unicode.IsLetter(prev) && unicode.IsDigit(cur) {
		return true
	}
	if unicode.IsDigit(prev) && unicode.IsLetter(cur) {
		return true
	}
	if unicode.IsUpper(prev) && unicode.IsUpper(cur) && i+1 < len(runes) && unicode.IsLower(runes[i+1]) {
		return true
	}
	return false
}

// splitIdentifier splits an identifier on underscores, then splits each part by camelCase.
func splitIdentifier(s string) []string {
	snakeParts := strings.Split(s, "_")
	var result []string

	for _, part := range snakeParts {
		if part == "" {
			continue
		}
		camelParts := splitCamelCase(part)
		result = append(result, camelParts...)
	}

	return result
}

// extractQueryTerms splits the query into lowercase alphanumeric terms for matching.
func extractQueryTerms(query string) []string {
	seen := make(map[string]struct{})
	var terms []string

	addTerm := func(t string) {
		if _, ok := seen[t]; !ok && len(t) >= 3 { //nolint:mnd // minimum term length to avoid noise
			seen[t] = struct{}{}
			terms = append(terms, t)
		}
	}

	rawWords := strings.Fields(query)

	for _, raw := range rawWords {
		lower := strings.ToLower(raw)
		cleaned := nonAlphanumRe.ReplaceAllString(lower, "")
		if len(cleaned) >= 3 { //nolint:mnd // minimum term length to avoid noise
			addTerm(cleaned)
		}
	}

	for _, raw := range rawWords {
		cleaned := nonAlphanumRe.ReplaceAllString(raw, "")
		if len(cleaned) < 3 { //nolint:mnd // minimum term length to avoid noise
			continue
		}
		subwords := splitIdentifier(cleaned)
		for _, sw := range subwords {
			addTerm(sw)
		}
	}

	return terms
}

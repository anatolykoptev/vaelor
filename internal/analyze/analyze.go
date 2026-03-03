// Package analyze provides analysis orchestration for MCP tool handlers.
//
// It wires together the ingest, parser, and ranking packages into
// high-level operations that correspond to the MCP tools:
//   - AnalyzeRepo: mechanical repo analysis (AST, ranking, import graph)
//   - BuildDepGraph: construct and render the dependency graph
//   - SearchSymbols: query the symbol index across a repo
package analyze

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"

	kitcache "github.com/anatolykoptev/go-kit/cache"
	"github.com/anatolykoptev/go-kit/llm"

	"github.com/anatolykoptev/go-code/internal/cache"
	"github.com/anatolykoptev/go-code/internal/github"
	"github.com/anatolykoptev/go-code/internal/goutil"
	"github.com/anatolykoptev/go-code/internal/ingest"
	"github.com/anatolykoptev/go-code/internal/parser"
	"github.com/anatolykoptev/go-code/internal/search"
)

// defaultMaxFileBytes is the default maximum file size for parsing (512 KB).
const defaultMaxFileBytes = 512 * 1024

// PathMapping maps an external filesystem prefix to a container-internal prefix.
type PathMapping struct {
	External string
	Internal string
}

// Deps holds injected dependencies for analysis operations.
type Deps struct {
	// LLM is the client used for natural-language queries.
	LLM *llm.Client

	// MaxFileBytes is the max file size to parse. 0 uses the default.
	MaxFileBytes int64

	// GithubToken is the optional GitHub token for cloning private repos.
	GithubToken string

	// WorkspaceDir is the directory used for temporary clones.
	WorkspaceDir string

	// PathMappings translates external paths to container-internal paths.
	PathMappings []PathMapping

	// ParseCache caches parsed AST results per file. Optional.
	ParseCache *cache.ParseCache

	// LLMCache caches LLM responses. Optional.
	LLMCache *cache.LLMCache

	// GitHub is the GitHub API client for search operations.
	GitHub *github.Client

	// SearXNG is the SearXNG client for web/repo search.
	SearXNG *search.SearXNGClient

	// ToolCache is a generic cache for tool results (search, etc.).
	ToolCache *kitcache.Cache
}

// maxFileBytes returns the effective file size limit.
func (d Deps) maxFileBytes() int64 {
	if d.MaxFileBytes > 0 {
		return d.MaxFileBytes
	}
	return defaultMaxFileBytes
}

// RepoAnalysisInput is the input for a repository analysis request.
type RepoAnalysisInput struct {
	// Root is the local path to the already-cloned repository.
	Root string

	// Query is the search query for ranking files by relevance.
	Query string

	// Focus limits analysis to a subdirectory or glob pattern.
	Focus string

	// Language limits analysis to files of this language.
	Language string

	// Depth controls analysis depth: overview, module (default), deep.
	Depth string
}

// RepoAnalysisResult is the output of a repository analysis.
type RepoAnalysisResult struct {
	// RepoName is the detected repository name.
	RepoName string

	// Language is the dominant programming language.
	Language string

	// FileCount is the number of files analyzed.
	FileCount int

	// Symbols is a sample of key symbols found (backward compat).
	Symbols []*parser.Symbol

	// Packages is the list of top-level package names.
	Packages []string

	// Files is per-file metadata ordered by relevance.
	Files []AnalyzedFile

	// ImportGraph maps pkg → []imported_pkgs (non-stdlib).
	ImportGraph map[string][]string

	// FileTree is the rendered directory tree.
	FileTree string

	// Languages maps language → file count.
	Languages map[string]int

	// TotalBytes is the total size of all ingested files.
	TotalBytes int64

	// Skipped is the number of files skipped during ingestion.
	Skipped int
}

// AnalyzedFile holds per-file mechanical analysis data.
type AnalyzedFile struct {
	RelPath    string           // path relative to repo root
	Language   string           // detected language
	Size       int64            // file size in bytes
	Lines      int              // line count (from symbol EndLine heuristic)
	Symbols    []*parser.Symbol // all symbols in this file
	Imports    []string         // imports declared in this file
	Relevance  float64          // BM25F+PageRank combined score
	ImportedBy int              // how many packages import this file's package
}

// SymbolSearchInput is the input for a symbol search request.
type SymbolSearchInput struct {
	// Root is the local path to the repository.
	Root string

	// Query is the symbol name or wildcard pattern.
	Query string

	// Kind limits results to this symbol kind.
	Kind parser.NodeKind

	// Language limits results to files of this language.
	Language string

	// IncludeBody includes the full source body in results.
	IncludeBody bool

	// Limit caps the number of results returned. Default 100, max 500.
	Limit int
}

// DepGraphInput is the input for building a dependency graph.
type DepGraphInput struct {
	// Root is the local path to the repository.
	Root string

	// Type selects the graph type.
	// TODO: reserved for future use; currently only "imports" is supported.
	Type string

	// Format controls output format: json, dot, mermaid.
	Format string

	// Focus limits the graph to a specific package.
	Focus string

	// MaxDepth limits traversal depth from the focus node.
	MaxDepth int

	// IncludeStdlib includes standard library imports. Default false.
	IncludeStdlib bool

	// CrossLanguage includes cross-language API route connections between layers.
	// Currently a schema-only field: cross-language dependencies are stored in
	// Apache AGE and queried via code_graph polyglot_overview and layer_deps templates.
	CrossLanguage bool
}

// fileParseResult pairs an ingest.File with its parser output.
type fileParseResult struct {
	file   *ingest.File
	result *parser.ParseResult
	calls  []parser.CallSite // call sites extracted for ranking
	err    error             // non-nil if parsing failed
}

// AnalyzeRepo ingests and analyzes a repository mechanically.
// It wires: ingest → parallel parse → rank → structured result.
// No LLM is involved — all data is extracted from ASTs and ranking algorithms.
func AnalyzeRepo(ctx context.Context, input RepoAnalysisInput, deps Deps) (*RepoAnalysisResult, error) {
	var langs []string
	if input.Language != "" {
		langs = []string{input.Language}
	}

	ingestResult, err := ingest.IngestRepo(ctx, ingest.IngestOpts{
		Root:         input.Root,
		Focus:        input.Focus,
		Languages:    langs,
		MaxFileBytes: deps.maxFileBytes(),
	})
	if err != nil {
		return nil, fmt.Errorf("ingest repo: %w", err)
	}

	parseResults := parseFilesParallel(ctx, ingestResult.Files, false, deps.ParseCache)

	cd := buildContextData(ingestResult, parseResults, input.Query)

	return buildAnalysisResult(input.Root, ingestResult, parseResults, cd), nil
}

// buildAnalysisResult assembles the RepoAnalysisResult from parsed data and ContextData.
func buildAnalysisResult(root string, ir *ingest.IngestResult, results []fileParseResult, cd *ContextData) *RepoAnalysisResult {
	repoName := filepath.Base(root)
	lang := dominantLanguage(ir.Files)
	packages := extractPackages(ir.Files)
	symbols := collectTopSymbols(results)

	// Build per-file index for parse results.
	parseByPath := make(map[string]*parser.ParseResult, len(results))
	for _, pr := range results {
		if pr.result != nil {
			parseByPath[pr.file.RelPath] = pr.result
		}
	}

	// Build AnalyzedFile slice from ranked files + context data.
	analyzedFiles := make([]AnalyzedFile, 0, len(cd.RankedFiles))
	for _, f := range cd.RankedFiles {
		af := AnalyzedFile{
			RelPath:    f.RelPath,
			Language:   f.Language,
			Size:       f.Size,
			Relevance:  cd.FileScores[f.RelPath],
			ImportedBy: cd.ImportedBy[f.RelPath],
		}
		if pr, ok := parseByPath[f.RelPath]; ok {
			af.Symbols = pr.Symbols
			af.Imports = pr.Imports
			// Estimate line count from the last symbol's EndLine.
			for _, sym := range pr.Symbols {
				if int(sym.EndLine) > af.Lines {
					af.Lines = int(sym.EndLine)
				}
			}
		}
		analyzedFiles = append(analyzedFiles, af)
	}

	// Convert importGraph to serializable map[string][]string.
	igExport := make(map[string][]string, len(cd.ImportGraph))
	for pkg, deps := range cd.ImportGraph {
		igExport[pkg] = goutil.SortedSetKeys(deps)
	}

	return &RepoAnalysisResult{
		RepoName:    repoName,
		Language:    lang,
		FileCount:   len(ir.Files),
		Symbols:     symbols,
		Packages:    packages,
		Files:       analyzedFiles,
		ImportGraph: igExport,
		FileTree:    cd.FileTree,
		Languages:   cd.Languages,
		TotalBytes:  ir.TotalBytes,
		Skipped:     ir.SkippedCount,
	}
}

const defaultSymbolLimit = 100
const maxSymbolLimit = 500

// SearchSymbols searches for symbols matching the query across the repository.
func SearchSymbols(ctx context.Context, input SymbolSearchInput) ([]*parser.Symbol, error) {
	var langs []string
	if input.Language != "" {
		langs = []string{input.Language}
	}

	ingestResult, err := ingest.IngestRepo(ctx, ingest.IngestOpts{
		Root:         input.Root,
		Languages:    langs,
		MaxFileBytes: defaultMaxFileBytes,
		ExcludeTests: true,
	})
	if err != nil {
		return nil, fmt.Errorf("ingest repo: %w", err)
	}

	parseResults := parseFilesParallel(ctx, ingestResult.Files, input.IncludeBody, nil)

	pattern, err := wildcardToRegexp(input.Query)
	if err != nil {
		return nil, fmt.Errorf("invalid query pattern: %w", err)
	}

	limit := input.Limit
	if limit <= 0 {
		limit = defaultSymbolLimit
	}
	if limit > maxSymbolLimit {
		limit = maxSymbolLimit
	}

	var matched []*parser.Symbol
	for _, pr := range parseResults {
		if pr.result == nil {
			continue
		}
		for _, sym := range pr.result.Symbols {
			if !matchesSymbol(sym, pattern, input.Kind) {
				continue
			}
			matched = append(matched, sym)
			if len(matched) >= limit {
				return matched, nil
			}
		}
	}
	return matched, nil
}

// BuildDepGraph constructs and renders the dependency graph for a repository.
func BuildDepGraph(ctx context.Context, input DepGraphInput) (string, error) {
	ingestResult, err := ingest.IngestRepo(ctx, ingest.IngestOpts{
		Root:         input.Root,
		Focus:        input.Focus,
		MaxFileBytes: defaultMaxFileBytes,
		ExcludeTests: true,
	})
	if err != nil {
		return "", fmt.Errorf("ingest repo: %w", err)
	}

	parseResults := parseFilesParallel(ctx, ingestResult.Files, false, nil)

	graph := buildImportGraph(ingestResult.Root, parseResults, input.IncludeStdlib)

	if input.Focus != "" {
		graph = filterGraph(graph, input.Focus, input.MaxDepth)
	}

	format := input.Format
	if format == "" {
		format = "mermaid"
	}

	return renderGraph(graph, format)
}

// parseFilesParallel reads and parses all files concurrently using a fixed
// worker pool capped at runtime.NumCPU(). This bounds both goroutine count
// and memory usage regardless of the number of files.
// parseCache may be nil to skip caching.
func parseFilesParallel(ctx context.Context, files []*ingest.File, includeBody bool, parseCache *cache.ParseCache) []fileParseResult {
	results := make([]fileParseResult, len(files))

	workers := runtime.NumCPU()
	if workers < 1 {
		workers = 1
	}

	work := make(chan int, len(files))
	for i := range files {
		work <- i
	}
	close(work)

	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range work {
				if ctx.Err() != nil {
					return
				}
				results[idx] = parseOneFile(files[idx], includeBody, parseCache)
			}
		}()
	}

	wg.Wait()
	return results
}

// parseOneFile reads and parses a single file. Parse failures are non-fatal:
// result is nil and the error is recorded in fileParseResult.err.
// parseCache may be nil to skip caching.
func parseOneFile(file *ingest.File, includeBody bool, parseCache *cache.ParseCache) fileParseResult {
	// Check parse cache before reading the file.
	var modTime, size int64
	if parseCache != nil {
		info, err := os.Stat(file.Path)
		if err != nil {
			return fileParseResult{file: file, err: fmt.Errorf("stat %s: %w", file.Path, err)}
		}
		modTime = info.ModTime().UnixNano()
		size = info.Size()
		if cached := parseCache.Get(file.Path, modTime, size); cached != nil {
			return fileParseResult{file: file, result: cached}
		}
	}

	source, err := os.ReadFile(file.Path)
	if err != nil {
		return fileParseResult{file: file, err: fmt.Errorf("read %s: %w", file.Path, err)}
	}

	pr, err := parser.ParseFile(file.Path, source, parser.ParseOpts{
		Language:       file.Language,
		IncludeBody:    includeBody,
		IncludeImports: true,
	})
	if err != nil {
		return fileParseResult{file: file, err: fmt.Errorf("parse %s: %w", file.Path, err)}
	}

	calls, _ := parser.ExtractCalls(file.Path, source, parser.ParseOpts{Language: file.Language})

	if parseCache != nil {
		parseCache.Put(file.Path, modTime, size, pr)
	}

	return fileParseResult{file: file, result: pr, calls: calls}
}

// matchAllRe matches any non-empty string, used when the query is empty or "*".
var matchAllRe = regexp.MustCompile(".")

// wildcardToRegexp converts a wildcard pattern (using * as glob) to a compiled regexp.
// An empty pattern matches everything.
func wildcardToRegexp(pattern string) (*regexp.Regexp, error) {
	if pattern == "" || pattern == "*" {
		return matchAllRe, nil
	}
	escaped := regexp.QuoteMeta(pattern)
	regexStr := "(?i)^" + strings.ReplaceAll(escaped, `\*`, ".*") + "$"
	return regexp.Compile(regexStr)
}

// matchesSymbol reports whether sym matches the pattern and kind filter.
func matchesSymbol(sym *parser.Symbol, pattern *regexp.Regexp, kind parser.NodeKind) bool {
	if kind != "" && sym.Kind != kind {
		return false
	}
	return pattern.MatchString(sym.Name)
}

// dominantLanguage returns the most common language among the files.
func dominantLanguage(files []*ingest.File) string {
	counts := make(map[string]int)
	for _, f := range files {
		if f.Language != "" {
			counts[f.Language]++
		}
	}
	best := ""
	max := 0
	for lang, count := range counts {
		if count > max {
			max = count
			best = lang
		}
	}
	return best
}

// extractPackages deduplicates directory names from file RelPaths.
func extractPackages(files []*ingest.File) []string {
	seen := make(map[string]struct{})
	for _, f := range files {
		dir := filepath.Dir(f.RelPath)
		if dir == "." {
			dir = "/"
		}
		seen[dir] = struct{}{}
	}
	pkgs := make([]string, 0, len(seen))
	for pkg := range seen {
		pkgs = append(pkgs, pkg)
	}
	sort.Strings(pkgs)
	return pkgs
}

// defaultSymbolSampleSize is the default cap for top-level symbol sampling.
const defaultSymbolSampleSize = 50

func collectTopSymbols(results []fileParseResult) []*parser.Symbol {
	return collectTopSymbolsN(results, defaultSymbolSampleSize)
}

func collectTopSymbolsN(results []fileParseResult, limit int) []*parser.Symbol {
	var symbols []*parser.Symbol
	for _, pr := range results {
		if pr.result == nil {
			continue
		}
		for _, sym := range pr.result.Symbols {
			if sym.Kind == parser.KindFunction || sym.Kind == parser.KindMethod ||
				sym.Kind == parser.KindStruct || sym.Kind == parser.KindInterface ||
				sym.Kind == parser.KindType {
				symbols = append(symbols, sym)
			}
			if len(symbols) >= limit {
				return symbols
			}
		}
	}
	return symbols
}

// importGraph maps a package path to the set of packages it imports.
type importGraph map[string]map[string]struct{}

// buildImportGraph builds a package-level import graph from parse results.
func buildImportGraph(root string, results []fileParseResult, includeStdlib bool) importGraph {
	graph := make(importGraph)

	for _, pr := range results {
		if pr.result == nil || len(pr.result.Imports) == 0 {
			continue
		}
		pkg := goutil.PackageDir(root, pr.file.Path)
		addImports(graph, pkg, pr.result.Imports, includeStdlib)
	}

	return graph
}

// addImports inserts all valid imports for a package into the graph.
func addImports(graph importGraph, pkg string, imports []string, includeStdlib bool) {
	if _, ok := graph[pkg]; !ok {
		graph[pkg] = make(map[string]struct{})
	}
	for _, imp := range imports {
		if imp == "" {
			continue
		}
		if !includeStdlib && goutil.IsStdlibImport(imp) {
			continue
		}
		// Skip self-import (e.g. test files importing their own package).
		if strings.HasSuffix(imp, "/"+pkg) {
			continue
		}
		graph[pkg][imp] = struct{}{}
	}
}

// filterGraph returns a subgraph reachable from focus within maxDepth hops.
// maxDepth <= 0 means no limit.
func filterGraph(graph importGraph, focus string, maxDepth int) importGraph {
	result := make(importGraph)
	visited := make(map[string]int)

	var walk func(node string, depth int)
	walk = func(node string, depth int) {
		if _, seen := visited[node]; seen {
			return
		}
		if maxDepth > 0 && depth > maxDepth {
			return
		}
		visited[node] = depth

		deps, ok := graph[node]
		if !ok {
			return
		}
		result[node] = deps
		for dep := range deps {
			walk(dep, depth+1)
		}
	}

	// Find matching nodes for the focus prefix.
	for pkg := range graph {
		if strings.Contains(pkg, focus) {
			walk(pkg, 0)
		}
	}

	return result
}

// renderGraph formats the import graph in the requested output format.
func renderGraph(graph importGraph, format string) (string, error) {
	switch format {
	case "mermaid", "":
		return renderMermaid(graph), nil
	case "dot":
		return renderDot(graph), nil
	case "json":
		return renderJSON(graph)
	case "summary":
		return renderSummary(graph), nil
	default:
		return "", fmt.Errorf("unsupported format %q: use mermaid, dot, json, or summary", format)
	}
}

// renderMermaid renders the graph as a Mermaid diagram.
func renderMermaid(graph importGraph) string {
	var sb strings.Builder
	sb.WriteString("graph TD\n")

	pkgs := sortedKeys(graph)
	for _, pkg := range pkgs {
		deps := graph[pkg]
		safeFrom := mermaidID(pkg)
		if len(deps) == 0 {
			fmt.Fprintf(&sb, "    %s\n", safeFrom)
			continue
		}
		sortedDeps := goutil.SortedSetKeys(deps)
		for _, dep := range sortedDeps {
			safeDep := mermaidID(dep)
			fmt.Fprintf(&sb, "    %s --> %s\n", safeFrom, safeDep)
		}
	}
	return sb.String()
}

// renderDot renders the graph in Graphviz DOT format.
func renderDot(graph importGraph) string {
	var sb strings.Builder
	sb.WriteString("digraph deps {\n")
	sb.WriteString("    rankdir=LR;\n")

	pkgs := sortedKeys(graph)
	for _, pkg := range pkgs {
		deps := graph[pkg]
		sortedDeps := goutil.SortedSetKeys(deps)
		for _, dep := range sortedDeps {
			fmt.Fprintf(&sb, "    %q -> %q;\n", pkg, dep)
		}
	}
	sb.WriteString("}\n")
	return sb.String()
}

// renderJSON renders the graph as a JSON adjacency list.
func renderJSON(graph importGraph) (string, error) {
	out := make(map[string][]string, len(graph))
	for pkg, deps := range graph {
		out[pkg] = goutil.SortedSetKeys(deps)
	}
	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal graph: %w", err)
	}
	return string(b), nil
}

// mermaidID converts an arbitrary string to a valid Mermaid node ID.
func mermaidID(s string) string {
	s = strings.ReplaceAll(s, "/", "_")
	s = strings.ReplaceAll(s, "-", "_")
	s = strings.ReplaceAll(s, ".", "_")
	return s
}

// sortedKeys returns sorted keys of an importGraph map.
func sortedKeys(m importGraph) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}


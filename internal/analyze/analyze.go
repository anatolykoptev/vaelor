// Package analyze provides analysis orchestration for MCP tool handlers.
//
// It wires together the ingest, parser, clean, and llm packages into
// high-level operations that correspond to the MCP tools:
//   - AnalyzeRepo: full repo analysis answering a natural-language query
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

	"github.com/anatolykoptev/go-code/internal/ingest"
	"github.com/anatolykoptev/go-code/internal/llm"
	"github.com/anatolykoptev/go-code/internal/parser"
)

// defaultMaxFileBytes is the default maximum file size for parsing (512 KB).
const defaultMaxFileBytes = 512 * 1024

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

	// Query is the natural-language question to answer.
	Query string

	// Focus limits analysis to a subdirectory or glob pattern.
	Focus string

	// Language limits analysis to files of this language.
	Language string
}

// RepoAnalysisResult is the output of a repository analysis.
type RepoAnalysisResult struct {
	// Answer is the LLM-generated answer to the query.
	Answer string

	// RepoName is the detected repository name.
	RepoName string

	// Language is the dominant programming language.
	Language string

	// FileCount is the number of files analyzed.
	FileCount int

	// Symbols is a sample of key symbols found.
	Symbols []*parser.Symbol

	// Packages is the list of top-level package names.
	Packages []string
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
}

// fileParseResult pairs an ingest.File with its parser output.
type fileParseResult struct {
	file   *ingest.File
	result *parser.ParseResult
	err    error // non-nil if parsing failed
}

// AnalyzeRepo ingests and analyzes a repository, answering the given query.
// It wires: ingest → parallel parse → LLM context → LLM completion.
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

	parseResults := parseFilesParallel(ctx, ingestResult.Files, false)

	llmCtx := buildLLMContext(ingestResult, parseResults, input.Query)

	answer, err := deps.LLM.Complete(ctx, llm.SystemPromptRepoAnalysis, llmCtx)
	if err != nil {
		return nil, fmt.Errorf("llm complete: %w", err)
	}

	return buildAnalysisResult(input.Root, answer, ingestResult, parseResults), nil
}

// buildAnalysisResult assembles the RepoAnalysisResult from parsed data.
func buildAnalysisResult(root, answer string, ir *ingest.IngestResult, results []fileParseResult) *RepoAnalysisResult {
	repoName := filepath.Base(root)
	lang := dominantLanguage(ir.Files)
	packages := extractPackages(ir.Files)
	symbols := collectTopSymbols(results)

	return &RepoAnalysisResult{
		Answer:    answer,
		RepoName:  repoName,
		Language:  lang,
		FileCount: len(ir.Files),
		Symbols:   symbols,
		Packages:  packages,
	}
}

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
	})
	if err != nil {
		return nil, fmt.Errorf("ingest repo: %w", err)
	}

	parseResults := parseFilesParallel(ctx, ingestResult.Files, input.IncludeBody)

	pattern, err := wildcardToRegexp(input.Query)
	if err != nil {
		return nil, fmt.Errorf("invalid query pattern: %w", err)
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
	})
	if err != nil {
		return "", fmt.Errorf("ingest repo: %w", err)
	}

	parseResults := parseFilesParallel(ctx, ingestResult.Files, false)

	graph := buildImportGraph(ingestResult.Root, parseResults)

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
func parseFilesParallel(ctx context.Context, files []*ingest.File, includeBody bool) []fileParseResult {
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
				results[idx] = parseOneFile(files[idx], includeBody)
			}
		}()
	}

	wg.Wait()
	return results
}

// parseOneFile reads and parses a single file. Parse failures are non-fatal:
// result is nil and the error is recorded in fileParseResult.err.
func parseOneFile(file *ingest.File, includeBody bool) fileParseResult {
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

	return fileParseResult{file: file, result: pr}
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

// collectTopSymbols gathers up to symbolSampleSize representative symbols.
const symbolSampleSize = 50

func collectTopSymbols(results []fileParseResult) []*parser.Symbol {
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
			if len(symbols) >= symbolSampleSize {
				return symbols
			}
		}
	}
	return symbols
}

// importGraph maps a package path to the set of packages it imports.
type importGraph map[string]map[string]struct{}

// buildImportGraph builds a package-level import graph from parse results.
func buildImportGraph(root string, results []fileParseResult) importGraph {
	graph := make(importGraph)

	for _, pr := range results {
		if pr.result == nil || len(pr.result.Imports) == 0 {
			continue
		}

		relPath, err := filepath.Rel(root, pr.file.Path)
		if err != nil {
			relPath = pr.file.Path
		}
		pkg := filepath.Dir(relPath)
		if pkg == "." {
			pkg = filepath.Base(root)
		}

		if _, ok := graph[pkg]; !ok {
			graph[pkg] = make(map[string]struct{})
		}
		for _, imp := range pr.result.Imports {
			if imp != "" {
				graph[pkg][imp] = struct{}{}
			}
		}
	}

	return graph
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
	case "dot":
		return renderDot(graph), nil
	case "json":
		return renderJSON(graph)
	default:
		return renderMermaid(graph), nil
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
		sortedDeps := sortedSetKeys(deps)
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
		sortedDeps := sortedSetKeys(deps)
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
		out[pkg] = sortedSetKeys(deps)
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

// sortedSetKeys returns sorted keys of a set (map[string]struct{}).
func sortedSetKeys(m map[string]struct{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

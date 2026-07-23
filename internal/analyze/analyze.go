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
	"fmt"
	"log/slog"

	"github.com/anatolykoptev/vaelor/internal/ingest"
	"github.com/anatolykoptev/vaelor/internal/parser"
)

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

	// MaxRepoBytes is a per-request override for the total ingested-source
	// cap. >0 overrides deps.maxRepoBytes() (the MAX_REPO_MB default) for this
	// call; 0 falls back to the configured default.
	MaxRepoBytes int64
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

// AnalyzeRepo ingests and analyzes a repository mechanically.
// It wires: ingest → parallel parse → rank → structured result.
// No LLM is involved — all data is extracted from ASTs and ranking algorithms.
func AnalyzeRepo(ctx context.Context, input RepoAnalysisInput, deps Deps) (*RepoAnalysisResult, error) {
	var langs []string
	if input.Language != "" {
		langs = []string{input.Language}
	}

	// Per-request override wins over the configured default; 0 => default.
	maxRepoBytes := deps.maxRepoBytes()
	if input.MaxRepoBytes > 0 {
		maxRepoBytes = input.MaxRepoBytes
	}

	ingestResult, err := ingest.IngestRepo(ctx, ingest.IngestOpts{
		Root:         input.Root,
		Focus:        input.Focus,
		Languages:    langs,
		MaxFileBytes: deps.maxFileBytes(),
		MaxRepoBytes: maxRepoBytes,
	})
	if err != nil {
		return nil, fmt.Errorf("ingest repo: %w", err)
	}
	// The size cap silently drops files; surface it so a truncated analysis
	// isn't mistaken for a complete one (raise via the max_repo_mb arg).
	if n := ingestResult.SkippedReasons["repo_oversize"]; n > 0 {
		slog.Warn("analyze: repo exceeded the size cap — files truncated from analysis; raise max_repo_mb to index them",
			slog.Int("files_skipped", n),
			slog.Int64("cap_bytes", maxRepoBytes),
			slog.Int64("ingested_bytes", ingestResult.TotalBytes),
		)
	}

	parseResults := parseFilesParallel(ctx, ingestResult.Files, false, deps.ParseCache)

	cd := buildContextData(ingestResult, parseResults, input.Query)

	// Boost file scores using pg_trgm symbol name matching when available.
	if deps.SymbolBooster != nil && deps.RepoKeyFunc != nil {
		repoKey := deps.RepoKeyFunc(ingestResult.Root)
		cd.FileScores = BoostBySymbolNames(ctx, cd.FileScores, deps.SymbolBooster, repoKey, input.Query, input.Language)
		// Re-sort RankedFiles to reflect the boosted scores.
		cd.RankedFiles, _ = sortByScores(cd.RankedFiles, cd.FileScores)
	}

	return buildAnalysisResult(input.Root, ingestResult, parseResults, cd), nil
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

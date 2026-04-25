package callgraph

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/anatolykoptev/go-code/internal/goanalysis"
	"github.com/anatolykoptev/go-code/internal/ingest"
	"github.com/anatolykoptev/go-code/internal/parser"
	"github.com/anatolykoptev/go-code/internal/parser/preproc"
)

const maxFileBytes = 512 * 1024

// TraceRepoInput configures a full repo call chain trace.
type TraceRepoInput struct {
	Root     string
	Symbol   string
	Focus    string
	Language string
	Opts     TraceOpts
}

type parseResult struct {
	symbols []*parser.Symbol
	calls   []parser.CallSite
	rels    []parser.TypeRelationship
	src     []byte // raw file bytes, needed for template-ref resolution
	fileRel string // file path relative to repo root
	tplRefs []preproc.TemplateRef
}

// BuildFromRepo ingests a repo, parses files, and returns the call graph
// without tracing a specific symbol.
func BuildFromRepo(ctx context.Context, input TraceRepoInput) (*CallGraph, error) {
	// Check cache first — parsing all repo files is expensive (15-60s on cold start).
	cacheKey := cgCacheKey(input)
	if cached, ok := cgCache.get(cacheKey); ok {
		slog.Debug("callgraph: BuildFromRepo cache hit", slog.String("root", input.Root))
		return cached, nil
	}

	var langs []string
	if input.Language != "" {
		langs = []string{input.Language}
	}

	ir, err := ingest.IngestRepo(ctx, ingest.IngestOpts{
		Root:         input.Root,
		Focus:        input.Focus,
		Languages:    langs,
		MaxFileBytes: maxFileBytes,
	})
	if err != nil {
		return nil, fmt.Errorf("ingest: %w", err)
	}

	results := parseFilesParallel(ctx, ir.Files)

	var allSymbols []*parser.Symbol
	var allCalls []parser.CallSite
	var allRels []parser.TypeRelationship
	for _, r := range results {
		allSymbols = append(allSymbols, r.symbols...)
		allCalls = append(allCalls, r.calls...)
		allRels = append(allRels, r.rels...)
	}

	cg := BuildCallGraph(allSymbols, allCalls)
	cg.TypeRels = allRels
	cg.Tier = "basic"
	cg.Backend = "tree-sitter"

	// Attempt go/types resolution for Go modules — purely additive.
	if goanalysis.HasGoModule(input.Root) {
		if typedCG := tryGoTypesResolution(ctx, input.Root, allSymbols); typedCG != nil {
			cg = MergeCallGraphs(cg, typedCG)
			cg.Tier = "enhanced"
			cg.Backend = "tree-sitter+go/types"
		}
	}

	// Attempt SCIP resolution for non-Go languages (or when go/types failed).
	if cg.Tier == "basic" {
		if scipCG := trySCIPResolution(ctx, input.Root, ir.Files, allSymbols); scipCG != nil {
			cg = MergeCallGraphs(cg, scipCG)
			cg.Tier = "enhanced"
			cg.Backend = "tree-sitter+scip"
		}
	}

	// Inject WordPress hook edges for PHP files.
	hookRoutes := extractHookRoutes(ir.Files)
	if len(hookRoutes) > 0 {
		InjectHookEdges(cg, hookRoutes)
	}

	// Populate UsesIndex from Astro template component references.
	cg.UsesIndex = buildUsesIndex(results, input.Root)

	// Cache the result for subsequent calls within the same session.
	cgCache.set(cacheKey, cg)
	slog.Debug("callgraph: BuildFromRepo cached", slog.String("root", input.Root),
		slog.String("tier", cg.Tier))
	return cg, nil
}

// TraceRepo ingests a repo, extracts symbols and calls, builds call graph, traces from symbol.
func TraceRepo(ctx context.Context, input TraceRepoInput) (*TraceResult, error) {
	g, err := BuildFromRepo(ctx, input)
	if err != nil {
		return nil, err
	}

	result := Trace(ctx, g, input.Symbol, input.Opts)
	result.Tier = g.Tier

	return &result, nil
}

// tryGoTypesResolution attempts to load Go packages and resolve typed call edges.
// Returns nil on any failure — callers fall back to tree-sitter-only graph.
func tryGoTypesResolution(ctx context.Context, root string, tsSymbols []*parser.Symbol) *CallGraph {
	lr, err := goanalysis.LoadPackages(ctx, root, goanalysis.LoadOpts{})
	if err != nil {
		return nil
	}
	typedEdges := goanalysis.Resolve(lr.Packages)
	if len(typedEdges) == 0 {
		return nil
	}
	return ConvertToCallGraph(typedEdges, tsSymbols)
}

// buildUsesIndex resolves Astro template refs from all parse results and returns
// a map from target-file → []using-file (all paths relative to root).
func buildUsesIndex(results []parseResult, root string) map[string][]string {
	idx := make(map[string][]string)
	for _, r := range results {
		if len(r.tplRefs) == 0 {
			continue
		}
		for _, u := range ResolveTemplateRefs(r.src, r.tplRefs, r.fileRel, root) {
			idx[u.To] = append(idx[u.To], u.From)
		}
	}
	if len(idx) == 0 {
		return nil
	}
	return idx
}

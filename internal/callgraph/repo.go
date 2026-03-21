package callgraph

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"sync"

	"github.com/anatolykoptev/go-code/internal/goanalysis"
	"github.com/anatolykoptev/go-code/internal/ingest"
	"github.com/anatolykoptev/go-code/internal/parser"
	"github.com/anatolykoptev/go-code/internal/routes"
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
}

// BuildFromRepo ingests a repo, parses files, and returns the call graph
// without tracing a specific symbol.
func BuildFromRepo(ctx context.Context, input TraceRepoInput) (*CallGraph, error) {
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

	return cg, nil
}

// TraceRepo ingests a repo, extracts symbols and calls, builds call graph, traces from symbol.
func TraceRepo(ctx context.Context, input TraceRepoInput) (*TraceResult, error) {
	g, err := BuildFromRepo(ctx, input)
	if err != nil {
		return nil, err
	}

	result := Trace(g, input.Symbol, input.Opts)
	result.Tier = g.Tier

	return &result, nil
}

func parseFilesParallel(ctx context.Context, files []*ingest.File) []parseResult {
	results := make([]parseResult, len(files))

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
				results[idx] = parseFileForCalls(files[idx])
			}
		}()
	}

	wg.Wait()
	return results
}

// extractHookRoutes collects WordPress hook routes from PHP files.
func extractHookRoutes(files []*ingest.File) []HookRoute {
	var out []HookRoute
	for _, f := range files {
		if f.Language != "php" {
			continue
		}
		src, err := os.ReadFile(f.Path)
		if err != nil {
			continue
		}
		for _, r := range routes.ExtractAll("php", src) {
			if r.Framework != "wordpress" {
				continue
			}
			out = append(out, HookRoute{
				Method:  r.Method,
				Path:    r.Path,
				Handler: r.Handler,
				Side:    r.Side,
				Line:    r.Line,
			})
		}
	}
	return out
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

func parseFileForCalls(file *ingest.File) parseResult {
	source, err := os.ReadFile(file.Path)
	if err != nil {
		return parseResult{}
	}

	opts := parser.ParseOpts{
		Language:       file.Language,
		IncludeBody:    true,
		IncludeImports: true,
	}

	pr, err := parser.ParseFile(file.Path, source, opts)
	if err != nil {
		return parseResult{}
	}

	calls, _ := parser.ExtractCalls(file.Path, source, opts)
	rels, _ := parser.ExtractRelationships(file.Path, source, opts)

	return parseResult{symbols: pr.Symbols, calls: calls, rels: rels}
}

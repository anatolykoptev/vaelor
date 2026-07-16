package callgraph

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/anatolykoptev/go-code/internal/ingest"
	"github.com/anatolykoptev/go-code/internal/parser"
)

// PipelineResult is the output of the unified ingest→parse→build→enrich
// pipeline (BuildAndEnrich). It carries everything a consumer needs:
//   - CG: the enriched call graph (tree-sitter + go/types + SCIP + IMPLEMENTS)
//   - Symbols: all parsed symbols (for analyze, embeddings)
//   - Files: the ingested file list (for explore, compare, codesearch)
//   - Rels: type relationships (IMPLEMENTS + INHERITS) from enrichment
//   - FileImports: per-file import paths (for code_graph's import edges)
//   - TemplateRefs: resolved Astro component tag refs (for code_graph's USES edges)
//   - HookRoutes: WordPress hook routes (for code_graph's HOOK edges)
//
// See issue #463. This is the single shared pipeline that subsumes the
// previously-duplicated ingest→parse→build→enrich sequences in
// BuildFromRepo (call_trace) and ingestAndParse+buildAGECallGraph (code_graph).
type PipelineResult struct {
	CG           *CallGraph
	Symbols      []*parser.Symbol
	Files        []*ingest.File
	Rels         []parser.TypeRelationship
	FileImports  map[string][]string
	TemplateRefs []TemplateRef
	HookRoutes   []HookRoute
}

// TemplateRef is a resolved Astro USES relationship ready for AGE insertion.
// Resolution (tag name → file path) is performed in BuildAndEnrich via
// ResolveTemplateRefs; unresolved refs are dropped before storage.
type TemplateRef struct {
	RelFile    string // Astro file that contains the tag usage
	ResolvedTo string // relative path of the imported component file
	Line       uint32
}

// PipelineOpts controls the unified pipeline behaviour.
type PipelineOpts struct {
	Root               string
	Focus              string
	Language           string
	IncludeFieldAccess bool
	MaxFileBytes       int64
	ExcludeTests       bool
	// TypedEnrich controls whether EnrichWithTypedResolution runs.
	// When false, the pipeline returns a tree-sitter-only graph (used by
	// code_graph when CODEGRAPH_TYPED_ENRICH is unset).
	TypedEnrich bool
}

// BuildAndEnrich is the single shared ingest→parse→build→enrich pipeline.
//
// It replaces the duplicated sequences in BuildFromRepo (call_trace) and
// ingestAndParse+buildAGECallGraph (code_graph) with one code path:
//
//	ingest.IngestRepo (cached — #464)
//	→ parseFilesParallel (unified — #469)
//	→ BuildCallGraphWithOpts
//	→ EnrichWithTypedResolution (go/types + SCIP + IMPLEMENTS — #467)
//	→ return *PipelineResult
//
// Consumers:
//   - call_trace: traces over result.CG in memory
//   - code_graph: persists result to AGE via buildGraph
//   - analyze: reads result.Symbols + result.Files
//
// See issue #463.
func BuildAndEnrich(ctx context.Context, opts PipelineOpts) (*PipelineResult, error) {
	var langs []string
	if opts.Language != "" {
		langs = []string{opts.Language}
	}

	ir, err := ingest.IngestRepo(ctx, ingest.IngestOpts{
		Root:         opts.Root,
		Focus:        opts.Focus,
		Languages:    langs,
		MaxFileBytes: opts.MaxFileBytes,
		ExcludeTests: opts.ExcludeTests,
	})
	if err != nil {
		return nil, fmt.Errorf("ingest: %w", err)
	}

	results := parseFilesParallel(ctx, ir.Files)

	var allSymbols []*parser.Symbol
	var allCalls []parser.CallSite
	var allRels []parser.TypeRelationship
	fileImports := make(map[string][]string)
	var tplRefs []TemplateRef
	for _, r := range results {
		allSymbols = append(allSymbols, r.symbols...)
		allCalls = append(allCalls, r.calls...)
		allRels = append(allRels, r.rels...)
		if len(r.imports) > 0 {
			fileImports[r.fileRel] = r.imports
		}
		// Resolve template refs to file paths immediately; unresolved refs are dropped.
		for _, u := range ResolveTemplateRefs(r.src, r.tplRefs, r.fileRel, opts.Root) {
			tplRefs = append(tplRefs, TemplateRef{
				RelFile:    u.From,
				ResolvedTo: u.To,
				Line:       u.Line,
			})
		}
	}

	cg := BuildCallGraphWithOpts(allSymbols, allCalls, BuildOpts{
		IncludeFieldAccess: opts.IncludeFieldAccess,
	})
	cg.TypeRels = allRels
	cg.Tier = "basic"
	cg.Backend = BackendTreeSitter

	if opts.TypedEnrich {
		cg = EnrichWithTypedResolution(ctx, opts.Root, cg, allSymbols, ir.Files)
	}

	// Inject WordPress hook edges for PHP files.
	hookRoutes := extractHookRoutes(ir.Files)
	if len(hookRoutes) > 0 {
		InjectHookEdges(cg, hookRoutes)
	}

	// Populate UsesIndex from Astro template component references.
	cg.UsesIndex = buildUsesIndex(results, opts.Root)

	slog.Debug("callgraph: BuildAndEnrich done",
		slog.String("root", opts.Root),
		slog.String("tier", cg.Tier),
		slog.String("backend", string(cg.Backend)),
		slog.Int("symbols", len(allSymbols)),
		slog.Int("edges", len(cg.Edges)),
		slog.Int("files", len(ir.Files)))

	return &PipelineResult{
		CG:           cg,
		Symbols:      allSymbols,
		Files:        ir.Files,
		Rels:         cg.TypeRels,
		FileImports:  fileImports,
		TemplateRefs: tplRefs,
		HookRoutes:   hookRoutes,
	}, nil
}

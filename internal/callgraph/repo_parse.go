package callgraph

import (
	"context"
	"os"

	"github.com/anatolykoptev/go-code/internal/ingest"
	"github.com/anatolykoptev/go-code/internal/parser"
	"github.com/anatolykoptev/go-code/internal/routes"
)

// parseFilesParallel reads and parses all files concurrently via the shared
// ingest.ParseFilesParallel, then adapts the results into parseResult (which
// carries the raw source bytes needed for template-ref resolution and
// buildUsesIndex). See issue #469.
func parseFilesParallel(ctx context.Context, files []*ingest.File) []parseResult {
	results := ingest.ParseFilesParallel(ctx, files, parser.ParseOpts{
		IncludeBody:     true,
		IncludeImports:  true,
		IncludeTypeRels: true,
	}, nil)

	out := make([]parseResult, len(results))
	for i, r := range results {
		if r.Err != nil || r.Result == nil {
			out[i] = parseResult{}
			continue
		}
		out[i] = parseResult{
			symbols: r.Result.Symbols,
			calls:   r.Calls,
			rels:    r.Result.TypeRels,
			imports: r.Result.Imports,
			src:     r.Raw,
			fileRel: r.File.RelPath,
			tplRefs: r.Result.TemplateRefs,
		}
	}
	return out
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

package callgraph

import (
	"context"
	"os"
	"runtime"
	"sync"

	"github.com/anatolykoptev/go-code/internal/ingest"
	"github.com/anatolykoptev/go-code/internal/parser"
	"github.com/anatolykoptev/go-code/internal/routes"
)

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

func parseFileForCalls(file *ingest.File) parseResult {
	source, err := os.ReadFile(file.Path)
	if err != nil {
		return parseResult{}
	}

	opts := parser.ParseOpts{
		Language:        file.Language,
		IncludeBody:     true,
		IncludeImports:  true,
		IncludeTypeRels: true,
	}

	pr, err := parser.ParseFile(file.Path, source, opts)
	if err != nil {
		return parseResult{}
	}

	calls, _ := parser.ExtractCalls(file.Path, source, opts)
	rels := pr.TypeRels

	return parseResult{
		symbols: pr.Symbols,
		calls:   calls,
		rels:    rels,
		src:     source,
		fileRel: file.RelPath,
		tplRefs: pr.TemplateRefs,
	}
}

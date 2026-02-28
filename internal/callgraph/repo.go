package callgraph

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"sync"

	"github.com/anatolykoptev/go-code/internal/ingest"
	"github.com/anatolykoptev/go-code/internal/parser"
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
}

// TraceRepo ingests a repo, extracts symbols and calls, builds call graph, traces from symbol.
func TraceRepo(ctx context.Context, input TraceRepoInput) (*TraceResult, error) {
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
	for _, r := range results {
		allSymbols = append(allSymbols, r.symbols...)
		allCalls = append(allCalls, r.calls...)
	}

	g := BuildCallGraph(allSymbols, allCalls)
	result := Trace(g, input.Symbol, input.Opts)

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

	return parseResult{symbols: pr.Symbols, calls: calls}
}

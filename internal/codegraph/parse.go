package codegraph

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"runtime"
	"sync"

	"github.com/anatolykoptev/go-code/internal/callgraph"
	"github.com/anatolykoptev/go-code/internal/ingest"
	"github.com/anatolykoptev/go-code/internal/parser"
)

// indexParseResult holds symbols, calls, imports, type relationships, and
// template refs parsed from one file.
type indexParseResult struct {
	file         *ingest.File
	symbols      []*parser.Symbol
	calls        []parser.CallSite
	imports      []string
	rels         []parser.TypeRelationship
	templateRefs []templateFileRef
}

// templateFileRef is a resolved Astro USES relationship ready for AGE insertion.
// Resolution (tag name → file path) is performed in indexParseFile via
// callgraph.ResolveTemplateRefs; unresolved refs are dropped before storage.
type templateFileRef struct {
	relFile    string // Astro file that contains the tag usage
	resolvedTo string // relative path of the imported component file
	line       uint32
}

// ingestAndParse ingests a repository and parses all files in parallel.
// The returned map associates each file's relative path with the import paths
// declared in that file.
func ingestAndParse(ctx context.Context, root string) ([]*ingest.File, []*parser.Symbol, []parser.CallSite, map[string][]string, []parser.TypeRelationship, []templateFileRef, error) {
	ir, err := ingest.IngestRepo(ctx, ingest.IngestOpts{
		Root:         root,
		MaxFileBytes: maxIndexFileBytes,
	})
	if err != nil {
		return nil, nil, nil, nil, nil, nil, fmt.Errorf("ingest repo: %w", err)
	}

	results := indexParseParallel(ctx, root, ir.Files)

	var allFiles []*ingest.File
	var allSymbols []*parser.Symbol
	var allCalls []parser.CallSite
	var allRels []parser.TypeRelationship
	var allTplRefs []templateFileRef
	fileImports := make(map[string][]string)

	for _, r := range results {
		if r.file == nil {
			continue
		}
		allFiles = append(allFiles, r.file)
		allSymbols = append(allSymbols, r.symbols...)
		allCalls = append(allCalls, r.calls...)
		allRels = append(allRels, r.rels...)
		allTplRefs = append(allTplRefs, r.templateRefs...)
		if len(r.imports) > 0 {
			fileImports[r.file.RelPath] = r.imports
		}
	}

	return allFiles, allSymbols, allCalls, fileImports, allRels, allTplRefs, nil
}

// indexParseParallel parses all files concurrently and returns results.
func indexParseParallel(ctx context.Context, root string, files []*ingest.File) []indexParseResult {
	results := make([]indexParseResult, len(files))

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
				results[idx] = indexParseFile(root, files[idx])
			}
		}()
	}

	wg.Wait()
	return results
}

func indexParseFile(root string, f *ingest.File) indexParseResult {
	source, err := os.ReadFile(f.Path)
	if err != nil {
		return indexParseResult{}
	}

	opts := parser.ParseOpts{
		Language:       f.Language,
		IncludeImports: true,
		IncludeBody:    true,
	}

	pr, err := parser.ParseFile(f.Path, source, opts)
	if err != nil {
		return indexParseResult{file: f}
	}

	calls, callErr := parser.ExtractCalls(f.Path, source, opts)
	if callErr != nil {
		slog.Debug("codegraph: extract calls failed", slog.String("file", f.Path), slog.Any("error", callErr))
	}
	rels, relErr := parser.ExtractRelationships(f.Path, source, opts)
	if relErr != nil {
		slog.Debug("codegraph: extract relationships failed", slog.String("file", f.Path), slog.Any("error", relErr))
	}

	// Resolve template refs to file paths immediately; unresolved refs are dropped.
	var tplRefs []templateFileRef
	for _, u := range callgraph.ResolveTemplateRefs(source, pr.TemplateRefs, f.RelPath, root) {
		tplRefs = append(tplRefs, templateFileRef{
			relFile:    u.From,
			resolvedTo: u.To,
			line:       u.Line,
		})
	}

	return indexParseResult{
		file:         f,
		symbols:      pr.Symbols,
		calls:        calls,
		imports:      pr.Imports,
		rels:         rels,
		templateRefs: tplRefs,
	}
}

// estimateLines returns an estimate of line count based on file size.
func estimateLines(f *ingest.File) int {
	const avgBytesPerLine = 40
	if f.Size <= 0 {
		return 0
	}
	n := int(f.Size) / avgBytesPerLine
	if n < 1 {
		n = 1
	}
	return n
}

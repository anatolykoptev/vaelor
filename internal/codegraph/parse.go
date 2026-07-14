package codegraph

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
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
	skipReason   string // non-empty when file was dropped at parse stage
}

// templateFileRef is a resolved Astro USES relationship ready for AGE insertion.
// Resolution (tag name → file path) is performed in indexParseFile via
// callgraph.ResolveTemplateRefs; unresolved refs are dropped before storage.
type templateFileRef struct {
	relFile    string // Astro file that contains the tag usage
	resolvedTo string // relative path of the imported component file
	line       uint32
}

// read_error skip-reason keys, broken out by errno class for operational
// diagnostics. "read_error_missing" (ENOENT) is the signature of the
// WalkDir-vs-clone-swap race; ops can grep structured logs for it directly.
const (
	skipReasonReadMissing = "read_error_missing" // fs.ErrNotExist — vanished between WalkDir and ReadFile
	skipReasonReadPerm    = "read_error_perm"    // fs.ErrPermission — process lacks read access
	skipReasonReadOther   = "read_error_other"   // any other I/O error
)

// ingestAndParse ingests a repository and parses all files in parallel.
// The returned map associates each file's relative path with the import paths
// declared in that file. parseSkipped tallies files dropped at the parse stage
// per reason (e.g. "read_error_missing", "read_error_perm", "read_error_other",
// "parse_error").
func ingestAndParse(ctx context.Context, root string) ([]*ingest.File, []*parser.Symbol, []parser.CallSite, map[string][]string, []parser.TypeRelationship, []templateFileRef, map[string]int, error) {
	ir, err := ingest.IngestRepo(ctx, ingest.IngestOpts{
		Root:         root,
		MaxFileBytes: maxIndexFileBytes,
	})
	if err != nil {
		return nil, nil, nil, nil, nil, nil, nil, fmt.Errorf("ingest repo: %w", err)
	}

	results := indexParseParallel(ctx, root, ir.Files)

	var allFiles []*ingest.File
	var allSymbols []*parser.Symbol
	var allCalls []parser.CallSite
	var allRels []parser.TypeRelationship
	var allTplRefs []templateFileRef
	fileImports := make(map[string][]string)
	parseSkipped := make(map[string]int)

	// Merge ingest-stage skip reasons into parseSkipped for unified reporting.
	for reason, count := range ir.SkippedReasons {
		parseSkipped[reason] += count
	}

	for _, r := range results {
		if r.skipReason != "" {
			parseSkipped[r.skipReason]++
			continue
		}
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

	return allFiles, allSymbols, allCalls, fileImports, allRels, allTplRefs, parseSkipped, nil
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

// indexParseFile reads and parses a single source file.
// On os.ReadFile failure the error is classified by errno and returned as a
// skipReason key (skipReasonRead*) so callers can distinguish race-induced
// ENOENT from permission errors and other I/O failures.
func indexParseFile(root string, f *ingest.File) indexParseResult {
	source, err := os.ReadFile(f.Path)
	if err != nil {
		reason := classifyReadError(err)
		switch reason {
		case skipReasonReadMissing:
			// ENOENT: file disappeared between WalkDir (T1) and ReadFile (T2).
			// Classic signature of the WalkDir-vs-RemoveAll race. Emit Warn
			// (not Debug) so ops can spot it without enabling verbose logging.
			slog.Warn("codegraph: file vanished between walk and read — possible clone-swap race",
				slog.String("file", f.Path),
				slog.String("skip_reason", reason))
		case skipReasonReadPerm:
			slog.Warn("codegraph: file read permission denied",
				slog.String("file", f.Path),
				slog.String("skip_reason", reason))
		default:
			slog.Warn("codegraph: file read error",
				slog.String("file", f.Path),
				slog.String("skip_reason", reason),
				slog.Any("error", err))
		}
		return indexParseResult{skipReason: reason}
	}

	opts := parser.ParseOpts{
		Language:        f.Language,
		IncludeImports:  true,
		IncludeBody:     true,
		IncludeTypeRels: true,
	}

	pr, err := parser.ParseFile(f.Path, source, opts)
	if err != nil {
		slog.Debug("codegraph: parse failed", slog.String("file", f.Path), slog.String("language", f.Language), slog.Any("error", err))
		return indexParseResult{file: f, skipReason: "parse_error"}
	}

	calls, callErr := parser.ExtractCalls(f.Path, source, opts)
	if callErr != nil {
		slog.Debug("codegraph: extract calls failed", slog.String("file", f.Path), slog.Any("error", callErr))
	}
	rels := pr.TypeRels

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

// classifyReadError maps an os.ReadFile error to a skipReason key.
func classifyReadError(err error) string {
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return skipReasonReadMissing
	case errors.Is(err, fs.ErrPermission):
		return skipReasonReadPerm
	default:
		return skipReasonReadOther
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

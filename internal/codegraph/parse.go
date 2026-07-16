package codegraph

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"

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
// Resolution (tag name → file path) is performed in ingestAndParse via
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

// indexParseParallel parses all files concurrently via the shared
// ingest.ParseFilesParallel, then adapts the results into indexParseResult
// (with template-ref resolution and read-error classification). See issue #469.
func indexParseParallel(ctx context.Context, root string, files []*ingest.File) []indexParseResult {
	results := ingest.ParseFilesParallel(ctx, files, parser.ParseOpts{
		IncludeImports:  true,
		IncludeBody:     true,
		IncludeTypeRels: true,
	}, nil)

	out := make([]indexParseResult, len(results))
	for i, r := range results {
		if r.Err != nil {
			// Distinguish read errors (Raw is nil — file was not read) from
			// parse errors (Raw is set — file was read but parsing failed).
			// ingest.ParseFilesParallel sets Raw only after a successful read.
			var reason string
			if r.Raw == nil {
				reason = classifyReadError(r.Err)
				switch reason {
				case skipReasonReadMissing:
					slog.Warn("codegraph: file vanished between walk and read — possible clone-swap race",
						slog.String("file", r.File.Path),
						slog.String("skip_reason", reason))
				case skipReasonReadPerm:
					slog.Warn("codegraph: file read permission denied",
						slog.String("file", r.File.Path),
						slog.String("skip_reason", reason))
				default:
					slog.Warn("codegraph: file read error",
						slog.String("file", r.File.Path),
						slog.String("skip_reason", reason),
						slog.Any("error", r.Err))
				}
				// Read error: file is nil (matches original indexParseFile contract).
				out[i] = indexParseResult{skipReason: reason}
			} else {
				reason = "parse_error"
				slog.Debug("codegraph: parse failed", slog.String("file", r.File.Path), slog.String("language", r.File.Language), slog.Any("error", r.Err))
				// Parse error: file is set (matches original indexParseFile contract).
				out[i] = indexParseResult{file: r.File, skipReason: reason}
			}
			continue
		}
		if r.Result == nil {
			out[i] = indexParseResult{file: r.File}
			continue
		}

		// Resolve template refs to file paths immediately; unresolved refs are dropped.
		var tplRefs []templateFileRef
		for _, u := range callgraph.ResolveTemplateRefs(r.Raw, r.Result.TemplateRefs, r.File.RelPath, root) {
			tplRefs = append(tplRefs, templateFileRef{
				relFile:    u.From,
				resolvedTo: u.To,
				line:       u.Line,
			})
		}

		out[i] = indexParseResult{
			file:         r.File,
			symbols:      r.Result.Symbols,
			calls:        r.Calls,
			imports:      r.Result.Imports,
			rels:         r.Result.TypeRels,
			templateRefs: tplRefs,
		}
	}
	return out
}

// indexParseFile parses a single file via the shared pipeline. Thin wrapper
// around indexParseParallel for tests that exercise one file at a time.
func indexParseFile(root string, f *ingest.File) indexParseResult {
	results := indexParseParallel(context.Background(), root, []*ingest.File{f})
	return results[0]
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

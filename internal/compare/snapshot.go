package compare

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"

	xxhash "github.com/cespare/xxhash/v2"

	"github.com/anatolykoptev/go-code/internal/goutil"
	"github.com/anatolykoptev/go-code/internal/ingest"
	"github.com/anatolykoptev/go-code/internal/parser"
	"github.com/anatolykoptev/go-code/internal/polyglot"
)

// maxFileBytes is the default maximum file size ingested per file (512 KB).
const maxFileBytes = 512 * 1024

// SnapshotOpts controls what gets ingested and parsed when building a snapshot.
type SnapshotOpts struct {
	// Focus limits ingestion to files under this subdirectory or matching this
	// glob pattern. Empty means process the entire repository.
	Focus string

	// Language limits ingestion to files of this programming language.
	// Empty means accept all supported languages.
	Language string

	// MaxFiles limits the number of files ingested and parsed.
	// 0 means use the default cap (10,000).
	MaxFiles int
}

// snapshotParseResult pairs an ingest.File with its parsed output and source.
type snapshotParseResult struct {
	file   *ingest.File
	result *parser.ParseResult
	lines  int
	// readErr is true when the file was enumerated by ingest but os.ReadFile
	// failed at parse time (vanished file / truncated tree / permission flip).
	// Distinguishes a real read failure from a parse failure (parse failures
	// still yield a valid file entry with a line count) and from a ctx-cancel
	// skip (which leaves the zero-value result with a nil file).
	readErr bool
}

// BuildSnapshot ingests and parses a repository, returning a RepoSnapshot
// suitable for structural comparison.
//
// Steps:
//  1. Ingest the repository tree filtered by opts.Focus / opts.Language.
//  2. Parse all files in parallel (worker pool of runtime.NumCPU goroutines).
//  3. Aggregate symbols, unique imports, per-file entries, and line counts.
func BuildSnapshot(ctx context.Context, root string, opts SnapshotOpts) (*RepoSnapshot, error) {
	var langs []string
	if opts.Language != "" {
		langs = []string{opts.Language}
	}

	ingestOpts := ingest.IngestOpts{
		Root:         root,
		Focus:        opts.Focus,
		Languages:    langs,
		MaxFileBytes: maxFileBytes,
		MaxFiles:     opts.MaxFiles,
	}

	ir, err := ingest.IngestRepo(ctx, ingestOpts)
	if err != nil {
		return nil, fmt.Errorf("ingest repo: %w", err)
	}

	var focusMode string

	// Content-based fallback: when focus matches no file paths,
	// re-ingest all files and filter by symbol names, imports, and calls.
	if len(ir.Files) == 0 && opts.Focus != "" {
		ir, err = ingest.ContentFallback(ctx, ingestOpts, opts.Focus)
		if err != nil {
			return nil, err
		}
		focusMode = "content"
	}

	parsed := parseSnapshotFiles(ctx, ir.Files)
	snap := buildSnapshotResult(root, ir, parsed)
	snap.FocusMode = focusMode

	return snap, nil
}

// parseSnapshotFiles parses all files concurrently using a CPU-bounded worker pool.
func parseSnapshotFiles(ctx context.Context, files []*ingest.File) []snapshotParseResult {
	results := make([]snapshotParseResult, len(files))

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
				results[idx] = parseSnapshotFile(files[idx])
			}
		}()
	}

	wg.Wait()
	return results
}

// parseSnapshotFile reads and parses a single file. Failures are non-fatal:
// the result field remains nil and lines is zero.
func parseSnapshotFile(file *ingest.File) snapshotParseResult {
	source, err := os.ReadFile(file.Path)
	if err != nil {
		return snapshotParseResult{file: file, readErr: true}
	}

	pr, err := parser.ParseFile(file.Path, source, parser.ParseOpts{
		Language:        file.Language,
		IncludeBody:     true,
		IncludeImports:  true,
		IncludeTypeRels: true,
	})
	if err != nil {
		return snapshotParseResult{file: file, lines: goutil.CountLines(source)}
	}

	return snapshotParseResult{file: file, result: pr, lines: goutil.CountLines(source)}
}

// buildSnapshotResult assembles a RepoSnapshot from parse results.
func buildSnapshotResult(root string, ir *ingest.IngestResult, parsed []snapshotParseResult) *RepoSnapshot {
	var (
		allSymbols       []*parser.Symbol
		importsSeen      = make(map[string]struct{})
		files            = make([]SnapshotFile, 0, len(parsed))
		totalLines       int
		droppedReadError int
		droppedCtxCancel int
	)

	for _, pr := range parsed {
		// A nil file is a parse worker that returned early on ctx.Err() before
		// reaching this slot — the file was enumerated but never parsed.
		if pr.file == nil {
			droppedCtxCancel++
			continue
		}
		// A read error means the file vanished or became unreadable between
		// ingest enumeration and the parse read (the use-after-delete signal).
		// Drop it rather than emit a hollow 0-line entry that silently zeroes
		// test-file detection and undercounts lines.
		if pr.readErr {
			droppedReadError++
			continue
		}

		totalLines += pr.lines
		sf := SnapshotFile{
			RelPath:  pr.file.RelPath,
			Language: pr.file.Language,
			Lines:    pr.lines,
		}

		if pr.result != nil {
			sf.Symbols = pr.result.Symbols
			sf.Imports = pr.result.Imports

			allSymbols = append(allSymbols, pr.result.Symbols...)
			for _, imp := range pr.result.Imports {
				if imp != "" {
					importsSeen[imp] = struct{}{}
				}
			}
		}

		files = append(files, sf)
	}

	uniqueImports := goutil.SortedSetKeys(importsSeen)

	computeBodyHashes(allSymbols)

	// Extract type relationships from parsed files.
	var allRels []parser.TypeRelationship
	for _, pr := range parsed {
		if pr.file == nil || pr.result == nil {
			continue
		}
		allRels = append(allRels, pr.result.TypeRels...)
	}

	return &RepoSnapshot{
		Name:             filepath.Base(root),
		Root:             root,
		Language:         polyglot.DominantLanguage(ir.Files),
		Symbols:          allSymbols,
		Imports:          uniqueImports,
		Files:            files,
		FileCount:        len(ir.Files),
		TotalLines:       totalLines,
		Rels:             allRels,
		Partial:          droppedReadError+droppedCtxCancel > 0,
		DroppedReadError: droppedReadError,
		DroppedCtxCancel: droppedCtxCancel,
	}
}

// computeBodyHashes sets BodyHash on each symbol that has a non-empty Body.
func computeBodyHashes(symbols []*parser.Symbol) {
	for _, sym := range symbols {
		if sym.Body != "" {
			sym.BodyHash = xxhash.Sum64String(sym.Body)
		}
	}
}

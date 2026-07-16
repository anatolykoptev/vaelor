package ingest

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"sync"

	"github.com/anatolykoptev/go-code/internal/parser"
)

// FileParseResult holds the outcome of parsing a single file: the raw bytes,
// the parsed result (symbols, imports, type rels, template refs), extracted
// call sites, and any read/parse error. Callers derive their own
// package-specific result types from this shared struct.
type FileParseResult struct {
	File   *File
	Result *parser.ParseResult
	Calls  []parser.CallSite
	Raw    []byte // raw file bytes, needed for template-ref resolution
	Err    error  // non-nil if read or parse failed
}

// FileParseCache is the cache interface ParseFilesParallel uses to skip
// re-parsing unchanged files. Implemented by cache.ParseCache; defined here
// to avoid a circular dependency (parser → ingest → cache → parser).
// May be nil to skip caching.
type FileParseCache interface {
	Get(path string, modTime, size int64, includeBody, includeTypeRels bool) (*parser.ParseResult, []parser.CallSite)
	Put(path string, modTime, size int64, includeBody, includeTypeRels bool, result *parser.ParseResult, calls []parser.CallSite)
}

// ParseFilesParallel reads and parses all files concurrently using a fixed
// worker pool capped at runtime.NumCPU(). This bounds both goroutine count
// and memory usage regardless of the number of files.
//
// parseCache may be nil to skip caching. opts controls parse behaviour
// (IncludeBody, IncludeImports, IncludeTypeRels); Language is set from each
// file's detected language.
//
// This is the single shared parse-parallel implementation for all pipelines
// (callgraph, analyze, codegraph). Callers adapt FileParseResult into their
// own package-specific result types. See issue #469.
func ParseFilesParallel(ctx context.Context, files []*File, opts parser.ParseOpts, parseCache FileParseCache) []FileParseResult {
	results := make([]FileParseResult, len(files))

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
				results[idx] = parseOneFile(files[idx], opts, parseCache)
			}
		}()
	}

	wg.Wait()
	return results
}

// parseOneFile reads and parses a single file. Parse failures are non-fatal:
// Err is set and Result is nil. parseCache may be nil to skip caching.
func parseOneFile(file *File, opts parser.ParseOpts, parseCache FileParseCache) FileParseResult {
	var modTime, size int64
	if parseCache != nil {
		info, err := os.Stat(file.Path)
		if err != nil {
			return FileParseResult{File: file, Err: fmt.Errorf("stat %s: %w", file.Path, err)}
		}
		modTime = info.ModTime().UnixNano()
		size = info.Size()
		if cachedResult, cachedCalls := parseCache.Get(file.Path, modTime, size, opts.IncludeBody, opts.IncludeTypeRels); cachedResult != nil {
			return FileParseResult{File: file, Result: cachedResult, Calls: cachedCalls}
		}
	}

	source, err := os.ReadFile(file.Path)
	if err != nil {
		return FileParseResult{File: file, Err: fmt.Errorf("read %s: %w", file.Path, err)}
	}

	fileOpts := opts
	fileOpts.Language = file.Language

	// Single parse for symbols+calls instead of ParseFile + ExtractCalls (issue #400).
	pr, calls, err := parser.ParseFileWithCalls(file.Path, source, fileOpts)
	if err != nil {
		return FileParseResult{File: file, Err: fmt.Errorf("parse %s: %w", file.Path, err)}
	}

	if parseCache != nil {
		parseCache.Put(file.Path, modTime, size, opts.IncludeBody, opts.IncludeTypeRels, pr, calls)
	}

	return FileParseResult{File: file, Result: pr, Calls: calls, Raw: source}
}

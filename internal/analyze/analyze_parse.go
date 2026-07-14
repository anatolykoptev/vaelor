package analyze

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"

	"github.com/anatolykoptev/go-code/internal/cache"
	"github.com/anatolykoptev/go-code/internal/goutil"
	"github.com/anatolykoptev/go-code/internal/ingest"
	"github.com/anatolykoptev/go-code/internal/parser"
	"github.com/anatolykoptev/go-code/internal/polyglot"
)

// fileParseResult holds the outcome of parsing a single file.
type fileParseResult struct {
	file   *ingest.File
	result *parser.ParseResult
	calls  []parser.CallSite // call sites extracted for ranking
	err    error             // non-nil if parsing failed
}

// parseFilesParallel reads and parses all files concurrently using a fixed
// worker pool capped at runtime.NumCPU(). This bounds both goroutine count
// and memory usage regardless of the number of files.
// parseCache may be nil to skip caching.
func parseFilesParallel(ctx context.Context, files []*ingest.File, includeBody bool, parseCache *cache.ParseCache) []fileParseResult {
	results := make([]fileParseResult, len(files))

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
				results[idx] = parseOneFile(files[idx], includeBody, parseCache)
			}
		}()
	}

	wg.Wait()
	return results
}

// parseOneFile reads and parses a single file. Parse failures are non-fatal:
// result is nil and the error is recorded in fileParseResult.err.
// parseCache may be nil to skip caching.
func parseOneFile(file *ingest.File, includeBody bool, parseCache *cache.ParseCache) fileParseResult {
	var modTime, size int64
	if parseCache != nil {
		info, err := os.Stat(file.Path)
		if err != nil {
			return fileParseResult{file: file, err: fmt.Errorf("stat %s: %w", file.Path, err)}
		}
		modTime = info.ModTime().UnixNano()
		size = info.Size()
		if cachedResult, cachedCalls := parseCache.Get(file.Path, modTime, size, includeBody, false); cachedResult != nil {
			return fileParseResult{file: file, result: cachedResult, calls: cachedCalls}
		}
	}

	source, err := os.ReadFile(file.Path)
	if err != nil {
		return fileParseResult{file: file, err: fmt.Errorf("read %s: %w", file.Path, err)}
	}

	pr, err := parser.ParseFile(file.Path, source, parser.ParseOpts{
		Language:       file.Language,
		IncludeBody:    includeBody,
		IncludeImports: true,
	})
	if err != nil {
		return fileParseResult{file: file, err: fmt.Errorf("parse %s: %w", file.Path, err)}
	}

	calls, _ := parser.ExtractCalls(file.Path, source, parser.ParseOpts{Language: file.Language})

	if parseCache != nil {
		parseCache.Put(file.Path, modTime, size, includeBody, false, pr, calls)
	}

	return fileParseResult{file: file, result: pr, calls: calls}
}

// defaultSymbolSampleSize is the default cap for top-level symbol sampling.
const defaultSymbolSampleSize = 50

func collectTopSymbols(results []fileParseResult) []*parser.Symbol {
	return collectTopSymbolsN(results, defaultSymbolSampleSize)
}

func collectTopSymbolsN(results []fileParseResult, limit int) []*parser.Symbol {
	var symbols []*parser.Symbol
	for _, pr := range results {
		if pr.result == nil {
			continue
		}
		for _, sym := range pr.result.Symbols {
			if sym.Kind == parser.KindFunction || sym.Kind == parser.KindMethod ||
				sym.Kind == parser.KindStruct || sym.Kind == parser.KindInterface ||
				sym.Kind == parser.KindType {
				symbols = append(symbols, sym)
			}
			if len(symbols) >= limit {
				return symbols
			}
		}
	}
	return symbols
}

// matchAllRe matches any non-empty string, used when the query is empty or "*".
var matchAllRe = regexp.MustCompile(".")

// wildcardToRegexp converts a wildcard pattern (using * as glob) to a compiled regexp.
// An empty pattern matches everything.
func wildcardToRegexp(pattern string) (*regexp.Regexp, error) {
	if pattern == "" || pattern == "*" {
		return matchAllRe, nil
	}
	escaped := regexp.QuoteMeta(pattern)
	regexStr := "(?i)^" + strings.ReplaceAll(escaped, `\*`, ".*") + "$"
	return regexp.Compile(regexStr)
}

// matchesSymbol reports whether sym matches the pattern and kind filter.
func matchesSymbol(sym *parser.Symbol, pattern *regexp.Regexp, kind parser.NodeKind) bool {
	if kind != "" && sym.Kind != kind {
		return false
	}
	return pattern.MatchString(sym.Name)
}

// extractPackages deduplicates directory names from file RelPaths.
func extractPackages(files []*ingest.File) []string {
	seen := make(map[string]struct{})
	for _, f := range files {
		dir := filepath.Dir(f.RelPath)
		if dir == "." {
			dir = "/"
		}
		seen[dir] = struct{}{}
	}
	pkgs := make([]string, 0, len(seen))
	for pkg := range seen {
		pkgs = append(pkgs, pkg)
	}
	sort.Strings(pkgs)
	return pkgs
}

// buildAnalysisResult assembles the RepoAnalysisResult from parsed data and ContextData.
func buildAnalysisResult(root string, ir *ingest.IngestResult, results []fileParseResult, cd *ContextData) *RepoAnalysisResult {
	repoName := filepath.Base(root)
	lang := polyglot.DominantLanguage(ir.Files)
	packages := extractPackages(ir.Files)
	symbols := collectTopSymbols(results)

	parseByPath := make(map[string]*parser.ParseResult, len(results))
	for _, pr := range results {
		if pr.result != nil {
			parseByPath[pr.file.RelPath] = pr.result
		}
	}

	analyzedFiles := make([]AnalyzedFile, 0, len(cd.RankedFiles))
	for _, f := range cd.RankedFiles {
		af := AnalyzedFile{
			RelPath:    f.RelPath,
			Language:   f.Language,
			Size:       f.Size,
			Relevance:  cd.FileScores[f.RelPath],
			ImportedBy: cd.ImportedBy[f.RelPath],
		}
		if pr, ok := parseByPath[f.RelPath]; ok {
			af.Symbols = pr.Symbols
			af.Imports = pr.Imports
			for _, sym := range pr.Symbols {
				if int(sym.EndLine) > af.Lines {
					af.Lines = int(sym.EndLine)
				}
			}
		}
		analyzedFiles = append(analyzedFiles, af)
	}

	igExport := make(map[string][]string, len(cd.ImportGraph))
	for pkg, deps := range cd.ImportGraph {
		igExport[pkg] = goutil.SortedSetKeys(deps)
	}

	return &RepoAnalysisResult{
		RepoName:    repoName,
		Language:    lang,
		FileCount:   len(ir.Files),
		Symbols:     symbols,
		Packages:    packages,
		Files:       analyzedFiles,
		ImportGraph: igExport,
		FileTree:    cd.FileTree,
		Languages:   cd.Languages,
		TotalBytes:  ir.TotalBytes,
		Skipped:     ir.SkippedCount,
	}
}

// Package explore provides a fast, structured overview of a repository.
// No LLM calls — purely static analysis combining file stats, language
// breakdown, top symbols by call frequency, dead code summary, and packages.
package explore

import (
	"context"
	"os"
	"path/filepath"
	"sort"

	"github.com/anatolykoptev/go-code/internal/callgraph"
	"github.com/anatolykoptev/go-code/internal/goutil"
	"github.com/anatolykoptev/go-code/internal/deadcode"
	"github.com/anatolykoptev/go-code/internal/ingest"
	"github.com/anatolykoptev/go-code/internal/parser"
)

// maxFileBytes limits the size of files parsed during exploration.
const maxFileBytes = 512 * 1024

// maxDeadCodeSamples is the maximum number of dead code names to include.
const maxDeadCodeSamples = 10

// maxTopSymbols is the maximum number of top symbols returned.
const maxTopSymbols = 20

// Input configures the exploration.
type Input struct {
	Root     string
	Language string
	Focus    string
}

// Result is the structured output of an exploration.
type Result struct {
	ReadmeExcerpt string           `json:"readme_excerpt,omitempty"`
	FocusMode     string           `json:"focus_mode,omitempty"`
	FileCount     int              `json:"file_count"`
	SymbolCount   int              `json:"symbol_count"`
	TotalLines    int              `json:"total_lines"`
	Languages     []LanguageStat   `json:"languages"`
	TopSymbols    []SymbolSummary  `json:"top_symbols"`
	DeadCode      *DeadCodeSummary `json:"dead_code,omitempty"`
	Packages      []string         `json:"packages"`
	DepHighlights *DepHighlights   `json:"dep_highlights,omitempty"`
	Health        *HealthSummary   `json:"health,omitempty"`
}

// LanguageStat holds file count and ratio for a detected language.
type LanguageStat struct {
	Name  string  `json:"name"`
	Files int     `json:"files"`
	Ratio float64 `json:"ratio"`
}

// SymbolSummary describes a symbol and how often it is called.
type SymbolSummary struct {
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	File      string `json:"file"`
	CallCount int    `json:"call_count"`
}

// DeadCodeSummary is a compact view of dead code findings.
type DeadCodeSummary struct {
	Count   int      `json:"count"`
	Samples []string `json:"samples"`
}

// parseResults holds aggregated parse output from all files.
type parseResults struct {
	symbols    []*parser.Symbol
	calls      []parser.CallSite
	totalLines int
	imports    map[string][]string // file path → import paths
}

// Run performs a fast, structured overview of the repository at input.Root.
func Run(ctx context.Context, input Input) (*Result, error) {
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
		return nil, err
	}

	var focusMode string

	// Content-based fallback: when focus matches no file paths,
	// re-ingest all files and filter by symbol names, imports, and calls.
	if len(ir.Files) == 0 && input.Focus != "" {
		irAll, err := ingest.IngestRepo(ctx, ingest.IngestOpts{
			Root:         input.Root,
			Languages:    langs,
			MaxFileBytes: maxFileBytes,
		})
		if err != nil {
			return nil, err
		}

		prAll, err := parseAllFiles(ctx, irAll.Files)
		if err != nil {
			return nil, err
		}

		matched := contentFilter(input.Focus, prAll.symbols, prAll.imports, prAll.calls)
		ir.Files = filterFiles(irAll.Files, matched)
		focusMode = "content"
	}

	pr, err := parseAllFiles(ctx, ir.Files)
	if err != nil {
		return nil, err
	}

	cg := callgraph.BuildCallGraph(pr.symbols, pr.calls)
	callCounts := countIncomingCalls(cg)
	topSymbols := buildTopSymbols(pr.symbols, callCounts, input.Root)
	dcSummary := buildDeadCodeSummary(cg)
	langStats := buildLanguageStats(ir.Files)
	packages := buildPackageList(ir.Files, input.Root)
	depHL := buildDepHighlights(ir.Files, pr.imports, input.Root)

	readme := readmeExcerpt(input.Root)
	health := computeHealth(pr.symbols, ir.Files)

	return &Result{
		ReadmeExcerpt: readme,
		FocusMode:     focusMode,
		FileCount:     len(ir.Files),
		SymbolCount:   len(pr.symbols),
		TotalLines:    pr.totalLines,
		Languages:     langStats,
		TopSymbols:    topSymbols,
		DeadCode:      dcSummary,
		Packages:      packages,
		DepHighlights: depHL,
		Health:        health,
	}, nil
}

// parseAllFiles parses all ingested files, collecting symbols, calls, imports, and line counts.
func parseAllFiles(ctx context.Context, files []*ingest.File) (*parseResults, error) {
	result := parseResults{imports: make(map[string][]string, len(files))}
	for _, f := range files {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		source, readErr := os.ReadFile(f.Path)
		if readErr != nil {
			continue
		}

		result.totalLines += goutil.CountLines(source)

		opts := parser.ParseOpts{
			Language:       f.Language,
			IncludeBody:    false,
			IncludeImports: true,
		}

		pr, parseErr := parser.ParseFile(f.Path, source, opts)
		if parseErr != nil {
			continue
		}
		result.symbols = append(result.symbols, pr.Symbols...)
		if len(pr.Imports) > 0 {
			result.imports[f.Path] = pr.Imports
		}

		calls, _ := parser.ExtractCalls(f.Path, source, opts)
		result.calls = append(result.calls, calls...)
	}
	return &result, nil
}

// countIncomingCalls returns a map of symbol to its incoming call count.
func countIncomingCalls(cg *callgraph.CallGraph) map[*parser.Symbol]int {
	callCounts := make(map[*parser.Symbol]int)
	for _, edge := range cg.Edges {
		if edge.Callee != nil {
			callCounts[edge.Callee]++
		}
	}
	return callCounts
}

// buildDeadCodeSummary runs dead code analysis and returns a compact summary.
func buildDeadCodeSummary(cg *callgraph.CallGraph) *DeadCodeSummary {
	dcResult := deadcode.Analyze(cg, deadcode.Options{})
	if dcResult.DeadCount == 0 {
		return nil
	}
	samples := make([]string, 0, maxDeadCodeSamples)
	for i, ds := range dcResult.DeadSymbols {
		if i >= maxDeadCodeSamples {
			break
		}
		samples = append(samples, ds.Name)
	}
	return &DeadCodeSummary{
		Count:   dcResult.DeadCount,
		Samples: samples,
	}
}

// buildLanguageStats computes per-language file counts and ratios.
func buildLanguageStats(files []*ingest.File) []LanguageStat {
	langFiles := make(map[string]int)
	for _, f := range files {
		if f.Language != "" {
			langFiles[f.Language]++
		}
	}

	fileCount := len(files)
	langStats := make([]LanguageStat, 0, len(langFiles))
	for name, count := range langFiles {
		var ratio float64
		if fileCount > 0 {
			ratio = float64(count) / float64(fileCount)
		}
		langStats = append(langStats, LanguageStat{
			Name:  name,
			Files: count,
			Ratio: ratio,
		})
	}
	sort.Slice(langStats, func(i, j int) bool {
		return langStats[i].Files > langStats[j].Files
	})
	return langStats
}

// buildTopSymbols returns the top symbols sorted by call count descending.
func buildTopSymbols(symbols []*parser.Symbol, callCounts map[*parser.Symbol]int, root string) []SymbolSummary {
	type entry struct {
		sym   *parser.Symbol
		count int
	}

	var entries []entry
	for _, sym := range symbols {
		if sym.Kind != parser.KindFunction && sym.Kind != parser.KindMethod {
			continue
		}
		count := callCounts[sym]
		if count == 0 {
			continue
		}
		entries = append(entries, entry{sym: sym, count: count})
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].count > entries[j].count
	})

	limit := maxTopSymbols
	if len(entries) < limit {
		limit = len(entries)
	}

	result := make([]SymbolSummary, limit)
	for i := range limit {
		e := entries[i]
		file := e.sym.File
		if rel, err := filepath.Rel(root, file); err == nil {
			file = rel
		}
		result[i] = SymbolSummary{
			Name:      e.sym.Name,
			Kind:      string(e.sym.Kind),
			File:      file,
			CallCount: e.count,
		}
	}
	return result
}

// buildPackageList collects unique directory paths relative to root.
func buildPackageList(files []*ingest.File, root string) []string {
	seen := make(map[string]struct{})
	for _, f := range files {
		dir := filepath.Dir(f.Path)
		rel, err := filepath.Rel(root, dir)
		if err != nil {
			rel = dir
		}
		seen[rel] = struct{}{}
	}

	pkgs := make([]string, 0, len(seen))
	for p := range seen {
		pkgs = append(pkgs, p)
	}
	sort.Strings(pkgs)
	return pkgs
}

// filterFiles returns only files whose absolute path is in the matched set.
func filterFiles(files []*ingest.File, matched map[string]bool) []*ingest.File {
	if len(matched) == 0 {
		return nil
	}
	out := make([]*ingest.File, 0, len(matched))
	for _, f := range files {
		if matched[f.Path] {
			out = append(out, f)
		}
	}
	return out
}


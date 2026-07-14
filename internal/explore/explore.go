// Package explore provides a fast, structured overview of a repository.
// No LLM calls — purely static analysis combining file stats, language
// breakdown, top symbols by call frequency, dead code summary, and packages.
package explore

import (
	"context"
	"log/slog"
	"time"

	"github.com/anatolykoptev/go-code/internal/callgraph"
	"github.com/anatolykoptev/go-code/internal/compare"
	"github.com/anatolykoptev/go-code/internal/ingest"
	"github.com/anatolykoptev/go-code/internal/parser"
)

// maxFileBytes limits the size of files parsed during exploration.
const maxFileBytes = 512 * 1024

// maxDeadCodeSamples is the maximum number of dead code names to include.
const maxDeadCodeSamples = 10

// maxTopSymbols is the maximum number of top symbols returned.
const maxTopSymbols = 20

// maxRecentCommits is the number of recent commits to include.
const maxRecentCommits = 20

// maxTopCoupledPairs is the number of top coupled file pairs to include.
const maxTopCoupledPairs = 5

// Input configures the exploration.
type Input struct {
	Root     string
	Language string
	Focus    string
}

// Result is the structured output of an exploration.
type Result struct {
	ReadmeExcerpt string             `json:"readme_excerpt,omitempty"`
	FocusMode     string             `json:"focus_mode,omitempty"`
	FileCount     int                `json:"file_count"`
	SymbolCount   int                `json:"symbol_count"`
	TotalLines    int                `json:"total_lines"`
	Languages     []LanguageStat     `json:"languages"`
	TopSymbols    []SymbolSummary    `json:"top_symbols"`
	DeadCode      *DeadCodeSummary   `json:"dead_code,omitempty"`
	Communities   *CommunityOverview `json:"communities,omitempty"`
	Packages      []string           `json:"packages"`
	DepHighlights *DepHighlights     `json:"dep_highlights,omitempty"`
	Health        *HealthSummary     `json:"health,omitempty"`
	Tier          string             `json:"tier,omitempty"`
	Backend       string             `json:"backend,omitempty"`
	RecentCommits []CommitSummary    `json:"recent_commits,omitempty"`
	TopCoupled    []CoupledSummary   `json:"top_coupled_files,omitempty"`
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

// CommitSummary is a compact view of a recent git commit.
type CommitSummary struct {
	Hash    string `json:"hash"`
	Message string `json:"message"`
	Date    string `json:"date"`
	Files   int    `json:"files_changed"`
}

// CoupledSummary is a pair of files that frequently change together.
type CoupledSummary struct {
	FileA     string `json:"file_a"`
	FileB     string `json:"file_b"`
	CoChanges int    `json:"co_changes"`
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

	ingestOpts := ingest.IngestOpts{
		Root:         input.Root,
		Focus:        input.Focus,
		Languages:    langs,
		MaxFileBytes: maxFileBytes,
	}
	ir, err := ingest.IngestRepo(ctx, ingestOpts)
	if err != nil {
		return nil, err
	}

	var focusMode string

	// Content-based fallback: when focus matches no file paths,
	// re-ingest all files and filter by symbol names, imports, and calls.
	if len(ir.Files) == 0 && input.Focus != "" {
		ir, err = ingest.ContentFallback(ctx, ingestOpts, input.Focus)
		if err != nil {
			return nil, err
		}
		focusMode = "content"
	}

	pr, err := parseAllFiles(ctx, ir.Files)
	if err != nil {
		return nil, err
	}

	cg, cgErr := callgraph.BuildFromRepo(ctx, callgraph.TraceRepoInput{
		Root:     input.Root,
		Language: input.Language,
		Focus:    input.Focus,
	})
	if cgErr != nil {
		slog.Debug("explore: BuildFromRepo failed, falling back", "err", cgErr)
		cg = callgraph.BuildCallGraph(pr.symbols, pr.calls)
	}

	callCounts := countIncomingCalls(cg)
	topSymbols := buildTopSymbols(cg.Symbols, callCounts, input.Root)
	dcSummary := buildDeadCodeSummary(cg)
	langStats := buildLanguageStats(ir.Files)
	packages := buildPackageList(ir.Files, input.Root)
	depHL := buildDepHighlights(ir.Files, pr.imports, input.Root)

	readme := readmeExcerpt(input.Root)
	health := computeHealth(pr.symbols, ir.Files)

	communityOverview := buildCommunityOverview(cg, input.Root)

	// Recent commits: last 20, non-fatal.
	var recentCommits []CommitSummary
	{
		rctx, rcancel := context.WithTimeout(ctx, 10*time.Second)
		commits, cerr := collectRecentCommits(rctx, input.Root, maxRecentCommits)
		rcancel()
		if cerr == nil {
			recentCommits = commits
		}
	}

	// Top coupled files: non-fatal (CollectCoupling has LRU cache).
	var topCoupled []CoupledSummary
	{
		cctx, ccancel := context.WithTimeout(ctx, 10*time.Second)
		pairs := compare.CollectCoupling(cctx, input.Root, 3)
		ccancel()
		limit := maxTopCoupledPairs
		if len(pairs) < limit {
			limit = len(pairs)
		}
		for _, p := range pairs[:limit] {
			topCoupled = append(topCoupled, CoupledSummary{
				FileA:     p.FileA,
				FileB:     p.FileB,
				CoChanges: p.CoChanges,
			})
		}
	}

	result := &Result{
		ReadmeExcerpt: readme,
		FocusMode:     focusMode,
		FileCount:     len(ir.Files),
		SymbolCount:   len(pr.symbols),
		TotalLines:    pr.totalLines,
		Languages:     langStats,
		TopSymbols:    topSymbols,
		DeadCode:      dcSummary,
		Communities:   communityOverview,
		Packages:      packages,
		DepHighlights: depHL,
		Health:        health,
		RecentCommits: recentCommits,
		TopCoupled:    topCoupled,
	}
	result.Tier = cg.Tier
	result.Backend = cg.Backend
	return result, nil
}

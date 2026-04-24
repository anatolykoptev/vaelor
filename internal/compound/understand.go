// Package compound provides high-level compound analysis tools that aggregate
// multiple lower-level analysis primitives into a single result.
package compound

import (
	"context"
	"strings"
	"log/slog"

	"github.com/anatolykoptev/go-code/internal/callgraph"
	"github.com/anatolykoptev/go-code/internal/graphx"
	"github.com/anatolykoptev/go-code/internal/learnings"
	"github.com/anatolykoptev/go-code/internal/oxcodes"
	"github.com/anatolykoptev/go-code/internal/parser"
)

const (
	defaultMaxCallees      = 20
	defaultMaxCallers      = 20
	defaultPriorLearningsK = 3
)

// LearningsLookup is the narrow interface compound.Understand uses to fetch
// prior review learnings for a (repo, symbol). *learnings.Store satisfies it.
// Kept local to the package so tests can supply an in-memory fake without
// spinning up Postgres.
type LearningsLookup interface {
	Nearest(ctx context.Context, repo, symbol string, k int) ([]learnings.Record, error)
}

// UnderstandOpts configures the understand compound analysis.
type UnderstandOpts struct {
	IncludeCallers bool
	MaxCallees     int // default 20
	MaxCallers     int // default 20

	// OxCodes enables body analysis via ox-codes scoped search. Optional.
	OxCodes *oxcodes.Client

	// Root is the repository root path, required when OxCodes is set.
	Root string

	// Repo is the user-supplied repo key (slug, URL, or host path) used to
	// look up prior learnings. Must match the value persisted by
	// review_pr (dry_run=false) for lookups to succeed.
	Repo string

	// Learnings optionally sources prior review findings for the symbol.
	// When nil (not configured) or Nearest returns no rows, the result's
	// PriorLearnings field stays empty and is omitted from JSON output.
	Learnings LearningsLookup

	// Graph optionally surfaces pagerank/community/surprise for the symbol.
	// When nil or the graph has no snapshot, GraphSignals.Found stays false.
	Graph graphx.Analytics

	// Refs optionally sources cross-reference edges from the AGE graph.
	// Used to surface tested_by coverage edges. When nil, tested_by is omitted.
	Refs graphx.CrossRefs

	// DeadCodeScores optionally sources CE reranker scores for dead-code detection.
	// When nil, dead_code_score is omitted from the result.
	DeadCodeScores DeadCodeScoreLookup
}

// DeadCodeScoreLookup is the narrow interface for fetching a single symbol's
// dead-code CE reranker score. *codegraph.Store satisfies it.
type DeadCodeScoreLookup interface {
	LoadDeadCodeScore(ctx context.Context, repoKey, name, file string) (float32, bool)
}

// UnderstandResult is the output of the understand compound tool.
type UnderstandResult struct {
	Symbol         SymbolInfo         `json:"symbol"`
	Callees        []CallRef          `json:"callees,omitempty"`
	Callers        []CallRef          `json:"callers,omitempty"`
	Tier           string             `json:"tier"`
	Warnings       []string           `json:"warnings,omitempty"`
	Body           *BodyAnalysis      `json:"body_analysis,omitempty"`
	PriorLearnings []learnings.Record `json:"prior_learnings,omitempty"`
	// GraphSignals holds pagerank/community/surprise when the persistent graph
	// has them. Signals.Found==false means the graph is cold; omitted from output.
	GraphSignals *graphx.Signals `json:"graph_signals,omitempty"`
	// DeadCodeScore is the CE reranker confidence that this symbol is genuine dead code.
	// Only present when the symbol has no callers and code_graph has pre-scored it.
	// Negative float; closer to 0 means more likely to be genuine dead code.
	DeadCodeScore *float32 `json:"dead_code_score,omitempty"`
	// DeadCodeNote explains the score when DeadCodeScore is set.
	DeadCodeNote string `json:"dead_code_note,omitempty"`
	// TestedBy lists test functions that directly cover this symbol via AGE TESTED_BY edges.
	TestedBy []graphx.SymbolRef `json:"tested_by,omitempty"`
}

// SymbolInfo is a summary of a symbol for compound tool output.
type SymbolInfo struct {
	Name       string `json:"name"`
	Kind       string `json:"kind"`
	File       string `json:"file"`
	StartLine  uint32 `json:"start_line"`
	EndLine    uint32 `json:"end_line"`
	Signature  string `json:"signature,omitempty"`
	Complexity int    `json:"complexity,omitempty"`
	Receiver   string `json:"receiver,omitempty"`
}

// CallRef is a reference to a called/calling function.
type CallRef struct {
	Name     string `json:"name"`
	File     string `json:"file"`
	Line     uint32 `json:"line"`
	Receiver string `json:"receiver,omitempty"`
}

// MatchRef is a lightweight symbol descriptor used in disambiguation responses.
type MatchRef struct {
	Name     string `json:"name"`
	Kind     string `json:"kind"`
	File     string `json:"file"`
	Line     uint32 `json:"line"`
	Receiver string `json:"receiver,omitempty"`
}

// FindSymbol returns all function/method symbols matching the given name.
func FindSymbol(symbols []*parser.Symbol, name string) []*parser.Symbol {
	var matches []*parser.Symbol
	for _, sym := range symbols {
		if sym.Name == name && (sym.Kind == parser.KindFunction || sym.Kind == parser.KindMethod) {
			matches = append(matches, sym)
		}
	}
	return matches
}

// Understand performs a deep-dive analysis of a single symbol.
func Understand(ctx context.Context, sym *parser.Symbol, cg *callgraph.CallGraph, opts UnderstandOpts) *UnderstandResult {
	maxCallees := opts.MaxCallees
	if maxCallees <= 0 {
		maxCallees = defaultMaxCallees
	}
	maxCallers := opts.MaxCallers
	if maxCallers <= 0 {
		maxCallers = defaultMaxCallers
	}

	result := &UnderstandResult{
		Symbol: SymbolInfo{
			Name:       sym.Name,
			Kind:       string(sym.Kind),
			File:       sym.File,
			StartLine:  sym.StartLine,
			EndLine:    sym.EndLine,
			Signature:  sym.Signature,
			Complexity: sym.Complexity,
			Receiver:   sym.Receiver,
		},
		Tier: cg.Tier,
	}

	result.Callees = collectCallees(cg, sym, maxCallees)

	if opts.IncludeCallers {
		result.Callers = collectCallers(cg, sym, maxCallers)
	}

	if opts.OxCodes != nil {
		result.Body = AnalyzeBody(ctx, opts.OxCodes, opts.Root, sym)
	}

	result.PriorLearnings = fetchPriorLearnings(ctx, opts, sym.Name)
	result.GraphSignals = fetchGraphSignals(ctx, opts, sym)
	result.DeadCodeScore, result.DeadCodeNote = fetchDeadCodeScore(ctx, opts, sym)
	result.TestedBy = fetchTestedBy(ctx, opts, sym)

	return result
}

// fetchGraphSignals queries the persistent graph for pagerank/community/surprise.
// Returns nil (omitted from JSON) when Graph is not configured or Found==false.
// Errors are swallowed with slog.Debug so understand stays functional when the
// graph is offline.
func fetchGraphSignals(ctx context.Context, opts UnderstandOpts, sym *parser.Symbol) *graphx.Signals {
	if opts.Graph == nil {
		return nil
	}
	// Graph is indexed under the resolved container path (opts.Root), not the
	// user-facing host path (opts.Repo). Using the wrong one picks a different
	// graph hash and always returns empty.
	repoKey := opts.Root
	if repoKey == "" {
		repoKey = opts.Repo
	}
	if repoKey == "" {
		return nil
	}
	sig, err := opts.Graph.Symbol(ctx, repoKey, sym.Name, sym.File)
	if err != nil {
		slog.Debug("graph signals unavailable", "symbol", sym.Name, "err", err)
		return nil
	}
	if !sig.Found {
		return nil
	}
	return &sig
}

// fetchPriorLearnings queries the learnings store for up to
// defaultPriorLearningsK records matching (opts.Repo, symbol). Returns nil if
// the store is not configured, the repo key is empty, or the lookup fails —
// lookup failures are non-fatal so understand keeps working when the store is
// offline.
func fetchPriorLearnings(ctx context.Context, opts UnderstandOpts, symbol string) []learnings.Record {
	if opts.Learnings == nil || opts.Repo == "" {
		return nil
	}
	recs, err := opts.Learnings.Nearest(ctx, opts.Repo, symbol, defaultPriorLearningsK)
	if err != nil {
		return nil
	}
	return recs
}

// fetchDeadCodeScore looks up the CE reranker confidence score for a symbol
// from the code_dead_code_scores table. Returns (nil, "") when no score is
// available (graph offline, no snapshot, or symbol not in orphan set).
func fetchDeadCodeScore(ctx context.Context, opts UnderstandOpts, sym *parser.Symbol) (*float32, string) {
	if opts.DeadCodeScores == nil {
		return nil, ""
	}
	repoKey := opts.Root
	if repoKey == "" {
		return nil, ""
	}
	// sym.File is absolute (/host/src/Repo/src/pkg/file.py); table stores relative.
	file := sym.File
	if opts.Root != "" {
		file = strings.TrimPrefix(strings.TrimPrefix(file, opts.Root), "/")
	}
	score, ok := opts.DeadCodeScores.LoadDeadCodeScore(ctx, repoKey, sym.Name, file)
	if !ok {
		return nil, ""
	}
	s := score
	return &s, "CE dead-code probability [0..1]: higher = more likely genuine dead code (not an entrypoint or test utility)."
}

// fetchTestedBy queries the AGE graph for test functions covering this symbol
// via TESTED_BY edges. Returns nil when the graph is offline or no edges exist.
func fetchTestedBy(ctx context.Context, opts UnderstandOpts, sym *parser.Symbol) []graphx.SymbolRef {
	if opts.Refs == nil {
		return nil
	}
	repoKey := opts.Root
	if repoKey == "" {
		return nil
	}
	refs, err := opts.Refs.TestedBy(ctx, repoKey, sym.Name, sym.File)
	if err != nil {
		slog.Debug("tested_by unavailable", "symbol", sym.Name, "err", err)
		return nil
	}
	return refs
}

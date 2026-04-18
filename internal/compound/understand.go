// Package compound provides high-level compound analysis tools that aggregate
// multiple lower-level analysis primitives into a single result.
package compound

import (
	"context"

	"github.com/anatolykoptev/go-code/internal/callgraph"
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
	// review_pr_post for lookups to succeed.
	Repo string

	// Learnings optionally sources prior review findings for the symbol.
	// When nil (not configured) or Nearest returns no rows, the result's
	// PriorLearnings field stays empty and is omitted from JSON output.
	Learnings LearningsLookup
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

	return result
}

// collectCallees walks the call graph for edges where sym is the caller and
// returns up to max unique callee references.
func collectCallees(cg *callgraph.CallGraph, sym *parser.Symbol, max int) []CallRef {
	var out []CallRef
	seen := make(map[string]struct{})
	for _, edge := range cg.Edges {
		if len(out) >= max {
			break
		}
		if edge.Caller != sym {
			continue
		}
		file := ""
		if edge.Callee != nil {
			file = edge.Callee.File
		}
		key := edge.CalleeName + ":" + file
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, CallRef{
			Name:     edge.CalleeName,
			File:     file,
			Line:     edge.Line,
			Receiver: edge.Receiver,
		})
	}
	return out
}

// collectCallers walks the call graph for edges where sym is the callee and
// returns up to max unique caller references.
func collectCallers(cg *callgraph.CallGraph, sym *parser.Symbol, max int) []CallRef {
	var out []CallRef
	seen := make(map[string]struct{})
	for _, edge := range cg.Edges {
		if len(out) >= max {
			break
		}
		if edge.Callee != sym || edge.Caller == nil {
			continue
		}
		key := edge.Caller.Name + ":" + edge.Caller.File
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, CallRef{
			Name:     edge.Caller.Name,
			File:     edge.Caller.File,
			Line:     edge.Line,
			Receiver: edge.Caller.Receiver,
		})
	}
	return out
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

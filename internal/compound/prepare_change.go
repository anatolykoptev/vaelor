package compound

import (
	"context"
	"log/slog"

	"github.com/anatolykoptev/go-code/internal/callgraph"
	"github.com/anatolykoptev/go-code/internal/deadcode"
	"github.com/anatolykoptev/go-code/internal/graphx"
	"github.com/anatolykoptev/go-code/internal/impact"
)

const (
	// topPageRankSampleSize is the number of top-ranked symbols fetched to
	// compute the repo-level decile threshold.
	topPageRankSampleSize = 20
	// decileDivisor is used to split the sample into deciles (top-10%).
	decileDivisor = 10
)

// PrepareChangeOpts configures pre-change analysis.
type PrepareChangeOpts struct {
	MaxDepth int // max impact traversal depth (default 5)

	// Graph optionally surfaces community and pagerank signals for the target
	// and its direct callers. When nil the new fields stay zero and are omitted.
	Graph graphx.Analytics

	// Repo is the user-supplied repo key used to query the persistent graph.
	// Must be non-empty for Graph queries to fire.
	Repo string
}

// PrepareChangeResult is the output of pre-change risk assessment.
type PrepareChangeResult struct {
	Found    bool             `json:"found"`
	Symbol   SymbolInfo       `json:"symbol,omitempty"`
	Impact   *impact.Result   `json:"impact,omitempty"`
	IsDead   bool             `json:"is_dead"`
	DeadCode *deadcode.Result `json:"dead_code_summary,omitempty"`
	Tier     string           `json:"tier"`
	Warnings []string         `json:"warnings,omitempty"`

	// CommunitiesCrossed is the count of distinct Louvain community IDs among
	// the target symbol and its direct callers. Zero when Graph is cold
	// (all Found=false) — omitted from JSON output in that case.
	CommunitiesCrossed int `json:"communities_crossed,omitempty"`

	// HighPRCallers names direct callers whose PageRank is in the repo-level
	// top decile (top 10% by TopPageRank). Nil when Graph is cold or no
	// caller qualifies — omitted from JSON output in that case.
	HighPRCallers []string `json:"high_pagerank_callers,omitempty"`
}

// PrepareChange runs pre-change risk assessment for a named symbol.
// It aggregates impact analysis (blast radius) and dead code detection.
func PrepareChange(ctx context.Context, cg *callgraph.CallGraph, symbolName string, opts PrepareChangeOpts) *PrepareChangeResult {
	result := &PrepareChangeResult{Tier: cg.Tier}

	impactResult := impact.Analyze(ctx, cg, symbolName, impact.Options{MaxDepth: opts.MaxDepth})
	result.Impact = impactResult
	result.Found = impactResult.Found

	if !result.Found {
		return result
	}

	// Populate symbol info from the call graph.
	for _, sym := range cg.Symbols {
		if sym.Name == symbolName {
			result.Symbol = SymbolInfo{
				Name:       sym.Name,
				Kind:       string(sym.Kind),
				File:       sym.File,
				StartLine:  sym.StartLine,
				EndLine:    sym.EndLine,
				Signature:  sym.Signature,
				Complexity: sym.Complexity,
				Receiver:   sym.Receiver,
			}
			break
		}
	}

	// Run dead code analysis with exported symbols included so we can check our target.
	dcResult := deadcode.Analyze(cg, deadcode.Options{
		IncludeExported: true,
		HookCallbacks:   cg.HookCallbacks,
		Relationships:   cg.TypeRels,
	})
	result.DeadCode = dcResult

	for _, ds := range dcResult.DeadSymbols {
		if ds.Name == symbolName {
			result.IsDead = true
			break
		}
	}

	if result.IsDead {
		result.Warnings = append(result.Warnings, "symbol appears to have no callers — consider removing instead of changing")
	}

	result.CommunitiesCrossed, result.HighPRCallers = enrichWithGraph(ctx, opts, result.Symbol, impactResult.DirectCallers)

	return result
}

// enrichWithGraph resolves community spread and high-pagerank callers using the
// persistent graph. Returns (0, nil) when Graph is nil, Repo is empty, or all
// signals are cold (Found=false). Errors are swallowed with slog.Debug so the
// tool stays functional when the graph is offline.
func enrichWithGraph(ctx context.Context, opts PrepareChangeOpts, target SymbolInfo, callers []impact.AffectedSymbol) (communitiesCrossed int, highPR []string) {
	if opts.Graph == nil || opts.Repo == "" {
		return 0, nil
	}

	// Collect community IDs for target + direct callers.
	communitySet := make(map[string]struct{})
	anyFound := false

	targetSig, err := opts.Graph.Symbol(ctx, opts.Repo, target.Name, target.File)
	if err != nil {
		slog.Debug("graph signals unavailable for target", "symbol", target.Name, "err", err)
	} else if targetSig.Found {
		anyFound = true
		if targetSig.Community != "" {
			communitySet[targetSig.Community] = struct{}{}
		}
	}

	// Fetch pagerank top-decile threshold once.
	topList, err := opts.Graph.TopPageRank(ctx, opts.Repo, topPageRankSampleSize)
	if err != nil {
		slog.Debug("TopPageRank unavailable", "repo", opts.Repo, "err", err)
		topList = nil
	}
	var prThreshold float64
	if len(topList) > 0 {
		idx := len(topList)/decileDivisor - 1
		if idx < 0 {
			idx = len(topList) - 1
		}
		prThreshold = topList[idx].PageRank
	}

	// Resolve each direct caller.
	for _, c := range callers {
		sig, err := opts.Graph.Symbol(ctx, opts.Repo, c.Name, c.File)
		if err != nil {
			slog.Debug("graph signals unavailable for caller", "symbol", c.Name, "err", err)
			continue
		}
		if !sig.Found {
			continue
		}
		anyFound = true
		if sig.Community != "" {
			communitySet[sig.Community] = struct{}{}
		}
		if len(topList) > 0 && sig.PageRank >= prThreshold {
			highPR = append(highPR, c.Name)
		}
	}

	if !anyFound {
		return 0, nil
	}
	return len(communitySet), highPR
}

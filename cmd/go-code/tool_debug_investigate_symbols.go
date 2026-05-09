// cmd/go-code/tool_debug_investigate_symbols.go
package main

import (
	"context"
	"fmt"

	"github.com/anatolykoptev/go-code/internal/analyze"
	"github.com/anatolykoptev/go-code/internal/callgraph"
	"github.com/anatolykoptev/go-code/internal/compound"
	"github.com/anatolykoptev/go-code/internal/deadcode"
	"github.com/anatolykoptev/go-code/internal/investigate"
	"github.com/anatolykoptev/go-code/internal/jaegerclient"
	"github.com/anatolykoptev/go-code/internal/parser"
)

// runSymbolsPhase executes Phase 3: span→operation→symbol correlation.
//
// It counts unique operations across all failed spans, then (if a repo path
// was supplied) attempts to resolve each operation name to a Go symbol using
// the callgraph. The resulting Hypotheses are ranked by investigate.RankHypotheses
// before being stored in res.
//
// Phase γ.B enrichments (applied in order after ranking):
//  1. Dead-code filter: drops hypotheses resolved to dead symbols.
//  2. Impact phase: enriches top-3 with blast radius.
//  3. Symbol body: enriches top-1 with compound.AnalyzeBody (requires OxCodes).
//
// Returns ops — the operation→spanCount map — so Phase 5 can include the
// operations in the LLM prompt.
//
// Note: resolveRoot returns a cleanup func that is deferred inside this
// function. The defer fires when runSymbolsPhase returns (before Phase 4/5
// execute). Nothing after this call reads the repo path, so the earlier
// cleanup is correct.
func runSymbolsPhase(
	ctx context.Context,
	deps analyze.Deps,
	input DebugInvestigateInput,
	traces []jaegerclient.Trace,
	anomalyScore float64,
	res *investigate.InvestigationResult,
) map[string]int {
	// Count unique operations across all failed spans.
	ops := map[string]int{}
	for _, tr := range traces {
		for _, sp := range tr.Spans {
			ops[sp.OperationName]++
			res.Diagnostics.SpansAnalyzed++
		}
	}

	repo := input.Repo
	if repo != "" {
		resolvedRoot, cleanup, resolveErr := resolveRoot(ctx, repo, "", deps)
		if resolveErr != nil {
			res.Diagnostics.Warnings = append(res.Diagnostics.Warnings,
				fmt.Sprintf("resolve root %q: %v", repo, resolveErr))
		} else {
			defer cleanup()
			cg, cgErr := callgraph.BuildFromRepo(ctx, callgraph.TraceRepoInput{
				Root:     resolvedRoot,
				Language: "go",
			})
			if cgErr != nil {
				res.Diagnostics.Warnings = append(res.Diagnostics.Warnings,
					fmt.Sprintf("build callgraph: %v", cgErr))
			}
			// symMap tracks the parser.Symbol for each hypothesis index so
			// Phase γ.B.3 (AnalyzeBody) can access the full symbol struct.
			symMap := make(map[int]*parser.Symbol)
			for op, count := range ops {
				funcName := investigate.OperationToFuncName(op)
				h := investigate.Hypothesis{
					Subject:       fmt.Sprintf("operation %q", op),
					SpanCount:     count,
					AnomalyScore:  anomalyScore,
					EvidenceLinks: []string{fmt.Sprintf("operation=%s; spans=%d", op, count)},
				}
				if cg != nil && funcName != "" {
					matches := compound.FindSymbol(cg.Symbols, funcName)
					if len(matches) > 0 {
						sym := matches[0]
						h.File = reverseToHost(sym.File, deps.PathMappings)
						h.Line = int(sym.StartLine)
						h.Subject = fmt.Sprintf("%s in %s", funcName, h.File)
						h.NextChecks = append(h.NextChecks,
							fmt.Sprintf("understand symbol=%q repo=%q", funcName, repo))
						res.Diagnostics.SymbolsTouched++
						// Invariant: key == index of hypothesis about to be appended at line ~94.
						// Reordering the append below would silently desync this map.
						symMap[len(res.Hypotheses)] = sym
					}
				}
				res.Hypotheses = append(res.Hypotheses, h)
			}

			// γ.B.1: Dead-code filter — build dead set and drop false-positive hypotheses.
			if cg != nil {
				dcResult := deadcode.Analyze(cg, deadcode.Options{
					OxCodes:       deps.OxCodes, // second-pass string-reference scan reduces false positives
					Root:          resolvedRoot, // required for ox-codes queries
					Language:      "go",
					Relationships: cg.TypeRels, // interface-aware filtering: prevents concrete methods
					// from being marked dead when they satisfy an interface
					// with no direct callgraph edge.
					IncludeExported: false, // conservative: exported symbols are not dead by definition
					Ctx:             ctx,
				})
				deadSet := make(map[string]bool, dcResult.DeadCount)
				for _, ds := range dcResult.DeadSymbols {
					deadSet[ds.Name] = true
				}
				res.Hypotheses = filterDeadHypotheses(res.Hypotheses, deadSet, &res.Diagnostics)
			}

			res.Hypotheses = investigate.RankHypotheses(res.Hypotheses)

			// γ.B.2: Impact phase — enrich top-3 surviving hypotheses.
			if cg != nil {
				res.Hypotheses = runImpactPhase(ctx, cg, res.Hypotheses, 3)
			}

			// γ.B.3: Symbol body — enrich top-1 only (requires OxCodes).
			// Build a post-ranking symMap keyed by ranked position 0..n-1.
			// We match by Subject prefix to find the original symbol.
			rankedSymMap := buildRankedSymMap(res.Hypotheses, symMap)
			res.Hypotheses = runSymbolBodyPhase(ctx, res.Hypotheses, rankedSymMap, deps.OxCodes, resolvedRoot, &res.Diagnostics)
		}
	}

	if len(res.Hypotheses) == 0 {
		// No symbol resolution (empty repo or no callgraph) — fall back to
		// frequency-only hypotheses so callers always get something useful.
		for op, count := range ops {
			res.Hypotheses = append(res.Hypotheses, investigate.Hypothesis{
				Subject:       fmt.Sprintf("operation %q", op),
				SpanCount:     count,
				AnomalyScore:  anomalyScore,
				EvidenceLinks: []string{fmt.Sprintf("operation=%s; spans=%d", op, count)},
			})
		}
	}

	if !isRanked(res.Hypotheses) {
		res.Hypotheses = investigate.RankHypotheses(res.Hypotheses)
	}
	return ops
}

// buildRankedSymMap maps ranked hypothesis position → *parser.Symbol by
// matching the ranked hypothesis Subject to the original symMap entries.
// The original symMap is keyed by the hypothesis index *before* ranking.
// After RankHypotheses re-orders, we match by reconstructing the hypothesis
// Subject for each original symMap entry.
func buildRankedSymMap(ranked []investigate.Hypothesis, origSymMap map[int]*parser.Symbol) map[int]*parser.Symbol {
	// Build subject → symbol from original map.
	subjectToSym := make(map[string]*parser.Symbol, len(origSymMap))
	for _, sym := range origSymMap {
		if sym == nil {
			continue
		}
		subjectToSym[sym.Name] = sym
	}
	result := make(map[int]*parser.Symbol, len(ranked))
	for i, h := range ranked {
		name := subjectFuncName(h.Subject)
		if sym, ok := subjectToSym[name]; ok {
			result[i] = sym
		}
	}
	return result
}

// isRanked returns true if RankHypotheses has already been called, detected
// by checking whether any hypothesis has a non-empty Confidence (ranking sets it).
func isRanked(hyps []investigate.Hypothesis) bool {
	for _, h := range hyps {
		if h.Confidence != "" {
			return true
		}
	}
	return false
}

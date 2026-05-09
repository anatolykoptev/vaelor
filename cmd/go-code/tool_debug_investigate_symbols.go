// cmd/go-code/tool_debug_investigate_symbols.go
package main

import (
	"context"
	"fmt"

	"github.com/anatolykoptev/go-code/internal/analyze"
	"github.com/anatolykoptev/go-code/internal/callgraph"
	"github.com/anatolykoptev/go-code/internal/compound"
	"github.com/anatolykoptev/go-code/internal/investigate"
	"github.com/anatolykoptev/go-code/internal/jaegerclient"
)

// runSymbolsPhase executes Phase 3: span→operation→symbol correlation.
//
// It counts unique operations across all failed spans, then (if a repo path
// was supplied) attempts to resolve each operation name to a Go symbol using
// the callgraph. The resulting Hypotheses are ranked by investigate.RankHypotheses
// before being stored in res.
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
					}
				}
				res.Hypotheses = append(res.Hypotheses, h)
			}
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

	res.Hypotheses = investigate.RankHypotheses(res.Hypotheses)
	return ops
}

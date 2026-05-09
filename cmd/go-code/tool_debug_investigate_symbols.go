// cmd/go-code/tool_debug_investigate_symbols.go
package main

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/anatolykoptev/go-code/internal/analyze"
	"github.com/anatolykoptev/go-code/internal/callgraph"
	"github.com/anatolykoptev/go-code/internal/compound"
	"github.com/anatolykoptev/go-code/internal/deadcode"
	"github.com/anatolykoptev/go-code/internal/investigate"
	"github.com/anatolykoptev/go-code/internal/jaegerclient"
	"github.com/anatolykoptev/go-code/internal/parser"
)

// buildOpsMap aggregates span data from traces into a map from operation name
// to OperationInfo. For each operation, the Count is incremented per span seen.
// OTEL semantic-convention tags (http.route, http.method, code.filepath,
// code.lineno, code.namespace) are captured with first-seen-wins semantics for
// stability across trace variations.
//
// code.lineno is handled defensively: the JSON wire format may deliver it as
// float64 (standard JSON numbers), int64 (rare), or string.
func buildOpsMap(traces []jaegerclient.Trace) map[string]*investigate.OperationInfo {
	ops := make(map[string]*investigate.OperationInfo)
	for _, tr := range traces {
		for _, sp := range tr.Spans {
			info, ok := ops[sp.OperationName]
			if !ok {
				info = &investigate.OperationInfo{Operation: sp.OperationName}
				ops[sp.OperationName] = info
			}
			info.Count++

			// First-seen wins for tag values — avoid per-trace noise.
			for _, tag := range sp.Tags {
				if v, ok := tag.Value.(string); ok {
					switch tag.Key {
					case "http.route":
						if info.HTTPRoute == "" {
							info.HTTPRoute = v
						}
					case "http.method":
						if info.HTTPMethod == "" {
							info.HTTPMethod = v
						}
					case "code.filepath":
						if info.CodeFilepath == "" {
							info.CodeFilepath = v
						}
					case "code.namespace":
						if info.CodeNamespace == "" {
							info.CodeNamespace = v
						}
					}
				}
				// code.lineno may arrive as float64/int64/string — handle all.
				if tag.Key == "code.lineno" && info.CodeLineno == 0 {
					switch v := tag.Value.(type) {
					case float64:
						info.CodeLineno = int(v)
					case int64:
						info.CodeLineno = int(v)
					case string:
						if n, err := strconv.Atoi(v); err == nil {
							info.CodeLineno = n
						}
					}
				}
			}
		}
	}
	return ops
}

// runSymbolsPhase executes Phase 3: span→operation→symbol correlation.
//
// It aggregates unique operations across all spans via buildOpsMap, then for
// each operation attempts one of two resolution strategies:
//
//  1. PREFERRED: OTEL code.* tags (code.filepath + code.lineno present) →
//     direct file:line resolution without a callgraph. Works for tower-http /
//     any OTEL-instrumented service emitting semantic-convention code attributes.
//     This path runs regardless of whether a repo was provided.
//
//  2. FALLBACK: OperationToFuncName + compound.FindSymbol — existing Go path.
//     Requires a callgraph built from the repo.
//
// The resulting Hypotheses are ranked by investigate.RankHypotheses before
// being stored in res.
//
// Phase γ.B enrichments (applied in order after ranking):
//  1. Dead-code filter: drops hypotheses resolved to dead symbols.
//  2. Impact phase: enriches top-3 with blast radius.
//  3. Symbol body: enriches top-1 with compound.AnalyzeBody (requires OxCodes).
//
// Returns ops — the operation→OperationInfo map — so Phase 5 can include the
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
) map[string]*investigate.OperationInfo {
	// Aggregate unique operations and their OTEL tags across all spans.
	ops := buildOpsMap(traces)
	for _, tr := range traces {
		res.Diagnostics.SpansAnalyzed += len(tr.Spans)
	}

	// symMap tracks the parser.Symbol for each hypothesis index so
	// Phase γ.B.3 (AnalyzeBody) can access the full symbol struct.
	symMap := make(map[int]*parser.Symbol)

	// resolvedFromCodeTags tracks operations resolved via code.* path so the
	// callgraph fallback skips them.
	resolvedFromCodeTags := make(map[string]bool, len(ops))

	// PASS 1: Preferred path — OTEL code.* tags give file:line directly.
	// Runs regardless of whether a repo was provided (no callgraph needed).
	for op, info := range ops {
		if info.CodeFilepath == "" || info.CodeLineno == 0 {
			continue
		}
		h := investigate.Hypothesis{
			Subject:       fmt.Sprintf("operation %q", op),
			SpanCount:     info.Count,
			AnomalyScore:  anomalyScore,
			EvidenceLinks: []string{fmt.Sprintf("operation=%s; spans=%d", op, info.Count)},
		}
		h.File = reverseToHost(info.CodeFilepath, deps.PathMappings)
		h.Line = info.CodeLineno
		symbol := info.CodeNamespace
		if symbol == "" {
			symbol = op
		}
		if info.HTTPRoute != "" {
			h.Subject = fmt.Sprintf("%s %s in %s:%d",
				info.HTTPMethod, info.HTTPRoute, h.File, info.CodeLineno)
		} else {
			h.Subject = fmt.Sprintf("%s in %s:%d", symbol, h.File, info.CodeLineno)
		}
		h.NextChecks = append(h.NextChecks, investigate.NextCheck{
			Tool: "understand",
			Args: map[string]string{
				"file": h.File,
				"line": fmt.Sprintf("%d", h.Line),
				"repo": input.Repo,
			},
		})
		res.Diagnostics.SymbolsTouched++
		res.Hypotheses = append(res.Hypotheses, h)
		resolvedFromCodeTags[op] = true
	}

	// PASS 2: Fallback — OperationToFuncName + callgraph for Go services
	// without code.* tags. Requires a repo path to be provided.
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
			for op, info := range ops {
				if resolvedFromCodeTags[op] {
					continue // already resolved via code.* path
				}
				h := investigate.Hypothesis{
					Subject:       fmt.Sprintf("operation %q", op),
					SpanCount:     info.Count,
					AnomalyScore:  anomalyScore,
					EvidenceLinks: []string{fmt.Sprintf("operation=%s; spans=%d", op, info.Count)},
				}
				funcName := investigate.OperationToFuncName(op)
				if cg != nil && funcName != "" {
					matches := compound.FindSymbol(cg.Symbols, funcName)
					if len(matches) > 0 {
						sym := matches[0]
						h.File = reverseToHost(sym.File, deps.PathMappings)
						h.Line = int(sym.StartLine)
						h.Subject = fmt.Sprintf("%s in %s", funcName, h.File)
						h.NextChecks = append(h.NextChecks,
							investigate.NextCheck{
								Tool: "understand",
								Args: map[string]string{
									"symbol": funcName,
									"repo":   repo,
								},
							})
						res.Diagnostics.SymbolsTouched++
						// Invariant: key == index of hypothesis about to be appended.
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

			// γ.C.3: Hint-driven codesearch — append hint_match hypotheses and re-rank.
			// Re-rank is mandatory: at this point existing hypotheses already have
			// Confidence set by the first RankHypotheses call above, so isRanked()
			// returns true and the final guard below would skip ranking entirely,
			// leaving hint_match hypotheses unordered at the tail.
			if input.Hint != "" {
				hintCtx, hintCancel := context.WithTimeout(ctx, 5*time.Second)
				hintMatches := runHintSearch(hintCtx, input.Hint, resolvedRoot)
				hintCancel() // synchronous — purpose-bounded scope, not deferred
				if len(hintMatches) > 0 {
					res.Hypotheses = applyHintMatches(res.Hypotheses, hintMatches)
					// Clear Confidence so RankHypotheses re-fills all entries uniformly.
					for i := range res.Hypotheses {
						res.Hypotheses[i].Confidence = ""
					}
					res.Hypotheses = investigate.RankHypotheses(res.Hypotheses)
				}
			}
		}
	}

	if len(res.Hypotheses) == 0 {
		// No symbol resolution (empty repo or no callgraph) — fall back to
		// frequency-only hypotheses so callers always get something useful.
		for op, info := range ops {
			res.Hypotheses = append(res.Hypotheses, investigate.Hypothesis{
				Subject:       fmt.Sprintf("operation %q", op),
				SpanCount:     info.Count,
				AnomalyScore:  anomalyScore,
				EvidenceLinks: []string{fmt.Sprintf("operation=%s; spans=%d", op, info.Count)},
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

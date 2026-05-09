// cmd/go-code/tool_debug_investigate_upstream.go
//
// Sprint B2: upstream callgraph walk for root-cause discovery.
//
// Motivation (dogfooding gap): Phase 3 attributed the anomaly to session.rs:226
// (the UDP send site) but the actual root cause was accept_renegotiation_answer
// (the caller's caller). B1 gives the LLM the function body; B2 ensures the
// upstream callers are in the hypothesis pool BEFORE FusionRank, so the rank
// pipeline (recency + complexity + impact) can promote the actual culprit.
package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/anatolykoptev/go-code/internal/callgraph"
	"github.com/anatolykoptev/go-code/internal/investigate"
	"github.com/anatolykoptev/go-code/internal/parser"
)

// runUpstreamPhase walks the callgraph upstream from the top-topN hypotheses
// and appends caller hypotheses with Source=upstream_caller.
//
// Only hypotheses with Source=="" (Tier-3) or Source==HypothesisSourceSpan
// are eligible seeds — hint_match and alert hypotheses lack a callgraph anchor.
//
// Each ancestor gets AnomalyScore = 0.4 / depth so depth-1 callers score 0.4
// and depth-2 score 0.2. FusionRank's recency and complexity signals will boost
// the actual culprit above this baseline.
//
// Total upstream additions are capped at 9 across all seeds (≈ topN=3 × depth=2
// × ~1.5 callers each). Duplicates against the existing pool are skipped.
//
// Best-effort: cg=nil or empty hyps returns hyps unchanged.
func runUpstreamPhase(
	ctx context.Context,
	cg *callgraph.CallGraph,
	hyps []investigate.Hypothesis,
	topN, maxDepth int,
) []investigate.Hypothesis {
	if cg == nil || len(hyps) == 0 {
		return hyps
	}

	const maxAdditions = 9

	seen := buildHypothesisKeySet(hyps)
	added := 0

	limit := topN
	if limit > len(hyps) {
		limit = len(hyps)
	}

	for i := 0; i < limit; i++ {
		if added >= maxAdditions {
			break
		}
		h := &hyps[i]
		// Skip non-span-attributed hypotheses — they lack a callgraph anchor.
		if h.Source != "" && h.Source != investigate.HypothesisSourceSpan {
			continue
		}
		symbolName := extractSymbolNameFromSubject(h.Subject)
		if symbolName == "" {
			continue
		}
		result := callgraph.Trace(ctx, cg, symbolName, callgraph.TraceOpts{
			Direction: "callers",
			MaxDepth:  maxDepth,
		})
		// Walk the tree; baseDepth=0 so the root (queried symbol) is skipped.
		for _, node := range flattenTraceTree(result.Tree, 0) {
			if added >= maxAdditions {
				break
			}
			key := node.symbol.Name + "@" + node.symbol.File
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			hyps = append(hyps, investigate.Hypothesis{
				Subject:       fmt.Sprintf("%s in %s:%d", node.symbol.Name, node.symbol.File, node.symbol.StartLine),
				File:          node.symbol.File,
				Line:          int(node.symbol.StartLine),
				EndLine:       int(node.symbol.EndLine),
				AnomalyScore:  0.4 / float64(node.depth),
				EvidenceLinks: []string{fmt.Sprintf("upstream caller of %s (depth=%d)", h.Subject, node.depth)},
				Source:        investigate.HypothesisSourceUpstream,
			})
			added++
		}
	}
	return hyps
}

// extractSymbolNameFromSubject extracts the function name from a Tier-3
// hypothesis Subject of the form "funcName in /path/to/file.rs:123".
//
// Returns "" for subjects that are not in that form (Tier-1 routes like
// "GET /api/x", Tier-2 "operation \"req\"", or bare strings without " in ").
// A non-empty return is guaranteed to be a single token (no spaces) — if the
// part before " in " contains a space it is not a symbol name and "" is returned.
func extractSymbolNameFromSubject(subject string) string {
	idx := strings.Index(subject, " in ")
	if idx <= 0 {
		return ""
	}
	name := subject[:idx]
	// If the name part contains a space it is not a bare function name
	// (e.g. "GET /api/x in /handler.rs:10" → "GET /api/x" has a space).
	if strings.ContainsRune(name, ' ') {
		return ""
	}
	return name
}

// flatNode is a flattened caller node with its depth relative to the queried symbol.
type flatNode struct {
	symbol *parser.Symbol
	depth  int
}

// flattenTraceTree flattens a CallChainNode tree into a slice of flatNodes.
// baseDepth=0 skips the root (depth 0 = queried symbol) and emits nodes
// starting at depth 1. Works for both callers and callees — the tree shape
// from callgraph.Trace is identical regardless of direction. Nodes with nil
// Symbol are skipped.
func flattenTraceTree(tree []callgraph.CallChainNode, baseDepth int) []flatNode {
	var out []flatNode
	for _, n := range tree {
		if n.Symbol == nil {
			continue
		}
		if n.Cycle {
			// Skip cycle-sentinel nodes — they reference an already-visited
			// ancestor; emitting would dedup-miss (different depth path) and
			// add a noise hypothesis with artificially low score that
			// undercuts the original. Children of a cycle node are never
			// traversed (Tree.traceNode returns empty Children for cycles).
			continue
		}
		if baseDepth > 0 {
			// Emit non-root nodes.
			out = append(out, flatNode{symbol: n.Symbol, depth: baseDepth})
		}
		out = append(out, flattenTraceTree(n.Children, baseDepth+1)...)
	}
	return out
}

// buildHypothesisKeySet builds a set of "name@file" keys from existing
// hypotheses for deduplication in runUpstreamPhase.
func buildHypothesisKeySet(hyps []investigate.Hypothesis) map[string]struct{} {
	seen := make(map[string]struct{}, len(hyps))
	for _, h := range hyps {
		if h.File == "" {
			continue
		}
		name := extractSymbolNameFromSubject(h.Subject)
		if name == "" {
			// Fall back to subject as key for non-Tier-3 hypotheses.
			seen[h.Subject+"@"+h.File] = struct{}{}
		} else {
			seen[name+"@"+h.File] = struct{}{}
		}
	}
	return seen
}

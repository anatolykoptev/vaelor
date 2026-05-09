// cmd/go-code/tool_debug_investigate_downstream.go
//
// Sprint B4: downstream callgraph walk for root-cause discovery.
//
// Motivation: B2 (upstream) walks callers UP from the symptom site. But for
// cases like SFU session.rs:226 (event-loop run) → bug is in
// accept_renegotiation_answer (renegotiation.rs:363) which the loop CALLS,
// we need to walk DOWN. B4 is symmetric to B2 but follows callees.
package main

import (
	"context"
	"fmt"

	"github.com/anatolykoptev/go-code/internal/callgraph"
	"github.com/anatolykoptev/go-code/internal/investigate"
)

const (
	downstreamMaxDepth     = 2
	downstreamMaxAdditions = 9
)

// runDownstreamPhase walks callees of the top-1 hypothesis 1-2 hops down
// and adds each as a candidate with Source=downstream_callee.
//
// Symmetric to runUpstreamPhase but with key differences:
//   - Top-1 only (not top-3): fan-out grows exponentially; capping per-source
//     avoids flooding the hypothesis pool with low-signal deep callees.
//   - AnomalyScore 0.3/depth (vs 0.4/depth for callers): symptom > caller >
//     callee in attribution priority, so callees start at a lower baseline.
//   - Skips non-span seeds: upstream-caller and other derived hypotheses must
//     not be walked downstream (prevents recursive compounding of synthetic hypotheses).
//
// Best-effort: cg=nil or empty hyps returns hyps unchanged.
func runDownstreamPhase(
	ctx context.Context,
	cg *callgraph.CallGraph,
	hyps []investigate.Hypothesis,
	maxDepth int,
) []investigate.Hypothesis {
	if cg == nil || len(hyps) == 0 {
		return hyps
	}

	seen := buildHypothesisKeySet(hyps)

	// Only top-1 seed; eligibility same as upstream: span source or no source.
	h := &hyps[0]
	if h.Source != "" && h.Source != investigate.HypothesisSourceSpan {
		return hyps
	}
	symbolName := extractSymbolNameFromSubject(h.Subject)
	if symbolName == "" {
		return hyps
	}

	result := callgraph.Trace(ctx, cg, symbolName, callgraph.TraceOpts{
		Direction: "callees",
		MaxDepth:  maxDepth,
	})

	// flattenCallers traverses any tree from callgraph.Trace regardless of
	// direction — the tree shape is identical for callees and callers.
	added := 0
	for _, child := range flattenCallers(result.Tree, 0) {
		if added >= downstreamMaxAdditions {
			break
		}
		if child.depth == 0 {
			continue
		}
		key := child.symbol.Name + "@" + child.symbol.File
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		hyps = append(hyps, investigate.Hypothesis{
			Subject:       fmt.Sprintf("%s in %s:%d", child.symbol.Name, child.symbol.File, child.symbol.StartLine),
			File:          child.symbol.File,
			Line:          int(child.symbol.StartLine),
			EndLine:       int(child.symbol.EndLine),
			AnomalyScore:  0.3 / float64(child.depth),
			EvidenceLinks: []string{fmt.Sprintf("downstream callee of %s (depth=%d)", h.Subject, child.depth)},
			Source:        investigate.HypothesisSourceDownstream,
		})
		added++
	}
	return hyps
}

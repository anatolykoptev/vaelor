// cmd/go-code/tool_debug_investigate_impact.go
package main

import (
	"context"

	"github.com/anatolykoptev/go-code/internal/callgraph"
	"github.com/anatolykoptev/go-code/internal/impact"
	"github.com/anatolykoptev/go-code/internal/investigate"
)

// runImpactPhase enriches the top-k hypotheses with blast radius data.
// Hypotheses beyond index k-1 get nil Impact. Hypotheses with no CG
// (cg == nil) are skipped silently. Returns the enriched slice (modified in place).
func runImpactPhase(
	ctx context.Context,
	cg *callgraph.CallGraph,
	hyps []investigate.Hypothesis,
	topK int,
) []investigate.Hypothesis {
	if cg == nil {
		return hyps
	}
	for i := range hyps {
		if i >= topK {
			break
		}
		subj := subjectFuncName(hyps[i].Subject)
		if subj == "" {
			continue
		}
		r := impact.Analyze(ctx, cg, subj, impact.Options{})
		if r == nil {
			continue
		}
		hyps[i].Impact = &investigate.ImpactInfo{
			DirectCallers: len(r.DirectCallers),
			TotalAffected: r.TotalAffected,
			BlastRadius:   r.BlastRadius,
			RiskScore:     r.RiskScore,
		}
	}
	return hyps
}

// applyImpactStub is a testable variant of impact enrichment that accepts a
// function instead of live infrastructure, enabling unit tests without a
// real call graph.
func applyImpactStub(
	hyps []investigate.Hypothesis,
	fn func(subject string) *investigate.ImpactInfo,
	topK int,
) []investigate.Hypothesis {
	out := make([]investigate.Hypothesis, len(hyps))
	copy(out, hyps)
	for i := range out {
		if i >= topK {
			break
		}
		out[i].Impact = fn(out[i].Subject)
	}
	return out
}

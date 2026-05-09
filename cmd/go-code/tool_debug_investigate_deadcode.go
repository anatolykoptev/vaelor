// cmd/go-code/tool_debug_investigate_deadcode.go
package main

import (
	"strings"

	"github.com/anatolykoptev/go-code/internal/investigate"
)

// filterDeadHypotheses removes hypotheses whose resolved subject name matches
// a dead-code symbol. Only hypotheses with File != "" are eligible for removal
// (unresolved hypotheses have no symbol to match). Increments
// diag.HypothesesDroppedAsDead for each dropped hypothesis.
//
// deadSet is keyed by symbol name (e.g. "DeadFn"). The subject field of a
// resolved hypothesis is typically "FuncName in /path/to/file.go" — we match
// the first space-delimited token.
func filterDeadHypotheses(
	hyps []investigate.Hypothesis,
	deadSet map[string]bool,
	diag *investigate.Diagnostics,
) []investigate.Hypothesis {
	if len(deadSet) == 0 {
		return hyps
	}
	out := hyps[:0:0] // reuse underlying array type but new header
	out = make([]investigate.Hypothesis, 0, len(hyps))
	for _, h := range hyps {
		// Only filter hypotheses that have been resolved to a file location.
		if h.File == "" {
			out = append(out, h)
			continue
		}
		name := subjectFuncName(h.Subject)
		if deadSet[name] {
			diag.HypothesesDroppedAsDead++
			continue
		}
		out = append(out, h)
	}
	return out
}

// subjectFuncName extracts the leading function name from a hypothesis Subject.
// Subjects are formatted as "FuncName in /path/file.go" or just "FuncName".
func subjectFuncName(subject string) string {
	if idx := strings.Index(subject, " "); idx > 0 {
		return subject[:idx]
	}
	return subject
}

package compound

import (
	"github.com/anatolykoptev/go-code/internal/callgraph"
	"github.com/anatolykoptev/go-code/internal/deadcode"
	"github.com/anatolykoptev/go-code/internal/impact"
)

// PrepareChangeOpts configures pre-change analysis.
type PrepareChangeOpts struct {
	MaxDepth int // max impact traversal depth (default 5)
}

// PrepareChangeResult is the output of pre-change risk assessment.
type PrepareChangeResult struct {
	Found    bool            `json:"found"`
	Symbol   SymbolInfo      `json:"symbol,omitempty"`
	Impact   *impact.Result  `json:"impact,omitempty"`
	IsDead   bool            `json:"is_dead"`
	DeadCode *deadcode.Result `json:"dead_code_summary,omitempty"`
	Tier     string          `json:"tier"`
	Warnings []string        `json:"warnings,omitempty"`
}

// PrepareChange runs pre-change risk assessment for a named symbol.
// It aggregates impact analysis (blast radius) and dead code detection.
func PrepareChange(cg *callgraph.CallGraph, symbolName string, opts PrepareChangeOpts) *PrepareChangeResult {
	result := &PrepareChangeResult{Tier: cg.Tier}

	impactResult := impact.Analyze(cg, symbolName, impact.Options{MaxDepth: opts.MaxDepth})
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

	return result
}

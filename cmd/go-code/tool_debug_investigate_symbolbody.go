// cmd/go-code/tool_debug_investigate_symbolbody.go
package main

import (
	"context"

	"github.com/anatolykoptev/vaelor/internal/compound"
	"github.com/anatolykoptev/vaelor/internal/investigate"
	"github.com/anatolykoptev/vaelor/internal/oxcodes"
	"github.com/anatolykoptev/vaelor/internal/parser"
)

// runSymbolBodyPhase enriches the top-1 hypothesis with compound.AnalyzeBody
// results. Gate: if oxClient is nil, appends a diagnostic warning and returns
// unchanged. If the top-1 hypothesis has no matching parser.Symbol in symMap,
// it is silently skipped.
func runSymbolBodyPhase(
	ctx context.Context,
	hyps []investigate.Hypothesis,
	symMap map[int]*parser.Symbol,
	oxClient *oxcodes.Client,
	root string,
	diag *investigate.Diagnostics,
) []investigate.Hypothesis {
	if len(hyps) == 0 {
		return hyps
	}
	if oxClient == nil {
		diag.Warnings = append(diag.Warnings, "symbol body skipped: ox-codes client not configured")
		return hyps
	}
	sym := symMap[0] // top-1 only
	if sym == nil {
		return hyps
	}
	ba := compound.AnalyzeBody(ctx, oxClient, root, sym)
	if ba == nil {
		return hyps
	}
	hyps[0].SymbolBody = &investigate.SymbolBodyInfo{
		ErrorExits:      ba.ErrorExits,
		HasDeferCleanup: ba.HasDeferCleanup,
		HasTODO:         ba.HasTODO,
	}
	return hyps
}

// applySymbolBodyStub is a testable variant that accepts a stub instead of
// live infrastructure.
func applySymbolBodyStub(
	hyps []investigate.Hypothesis,
	fn func(subject string) *investigate.SymbolBodyInfo,
) []investigate.Hypothesis {
	out := make([]investigate.Hypothesis, len(hyps))
	copy(out, hyps)
	if len(out) == 0 {
		return out
	}
	out[0].SymbolBody = fn(out[0].Subject)
	return out
}

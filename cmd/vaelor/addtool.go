package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/anatolykoptev/vaelor/internal/argnorm"
	"github.com/anatolykoptev/vaelor/internal/mcpmeta"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// addTool is the budget-aware wrapper around argnorm.AddTool (which itself
// registers through mcpserver.AddTool and records the tool's accepted
// property set in the argnorm registry — see internal/argnorm/registry.go).
// Every tool registration in this package MUST go through addTool (guarded
// by TestNoDirectMCPServerAddTool in argnorm_registration_test.go): calling
// mcpserver.AddTool directly would bypass the argnorm registry, and the
// normalization middleware fail-closes on registry membership — the tool
// would be silently uncallable ("unknown tool"). addTool wraps the handler
// so every response also gets:
//
//  1. Response budget shaping (default 8 KB) — when the response text
//     exceeds the budget, the RANKED HEAD is kept and a continuation footer
//     is appended so the agent knows the tail was truncated and how to
//     narrow/paginate.
//  2. A compact took_ms footer — one-line observability on every response.
//
// Tools that accept a max_bytes / max_tokens override should call
// mcpmeta.Shape on their output text themselves before returning; the
// wrapper detects already-shaped output (mcpmeta.IsShaped) and skips
// double-shaping. The took_ms footer is always appended (idempotent —
// tools that already emitted one are not double-tagged).
//
// Error results (IsError=true) are returned unchanged — they are already
// short and budget-shaping an error message would bury the diagnostic.
func addTool[In any](
	s *mcp.Server,
	t *mcp.Tool,
	h func(context.Context, *mcp.CallToolRequest, In) (*mcp.CallToolResult, error),
) {
	argnorm.AddTool(s, t, func(ctx context.Context, req *mcp.CallToolRequest, in In) (*mcp.CallToolResult, error) {
		t0 := time.Now()
		res, err := h(ctx, req, in)
		if err != nil {
			// mcpserver.AddTool (via argnorm.AddTool) converts errors to
			// toolError results; we let that happen by returning the error
			// as-is. No footer on errors.
			return res, err
		}
		if res == nil {
			return res, nil
		}
		// Skip shaping/footer for error results — they are short by construction.
		if res.IsError {
			return res, nil
		}
		elapsed := time.Since(t0)
		applyBudgetAndTook(res, elapsed)
		return res, nil
	})
}

// applyBudgetAndTook mutates res in place: applies the default response
// budget shaping to the first text content block, then appends the took_ms
// footer. Already-shaped output (from a tool that applied a custom budget)
// is not re-shaped; already-took-tagged output is not double-tagged.
func applyBudgetAndTook(res *mcp.CallToolResult, elapsed time.Duration) {
	if res == nil || res.IsError {
		return
	}
	if len(res.Content) == 0 {
		return
	}
	tc, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		return
	}
	text := tc.Text
	// Budget shaping — skip if the tool already shaped its output.
	if !mcpmeta.IsShaped(text) {
		text = mcpmeta.Shape(text, mcpmeta.DefaultBudget, "")
	}
	// took_ms footer — idempotent.
	text = mcpmeta.AppendTook(text, elapsed)
	tc.Text = text
	res.Content[0] = tc
}

// softDeadlineResult wraps a partial result text with the partial footer
// and the took_ms tag. Used by tools that hit the soft deadline and need
// to return what they have so far.
func softDeadlineResult(text string, skipped string, elapsed time.Duration) *mcp.CallToolResult {
	out := text
	if !mcpmeta.IsShaped(out) {
		out = mcpmeta.Shape(out, mcpmeta.DefaultBudget, "")
	}
	out += mcpmeta.PartialFooter(skipped)
	out = mcpmeta.AppendTook(out, elapsed)
	return textResult(out)
}

// budgetOverride resolves a per-call max_bytes override against the default
// budget. Returns the effective budget in bytes. override <= 0 → default.
func budgetOverride(override int) int {
	return mcpmeta.ResolveBudget(override, mcpmeta.DefaultBudget)
}

// logSoftDeadlineHit records that a tool hit its soft deadline, for ops
// visibility. Non-fatal — just a structured log line.
func logSoftDeadlineHit(tool string, elapsed time.Duration) {
	slog.Warn("soft deadline hit — returning partial result",
		slog.String("tool", tool),
		slog.Duration("elapsed", elapsed),
	)
}

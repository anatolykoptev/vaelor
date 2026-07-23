package mcpmeta

import (
	"context"
	"time"
)

// DefaultSoftDeadline is the default internal per-tool soft deadline.
// It sits below common MCP client timeouts (30s for semantic_search,
// 95s for code_compare) so the tool can return PARTIAL results with a
// continuation handle instead of computing past the point anyone is
// listening and returning nothing.
const DefaultSoftDeadline = 25 * time.Second

// SlowToolSoftDeadline is for the heavy tools (explore, code_compare,
// dataflow_analyze) that legitimately take tens of seconds on large repos and
// run under the ~95-100s external proxy budget. It sits safely below that hard
// kill (keepalive holds the transport for long calls) so these tools COMPLETE
// when they can and only fall back to a partial result near the real limit —
// the default 25s would fire before dataflow's own 30s ox-codes stage even
// finishes, needlessly returning partial on every non-trivial repo.
const SlowToolSoftDeadline = 80 * time.Second

// SoftDeadline wraps a context with the default soft deadline. The returned
// context is cancelled when the deadline expires; callers should check
// ctx.Err() at natural boundaries and return a partial result when it fires.
//
// If the parent context already has an earlier deadline, that deadline is
// preserved (the shorter of the two wins).
func SoftDeadline(ctx context.Context) (context.Context, context.CancelFunc) {
	return SoftDeadlineWith(ctx, DefaultSoftDeadline)
}

// SoftDeadlineWith is the configurable variant. d <= 0 returns the parent
// context unchanged with a no-op cancel.
func SoftDeadlineWith(ctx context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	if d <= 0 {
		return ctx, func() {}
	}
	// If the parent already has a shorter deadline, respect it.
	if parentDeadline, ok := ctx.Deadline(); ok {
		remaining := time.Until(parentDeadline)
		if remaining < d {
			d = remaining
		}
	}
	ctx, cancel := context.WithTimeout(ctx, d)
	return ctx, cancel
}

// PartialFooter returns the partial-result footer to append when a soft
// deadline fires:
//
//	partial: true — <what>
//
// what describes what was skipped (e.g. "LLM analysis, route diff, 3/5
// enrichment stages").
func PartialFooter(what string) string {
	if what == "" {
		what = "some stages skipped"
	}
	return "\npartial: true — " + what
}

package mcpmeta

import (
	"fmt"
	"strings"
	"time"
)

// DefaultBudget is the default per-tool response budget in bytes (~8 KB).
// MCP clients (Devin CLI) truncate tool results at ~10149 chars; staying
// below this ceiling ensures the tail (summaries, verdicts, continuation
// handles) is never silently lost. Tools that return ranked lists should
// truncate at this budget and emit a continuation footer via Shape.
const DefaultBudget = 8192

// MaxBudget is the ceiling for a per-call budget override. Clients hard-cut
// tool results at ~10149 bytes (observed: Devin CLI); an uncapped override at
// or above that ceiling would make Shape a no-op and reintroduce silent
// client-side truncation — tail AND continuation footer lost. 9000 leaves
// headroom for the took_ms footer and the argnorm note under the ceiling.
const MaxBudget = 9000

// MinBudget is the floor for a per-call budget override — anything smaller
// would leave no room for even a single result item plus the footer.
const MinBudget = 512

// truncationFooterPrefix is the sentinel prefix of the continuation footer
// emitted by Shape. Tools can check for it to detect already-shaped output.
const truncationFooterPrefix = "\n[truncated:"

// tookFooterPrefix is the sentinel prefix of the took_ms footer.
const tookFooterPrefix = "\ntook_ms="

// Shape applies a response budget to text. When the text fits within budget
// (or budget <= 0), it is returned unchanged. When it exceeds the budget,
// Shape truncates at the last newline that fits within the budget and
// appends a continuation footer:
//
//	[truncated: N more chars — <hint>]
//
// continuationHint is the tool-specific guidance shown to the agent (e.g.
// "narrow with tier=exact or pass offset=20"). When empty, a generic hint
// is used.
//
// If the text has no newline within the budget, it is hard-truncated at the
// budget boundary. The footer itself is NOT counted against the budget so
// the agent always sees the continuation handle.
func Shape(text string, budget int, continuationHint string) string {
	if budget <= 0 || len(text) <= budget {
		return text
	}
	if budget < MinBudget {
		budget = MinBudget
	}

	// Try to truncate at the last newline within the budget.
	cut := budget
	lastNL := strings.LastIndex(text[:budget], "\n")
	if lastNL > budget/2 { //nolint:mnd // only back up if we keep at least half
		cut = lastNL
	}

	head := text[:cut]
	remaining := len(text) - cut

	hint := continuationHint
	if hint == "" {
		hint = "narrow your query or pass a tighter limit"
	}
	footer := fmt.Sprintf("%s %d more chars — %s]", truncationFooterPrefix, remaining, hint)
	return head + footer
}

// IsShaped reports whether text already carries a truncation footer emitted
// by Shape. Used by the addTool wrapper to skip double-shaping when a tool
// handler already applied a custom budget.
func IsShaped(text string) bool {
	return strings.Contains(text, truncationFooterPrefix)
}

// HasTookFooter reports whether text already carries a took_ms footer.
func HasTookFooter(text string) bool {
	return strings.Contains(text, tookFooterPrefix)
}

// TookFooter returns a compact one-line observability footer:
//
//	took_ms=N
//
// elapsed is clamped to >= 1 ms. The footer is newline-prefixed so it can
// be appended to any response body (text, XML, JSON) without merging into
// the last line.
func TookFooter(elapsed time.Duration) string {
	ms := elapsed.Milliseconds()
	if ms < 1 {
		ms = 1
	}
	return fmt.Sprintf("%s%d", tookFooterPrefix, ms)
}

// AppendTook returns text with the took_ms footer appended, unless the text
// already carries one (idempotent).
func AppendTook(text string, elapsed time.Duration) string {
	if HasTookFooter(text) {
		return text
	}
	return text + TookFooter(elapsed)
}

// ResolveBudget picks the effective budget from a per-call override and the
// default. A non-positive override yields the default; an override below
// MinBudget is clamped to MinBudget; an override above MaxBudget is clamped
// to MaxBudget (see MaxBudget for why the ceiling exists).
func ResolveBudget(override, defaultBudget int) int {
	if override <= 0 {
		return defaultBudget
	}
	if override < MinBudget {
		return MinBudget
	}
	if override > MaxBudget {
		return MaxBudget
	}
	return override
}

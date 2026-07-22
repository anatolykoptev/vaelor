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

// budgetAppliedMarker is a sentinel appended by ShapeWithHint when the text
// fits within the budget — it signals to the addTool wrapper that a per-call
// budget was already applied and the wrapper must NOT re-shape with the
// default budget (#582). Without this, a tool that passes max_bytes > default
// would have its text re-shaped by the wrapper with the smaller default budget,
// losing the tool-specific pagination hint.
const budgetAppliedMarker = "\n[budget-applied]"

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

// ShapeWithHint is like Shape but guarantees the tool-specific continuationHint
// is preserved even when the text fits within the budget. When the text fits,
// ShapeWithHint appends a budget-applied marker so the addTool wrapper detects
// already-shaped output (IsShaped=true) and skips re-shaping with the default
// budget — which would truncate the text and replace the tool-specific hint
// with a generic one (#582).
//
// Use ShapeWithHint (instead of Shape) when the tool has a per-call max_bytes
// override that may be larger than the default budget. When the text fits
// within the override, only the budget-applied marker is appended; the wrapper
// strips it (StripBudgetMarker) before the agent sees the output, so it's
// invisible. When the text exceeds the override, the hint is appended as a
// truncation footer (same as Shape).
func ShapeWithHint(text string, budget int, continuationHint string) string {
	if budget <= 0 || len(text) <= budget {
		// Text fits — mark as budget-applied so the wrapper doesn't re-shape
		// with the default budget (which would lose the tool-specific hint).
		if budget > 0 {
			return text + budgetAppliedMarker
		}
		return text
	}
	// Text exceeds budget — truncate with the tool-specific hint (same as Shape).
	return Shape(text, budget, continuationHint)
}

// IsShaped reports whether text already carries a truncation footer emitted
// by Shape, or a budget-applied marker emitted by ShapeWithHint. Used by the
// addTool wrapper to skip double-shaping when a tool handler already applied
// a custom budget (#582).
func IsShaped(text string) bool {
	return strings.Contains(text, truncationFooterPrefix) ||
		strings.Contains(text, budgetAppliedMarker)
}

// HasTookFooter reports whether text already carries a took_ms footer.
func HasTookFooter(text string) bool {
	return strings.Contains(text, tookFooterPrefix)
}

// StripBudgetMarker removes the budget-applied sentinel from text if present.
// Called by the addTool wrapper after IsShaped check so the marker is not
// visible in the final agent-facing output (#582).
func StripBudgetMarker(text string) string {
	return strings.ReplaceAll(text, budgetAppliedMarker, "")
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

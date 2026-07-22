package main

import (
	"strings"
	"testing"
	"time"
)

// TestAddTookFooter_PresentOnEveryResponse verifies that the addTool wrapper
// appends a took_ms footer to every non-error tool response. This is the
// observability contract from #572.
func TestAddTookFooter_PresentOnEveryResponse(t *testing.T) {
	// Simulate what the addTool wrapper does after a handler returns.
	res := textResult("hello world")
	applyBudgetAndTook(res, 42*time.Millisecond)

	got := textContentOf(t, res)
	if !strings.Contains(got, "took_ms=") {
		t.Fatalf("response must contain took_ms footer, got:\n%s", got)
	}
	if !strings.Contains(got, "took_ms=42") {
		t.Fatalf("footer must show 42ms, got:\n%s", got)
	}
}

// TestAddTookFooter_NotOnErrors verifies that error results are returned
// unchanged (no budget shaping, no took_ms).
func TestAddTookFooter_NotOnErrors(t *testing.T) {
	res := errResult("something went wrong")
	applyBudgetAndTook(res, 10*time.Millisecond)

	got := textContentOf(t, res)
	if strings.Contains(got, "took_ms=") {
		t.Fatalf("error result must not get took_ms footer, got:\n%s", got)
	}
}

// TestAddBudgetShaping_OverBudget verifies that the wrapper applies the
// default budget shaping to over-budget responses.
func TestAddBudgetShaping_OverBudget(t *testing.T) {
	long := strings.Repeat("line of content\n", 1000)
	res := textResult(long)
	applyBudgetAndTook(res, 5*time.Millisecond)

	got := textContentOf(t, res)
	if len(got) >= len(long) {
		t.Fatalf("over-budget response must be shaped: got %d, orig %d", len(got), len(long))
	}
	if !strings.Contains(got, "[truncated:") {
		t.Fatalf("shaped response must have truncation footer")
	}
	if !strings.Contains(got, "took_ms=") {
		t.Fatalf("shaped response must still have took_ms footer")
	}
}

// TestAddBudgetShaping_SkipsAlreadyShaped verifies that a tool that already
// shaped its output (with a custom budget) is not double-shaped.
func TestAddBudgetShaping_SkipsAlreadyShaped(t *testing.T) {
	// Simulate a tool that shaped its own output.
	alreadyShaped := "head content\n[truncated: 500 more chars — pass offset=10]"
	res := textResult(alreadyShaped)
	applyBudgetAndTook(res, 1*time.Millisecond)

	got := textContentOf(t, res)
	// Should not have a second truncation footer.
	count := strings.Count(got, "[truncated:")
	if count != 1 {
		t.Fatalf("already-shaped output must not be double-shaped: found %d truncation footers", count)
	}
}

// TestSoftDeadlineResult_Format verifies the partial-result helper produces
// the correct footer structure.
func TestSoftDeadlineResult_Format(t *testing.T) {
	res := softDeadlineResult("partial data here", "LLM analysis skipped", 30*time.Second)
	got := textContentOf(t, res)

	if !strings.Contains(got, "partial data here") {
		t.Fatalf("partial result must contain the body, got:\n%s", got)
	}
	if !strings.Contains(got, "partial: true") {
		t.Fatalf("partial result must contain 'partial: true' footer, got:\n%s", got)
	}
	if !strings.Contains(got, "LLM analysis skipped") {
		t.Fatalf("partial result must contain what was skipped, got:\n%s", got)
	}
	if !strings.Contains(got, "took_ms=") {
		t.Fatalf("partial result must contain took_ms, got:\n%s", got)
	}
}

// TestBudgetOverride verifies the budget override resolution.
func TestBudgetOverride(t *testing.T) {
	if budgetOverride(0) != 8192 {
		t.Fatal("zero override must yield default")
	}
	if budgetOverride(4096) != 4096 {
		t.Fatal("valid override must be used")
	}
	if budgetOverride(100) < 512 {
		t.Fatal("tiny override must be clamped to MinBudget")
	}
}

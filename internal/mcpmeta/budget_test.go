package mcpmeta

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestShape_UnderBudget_ReturnsUnchanged verifies that text within the budget
// is returned byte-identical.
func TestShape_UnderBudget_ReturnsUnchanged(t *testing.T) {
	text := "short response"
	got := Shape(text, 1000, "")
	if got != text {
		t.Fatalf("under-budget text must be unchanged, got %q", got)
	}
}

// TestShape_OverBudget_TruncatesAndAppendsFooter verifies that over-budget
// text is truncated and a continuation footer is appended. RED-on-revert:
// if Shape is replaced with a no-op (returns text unchanged), the assertion
// that the result is shorter than the original fails.
func TestShape_OverBudget_TruncatesAndAppendsFooter(t *testing.T) {
	// Build a text with many lines so truncation lands on a newline.
	var sb strings.Builder
	for i := 0; i < 200; i++ {
		sb.WriteString("line ")
		sb.WriteString(string(rune('A' + i%26)))
		sb.WriteString("\n")
	}
	text := sb.String()
	budget := 500

	got := Shape(text, budget, "pass offset=20")

	if len(got) >= len(text) {
		t.Fatalf("shaped result must be shorter than original: got %d, orig %d", len(got), len(text))
	}
	if !strings.Contains(got, truncationFooterPrefix) {
		t.Fatalf("shaped result must contain truncation footer, got:\n%s", got)
	}
	if !strings.Contains(got, "pass offset=20") {
		t.Fatalf("shaped result must contain continuation hint, got:\n%s", got)
	}
	if !strings.Contains(got, "more chars") {
		t.Fatalf("shaped result must mention remaining chars, got:\n%s", got)
	}
}

// TestShape_OverBudget_DefaultHint verifies that an empty hint falls back
// to the generic continuation message.
func TestShape_OverBudget_DefaultHint(t *testing.T) {
	text := strings.Repeat("x", 2000)
	got := Shape(text, 500, "")
	if !strings.Contains(got, "narrow your query") {
		t.Fatalf("empty hint must fall back to generic message, got:\n%s", got)
	}
}

// TestShape_ZeroBudget_ReturnsUnchanged verifies that budget <= 0 disables
// shaping (returns text unchanged).
func TestShape_ZeroBudget_ReturnsUnchanged(t *testing.T) {
	text := strings.Repeat("x", 10000)
	got := Shape(text, 0, "")
	if got != text {
		t.Fatalf("zero budget must return text unchanged")
	}
}

// TestShape_MinBudgetClamp verifies that a budget below MinBudget is clamped.
func TestShape_MinBudgetClamp(t *testing.T) {
	text := strings.Repeat("x", 2000)
	got := Shape(text, 10, "")
	// Should still truncate (clamped to MinBudget=512, text is 2000).
	if len(got) >= len(text) {
		t.Fatalf("clamped budget must still truncate long text")
	}
	if !strings.Contains(got, truncationFooterPrefix) {
		t.Fatalf("clamped budget must still append footer")
	}
}

// TestIsShaped detects the truncation footer.
func TestIsShaped(t *testing.T) {
	if IsShaped("plain text") {
		t.Fatal("plain text must not be shaped")
	}
	shaped := Shape(strings.Repeat("x", 2000), 500, "")
	if !IsShaped(shaped) {
		t.Fatal("shaped text must be detected")
	}
}

// TestTookFooter_Format verifies the footer format.
func TestTookFooter_Format(t *testing.T) {
	got := TookFooter(42 * time.Millisecond)
	if got != "\ntook_ms=42" {
		t.Fatalf("unexpected footer: %q", got)
	}
}

// TestTookFooter_ClampsToMin1ms verifies sub-millisecond durations are
// clamped to 1.
func TestTookFooter_ClampsToMin1ms(t *testing.T) {
	got := TookFooter(0)
	if got != "\ntook_ms=1" {
		t.Fatalf("zero duration must clamp to 1ms, got %q", got)
	}
}

// TestAppendTook_Idempotent verifies that appending took twice does not
// double-tag.
func TestAppendTook_Idempotent(t *testing.T) {
	text := "body"
	tagged := AppendTook(text, 10*time.Millisecond)
	doubleTagged := AppendTook(tagged, 20*time.Millisecond)
	if doubleTagged != tagged {
		t.Fatalf("double-append must be idempotent:\nfirst: %q\nsecond: %q", tagged, doubleTagged)
	}
}

// TestHasTookFooter detects the took_ms footer.
func TestHasTookFooter(t *testing.T) {
	if HasTookFooter("plain text") {
		t.Fatal("plain text must not have took footer")
	}
	tagged := AppendTook("body", 5*time.Millisecond)
	if !HasTookFooter(tagged) {
		t.Fatal("tagged text must be detected")
	}
}

// TestResolveBudget verifies the override/default resolution logic.
func TestResolveBudget(t *testing.T) {
	tests := []struct {
		name     string
		override int
		def      int
		want     int
	}{
		{"no override uses default", 0, 8192, 8192},
		{"negative override uses default", -1, 8192, 8192},
		{"valid override used", 4096, 8192, 4096},
		{"override below min clamped", 100, 8192, MinBudget},
		{"override above max clamped", 20000, 8192, MaxBudget},
		{"override at client ceiling clamped", 10149, 8192, MaxBudget},
		{"override at max passes", MaxBudget, 8192, MaxBudget},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ResolveBudget(tc.override, tc.def)
			if got != tc.want {
				t.Fatalf("ResolveBudget(%d, %d) = %d, want %d", tc.override, tc.def, got, tc.want)
			}
		})
	}
}

// TestSoftDeadline_Default verifies the default deadline is applied.
func TestSoftDeadline_Default(t *testing.T) {
	ctx, cancel := SoftDeadline(context.Background())
	defer cancel()
	dl, ok := ctx.Deadline()
	if !ok {
		t.Fatal("soft deadline must set a deadline")
	}
	remaining := time.Until(dl)
	if remaining > DefaultSoftDeadline || remaining < DefaultSoftDeadline-2*time.Second {
		t.Fatalf("soft deadline should be ~%v, remaining=%v", DefaultSoftDeadline, remaining)
	}
}

// TestSoftDeadlineWith_PreservesShorterParent verifies that a parent context
// with an earlier deadline is respected.
func TestSoftDeadlineWith_PreservesShorterParent(t *testing.T) {
	parent, parentCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer parentCancel()
	ctx, cancel := SoftDeadlineWith(parent, 30*time.Second)
	defer cancel()
	dl, ok := ctx.Deadline()
	if !ok {
		t.Fatal("deadline must be set")
	}
	parentDL, _ := parent.Deadline()
	if !dl.Equal(parentDL) {
		t.Fatalf("soft deadline must not extend past parent: got %v, parent %v", dl, parentDL)
	}
}

// TestSoftDeadlineWith_ZeroReturnsParent verifies that d <= 0 returns the
// parent context unchanged.
func TestSoftDeadlineWith_ZeroReturnsParent(t *testing.T) {
	ctx, cancel := SoftDeadlineWith(context.Background(), 0)
	defer cancel()
	if _, ok := ctx.Deadline(); ok {
		t.Fatal("zero duration must not set a deadline")
	}
}

// TestPartialFooter verifies the partial-result footer format.
func TestPartialFooter(t *testing.T) {
	got := PartialFooter("LLM analysis skipped")
	if !strings.Contains(got, "partial: true") {
		t.Fatalf("footer must contain 'partial: true', got %q", got)
	}
	if !strings.Contains(got, "LLM analysis skipped") {
		t.Fatalf("footer must contain the description, got %q", got)
	}
}

// TestPartialFooter_EmptyUsesDefault verifies empty description falls back.
func TestPartialFooter_EmptyUsesDefault(t *testing.T) {
	got := PartialFooter("")
	if !strings.Contains(got, "some stages skipped") {
		t.Fatalf("empty description must use default, got %q", got)
	}
}

// TestShapeWithHint_FitsBudget_MarksAsShaped proves #582: when text fits
// within the override budget, ShapeWithHint appends a budget-applied marker
// so IsShaped returns true and the addTool wrapper skips re-shaping with the
// default budget. Without this, the wrapper would re-shape with the smaller
// default budget and replace the tool-specific hint with a generic one.
func TestShapeWithHint_FitsBudget_MarksAsShaped(t *testing.T) {
	text := "short response that fits within the override budget"
	overrideBudget := 1000 // larger than text, larger than DefaultBudget

	got := ShapeWithHint(text, overrideBudget, "pass offset=20 for the next page")

	if !IsShaped(got) {
		t.Fatal("ShapeWithHint must mark text as shaped even when it fits within budget")
	}

	// The marker must be strippable so it's not visible to the agent.
	stripped := StripBudgetMarker(got)
	if stripped != text {
		t.Errorf("after stripping marker, text must equal original, got %q", stripped)
	}
}

// TestShapeWithHint_ExceedsBudget_TruncatesWithHint verifies that when text
// exceeds the override budget, ShapeWithHint truncates and appends the
// tool-specific hint (same behavior as Shape).
func TestShapeWithHint_ExceedsBudget_TruncatesWithHint(t *testing.T) {
	var sb strings.Builder
	for i := 0; i < 200; i++ {
		sb.WriteString("line ")
		sb.WriteString(string(rune('A' + i%26)))
		sb.WriteString("\n")
	}
	text := sb.String()
	overrideBudget := 500

	got := ShapeWithHint(text, overrideBudget, "pass offset=20 for the next page")

	if !IsShaped(got) {
		t.Fatal("ShapeWithHint must mark text as shaped when it truncates")
	}
	if !strings.Contains(got, "pass offset=20") {
		t.Errorf("truncated text must contain the tool-specific hint, got %q", got[:min(200, len(got))])
	}
	if !strings.Contains(got, truncationFooterPrefix) {
		t.Error("truncated text must contain the truncation footer")
	}
	// Should NOT contain the budget-applied marker (truncation path, not fit path).
	if strings.Contains(got, budgetAppliedMarker) {
		t.Error("truncated text should not contain the budget-applied marker")
	}
}

// TestShapeWithHint_ZeroBudget_NoMarker verifies that budget <= 0 returns text
// unchanged with no marker (same as Shape).
func TestShapeWithHint_ZeroBudget_NoMarker(t *testing.T) {
	text := "some response"
	got := ShapeWithHint(text, 0, "hint")
	if got != text {
		t.Errorf("zero budget should return text unchanged, got %q", got)
	}
	if IsShaped(got) {
		t.Error("zero budget should not mark as shaped")
	}
}

// TestIsShaped_DetectsBudgetAppliedMarker verifies that IsShaped detects the
// budget-applied marker (not just the truncation footer).
func TestIsShaped_DetectsBudgetAppliedMarker(t *testing.T) {
	text := "response" + budgetAppliedMarker
	if !IsShaped(text) {
		t.Error("IsShaped must detect the budget-applied marker")
	}
}

// TestStripBudgetMarker_RemovesMarker verifies that StripBudgetMarker removes
// the marker and leaves the rest of the text intact.
func TestStripBudgetMarker_RemovesMarker(t *testing.T) {
	text := "response body\n[budget-applied]\nmore text"
	got := StripBudgetMarker(text)
	expected := "response body\nmore text"
	if got != expected {
		t.Errorf("StripBudgetMarker: got %q, want %q", got, expected)
	}
}

// TestShapeWithHint_WrapperSimulation proves the end-to-end #582 scenario:
// a tool calls ShapeWithHint with an override budget > DefaultBudget, text
// fits within the override but exceeds DefaultBudget. The wrapper's
// applyBudgetAndTook logic must NOT re-shape (IsShaped=true), and after
// stripping the marker, the text is intact with no generic hint.
func TestShapeWithHint_WrapperSimulation(t *testing.T) {
	// Text that fits within override (9000) but exceeds default (8192).
	var sb strings.Builder
	sb.WriteString(strings.Repeat("x", 8500))
	text := sb.String()

	overrideBudget := 9000
	hint := "pass offset=20 for the next page"

	// Tool calls ShapeWithHint.
	shaped := ShapeWithHint(text, overrideBudget, hint)

	// Wrapper checks IsShaped — must be true so it skips re-shaping.
	if !IsShaped(shaped) {
		t.Fatal("wrapper must see IsShaped=true and skip re-shaping with default budget")
	}

	// Wrapper strips the marker.
	final := StripBudgetMarker(shaped)

	// The final text must equal the original (no truncation, no generic hint).
	if final != text {
		t.Errorf("final text must equal original (no re-shaping), got len %d vs %d", len(final), len(text))
	}
	if strings.Contains(final, "narrow your query") {
		t.Error("final text must NOT contain the generic hint from default-budget re-shaping")
	}
}

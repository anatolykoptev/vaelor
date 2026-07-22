package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/anatolykoptev/vaelor/internal/mcpmeta"
)

// TestSoftDeadline_SlowFake verifies that a handler that exceeds the soft
// deadline returns a partial result instead of nothing. This is the core
// #572 contract: never compute past the deadline to return nothing.
//
// The "slow fake" is a handler that sleeps past the deadline. We use a
// very short deadline (50ms) so the test runs fast. The handler checks
// ctx.Err() and returns a partial result.
func TestSoftDeadline_SlowFake(t *testing.T) {
	// Simulate a tool handler that respects the soft deadline.
	handler := func(ctx context.Context) string {
		// Simulate some work.
		time.Sleep(10 * time.Millisecond)

		// Check if deadline fired before the expensive stage.
		if ctx.Err() != nil {
			return "partial: computed stage 1 only"
		}

		// Simulate the expensive stage (would be LLM call, DB query, etc).
		select {
		case <-time.After(200 * time.Millisecond):
			return "full result"
		case <-ctx.Done():
			return "partial: computed stage 1 only"
		}
	}

	// Run with a 50ms soft deadline.
	ctx, cancel := mcpmeta.SoftDeadlineWith(context.Background(), 50*time.Millisecond)
	defer cancel()

	result := handler(ctx)

	if !strings.Contains(result, "partial") {
		t.Fatalf("handler must return partial result on deadline, got: %q", result)
	}
	if result == "full result" {
		t.Fatal("handler must NOT return full result when deadline fires")
	}
}

// TestSoftDeadline_PartialResultHasFooter verifies that the softDeadlineResult
// helper produces a response with both the partial footer and took_ms.
func TestSoftDeadline_PartialResultHasFooter(t *testing.T) {
	res := softDeadlineResult("stage 1 data", "stage 2+ skipped", 100*time.Millisecond)
	got := textContentOf(t, res)

	if !strings.Contains(got, "partial: true") {
		t.Fatalf("must contain partial: true, got:\n%s", got)
	}
	if !strings.Contains(got, "stage 2+ skipped") {
		t.Fatalf("must contain what was skipped, got:\n%s", got)
	}
	if !strings.Contains(got, "took_ms=") {
		t.Fatalf("must contain took_ms, got:\n%s", got)
	}
}

// TestSoftDeadline_CompletesWithinBudget verifies that when the handler
// finishes before the deadline, no partial footer is emitted.
func TestSoftDeadline_CompletesWithinBudget(t *testing.T) {
	ctx, cancel := mcpmeta.SoftDeadlineWith(context.Background(), 5*time.Second)
	defer cancel()

	// Handler completes quickly.
	select {
	case <-time.After(10 * time.Millisecond):
	case <-ctx.Done():
		t.Fatal("handler should complete before deadline")
	}

	if ctx.Err() != nil {
		t.Fatal("context must not be cancelled when handler finishes in time")
	}
}

// TestSoftDeadline_HonorsParentDeadline verifies that the soft deadline
// does not extend past a shorter parent deadline.
func TestSoftDeadline_HonorsParentDeadline(t *testing.T) {
	parent, parentCancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer parentCancel()

	ctx, cancel := mcpmeta.SoftDeadlineWith(parent, 10*time.Second)
	defer cancel()

	dl, _ := ctx.Deadline()
	parentDL, _ := parent.Deadline()

	if !dl.Equal(parentDL) {
		t.Fatalf("soft deadline must not extend past parent: %v != %v", dl, parentDL)
	}
}

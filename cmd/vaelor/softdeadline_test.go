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
// The test itself plays the deadline: the handler signals when stage 1 is
// done and parks in the expensive-stage select; the test then cancels the
// context — the exact signal a firing deadline sends. No real-time margin is
// involved, so machine load cannot let the "full result" arm win (#751: the
// old 50ms-deadline vs 200ms-work race flipped under CI load).
func TestSoftDeadline_SlowFake(t *testing.T) {
	stage1Done := make(chan struct{})
	handler := func(ctx context.Context) string {
		// Stage 1 completes; tell the test so it can "fire" the deadline
		// while this handler is parked in the expensive stage below.
		close(stage1Done)

		// Check if the deadline fired before the expensive stage.
		if ctx.Err() != nil {
			return "partial: computed stage 1 only"
		}

		// Simulate the expensive stage (would be LLM call, DB query, etc).
		select {
		case <-time.After(10 * time.Second):
			return "full result"
		case <-ctx.Done():
			return "partial: computed stage 1 only"
		}
	}

	ctx, cancel := mcpmeta.SoftDeadlineWith(context.Background(), time.Hour)
	defer cancel()

	done := make(chan string, 1)
	go func() { done <- handler(ctx) }()

	<-stage1Done // stage 1 complete; handler is at/past the ctx.Err() check
	cancel()     // the deadline "fires" — deterministically, not via a real timer

	result := <-done

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

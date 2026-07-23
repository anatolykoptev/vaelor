package main

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

// TestRunGaugeTicker_ExitsOnCtxCancel is the RED-on-revert guard for #596 and
// #597: the metric-publishing goroutines (orphan gauge, code-graph age gauge)
// run a time.Ticker in a bare `for range t.C` loop with no cancellation path.
// runGaugeTicker is the shared loop primitive both publishers use; it MUST
// exit when ctx is cancelled and close its done channel so no goroutine leaks
// on shutdown/re-init.
//
// Falsify: revert the `case <-ctx.Done(): return` branch → the goroutine
// lingers → done never closes → the select hits the timeout → RED.
func TestRunGaugeTicker_ExitsOnCtxCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var ticks atomic.Int64
	done := runGaugeTicker(ctx, 10*time.Millisecond, func() { ticks.Add(1) })

	// Let at least one tick fire so we know the goroutine is live.
	time.Sleep(35 * time.Millisecond)
	if ticks.Load() == 0 {
		t.Fatal("ticker never fired — goroutine not running")
	}

	cancel()
	select {
	case <-done:
		// goroutine exited cleanly — pass
	case <-time.After(2 * time.Second):
		t.Fatal("done channel did not close after ctx cancel — goroutine leaked (#596/#597)")
	}
}

// TestRunGaugeTicker_ImmediateFirstCall verifies the boot-warm semantics: fn
// runs once immediately (before the first tick), matching the prior inline
// `publishX(); for range t.C { publishX() }` shape.
func TestRunGaugeTicker_ImmediateFirstCall(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var calls atomic.Int64
	done := runGaugeTicker(ctx, 1*time.Hour, func() { calls.Add(1) })
	// With a 1h interval the only way calls > 0 is the immediate boot call.
	time.Sleep(20 * time.Millisecond)
	if got := calls.Load(); got != 1 {
		t.Fatalf("expected exactly 1 immediate call, got %d", got)
	}
	cancel()
	<-done
}

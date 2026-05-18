package codegraph

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/anatolykoptev/go-kit/llm"
)

// countingCompleter wraps a Completer and counts Complete calls.
type countingCompleter struct {
	inner llm.Completer
	calls atomic.Int32
}

func (c *countingCompleter) Complete(ctx context.Context, system, user string, opts ...llm.ChatOption) (string, error) {
	c.calls.Add(1)
	return c.inner.Complete(ctx, system, user, opts...)
}

// TestClassifyAndBuildCypherNoLLM_SingleRoundTrip asserts that when the LLM is
// unavailable (NoOp Completer), classifyAndBuildCypher makes exactly ONE
// Complete call (Classify) and returns ErrLLMUnavailable without falling
// through to the freeform GenerateCypher path (which would be a second call).
//
// Before the fix: Classify error causes freeform fallback that calls
// GenerateCypher — a second NoOp round-trip (double call).
// After the fix: ErrLLMUnavailable short-circuits at the Classify error branch.
func TestClassifyAndBuildCypherNoLLM_SingleRoundTrip(t *testing.T) {
	t.Parallel()

	cc := &countingCompleter{inner: llm.NoOp{}}
	_, _, _, err := classifyAndBuildCypher(context.Background(), cc, "who calls Parse?")

	if !errors.Is(err, llm.ErrUnavailable) {
		t.Errorf("want ErrLLMUnavailable; got %v", err)
	}
	if cc.calls.Load() != 1 {
		t.Errorf("Complete called %d times, want exactly 1 (classify only, no freeform fallback)", cc.calls.Load())
	}
}

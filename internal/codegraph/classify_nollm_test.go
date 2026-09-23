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
	_, _, _, err := classifyAndBuildCypher(context.Background(), cc, "who calls Parse?", nil)

	if !errors.Is(err, llm.ErrUnavailable) || !errors.Is(err, ErrLLMStep) {
		t.Errorf("want ErrLLMUnavailable wrapped as ErrLLMStep; got %v", err)
	}
	if cc.calls.Load() != 1 {
		t.Errorf("Complete called %d times, want exactly 1 (classify only, no freeform fallback)", cc.calls.Load())
	}
}

// scriptedCompleter returns its replies in order, then errors.
type scriptedCompleter struct {
	replies []string
	errs    []error
	n       int
}

func (s *scriptedCompleter) Complete(context.Context, string, string, ...llm.ChatOption) (string, error) {
	i := s.n
	s.n++
	if i >= len(s.replies) {
		return "", errors.New("scripted completer exhausted")
	}
	return s.replies[i], s.errs[i]
}

// TestClassifyAndBuildCypher_GenerateFailureIsLLMStep: classify answers
// garbage (freeform fallback), then Cypher generation fails — the error must
// carry ErrLLMStep so code_graph points the caller at template + params.
func TestClassifyAndBuildCypher_GenerateFailureIsLLMStep(t *testing.T) {
	t.Parallel()

	sc := &scriptedCompleter{
		replies: []string{"not json", ""},
		errs:    []error{nil, errors.New("model returned 502")},
	}
	_, _, _, err := classifyAndBuildCypher(context.Background(), sc, "count functions per file", nil)
	if !errors.Is(err, ErrLLMStep) {
		t.Fatalf("err = %v, want ErrLLMStep", err)
	}
}

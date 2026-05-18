// Package llmiface defines the minimal LLM surface consumed by go-code tools.
// *llm.Client (from go-kit) satisfies Completer; NoOp is used when LLM is unconfigured.
package llmiface

import (
	"context"
	"errors"

	"github.com/anatolykoptev/go-kit/llm"
)

// Completer is the minimal LLM surface used across go-code.
// Method signature mirrors *llm.Client.Complete exactly so the kit client
// satisfies it without an adapter.
type Completer interface {
	Complete(ctx context.Context, system, user string, opts ...llm.ChatOption) (string, error)
}

// NoOp is a Completer that always returns ErrLLMUnavailable.
// Used in Deps.LLM when LLM_API_KEY is unset, so tool handlers never
// have to nil-check the LLM field.
type NoOp struct{}

func (NoOp) Complete(context.Context, string, string, ...llm.ChatOption) (string, error) {
	return "", ErrLLMUnavailable
}

// ErrLLMUnavailable is returned by NoOp.Complete. Tools that have a
// deterministic fallback should treat this like any other LLM error;
// tools that require LLM should check Deps.LLMHasKey before calling.
var ErrLLMUnavailable = errors.New("llm: not configured (set LLM_API_KEY)")

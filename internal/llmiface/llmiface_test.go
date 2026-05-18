package llmiface_test

import (
	"context"
	"errors"
	"testing"

	"github.com/anatolykoptev/go-code/internal/llmiface"
	"github.com/anatolykoptev/go-kit/llm"
)

func TestNoOpReturnsErrUnavailable(t *testing.T) {
	var c llmiface.Completer = llmiface.NoOp{}
	out, err := c.Complete(context.Background(), "sys", "user")
	if out != "" {
		t.Errorf("want empty output, got %q", out)
	}
	if !errors.Is(err, llmiface.ErrLLMUnavailable) {
		t.Errorf("want ErrLLMUnavailable, got %v", err)
	}
}

func TestKitClientSatisfiesCompleter(t *testing.T) {
	// Compile-time assertion: *llm.Client must implement Completer.
	var _ llmiface.Completer = (*llm.Client)(nil)
}

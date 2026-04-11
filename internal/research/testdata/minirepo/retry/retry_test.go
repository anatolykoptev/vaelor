package retry

import "testing"

func TestWithBackoff(t *testing.T) {
	calls := 0
	_ = WithBackoff(func() error { calls++; return nil }, 3)
	if calls != 1 {
		t.Errorf("calls = %d", calls)
	}
}

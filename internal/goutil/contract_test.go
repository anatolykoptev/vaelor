package goutil

import (
	"strings"
	"testing"
)

func TestAssert_passes(t *testing.T) {
	Assert(true, "should not panic")
}

func TestAssert_panics(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic")
		}
		msg, ok := r.(string)
		if !ok || !strings.Contains(msg, "must be positive") {
			t.Fatalf("unexpected panic value: %v", r)
		}
	}()
	Assert(false, "must be positive")
}

func TestAssertf_panics(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic")
		}
		msg, ok := r.(string)
		if !ok || !strings.Contains(msg, "got 0") {
			t.Fatalf("unexpected panic value: %v", r)
		}
	}()
	Assertf(false, "count must be > 0, got %d", 0)
}

func TestFail_panics(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic")
		}
		msg, ok := r.(string)
		if !ok || !strings.Contains(msg, "unreachable") {
			t.Fatalf("unexpected panic value: %v", r)
		}
	}()
	Fail("unreachable branch hit")
}

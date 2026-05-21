// Package ssh internal tests for cappedWriter.
// This file is in package ssh (not ssh_test) to access the unexported type.
package ssh

import (
	"bytes"
	"io"
	"testing"
)

// TestCappedWriter_AllowsWritesBelowCap verifies normal writes pass through.
func TestCappedWriter_AllowsWritesBelowCap(t *testing.T) {
	var buf bytes.Buffer
	w := &cappedWriter{inner: &buf, max: 10}
	n, err := w.Write([]byte("hello"))
	if err != nil {
		t.Fatalf("Write: unexpected error: %v", err)
	}
	if n != 5 {
		t.Errorf("n: want 5, got %d", n)
	}
	if buf.String() != "hello" {
		t.Errorf("inner: want %q, got %q", "hello", buf.String())
	}
}

// TestCappedWriter_ExactBoundary verifies a write filling exactly the cap
// succeeds without error.
func TestCappedWriter_ExactBoundary(t *testing.T) {
	var buf bytes.Buffer
	w := &cappedWriter{inner: &buf, max: 5}
	n, err := w.Write([]byte("hello"))
	if err != nil {
		t.Fatalf("Write to cap boundary: unexpected error: %v", err)
	}
	if n != 5 {
		t.Errorf("n: want 5, got %d", n)
	}
}

// TestCappedWriter_TruncatesAndReturnsShortWrite verifies that a write
// exceeding the cap is truncated and returns io.ErrShortWrite.
func TestCappedWriter_TruncatesAndReturnsShortWrite(t *testing.T) {
	var buf bytes.Buffer
	var cancelCalled bool
	w := &cappedWriter{
		inner:  &buf,
		max:    5,
		cancel: func() { cancelCalled = true },
	}

	// Write 10 bytes to a 5-byte cap.
	n, err := w.Write([]byte("hello world!"))
	if !cancelCalled {
		t.Error("cancel must be called on overflow")
	}
	if err != io.ErrShortWrite {
		t.Errorf("err: want io.ErrShortWrite, got %v", err)
	}
	// Only up to max bytes are written.
	if buf.Len() > 5 {
		t.Errorf("inner buf: want <= 5 bytes, got %d", buf.Len())
	}
	_ = n
}

// TestCappedWriter_WriteAfterExhausted verifies that once the cap is hit,
// subsequent writes return io.ErrShortWrite immediately without writing.
func TestCappedWriter_WriteAfterExhausted(t *testing.T) {
	var buf bytes.Buffer
	cancelCount := 0
	w := &cappedWriter{
		inner:  &buf,
		max:    3,
		cancel: func() { cancelCount++ },
	}

	// First write overflows.
	_, _ = w.Write([]byte("hello"))
	initialLen := buf.Len()
	initialCancel := cancelCount

	// Second write: already exhausted, cancel not called again.
	_, err := w.Write([]byte("more"))
	if err != io.ErrShortWrite {
		t.Errorf("second write: want io.ErrShortWrite, got %v", err)
	}
	if buf.Len() != initialLen {
		t.Errorf("second write must not add bytes; before=%d after=%d", initialLen, buf.Len())
	}
	// cancel already nilled out — should not be called again.
	if cancelCount != initialCancel {
		t.Errorf("cancel called %d times after exhaustion, want 0 new calls", cancelCount-initialCancel)
	}
}

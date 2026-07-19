package watcher

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// helper: write a file, creating parent dirs as needed.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// TestDebounceCoalescing verifies that rapid writes to the same file within
// the debounce window produce only a single event on the channel.
func TestDebounceCoalescing(t *testing.T) {
	dir := t.TempDir()
	w, err := New([]string{dir}, WithDebounce(200*time.Millisecond))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer w.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events, err := w.Watch(ctx)
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}

	// Give the watcher a moment to install its inotify watches.
	time.Sleep(100 * time.Millisecond)

	target := filepath.Join(dir, "burst.txt")
	// Write rapidly 5 times within the debounce window.
	for i := 0; i < 5; i++ {
		writeFile(t, target, "data")
	}

	// Wait for the debounce window to expire plus margin.
	time.Sleep(400 * time.Millisecond)

	// Collect events that arrived.
	var got []Event
	for {
		select {
		case ev, ok := <-events:
			if !ok {
				goto done
			}
			got = append(got, ev)
		default:
			goto done
		}
	}
done:

	if len(got) != 1 {
		t.Fatalf("expected exactly 1 coalesced event, got %d: %+v", len(got), got)
	}
}

// TestIgnoreDirs verifies that a file created under an ignored directory
// produces no event.
func TestIgnoreDirs(t *testing.T) {
	dir := t.TempDir()
	w, err := New([]string{dir}, WithRecursive(true), WithIgnoreDirs("ignored"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer w.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events, err := w.Watch(ctx)
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	// Create a file inside the ignored directory.
	writeFile(t, filepath.Join(dir, "ignored", "secret.txt"), "nope")

	// Wait long enough for any event to have arrived.
	time.Sleep(300 * time.Millisecond)

	select {
	case ev := <-events:
		t.Fatalf("expected no event from ignored dir, got %+v", ev)
	default:
		// pass — no event arrived
	}
}

// TestContextCancellation verifies that cancelling the context closes the
// event channel and Watch returns.
func TestContextCancellation(t *testing.T) {
	dir := t.TempDir()
	w, err := New([]string{dir})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer w.Close()

	ctx, cancel := context.WithCancel(context.Background())

	events, err := w.Watch(ctx)
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}

	cancel()

	// The channel must close within a reasonable time.
	select {
	case _, ok := <-events:
		if ok {
			t.Fatal("expected channel to be closed after cancel, got an event")
		}
		// pass — channel closed
	case <-time.After(2 * time.Second):
		t.Fatal("channel did not close within 2s after context cancellation")
	}
}

// TestEnvSkip verifies that a .env file change produces no event when the
// watcher is configured with WithExtensions filtering for non-.env files.
// (ADR-13 security_cost: .env files must not trigger indexing.)
func TestEnvSkip(t *testing.T) {
	dir := t.TempDir()
	// Only watch .go files — a .env change must be filtered out.
	w, err := New([]string{dir}, WithExtensions(".go"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer w.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events, err := w.Watch(ctx)
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	// Create a .env file — should be filtered out by the extension filter.
	writeFile(t, filepath.Join(dir, ".env"), "SECRET=leaked")

	// Wait long enough for any event to have arrived.
	time.Sleep(300 * time.Millisecond)

	select {
	case ev := <-events:
		t.Fatalf("expected no event for .env file, got %+v", ev)
	default:
		// pass — no event arrived
	}
}

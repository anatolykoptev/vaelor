package main

// B5 (#644) regression tests: the file watcher must restrict events to files
// vaelor can actually index (the parser-registered source extensions), so that
// non-source file changes (README, go.sum, .git internals, .env) do not fill
// the cap-100 event channel and drop real source-file events under burst.
//
// The extension list is sourced from parser.RegisteredExtensions() (the single
// source of truth) — NOT a hardcoded duplicate. A duplicated ext map is the
// documented historical bug (`.cjs` was in the handler but missing from a
// separate lang map, making ParseFile("foo.cjs") fail silently).
//
// Falsification:
//   - TestSourceWatchExtensions_MatchesParserRegistry: replace
//     sourceWatchExtensions with a hardcoded list missing one registered ext →
//     the equality assertion REDS.
//   - TestBuildWatcherOptions_FiltersNonSourceExtensions: drop the
//     WithExtensions option from buildWatcherOptions → the non-source (.md)
//     file produces an event → the "no event" assertion REDS.

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/anatolykoptev/go-kit/watcher"
	"github.com/anatolykoptev/vaelor/internal/parser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSourceWatchExtensions_MatchesParserRegistry is the single-source-of-truth
// guard: the watcher's extension list MUST be exactly parser.RegisteredExtensions
// with no hardcoded duplicate. A duplicated list drifts as new languages are
// registered (the historical .cjs bug).
//
// Falsifiable: hardcode a list in sourceWatchExtensions missing one registered
// ext (or adding a non-registered one) → the equality assertion REDS.
func TestSourceWatchExtensions_MatchesParserRegistry(t *testing.T) {
	got := sourceWatchExtensions()
	want := parser.RegisteredExtensions()

	require.NotEmpty(t, want, "precondition: parser must register at least one extension")
	assert.Equal(t, want, got,
		"sourceWatchExtensions must equal parser.RegisteredExtensions exactly — "+
			"a hardcoded duplicate drifts as languages are registered (historical .cjs bug)")
}

// TestSourceWatchExtensions_SourceVsNonSource is a table-driven membership guard
// proving the registry (and thus the watcher) admits source extensions and
// rejects non-source ones. This is the load-bearing classification the watcher
// relies on to keep non-source events out of the channel.
//
// Falsifiable: if sourceWatchExtensions returned a hardcoded list that included
// ".md" or excluded ".go", the corresponding row REDS.
func TestSourceWatchExtensions_SourceVsNonSource(t *testing.T) {
	exts := sourceWatchExtensions()
	set := make(map[string]bool, len(exts))
	for _, e := range exts {
		set[e] = true
	}

	cases := []struct {
		name     string
		ext      string
		expected bool
	}{
		{"go is source", ".go", true},
		{"python is source", ".py", true},
		{"rust is source", ".rs", true},
		{"typescript is source", ".ts", true},
		{"markdown is not source", ".md", false},
		{"env is not source", ".env", false},
		{"go.sum is not source", ".sum", false},
		{"yaml is not source", ".yaml", false},
		{"plain text is not source", ".txt", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, set[tc.ext],
				"extension %q: source=%v, got present=%v", tc.ext, tc.expected, set[tc.ext])
		})
	}
}

// TestBuildWatcherOptions_FiltersNonSourceExtensions is the behavioral guard:
// a watcher built with buildWatcherOptions must emit an event for a source file
// (.go) and must NOT emit an event for a non-source file (.md). This proves the
// WithExtensions option is present and wired to the registered extensions.
//
// Falsifiable: drop the WithExtensions option from buildWatcherOptions → the
// non-source (.md) file produces an event → the "expected no event" assertion
// REDS (and the source-file event still arrives, proving the watcher itself
// works — only the filter was removed).
func TestBuildWatcherOptions_FiltersNonSourceExtensions(t *testing.T) {
	dir := t.TempDir()

	w, err := watcher.New([]string{dir}, buildWatcherOptions(100*time.Millisecond)...)
	require.NoError(t, err)
	t.Cleanup(func() { _ = w.Close() })

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	events, err := w.Watch(ctx)
	require.NoError(t, err)

	// Allow inotify watches to install (mirrors the go-kit watcher test pattern).
	time.Sleep(150 * time.Millisecond)

	// Source file: MUST produce an event.
	srcPath := filepath.Join(dir, "real.go")
	require.NoError(t, os.WriteFile(srcPath, []byte("package main\n"), 0o600))

	// Non-source file: MUST NOT produce an event (filtered by WithExtensions).
	nonSrcPath := filepath.Join(dir, "README.md")
	require.NoError(t, os.WriteFile(nonSrcPath, []byte("# readme\n"), 0o600))

	// Wait past the debounce window for events to settle.
	time.Sleep(400 * time.Millisecond)

	var got []watcher.Event
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

	// Classify collected events by base name.
	seen := make(map[string]bool, len(got))
	for _, ev := range got {
		seen[filepath.Base(ev.Path)] = true
	}

	assert.True(t, seen["real.go"],
		"a source (.go) file MUST produce an event — the watcher is configured to watch source extensions")
	assert.False(t, seen["README.md"],
		"a non-source (.md) file MUST NOT produce an event — WithExtensions filters it "+
			"(drop WithExtensions from buildWatcherOptions → this REDS, the bug #644 regression)")
}

// TestBuildWatcherOptions_KeepsIgnoreDirs is a non-regression guard that the
// extension filter did not displace the existing WithIgnoreDirs option. A file
// under an ignored directory must produce no event even though its extension is
// a registered source extension.
func TestBuildWatcherOptions_KeepsIgnoreDirs(t *testing.T) {
	dir := t.TempDir()

	w, err := watcher.New([]string{dir}, buildWatcherOptions(100*time.Millisecond)...)
	require.NoError(t, err)
	t.Cleanup(func() { _ = w.Close() })

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	events, err := w.Watch(ctx)
	require.NoError(t, err)

	time.Sleep(150 * time.Millisecond)

	// A .go file inside an ignored dir (node_modules is in ingest.IgnoredDirNames).
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "node_modules"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "node_modules", "vendored.go"),
		[]byte("package main\n"), 0o600))

	time.Sleep(400 * time.Millisecond)

	select {
	case ev := <-events:
		t.Fatalf("expected no event from ignored dir, got %+v", ev)
	default:
		// pass — no event arrived (both WithIgnoreDirs and WithExtensions honored).
	}
}

// TestBuildWatcherOptions_IncludesAllRegisteredExtensions is a non-regression
// guard that the watcher subscribes to every parser-registered extension, so a
// newly registered language is automatically watched without a code change
// here. It checks the extension membership set (via the behavioral filter is
// impractical for 16 langs); the equality is already covered by
// TestSourceWatchExtensions_MatchesParserRegistry, this test asserts the option
// list is non-empty and the registry is the source.
func TestBuildWatcherOptions_IncludesAllRegisteredExtensions(t *testing.T) {
	opts := buildWatcherOptions(100 * time.Millisecond)
	assert.NotEmpty(t, opts, "buildWatcherOptions must return at least one option")

	registered := parser.RegisteredExtensions()
	sort.Strings(registered)
	assert.NotEmpty(t, registered)
	// Sanity: the helper that feeds WithExtensions is the registry, not a
	// hardcoded list. The behavioral test above proves the filter is wired.
	assert.Equal(t, registered, sourceWatchExtensions())
}

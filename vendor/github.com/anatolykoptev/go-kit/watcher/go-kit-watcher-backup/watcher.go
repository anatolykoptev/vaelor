// Package watcher provides a thin generic adapter over
// github.com/larsartmann/go-filewatcher/v2, exposing a decoupled Event type
// (Path + Op string) so callers never depend on fsnotify types directly.
package watcher

import (
	"context"
	"time"

	filewatcher "github.com/larsartmann/go-filewatcher/v2"
)

// Event is the generic file-change event surfaced to callers. Op is a
// human-readable operation string ("CREATE", "WRITE", "REMOVE", "RENAME")
// decoupled from the underlying fsnotify.Op bitfield.
type Event struct {
	Path string
	Op   string
}

// Watcher wraps a go-filewatcher Watcher, mapping its events to the generic
// Event type and forwarding context cancellation.
type Watcher struct {
	fw *filewatcher.Watcher
}

// Option configures the Watcher at construction time.
type Option = filewatcher.Option

// New creates a Watcher for the given directories, applying any functional
// options. At least one directory must be provided.
func New(dirs []string, opts ...Option) (*Watcher, error) {
	fw, err := filewatcher.New(dirs, opts...)
	if err != nil {
		return nil, err
	}
	return &Watcher{fw: fw}, nil
}

// Watch starts watching and returns a channel of generic Events. The channel
// is closed when ctx is cancelled.
func (w *Watcher) Watch(ctx context.Context) (<-chan Event, error) {
	src, err := w.fw.Watch(ctx)
	if err != nil {
		return nil, err
	}
	out := make(chan Event, cap(src))
	go func() {
		defer close(out)
		for ev := range src {
			out <- Event{Path: ev.Path, Op: ev.Op.String()}
		}
	}()
	return out, nil
}

// Close releases the underlying watcher resources.
func (w *Watcher) Close() error { return w.fw.Close() }

// WithDebounce sets the global debounce window; rapid bursts of changes to
// the same path are coalesced into a single event.
func WithDebounce(d time.Duration) Option { return filewatcher.WithDebounce(d) }

// WithIgnoreDirs excludes the given directory names from producing events.
func WithIgnoreDirs(dirs ...string) Option { return filewatcher.WithIgnoreDirs(dirs...) }

// WithExtensions restricts events to files matching the given extensions
// (e.g. ".go", ".yaml").
func WithExtensions(exts ...string) Option { return filewatcher.WithExtensions(exts...) }

// WithRecursive enables or disables recursive directory watching.
func WithRecursive(b bool) Option { return filewatcher.WithRecursive(b) }

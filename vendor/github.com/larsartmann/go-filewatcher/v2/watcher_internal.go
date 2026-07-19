//nolint:varnamelen,exhaustruct // Idiomatic short names: op (operation); partial ErrorContext initialization acceptable
package filewatcher

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"time"

	"github.com/fsnotify/fsnotify"
)

const operationFsnotify = "fsnotify"

// watchLoop is the main event processing goroutine.
// Exits when ctx is cancelled or fsnotify watcher is closed.
// Note: eventCh is closed by Close() after debouncer is stopped.
// debugLog logs a debug message if debug mode is enabled.
func (w *Watcher) debugLog(msg string, args ...any) {
	if w.debug {
		w.debugLogger.Debug(msg, args...)
	}
}

// debugLogEvent logs a debug message for the given event, including the path
// and operation as structured attributes. Centralizes the "log with path+op" pattern.
func (w *Watcher) debugLogEvent(msg string, event Event) {
	w.debugLog(msg, slog.String("path", event.Path), slog.String("op", event.Op.String()))
}

func (w *Watcher) watchLoop(ctx context.Context, eventCh chan<- Event) {
	defer w.wg.Done()
	defer func() {
		if w.debounceInterface != nil {
			w.debounceInterface.Stop()
		}

		w.closeEventChOnce.Do(func() { close(eventCh) })
	}()

	w.debugLog("watch loop started")

	for {
		select {
		case <-ctx.Done():
			w.debugLog("watch loop exiting: context cancelled")

			return

		case fsEvent, ok := <-w.fswatcher.Events:
			if !ok {
				w.debugLog("watch loop exiting: fsnotify events channel closed")

				return
			}

			w.processEvent(ctx, fsEvent, eventCh)

		case err, ok := <-w.fswatcher.Errors:
			if !ok {
				w.debugLog("watch loop exiting: fsnotify errors channel closed")

				return
			}

			w.handleError(ErrorContext{Operation: operationFsnotify, Retryable: true}, err)
		}
	}
}

// processEvent converts an fsnotify event, applies filters and debounce,
// and emits it to the channel.
func (w *Watcher) processEvent(ctx context.Context, fsEvent fsnotify.Event, eventCh chan<- Event) {
	event := convertEvent(fsEvent, w.lazyIsDir, w.contentHashing)
	if event == nil {
		w.debugLog(
			"event ignored: unrecognized fsnotify operation",
			slog.String("name", fsEvent.Name),
			slog.String("op", fsEvent.Op.String()),
		)

		return
	}

	w.debugLogEvent("event received", *event)

	if !w.passesFilters(*event) {
		w.eventsFilteredOut.Add(1)
		w.debugLog("event filtered out", slog.String("path", event.Path))
		w.handleFilteredEvent(fsEvent, *event)

		return
	}

	w.handleNewDirectory(fsEvent.Name)
	w.emitEvent(ctx, *event, eventCh)
}

// handleFilteredEvent processes events that don't pass filters.
func (w *Watcher) handleFilteredEvent(fsEvent fsnotify.Event, event Event) {
	if event.Op == Create {
		w.handleNewDirectory(fsEvent.Name)
	}
}

// incrementProcessedEvent increments the eventsProcessed counter.
// Called from emitEvent when an event successfully passes through.
func (w *Watcher) incrementProcessedEvent() {
	w.eventsProcessed.Add(1)
}

// emitEvent handles the actual event emission with middleware and debouncing.
func (w *Watcher) emitEvent(ctx context.Context, event Event, eventCh chan<- Event) {
	execute := func() {
		emit := w.buildEmitFunc(ctx, eventCh)
		handler := w.buildMiddlewareHandler(emit)
		w.executeHandler(ctx, event, handler)
	}

	if w.debounceInterface == nil {
		w.debugLogEvent("emitting event directly", event)
		execute()

		return
	}

	key := w.getDebounceKey(event.Path)
	w.debugLog("debouncing event", slog.String("path", event.Path), slog.String("key", string(key)))
	w.debounceInterface.Debounce(key, execute)
}

// buildEmitFunc creates the emit function for sending events.
func (w *Watcher) buildEmitFunc(ctx context.Context, eventCh chan<- Event) func(Event) {
	return func(e Event) {
		select {
		case eventCh <- e:
		case <-w.done:
		case <-ctx.Done():
		}
	}
}

// buildMiddlewareHandler creates the handler chain with all middleware applied.
func (w *Watcher) buildMiddlewareHandler(emit func(Event)) Handler {
	handler := func(_ context.Context, e Event) {
		emit(e)
	}

	for _, v := range slices.Backward(w.middleware) {
		handler = w.wrapWithMiddleware(handler, v)
	}

	return wrapHandlerWithNilReturn(handler)
}

// wrapHandlerWithNilReturn wraps a handler to return nil error.
func wrapHandlerWithNilReturn(handler func(context.Context, Event)) Handler {
	return func(ctx context.Context, e Event) error {
		handler(ctx, e)

		return nil
	}
}

// wrapWithMiddleware wraps a handler function with a middleware.
func (w *Watcher) wrapWithMiddleware(
	handler func(context.Context, Event),
	mw Middleware,
) func(context.Context, Event) {
	wrapped := mw(wrapHandlerWithNilReturn(handler))

	return func(ctx context.Context, e Event) {
		err := wrapped(ctx, e)
		if err != nil {
			w.handleError(
				ErrorContext{Operation: "middleware", Path: e.Path, Retryable: false},
				fmt.Errorf("middleware error: %w", err),
			)
		}
	}
}

// executeHandler runs the handler.
func (w *Watcher) executeHandler(ctx context.Context, event Event, handler Handler) {
	err := handler(ctx, event)
	if err != nil {
		w.handleError(
			ErrorContext{Operation: "handler", Path: event.Path, Retryable: false},
			fmt.Errorf("handler error: %w", err),
		)

		return
	}

	// Event was successfully processed
	w.incrementProcessedEvent()
}

func (w *Watcher) getDebounceKey(path string) DebounceKey {
	return NewDebounceKey(path)
}

// handleNewDirectory adds newly created directories to the watcher
// when recursive mode is enabled. Called from watchLoop without holding lock.
func (w *Watcher) handleNewDirectory(path string) {
	if !w.recursive {
		return
	}

	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return
	}

	w.debugLog("new directory detected", slog.String("path", path))

	w.mu.RLock()
	closed := w.state&flagClosed != 0
	w.mu.RUnlock()

	if closed {
		return
	}

	// Acquire write lock for addPath
	w.mu.Lock()
	defer w.mu.Unlock()

	addErr := w.addPath(NewRootPath(path))
	if addErr != nil {
		w.handleError(ErrorContext{Operation: opAddPath, Path: path, Event: nil, Retryable: true}, addErr)
	}
}

// passesFilters checks if an event passes all registered filters.
func (w *Watcher) passesFilters(event Event) bool {
	for _, f := range w.filters {
		if !f(event) {
			return false
		}
	}

	return true
}

// handleError dispatches errors to the configured handler, errors channel, or stderr.
func (w *Watcher) handleError(ctx ErrorContext, err error) {
	w.debugLog(
		"error occurred",
		slog.String("operation", ctx.Operation),
		slog.String("path", ctx.Path),
		slog.Bool("retryable", ctx.Retryable),
		slog.String("error", err.Error()),
	)

	// Increment error counter
	w.errorsEncountered.Add(1)

	// Send to errors channel if it's being used (non-blocking)
	w.errorsMu.Lock()
	if w.errorsCh != nil {
		select {
		case w.errorsCh <- err:
		default:
			// Channel is full or closed, drop the error
		}
	}
	w.errorsMu.Unlock()

	// Also call error handler if configured
	if w.errorHandler != nil {
		w.errorHandler(ctx, err)

		return
	}

	// Default: log to stderr
	if ctx.Path != "" {
		_, _ = fmt.Fprintf(os.Stderr, "filewatcher: %s: %s: %v\n", ctx.Operation, ctx.Path, err)
	} else {
		_, _ = fmt.Fprintf(os.Stderr, "filewatcher: %s: %v\n", ctx.Operation, err)
	}
}

// convertEvent converts an fsnotify.Event to a filewatcher.Event.
// Returns nil for operations that are not mapped (e.g., Chmod).
//
// Priority of combined operations: Create > Write > Remove > Rename.
// This ensures the most meaningful operation is reported when multiple
// operations occur simultaneously.
//
// If lazyIsDir is true, skips the os.Stat call and always returns IsDir=false.
// If computeHash is true and the event is a Create or Write for a regular file,
// reads the file and computes a SHA-256 hex hash. Hash is empty for directories,
// removed files, permission errors, or when lazyIsDir is true.
func convertEvent(fsEvent fsnotify.Event, lazyIsDir, computeHash bool) *Event {
	var op Op

	switch {
	case fsEvent.Op&fsnotify.Create == fsnotify.Create:
		op = Create
	case fsEvent.Op&fsnotify.Write == fsnotify.Write:
		op = Write
	case fsEvent.Op&fsnotify.Remove == fsnotify.Remove:
		op = Remove
	case fsEvent.Op&fsnotify.Rename == fsnotify.Rename:
		op = Rename
	default:
		return nil
	}

	// Check if path is a directory. For Remove events, the file may already
	// be gone, so we ignore stat errors in that case.
	// If lazyIsDir is true, skip the stat call for performance.
	var (
		isDir   bool
		size    int64
		modTime time.Time
	)

	if !lazyIsDir {
		info, err := os.Stat(fsEvent.Name)
		if err == nil {
			isDir = info.IsDir()
			size = info.Size()
			modTime = info.ModTime()
		}
	}

	hash := ""

	if computeHash && !isDir && (op == Create || op == Write) {
		hash = hashFileContents(fsEvent.Name)
	}

	return &Event{
		Path:      fsEvent.Name,
		Op:        op,
		Timestamp: time.Now(),
		IsDir:     isDir,
		Size:      size,
		ModTime:   modTime,
		Hash:      hash,
	}
}

// hashFileContents returns the hex-encoded SHA-256 hash of the file at path.
// Returns empty string on any error (file missing, permission denied, etc.).
// Hashing is bounded by maxHashFileSize to avoid reading huge files.
func hashFileContents(path string) string {
	return hashFile(path)
}

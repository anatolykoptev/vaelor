//nolint:varnamelen // Idiomatic short names: p (path), w (watcher)
package filewatcher

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fsnotify/fsnotify"
)

const defaultEventBufferSize = 64 // Default capacity for the event channel buffer

// buildDir is the canonical name for build-output directories.
// Extracted to a constant to satisfy goconst (the string "build" appears in
// examples, benchmarks, and DefaultIgnoreDirs).
const buildDir = "build"

// WatcherStateFlags holds state booleans as bit flags for memory efficiency.
// 4 bools (4 bytes) → 1 byte with 4 bit flags.
type WatcherStateFlags byte

const (
	flagClosed WatcherStateFlags = 1 << iota
	flagWatching
)

// DefaultIgnoreDirs returns a copy of the commonly ignored directory names.
// The returned slice is safe to modify without affecting the defaults.
//
//nolint:gochecknoglobals // Exported for user reference in configuration.
var DefaultIgnoreDirs = []string{
	".git", ".hg", ".svn",
	"vendor", "node_modules",
	"dist", buildDir, "bin", "out",
	"__pycache__", ".cache",
}

// DefaultIgnoreDirsCopy returns a defensive copy of DefaultIgnoreDirs
// so callers cannot mutate the global default.
func DefaultIgnoreDirsCopy() []string {
	result := make([]string, len(DefaultIgnoreDirs))
	copy(result, DefaultIgnoreDirs)

	return result
}

// Watcher watches file system paths for changes and emits filtered,
// debounced events through a channel.
//
// Thread-safety guarantees:
//   - New() is not safe for concurrent use during creation
//   - Watch() is safe to call concurrently with Close()
//   - Add(), Remove(), WatchList(), Stats(), IsClosed(), Errors() are safe for concurrent use
//   - Close() is safe to call multiple times and concurrently with other methods
//   - The event channel returned by Watch() is closed when the watcher stops
//   - The error channel returned by Errors() is closed when the watcher stops
//   - All callbacks (errorHandler, onAdd) may be called concurrently
type Watcher struct {
	fswatcher *fsnotify.Watcher

	// Configuration
	paths            []string
	filters          []Filter
	middleware       []Middleware
	recursive        bool
	globalDebounce   time.Duration
	perPathDebounce  time.Duration
	skipDotDirs      bool
	bufferSize       int
	onAdd            func(path string)   // callback when a path is added
	ignoreDirNames   []string            // user-configured dir names to skip during walk
	excludePaths     map[string]struct{} // absolute paths to exclude during walk
	errorHandler     ErrorHandler        // callback for errors during event processing
	lazyIsDir        bool                // skip os.Stat calls in convertEvent for performance
	pollInterval     time.Duration       // polling interval for NFS/FUSE filesystems (0 = disabled)
	polling          bool                // polling mode enabled (supplements fsnotify with periodic scans)
	debug            bool                // enable verbose debug logging
	debugLogger      *slog.Logger        // logger for debug output
	followSymlinks   bool                // follow symbolic links during directory walking
	gitignoreEnabled bool                // enable .gitignore-aware walk filtering
	gitignoreCache   *gitignoreCache     // cache of compiled gitignore matchers
	contentHashing   bool                // compute SHA-256 hash of file content on events
	selfHealInterval time.Duration       // interval for self-healing failed watch registrations (0=disabled)
	failedPaths      map[string]struct{} // paths that failed to add; retried by selfHealLoop
	done             chan struct{}       // closed by Close() to signal shutdown to in-flight goroutines

	// Internal state
	mu        sync.RWMutex
	state     WatcherStateFlags // bit flags: closed, watching
	watchList []string          // tracked paths currently being watched
	walkBatch []string          // batch accumulator for walkDirFunc (nil when not batching)
	wg        sync.WaitGroup    // tracks watchLoop goroutine for clean shutdown

	// Event channel - stored so Close() can close it after stopping debouncer
	// This prevents race between debouncer callbacks and channel close
	eventCh chan<- Event
	// closeEventChOnce ensures eventCh is closed exactly once, either by watchLoop
	// when context is cancelled, or by Close() when watcher is stopped
	closeEventChOnce sync.Once

	// Debouncer (initialized based on config)
	debounceInterface DebouncerInterface

	// Error channel - lazily initialized when Errors() is first called
	errorsMu   sync.Mutex
	errorsCh   chan error
	errorsOnce sync.Once

	// Observability metrics (atomic counters for thread-safe access)
	eventsProcessed   atomic.Uint64 // Total events that passed all filters
	eventsFilteredOut atomic.Uint64 // Events filtered out (dropped by filters)
	errorsEncountered atomic.Uint64 // Errors encountered during processing
	watchErrors       atomic.Uint64 // Watch add failures (ENOSPC, permission denied, etc.)
	startTime         time.Time     // When watcher was created/started
	maxWatches        int           // Maximum inotify watches allowed (0 = no limit)
}

// Compile-time interface check: Watcher implements io.Closer.
var _ io.Closer = (*Watcher)(nil)

// isClosed reports if the watcher has been closed (caller must hold lock).
func (w *Watcher) isClosed() bool {
	return w.state&flagClosed != 0
}

// isWatching reports if the watcher is currently running (caller must hold lock).
func (w *Watcher) isWatching() bool {
	return w.state&flagWatching != 0
}

// IsClosed reports if the watcher has been closed.
// This is safe to call concurrently with other methods.
func (w *Watcher) IsClosed() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()

	return w.isClosed()
}

// IsWatching reports if the watcher is currently running and watching for events.
// This is safe to call concurrently with other methods.
func (w *Watcher) IsWatching() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()

	return w.isWatching()
}

// checkClosedOp returns an error if the watcher is closed.
// operation is the operation being attempted (e.g., "add path", "remove path").
// The lock must be held by the caller.
func (w *Watcher) checkClosedOp(operation string) error {
	if w.state&flagClosed != 0 {
		return fmt.Errorf("%w: cannot %s on closed watcher", ErrWatcherClosed, operation)
	}

	return nil
}

// DebouncerInterface is the interface for debouncer implementations.
type DebouncerInterface interface {
	Debounce(key DebounceKey, fn func())
	Stop()
	Flush()
}

// Compile-time interface checks.
var (
	_ DebouncerInterface = (*Debouncer)(nil)
	_ DebouncerInterface = (*GlobalDebouncer)(nil)
)

// New creates a new Watcher for the given paths with the specified options.
// At least one path must be provided. Paths are validated to exist.
//
// The watcher is not started until Watch() is called.
func New( //nolint:funlen // constructor with full field initialization
	paths []string,
	opts ...Option,
) (*Watcher, error) {
	if len(paths) == 0 {
		return nil, fmt.Errorf("%w: at least one path must be provided", ErrNoPaths)
	}

	// Validate all paths exist
	for _, p := range paths {
		abs, resolveErr := filepath.Abs(p)
		if resolveErr != nil {
			return nil, fmt.Errorf("resolving path %q during validation: %w", p, resolveErr)
		}

		info, statErr := os.Stat(abs)
		if statErr != nil {
			return nil, fmt.Errorf("%w: path %q (resolved: %q)", ErrPathNotFound, p, abs)
		}

		if !info.IsDir() {
			return nil, fmt.Errorf("%w: path %q must be a directory", ErrPathNotDir, p)
		}
	}

	fswatcher, fsErr := fsnotify.NewWatcher()
	if fsErr != nil {
		return nil, fmt.Errorf("creating fsnotify watcher: %w", fsErr)
	}

	w := &Watcher{
		fswatcher:         fswatcher,
		paths:             paths,
		recursive:         true,
		filters:           nil,
		middleware:        nil,
		globalDebounce:    0,
		perPathDebounce:   0,
		errorHandler:      nil,
		skipDotDirs:       true,
		bufferSize:        defaultEventBufferSize,
		onAdd:             nil,
		ignoreDirNames:    nil,
		excludePaths:      make(map[string]struct{}),
		mu:                sync.RWMutex{},
		state:             0,
		watchList:         make([]string, 0, len(paths)),
		walkBatch:         nil,
		wg:                sync.WaitGroup{},
		eventCh:           nil,
		closeEventChOnce:  sync.Once{},
		debounceInterface: nil,
		errorsCh:          nil,
		errorsMu:          sync.Mutex{},
		errorsOnce:        sync.Once{},
		eventsProcessed:   atomic.Uint64{},
		eventsFilteredOut: atomic.Uint64{},
		errorsEncountered: atomic.Uint64{},
		watchErrors:       atomic.Uint64{},
		startTime:         time.Time{},
		maxWatches:        0,
		lazyIsDir:         false,
		pollInterval:      0,
		polling:           false,
		debug:             false,
		debugLogger:       nil,
		followSymlinks:    false,
		gitignoreEnabled:  true,
		gitignoreCache:    newGitignoreCache(),
		contentHashing:    false,
		selfHealInterval:  0,
		failedPaths:       make(map[string]struct{}),
		done:              make(chan struct{}),
	}

	for _, opt := range opts {
		opt(w)
	}

	// Initialize debouncer based on configuration
	w.initDebouncer()

	// Disable gitignore cache if not enabled
	if !w.gitignoreEnabled {
		w.gitignoreCache = nil
	}

	// Auto-detect max watches from system if not explicitly set
	if w.maxWatches == 0 {
		w.maxWatches = detectMaxWatches()
	}

	return w, nil
}

// Watch starts watching the configured paths and returns a read-only channel
// of filtered, debounced events. The channel is closed when the context is
// cancelled or Close() is called.
//
// Callers should range over the returned channel to process events:
//
//	events, err := watcher.Watch(ctx)
//	for event := range events {
//	    handleEvent(event)
//	}
func (w *Watcher) Watch(ctx context.Context) (<-chan Event, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.isClosed() {
		return nil, fmt.Errorf("%w: cannot start watch on closed watcher", ErrWatcherClosed)
	}

	if w.isWatching() {
		return nil, fmt.Errorf("%w: watcher is already running", ErrWatcherRunning)
	}

	// Add initial paths to the fsnotify watcher
	for _, p := range w.paths {
		addErr := w.addPath(NewRootPath(p))
		if addErr != nil {
			return nil, fmt.Errorf("adding watch path %q during Watch(): %w", p, addErr)
		}
	}

	eventCh := make(chan Event, w.bufferSize)
	w.eventCh = eventCh

	w.state |= flagWatching

	// Record start time for uptime tracking
	if w.startTime.IsZero() {
		w.startTime = time.Now()
	}

	w.wg.Add(1)

	go w.watchLoop(ctx, eventCh)

	if w.polling {
		w.wg.Add(1)

		go w.pollLoop(ctx, eventCh)
	}

	if w.selfHealInterval > 0 {
		w.wg.Add(1)

		go w.selfHealLoop(ctx)
	}

	w.debugLog(
		"watch started",
		slog.Bool("polling", w.polling),
		slog.Duration("poll_interval", w.pollInterval),
		slog.Int("buffer_size", w.bufferSize),
	)

	return eventCh, nil
}

// WatchOnce starts watching and returns the first matching event, then stops.
// This is a convenience method for one-shot file watching. The watcher is
// automatically closed after the event is received or the context is cancelled.
//
// Returns the event and nil on success, or an empty Event and an error if
// the context is cancelled or the watcher encounters an error.
func (w *Watcher) WatchOnce(ctx context.Context) (Event, error) {
	events, err := w.Watch(ctx)
	if err != nil {
		return Event{}, fmt.Errorf("starting watch in WatchOnce: %w", err)
	}

	select {
	case event, ok := <-events:
		if !ok {
			return Event{}, fmt.Errorf("%w: event channel closed", ErrWatcherClosed)
		}

		closeErr := w.Close()
		if closeErr != nil {
			return event, fmt.Errorf("closing watcher in WatchOnce: %w", closeErr)
		}

		return event, nil
	case <-ctx.Done():
		closeErr := w.Close()
		if closeErr != nil {
			return Event{}, fmt.Errorf("watchonce cancelled: close: %w, context: %w", closeErr, ctx.Err())
		}

		return Event{}, fmt.Errorf("watchonce cancelled: %w", ctx.Err())
	}
}

// Add adds a new path to the watcher. The path must be an existing directory.
// This method is safe for concurrent use with other methods.
func (w *Watcher) Add(path string) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	err := w.checkClosedOp("add path")
	if err != nil {
		return err
	}

	abs, resolveErr := filepath.Abs(path)
	if resolveErr != nil {
		return fmt.Errorf("resolving path %q in Add(): %w", path, resolveErr)
	}

	pathErr := w.addPath(NewRootPath(abs))
	if pathErr != nil {
		return fmt.Errorf("adding resolved path %q to watcher: %w", abs, pathErr)
	}

	return nil
}

// AddRecursive adds a path with selective recursive depth.
// If maxDepth is 0, only the immediate directory is watched (equivalent to Add).
// If maxDepth is -1, full recursion is used (equivalent to the default recursive behavior).
// If maxDepth is N > 0, directories up to N levels deep are watched.
// This method is safe for concurrent use with other methods.
func (w *Watcher) AddRecursive(path string, maxDepth int) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	err := w.checkClosedOp("add recursive path")
	if err != nil {
		return err
	}

	abs, resolveErr := filepath.Abs(path)
	if resolveErr != nil {
		return fmt.Errorf("resolving path %q in AddRecursive(): %w", path, resolveErr)
	}

	if maxDepth == 0 {
		return w.addPath(NewRootPath(abs))
	}

	if maxDepth < 0 {
		addErr := w.walkAndAddPaths(NewRootPath(abs))
		if addErr != nil {
			return fmt.Errorf("adding resolved path %q to watcher: %w", abs, addErr)
		}

		return nil
	}

	// Depth-limited recursive add
	currentDepth := 0

	return w.addPathWithDepth(NewRootPath(abs), maxDepth, &currentDepth)
}

// addPathWithDepth adds directories up to the specified depth.
func (w *Watcher) addPathWithDepth(root RootPath, maxDepth int, currentDepth *int) error {
	entries, err := os.ReadDir(root.Get())
	if err != nil {
		return fmt.Errorf("reading directory %q: %w", root, err)
	}

	w.tryAddPath(root.Get())

	if *currentDepth >= maxDepth {
		return nil
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		if w.shouldSkipDir(entry.Name()) {
			continue
		}

		subPath := filepath.Join(root.Get(), entry.Name())

		if w.shouldExcludePath(subPath) {
			continue
		}

		w.loadGitignoreForDir(subPath)

		if w.shouldSkipByGitignore(subPath) {
			continue
		}

		*currentDepth++

		addPathErr := w.addPathWithDepth(NewRootPath(subPath), maxDepth, currentDepth)
		if addPathErr != nil {
			return addPathErr
		}

		*currentDepth--
	}

	return nil
}

// opAddPath is the operation name for adding a watch path.
const opAddPath = "add-path"

// Remove removes a path from the watcher. The watcher stops monitoring
// this path and all its subdirectories (if recursive).
// This method is safe for concurrent use with other methods.
func (w *Watcher) Remove(path string) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	err := w.checkClosedOp("remove path")
	if err != nil {
		return err
	}

	abs, resolveErr := filepath.Abs(path)
	if resolveErr != nil {
		return fmt.Errorf("resolving path %q in Remove(): %w", path, resolveErr)
	}

	// Remove the path itself from fsnotify
	_ = w.fswatcher.Remove(abs)

	// Remove all subdirectory watches under this path
	prefix := abs + string(filepath.Separator)

	var remaining []string

	for _, p := range w.watchList {
		if p == abs || strings.HasPrefix(p, prefix) {
			_ = w.fswatcher.Remove(p)
		} else {
			remaining = append(remaining, p)
		}
	}

	w.watchList = remaining

	return nil
}

// copyWatchList returns a defensive copy of the watch list under RLock.
func (w *Watcher) copyWatchList() []string {
	w.mu.RLock()
	defer w.mu.RUnlock()

	result := make([]string, len(w.watchList))
	copy(result, w.watchList)

	return result
}

// WatchList returns a copy of the list of paths currently being watched.
// This method is safe for concurrent use with other methods.
func (w *Watcher) WatchList() []string {
	return w.copyWatchList()
}

// Stats provides observability metrics for the watcher.
type Stats struct {
	WatchCount        int
	IsWatching        bool
	IsClosed          bool
	EventsProcessed   uint64        // Total events that passed all filters
	EventsFilteredOut uint64        // Events filtered out (dropped by filters)
	ErrorsEncountered uint64        // Errors encountered during processing
	WatchErrors       uint64        // Watch add failures (ENOSPC, permission denied, etc.)
	Uptime            time.Duration // Time since watcher was started
	WatchLimit        int           // System inotify limit (0 if unknown)
	WatchBudgetUsed   float64       // Percentage of budget used (0.0-1.0)
}

// Stats returns current statistics about the watcher.
// This method is safe for concurrent use with other methods.
func (w *Watcher) Stats() Stats {
	w.mu.RLock()
	defer w.mu.RUnlock()

	var uptime time.Duration
	if !w.startTime.IsZero() {
		uptime = time.Since(w.startTime)
	}

	var budgetUsed float64
	if w.maxWatches > 0 {
		budgetUsed = float64(len(w.watchList)) / float64(w.maxWatches)
	}

	return Stats{
		WatchCount:        len(w.watchList),
		IsWatching:        w.state&flagWatching != 0,
		IsClosed:          w.state&flagClosed != 0,
		EventsProcessed:   w.eventsProcessed.Load(),
		EventsFilteredOut: w.eventsFilteredOut.Load(),
		ErrorsEncountered: w.errorsEncountered.Load(),
		WatchErrors:       w.watchErrors.Load(),
		Uptime:            uptime,
		WatchLimit:        w.maxWatches,
		WatchBudgetUsed:   budgetUsed,
	}
}

// Errors returns a receive-only channel that receives errors from the watcher.
// This provides an alternative to the error handler callback. If both are
// configured, errors are sent to the channel AND passed to the error handler.
// The channel is closed when the watcher is closed.
//
// This method is safe for concurrent use with other methods.
func (w *Watcher) Errors() <-chan error {
	w.errorsOnce.Do(func() {
		w.errorsCh = make(chan error, w.bufferSize)
	})

	return w.errorsCh
}

// Reset clears the watcher's runtime state while preserving configuration.
// After calling Reset(), the watcher can be started again with Watch().
// This is useful for restarting the watcher after Close() without losing
// filters, middleware, debounce settings, and other options.
//
// The watcher must be closed before calling Reset. Returns an error if
// the watcher is still running.
func (w *Watcher) Reset() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.state&flagWatching != 0 {
		return fmt.Errorf("%w: cannot reset while watcher is running", ErrWatcherRunning)
	}

	// Create a new fsnotify watcher
	fswatcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("creating fsnotify watcher during reset: %w", err)
	}

	// Close old fsnotify watcher if it exists
	if w.fswatcher != nil {
		_ = w.fswatcher.Close()
	}

	// Reset runtime state
	w.fswatcher = fswatcher
	w.state = 0
	w.watchList = make([]string, 0, len(w.paths))
	w.done = make(chan struct{})
	w.closeEventChOnce = sync.Once{}
	w.eventCh = nil
	w.errorsOnce = sync.Once{}
	w.errorsCh = nil

	// Reset metrics
	w.eventsProcessed.Store(0)
	w.eventsFilteredOut.Store(0)
	w.errorsEncountered.Store(0)
	w.watchErrors.Store(0)
	w.startTime = time.Time{}

	// Re-initialize debouncer from configuration
	w.initDebouncer()

	// Re-initialize gitignore cache
	if w.gitignoreEnabled {
		w.gitignoreCache = newGitignoreCache()
	}

	// Re-detect max watches from system
	w.maxWatches = detectMaxWatches()

	return nil
}

// Close stops the watcher and releases all resources.
// It is safe to call Close multiple times.
func (w *Watcher) Close() error {
	w.mu.Lock()

	if w.state&flagClosed != 0 {
		w.mu.Unlock()

		return nil
	}

	w.state |= flagClosed
	w.state &^= flagWatching
	w.watchList = w.watchList[:0]

	w.mu.Unlock()

	// Signal in-flight goroutines to stop before closing channels.
	close(w.done)

	// Stop the debouncer FIRST - waits for all in-flight callbacks to complete.
	// This must happen before closing eventCh to prevent send-on-closed-channel.
	if w.debounceInterface != nil {
		w.debounceInterface.Stop()
	}

	// Close fsnotify watcher - this causes watchLoop to exit.
	err := w.fswatcher.Close()
	if err != nil {
		return fmt.Errorf("closing fsnotify watcher: %w", err)
	}

	// Wait for watchLoop to fully exit before closing eventCh.
	// This ensures no goroutine is mid-send when we close the channel.
	w.wg.Wait()

	// Now safe to close eventCh - watchLoop and all callbacks are done.
	// Use sync.Once to coordinate with watchLoop's defer.
	w.mu.RLock()
	ch := w.eventCh
	w.mu.RUnlock()

	if ch != nil {
		w.closeEventChOnce.Do(func() { close(ch) })
	}

	// Close the errors channel if it was created
	w.errorsMu.Lock()
	if w.errorsCh != nil {
		close(w.errorsCh)
	}
	w.errorsMu.Unlock()

	return nil
}

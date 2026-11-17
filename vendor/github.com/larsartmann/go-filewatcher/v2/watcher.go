//nolint:varnamelen // Idiomatic short names: p (path), w (watcher)
package filewatcher

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
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
func DefaultIgnoreDirsCopy() []string { return slices.Clone(DefaultIgnoreDirs) }

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
	fswatcher watchBackend

	// Configuration
	paths                    []string
	filters                  []Filter
	middleware               []Middleware
	recursive                bool
	globalDebounce           time.Duration
	perPathDebounce          time.Duration
	skipDotDirs              bool
	bufferSize               int
	onAdd                    func(path string)         // callback when a path is added
	ignoreDirNames           []string                  // user-configured dir names to skip during walk
	excludePaths             map[string]struct{}       // absolute paths to exclude during walk
	errorHandler             ErrorHandler              // callback for errors during event processing
	lazyIsDir                bool                      // skip os.Stat calls in convertEvent for performance
	pollInterval             time.Duration             // polling interval for NFS/FUSE filesystems (0 = disabled)
	polling                  bool                      // polling mode enabled (supplements fsnotify with periodic scans)
	errorBufferSize          int                       // size of the error channel (0 = use bufferSize)
	eventDropOnFull          bool                      // drop events when channel is full instead of blocking
	debug                    bool                      // enable verbose debug logging
	debugLogger              *slog.Logger              // logger for debug output
	followSymlinks           bool                      // follow symbolic links during directory walking
	watchFilteredDirs        bool                      // watch new directories even when the Create event was filtered out
	gitignoreEnabled         bool                      // enable .gitignore-aware walk filtering
	gitignoreCache           *gitignoreCache           // cache of compiled gitignore matchers
	contentHashMaxSize       int64                     // Max file size for content hashing (0 = disabled, default 10 MiB when enabled)
	selfHealInterval         time.Duration             // interval for self-healing failed watch registrations (0=disabled)
	failedPaths              map[string]string         // pathKey → original path; retried by selfHealLoop
	done                     chan struct{}             // closed by Close() to signal shutdown to in-flight goroutines
	cleanups                 []func() error            // lifecycle cleanup funcs registered via WithCleanup
	caseSensitivity          FilesystemCaseSensitivity // configured case-sensitivity mode
	effectiveCaseSensitivity FilesystemCaseSensitivity // resolved (auto → concrete) for fast pathKey

	// Internal state
	mu             sync.RWMutex
	state          WatcherStateFlags   // bit flags: closed, watching
	watchList      []string            // tracked paths currently being watched
	watchListKeys  map[string]struct{} // pathKey(path) → present; O(1) watched check and dedup
	walkBatch      []string            // batch accumulator for walkDirFunc (nil when not batching)
	symlinkVisited map[string]struct{} // canonical paths already visited via symlink (nil when not walking)
	wg             sync.WaitGroup      // tracks watchLoop goroutine for clean shutdown

	// Event channel - stored so Close() can close it after stopping debouncer
	// This prevents race between debouncer callbacks and channel close
	eventCh chan<- Event
	// closeEventChOnce ensures eventCh is closed exactly once, either by the
	// per-Watch closer goroutine once every channel-sending loop has exited,
	// or by Close() when the watcher is stopped
	closeEventChOnce sync.Once

	// Debouncer (initialized based on config)
	debounceInterface DebouncerInterface

	// Error channel - lazily initialized when Errors() is first called
	errorsMu   sync.Mutex
	errorsCh   chan error
	errorsOnce sync.Once

	// Observability metrics (atomic counters for thread-safe access)
	eventsProcessed             atomic.Uint64 // Total events that reached the event channel (passed all middleware)
	eventsFilteredOut           atomic.Uint64 // Events filtered out (dropped by filters)
	eventsDroppedByMiddleware   atomic.Uint64 // Events dropped by middleware (rate limit, dedup, circuit breaker, etc.)
	eventsDroppedByBackpressure atomic.Uint64 // Events dropped because the event channel was full (DropOnFull mode)
	errorsEncountered           atomic.Uint64 // Errors encountered during processing
	errorsDropped               atomic.Uint64 // Errors dropped because the error channel was full
	watchErrors                 atomic.Uint64 // Watch add failures (ENOSPC, permission denied, etc.)
	startTime                   time.Time     // When watcher was created/started
	maxWatches                  int           // Maximum inotify watches allowed (0 = no limit)
	maxWatchesExplicit          bool          // True when maxWatches was set via WithMaxWatches(n)
	maxWatchesFraction          float64       // Safety fraction applied to auto-detected maxWatches (default 1.0)
	maxWatchesDetected          int           // Raw system-detected limit before safety fraction (0 if explicit or unknown)
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

// validateDirExists stats abs and returns a wrapped sentinel error if the
// path is missing or not a directory. This prevents non-existent paths from
// silently entering the self-heal retry loop and gives consumers a clear,
// categorized error. abs must already be an absolute, cleaned path.
func validateDirExists(abs string) error {
	info, err := os.Stat(abs)
	if err != nil {
		return fmt.Errorf("%w: %s", ErrPathNotFound, abs)
	}

	if !info.IsDir() {
		return fmt.Errorf("%w: %s", ErrPathNotDir, abs)
	}

	return nil
}

// withResolvedPath acquires the watcher mutex, verifies the watcher isn't closed,
// resolves path to an absolute path, then invokes fn with the resolved path.
// The mutex is held for the duration of fn. opName describes the operation for
// the closed-check error; caller identifies the public method for the resolution
// error. This is the canonical entry point for mutating watcher operations
// (Add, Remove, AddRecursive) — keeping their pre-op validation in one place.
func (w *Watcher) withResolvedPath(path, opName string, fn func(abs string) error) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	err := w.checkClosedOp(opName)
	if err != nil {
		return err //nolint:erraudit // checkClosedOp already embeds opName in its message
	}

	abs, resolveErr := normalizePath(path)
	if resolveErr != nil {
		return fmt.Errorf("resolving path %q during %s: %w", path, opName, resolveErr)
	}

	return fn(abs)
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
func New(
	paths []string,
	opts ...Option,
) (*Watcher, error) {
	if len(paths) == 0 {
		return nil, fmt.Errorf("%w: at least one path must be provided", ErrNoPaths)
	}

	normalizedPaths, err := validateAndNormalizePaths(paths)
	if err != nil {
		return nil, err
	}

	w := &Watcher{
		fswatcher:                   nil,
		paths:                       normalizedPaths,
		recursive:                   true,
		filters:                     nil,
		middleware:                  nil,
		globalDebounce:              0,
		perPathDebounce:             0,
		errorHandler:                nil,
		skipDotDirs:                 true,
		bufferSize:                  defaultEventBufferSize,
		onAdd:                       nil,
		ignoreDirNames:              nil,
		excludePaths:                make(map[string]struct{}),
		mu:                          sync.RWMutex{},
		state:                       0,
		watchList:                   make([]string, 0, len(paths)),
		watchListKeys:               make(map[string]struct{}),
		walkBatch:                   nil,
		symlinkVisited:              nil,
		wg:                          sync.WaitGroup{},
		eventCh:                     nil,
		closeEventChOnce:            sync.Once{},
		debounceInterface:           nil,
		errorsCh:                    nil,
		errorsMu:                    sync.Mutex{},
		errorsOnce:                  sync.Once{},
		eventsProcessed:             atomic.Uint64{},
		eventsFilteredOut:           atomic.Uint64{},
		eventsDroppedByMiddleware:   atomic.Uint64{},
		eventsDroppedByBackpressure: atomic.Uint64{},
		errorsEncountered:           atomic.Uint64{},
		errorsDropped:               atomic.Uint64{},
		watchErrors:                 atomic.Uint64{},
		startTime:                   time.Time{},
		maxWatches:                  0,
		maxWatchesExplicit:          false,
		maxWatchesFraction:          1.0, // default: use full system limit (set WithMaxWatchesSafetyFraction for shared environments)
		maxWatchesDetected:          0,
		lazyIsDir:                   false,
		pollInterval:                0,
		polling:                     false,
		eventDropOnFull:             false,
		errorBufferSize:             0,
		debug:                       false,
		debugLogger:                 nil,
		followSymlinks:              false,
		watchFilteredDirs:           true, // default: keep watching new dirs even if Create event was filtered
		gitignoreEnabled:            true,
		gitignoreCache:              newGitignoreCache(),
		contentHashMaxSize:          0,
		selfHealInterval:            0,
		failedPaths:                 make(map[string]string),
		done:                        make(chan struct{}),
		cleanups:                    nil,
		caseSensitivity:             CaseSensitivityAuto,
		effectiveCaseSensitivity:    CaseSensitive,
	}

	for _, opt := range opts {
		opt(w)
	}

	// Resolve auto case-sensitivity after options have been applied.
	w.effectiveCaseSensitivity = resolveCaseSensitivity(w.caseSensitivity)

	// Normalize exclude-path keys to the effective case-sensitivity mode so
	// that lookups in shouldExcludePath are O(1) and case-aware.
	if len(w.excludePaths) > 0 {
		normalized := make(map[string]struct{}, len(w.excludePaths))

		for p := range w.excludePaths {
			normalized[w.pathKey(p)] = struct{}{}
		}

		w.excludePaths = normalized
	}

	// Create real backend if not injected by withBackend option (tests)
	if w.fswatcher == nil {
		fw, fsErr := fsnotify.NewWatcher()
		if fsErr != nil {
			return nil, fmt.Errorf("creating fsnotify watcher: %w", fsErr)
		}

		w.fswatcher = fsnotifyBackend{fw}
	}

	// Initialize debouncer based on configuration
	w.initDebouncer()

	// Disable gitignore cache if not enabled
	if !w.gitignoreEnabled {
		w.gitignoreCache = nil
	}

	// Auto-detect max watches from system if not explicitly set.
	// The safety fraction only applies to auto-detected limits — explicit
	// limits from WithMaxWatches(n) are used as-is.
	if w.maxWatches == 0 {
		w.maxWatches = detectMaxWatches()
		w.maxWatchesDetected = w.maxWatches
		w.applyMaxWatchesFraction()
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

	// Re-validate paths still exist before adding (they may have been deleted
	// between New() and Watch()). This gives a clear error instead of a
	// generic fsnotify failure or silent self-heal retry loop.
	for _, p := range w.paths {
		if err := validateDirExists(p); err != nil {
			return nil, fmt.Errorf("Watch(): %w", err)
		}
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

	// chUsers tracks every goroutine that can send on eventCh (watchLoop,
	// pollLoop). A closer goroutine closes the channel only after ALL of them
	// have exited, so close can never race an in-flight send — the poll loop
	// and the fsnotify loop share this channel, and closing it from whichever
	// loop happens to exit first raced the other's trackedEmit select (CI
	// run 37585089337: "DATA RACE closechan vs chansend" under -bench -race).
	// WaitGroup.Wait provides the happens-before edge between the last
	// sender's return and the close.
	var chUsers sync.WaitGroup

	chUsers.Add(1)

	w.wg.Add(1)

	go w.watchLoop(ctx, eventCh, &chUsers)

	if w.polling {
		chUsers.Add(1)
		w.wg.Add(1)

		go w.pollLoop(ctx, eventCh, &chUsers)
	}

	// The closer joins w.wg so Close()'s wg.Wait() also awaits it — Reset()
	// reassigns closeEventChOnce, and it is only safe to touch the old Once
	// (and the field) when no goroutine can still be doing so. The drain in
	// cancelAndDrain-style callers unblocks when this closes the channel.
	w.wg.Go(func() {
		chUsers.Wait()
		w.closeEventChOnce.Do(func() { close(eventCh) })
	})

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
	return w.withResolvedPath(path, "add path", func(abs string) error {
		if err := validateDirExists(abs); err != nil {
			return err
		}

		pathErr := w.addPath(NewRootPath(abs))
		if pathErr != nil {
			return fmt.Errorf("adding resolved path %q to watcher: %w", abs, pathErr)
		}

		return nil
	})
}

// AddRecursive adds a path with selective recursive depth.
// If maxDepth is 0, only the immediate directory is watched (equivalent to Add).
// If maxDepth is -1, full recursion is used (equivalent to the default recursive behavior).
// If maxDepth is N > 0, directories up to N levels deep are watched.
// This method is safe for concurrent use with other methods.
func (w *Watcher) AddRecursive(path string, maxDepth int) error {
	err := w.withResolvedPath(path, "add recursive path", func(abs string) error {
		if err := validateDirExists(abs); err != nil {
			return err
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

		currentDepth := 0

		return w.addPathWithDepth(NewRootPath(abs), maxDepth, &currentDepth)
	})
	if err != nil {
		return fmt.Errorf("maxDepth=%d: %w", maxDepth, err)
	}

	return nil
}

// addPathWithDepth adds directories up to the specified depth.
func (w *Watcher) addPathWithDepth(root RootPath, maxDepth int, currentDepth *int) error {
	entries, err := os.ReadDir(root.Get())
	if err != nil {
		return fmt.Errorf("reading directory %q (maxDepth=%d): %w", root, maxDepth, err)
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
			return fmt.Errorf("adding %q (maxDepth=%d): %w", subPath, maxDepth, addPathErr)
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
	return w.withResolvedPath(path, "remove path", func(abs string) error {
		// Remove the path itself from fsnotify
		_ = w.fswatcher.Remove(abs) //nolint:erraudit // best-effort: an unwatched path is expected here

		// Remove all subdirectory watches under this path, respecting
		// filesystem case-sensitivity for the comparison.
		absKey := w.pathKey(abs)
		prefix := absKey + string(filepath.Separator)

		var remaining []string

		for _, p := range w.watchList {
			pKey := w.pathKey(p)

			if pKey == absKey || strings.HasPrefix(pKey, prefix) {
				_ = w.fswatcher.Remove(
					p,
				) //nolint:erraudit // best-effort subtree prune; not-watched paths are expected
				delete(w.watchListKeys, pKey) // incremental: drop only pruned subtree keys
			} else {
				remaining = append(remaining, p)
			}
		}

		w.watchList = remaining

		return nil
	})
}

// addToWatchList appends path to both watchList and watchListKeys, keeping the
// parallel data structures in sync. The caller must hold w.mu and must have
// already verified the path is not already watched (via watchListKeys).
func (w *Watcher) addToWatchList(path string) {
	w.watchList = append(w.watchList, path)
	w.watchListKeys[w.pathKey(path)] = struct{}{}
}

// copyWatchList returns a defensive copy of the watch list under RLock.
func (w *Watcher) copyWatchList() []string {
	w.mu.RLock()
	defer w.mu.RUnlock()

	result := slices.Clone(w.watchList)

	return result
}

// WatchList returns a copy of the list of paths currently being watched.
// This method is safe for concurrent use with other methods.
func (w *Watcher) WatchList() []string {
	return w.copyWatchList()
}

// Stats provides observability metrics for the watcher.
type Stats struct {
	WatchCount                  int                       // Number of paths currently being watched
	IsWatching                  bool                      // Whether the watcher is running
	IsClosed                    bool                      // Whether the watcher is closed
	EventsProcessed             uint64                    // Total events that reached the event channel
	EventsFilteredOut           uint64                    // Events dropped by filters
	EventsDroppedByMiddleware   uint64                    // Events dropped by middleware (rate limit, dedup, etc.)
	EventsDroppedByBackpressure uint64                    // Events dropped because the event channel was full (DropOnFull mode)
	ErrorsEncountered           uint64                    // Total errors encountered
	ErrorsDropped               uint64                    // Errors dropped because the error channel was full
	WatchErrors                 uint64                    // Watch add failures (ENOSPC, permission denied, etc.)
	Uptime                      time.Duration             // Time since watcher was started
	WatchLimit                  int                       // System inotify limit before safety fraction (0 if unknown or explicit)
	WatchBudgetCap              int                       // Effective watch budget cap after safety fraction
	WatchBudgetUsed             float64                   // Percentage of budget used (0.0-1.0)
	CaseSensitivity             string                    // Resolved case-sensitivity mode ("case-sensitive", "case-insensitive", "auto")
	CaseSensitivityMode         FilesystemCaseSensitivity // Resolved case-sensitivity mode (enum, type-safe)
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
		WatchCount:                  len(w.watchList),
		IsWatching:                  w.state&flagWatching != 0,
		IsClosed:                    w.state&flagClosed != 0,
		EventsProcessed:             w.eventsProcessed.Load(),
		EventsFilteredOut:           w.eventsFilteredOut.Load(),
		EventsDroppedByMiddleware:   w.eventsDroppedByMiddleware.Load(),
		EventsDroppedByBackpressure: w.eventsDroppedByBackpressure.Load(),
		ErrorsEncountered:           w.errorsEncountered.Load(),
		ErrorsDropped:               w.errorsDropped.Load(),
		WatchErrors:                 w.watchErrors.Load(),
		Uptime:                      uptime,
		WatchLimit:                  w.maxWatchesDetected,
		WatchBudgetCap:              w.maxWatches,
		WatchBudgetUsed:             budgetUsed,
		CaseSensitivity:             w.effectiveCaseSensitivity.String(),
		CaseSensitivityMode:         w.effectiveCaseSensitivity,
	}
}

// EffectiveCaseSensitivity returns the resolved case-sensitivity mode
// (auto-detection resolved to concrete CaseSensitive or CaseInsensitive).
// This is the type-safe enum equivalent of Stats().CaseSensitivity (string).
//
// This method is safe for concurrent use with other methods.
func (w *Watcher) EffectiveCaseSensitivity() FilesystemCaseSensitivity {
	w.mu.RLock()
	defer w.mu.RUnlock()

	return w.effectiveCaseSensitivity
}

// Errors returns a receive-only channel that receives errors from the watcher.
// This provides an alternative to the error handler callback. If both are
// configured, errors are sent to the channel AND passed to the error handler.
// The channel is closed when the watcher is closed.
//
// This method is safe for concurrent use with other methods.
func (w *Watcher) Errors() <-chan error {
	w.errorsOnce.Do(func() {
		size := w.errorBufferSize
		if size <= 0 {
			size = w.bufferSize
		}

		w.errorsCh = make(chan error, size)
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
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("creating fsnotify watcher during reset: %w", err)
	}

	// Close old fsnotify watcher if it exists
	if w.fswatcher != nil {
		_ = w.fswatcher.Close() //nolint:erraudit // the old watcher is discarded either way
	}

	// Reset runtime state
	w.fswatcher = fsnotifyBackend{fw}
	w.state = 0
	w.watchList = make([]string, 0, len(w.paths))
	w.watchListKeys = make(map[string]struct{})
	w.done = make(chan struct{})
	w.closeEventChOnce = sync.Once{}
	w.eventCh = nil
	w.errorsOnce = sync.Once{}
	w.errorsCh = nil
	w.cleanups = nil
	w.effectiveCaseSensitivity = resolveCaseSensitivity(w.caseSensitivity)

	// Reset metrics
	w.eventsProcessed.Store(0)
	w.eventsFilteredOut.Store(0)
	w.eventsDroppedByMiddleware.Store(0)
	w.eventsDroppedByBackpressure.Store(0)
	w.errorsEncountered.Store(0)
	w.errorsDropped.Store(0)
	w.watchErrors.Store(0)
	w.startTime = time.Time{}

	// Re-initialize debouncer from configuration
	w.initDebouncer()

	// Re-initialize gitignore cache
	if w.gitignoreEnabled {
		w.gitignoreCache = newGitignoreCache()
	}

	// Re-detect max watches from system (only when not explicitly configured)
	if !w.maxWatchesExplicit {
		w.maxWatches = detectMaxWatches()
		w.maxWatchesDetected = w.maxWatches
		w.applyMaxWatchesFraction()
	}

	return nil
}

// applyMaxWatchesFraction reduces the watch limit by the configured safety
// fraction, leaving headroom for other processes on shared machines.
// No-op when the fraction is 1.0 (default) or when maxWatches is 0/unlimited.
func (w *Watcher) applyMaxWatchesFraction() {
	if w.maxWatches > 0 && w.maxWatchesFraction > 0 && w.maxWatchesFraction < 1.0 {
		w.maxWatches = int(float64(w.maxWatches) * w.maxWatchesFraction)
	}
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
	clear(w.watchListKeys)

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
	// Use sync.Once to coordinate with the per-Watch closer goroutine.
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

	// Run registered cleanup functions (e.g., close file handles from
	// NewFileLogMiddleware + WithCleanup). Called after all goroutines and
	// channels are torn down so cleanup funcs can safely release resources.
	for _, cleanup := range w.cleanups {
		if cleanup != nil {
			_ = cleanup() //nolint:erraudit // teardown: cleanup errors have nowhere to be reported
		}
	}

	return nil
}

// validateAndNormalizePaths validates that all paths exist and are directories,
// then returns absolute paths. Centralized so the constructor stays lean.
func validateAndNormalizePaths(paths []string) ([]string, error) {
	normalized := make([]string, 0, len(paths))

	for _, p := range paths {
		abs, resolveErr := normalizePath(p)
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

		normalized = append(normalized, abs)
	}

	return normalized, nil
}

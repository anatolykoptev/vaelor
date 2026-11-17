package filewatcher

import (
	"fmt"
	"log/slog"
	"time"
)

// Option configures a Watcher during creation.
type Option func(*Watcher)

// requireNonNegativeDuration panics if delay is negative. Centralizes the
// input validation shared by WithDebounce and WithPerPathDebounce. optionName
// is included in the panic message to identify the caller.
func requireNonNegativeDuration(optionName string, delay time.Duration) {
	if delay < 0 {
		panic(fmt.Sprintf("filewatcher: %s: negative duration %v", optionName, delay))
	}
}

// WithDebounce sets a global debounce delay. All events are coalesced
// into a single emission after the delay since the last event.
// Default is no debouncing. Panics if delay is negative.
func WithDebounce(delay time.Duration) Option {
	requireNonNegativeDuration("WithDebounce", delay)

	return func(w *Watcher) {
		w.globalDebounce = delay
	}
}

// WithPerPathDebounce sets a per-path debounce delay. Events for different
// file paths are debounced independently. This is useful when watching
// many files and changes to different files should trigger separate actions.
// Panics if delay is negative.
func WithPerPathDebounce(delay time.Duration) Option {
	requireNonNegativeDuration("WithPerPathDebounce", delay)

	return func(w *Watcher) {
		w.perPathDebounce = delay
	}
}

// WithFilter adds an event filter. Only events that pass all registered
// filters are emitted. Multiple filters are ANDed together.
func WithFilter(f Filter) Option {
	return func(w *Watcher) {
		w.filters = append(w.filters, f)
	}
}

// WithExtensions filters events to only those matching the given file
// extensions. Extensions should include the dot prefix (e.g., ".go", ".md").
func WithExtensions(exts ...string) Option {
	return func(w *Watcher) {
		w.filters = append(w.filters, FilterExtensions(exts...))
	}
}

// WithIgnoreDirs discards events for files within the given directory names.
// Common values: "vendor", "node_modules", ".git", "dist", "build", "bin".
// Also skips these directories during recursive walking.
func WithIgnoreDirs(dirs ...string) Option {
	return func(w *Watcher) {
		w.filters = append(w.filters, FilterIgnoreDirs(dirs...))
		w.ignoreDirNames = append(w.ignoreDirNames, dirs...)
	}
}

// WithIgnoreHidden discards events for hidden files and directories
// (those starting with a dot).
func WithIgnoreHidden() Option {
	return func(w *Watcher) {
		w.filters = append(w.filters, FilterIgnoreHidden())
	}
}

// WithRecursive enables recursive directory watching. When enabled,
// subdirectories are added to the watcher automatically, and newly
// created directories are added dynamically. Default is true.
func WithRecursive(b bool) Option {
	return func(w *Watcher) {
		w.recursive = b
	}
}

// WithMiddleware adds middleware to the event processing pipeline.
// Middleware is applied in reverse order (last added runs first),
// matching the go-cqrs-lite convention.
func WithMiddleware(m ...Middleware) Option {
	return func(w *Watcher) {
		w.middleware = append(w.middleware, m...)
	}
}

// WithErrorHandler sets a callback for watcher errors that occur during
// the event loop. Errors are passed to this handler with context about
// what operation was being performed. If not set, errors are logged to stderr.
func WithErrorHandler(handler ErrorHandler) Option {
	return func(w *Watcher) {
		w.errorHandler = handler
	}
}

// WithSkipDotDirs controls whether directories starting with a dot (.
// are skipped during recursive directory walking. Default is true.
// Set to false to watch dot-directories like .config, .vscode, etc.
func WithSkipDotDirs(skip bool) Option {
	return func(w *Watcher) {
		w.skipDotDirs = skip
	}
}

// WithBuffer sets the buffer size for the event channel.
// A larger buffer helps handle event bursts without dropping events.
// Default is 64. A value of 0 creates an unbuffered channel which may
// cause deadlocks if the consumer is slow; use with caution.
func WithBuffer(size int) Option {
	return func(w *Watcher) {
		if size >= 0 {
			w.bufferSize = size
		}
	}
}

// WithErrorBufferSize sets the buffer size for the error channel (Errors()).
// This decouples the error channel capacity from the event channel, allowing
// a larger error buffer under high error load (e.g., many ENOSPC errors
// during a large tree walk). When not set or set to 0, the error channel
// uses the same buffer size as the event channel (WithBuffer).
func WithErrorBufferSize(size int) Option {
	return func(w *Watcher) {
		w.errorBufferSize = size
	}
}

// WithOnAdd sets a callback that is invoked whenever a new path is added
// to the watcher. This is useful for logging or tracking which directories
// are being watched.
func WithOnAdd(fn func(path string)) Option {
	return func(w *Watcher) {
		w.onAdd = fn
	}
}

// WithOnError sets a simple callback for errors that occur during watching.
// This is a convenience wrapper around WithErrorHandler for simple use cases.
//
// Deprecated: Use WithErrorHandler instead. It provides the full ErrorContext
// (operation, path, retryability) and is the canonical error-handling option.
// To preserve the simple behavior, wrap the callback:
//
//	WithErrorHandler(func(_ ErrorContext, err error) { fn(err) })
func WithOnError(fn func(error)) Option {
	return func(w *Watcher) {
		w.errorHandler = func(_ ErrorContext, err error) {
			fn(err)
		}
	}
}

// WithLazyIsDir skips the os.Stat call in convertEvent for better performance.
// When enabled, Event.IsDir will always be false. This is useful when you
// don't need directory information and want to minimize filesystem calls.
// Default is false (IsDir is populated accurately).
func WithLazyIsDir() Option {
	return func(w *Watcher) {
		w.lazyIsDir = true
	}
}

// WithIgnorePatterns adds glob patterns to ignore at the filter level.
// Files matching any of the patterns are filtered out.
// Patterns are applied to the filename only (not the full path).
// Example patterns: "*.log", "*.tmp", ".*".
func WithIgnorePatterns(patterns ...string) Option {
	return func(w *Watcher) {
		w.filters = append(w.filters, FilterIgnoreGlobs(patterns...))
	}
}

// WithPollInterval sets a polling interval for filesystem change detection.
// When set to a non-zero duration, the watcher will periodically poll watched
// paths instead of relying solely on OS-native file events. This is useful
// for NFS mounts, FUSE filesystems, and other environments where inotify/FSEvents
// are unreliable or unavailable.
//
// A typical value is 1–5 seconds. Shorter intervals detect changes faster
// but consume more CPU. Set to 0 (default) to disable polling.
func WithPollInterval(interval time.Duration) Option {
	return func(w *Watcher) {
		w.pollInterval = interval
	}
}

const defaultPollInterval = 2 * time.Second // Default polling interval for NFS/FUSE

// WithPolling enables or disables polling-based filesystem watching as a
// complement to OS-native events. When enabled (fallback=true), the watcher
// supplements fsnotify with periodic polling at the configured poll interval
// (default 2s, customizable via WithPollInterval). This detects changes on
// NFS mounts, FUSE filesystems, Docker volumes, and other environments where
// inotify/FSEvents may not fire.
//
// When fallback=false, polling is fully disabled (identical to not calling
// this option).
//
// Limitation: when both backends are active, a single file change can produce
// two events (one from fsnotify, one from the poll loop). Polling is a fallback,
// not a duplicate-suppression layer. Consumers that need exactly-once semantics
// should use [MiddlewareDeduplicate] or rely on idempotent event handling.
// See the Troubleshooting guide for details.
func WithPolling(fallback bool) Option {
	return func(w *Watcher) {
		w.polling = fallback
		if fallback && w.pollInterval == 0 {
			w.pollInterval = defaultPollInterval
		}
	}
}

// WithDebug enables verbose debug logging to the provided logger.
// If logger is nil, log/slog.Default() is used. When enabled, the watcher
// logs detailed information about event processing, filter decisions,
// debounce actions, and error handling.
func WithDebug(logger *slog.Logger) Option {
	return func(w *Watcher) { //nolint:varnamelen // idiomatic short name for Watcher
		w.debug = true

		if logger == nil {
			logger = slog.Default()
		}

		w.debugLogger = logger
	}
}

// WithWatchedIgnoreDirs adds directory names that are ignored at the event
// filtering level only, without affecting directory walking. This differs
// from WithIgnoreDirs which both filters events AND skips directories during
// recursive walking. Use this when you want to walk into a directory (to watch
// its subdirectories) but ignore file events within it.
//
// Deprecated: Use WithFilter(FilterIgnoreDirs(dirs...)) instead for better composability.
func WithWatchedIgnoreDirs(dirs ...string) Option {
	return func(w *Watcher) {
		w.filters = append(w.filters, FilterIgnoreDirs(dirs...))
	}
}

// WithFollowSymlinks enables following symbolic links during directory walking.
// When enabled, symlinked directories are resolved to their targets and
// added to the watcher. This allows monitoring directories accessed through
// symlinks. Default is false (symlinks are not followed).
//
// Cycle detection is built-in: if a symlink target was already visited
// (directly or via another symlink), it is skipped to prevent infinite recursion.
func WithFollowSymlinks(follow bool) Option {
	return func(w *Watcher) {
		w.followSymlinks = follow
	}
}

// WithWatchFilteredDirectories controls whether newly created directories
// are added to the watcher even when their Create event was filtered out.
//
// When true (default), the watcher adds new directories to maintain a complete
// watch tree, even if events for files within them are filtered. This ensures
// that files created in those directories are still detected.
//
// When false, directories whose Create event was filtered out are NOT added,
// saving inotify budget. Use this when you truly want to ignore certain
// subtrees (e.g., build output directories filtered by name).
func WithWatchFilteredDirectories(watch bool) Option {
	return func(w *Watcher) {
		w.watchFilteredDirs = watch
	}
}

// WithEventChannelMode configures how the watcher behaves when the event
// channel is full and the consumer is not reading fast enough.
//
//   - EventChannelBlocking (default): the watch loop blocks until the event
//     can be sent. This preserves all events but can cause the kernel to drop
//     fsnotify events if the consumer is consistently slow.
//   - EventChannelDropOnFull: events are dropped (and counted in
//     Stats.EventsDroppedByBackpressure) when the channel is full. This
//     prevents the watch loop from blocking but loses events.
//
// Use DropOnFull when you prefer losing events over blocking the watcher
// (e.g., in firehose scenarios where eventual consistency is acceptable).
func WithEventChannelMode(mode EventChannelMode) Option {
	return func(w *Watcher) {
		w.eventDropOnFull = mode == EventChannelDropOnFull
	}
}

// EventChannelMode controls the watcher's behavior when the event channel is full.
type EventChannelMode int

const (
	// EventChannelBlocking blocks the watch loop until the event can be sent.
	// This is the default and preserves all events at the cost of potential
	// kernel-level event drops if the consumer is consistently slow.
	EventChannelBlocking EventChannelMode = iota
	// EventChannelDropOnFull drops events when the channel is full, incrementing
	// Stats.EventsDroppedByBackpressure. Use this when you prefer losing events
	// over blocking the watcher.
	EventChannelDropOnFull
)

// WithExcludePaths excludes specific absolute paths (and their subtrees) from
// being watched during directory walking. This is a walk-time exclusion — directories
// matching these paths are never added to inotify. Paths are normalized to absolute paths.
// Use this to exclude entire subtrees like "/home/user/projects/forks" without
// affecting directories with the same name elsewhere.
func WithExcludePaths(paths ...string) Option {
	return func(w *Watcher) {
		for _, p := range paths {
			w.excludePaths[cleanPath(p)] = struct{}{}
		}
	}
}

// WithGitignore enables or disables .gitignore-aware walk filtering.
// When enabled (default: true), the watcher reads .gitignore files during
// directory traversal and skips directories matching gitignore patterns.
// This significantly reduces the number of inotify watches needed for
// directories with large build artifacts, generated code, etc.
func WithGitignore(enabled bool) Option {
	return func(w *Watcher) {
		w.gitignoreEnabled = enabled
	}
}

// WithCaseSensitivity configures how the watcher compares paths.
//
// Different filesystems treat filename case differently: NTFS (Windows) and
// APFS (macOS) are case-insensitive by default, while ext4/XFS/btrfs (Linux)
// are case-sensitive. The watcher uses this setting when deduplicating watches,
// matching Remove() targets, comparing exclude paths, and generating debounce
// keys.
//
//   - CaseSensitivityAuto (default): case-insensitive on Windows and macOS,
//     case-sensitive everywhere else.
//   - CaseSensitive: paths that differ only in case are treated as distinct.
//   - CaseInsensitive: paths that differ only in case are treated as identical.
//
// Event paths reported by the watcher preserve the case provided by the
// underlying backend; only internal comparisons are affected.
func WithCaseSensitivity(mode FilesystemCaseSensitivity) Option {
	return func(w *Watcher) {
		w.caseSensitivity = mode
	}
}

// WithMaxWatches sets the maximum number of inotify watches the watcher
// will attempt to create. When this limit is reached, additional directories
// are skipped with a warning. If set to 0 (default), the limit is auto-detected
// from /proc/sys/fs/inotify/max_user_watches on Linux, or unlimited on other platforms.
func WithMaxWatches(n int) Option {
	return func(w *Watcher) {
		w.maxWatches = n
		w.maxWatchesExplicit = n > 0
	}
}

// WithMaxWatchesSafetyFraction sets a safety fraction applied to the auto-detected
// inotify watch limit. This leaves headroom for other processes on shared or
// multi-tenant machines, preventing the watcher from exhausting the entire
// system-wide budget.
//
// A value of 0.75 means the watcher uses at most 75% of the system limit.
// Values <= 0 or > 1 are clamped to the default (1.0 = no reduction).
// This only affects auto-detected limits (WithMaxWatches(0)); explicit limits
// set via WithMaxWatches(n) are used as-is.
//
// Default: 1.0 (full system limit). Recommended for shared environments: 0.75.
func WithMaxWatchesSafetyFraction(fraction float64) Option {
	return func(w *Watcher) {
		if fraction <= 0 || fraction > 1.0 {
			fraction = 1.0
		}

		w.maxWatchesFraction = fraction
	}
}

// WithContentHashing enables SHA-256 content hashing of file events.
// When enabled, Event.Hash is populated with the hex-encoded SHA-256 digest
// of the file content for Create and Write events on regular files. Hashing
// is skipped for directories, removed files, and files larger than the max
// size (default 10 MiB, configurable via WithContentHashMaxSize).
//
// Performance: each hashed event requires reading the entire file. This adds
// filesystem I/O proportional to file size. Use this option when content
// verification is needed (e.g., cache invalidation, deduplication) and
// disable it for high-throughput scenarios.
func WithContentHashing() Option {
	return func(w *Watcher) {
		if w.contentHashMaxSize == 0 {
			w.contentHashMaxSize = defaultContentHashMaxSize
		}
	}
}

// WithContentHashMaxSize sets the maximum file size for content hashing.
// Files larger than this are skipped to avoid blocking the event loop on I/O.
// This option implicitly enables content hashing if not already enabled via
// WithContentHashing. Set to 0 to disable content hashing entirely.
//
// Default is 10 MiB (10 * 1024 * 1024 bytes).
func WithContentHashMaxSize(bytes int64) Option {
	return func(w *Watcher) {
		w.contentHashMaxSize = bytes
	}
}

// WithSelfHeal enables automatic retry of failed watch registrations.
// When a path fails to be added to the underlying fsnotify watcher (e.g.,
// due to inotify resource exhaustion / ENOSPC), the watcher will periodically
// retry the failed path at the given interval until it succeeds or the
// watcher is closed. The interval must be > 0; a value of 0 is ignored.
//
// This is useful for long-running watchers that may encounter transient
// resource exhaustion when directories are created faster than watches
// can be added. Failed paths that succeed on retry are added to the watch
// list normally and will receive subsequent fsnotify events.
func WithSelfHeal(interval time.Duration) Option {
	return func(w *Watcher) {
		if interval > 0 {
			w.selfHealInterval = interval
		}
	}
}

// WithCleanup registers a cleanup function that is called when the watcher's
// Close() method is invoked, after all goroutines and channels are torn down.
// Use this to release resources held by middleware (e.g., file handles from
// NewFileLogMiddleware). Cleanup functions are called in registration order
// and are cleared on Reset().
func WithCleanup(fn func() error) Option {
	return func(w *Watcher) {
		w.cleanups = append(w.cleanups, fn)
	}
}

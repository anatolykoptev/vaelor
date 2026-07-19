# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/).

## [2.2.0] - 2026-06-03

### Added

- `WithGitignore(enabled bool)` option — `.gitignore`-aware directory walking, enabled by default
- `WithExcludePaths(paths...)` option — exclude absolute paths during walk (prefix matching, skips subdirectories)
- `WithMaxWatches(n int)` option — override inotify watch budget; auto-detected from `/proc/sys/fs/inotify/max_user_watches` on Linux
- `WithContentHashing()` option — SHA-256 content hash in `Event.Hash` field (opt-in, capped at 10 MiB)
- `WithSelfHeal(interval)` option — self-healing watcher that auto-retries failed watch paths at configurable intervals
- `FilterGitignore(repoRoot string)` filter — match files against `.gitignore` patterns from a repository root
- `FilterWithMeta` type, `MatchResult` struct, `FilterFromWithMeta()`, `FilterWithMetaAnd/Or/Not()`, `WithMeta()` wrapper — filter functions that return match metadata
- `MiddlewareExponentialBackoff()` — configurable exponential backoff for event processing with initial/max intervals
- `PrometheusCollector` — zero-dependency Prometheus collector with `StatsFunc`, `CounterMetric`, `GaugeMetric` interfaces
- `OTelMiddleware` — zero-dependency OpenTelemetry tracing middleware with `OTelSpan` interface
- `Event.Hash` field for content hash metadata
- `Watcher.Reset()` method — clears runtime state while preserving configuration (filters, middleware, debounce, options)
- `Remove()` now cleans up all subdirectory watches under the given path to prevent watch leaks
- `Stats.WatchErrors` — tracks how many paths failed to add during walk
- `Stats.WatchLimit` and `Stats.WatchBudgetUsed` — track inotify budget usage
- Batched watch registration — directories collected during walk and added in batches of 1000 with `runtime.Gosched()` between batches
- Graceful ENOSPC handling — `fswatcher.Add()` errors no longer abort the entire walk; errors are logged and walking continues in degraded mode
- 7 new godoc examples: `ExampleWatcher_Add`, `ExampleWatcher_Errors`, `ExampleWithErrorHandler`, `ExampleWithDebug`, `ExampleFilterMinSize`, `ExampleOp`, `ExampleOp_MarshalJSON`
- CI: `examples-build` job and benchmark artifact upload for regression detection

### Changed

- `MiddlewareRateLimit` now delegates to `MiddlewareThrottle` internally, eliminating duplicate rate-limiting logic
- `addPath` unified into a single code path — eliminated `walkDirFunc` duplication for recursive and non-recursive walks
- Generic `makeSetFilter[T]` replaces duplicate extension and operation filter factories
- Shared `hashFile` function consolidates SHA-256 logic from filter and self-heal modules

### Fixed

- `.gitignore` matching now correctly handles ancestor-only patterns and avoids double `os.Stat` calls
- `maxWatches` budget check applied to all direct `addPath` calls, not just walked directories
- Gitignore, exclude paths, watch budget, and ENOSPC handling now apply to `addPathWithDepth` for nested directories
- Duplicate root paths no longer appear in `WatchList()` after recursive walks

### Dependencies

- `github.com/sabhiram/go-gitignore` added for `.gitignore` pattern matching (zero transitive deps)
- `github.com/LarsArtmann/gogenfilter` updated to v3.0.3

## [2.1.0] - 2026-06-01

### Added

- `Event.Size` and `Event.ModTime` fields for file metadata in events
- `WithPolling()` and `WithPollInterval()` options for NFS/FUSE filesystem support
- `WithDebug()` option with structured `slog` debug logging throughout the pipeline
- `ErrorCode` typed constants for programmatic error matching (`WATCHER_CLOSED`, `PATH_NOT_FOUND`, etc.)
- `WatcherError.Stack` — auto-captured `debug.Stack()` traces for debugging
- `FilterContentHash()` filter for SHA-256 content-change detection
- `MiddlewareCircuitBreaker()` — fault tolerance with closed/open/half-open states
- `MiddlewareThrottle()` — fixed-window rate limiting
- `MiddlewareWriteFileLog()` — audit trail to filesystem
- `MiddlewareErrorSanitization()` — safe error message scrubbing preserving error chains
- `MiddlewareErrorRateLimit()` — per-error-type rate limiting
- `MiddlewareErrorRecovery()` — recoverable error handling with custom strategies
- `MiddlewareErrorCorrelation()` — request tracing across handler chains
- `MiddlewareErrorBatch()` — batch error flushing for analytics
- `WithFollowSymlinks()` option for symbolic link traversal during directory walking
- Fuzz tests for `ParseFamily`, `Classify`, and error formatting
- `.goreleaser.yml` configuration for cross-platform release artifacts
- `API_STABILITY.md` — public API stability policy and guarantees
- `Troubleshooting.md` — common issues and resolutions
- `CODE_OF_CONDUCT.md` — contributor code of conduct
- `docs/research/` directory for architecture and adoption research

### Changed

- Go version bumped to 1.26.3 (was 1.26.2)
- `Event.ModTime` JSON tag changed from `omitempty` to `omitzero` for correct zero-time handling
- `Event` `LogValue` now includes `size` and `modTime` fields
- Refactored `copyWatchList()` helper, eliminating 3× lock+copy duplication in path management
- Refactored `ErrorCategory.String()` to inline string constants, removing redundant `categoryStr` variables
- Streamlined nolint directives and extracted magic strings to named constants across the codebase
- Project documentation and configuration files reorganized

### Fixed

- `WatchOnce()` double `%w` formatting caused silent error dropping
- `MiddlewareErrorSanitization` now preserves error chains via `%w` instead of discarding them
- `categorizeError()` was missing several sentinel errors in its classification mapping
- `OpString` receiver variable shadowed the `os` package import
- `Event.ModTime` JSON serialization now correctly distinguishes zero values
- CI: aligned `golangci-lint-action` to v7
- CI: added `go vet` and `go fmt` checks to the lint workflow

### Deprecated

- `WithWatchedIgnoreDirs()` — superseded by `WithIgnoreDirs()`. Will be removed in v3.0.0.

## [2.0.0] - 2026-05-23

### Added

- `DefaultIgnoreDirsCopy()` function for safe access without mutation risk
- Debounce option validation: panics on negative durations
- `Errors() <-chan error` method for channel-based error consumption
- `IsWatching()` and `IsClosed()` state inspection methods
- `WithLazyIsDir()` option to skip `os.Stat` calls for performance
- `WithOnAdd()` callback option for path tracking
- `WithOnError()` simplified error callback option
- `FilterMaxSize()`, `FilterMinAge()`, `FilterModifiedSince()` filters
- `MiddlewareDeduplicate()`, `MiddlewareBatch()`, `MiddlewareSlidingWindowRateLimit()`
- `FilterGeneratedCode()`, `FilterGeneratedCodeFull()` via gogenfilter integration
- Compile-time phantom types for `EventPath`, `RootPath`, `DebounceKey`, `OpString`
- `Event.GetPath()` returning phantom-typed `EventPath`
- `slog.LogValuer` on `Event` for structured logging
- 15 new tests covering rename events, multi-directory init, concurrent ops, state transitions

### Changed

- **BREAKING**: Relicensed from Proprietary to MIT
- **BREAKING**: Module path changed to `github.com/larsartmann/go-filewatcher/v2`
- Replaced hand-rolled `Op.MarshalJSON` with `json.Marshal` for robustness
- Modernized `errors.As` to Go 1.26 `AsType` pattern
- `testing_helpers.go` renamed to `testing_helpers_test.go` (no longer ships to consumers)
- `flake.nix` Go version aligned to 1.26 (was 1.24)
- `FilterExcludePaths` no longer calls `filepath.Abs` per event
- `WithBuffer(0)` now allowed with documented caveat

### Fixed

- `Add()` no longer double-appends to `WatchList()` in recursive mode
- `MiddlewareBatch` timer-triggered flush errors now logged via `slog.Error` instead of silently dropped
- `handleNewDirectory` now propagates `addPath` errors to the error handler
- `MiddlewareSlidingWindowRateLimit` uses in-place slice compaction instead of per-event allocation

### Removed

- 306 lines of test-only code from production binary

## [0.2.0] - 2026-04-23

### Added

- `go-branded-id` integration for compile-time phantom type safety
- `FilterGeneratedCode()` and `FilterGeneratedCodeFull()` via gogenfilter v0.2.0
- `OpString`, `LogSubstring`, `TempDir`, `DebounceKey`, `RootPath` phantom types
- Extracted shared test helper functions for DRY
- Benchmark suite migrated to `b.Loop()` pattern

### Changed

- Migrated to gogenfilter v0.2.0 API
- Updated flake.lock to nixpkgs eb3b085

### Fixed

- Data race between `Close()` and `buildEmitFunc`
- Data race between `Close()` and debouncer callbacks
- fsnotify assertion tests tolerant of duplicate events

## [0.1.0] - 2026-04-04

### Added

- Core watcher: `New()`, `Watch(ctx)→<-chan Event`, `Add()`, `Remove()`, `WatchList()`, `Stats()`, `Close()`
- 14 functional options: debounce, per-path debounce, filter, extensions, ignore dirs, ignore hidden, recursive, middleware, error handler, skip dot dirs, buffer, on add, on error, lazy is dir
- 13 composable filters: Extensions, IgnoreExtensions, IgnoreDirs, ExcludePaths, IgnoreHidden, Operations, NotOperations, Glob, Regex, MinSize, MaxSize, MinAge, ModifiedSince
- Filter combinators: `FilterAnd`, `FilterOr`, `FilterNot`
- 10 middleware: Logging, Recovery, Filter, OnError, RateLimit, SlidingWindowRateLimit, Metrics, Deduplicate, Batch, WriteFileLog
- Per-key `Debouncer` and `GlobalDebouncer` with Flush/Pending/Stop
- 10 sentinel errors with structured `WatcherError` (transient/permanent categorization)
- `Errors() <-chan error` for channel-based error consumption
- `IsWatching()` and `IsClosed()` state inspection
- Channel-based event streaming with context cancellation
- Automatic recursive directory watching with dynamic new-dir detection
- `MiddlewareLogging` accepts `*slog.Logger` for structured logging
- `slog.LogValuer` on `Event` type
- JSON marshaling for `Op` and `Event` types
- Benchmarks for creation, filters, middleware, debounce, full pipeline
- GitHub Actions CI (test with race + 90% threshold, lint with 90+ rules)
- Nix flake dev shell for 4 platforms
- Comprehensive documentation: README, ARCHITECTURE.md, MIGRATION.md, examples

### Changed

- Replaced `cockroachdb/errors` with stdlib (eliminated 39 transitive dependencies)
- Split `watcher.go` into `watcher.go`, `watcher_internal.go`, `watcher_walk.go`

### Removed

- `cockroachdb/errors` dependency
- Dead artifacts: `report/jscpd-report.json`, empty `pkg/` directory

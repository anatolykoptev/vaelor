# Feature Inventory

**Last Updated:** 2026-10-07 · **Version:** v2.4.1

Honest status of every capability in go-filewatcher. Statuses:

- ✅ **DONE** — Production-ready, tested, documented
- 🟡 **PARTIALLY DONE** — Works but incomplete, rough edges, or limited docs
- 🔵 **PLANNED** — Committed to in TODO_LIST.md, not yet started
- ⚪ **WORTH CONSIDERING** — Ideas worth exploring; no commitment

---

## Core Watching

| Feature                           | Status | Notes                                                                |
| --------------------------------- | ------ | -------------------------------------------------------------------- |
| Create watcher from paths         | ✅     | `New(paths, opts...)` validates paths exist and are directories      |
| Start/stop with `context.Context` | ✅     | `Watch(ctx)` returns `<-chan Event`, channel closes on cancel/Close  |
| One-shot mode                     | ✅     | `WatchOnce(ctx)` returns the first event and closes                  |
| Recursive directory watching      | ✅     | On by default; `WithRecursive(false)` disables                       |
| Selective recursion depth         | ✅     | `AddRecursive(path, maxDepth)` — 0=flat, -1=full, N=depth-limited    |
| Dynamic path management           | ✅     | `Add`, `Remove` (subtree-aware), `WatchList`                         |
| Reset without rebuilding config   | ✅     | `Reset()` clears runtime state, preserves filters/middleware/options |
| Thread-safe concurrent access     | ✅     | All public methods documented safe-by-design; tested with `-race`    |
| Graceful close                    | ✅     | `Close()` idempotent; stops debouncer before closing channels        |

## Filtering

| Feature                         | Status | Notes                                                                                                                                                            |
| ------------------------------- | ------ | ---------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Extensions / IgnoreExtensions   | ✅     | Dot-prefixed                                                                                                                                                     |
| IgnoreDirs / ExcludePaths       | ✅     | Name-based (`WithIgnoreDirs`, `DefaultIgnoreDirs` / `DefaultIgnoreDirsCopy`) vs absolute-path prefix matching (`WithExcludePaths`)                               |
| IgnoreHidden                    | ✅     | Dot-prefixed files/dirs                                                                                                                                          |
| Operations / NotOperations      | ✅     | By `Op` enum                                                                                                                                                     |
| Glob / Regex                    | ✅     | Filename glob, full-path regex                                                                                                                                   |
| MinSize / MaxSize               | ✅     | Bytes                                                                                                                                                            |
| MinAge / ModifiedSince          | ✅     | Time-based                                                                                                                                                       |
| IgnoreGlobs (patterns)          | ✅     | `WithIgnorePatterns` option                                                                                                                                      |
| ContentHash                     | ✅     | `FilterContentHash` + `ContentCheckMode`; `WithContentHashing()` — SHA-256                                                                                       |
| Gitignore repository matcher    | ✅     | `FilterGitignore(repoRoot)` — event-time check against .gitignore                                                                                                |
| Generated-code detection        | ✅     | sqlc, protobuf, templ, mockgen, stringer via `NewGeneratedCodeDetector` + gogenfilter v3.6.1 (sqlc: `*.sql.go` by filename, `models.go` only via content marker) |
| Generated-code full content check | ✅   | `FilterGeneratedCodeFull(mode, options...)` — `ContentCheckEnabled` reads files for `DO NOT EDIT` markers (catches weak sqlc filenames), `ContentCheckDisabled` is filename-only with zero I/O |
| Generated-code custom instance  | ✅     | `FilterGeneratedCodeWithFilter(genFilter)` — wraps your own `gogenfilter.Filter` for custom filesystems and include/exclude patterns                        |
| Filter combinators (AND/OR/NOT) | ✅     | `FilterAnd`, `FilterOr`, `FilterNot`                                                                                                                             |
| Case-insensitive filter wrapper | ✅     | `FilterCaseInsensitive(inner)` — lowercases + NFC-normalizes the event path before delegating to the inner filter                                                |
| Case-sensitive filter wrapper   | ✅     | `FilterCaseSensitive(inner)` — NFC-normalizes without case-folding (useful on macOS NFD paths)                                                                   |
| Case-insensitive dirs filter    | ✅     | `FilterIgnoreDirsCaseInsensitive(dirs...)` — matches dir names case-insensitively (NTFS, APFS)                                                                   |
| Metadata-returning filters      | ✅     | `FilterWithMeta`, `MatchResult`, `FilterWithMetaAnd`/`FilterWithMetaOr`/`FilterWithMetaNot`, `FilterFromWithMeta`                                                |

## Middleware

| Feature                        | Status | Notes                                                                                                                       |
| ------------------------------ | ------ | --------------------------------------------------------------------------------------------------------------------------- |
| Logging (slog)                 | ✅     | `MiddlewareLogging(*slog.Logger)`                                                                                           |
| Panic recovery                 | ✅     | `MiddlewareRecovery()`                                                                                                      |
| Filter-as-middleware           | ✅     | `MiddlewareFilter(Filter)`                                                                                                  |
| OnError handling               | ✅     | `MiddlewareOnError(func(Event, error))`                                                                                     |
| Rate limiting (fixed)          | ✅     | `MiddlewareRateLimit(maxEvents)`                                                                                            |
| Rate limiting (sliding)        | ✅     | `MiddlewareSlidingWindowRateLimit(maxEvents, window)`                                                                       |
| Throttle (token bucket)        | ✅     | `MiddlewareThrottle(maxEvents, burst)` via `golang.org/x/time`                                                              |
| Metrics counter                | ✅     | `MiddlewareMetrics(func(Op))`                                                                                               |
| Deduplicate                    | ✅     | `MiddlewareDeduplicate(window)` — NFC-normalized keys                                                                       |
| Deduplicate (case-insensitive) | ✅     | `MiddlewareDeduplicateCaseInsensitive(window)` — lowercased + NFC keys for case-insensitive filesystems                     |
| Batch                          | ✅     | `MiddlewareBatch(window, maxSize, flush)`                                                                                   |
| Audit to file                  | ✅     | `MiddlewareWriteFileLog(path)` or `NewFileLogMiddleware` (returns closer for fd cleanup via `WithCleanup`)                  |
| Circuit breaker                | ✅     | `MiddlewareCircuitBreaker(maxFailures, resetTimeout)`; `CircuitState` enum: `CircuitClosed`→`CircuitOpen`→`CircuitHalfOpen` |
| Exponential backoff            | ✅     | `MiddlewareExponentialBackoff(maxFailures, initial, max)`                                                                   |
| Error rate limit               | ✅     | `MiddlewareErrorRateLimit(maxErrors, window)`                                                                               |
| Error recovery strategy        | ✅     | `MiddlewareErrorRecovery(strategy)`                                                                                         |
| Error correlation IDs          | ✅     | `MiddlewareErrorCorrelation(idGenerator)`                                                                                   |
| Error sanitization             | ✅     | `MiddlewareErrorSanitization(sanitize)`                                                                                     |
| Error batching                 | ✅     | `MiddlewareErrorBatch(window, maxSize, flush)`                                                                              |

## Debouncing

| Feature                | Status | Notes                                                                                  |
| ---------------------- | ------ | -------------------------------------------------------------------------------------- |
| Global debounce        | ✅     | `WithDebounce(d)` — all events coalesced                                               |
| Per-path debounce      | ✅     | `WithPerPathDebounce(d)` — independent per file                                        |
| Programmatic debouncer | ✅     | `NewDebouncer` / `NewGlobalDebouncer`; `DebouncerInterface` for custom implementations |

## Observability

| Feature                          | Status | Notes                                                                                                                          |
| -------------------------------- | ------ | ------------------------------------------------------------------------------------------------------------------------------ |
| `Stats()` struct                 | ✅     | Events, filters, middleware drops, backpressure drops, error drops, uptime, watch budget, budget cap                           |
| Middleware drop counter          | ✅     | `Stats.EventsDroppedByMiddleware` — surfaces rate-limit/dedup/circuit-breaker drops                                            |
| Backpressure drop counter        | ✅     | `Stats.EventsDroppedByBackpressure` — events dropped in DropOnFull mode                                                        |
| Error drop counter               | ✅     | `Stats.ErrorsDropped` — errors dropped when error channel is full                                                              |
| Budget cap                       | ✅     | `Stats.WatchBudgetCap` — effective cap after safety fraction; `WatchLimit` shows raw system limit                              |
| Structured debug logging         | ✅     | `WithDebug(*slog.Logger)`                                                                                                      |
| Prometheus collector             | ✅     | `PrometheusCollector` with `StatsFunc`, `CounterMetric`, `GaugeMetric` interfaces (includes backpressure + budget cap metrics) |
| OpenTelemetry tracing middleware | ✅     | `OTelMiddleware` with `OTelSpan` interface (zero-dep)                                                                          |
| Stack traces on errors           | ✅     | `WatcherError.Stack` via `debug.Stack()`; `NewWatcherErrorWithStack` for caller-provided stacks                                |

## Resilience & Scalability

| Feature                                  | Status | Notes                                                                                                                                     |
| ---------------------------------------- | ------ | ----------------------------------------------------------------------------------------------------------------------------------------- |
| Graceful ENOSPC handling                 | ✅     | Add errors logged, walk continues, `Stats.WatchErrors` tracks fails                                                                       |
| Inotify budget awareness                 | ✅     | Auto-detected from `/proc/sys/fs/inotify/max_user_watches`                                                                                |
| Watch limit override                     | ✅     | `WithMaxWatches(n)`                                                                                                                       |
| Watch budget safety fraction             | ✅     | `WithMaxWatchesSafetyFraction(0.75)` — leaves headroom on shared machines; only affects auto-detected limits                              |
| Slow-consumer backpressure (DropOnFull)  | ✅     | `WithEventChannelMode(EventChannelDropOnFull)` drops events when channel is full instead of blocking; counted in `Stats`                  |
| Filtered directory watching control      | ✅     | `WithWatchFilteredDirectories(false)` prevents new dirs whose Create event was filtered from being watched                                |
| Symlink cycle detection                  | ✅     | `WithFollowSymlinks(true)` detects and skips cycles via `symlinkVisited` map                                                              |
| Path validation (fail-fast)              | ✅     | `Add`/`AddRecursive`/`Watch` return `ErrPathNotFound` immediately for non-existent paths                                                  |
| Syscall error classification             | ✅     | `os.ErrPermission`/`os.ErrNotExist`/`syscall.ENOTDIR` → permanent; `ENOSPC` → transient; self-heal abandons permanent failures            |
| Polling mode respects exclusions         | ✅     | `pollWalkDir` now applies `shouldExcludePath` and `.gitignore` matching (was only checking `shouldSkipDir`)                               |
| Case-insensitive walk-time dir skipping  | ✅     | `shouldSkipDir` matches directory names case-insensitively on `CaseInsensitive` filesystems                                               |
| Configurable content hash size           | ✅     | `WithContentHashMaxSize(bytes)` — max file size for content hashing (default 10 MiB)                                                      |
| Decoupled error channel buffer           | ✅     | `WithErrorBufferSize(n)` — independent error channel capacity from event channel                                                          |
| Self-healing watches                     | ✅     | `WithSelfHeal(interval)` retries failed paths                                                                                             |
| Batched watch registration               | ✅     | 1000 dirs/batch with `runtime.Gosched()` between batches                                                                                  |
| Polling mode (NFS/FUSE)                  | ✅     | `WithPolling(true)` + `WithPollInterval(d)`                                                                                               |
| Symlink following                        | ✅     | `WithFollowSymlinks(true)`                                                                                                                |
| .gitignore-aware walking                 | ✅     | `WithGitignore(true)` (default) skips gitignored dirs at walk time. Note: go-gitignore trailing-slash limitation — see Troubleshooting.md |
| Path-level exclusions                    | ✅     | `WithExcludePaths(paths...)` prefix-matches during walk                                                                                   |
| Filesystem case-sensitivity awareness    | ✅     | `WithCaseSensitivity(mode)` — auto/sensitive/insensitive; dedup, Remove, debounce, exclude, poll loop, gitignore all case-aware           |
| NFC Unicode normalization                | ✅     | `pathKey()` applies `norm.NFC.String()` — fixes macOS NFD/NFC path mismatch on all non-ASCII filenames                                    |
| Effective case-sensitivity query         | ✅     | `EffectiveCaseSensitivity()` — returns resolved enum without full `Stats()` call. Also via `Stats.CaseSensitivityMode` (type-safe enum)   |
| O(1) watched-path lookup & deduplication | ✅     | `watchListKeys` set prevents duplicate watch registration                                                                                 |

## Event Metadata

| Feature              | Status | Notes                                                    |
| -------------------- | ------ | -------------------------------------------------------- |
| Path, Op, Timestamp  | ✅     | Core fields                                              |
| IsDir, Size, ModTime | ✅     | Populated from `os.Stat`                                 |
| Content hash         | ✅     | `WithContentHashing()` — SHA-256, capped 10 MiB          |
| JSON marshaling      | ✅     | `Op` and `Event` implement `MarshalJSON`/`UnmarshalJSON` |
| Text marshaling      | ✅     | `Op` implements `MarshalText`/`UnmarshalText`            |
| `slog.LogValuer`     | ✅     | `Event` integrates with structured logging               |

## Type Safety (Phantom Types)

| Feature                   | Status | Notes                                                             |
| ------------------------- | ------ | ----------------------------------------------------------------- |
| `EventPath`               | ✅     | `Event.GetPath()` returns typed path with `.Base/.Dir/.Ext/.Join` |
| `RootPath`                | ✅     | Walking roots                                                     |
| `DebounceKey`             | ✅     | Debouncer keys                                                    |
| `OpString`                | ✅     | Operation names on `WatcherError`                                 |
| `LogSubstring`, `TempDir` | ✅     | Test-only assertion helpers                                       |

## Error Handling

| Feature                       | Status | Notes                                                                                                                              |
| ----------------------------- | ------ | ---------------------------------------------------------------------------------------------------------------------------------- |
| Sentinel errors               | ✅     | `ErrWatcherClosed`, `ErrNoPaths`, `ErrPathNotFound`, ...                                                                           |
| Typed error codes             | ✅     | `ErrorCode` constants for programmatic matching                                                                                    |
| Error category classification | ✅     | `ErrorCategory`: `CategoryTransient` (retryable) vs `CategoryPermanent` (abandon); `IsPermanentError` / `IsTransientError` helpers |
| Structured `WatcherError`     | ✅     | Category (transient/permanent), op, stack trace; `NewWatcherError` constructor                                                     |
| Channel-based error stream    | ✅     | `Errors() <-chan error`                                                                                                            |
| `ErrorHandler` type           | ✅     | `func(ctx, Event, ErrorContext)` — full error context via `WithErrorHandler`                                                       |
| Batch error aggregation       | ✅     | `BatchError` type from `MiddlewareErrorBatch`                                                                                      |
| Custom error handler callback | ✅     | `WithErrorHandler` / `WithOnError` (deprecated)                                                                                    |

## Developer Experience

| Feature                            | Status | Notes                                                                                                                                                                                                                                                                   |
| ---------------------------------- | ------ | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Nix flake dev shell                | ✅     | `nix develop`, `direnv allow`                                                                                                                                                                                                                                           |
| Nix apps for all common commands   | ✅     | `nix run .#{check,ci,test,lint,lint-fix,bench,coverage,...}`                                                                                                                                                                                                            |
| GitHub Actions CI                  | ✅     | Test with race + 90% threshold, lint, examples-build, bench                                                                                                                                                                                                             |
| Documentation website              | ✅     | Astro + Starlight site at `filewatcher.lars.software`                                                                                                                                                                                                                   |
| Godoc examples                     | ✅     | 30 examples in `example_test.go`                                                                                                                                                                                                                                        |
| Error simulation testing framework | ✅     | `error_simulation_test.go` + `fake_backend_test.go` — scripted Add failures, error injection, full pipeline tests                                                                                                                                                       |
| Runnable example programs          | ✅     | `examples/{basic,middleware,per-path-debounce,demo,filter-generated}`                                                                                                                                                                                                   |
| Cross-platform releases            | 🟡     | `release.yml` triggers on `v*` tags (tests + lint + GitHub Release with auto-generated notes); release-please automates versioning from conventional commits. `.goreleaser.yml` exists but is NOT invoked — no compiled binaries shipped (see TODO_LIST open questions) |
| Issue templates                    | ✅     | Bug report + feature request                                                                                                                                                                                                                                            |

## Planned / Worth Considering

See [ROADMAP.md](./ROADMAP.md) for long-term direction and [TODO_LIST.md](./TODO_LIST.md) for committed short/mid-term work. Highlights:

| Feature                                         | Status | Notes                                                                                                                                                |
| ----------------------------------------------- | ------ | ---------------------------------------------------------------------------------------------------------------------------------------------------- |
| Windows-specific edge case tests                | 🔵     | Currently CI runs Linux only                                                                                                                         |
| Fuzz testing expansion                          | 🔵     | Existing fuzz tests; expand to more surfaces                                                                                                         |
| Semantic-release automation                     | ✅     | release-please wired in (`.github/workflows/release-please.yml`) + commitlint gate; see [evaluation](./docs/research/semantic-release-evaluation.md) |
| Localizable error messages                      | ⚪     | Sentinel errors are English-only today                                                                                                               |
| fsnotify v2 tracking                            | ⚪     | Monitor upstream for breaking changes                                                                                                                |
| `WatchChanges(ctx, targetState)` idempotent API | ⚪     | For sync-style workflows; see [contract sketch](./docs/research/watchchanges-contract.md)                                                            |

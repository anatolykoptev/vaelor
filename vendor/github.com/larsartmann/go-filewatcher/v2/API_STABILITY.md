# API Stability Policy

## Versioning

go-filewatcher follows [Semantic Versioning 2.0.0](https://semver.org/).

Given a version `MAJOR.MINOR.PATCH`:

- **MAJOR**: Breaking API changes (e.g., removed methods, changed signatures)
- **MINOR**: New features added in a backward-compatible manner
- **PATCH**: Bug fixes that are backward-compatible

## Stability Guarantees

### Stable APIs (safe for production use)

These types and functions have strong backward-compatibility guarantees:

| Category | Symbols | Status |
| -------- | ------- | ------ |

| Constructor | `New()` | **Stable** |
| Options | `WithDebounce`, `WithPerPathDebounce`, `WithFilter`, `WithExtensions`, `WithIgnoreDirs`, `WithIgnoreHidden`, `WithRecursive`, `WithMiddleware`, `WithBuffer`, `WithErrorHandler`, `WithSkipDotDirs`, `WithOnAdd`, `WithLazyIsDir`, `WithIgnorePatterns`, `WithGitignore`, `WithExcludePaths`, `WithMaxWatches` | **Stable** |
| Filters | `FilterExtensions`, `FilterIgnoreDirs`, `FilterIgnoreHidden`, `FilterGlob`, `FilterRegex`, `FilterOperations`, `FilterMinSize`, `FilterMaxSize`, `FilterMinAge`, `FilterMaxAge`, `FilterModifiedSince`, `FilterExcludePaths`, `FilterIgnoreExtensions`, `FilterNotOperations`, `FilterIgnoreGlobs`, `FilterGitignore` | **Stable** |
| Middleware | `MiddlewareLogging`, `MiddlewareRecovery`, `MiddlewareFilter`, `MiddlewareOnError`, `MiddlewareSlidingWindowRateLimit`, `MiddlewareMetrics`, `MiddlewareDeduplicate`, `MiddlewareThrottle`, `MiddlewareWriteFileLog`, `MiddlewareExponentialBackoff` | **Stable** |
| Methods | `Watch()`, `WatchOnce()`, `Add()`, `Remove()`, `Close()`, `WatchList()`, `Stats()`, `IsClosed()`, `IsWatching()`, `Errors()`, `Reset()` | **Stable** |
| Errors | `WatcherError`, `ErrorCategory`, all sentinel errors | **Stable** |
| Handlers | `Handler`, `ErrorHandler`, `ErrorContext` | **Stable** |
| Observability | `PrometheusCollector`, `NewPrometheusCollector`, `StatsFunc`, `CounterMetric`, `GaugeMetric`, `Attribute` | **Stable** |

### Evolving APIs (may change between minor versions)

These APIs work as documented but may have behavioral changes:

| Category   | Symbols                                                                                                                                                                                                                                                                                                                                           | Status       |
| ---------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------ |
| Features   | `WithPolling`, `WithPollInterval`, `WithDebug`, `WithFollowSymlinks`, `WithSelfHeal`, `WithContentHashing`, `WithContentHashMaxSize`, `WithCleanup`, `WithCaseSensitivity`, `EffectiveCaseSensitivity`, `WithMaxWatchesSafetyFraction`, `WithWatchFilteredDirectories`, `WithEventChannelMode`, `WithErrorBufferSize`, `NewWatcherErrorWithStack` | **Evolving** |
| Debouncer  | `GlobalDebouncer`, `Debouncer`, `DebounceKey`                                                                                                                                                                                                                                                                                                     | **Evolving** |
| Types      | `EventPath`, `OpString`, `DebounceKey`, `RootPath`, `OTelSpan`, `EventChannelMode`                                                                                                                                                                                                                                                                | **Evolving** |
| Filters    | `FilterContentHash`, `FilterWithMeta`, `FilterFromWithMeta`, `FilterWithMetaAnd`, `FilterWithMetaOr`, `FilterWithMetaNot`, `WithMeta`, `MatchResult`, `FilterCaseInsensitive`, `FilterCaseSensitive`, `FilterIgnoreDirsCaseInsensitive`                                                                                                           | **Evolving** |
| Middleware | `MiddlewareCircuitBreaker`, `MiddlewareThrottle`, `MiddlewareErrorSanitization`, `MiddlewareErrorRateLimit`, `MiddlewareErrorRecovery`, `MiddlewareErrorCorrelation`, `MiddlewareErrorBatch`, `OTelMiddleware`, `NewFileLogMiddleware`, `MiddlewareDeduplicateCaseInsensitive`                                                                    | **Evolving** |

Evolving APIs will not be removed without a deprecation period of at least
one minor version.

### Experimental APIs (may change at any time)

None currently.

### Deprecated APIs (will be removed in v3.0.0)

These APIs continue to work but are scheduled for removal in the next major
version. Each is a strict superset duplicate of a more capable alternative.

| Category   | Symbols                 | Status         | Superseded by                           | Migration                                                                                                            |
| ---------- | ----------------------- | -------------- | --------------------------------------- | -------------------------------------------------------------------------------------------------------------------- |
| Options    | `WithWatchedIgnoreDirs` | **Deprecated** | `WithFilter(FilterIgnoreDirs(dirs...))` | Replace the option call with `WithFilter(FilterIgnoreDirs(...))`.                                                    |
| Options    | `WithOnError`           | **Deprecated** | `WithErrorHandler`                      | Wrap the callback: `WithErrorHandler(func(_ ErrorContext, err error) { fn(err) })`. Gains full `ErrorContext`.       |
| Middleware | `MiddlewareRateLimit`   | **Deprecated** | `MiddlewareThrottle`                    | `MiddlewareRateLimit(n)` is exactly `MiddlewareThrottle(n, n)`. Use `MiddlewareThrottle(maxEvents, burst)` directly. |

### v3 Removal Candidates (under consideration, not yet deprecated)

The following are **not** deprecated yet but are being evaluated for change or
removal in v3. They are listed here so the v3 scope is visible before cutting.
None of these will be removed without first receiving a `// Deprecated:` comment
for at least one minor version.

| Category   | Symbol                                                                       | Concern                                                                                                               | Tentative v3 resolution                                                                                         |
| ---------- | ---------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------- |
| Middleware | `MiddlewareOnError`                                                          | Overlaps conceptually with `WithErrorHandler`, but operates at a different layer (handler errors vs. watcher errors). | Keep — the layer distinction is meaningful. Document the boundary clearly rather than merge.                    |
| Middleware | `MiddlewareSlidingWindowRateLimit`                                           | Third rate limiter alongside `MiddlewareThrottle`.                                                                    | Keep — per-arbitrary-window semantics differ from per-second token bucket. Consider better doc differentiation. |
| Options    | `WithExtensions`, `WithIgnoreDirs`, `WithIgnoreHidden`, `WithIgnorePatterns` | Thin convenience wrappers over `WithFilter(FilterX(...))`.                                                            | Keep — intentional boilerplate reducers; widely used. Not deprecation candidates.                               |
| Types      | `ErrorHandler` (two-arg signature)                                           | `func(ctx ErrorContext, err error)` — richer than the simple `func(error)`.                                           | Keep — the context is valuable; `WithOnError` (the simple form) is deprecated instead.                          |

## Breaking Change Policy

A **breaking change** is any change that requires users to modify their code
when upgrading. This includes:

- Removing or renaming exported types, functions, or methods
- Changing function signatures
- Changing struct field types
- Changing behavior that user code depends on in a way that breaks existing usage

### Process for Breaking Changes

1. **Deprecation**: Mark the old API with a deprecation comment for at least
   one minor version.
2. **Migration guide**: Document how to migrate in `MIGRATION.md`.
3. **Major version bump**: Only remove deprecated APIs in a major version.

### Exceptions

The following are NOT considered breaking changes:

- Adding new fields to structs (users should not rely on exact struct layout)
- Adding new values to enums/constants
- Changing internal/unexported implementation details
- Bug fixes that change incorrect behavior

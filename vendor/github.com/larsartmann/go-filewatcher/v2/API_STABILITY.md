# API Stability Policy

**Last Updated:** 2026-06-03

## Versioning

go-filewatcher follows [Semantic Versioning 2.0.0](https://semver.org/).

Given a version `MAJOR.MINOR.PATCH`:

- **MAJOR**: Breaking API changes (e.g., removed methods, changed signatures)
- **MINOR**: New features added in a backward-compatible manner
- **PATCH**: Bug fixes that are backward-compatible

## Stability Guarantees

### Stable APIs (safe for production use)

These types and functions have strong backward-compatibility guarantees:

| Category    | Symbols                                                                                                                                                                                                                                                                                            | Status     |
| ----------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------- |
| Core types  | `Event`, `Op`, `Watcher`, `Filter`                                                                                                                                                                                                                                                                 | **Stable** |
| Constructor | `New()`                                                                                                                                                                                                                                                                                            | **Stable** |
| Options     | `WithDebounce`, `WithPerPathDebounce`, `WithFilter`, `WithExtensions`, `WithIgnoreDirs`, `WithIgnoreHidden`, `WithRecursive`, `WithMiddleware`, `WithBuffer`, `WithOnError`, `WithErrorHandler`, `WithSkipDotDirs`, `WithOnAdd`, `WithLazyIsDir`, `WithIgnorePatterns`, `WithGitignore`, `WithExcludePaths`, `WithMaxWatches` | **Stable** |
| Filters     | `FilterExtensions`, `FilterIgnoreDirs`, `FilterIgnoreHidden`, `FilterGlob`, `FilterRegex`, `FilterOperations`, `FilterMinSize`, `FilterMaxSize`, `FilterMinAge`, `FilterMaxAge`, `FilterModifiedSince`, `FilterExcludePaths`, `FilterIgnoreExtensions`, `FilterNotOperations`, `FilterIgnoreGlobs`, `FilterGitignore` | **Stable** |
| Middleware  | `MiddlewareLogging`, `MiddlewareRecovery`, `MiddlewareFilter`, `MiddlewareOnError`, `MiddlewareRateLimit`, `MiddlewareSlidingWindowRateLimit`, `MiddlewareMetrics`, `MiddlewareDeduplicate`, `MiddlewareThrottle`, `MiddlewareWriteFileLog`, `MiddlewareExponentialBackoff` | **Stable** |
| Methods     | `Watch()`, `WatchOnce()`, `Add()`, `Remove()`, `Close()`, `WatchList()`, `Stats()`, `IsClosed()`, `IsWatching()`, `Errors()`, `Reset()` | **Stable** |
| Errors      | `WatcherError`, `ErrorCategory`, all sentinel errors                                                                                                                                                                                                                                               | **Stable** |
| Handlers    | `Handler`, `ErrorHandler`, `ErrorContext`                                                                                                                                | **Stable** |
| Observability | `PrometheusCollector`, `NewPrometheusCollector`, `StatsFunc`, `CounterMetric`, `GaugeMetric`, `Attribute`                                                            | **Stable** |

### Evolving APIs (may change between minor versions)

These APIs work as documented but may have behavioral changes:

| Category   | Symbols                                                                                                                                                                                      | Status       |
| ---------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------ |
| Features   | `WithPolling`, `WithPollInterval`, `WithDebug`, `WithFollowSymlinks`, `WithSelfHeal`, `WithContentHashing`                                                                                       | **Evolving** |
| Debouncer  | `GlobalDebouncer`, `Debouncer`, `DebounceKey`                                                                                                                                                | **Evolving** |
| Types      | `EventPath`, `OpString`, `DebounceKey`, `RootPath`, `OTelSpan`                                                                                                                               | **Evolving** |
| Filters    | `FilterContentHash`, `FilterWithMeta`, `FilterFromWithMeta`, `FilterWithMetaAnd`, `FilterWithMetaOr`, `FilterWithMetaNot`, `WithMeta`, `MatchResult` | **Evolving** |
| Middleware | `MiddlewareCircuitBreaker`, `MiddlewareThrottle`, `MiddlewareErrorSanitization`, `MiddlewareErrorRateLimit`, `MiddlewareErrorRecovery`, `MiddlewareErrorCorrelation`, `MiddlewareErrorBatch`, `OTelMiddleware` | **Evolving** |

Evolving APIs will not be removed without a deprecation period of at least
one minor version.

### Experimental APIs (may change at any time)

None currently.

### Deprecated APIs (will be removed in v3.0.0)

These APIs continue to work but are scheduled for removal in the next major version:

| Category | Symbols                   | Status         |
| -------- | ------------------------- | -------------- |
| Options  | `WithWatchedIgnoreDirs()` | **Deprecated** |

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

# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [v3.0.2] — 2026-05-25

### Changed

- **Trace/non-trace detection unified** — `*WithTrace` variants are now canonical implementations; non-trace versions are thin wrappers that discard the trace string. Eliminates the biggest source of code duplication in the detection engine.
- **`coverage_test.go` dissolved** — Tests moved to their natural test files (`errors_test.go`, `filter_test.go`, `pattern_test.go`, `sqlc_test.go`, `project_test.go`).
- **Test string literals centralized** — Repeated test constants extracted to `testhelpers/constants.go` and named constants in `testdata_test.go`.
- **`FilterResult` construction DRYed up** — `filteredResult()` and `notFilteredResult()` helpers eliminate repetitive struct literal construction.
- **Error system simplified** — Removed `errorCodeDefs` table, `AllErrorCodes()`, `CodeHelp()`, `Helper` interface, `CodeEqual[T]` generic. Kept `ErrorCode` type, `ErrorCoder` interface, sentinel errors, branded prefix.
- **Phantom types removed** — `StartPath`, `ConfigPath`, `Operation`, `ErrorMessage` replaced with plain `string` fields on error structs.
- **Detection helpers unexported** — `MatchesSQLCFilename`, `HasSQLCContent`, `HasSQLCCodePatterns` → unexported. Users should use `DetectReason()` or `Filter`.
- **`codeGeneratedPrefix` moved to `detection.go`** — Only used there, not in `types.go`.
- **`matchAnyContentPattern` renamed** → `matchesAnyContentPattern` for consistency with naming conventions.
- **`Filter.String()` improved** — Better debug output showing options, include, and exclude patterns.
- **`parseV1AsV2` cleaned up** — Removed zero-value noise from struct construction.
- **`validatable` interface removed** — Dead code in production.
- **Plausible analytics removed** from website — Tightened Content Security Policy.
- **Flake configuration improved** — Better nix build setup.
- **Release workflow added** — Tag-based GitHub release with automated tests, lint, and release notes.

### Fixed

- **Website CI: checkout `path` parameter placement** — `path:` was outside `with:` block for `md-go-validator` checkout
- **Website CI: private repo access** — added `token: ${{ secrets.PRIVATE_REPO_TOKEN || github.token }}` fallback
- **Benchmark CI: missing `gh-pages` branch** — created orphan `gh-pages` branch for benchmark data
- **Lighthouse CI: budgets+assertions conflict** — removed `budgetPath` input from workflow
- **Node.js 20 deprecation** — updated `actions/setup-go@v5` → `@v6` across all workflows

## [v3.0.1] — 2026-05-04

### Added

- **`FilterResult` struct** — structured result type with `Filtered bool`, `Reason FilterReason`, `Path string`, `Trace string` fields.
- **`FilterDetailed(filePath) (FilterResult, error)`** — like `Filter()` but returns a `FilterResult` with trace information.
- **`FilterPathsDetailed(paths) ([]FilterResult, error)`** — batch variant of `FilterDetailed`.
- **`AllGeneratorOptions()`** — returns all detector `FilterOption` values (excluding meta-option `FilterAll`).
- **`FilterResult.String()`** — human-readable representation of filter results.
- **`Filter.FilterReasons()`** — returns the `FilterReason` values that this filter will detect.
- **`Filter.String()`** — human-readable debug representation of filter state.

### Changed

- **Breaking: `FilterOption.Reason()` now returns `(FilterReason, bool)`** — previously returned `FilterReason` and panicked on `FilterAll`. Now returns `("", false)` for meta-options.
- **Breaking: `Cause` field renamed to `Err` on all error types** — follows Go stdlib convention.
- **`errors.AsType[T]` migration** — source code and tests use Go 1.26 `errors.AsType[T]` exclusively.
- **Module path** — added `/v3` suffix for Go module convention compliance.

### Removed

- **Breaking: context methods removed** — `FilterContext`, `FilterDetailedContext`, `FilterPathsContext` deleted. They promised cancellation over synchronous I/O.
- **Breaking: metrics system removed** — `Metrics`, `MetricsMixin`, `FilterStats`, `NewMetrics`, `GetStats`, `FilteredBy`, `FilteredFiles`, `TotalFiltered`, `WithMetricsCap`, `RecordChecked`, `RecordFiltered`.
- **Breaking: phantom types removed** — `StartPath`, `ConfigPath`, `Operation`, `ErrorMessage` deleted.
- **Breaking: error system over-engineering removed** — `errorCodeDefs` table, `AllErrorCodes()`, `CodeHelp()`, `Helper` interface, `CodeEqual[T]` generic, `Causable` interface deleted.
- **Breaking: detection helpers unexported** — `MatchesSQLCFilename`, `HasSQLCContent`, `HasSQLCCodePatterns`.
- **`Enabled()` and `Disabled()` options** — filter is enabled when it has options, include patterns, or exclude patterns.

## [v3.0.0] — 2026-05-04

### Added

- `FilterPaths(paths []string) ([]bool, error)` — batch filtering of multiple paths; returns partial results on error
- `FilterContext(ctx context.Context, filePath string) (bool, error)` — context-aware filtering with cancellation support
- `FilterPathsContext(ctx context.Context, paths []string) ([]bool, error)` — batch filtering with context cancellation between paths
- `FilterConfigError` type — returned when invalid filter options are provided; implements `ErrorCoder`, `Helper`, and `Unwrap` interfaces
- `ErrInvalidFilterOption` sentinel error — for `errors.Is()` matching
- `CodeInvalidFilterOption` error code — for programmatic error handling

- **Breaking: `WithFilterOptions` returns `(FilterConfig, error)`** — previously panicked on invalid options; now returns `*FilterConfigError` with `errors.Is()` support
- **Breaking: `NewFilter` returns `(*Filter, error)`** — previously returned `*Filter` only; now uses `errors.Join()` to aggregate config errors
- **Breaking: `FilterConfig` returns `error`** — config functions now return error to support validation; `WithFS`, `WithIncludePatterns`, `WithExcludePatterns` return `nil` error
- **Breaking: `Enabled()` and `Disabled()` removed** — a filter is now enabled when it has filter options, include patterns, or exclude patterns; `NewFilter()` with no arguments is disabled. This eliminates silent misconfiguration where forgetting `Enabled()` caused the filter to silently pass everything through.
- `IsEnabled()` now derives state from configuration — returns `true` when `len(f.options) > 0 || len(f.includePatterns) > 0 || len(f.excludePatterns) > 0`
- `enabled bool` field removed from `Filter` struct — state is implicit, not stored

### Removed

- `Enabled()` option — no longer needed; pass options to enable
- `Disabled()` option — no longer needed; call `NewFilter()` with no args

### Fixed

- **Silent misconfiguration** — previously, `NewFilter(WithFilterOptions(FilterAll))` (without `Enabled()`) compiled fine but silently did nothing; now passing options automatically enables the filter
- `FilterConfigError` type — returned when invalid filter options are provided; implements `ErrorCoder`, `Helper`, and `Unwrap` interfaces
- `ErrInvalidFilterOption` sentinel error — for `errors.Is()` matching
- `CodeInvalidFilterOption` error code — for programmatic error handling
- `FilterStats.FilteredFiles(reason FilterReason) []string` — returns file paths filtered for a given reason (defensive copy, safe to mutate)
- SQLC config v1 format test coverage — verifies v1 config parses but returns zero output dirs
- Cross-platform path matching tests — forward slash and backslash detection patterns
- `DetectReasonReader(filePath string, r io.Reader, opts ...FilterOption) (FilterReason, error)` — detection from an `io.Reader`, useful when the caller already has file content in a stream
- Integration test fixtures (`testdata/`) from 11 real code generators plus 2 handwritten negatives, loaded via `//go:embed`
- `errorCodeDefs` single-source-of-truth table — `AllErrorCodes()` and `CodeHelp()` now derive from one table
- Error code derivation tests — verify `errorCodeDefs` covers every const, has no duplicates, and matches `AllErrorCodes()` exactly
- `map[FilterOption]struct{}` replaces `map[FilterOption]bool` — values were never `false`
- `fmt.Stringer` implementation on all 5 phantom types (`StartPath`, `ConfigPath`, `Operation`, `ErrorMessage`, `TotalFilesChecked`)
- Runnable examples for `Filter`, `WithFS`, `WithIncludePatterns`, `GetStats`/`FilteredBy`/`TotalFiltered`, and `DetectReasonReader`
- Error handling examples (`errors.Is`, `ErrorCode()`, `Help()`, `CodeHelp`, `AllErrorCodes`, `FindProjectRoot`)
- Phantom type `String()` method tests — 5 types × 3 cases each
- `BenchmarkCodeHelp` — 4.9ns/op, zero allocations (map lookup)
- `Filter` method — replaces `ShouldFilter` with cleaner name; `MustFilter` removed
- CI bench step (`go test -bench=. -benchmem`)

### Changed

- **`ShouldFilter` renamed to `Filter`** — the method `ShouldFilter(filePath string) (bool, error)` is now `Filter(filePath string) (bool, error)`. The `MustFilter` panic-on-error variant has been removed; callers should handle errors explicitly.
- **`MustShouldFilter` renamed to `MustFilter`** — the double-modal name was unnecessarily verbose; the new name follows the standard Go `Must` prefix convention
- **`IsValid()` methods derived from tables** — `FilterOption.IsValid()` and `FilterReason.IsValid()` now iterate `AllFilterOptions()`/`AllFilterReasons()` instead of manual switches, eliminating split-brain bugs when adding new detectors
- **SQLC patterns consolidated** — `sqlcFilePatterns`/`sqlcCodePatterns` inlined into their consuming functions (`matchesSQLCFilenamePattern`, `HasSQLCContent`, `HasSQLCCodePatterns`)
- **SQLC filename patterns cached** — `sqlcFilenamePatterns` moved to package-level var to avoid re-allocation on every call
- **`WithFilterOptions` reuses `optionsMap`** — `FilterAll` expansion no longer duplicated between `filter.go` and `detection.go`
- **`filteredFiles` moved to `MetricsMixin`** — file path tracking now included in `GetStats()` snapshots via `FilteredFiles()` accessor
- **`slog` dependency removed** — library no longer produces log output; `warnMultipleSQLCConfigs` removed entirely
- **`FilterOption.Reason()` invariant documented** — godoc now explains the shared string-value coupling and maintenance obligation when adding new detectors
- **Include patterns semantics documented** — godoc and README clarify the "restrict scope" whitelist behavior
- **`needsContentCheck` guard documented** — comment explains I/O optimization and correctness purpose
- **Phantom types used directly** — eliminated 8+ explicit `string()` casts across `errors.go` and `project.go` via `fmt.Stringer`
- **`sqlcConfigError` bridge removed** — all internal callers now use `newSQLCConfigError` with typed phantom values
- **`Validatable` interface unexported** — renamed to `validatable`; only used as internal generic constraint

### Fixed

- **Data race in `Metrics.filteredFiles`** — field unexported; was accessible without mutex protection
- **Leaky `fs.FS` abstraction** — `detectReasonFS` no longer falls back to `os.ReadFile` when the provided filesystem doesn't contain the file
- **README metrics example** — `TotalFilesChecked == 3` (was incorrectly `1`)

### Removed

- `os.ReadFile` fallback in `detectReasonFS` — custom `fs.FS` implementations now behave correctly
- `warnMultipleSQLCConfigs` function and all `slog` usage
- `sqlcConfigError()` bridge function — replaced by direct phantom-typed calls to `newSQLCConfigError`

---

## [Pre-release] — Session 1-4

### Added

- **Error system** — centralized, branded, user-friendly error architecture:
  - `ErrorCode` string type with `String()` via direct `string(c)` conversion
  - 7 error code constants: `CodeProjectRootNotFound`, `CodeProjectRootInvalidPath`, `CodeSQLCConfigRead`, `CodeSQLCConfigParse`, `CodeSQLCConfigWalk`, `CodeSQLCConfigCollect`, `CodeSQLCConfigFind`
  - `AllErrorCodes()` function returning all defined error codes
  - `CodeHelp(code)` function returning user-friendly guidance for each error code
  - Branded `[gogenfilter:<code>]` prefix in every `Error()` message for library identification
  - 7 sentinel errors for use with `errors.Is`: `ErrProjectRootNotFound`, `ErrProjectRootInvalidPath`, `ErrSQLCConfigRead`, `ErrSQLCConfigParse`, `ErrSQLCConfigWalk`, `ErrSQLCConfigCollect`, `ErrSQLCConfigFind`
  - `ErrorCoder` interface for programmatic error code access
  - `Helper` interface for user-friendly guidance
  - `Causable` interface for errors that wrap an underlying cause _(later removed as unused)_
  - `CodeEqual[T]` generic function consolidating `Is()` comparison logic
  - `ProjectRootError` struct with `Code`, `StartPath`, `Markers`, `Cause` fields
  - `SQLCConfigError` struct with `Code`, `ConfigPath`, `Operation`, `Message`, `Cause` fields
  - Both error types implement `Error()`, `Unwrap()`, `Is()`, `ErrorCode()`, `Help()`
- **Phantom types** — type-safe wrappers at API boundaries:
  - `StartPath` for project root search starting point
  - `ConfigPath` for sqlc config file paths
  - `Operation` for error operation descriptions
  - `ErrorMessage` for error message text
  - `TotalFilesChecked` for metrics counter
- Each phantom and string-based type implements `String()` directly via `string()` conversion
- `validatable` interface for internal types with `IsValid()` (unexported)
- `newSQLCConfigError(code, ConfigPath, Operation, ErrorMessage, error)` constructor with phantom types — all internal callers now use phantom types directly
- `sqlcFindError` and `sqlcWalkError` helper constructors
- `unmarshalSQLCConfig` extracted from `parseSQLCConfig`/`parseSQLCConfigFS` for shared YAML parsing
- `walkDirForSQLCConfigs` extracted walk callback shared between OS and FS variants
- `isGeneratedBy` and `matchAnyContentPattern` extracted from detection logic
- Comprehensive `errors_test.go` with generic test helpers (`assertErrorType[T]`, `assertBrandedErrorMessage`, `testErrorCodeReturnsCode`, `assertErrorsIs`, `testCrossTypeMismatch`)
- `sqlc_test.go` error code verification tests
- `TestFindProjectRootErrorCode` in `project_test.go`
- `FilterOption.Reason()` — derives the corresponding `FilterReason` from any `FilterOption` via type conversion
- `FilterOption.IsValid()` — reports whether a `FilterOption` is a recognized value
- `Filter.IsEnabled()` — reports whether the filter is enabled without accessing internal fields
- `FilterStats.FilteredBy(reason)` — accessor for per-reason counts without exposing the internal map
- `DetectReason(path, content, options)` — public zero-I/O API that accepts content as a parameter
- Comprehensive test coverage for `ShouldFilterWithIncludes`, `IsTemplGenerated` Render path, `HasSQLCContent` versions block, `GetStats` nil metrics branch, `?` wildcard in `MatchPattern`, and `FilterOption.Reason()`
- `fmt.Stringer` compile-time compliance test for `ErrorCode`
- Unwrap chain integration tests verifying `errors.Is` traverses nested error layers for both `ProjectRootError` and `SQLCConfigError`
- Benchmarks for error construction, `Error()` formatting, and `errors.Is` matching

### Changed

- **Breaking**: `DetectGenerated` replaced by `DetectReason` (public, zero-I/O) and `detectReason` (internal, disk I/O)
- **Breaking**: `Metrics.Record()` unexported to `record()` — not part of public API
- **Breaking**: `GetMetrics()` removed from `Filter` — use `GetStats()` instead
- **Breaking**: `FilteredByReason` map unexported to `filteredByReason` — use `FilteredBy(reason)` accessor
- **Breaking**: `ParseSQLCConfig` unexported to `parseSQLCConfig` along with `SQLCConfig`/`SQLCVersion` types
- `detector` struct unified from separate `contentCheck`/`filenameCheck` types into single type with optional fields
- Table lookup functions converted to package-level `var` for zero-allocation lookup
- `matchesAnySuffix`/`matchesAnyContains` consolidated into `anyMatch`
- `filepath.Walk` replaced with `filepath.WalkDir` for better performance
- `fileExists` simplified from 7 lines to `return err == nil`
- `go.mod` toolchain downgraded from `1.26.1` to `1.26.0` for local compatibility

### Fixed

- `matchesMockgenFilename` false positive: `"mock_"` now uses prefix check instead of `Contains`, preventing matches like `remove_mock_data.go`

### Removed

- `Reasons()` method from `FilterStats` — unused and untested

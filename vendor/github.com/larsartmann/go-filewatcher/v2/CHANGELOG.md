## [Unreleased]

### Added

- Nothing yet.

### Fixed

- Nothing yet.

## [2.4.2](https://github.com/LarsArtmann/go-filewatcher/compare/v2.4.1...v2.4.2) (2026-10-07)


### Bug Fixes

* close the event channel only after every sender has exited ([e143dc0](https://github.com/LarsArtmann/go-filewatcher/commit/e143dc0a0dd29def81ee2ce15c1bb0583c2fc0c9))
* make the channel closer observable to Close before Reset ([96b7441](https://github.com/LarsArtmann/go-filewatcher/commit/96b744117d66053c7ec7a1bbadbc09050f78094d))

## [2.4.1](https://github.com/LarsArtmann/go-filewatcher/compare/v2.4.0...v2.4.1) (2026-10-07)


### Bug Fixes

* **website:** stop card grids overflowing into the right-hand TOC ([7c4ea79](https://github.com/LarsArtmann/go-filewatcher/commit/7c4ea79c6781b8dad78af1a22f96bf29b0fa5583))

## [2.4.0](https://github.com/LarsArtmann/go-filewatcher/compare/v2.3.0...v2.4.0) (2026-10-06)

### Added

- **NFC Unicode normalization in `pathKey()`** (`filesystem.go`) — all path comparisons now apply `norm.NFC.String()` before case-folding, fixing the invisible mismatch where macOS stores filenames as NFD (decomposed) but user-configured paths are NFC (composed). Without this, exclude-path matching, debounce deduplication, and gitignore prefix checks silently fail on any non-ASCII filename (`café`, `München`, `東京`). Idempotent on ASCII, zero impact on pure-ASCII paths.
- **`normalizePath()` helper** (`filesystem.go`) — `filepath.Clean(filepath.Abs(path))` applied to all path resolution sites (`New`, `Add`, `Remove`, `WithExcludePaths`, `FilterExcludePaths`), preventing trailing-slash, `..`, and redundant-separator mismatches between operations.
- **`addToWatchList()` helper** (`watcher.go`) — single point of mutation for the parallel `watchList` slice and `watchListKeys` map, eliminating the desync risk from updating them independently at 5+ call sites. `Remove()` prunes subtree keys incrementally via `delete()` rather than a full O(n) rebuild.
- **Poll loop case-awareness** (`watcher_poll.go`) — `pollWalkDir` and `pollDetectChanges` now use `pathKey()` for snapshot/current map keys, preventing phantom Create+Remove events on case-only renames on case-insensitive filesystems.
- **Gitignore matcher case-awareness** (`watcher_gitignore.go`) — `gitignoreCache.load()` and `shouldSkipByGitignore()` now use canonical pathKeys for map storage and ancestor-prefix matching, closing the gap where gitignore rules were silently bypassed on macOS.
- **NFC normalization tests** (`filesystem_test.go`) — 6 tests covering NFC vs NFD pathKey equality, ASCII idempotency, exclude-path matching, and debounce key collision.
- **Poll + gitignore case-awareness tests** (`filesystem_poll_gitignore_test.go`) — 7 tests covering poll snapshot canonical keys, original-path preservation in event emission, case-only rename phantom-event prevention, and gitignore ancestor-prefix matching.
- **`FilterCaseInsensitive(inner Filter)` wrapper** (`filter.go`) — lowercases + NFC-normalizes the event path before delegating to an inner filter, enabling filter-level case-insensitivity without changing the watcher-wide mode.
- **`Stats.CaseSensitivity` field + Prometheus gauge** (`watcher.go`, `metrics.go`) — the resolved case-sensitivity mode is now exposed via `Stats()` and as a `filewatcher_case_sensitivity` gauge (0=case-sensitive, 1=case-insensitive, 2=auto).
- **`pathKey()` benchmarks** (`benchmark_test.go`) — `BenchmarkPathKey_{ASCII,UnicodeNFC,UnicodeNFD}_{CaseSensitive,CaseInsensitive}` measuring NFC normalization cost: ASCII is ~26 ns/0 allocs (free), pre-composed NFC ~140 ns/0 allocs, decomposed NFD ~1 µs/3 allocs (macOS fs paths only).
- **`FuzzPathKey`** (`fuzz_test.go`) — fuzz target proving `pathKey()` never panics and is deterministic + idempotent across combining marks, emoji, ZWJ sequences, and invalid UTF-8.
- **README "Filesystem Compatibility" section** — documents case-sensitivity awareness, NFC normalization, and `FilterCaseInsensitive` for end users.
- **`cleanPath()` helper** (`filesystem.go`) — best-effort path cleaning without error return. Replaces `normalized, _ := normalizePath(path)` idiom in `FilterExcludePaths` and `WithExcludePaths`, eliminating the error-discarding code smell.
- **`Stats.CaseSensitivityMode` field** (`watcher.go`) — additive `FilesystemCaseSensitivity` enum field alongside the existing `CaseSensitivity string`. Provides type-safe enum access without parsing, and enables the Prometheus gauge to call `GaugeValue()` directly.
- **`FilterCaseSensitive(inner Filter)` wrapper** (`filter.go`) — NFC-normalizes the event path without case-folding. Symmetric with `FilterCaseInsensitive`. Useful on macOS where event paths arrive as NFD but case-sensitive matching is needed.
- **`EffectiveCaseSensitivity()` method** (`watcher.go`) — returns the resolved `FilesystemCaseSensitivity` enum without the overhead of a full `Stats()` call. Type-safe alternative to `Stats().CaseSensitivity` (string).
- **GaugeValue + consistency tests** (`filesystem_test.go`) — `TestFilesystemCaseSensitivity_GaugeValue`, `TestStats_CaseSensitivityFieldsConsistent`, `TestStats_CaseSensitivityModeReflectsMode`, and `TestCleanPath` verify the wired-up gauge encoding and string/enum field consistency.
- **FilterCaseSensitive + EffectiveCaseSensitivity tests** (`filesystem_test.go`) — verify NFC normalization without case-folding, case preservation, and correct auto-detection resolution.
- **Edge-case tests** (`watcher_test.go`) — `TestWatcher_Add_TrailingSlashNormalized` and `TestWatcher_Remove_DeeplyNestedUnicodeSubtree` (3-level Unicode tree: café/München/東京) verify path normalization and subtree key cleanup.
- **Filter + emoji benchmarks** (`benchmark_test.go`) — `BenchmarkFilterCaseInsensitive` (~250 ns/1 alloc), `BenchmarkFilterCaseSensitive` (~115 ns/0 allocs), and `BenchmarkPathKey_EmojiZWJ_CaseSensitive` (~170 ns/0 allocs) measuring wrapper overhead and deep Unicode normalization cost.
- **`WithMaxWatchesSafetyFraction(float64)` option** (`options.go`) — applies a safety fraction to the auto-detected inotify watch limit, leaving headroom for other processes on shared or multi-tenant machines. Default 1.0 (no reduction); recommended 0.75 for shared environments. Only affects auto-detected limits; explicit `WithMaxWatches(n)` values are used as-is.
- **`WithWatchFilteredDirectories(bool)` option** (`options.go`) — controls whether newly created directories whose `Create` event was filtered out are still added to the watcher. Default `true` (preserves existing behavior). When `false`, filtered directories are not watched, saving inotify budget for consumers that truly want to ignore subtrees.
- **`WithEventChannelMode(EventChannelMode)` option + `EventChannelMode` enum** (`options.go`) — `EventChannelBlocking` (default, preserves backpressure semantics) and `EventChannelDropOnFull` (drops events when the channel is full instead of blocking the watch loop). DropOnFull is valuable for consumers that prefer losing events over blocking the watcher.
- **`MiddlewareDeduplicateCaseInsensitive` middleware** (`middleware.go`) — case-insensitive variant of `MiddlewareDeduplicate` that lowercases dedup keys. Use on case-insensitive filesystems (NTFS, APFS) where `/File.go` and `/file.go` are the same file.
- **`FilterIgnoreDirsCaseInsensitive` filter** (`filter.go`) — case-insensitive variant of `FilterIgnoreDirs` for event-time directory-name matching on case-insensitive filesystems.
- **`Stats.EventsDroppedByMiddleware` field** (`watcher.go`) — counts events dropped by middleware (rate limiting, dedup, circuit breaker, etc.) via `atomic.Bool` emit tracking in `emitEvent`. Surfaces middleware drops that were previously invisible in stats.
- **`Stats.EventsDroppedByBackpressure` field** (`watcher.go`) — counts events dropped because the event channel was full when `DropOnFull` mode is active.
- **`Stats.ErrorsDropped` field** (`watcher.go`) — counts errors dropped because the error channel was full. Gives consumers a way to detect they need to increase `WithBuffer` or read errors more frequently.
- **Prometheus metrics for drops** (`metrics.go`) — `filewatcher_events_dropped_by_middleware_total` and `filewatcher_errors_dropped_total` counters added to `PrometheusCollector`.
- **MiddlewareBatch timer flush error routing** (`middleware.go`) — timer-flush errors are now stored in `batchState.flushErr` and returned on the next event, routing through the watcher's `handleError` pipeline instead of being silently logged.
- **Symlink cycle detection** (`watcher_walk.go`) — `WithFollowSymlinks(true)` now tracks resolved symlink targets in a `symlinkVisited` map, preventing infinite recursion and duplicate watches when symlinks point to ancestors or already-watched directories.
- **Content size cap for `FilterGeneratedCodeFull`** (`filter_gogen.go`) — files larger than 10 MiB (`defaultGogenMaxFileSize`) are skipped for content-based generated-code detection, preventing synchronous I/O stalls on large files.
- **Troubleshooting guide sections** (`Troubleshooting.md`) — 5 new sections: Polling Mode Limitations, Slow Consumers and Event Loss, Middleware Drops Not Visible, Content Hashing Performance, Symlink Cycles.
- **`Stats.WatchBudgetCap` field** (`watcher.go`) — shows the effective watch budget cap after the safety fraction is applied. `WatchLimit` now shows the raw system-detected limit before the fraction.
- **`filewatcher_events_dropped_by_backpressure_total` + `filewatcher_watch_budget_cap` Prometheus metrics** (`metrics.go`) — the backpressure drop counter and budget cap gauge are now exposed by `PrometheusCollector`.
- **`WithContentHashMaxSize(bytes int64)` option** (`options.go`) — configurable upper bound for content hashing. Files larger than this are skipped to avoid blocking the event loop. Default 10 MiB. Implicitly enables content hashing when set to a positive value.
- **`WithErrorBufferSize(n int)` option** (`options.go`) — decouples the error channel capacity from the event channel (`WithBuffer`). When not set, the error channel uses the same buffer size as the event channel.
- **`NewWatcherErrorWithStack` constructor** (`errors.go`) — creates a `WatcherError` with a caller-provided stack trace, useful when the underlying error originates in a different goroutine or deep in a syscall where `NewWatcherError`'s auto-captured stack would be misleading.

### Fixed

- **`WithDebug(nil)` panic** (`options.go`, `watcher_internal.go`) — the option doc says "If logger is nil, `log/slog.Default()` is used" but the implementation stored nil, causing a nil-pointer panic on the first debug log. Now honors the documented contract with a nil guard in `debugLog` as defense-in-depth.
- **`MiddlewareBatch` duplicate emission on full batch** (`middleware.go`) — when the batch reached `maxSize`, the first N-1 events were flushed AND the triggering event was also emitted individually via `next(ctx, event)`. The triggering event is already in the flushed batch, so the individual emission was a duplicate. Removed the spurious `next` call.
- **Polling mode ignores walk-time exclusions** (`watcher_poll.go`) — `pollWalkDir` only checked `shouldSkipDir` but never `shouldExcludePath` or `shouldSkipByGitignore`. Now applies the same skip logic as the initial walk, including loading `.gitignore` per directory.
- **`FilterExcludePaths` and `FilterGitignore` sensitive to path form** (`filter.go`) — relative paths passed by the consumer were compared against absolute event paths, causing silent filter misses. Both now normalize to absolute via `filepath.Abs` before comparison.
- **`MiddlewareDeduplicate` does not normalize path keys** (`middleware.go`) — dedup keys used raw `event.Path`, so NFD/NFC variants (macOS) or case-only differences (NTFS/APFS) were not deduplicated. Now applies NFC normalization; `MiddlewareDeduplicateCaseInsensitive` adds case-folding.
- **Path validation in `Add`/`AddRecursive`/`Watch()`** (`watcher.go`) — invalid paths were silently added to `failedPaths` and retried by self-heal forever. Now `validateDirExists()` stats the path and returns `ErrPathNotFound` immediately for non-existent paths or `ErrPathNotDir` for files.
- **Syscall error classification** (`errors.go`) — `categorizeError` did not recognize `os.ErrPermission`, `os.ErrNotExist`, or `syscall.ENOTDIR` as permanent, causing self-heal to retry permission-denied paths forever. Now classifies these as permanent; `syscall.ENOSPC` as transient.
- **Case-insensitive `shouldSkipDir`** (`watcher_walk.go`) — walk-time directory-name matching was always case-sensitive, so `BUILD` and `Node_Modules` were not skipped on case-insensitive filesystems. Now lowercases comparisons when `CaseInsensitive` mode is active.
- **`applyMaxWatchesFraction` applied to explicit limits** (`watcher.go`) — the safety fraction was applied to all `maxWatches > 0`, including explicit `WithMaxWatches(n)` values, contradicting the documentation. Now only applies to auto-detected limits. `Reset()` also preserves explicit settings instead of always re-detecting.
- **`EventsProcessed` counted dropped events** (`watcher_internal.go`) — the counter was incremented before the channel send, so events dropped by `DropOnFull` backpressure or aborted during shutdown were counted as "processed." Now only increments on successful send.


- **Dead code: `GaugeValue()` never called** (`metrics.go`, `filesystem.go`) — the `GaugeValue()` method was defined but never invoked. The Prometheus gauge used a separate `caseSensitivityGauge(string)` function with its own switch/case, duplicating the gauge encoding. Now wired via `Stats.CaseSensitivityMode`, and the duplicate `caseSensitivityGauge()` function is deleted.
- **`_` error discarding code smell** (`filter.go`, `options.go`) — `FilterExcludePaths` and `WithExcludePaths` used `normalized, _ := normalizePath(path)`, discarding the error with a comment. Replaced with the `cleanPath()` helper that encapsulates the best-effort intent.
- **go.mod language version accidentally bumped to 1.27** (`go.mod`) — an auto-commit raised the required Go version to 1.27, breaking builds on Go 1.26 toolchains. Restored the intended `go 1.26.7` floor.

### Changed

- **`eventsProcessed` semantics** (`watcher_internal.go`) — the counter now increments when an event reaches the event channel (inside `trackedEmit`), not when the middleware chain returns nil. This is more correct: an event is "processed" when it's emitted, not when middleware returns nil (which could happen even if the event was never sent, e.g., due to context cancellation). Events dropped by middleware are no longer counted as processed.
- **Middleware drop tracking via `atomic.Bool`** (`watcher_internal.go`) — `emitEvent` wraps the emit function with an `atomic.Bool` that detects whether the middleware chain called emit. If middleware returns nil without calling emit, the event was dropped and `EventsDroppedByMiddleware` is incremented. This replaces the removed `executeHandler` method.

- **`Remove()` watchListKeys desync** (`watcher.go`) — `Remove()` only deleted the exact path key from `watchListKeys`, leaving subtree path keys orphaned. Now deletes each pruned subtree key incrementally (O(removed), not O(n) rebuild), guaranteeing the map stays in sync after bulk subtree removal.
- **`normalizePath()` best-effort cleaning on `Abs` failure** (`filesystem.go`) — when `filepath.Abs` fails (e.g. unreadable cwd), the returned path is now still `filepath.Clean`'d so trailing slashes, `..`, and redundant separators are normalized. `FilterExcludePaths`/`WithExcludePaths` use the always-cleaned result instead of swallowing the error and falling back to the raw path.
- **`golang.org/x/text` promoted to direct dependency** (`go.mod`) — was an indirect dependency; now explicitly required for `unicode/norm` NFC normalization in `pathKey()`.
- **`WatchLimit` semantics** (`watcher.go`) — now shows the raw system-detected inotify limit before the safety fraction. The effective cap (after fraction) is exposed as the new `Stats.WatchBudgetCap` field. Consumers who previously read `WatchLimit` to see the effective cap should switch to `WatchBudgetCap`.
- **`hashFile` accepts `maxSize` parameter** (`filter.go`) — the hardcoded 10 MiB cap is now a parameter. `convertEvent` receives `maxHashSize int64` instead of `computeHash bool`; when `maxHashSize > 0`, hashing is enabled with that cap.


- **`FilesystemCaseSensitivity` promoted to Stable API** (`API_STABILITY.md`) — the enum values are foundational (control `pathKey()` behavior) and will not change. Adding new values (e.g., `CaseSensitivityProbed` in v3) is backward-compatible. `WithCaseSensitivity` and `FilterCaseInsensitive` remain Evolving.
- **Prometheus gauge uses `GaugeValue()` directly** (`metrics.go`) — the `filewatcher_case_sensitivity` gauge now reads `stats.CaseSensitivityMode.GaugeValue()` instead of parsing a string. Single source of truth for the gauge encoding.
- **`FilterGeneratedCode` sqlc detection follows gogenfilter v3.6 semantics** (`filter_gogen.go`, `go.mod`) — the gogenfilter dependency moved from v3.2 to v3.6.1: weak filenames such as `models.go` are no longer treated as sqlc-generated on their own. Strong `*.sql.go` patterns still match by filename; content markers (`// Code generated by sqlc. DO NOT EDIT.`) are detected by `FilterGeneratedCodeFull` with `ContentCheckEnabled`.


## [2.3.0] - 2026-07-27

### Added

- **`WithCleanup` option** (`options.go`) — registers cleanup functions called on `Close()` after goroutines/channels are torn down. Cleared on `Reset()`. Enables lifecycle-managed resource cleanup for middleware that hold file handles.
- **`NewFileLogMiddleware`** (`middleware.go`) — returns `(Middleware, func() error)` for file-logging middleware with proper fd cleanup. Pair with `WithCleanup` to avoid descriptor leaks in long-lived watchers. `MiddlewareWriteFileLog` delegates to this but discards the closer (backward compat).
- **Hermetic `benchstat`** (`flake.nix`) — `bench-diff` now uses a `buildGoModule`-built `benchstat` instead of `go run golang.org/x/perf/cmd/benchstat@latest` (network, non-reproducible).
- **`nix run .#lint-tests`** (`flake.nix`) — dedicated app for explicit test-file linting (`--tests` flag).
- **`examples-build` nix check** (`flake.nix`) — `examples/` added to the source `fileset.unions` and a new check that compiles `go build ./examples/...`.
- **Commitlint CI gate** (`.github/workflows/commitlint.yml`) — validates PR commit subjects follow `type(scope?): subject` conventional-commit format with 72-char limit.
- **Release-please workflow** (`.github/workflows/release-please.yml`) — opens release PRs from conventional commits, automating CHANGELOG + version bumps. Go-aware with `v` tag prefix.
- **Docs-consistency CI gate** (`.github/workflows/docs-consistency.yml`) — checks that deprecation claims don't drift between `README.md` and `API_STABILITY.md`.
- **Benchmark baseline workflow** (`flake.nix`) — `nix run .#bench-baseline` captures a clean (no `-race`, `-count=6`, `-run=^$`) baseline to the gitignored `bench-baseline.txt`; `nix run .#bench-diff` runs fresh benchmarks through `benchstat` against it for one-command regression checks. Pairs with the ROADMAP "benchmark freshness CI" idea.
- **`GOTMPDIR` in devShell** (`flake.nix`) — the default devShell redirects Go's temp dir to a disk-backed `${XDG_CACHE_HOME:-$HOME/.cache}/go-filewatcher/gotmp`, preventing tmpfs `/tmp` exhaustion during long sessions.
- **gocritic `exitAfterDefer` exclusion for `examples/`** (`.golangci.yml`) — defensive path+text exclusion so `log.Fatal` in example `main()` functions doesn't trigger per-line nolint fights.
- **Middleware default-const usage guard** (`middleware_test.go`) — `TestMiddlewareDefaultConsts_AllUsed` is an AST-based test asserting every middleware default const is both declared in `middleware.go` and referenced, catching dead constants if defaulting is refactored away (and drift if the expected list falls out of sync).
- **`FilterAnd` short-circuit regression test** (`filter_test.go`) — `TestFilterAndShortCircuitsOnFirstFalse` locks in the lazy evaluation contract.
- **Default-guard convention docs** (`AGENTS.md`) — a worked-example "Default-guard convention" subsection documenting the shared-vs-unique `resolve*Defaults` decision.
- **`WatchChanges` contract sketch** (`docs/research/watchchanges-contract.md`) — design for an idempotent reconcile-to-target-state sync API for sync/backup workflows.
- **Semantic-release / conventional-commits evaluation** (`docs/research/semantic-release-evaluation.md`) — recommends `release-please` + a commit-subject lint gate over `semantic-release` for a Go module.

### Changed

- **`nix run .#ci`/`.#fmt`/`.#tidy` run from caller CWD** (`flake.nix`) — these write-modifying apps no longer `cd "${self}"` into the read-only nix store; they operate on the working directory directly. Documented as "invoke from repo root."
- **`MiddlewareDeduplicate` cleanup trigger** (`middleware.go`) — replaced `len(seen)%100 == 0` with an event counter (`eventCount%dedupeCleanupInterval`), eliminating the quirk where a map hovering at a multiple of 100 triggered cleanup every event.
- **Self-heal respects permanent failures** (`watcher_selfheal.go`) — `attemptSelfHeal` now checks `IsPermanent()` and abandons paths that can never recover (deleted directory, wrong entry type) instead of retrying them forever. Retry budget is now spent only on genuinely transient failures.
- Go toolchain bumped from 1.26.4 to 1.26.5
- **Middleware default-guard consistency** (`middleware.go`) — every middleware default is now a named const (`defaultThrottleEvents` extracted; zero magic literals remain in default-guard code). Shared defaulting (2+ functions) goes through a `resolve*Defaults` helper; single-function defaulting uses an inline guard with a named const.
- **Debouncer `Stop` lifecycle consolidated** (`debouncer.go`) — `baseDebouncer.stop(cleanup)` centralizes the lock → markStopped → cleanup → unlock → wait envelope shared by `Debouncer.Stop()` and `GlobalDebouncer.Stop()`.
- **Negative-duration panics unified** (`options.go`) — `requireNonNegativeDuration(optionName, delay)` is now used by both `WithDebounce` and `WithPerPathDebounce`.

### Fixed

- **README Prometheus snippet** (`README.md`) — replaced the broken polling-loop pattern (used fake `prometheus.ExemplarAdder` type that would panic at runtime) with a correct `prometheus.Collector` wrapper implementation (Describe + Collect using `MustNewConstMetric`). Prometheus now scrapes on its own schedule — no polling loop needed.
- **README OTel snippet** (`README.md`) — fixed incorrect API references: `trace.Attribute` → `attribute.KeyValue`, `trace.StringAttribute` → `attribute.String`, `trace.StatusError`/`trace.StatusOk` → `codes.Error`/`codes.Ok`, `stdouttracer.New()` → proper `stdouttrace.New()` + `sdktrace.NewTracerProvider` setup. Verified against the actual OTel SDK API (`go.opentelemetry.io/otel/{attribute,codes,trace}` + `exporters/stdout/stdouttrace`).
- **`MiddlewareWriteFileLog` fd leak** (`middleware.go`) — file handle was opened lazily but never closed. Added `NewFileLogMiddleware` returning a closer, plus `WithCleanup` option to register it with the watcher lifecycle. `MiddlewareWriteFileLog` delegates to `NewFileLogMiddleware` (backward compat).
- **`bench-baseline.txt` slog pollution** (`flake.nix`) — benchmarks now use `-run=^$` to skip test functions (whose watcher error output polluted the baseline file). The reference file is now clean for `benchstat`.
- **4 broken `BenchmarkEmitEvent_*` benchmarks** (`benchmark_test.go`) — deadlocked because the size-1 event channel was never drained; the second emit blocked forever on a zero-value `Watcher` (nil `done` channel, uncancellable context). Extracted a shared `benchmarkEmitEvent` helper that uses a non-blocking drain, valid for both direct-emit and debounced paths.
- **2 flaky tests hardened** (`watcher_test.go`, `testing_helpers_test.go`) — `TestWatcher_Stats_Metrics` and `TestWatcher_Watch_WithMiddleware` relied on fixed `time.Sleep` to wait for async fsnotify delivery and counter propagation. Replaced with a new `waitForCondition` polling helper that waits deterministically until the assertion holds (or times out), eliminating the race that caused intermittent failures.

### Deprecated

- **`WithOnError`** (`options.go`) — redundant convenience wrapper around `WithErrorHandler` that discards the `ErrorContext`. Deprecated in favor of `WithErrorHandler`; see `API_STABILITY.md`.
- **`MiddlewareRateLimit`** (`middleware.go`) — strict special case of `MiddlewareThrottle` (`MiddlewareRateLimit(n)` == `MiddlewareThrottle(n, n)`). Deprecated in favor of `MiddlewareThrottle`; see `API_STABILITY.md`.

### Internal

- **`addAttemptCount` wired up** (`error_simulation_test.go`, `fake_backend_test.go`) — the previously-dead `fakeBackend.addAttemptCount` method is now used in self-heal and specific-path-failure tests to verify retry counts and per-path attempt tracking.
- **Deprecation docs published** (`README.md`, `MIGRATION.md`, `api-reference.mdx`) — `WithOnError` and `MiddlewareRateLimit` now have visual deprecation callouts in README tables, a v2.3→v3 migration section in MIGRATION.md, and Starlight deprecation badges in the website API reference.
- **`WithCleanup` + `NewFileLogMiddleware` in API_STABILITY.md** — registered as Evolving APIs.
- **Backend abstraction for testability** (`backend.go`) — introduced `watchBackend` interface abstracting `*fsnotify.Watcher`, with `fsnotifyBackend` adapter for production and unexported `withBackend()` option for test injection. `Watcher.fswatcher` field type changed from `*fsnotify.Watcher` to `watchBackend`. Enables deterministic error simulation without a real filesystem.
- **Error simulation test suite** (`error_simulation_test.go`, `fake_backend_test.go`) — 11 tests covering self-heal retry/abandon, error channel propagation, full pipeline event flow, closed-backend graceful shutdown, circuit breaker state machine, and error recovery strategy. Uses a fake backend that scripts Add failures (ENOSPC, permission denied, permanent), event injection, and error injection.
- **`MustWatch` helper** (`examples/demo/shared.go`) — eliminates the last `art-dupl -t 1` clone group in examples. All 4 example `main()` functions migrated; `filter-generated` also fixed a pre-existing resource leak (deferred Close ran before events were consumed).
- **`resolveBatchDefaults` unit test** (`middleware_test.go`) — 6 table-driven cases closing the coverage gap on the third shared `resolve*` helper.
- **Test-helper adoption** — `newTestWatcher` (the pre-existing but underused `New + err-check + defer Close` helper) now has 96 call sites across 12 test files (`grep -rc newTestWatcher *_test.go`); inline `New(` calls in tests were reduced to the deliberately-retained set (benchmarks, `Example*` functions, error-path and lifecycle tests).
- **Direct unit tests** added for the shared helpers: `resolveRateLimitDefaults` (6 table cases), `resolveMaxFailures` (3 table cases), `resolveBatchDefaults` (6 table cases), `requireNonNegativeDuration` (panic + zero/positive subtests).
- **Examples lint hygiene** — extracted `debounceDelay` named consts in `examples/basic` and `examples/per-path-debounce` (resolves pre-existing `mnd` warnings); fixed `gocritic exitAfterDefer` and stale `nolintlint` directives in `examples/middleware` and `examples/filter-generated`.
- `.editorconfig` added for consistent editor settings across platforms.
- `art-dupl` clone groups driven to zero at all thresholds (`-t 1` through `-t 5`); the previous surviving groups at `-t 1` were eliminated by the `MustWatch` helper in `examples/demo`.
- **Ecosystem integration (dogfooding)** — go-filewatcher integrated into three real consumer projects: `dynamic-markdown-site`, `auto-deduplicate`, and Cyberdom. Validates the public API surface against production usage patterns.
- **`docs/DOMAIN_LANGUAGE.md` refreshed** — added missing terms: `FilterWithMeta`, `MatchResult`, `ContentHash`, `ErrorCategory`, `CircuitState` (`CircuitClosed`/`CircuitOpen`/`CircuitHalfOpen`), `Handler`, `Circuit Breaker`, `Error Category`.
- **Docs-consistency exemption list shrunk 36→14** (`docs-consistency.yml`) — documented 22 previously-exempted exported symbols in FEATURES.md (error taxonomy, metrics types, filter variants, debouncer constructors). Remaining exemptions are phantom-type helpers (11) + 2 filter gaps + 1 deprecated.
- **`wrapHandlerWithNilReturn` architectural constraint documented** (`watcher_internal.go`) — added doc comment explaining why middleware errors are absorbed (prevents cascade) and the implication for error-aware middleware (must be innermost layer to observe handler failures).

## [2.2.1] - 2026-07-24

### Added

- Nix flake support — `flake.nix` with devShell, quality-gate apps (`nix run .#check`, `.#lint`, `.#test`, `.#ci`), and reproducible vendored builds (`proxyVendor`, `vendorHash`)
- `.golangci.yml` — strict linting configuration with 50+ linters (exhaustruct, wrapcheck, paralleltest, gci, etc.)
- `.gitattributes` for consistent line-ending handling across platforms
- `FEATURES.md` — honest feature inventory with status indicators (DONE, PARTIALLY DONE, PLANNED, WORTH CONSIDERING)
- `ROADMAP.md` — long-term direction, raw ideas, and non-goals
- Public documentation website (`website/`) — Astro + Starlight site deployed to Firebase Hosting at `filewatcher.lars.software`, with 17 components, full docs content, PWA manifest, and dark/light theming
- `docs/DOMAIN_LANGUAGE.md` — rebuilt from scratch with 20+ domain terms grounded in actual code (Watcher, Event, Op, Filter, Middleware, Debouncer, Watch Budget, Self-healing, Polling Mode, etc.)
- `CODE_OF_CONDUCT.md` — contributor community guidelines
- Competitive analysis research: `docs/research/go-filewatcher-vs-ro-fsnotify.md` and `docs/research/adopting-samber-ro-pro-contra.md`
- `AddRecursive` error messages now include the `maxDepth` parameter for easier debugging

### Changed

- Go toolchain bumped from 1.26.3 to 1.26.4
- `README.md` rewritten as a marketing-focused landing page with feature comparison table, "How it works" guide, and link to the documentation website
- `DefaultIgnoreDirsCopy()` now uses `slices.Clone` instead of a manual slice copy
- `withResolvedPath` helper consolidates the lock → closed-check → `filepath.Abs` → error-wrapping prologue shared by `Add`, `AddRecursive`, and `Remove` — eliminates copy-pasted boilerplate
- `runExampleWatcher` helper deduplicates watcher setup across ~20 godoc examples in `example_test.go`
- `demo.Run` helper deduplicates context setup across example programs in `examples/`
- Import ordering in `watcher.go` normalized via goimports
- `AGENTS.md` restructured: removed version-stamped gotchas, added pointers to companion docs (FEATURES/ROADMAP/TODO_LIST), added website section and file organization for `filter_gogen.go` and `phantom_types.go`
- `TODO_LIST.md` rebuilt: removed 140+ completed historical items, kept only actionable short/mid-term work with clear priorities
- CHANGELOG backfilled with three missing historical releases: v0.2.1, v0.2.2, v0.3.0
- CI: added `GOEXPERIMENT=jsonv2`, pinned `golangci-lint` to v2.12.2, aligned coverage threshold from 90% to 85% (actual: 85.8%)
- `makezero` lint compliance: switched from pre-allocated indexing to `make([]T, 0, N)` + `append` pattern
- Expanded `.gitignore` with JS/TS build artifacts and cache directories

### Dependencies

- `github.com/LarsArtmann/gogenfilter/v3` updated from v3.1.0 to v3.2.0 — removed unused transitive dependencies (pprof, ginkgo, gomega, x/net)

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

## [0.3.0] - 2026-05-05

Version bump — same commit as v0.2.2. No additional code changes.

## [0.2.2] - 2026-05-05

### Fixed

- Migrated gogenfilter import to `/v3` module path

## [0.2.1] - 2026-05-04

### Added

- `WatchOnce(ctx)` — one-shot mode that returns the first event and closes
- `MiddlewareThrottle(maxEvents, burst)` — token-bucket rate limiting via `golang.org/x/time/rate`
- `FilterIgnoreGlobs(patterns...)` and `WithIgnorePatterns(pats...)` option
- MIT license (relicensed from Proprietary)
- Benchmark CI job and Dependabot configuration
- 15 new tests covering WatchOnce, MiddlewareThrottle, FilterIgnoreGlobs, WithIgnorePatterns

### Changed

- Migrated gogenfilter from v0.2.0 to v3.x
- Simplified phantom types from go-branded-id to plain named types
- Replaced custom rate-limit implementations with `golang.org/x/time/rate`
- Pinned nixpkgs to nixos-unstable instead of channel default

### Fixed

- `WatchOnce` nil-error wrapping in cancellation path
- Double-append bug, error swallowing, toolchain, and test pollution issues

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

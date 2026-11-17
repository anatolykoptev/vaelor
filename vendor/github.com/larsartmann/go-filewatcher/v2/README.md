<h1 align="center">go-filewatcher</h1>

<p align="center"><strong>A high-performance, composable file system watcher for Go.</strong></p>

<p align="center">
<a href="https://pkg.go.dev/github.com/larsartmann/go-filewatcher/v2"><img src="https://pkg.go.dev/badge/github.com/larsartmann/go-filewatcher/v2.svg" alt="Go Reference"></a>
<a href="https://github.com/larsartmann/go-filewatcher/actions/workflows/ci.yml"><img src="https://github.com/larsartmann/go-filewatcher/actions/workflows/ci.yml/badge.svg" alt="CI"></a>

<a href="LICENSE"><img src="https://img.shields.io/badge/license-MIT-blue.svg" alt="License: MIT"></a>

</p>

<p align="center">
<a href="https://filewatcher.lars.software">Documentation</a> · <a href="https://pkg.go.dev/github.com/larsartmann/go-filewatcher/v2">API Reference</a>
</p>

---

Built on [fsnotify](https://github.com/fsnotify/fsnotify). Eliminates the boilerplate of raw fsnotify with sensible defaults, automatic recursion, 17+ composable filters, 18 middleware, and production-grade resilience.

## Why?

Raw fsnotify gives you events and nothing else. Every real-world file watcher needs the same infrastructure:

- **Recursion** — you must walk directories and add each one manually
- **Filtering** — you write the same extension/ignore/pattern logic every time
- **Debouncing** — editors trigger 5 events on a single save; you coalesce them yourself
- **Resilience** — ENOSPC crashes your watcher; you handle it or you don't
- **Observability** — no stats, no metrics, no tracing hooks

go-filewatcher handles all of this. Built for production, tested with `-race`, and deployed in real systems.

## Comparison

| Feature            | Raw fsnotify | Other wrappers | go-filewatcher |
| ------------------ | :----------: | :------------: | :------------: |
| Recursive watching |              |    Partial     |       ✓        |
| Built-in filters   |              |      Few       |      17+       |
| Middleware chains  |              |                |       18       |
| Debouncing         |              |    Partial     | Global+PerPath |
| .gitignore-aware   |              |                |       ✓        |
| ENOSPC resilience  |              |                |       ✓        |
| NFS/FUSE polling   |              |                |       ✓        |
| Self-healing       |              |                |       ✓        |
| Prometheus + OTel  |              |                |       ✓        |

## How it works

1. **Create** — `New(paths, opts...)` validates paths, walks directories, applies options, and registers inotify watches
2. **Watch** — `Watch(ctx)` starts the event loop goroutine, returns a read-only event channel
3. **Pipeline** — each event passes through your filter chain (AND/OR/NOT composition) and middleware (logging, recovery, rate limiting, metrics)
4. **Deliver** — filtered events arrive on the channel with path, op, timestamp, size, modtime, and optional content hash

## Install

```bash
go get github.com/larsartmann/go-filewatcher/v2
```

Requires Go 1.26.7 or later (the version pinned in `go.mod` and the CI matrix).

## Usage

```go
package main

import (
    "context"
    "fmt"
    "log"
    "time"

    filewatcher "github.com/larsartmann/go-filewatcher/v2"
)

func main() {
    watcher, err := filewatcher.New(
        []string{"./src"},
        filewatcher.WithExtensions(".go"),
        filewatcher.WithDebounce(500*time.Millisecond),
        filewatcher.WithIgnoreDirs("vendor", "node_modules"),
    )
    if err != nil {
        log.Fatal(err)
    }
    defer watcher.Close()

    events, err := watcher.Watch(context.Background())
    if err != nil {
        log.Fatal(err)
    }

    for event := range events {
        fmt.Printf("%s: %s\n", event.Op, event.Path)
    }
}
```

### With Middleware

```go
watcher, err := filewatcher.New(
    []string{"./src"},
    filewatcher.WithExtensions(".go"),
    filewatcher.WithMiddleware(
        filewatcher.MiddlewareRecovery(),   // Runs LAST (innermost)
        filewatcher.MiddlewareLogging(nil), // Runs FIRST (outermost)
    ),
)
```

### With Custom Filters

```go
filter := filewatcher.FilterAnd(
    filewatcher.FilterExtensions(".go"),
    filewatcher.FilterNot(filewatcher.FilterIgnoreDirs("vendor")),
    filewatcher.FilterOperations(filewatcher.Write, filewatcher.Create),
)

watcher, err := filewatcher.New(
    []string{"./src"},
    filewatcher.WithFilter(filter),
)
```

### Filtering Generated Code

Exclude auto-generated Go files via [gogenfilter](https://github.com/LarsArtmann/gogenfilter):

```go
watcher, err := filewatcher.New(
    []string{"./src"},
    filewatcher.WithFilter(filewatcher.FilterGeneratedCode()),
)
```

Detects sqlc, protobuf, templ, mockgen, stringer, and 13 more generators.

> **sqlc note (gogenfilter v3.6):** weak filenames like `models.go` are NOT
> treated as sqlc-generated on their own (hand-written `models.go` is too
> common). Strong `*.sql.go` patterns still match by filename, and the
> `// Code generated by sqlc. DO NOT EDIT.` content marker is detected when
> content checking is enabled.

## Configuration Options

| Option                        | Description                                                                 | Default                   |
| ----------------------------- | --------------------------------------------------------------------------- | ------------------------- |
| `WithDebounce(d)`             | Global debounce — all events coalesced into one emission after delay        | `0` (disabled)            |
| `WithPerPathDebounce(d)`      | Per-path debounce — each file debounced independently                       | `0` (disabled)            |
| `WithFilter(f)`               | Add a custom filter function                                                | —                         |
| `WithExtensions(exts...)`     | Only emit events for given file extensions                                  | —                         |
| `WithIgnoreDirs(dirs...)`     | Discard events from given directory names (also skips during walk)          | —                         |
| `WithIgnorePatterns(pats...)` | Discard events for files matching glob patterns (filename only)             | —                         |
| `WithIgnoreHidden()`          | Discard events for hidden files/dirs (dot prefix)                           | —                         |
| `WithRecursive(b)`            | Enable/disable recursive directory watching                                 | `true`                    |
| `WithMiddleware(m...)`        | Add middleware to the event processing pipeline                             | —                         |
| `WithErrorHandler(fn)`        | Set custom error handler for watcher errors                                 | `stderr` logging          |
| `WithOnError(fn)` ⚠️           | **Deprecated.** Use `WithErrorHandler`. Simplified callback (`func(error)`) | —                         |
| `WithSkipDotDirs(skip)`       | Skip directories starting with a dot during walking                         | `true`                    |
| `WithBuffer(size)`            | Event channel buffer size for handling bursts                               | `64`                      |
| `WithOnAdd(fn)`               | Callback invoked when a new path is added to the watcher                    | —                         |
| `WithLazyIsDir()`             | Skip `os.Stat` calls in event conversion (IsDir always false)               | `false`                   |
| `WithPolling(fallback)`       | Supplement fsnotify with periodic polling (NFS/FUSE/Docker volumes)         | `false`                   |
| `WithPollInterval(d)`         | Polling interval (requires `WithPolling(true)`)                             | `2s` when polling enabled |
| `WithDebug(logger)`           | Enable verbose structured debug logging                                     | —                         |
| `WithFollowSymlinks(b)`       | Follow symbolic links during directory walking                              | `false`                   |
| `WithGitignore(b)`            | `.gitignore`-aware walk filtering (skips ignored subtrees)                  | `true`                    |
| `WithExcludePaths(paths...)`  | Exclude absolute paths (and subtrees) during walk (prefix matching)         | —                         |
| `WithMaxWatches(n)`           | Override inotify watch budget (auto-detected from `/proc/sys/...` on Linux) | auto-detected             |
| `WithContentHashing()`        | Populate `Event.Hash` with SHA-256 of file content (capped 10 MiB)          | `false`                   |
| `WithSelfHeal(interval)`      | Auto-retry failed watch registrations at the given interval                 | `0` (disabled)            |

## Filters

Filters determine which events are emitted. Return `true` to keep, `false` to discard.

| Filter                            | Description                                              |
| --------------------------------- | -------------------------------------------------------- |
| `FilterExtensions(exts...)`       | Only files with given extensions                         |
| `FilterIgnoreExtensions(exts...)` | Exclude files with given extensions                      |
| `FilterIgnoreDirs(dirs...)`       | Exclude files within given directory names               |
| `FilterExcludePaths(paths...)`    | Exclude files within given absolute paths (prefix match) |
| `FilterIgnoreHidden()`            | Exclude hidden files/directories                         |
| `FilterIgnoreGlobs(patterns...)`  | Exclude files matching glob patterns (filename only)     |
| `FilterOperations(ops...)`        | Only given operation types                               |
| `FilterNotOperations(ops...)`     | Exclude given operation types                            |
| `FilterGlob(pattern)`             | Match file name against glob pattern                     |
| `FilterRegex(pattern)`            | Match path against regex pattern                         |
| `FilterMinSize(bytes)`            | Only files at least the given size                       |
| `FilterMaxSize(bytes)`            | Only files at most the given size                        |
| `FilterMinAge(age)`               | Only files older than given duration                     |
| `FilterModifiedSince(t)`          | Only files modified after given time                     |
| `FilterContentHash(expectedHex)`  | Only files matching expected SHA-256                     |
| `FilterGitignore(repoRoot)`       | Exclude files ignored by `.gitignore` in `repoRoot`      |
| `FilterGeneratedCode(gens...)`    | Exclude auto-generated Go files (sqlc, protobuf, ...)    |

Compose with `FilterAnd`, `FilterOr`, and `FilterNot`.

## Middleware

Middleware wraps event handlers for cross-cutting concerns. Applied in **reverse order** (last added runs first).

| Middleware                                      | Description                                                            |
| ----------------------------------------------- | ---------------------------------------------------------------------- |
| `MiddlewareLogging(logger)`                     | Log all events with structured logging (slog)                          |
| `MiddlewareRecovery()`                          | Recover from panics, log stack trace                                   |
| `MiddlewareFilter(filter)`                      | Filter events (same as WithFilter)                                     |
| `MiddlewareOnError(handler)`                    | Handle errors from downstream handlers                                 |
| `MiddlewareRateLimit(maxEvents)` ⚠️              | **Deprecated.** Use `MiddlewareThrottle(maxEvents, maxEvents)` instead |
| `MiddlewareSlidingWindowRateLimit(n, win)`      | Sliding-window rate limiting                                           |
| `MiddlewareThrottle(maxEvents, burst)`          | Token-bucket rate limiting via `golang.org/x/time/rate`                |
| `MiddlewareMetrics(counter)`                    | Count processed events by operation                                    |
| `MiddlewareDeduplicate(window)`                 | Drop duplicate events within a time window                             |
| `MiddlewareBatch(window, maxSize, flush)`       | Batch events over a window or size threshold                           |
| `MiddlewareWriteFileLog(path)`                  | Write events to file for audit trail                                   |
| `MiddlewareCircuitBreaker(maxFail, reset)`      | Fault tolerance with closed/open/half-open states                      |
| `MiddlewareExponentialBackoff(maxF, init, max)` | Configurable backoff for event processing                              |
| `MiddlewareErrorRateLimit(maxErrs, window)`     | Per-error-type rate limiting                                           |
| `MiddlewareErrorRecovery(strategy)`             | Recoverable error handling with custom strategies                      |
| `MiddlewareErrorCorrelation(idGen)`             | Attach correlation IDs for request tracing                             |
| `MiddlewareErrorSanitization(sanitize)`         | Safe error message scrubbing preserving error chains                   |
| `MiddlewareErrorBatch(window, maxSize, flush)`  | Batch errors for analytics                                             |

## Event

```go
type Event struct {
    Path      string    // Absolute path of changed file/directory
    Op        Op        // Create, Write, Remove, or Rename
    Timestamp time.Time // When the event was detected
    IsDir     bool      // True if directory, false if file
    Size      int64     // File size in bytes (0 if unavailable)
    ModTime   time.Time // File modification time (zero if unavailable)
    Hash      string    // SHA-256 hex digest (populated only with WithContentHashing)
}
```

Event priority when multiple operations coalesce: `Create > Write > Remove > Rename`.

Full JSON marshaling and `slog.LogValuer` support. Chmod events are ignored.

## Resilience

Built for large, long-running watchers:

```go
watcher, err := filewatcher.New(
    []string{"./large-monorepo"},
    filewatcher.WithGitignore(true),                   // default: true
    filewatcher.WithExcludePaths("/home/me/forks"),    // skip subtrees
    filewatcher.WithMaxWatches(524288),                 // override inotify budget
    filewatcher.WithSelfHeal(30 * time.Second),         // auto-retry failures
    filewatcher.WithFollowSymlinks(true),               // follow symlinks
)
```

When the inotify budget is exhausted, directories are skipped silently and counted in `Stats.WatchErrors` — the watcher starts in degraded mode instead of failing entirely.

### NFS/FUSE Support

```go
watcher, err := filewatcher.New(
    []string{"/mnt/nfs/share"},
    filewatcher.WithPolling(true),
    filewatcher.WithPollInterval(2 * time.Second),
)
```

### Filesystem Compatibility

Filesystems differ in two ways that silently break naive watchers: **case
sensitivity** (NTFS/APFS are case-insensitive, ext4/XFS are case-sensitive) and
**Unicode normalization** (macOS stores filenames as decomposed NFD; most other
systems use composed NFC). `go-filewatcher` handles both automatically.

```go
watcher, err := filewatcher.New(
    []string{"./project"},
    // Auto-detects from the OS: case-insensitive on macOS/Windows,
    // case-sensitive on Linux. Override explicitly when needed:
    filewatcher.WithCaseSensitivity(filewatcher.CaseSensitivityAuto),
)
```

How it works:

- **NFC normalization is always on.** Every path key (`Add`, `Remove`, debounce,
  exclude-path, poll snapshot, and gitignore matching) is normalized to NFC, so
  an NFD path from the macOS filesystem and an NFC path from user input resolve
  to the same key. ASCII paths are unaffected (zero-cost fast path).
- **Case-sensitivity is configurable.** In case-insensitive mode, path keys are
  lowercased so `File.txt` and `file.txt` collide — preventing duplicate watches,
  missed `Remove()` calls, and case-only-rename phantom events.
- **Check the resolved mode at runtime:** `watcher.EffectiveCaseSensitivity()` returns the type-safe `FilesystemCaseSensitivity` enum, or `stats.CaseSensitivity` for the string form.

Filter-level wrappers (without changing the watcher-wide mode):

```go
// Case-insensitive: lowercase + NFC-normalize before the inner filter:
filter := filewatcher.FilterCaseInsensitive(filewatcher.FilterExtensions(".go"))

// Case-sensitive: NFC-normalize only (useful on macOS NFD paths):
filter := filewatcher.FilterCaseSensitive(filewatcher.FilterRegex(`^/Project/.*\.go$`))
```

## Observability

### Stats

```go
stats := watcher.Stats()
fmt.Printf("watching %d/%d paths (%.1f%% budget), %d add failures\n",
    stats.WatchCount, stats.WatchBudgetCap,
    stats.WatchBudgetUsed*100, stats.WatchErrors)
```

### Prometheus

The `PrometheusCollector` is dependency-free: it exposes `Counters()` and
`Gauges()` that you adapt to your own metrics registry. Here is a complete
adapter using the standard `prometheus/client_golang` package:

```go
import "github.com/prometheus/client_golang/prometheus"

// filewatcherCollector implements prometheus.Collector by delegating
// to filewatcher.PrometheusCollector. Prometheus calls Collect on each
// scrape — no polling loop needed.
type filewatcherCollector struct {
    coll  *filewatcher.PrometheusCollector
    descs map[string]*prometheus.Desc
}

func newFilewatcherCollector(coll *filewatcher.PrometheusCollector) *filewatcherCollector {
    descs := make(map[string]*prometheus.Desc)
    for _, c := range coll.Counters() {
        descs[c.Name] = prometheus.NewDesc(c.Name, c.Help, nil, nil)
    }
    for _, g := range coll.Gauges() {
        descs[g.Name] = prometheus.NewDesc(g.Name, g.Help, nil, nil)
    }
    return &filewatcherCollector{coll: coll, descs: descs}
}

func (f *filewatcherCollector) Describe(ch chan<- *prometheus.Desc) {
    for _, desc := range f.descs {
        ch <- desc
    }
}

func (f *filewatcherCollector) Collect(ch chan<- prometheus.Metric) {
    for _, c := range f.coll.Counters() {
        ch <- prometheus.MustNewConstMetric(f.descs[c.Name], prometheus.CounterValue, float64(c.Value))
    }
    for _, g := range f.coll.Gauges() {
        ch <- prometheus.MustNewConstMetric(f.descs[g.Name], prometheus.GaugeValue, g.Value)
    }
}

// Register once at startup — Prometheus scrapes on its own schedule:
coll := filewatcher.NewPrometheusCollector(watcher.Stats)
prometheus.MustRegister(newFilewatcherCollector(coll))
```

The `filewatcher_case_sensitivity` gauge encodes the resolved mode as a number:
`0` = case-sensitive, `1` = case-insensitive, `2` = auto (will resolve to 0 or 1
at runtime). Use this to alert when a watcher is unexpectedly in the wrong mode.

### OpenTelemetry

`OTelMiddleware` is zero-dependency: you provide an `OTelSpan` adapter that
bridges to the real `go.opentelemetry.io/otel/trace.Span`. Spans are created
per event, with `filewatcher.path`, `filewatcher.op`, and `filewatcher.is_dir`
attributes attached. Status is set to `ok` or `error` automatically.

```go
import (
    "context"

    "go.opentelemetry.io/otel"
    "go.opentelemetry.io/otel/attribute"
    "go.opentelemetry.io/otel/codes"
    "go.opentelemetry.io/otel/trace"
)

// otelSpanAdapter bridges trace.Span to filewatcher.OTelSpan.
type otelSpanAdapter struct {
    span trace.Span
}

func (a *otelSpanAdapter) End() { a.span.End() }

func (a *otelSpanAdapter) SetStatus(code, desc string) {
    if code == "error" {
        a.span.SetStatus(codes.Error, desc)
    } else {
        a.span.SetStatus(codes.Ok, desc)
    }
}

func (a *otelSpanAdapter) SetAttributes(attrs ...filewatcher.Attribute) {
    converted := make([]attribute.KeyValue, len(attrs))
    for i, attr := range attrs {
        converted[i] = attribute.String(attr.Key, attr.Value)
    }
    a.span.SetAttributes(converted...)
}

// Wire it into the watcher pipeline:
watcher, err := filewatcher.New(paths,
    filewatcher.WithMiddleware(
        filewatcher.OTelMiddleware(func(path, op string) filewatcher.OTelSpan {
            _, span := otel.Tracer("filewatcher").Start(
                context.Background(), "filewatcher.event",
            )
            return &otelSpanAdapter{span: span}
        }),
    ),
)
```

To see spans in a real exporter, initialize the OTel SDK with a stdout or
Jaeger exporter at program startup. Each file event will produce a span that
propagates to the configured exporter:

```go
import (
    "go.opentelemetry.io/otel"
    "go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
    sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// stdout exporter for development — pretty-prints spans as JSON:
exp, _ := stdouttrace.New(stdouttrace.WithPrettyPrint())
tp := sdktrace.NewTracerProvider(sdktrace.WithBatcher(exp))
otel.SetTracerProvider(tp)

// Each filewatcher event now produces a span you can see in the trace output.
```

## Benchmarks

Performance characteristics on Apple M2 (arm64):

| Benchmark               | Operations/sec | Time/op | Allocations |
| ----------------------- | -------------- | ------- | ----------- |
| `New/SinglePath`        | 53,822         | 30.9 µs | 18 allocs   |
| `New/WithOptions`       | 31,879         | 34.3 µs | 28 allocs   |
| `ConvertEvent/Create`   | 179,262        | 7.5 µs  | 3 allocs    |
| `ConvertEvent/Chmod`    | 178,305,804    | 10.8 ns | 0 allocs    |
| `PassesFilters/Single`  | 26,671,284     | 61.4 ns | 0 allocs    |
| `PassesFilters/Complex` | 2,325,330      | 595 ns  | 0 allocs    |
| `BuildMiddleware/None`  | 7,333,308      | 302 ns  | 2 allocs    |
| `BuildMiddleware/Three` | 1,000,000      | 1.37 µs | 11 allocs   |
| `Stats/Empty`           | 21,545,258     | 51.0 ns | 0 allocs    |
| `WatchList/Copy`        | 444,613        | 6.4 µs  | 1 alloc     |

Run benchmarks: `nix run .#bench` or `go test -bench=. -benchmem`

## Dependencies

| Dependency                                                                            | Purpose                                              |
| ------------------------------------------------------------------------------------- | ---------------------------------------------------- |
| [`fsnotify/fsnotify`](https://github.com/fsnotify/fsnotify)                           | Core file watching (v1.10.1)                         |
| [`LarsArtmann/gogenfilter/v3`](https://github.com/LarsArtmann/gogenfilter)            | Generated code detection (v3.6.1)                    |
| [`sabhiram/go-gitignore`](https://github.com/sabhiram/go-gitignore)                   | `.gitignore` pattern matching (zero transitive deps) |
| [`golang.org/x/text/unicode/norm`](https://pkg.go.dev/golang.org/x/text/unicode/norm) | NFC Unicode normalization in path keys               |
| [`golang.org/x/time/rate`](https://pkg.go.dev/golang.org/x/time/rate)                 | Token-bucket rate limiting for `MiddlewareThrottle`  |

## Design Decisions

- **Functional Options** — clean, extensible configuration API
- **Channel Streaming** — natural Go concurrency patterns, no callbacks
- **Middleware Chains** — composable cross-cutting concerns, applied in reverse order
- **Sentinel Errors** — `errors.Is()` for error checking, typed `WatcherError` with codes and categories
- **Context First** — `context.Context` for cancellation and timeouts
- **Composition** — filters and middleware compose elegantly with AND/OR/NOT
- **Minimal Dependencies** — only `fsnotify`, `gogenfilter`, `gitignore`, `x/text/norm`, and `x/time/rate`
- **Phantom Types** — `EventPath`, `RootPath`, `DebounceKey` for compile-time type safety

## Error Handling

All errors are wrapped and checkable with `errors.Is`:

```go
watcher, err := filewatcher.New(paths)
if errors.Is(err, filewatcher.ErrPathNotFound) {
    // Handle missing path
}
```

11 sentinel errors, 11 error codes, and structured `WatcherError` with transient/permanent categorization. Access runtime errors via `watcher.Errors()` channel.

## Development

This project uses [Nix Flakes](https://nixos.wiki/wiki/Flakes) for reproducible builds:

```bash
nix develop              # Enter development shell
nix run .#check          # Full quality: vet + lint + test
nix run .#ci             # Full CI: tidy + fmt + vet + lint + test
nix run .#lint-fix       # Auto-fix linter issues
nix run .#test           # Run tests with -race
nix flake check          # Run all quality gates
```

**Related docs:** [Features](./FEATURES.md) · [Roadmap](./ROADMAP.md) · [API Stability](./API_STABILITY.md) · [Troubleshooting](./Troubleshooting.md) · [Migration Guide](./MIGRATION.md) · [Changelog](./CHANGELOG.md)

## Examples

Runnable examples in the [`examples/`](./examples) directory:

```bash
go run ./examples/basic              # Simplest usage
go run ./examples/per-path-debounce   # Each file independently
go run ./examples/middleware          # Logging, recovery, metrics
```

## Related Projects

- **[go-daemon](https://github.com/LarsArtmann/go-daemon)** — building blocks for long-lived daemons served over unix domain sockets: socket lifecycle, `sd_notify` readiness/watchdog, graceful drain, JSON/CBOR/SSE. The natural companion when a watcher outgrows its process: start watching in its `Lifecycle.OnListen` hook and stop in `OnShutdown`, gate systemd readiness on established watches, stream `Event`s over the socket, and back a health endpoint with `Stats()`. Its systemd watchdog pairs well with `WithSelfHeal` — self-heal recovers transient failures in-process, the watchdog catches hard hangs.

## API Stability

This library follows [Go module versioning](https://go.dev/doc/modules/version-numbers). The core `New`/`Watch`/`Event` API is stable and unlikely to change. See [API_STABILITY.md](./API_STABILITY.md) for details.

## License

[MIT](LICENSE) &copy; Lars Artmann

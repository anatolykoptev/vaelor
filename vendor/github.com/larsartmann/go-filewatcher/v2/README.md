# go-filewatcher

[![Go Version](https://img.shields.io/badge/go-1.26.3+-blue.svg)](https://golang.org/dl/)
[![Go Reference](https://pkg.go.dev/badge/github.com/larsartmann/go-filewatcher/v2.svg)](https://pkg.go.dev/github.com/larsartmann/go-filewatcher/v2)
[![CI](https://github.com/larsartmann/go-filewatcher/actions/workflows/ci.yml/badge.svg)](https://github.com/larsartmann/go-filewatcher/actions)
[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

A high-performance, composable file system watcher for Go, built on [fsnotify](https://github.com/fsnotify/fsnotify). Eliminates the boilerplate of raw fsnotify usage with sensible defaults, automatic recursive watching, powerful filtering, and elegant middleware chains.

---

## ✨ Features

- **🎯 Zero Boilerplate** — Start watching with 5 lines of code
- **🌳 Automatic Recursion** — Subdirectories watched automatically, including newly created ones
- **⏱️ Smart Debouncing** — Global or per-path debouncing to handle rapid file changes
- **🔍 Powerful Filtering** — 13 built-in filters with AND/OR/NOT composition
- **🤖 Auto-Generated Code Detection** — Filter files from sqlc, protobuf, templ, etc. via gogenfilter
- **🔗 Middleware Chains** — Cross-cutting concerns (logging, recovery, metrics) via composable middleware
- **🎬 Context-Aware** — Graceful shutdown with Go's `context.Context`
- **⚡ High Performance** — Channel-based streaming, minimal allocations, race-safe
- **📦 Minimal Dependencies** — Only `fsnotify` (stdlib for everything else)
- **🧪 Battle Tested** — Comprehensive test suite with race detection

---

## 📑 Table of Contents

- [Installation](#installation)
- [Quick Start](#quick-start)
- [Configuration Options](#configuration-options)
- [Filters](#filters)
- [Middleware](#middleware)
- [Debounce Modes](#debounce-modes)
- [Event Types](#event-types)
- [Error Handling](#error-handling)
- [Advanced Usage](#advanced-usage)
- [Design Principles](#design-principles)
- [Examples](#examples)
- [License](#license)

---

## Installation

```bash
go get github.com/larsartmann/go-filewatcher/v2
```

Requires Go 1.26.3 or later.

### Development with Nix

This project uses [Nix Flakes](https://nixos.wiki/wiki/Flakes) for reproducible builds and development:

```bash
# Enter development shell (all tools included)
nix develop

# Or use direnv for automatic environment loading
direnv allow

# Run commands via Nix (no dev shell needed)
nix run .#check          # vet + lint + test
nix run .#ci             # full CI pipeline
nix run .#test           # run tests with -race
nix run .#lint           # run linter
nix run .#lint-fix       # auto-fix lint issues
nix flake check          # run all quality gates
nix build .              # validate reproducible build
```

---

## Quick Start

### Basic Usage

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
    // Create watcher with extensions filter and debounce
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

    // Start watching
    ctx := context.Background()
    events, err := watcher.Watch(ctx)
    if err != nil {
        log.Fatal(err)
    }

    // Process events
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
        filewatcher.MiddlewareRecovery(),   // Recover from panics
        filewatcher.MiddlewareLogging(nil), // Structured logging
    ),
)
```

### With Custom Filter

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

---

## Configuration Options

| Option                    | Description                                                          | Default                               |
| ------------------------- | -------------------------------------------------------------------- | ------------------------------------- |
| `WithDebounce(d)`         | Global debounce — all events coalesced into one emission after delay | `0` (disabled)                        |
| `WithPerPathDebounce(d)`  | Per-path debounce — each file debounced independently                | `0` (disabled)                        |
| `WithFilter(f)`           | Add a custom filter function                                         | —                                     |
| `WithExtensions(exts...)` | Only emit events for given file extensions                           | —                                     |
| `WithIgnoreDirs(dirs...)` | Discard events from given directory names                            | —                                     |
| `WithIgnoreHidden()`      | Discard events for hidden files/dirs (dot prefix)                    | `true` (dot dirs skipped during walk) |
| `WithRecursive(b)`        | Enable/disable recursive directory watching                          | `true`                                |
| `WithMiddleware(m...)`    | Add middleware to the event processing pipeline                      | —                                     |
| `WithErrorHandler(fn)`    | Set custom error handler for watcher errors                          | `stderr` logging                      |
| `WithSkipDotDirs(skip)`   | Skip directories starting with a dot during walking                  | `true`                                |
| `WithBuffer(size)`        | Event channel buffer size for handling bursts                        | `64`                                  |
| `WithOnAdd(fn)`           | Callback invoked when a new path is added to the watcher             | —                                     |

---

## Filters

Filters determine which events are emitted. Return `true` to keep, `false` to discard.

### Built-in Filters

| Filter                            | Description                            |
| --------------------------------- | -------------------------------------- |
| `FilterExtensions(exts...)`       | Only files with given extensions       |
| `FilterIgnoreExtensions(exts...)` | Exclude files with given extensions    |
| `FilterIgnoreDirs(dirs...)`       | Exclude files within given directories |
| `FilterIgnoreHidden()`            | Exclude hidden files/directories       |
| `FilterOperations(ops...)`        | Only given operation types             |
| `FilterNotOperations(ops...)`     | Exclude given operation types          |
| `FilterGlob(pattern)`             | Match file name against glob pattern   |
| `FilterRegex(pattern)`            | Match path against regex pattern       |
| `FilterMinSize(bytes)`            | Only files ≥ given size                |

### Composition Filters

| Filter                  | Description                        |
| ----------------------- | ---------------------------------- |
| `FilterAnd(filters...)` | All filters must pass (AND)        |
| `FilterOr(filters...)`  | At least one filter must pass (OR) |
| `FilterNot(filter)`     | Invert the filter (NOT)            |

### Filter Examples

```go
// Only .go files, excluding vendor
filter := filewatcher.FilterAnd(
    filewatcher.FilterExtensions(".go"),
    filewatcher.FilterNot(filewatcher.FilterIgnoreDirs("vendor")),
)

// Either .go or .md files
goOrMd := filewatcher.FilterOr(
    filewatcher.FilterExtensions(".go"),
    filewatcher.FilterExtensions(".md"),
)

// Only write and create operations
writeOrCreate := filewatcher.FilterOperations(
    filewatcher.Write,
    filewatcher.Create,
)

// Match files by glob
logsOnly := filewatcher.FilterGlob("*.log")

// Minimum file size (1KB+)
largeFiles := filewatcher.FilterMinSize(1024)

// Complex: .go files, not in vendor, not hidden, write/create only
complexFilter := filewatcher.FilterAnd(
    filewatcher.FilterExtensions(".go"),
    filewatcher.FilterNot(filewatcher.FilterIgnoreDirs("vendor", "node_modules")),
    filewatcher.FilterNot(filewatcher.FilterIgnoreHidden()),
    filewatcher.FilterOperations(filewatcher.Write, filewatcher.Create),
)
```

### Filter Generated Code (gogenfilter)

Use the `FilterGeneratedCode` filter to automatically exclude auto-generated Go files from events. This integrates with [gogenfilter](https://github.com/LarsArtmann/gogenfilter) to detect files from common generators:

| Generator | Detection Pattern                                     |
| --------- | ----------------------------------------------------- |
| sqlc      | `models.go`, `querier.go`, `*.sql.go`                 |
| templ     | `*_templ.go`                                          |
| go-enum   | `*_enum.go`                                           |
| protobuf  | `*.pb.go`, `*_grpc.pb.go`                             |
| mockgen   | `*_mock.go`, `mock_*.go`                              |
| stringer  | Content detection (`// Code generated by "stringer"`) |
| Generic   | Content detection (`// Code generated by ...`)        |

```go
import "github.com/LarsArtmann/gogenfilter"

// Filter all generated code types
watcher, _ := filewatcher.New("./src",
    filewatcher.WithFilter(filewatcher.FilterGeneratedCode()),
)

// Filter specific generators only
watcher, _ := filewatcher.New("./src",
    filewatcher.WithFilter(filewatcher.FilterGeneratedCode(
        gogenfilter.FilterSQLC,
        gogenfilter.FilterProtobuf,
    )),
)

// Combine with other filters
filter := filewatcher.FilterAnd(
    filewatcher.FilterExtensions(".go"),
    filewatcher.FilterGeneratedCode(),  // Exclude generated .go files
)
```

### Filter Composition Examples

```go
// Only .go files, excluding generated code AND vendor
cleanFilter := filewatcher.FilterAnd(
    filewatcher.FilterExtensions(".go"),
    filewatcher.FilterNot(filewatcher.FilterIgnoreDirs("vendor")),
    filewatcher.FilterGeneratedCode(),  // Auto-excludes sqlc, protobuf, etc.
)

// Combine with other filters using FilterOr
goOrTempl := filewatcher.FilterOr(
    filewatcher.FilterExtensions(".go"),
    filewatcher.FilterGlob("*_templ.go"),  // Explicitly watch templ files
)
```

---

## Middleware

Middleware wraps event handlers for cross-cutting concerns. Applied in **reverse order** (last added runs first).

### Built-in Middleware

| Middleware                       | Description                                   |
| -------------------------------- | --------------------------------------------- |
| `MiddlewareLogging(logger)`      | Log all events with structured logging (slog) |
| `MiddlewareRecovery()`           | Recover from panics, log stack trace          |
| `MiddlewareRateLimit(maxEvents)` | Limit to maxEvents events per second          |
| `MiddlewareFilter(filter)`       | Filter events (same as WithFilter)            |
| `MiddlewareOnError(handler)`     | Handle errors from downstream handlers        |
| `MiddlewareMetrics(counter)`     | Count processed events by operation           |
| `MiddlewareWriteFileLog(path)`   | Write events to file for audit trail          |

### Middleware Examples

```go
// Basic: logging + recovery
watcher, _ := filewatcher.New(paths,
    filewatcher.WithMiddleware(
        filewatcher.MiddlewareRecovery(),
        filewatcher.MiddlewareLogging(nil),
    ),
)

// With metrics
var createCount, writeCount atomic.Int64

watcher, _ := filewatcher.New(paths,
    filewatcher.WithMiddleware(
        filewatcher.MiddlewareRecovery(),
        filewatcher.MiddlewareLogging(nil),
        filewatcher.MiddlewareMetrics(func(op filewatcher.Op) {
            switch op {
            case filewatcher.Create:
                createCount.Add(1)
            case filewatcher.Write:
                writeCount.Add(1)
            }
        }),
    ),
)

// Rate limiting (max 1 event per second)
watcher, _ := filewatcher.New(paths,
    filewatcher.WithMiddleware(
        filewatcher.MiddlewareRateLimit(100),
    ),
)

// Audit logging to file
watcher, _ := filewatcher.New(paths,
    filewatcher.WithMiddleware(
        filewatcher.MiddlewareWriteFileLog("/var/log/filewatcher.log"),
    ),
)

// Custom error handling
watcher, _ := filewatcher.New(paths,
    filewatcher.WithMiddleware(
        filewatcher.MiddlewareOnError(func(event filewatcher.Event, err error) {
            slog.Error("event processing failed",
                "path", event.Path,
                "error", err,
            )
        }),
    ),
)
```

---

## Debounce Modes

### Global Debounce (`WithDebounce`)

All events are coalesced into a single emission after the delay since the last event.

**Use case:** Build systems, test runners — you want to trigger once after a burst of changes.

```go
// Wait 500ms after last event, then emit once
filewatcher.WithDebounce(500 * time.Millisecond)
```

### Per-Path Debounce (`WithPerPathDebounce`)

Each file path is debounced independently.

**Use case:** Hot reloading where each file triggers its own reload.

```go
// Each file emits independently after 500ms since its last change
filewatcher.WithPerPathDebounce(500 * time.Millisecond)
```

### No Debounce

Events are emitted immediately (may cause high frequency for rapid changes).

---

## Event Types

```go
type Event struct {
    Path      string    // Absolute path of changed file/directory
    Op        Op        // Operation type
    Timestamp time.Time // When the event was detected
    IsDir     bool      // True if directory, false if file
    Size      int64     // File size in bytes (0 if unavailable)
    ModTime   time.Time // File modification time (zero if unavailable)
}
```

### Operations

| Op       | Description               |
| -------- | ------------------------- |
| `Create` | File or directory created |
| `Write`  | File modified             |
| `Remove` | File or directory removed |
| `Rename` | File or directory renamed |

**Note:** Event priority when multiple operations occur: `Create` > `Write` > `Remove` > `Rename`.

### Event Methods

```go
event.String()              // "CREATE /path/to/file at 2026-01-15T10:30:00Z"
event.Op.String()           // "CREATE", "WRITE", "REMOVE", "RENAME"

// JSON marshaling supported
data, _ := json.Marshal(event)
```

---

## Error Handling

All errors are returned explicitly (no panics). Sentinel errors for common cases:

```go
var (
    ErrWatcherClosed = errors.New("watcher is closed")
    ErrNoPaths       = errors.New("at least one path is required")
    ErrPathNotFound  = errors.New("path not found")
    ErrPathNotDir    = errors.New("path is not a directory")
    ErrWatcherRunning = errors.New("watcher is already running")
    ErrUnknownOp     = errors.New("unknown operation")
)
```

### Error Handling Example

```go
watcher, err := filewatcher.New(paths)
if err != nil {
    if errors.Is(err, filewatcher.ErrPathNotFound) {
        log.Printf("Path not found: %v", err)
    } else {
        log.Fatalf("Failed to create watcher: %v", err)
    }
}

// Set error handler for runtime errors
watcher, _ = filewatcher.New(paths,
    filewatcher.WithErrorHandler(func(err error) {
        slog.Error("watcher error", "error", err)
    }),
)
```

---

## Advanced Usage

### Dynamic Path Management

```go
watcher, _ := filewatcher.New([]string{"./src"})

// Add paths dynamically
if err := watcher.Add("./extra"); err != nil {
    log.Printf("Failed to add path: %v", err)
}

// Remove paths
if err := watcher.Remove("./src/old"); err != nil {
    log.Printf("Failed to remove path: %v", err)
}

// Get currently watched paths
paths := watcher.WatchList()

// Get statistics
stats := watcher.Stats()
fmt.Printf("Watching %d paths\n", stats.WatchCount)
```

### Context Cancellation

```go
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()

events, _ := watcher.Watch(ctx)

// Process events until context is cancelled
for event := range events {
    // Handle event
}
// Channel is closed, watcher stopped
```

### Custom Middleware

```go
func MyMiddleware(next filewatcher.Handler) filewatcher.Handler {
    return func(ctx context.Context, event filewatcher.Event) error {
        // Before processing
        start := time.Now()

        err := next(ctx, event)

        // After processing
        duration := time.Since(start)
        fmt.Printf("Processed %s in %v\n", event.Path, duration)

        return err
    }
}

watcher, _ := filewatcher.New(paths,
    filewatcher.WithMiddleware(MyMiddleware),
)
```

### Safe Defaults Reference

```go
// Common directories to ignore
filewatcher.DefaultIgnoreDirs
// []string{
//     ".git", ".hg", ".svn",
//     "vendor", "node_modules",
//     "dist", "build", "bin", "out",
//     "__pycache__", ".cache",
// }
```

### Structured Logging

```go
import "log/slog"

// Create a custom JSON logger for production
logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
    Level: slog.LevelInfo,
}))

watcher, _ := filewatcher.New(paths,
    filewatcher.WithMiddleware(
        filewatcher.MiddlewareRecovery(),
        filewatcher.MiddlewareLogging(logger), // Uses your custom logger
    ),
)
```

### Dependency Injection Patterns

go-filewatcher integrates naturally with Go DI frameworks:

#### Manual DI (Constructor Injection)

```go
type FileProcessor struct {
    watcher *filewatcher.Watcher
}

func NewFileProcessor(dirs []string, logger *slog.Logger) (*FileProcessor, error) {
    w, err := filewatcher.New(dirs,
        filewatcher.WithExtensions(".go"),
        filewatcher.WithMiddleware(
            filewatcher.MiddlewareRecovery(),
            filewatcher.MiddlewareLogging(logger),
        ),
    )
    if err != nil {
        return nil, fmt.Errorf("creating watcher: %w", err)
    }

    return &FileProcessor{watcher: w}, nil
}

func (fp *FileProcessor) Run(ctx context.Context) error {
    defer fp.watcher.Close()

    events, err := fp.watcher.Watch(ctx)
    if err != nil {
        return err
    }

    for event := range events {
        // Process events
    }

    return nil
}
```

#### Interface-based Testing

```go
// Define an interface for the watcher dependency
type Watcher interface {
    Watch(ctx context.Context) (<-chan filewatcher.Event, error)
    Close() error
}

// Production implementation
func NewWatcher(paths []string, opts ...filewatcher.Option) (Watcher, error) {
    return filewatcher.New(paths, opts...)
}

// Test mock
type mockWatcher struct {
    events chan filewatcher.Event
}

func (m *mockWatcher) Watch(_ context.Context) (<-chan filewatcher.Event, error) {
    return m.events, nil
}

func (m *mockWatcher) Close() error {
    close(m.events)
    return nil
}
```

### Polling Mode (NFS/Docker Volumes)

```go
// For environments where OS-native events don't work
watcher, _ := filewatcher.New(
    []string{"/mnt/nfs/share"},
    filewatcher.WithPolling(true),
    filewatcher.WithPollInterval(2 * time.Second),
)
```

---

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

---

## Design Principles

- **Functional Options** — Clean, extensible configuration API
- **Sentinel Errors** — `errors.Is()` for error checking
- **No Panics** — Explicit error handling throughout
- **Context First** — `context.Context` for cancellation and timeouts
- **Channel Streaming** — Natural Go concurrency patterns
- **Middleware Chains** — Composable cross-cutting concerns
- **Composition** — Filters and middleware compose elegantly
- **Minimal Dependencies** — Only `fsnotify`, stdlib for rest

**Related docs:** [API Stability](./API_STABILITY.md) · [Troubleshooting](./Troubleshooting.md) · [Migration Guide](./MIGRATION.md)

---

## Examples

Runnable examples in the [`examples/`](./examples) directory:

```bash
# Basic usage with extensions and debounce
go run ./examples/basic

# Per-path debounce (each file independently)
go run ./examples/per-path-debounce

# Middleware chain (logging, recovery, metrics)
go run ./examples/middleware

# Filter auto-generated code (sqlc, protobuf, templ, etc.)
go run ./examples/filter-generated
```

| Example                                           | Description                                               |
| ------------------------------------------------- | --------------------------------------------------------- |
| [basic](./examples/basic)                         | Simplest usage with extensions filter and global debounce |
| [per-path-debounce](./examples/per-path-debounce) | Each file debounced independently                         |
| [middleware](./examples/middleware)               | Logging, recovery, and metrics middleware                 |
| [filter-generated](./examples/filter-generated)   | Exclude auto-generated Go files from events               |

---

## License

MIT — See [LICENSE](LICENSE) file for details.

Copyright © 2026 Lars Artmann.

---

<div align="center">

**Made with ❤️ for Go developers**

[Report Issue](https://github.com/larsartmann/go-filewatcher/issues) · [View Documentation](https://pkg.go.dev/github.com/larsartmann/go-filewatcher/v2)

</div>

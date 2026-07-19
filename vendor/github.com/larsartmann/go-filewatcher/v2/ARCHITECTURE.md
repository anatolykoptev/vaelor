# Architecture: go-filewatcher

**Overview:** High-performance, composable file system watcher for Go built on fsnotify.

## Core Design Principles

1. **Zero Boilerplate** — Start watching with 5 lines of code
2. **Automatic Recursion** — Subdirectories watched automatically
3. **Composability** — Filters and middleware compose elegantly
4. **Type Safety** — Phantom types prevent mixing path representations
5. **Thread Safety** — All public methods are safe for concurrent use

## Architecture Layers

```
┌─────────────────────────────────────────────────────────────┐
│                    Public API Layer                        │
│  New()  Watch()  Add()  Remove()  Close()  Stats()         │
└─────────────────────────────────────────────────────────────┘
                              │
┌─────────────────────────────────────────────────────────────┐
│                   Event Processing Layer                   │
│  watchLoop → processEvent → passesFilters → emitEvent      │
│                    ↓              ↓                        │
│         handleNewDirectory    Middleware Chain             │
│                                     ↓                      │
│                               Debouncer                    │
└─────────────────────────────────────────────────────────────┘
                              │
┌─────────────────────────────────────────────────────────────┐
│                    File System Layer                       │
│  fsnotify.Watcher → Events/Errors → convertEvent()         │
│                    ↓                                       │
│  Recursive walking via addPath()                          │
└─────────────────────────────────────────────────────────────┘
```

## Key Components

### Watcher

The central struct coordinating all operations:

- **Configuration**: paths, filters, middleware, debounce settings
- **State Management**: Bit flags for closed/watching states
- **Concurrency Control**: RWMutex for state, WaitGroup for emissions
- **Resource Management**: eventCh, debouncer, error channel

### Event Pipeline

```
fsnotify.Event
      ↓
convertEvent() → *Event (nil for Chmod)
      ↓
passesFilters() → bool
      ↓
handleNewDirectory() (if Create)
      ↓
emitEvent() → middleware chain → debouncer → eventCh
```

### Filter System

**Type**: `func(Event) bool`

Filters are pure functions that determine which events are emitted:

- Return `true` to keep the event
- Return `false` to discard the event
- Compose with `FilterAnd()`, `FilterOr()`, `FilterNot()`

**Built-in Filters**:

- `FilterExtensions()` — File extension matching
- `FilterIgnoreDirs()` — Directory name exclusion
- `FilterOperations()` — Operation type filtering
- `FilterRegex()` — Pattern matching
- `FilterMinSize()` / `FilterMaxSize()` — Size-based
- `FilterModifiedSince()` / `FilterMinAge()` — Time-based

### Middleware Chain

**Type**: `func(Handler) Handler`

Middleware wraps handlers for cross-cutting concerns:

- Applied in **reverse order** (last added runs first)
- Can transform events, log, recover panics, rate limit
- Chain ends with the emit function that sends to eventCh

**Built-in Middleware**:

- `MiddlewareRecovery()` — Panic recovery
- `MiddlewareLogging()` — Structured logging
- `MiddlewareRateLimit()` / `MiddlewareRateLimitWindow()` — Rate limiting
- `MiddlewareMetrics()` — Event counting
- `MiddlewareBatch()` — Event batching

### Debouncing

Two strategies for handling rapid file changes:

**GlobalDebouncer**: All events coalesced into one callback
**Debouncer**: Per-key debouncing (e.g., per-file-path)

Both use `sync.WaitGroup` to track in-flight callbacks during shutdown.

## Concurrency Model

### Goroutines

1. **watchLoop**: Main event processing (1 per Watch call)
2. **debouncer timers**: time.AfterFunc goroutines
3. **test goroutines**: Parallel test execution

### Synchronization

- **w.mu**: Protects state, watchList, eventCh
- **emitWg**: Waits for event emissions before close
- **debouncer.wg**: Waits for debounced callbacks
- **errorsMu**: Protects error channel initialization

### State Machine

```
[Created] → Watch() → [Watching] → Close() → [Closed]
   ↑                                    |
   └────────────────────────────────────┘ (idempotent)
```

## Phantom Types

Type-safe wrappers to prevent mixing different path representations:

```go
type EventPath string    // Absolute validated path in events
type RootPath string     // Absolute validated path for watching
```

Usage:

```go
path := filewatcher.GetPath(event)  // Returns EventPath
// Cannot accidentally use EventPath where RootPath is expected
```

## Error Handling

### Sentinel Errors

```go
var ErrWatcherClosed  = errors.New("watcher is closed")
var ErrNoPaths        = errors.New("at least one path is required")
var ErrPathNotFound   = errors.New("path not found")
var ErrWatcherRunning = errors.New("watcher is already running")
```

### Error Context

All errors are wrapped with context using `fmt.Errorf("...: %w", err)`.

### Error Channels

Two ways to receive errors:

1. **ErrorHandler callback**: Synchronous during event processing
2. **Errors() channel**: Asynchronous error stream

## Memory Layout

### Watcher Struct

```go
type Watcher struct {
    // fsnotify wrapper (8 bytes)
    fswatcher *fsnotify.Watcher

    // Configuration (slice headers + values)
    paths           []string    // 24 bytes
    filters         []Filter    // 24 bytes
    middleware      []Middleware // 24 bytes

    // Settings (packed)
    globalDebounce  time.Duration  // 8 bytes
    perPathDebounce time.Duration  // 8 bytes
    bufferSize      int            // 8 bytes

    // Callbacks (function pointers)
    onAdd           func(string)   // 8 bytes
    errorHandler    ErrorHandler   // 8 bytes

    // State (1 byte bit flags)
    state           WatcherStateFlags

    // Synchronization
    mu              sync.RWMutex   // 24 bytes
    emitWg          sync.WaitGroup // 12 bytes
    errorsMu        sync.Mutex     // 8 bytes
}
```

Total: ~200 bytes per watcher (excluding slices and fsnotify internals)

## Performance Characteristics

### Event Processing

- **Allocation**: 1 allocation per event (Event struct)
- **Channel**: Buffered with configurable size (default 64)
- **Filters**: O(n) where n = number of filters
- **Middleware**: O(m) where m = middleware chain depth

### Debouncing

- **Global**: O(1) timer management
- **Per-path**: O(1) map lookup + timer management

### Memory

- **Watcher**: ~200 bytes base + slices
- **Per watched path**: ~50 bytes + fsnotify overhead
- **Per debounce entry**: ~40 bytes + timer

### Scalability

- Tested with 10k+ files
- Channel backpressure for burst handling
- Lock-free reads for Stats(), IsClosed(), IsWatching()

## Testing Strategy

### Unit Tests

- Individual component testing (filters, middleware, debouncer)
- Table-driven tests for filter/middleware combinations
- Mock fsnotify for isolated testing

### Integration Tests

- Full Watch → Event → Close lifecycle
- Recursive directory watching
- Per-path debounce correctness
- Race condition detection with `-race`

### Benchmarks

- Event throughput: ~100k events/sec
- Memory allocation: <1 alloc per filtered event
- Startup time: <10ms for 1000 files

## Security Considerations

1. **Path Validation**: All paths converted to absolute before use
2. **Symlinks**: Not followed by default (fsnotify behavior)
3. **Resource Limits**: Configurable buffer size prevents memory exhaustion
4. **Panic Recovery**: MiddlewareRecovery catches handler panics

## Extension Points

### Adding a Filter

```go
func FilterCustom(pattern string) Filter {
    return func(e Event) bool {
        // Return true to keep event
        return matchesPattern(e.Path, pattern)
    }
}
```

### Adding Middleware

```go
func MiddlewareCustom() Middleware {
    return func(next Handler) Handler {
        return func(ctx context.Context, e Event) error {
            // Before next handler
            err := next(ctx, e)
            // After next handler
            return err
        }
    }
}
```

## Dependencies

- `github.com/fsnotify/fsnotify` — Core file watching
- Standard library only (no external dependencies for filters/middleware)

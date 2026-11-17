# Troubleshooting

Common issues and solutions when using go-filewatcher.

## Table of Contents

- [No Events Received](#no-events-received)
- [Duplicate Events](#duplicate-events)
- [High CPU Usage](#high-cpu-usage)
- [Memory Growth](#memory-growth)
- [Events on NFS/Docker Volumes](#events-on-nfsdocker-volumes)
- [Too Many Events](#too-many-events)
- [Watcher Won't Start](#watcher-wont-start)
- [Race Detector Warnings](#race-detector-warnings)
- [Platform-Specific Issues](#platform-specific-issues)
- [Filesystem Compatibility](#filesystem-compatibility)
- [Polling Mode Limitations](#polling-mode-limitations)
- [Slow Consumers and Event Loss](#slow-consumers-and-event-loss)
- [Middleware Drops Not Visible](#middleware-drops-not-visible)
- [Content Hashing Performance](#content-hashing-performance)
- [Symlink Cycles](#symlink-cycles)

## No Events Received

**Symptoms:** `Watch()` returns a channel but no events arrive.

**Check:**

1. Is the watcher started? Call `IsWatching()` after `Watch()`.
2. Are you watching the correct path? Use `WatchList()` to verify.
3. Does a filter reject your events? Try without filters first:

```go
watcher, _ := filewatcher.New([]string{"./testdata"})
// No filters — all events pass through
```

4. Is the context cancelled? Ensure your context has a sufficient timeout.

5. Are you modifying files programmatically? Some editors use atomic saves
   (write to temp, rename) which produce `Rename` + `Create` instead of `Write`.

## Duplicate Events

**Symptoms:** The same file change triggers multiple events.

**Cause:** Most OS file watchers (inotify, FSEvents) emit multiple events for
a single logical change. For example, saving a file in an editor may produce
`Create` + `Write` or `Write` + `Write`.

**Solution:** Use debouncing:

```go
// Global debounce — coalesce all events within 300ms
filewatcher.WithDebounce(300 * time.Millisecond)

// Per-path debounce — each file gets its own debounce window
filewatcher.WithPerPathDebounce(300 * time.Millisecond)
```

Or use the deduplication middleware:

```go
filewatcher.WithMiddleware(
    filewatcher.MiddlewareDeduplicate(200 * time.Millisecond),
)
```

## High CPU Usage

**Symptoms:** The watcher process uses significant CPU even when idle.

**Causes and solutions:**

1. **Watching too many directories:** Use `WithRecursive(false)` or
   `WithIgnoreDirs("vendor", "node_modules")` to limit scope.

2. **Busy event loop with no debounce:** Add debouncing to reduce processing.

3. **Tight polling:** If using `WithPolling(true)`, increase `WithPollInterval`
   (default is 2s).

## Memory Growth

**Symptoms:** Memory usage grows over time.

**Cause:** The deduplication middleware keeps a map of recent events. If the
map grows very large (>10,000 entries), automatic cleanup runs but may not
keep up with extremely high event rates.

**Solution:** Use a shorter deduplication window or add filters to reduce
event volume:

```go
filewatcher.WithMiddleware(
    filewatcher.MiddlewareDeduplicate(50 * time.Millisecond),
)
```

## Events on NFS/Docker Volumes

**Symptoms:** No events on network filesystems, Docker bind mounts, or
FUSE filesystems.

**Cause:** OS-native file watchers (inotify, kqueue, FSEvents) do not detect
changes on network filesystems.

**Solution:** Enable polling mode:

```go
watcher, _ := filewatcher.New(
    []string{"/mnt/nfs/share"},
    filewatcher.WithPolling(true),              // Enable polling fallback
    filewatcher.WithPollInterval(2 * time.Second), // Optional: customize interval
)
```

## Too Many Events

**Symptoms:** Event channel is overwhelmed during bulk operations (git checkout,
build, etc.).

**Solutions:**

1. **Rate limiting:**

```go
filewatcher.WithMiddleware(
    filewatcher.MiddlewareRateLimit(100), // 100 events/sec max
)
```

2. **Filter by extension:**

```go
filewatcher.WithExtensions(".go", ".md")
```

3. **Ignore directories:**

```go
filewatcher.WithIgnoreDirs("vendor", "node_modules", ".git", "dist")
```

4. **Larger buffer:**

```go
filewatcher.WithBuffer(256) // Default is 64
```

## Watcher Won't Start

**Error: "path not found"**

The path must exist and be a directory:

```go
// Wrong: file path
filewatcher.New([]string{"./config.yaml"})

// Correct: directory path
filewatcher.New([]string{"./config"})
```

**Error: "watcher is already running"**

`Watch()` was called twice without `Close()`. Use a single `Watch()` call and
consume events from the channel:

```go
events, _ := watcher.Watch(ctx)
for event := range events {
    handleEvent(event)
}
```

## Race Detector Warnings

**Symptoms:** `go test -race` reports data races.

If you see races in your own code consuming events, ensure:

- You don't call `Watch()` from multiple goroutines on the same watcher.
- You use proper synchronization when sharing state between event handlers
  and other goroutines.

go-filewatcher itself is tested with `-race` and should not produce races.
If you find one, please file a bug report.

## Platform-Specific Issues

### Linux (inotify)

- **"too many open files"** or **"no space left on device"**: Increase
  inotify watches: `echo fs.inotify.max_user_watches=524288 | sudo tee -a /etc/sysctl.conf`

### macOS (FSEvents)

- FSEvents may coalesce rapid changes into a single event.
- Some editors trigger `Rename` instead of `Write` due to atomic save.

### Windows

- Long paths (>260 chars) may cause issues. Use UNC paths (`\\?\C:\...`).
- Network drives may not emit events. Use `WithPolling(true)`.

## Filesystem Compatibility

### Wrong Events or Missing Matches on macOS

**Symptoms:** Exclude paths don't match, gitignore rules are silently bypassed,
or debounce doesn't coalesce events for files with non-ASCII names (e.g., `café`,
`München`).

**Cause:** macOS stores filenames as NFD (decomposed Unicode) but most
user-configured paths are NFC (composed). Additionally, APFS is
case-insensitive by default. The watcher handles both transparently via
`pathKey()`, which applies NFC normalization and optional case-folding to all
internal path comparisons.

**Check:**

```go
// Verify the resolved mode
stats := watcher.Stats()
fmt.Println(stats.CaseSensitivity) // "case-insensitive" on macOS
```

**Fix:** The default `CaseSensitivityAuto` handles this automatically. For
non-default filesystems (e.g., case-sensitive APFS), override explicitly:

```go
filewatcher.WithCaseSensitivity(filewatcher.CaseSensitive)
```

### No Events on NFS/Docker Volumes

**Symptoms:** `Watch()` succeeds but no events arrive from files modified
through NFS mounts, Docker bind-mounts, or FUSE filesystems.

**Cause:** These filesystems may not support inotify/fsnotify events.

**Fix:** Enable polling mode:

```go
filewatcher.WithPolling(true),
filewatcher.WithPollInterval(500 * time.Millisecond),
```

### Case-Only Rename Produces Phantom Events

**Symptoms:** On case-insensitive filesystems (NTFS, APFS), renaming `File.go`
to `file.go` produces Create + Remove events instead of being recognized as the
same file.

**Status:** Fixed. The poll loop snapshot now uses canonical pathKeys for map
comparison, so case-only renames don't trigger phantom events. Ensure
`WithCaseSensitivity(CaseInsensitive)` is set (default on macOS/Windows).

### Gitignored Directory Still Gets Watched

**Symptoms:** A `.gitignore` with `Build/` does not prevent the `Build`
directory from being added to the watch list.

**Cause:** The `github.com/sabhiram/go-gitignore` library returns
`MatchesPath("Build") == false` for a trailing-slash directory pattern (`Build/`).
It only matches paths _inside_ the directory (`Build/foo`). This is library
behavior, not a bug in go-filewatcher.

**Fix:** Use the pattern without the trailing slash for directories that should
be skipped entirely during walk:

```gitignore
# Instead of:
Build/

# Use:
Build
```

The trailing-slash form still works for filtering _events_ from files inside the
directory — it just doesn't prevent the directory itself from being watched.

## Polling Mode Limitations

**Polling is a fallback, not a duplicate-suppression layer.** When both
`WithPolling(true)` and fsnotify are active, a single file change can produce
two events: one from fsnotify and one from the poll loop. Use
`MiddlewareDeduplicate` or rely on idempotent handling if this matters.

As of v2.4.0, `WithExcludePaths` and `.gitignore` filtering now apply to the
poll loop as well, consistent with the initial walk. Previously, the poll loop
ignored exclusions and could emit events for gitignored or excluded subtrees.

## Slow Consumers and Event Loss

**Symptoms:** Under high event load, the consumer falls behind. The event
channel fills and the watch loop blocks, which can cause the kernel to drop
fsnotify events with no indication.

**Default behavior:** The event channel uses blocking sends. This preserves
backpressure but can stall the entire pipeline if the consumer is slow.

**Solution — DropOnFull mode:** Prefer losing events over blocking:

```go
filewatcher.WithEventChannelMode(filewatcher.EventChannelDropOnFull),
```

Dropped events are counted in `Stats.EventsDroppedByBackpressure`. Check this
counter to detect consumer overload:

```go
stats := watcher.Stats()
if stats.EventsDroppedByBackpressure > 0 {
    log.Warn("events dropped due to slow consumer", "count", stats.EventsDroppedByBackpressure)
}
```

## Middleware Drops Not Visible

**Symptoms:** `Stats.EventsProcessed` is higher than expected. Events dropped
by rate limiting, deduplication, or circuit breaker middleware are not visible
in any counter.

**As of v2.4.0:** Middleware-dropped events are now counted separately in
`Stats.EventsDroppedByMiddleware`. This counter tracks events that passed
filters but were dropped by middleware (returned `nil` without forwarding).

Similarly, errors dropped because the error channel (`Errors()`) was full are
counted in `Stats.ErrorsDropped`. If this counter is non-zero, increase
`WithBuffer(n)` or read errors more frequently.

## Content Hashing Performance

**Symptoms:** High I/O latency when `WithContentHashing()` or
`FilterGeneratedCodeFull(ContentCheckEnabled)` is enabled, especially with
large files.

**Cause:** Both features read file content synchronously from the event loop.
A burst of large file events can delay all subsequent events.

**Mitigations:**

1. Content hashing skips files larger than 10 MiB (returns empty hash).
2. Generated code content detection also skips files larger than 10 MiB.
3. For high-throughput scenarios, prefer filename-only detection
   (`FilterGeneratedCode` or `ContentCheckDisabled`).
4. Disable content hashing entirely if you don't need it.

## Symlink Cycles

**Symptoms:** `WithFollowSymlinks(true)` follows a symlink that points to an
ancestor directory, causing an infinite walk.

**As of v2.4.0:** Cycle detection is built-in. The walker tracks all resolved
symlink targets and skips any target that was already visited (directly or via
another symlink). A debug log message is emitted when a cycle is detected.

You can safely use `WithFollowSymlinks(true)` without worrying about cycles.
The walker also deduplicates watch-list entries for symlink targets that are
already watched under their real path.

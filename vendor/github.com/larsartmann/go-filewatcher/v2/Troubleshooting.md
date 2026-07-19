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

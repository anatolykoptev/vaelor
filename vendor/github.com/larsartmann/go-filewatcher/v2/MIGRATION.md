# Migration Guide

This document provides guidance for migrating between major versions of go-filewatcher.

---

## Migrating from v1.x to v2.0

### Breaking Change: ErrorHandler Signature

The `ErrorHandler` type signature has changed to provide richer error context.

#### Before (v1.x)

```go
watcher, _ := filewatcher.New(
    []string{"./src"},
    filewatcher.WithErrorHandler(func(err error) {
        log.Printf("Error: %v", err)
    }),
)
```

#### After (v2.0)

```go
watcher, _ := filewatcher.New(
    []string{"./src"},
    filewatcher.WithErrorHandler(func(ctx filewatcher.ErrorContext, err error) {
        log.Printf("[%s] Error on %s: %v", ctx.Operation, ctx.Path, err)
        if ctx.Retryable {
            log.Printf("  (this error may resolve on retry)")
        }
    }),
)
```

### New ErrorContext Fields

| Field       | Type     | Description                           |
| ----------- | -------- | ------------------------------------- |
| `Operation` | `string` | The operation being performed         |
| `Path`      | `string` | The file path involved (if any)       |
| `Event`     | `*Event` | The event being processed (if any)    |
| `Retryable` | `bool`   | Whether retry might resolve the error |

### Error Categories

Errors are now automatically categorized:

```go
// Check if an error is transient (might resolve on retry)
if filewatcher.IsTransientError(err) {
    // Consider retry logic
}

// Check if an error is permanent (won't resolve)
if filewatcher.IsPermanentError(err) {
    // Log and alert, no retry needed
}
```

### Sentinel Errors

All sentinel errors remain the same:

- `ErrWatcherClosed`
- `ErrNoPaths`
- `ErrPathNotFound`
- `ErrPathNotDir`
- `ErrWatcherRunning`
- `ErrUnknownOp`
- `ErrFsnotifyFailed`
- `ErrWalkFailed`
- `ErrPathResolveFailed`
- `ErrEventProcessingFailed`
- `ErrMiddlewareFailed`

These can still be checked with `errors.Is()`:

```go
if errors.Is(err, filewatcher.ErrWatcherClosed) {
    // Handle closed watcher
}
```

---

## Other Changes in v2.0

### Watcher State Flags

The internal `Watcher` struct now uses bit flags for state management:

- `closed` and `watching` are now stored as bit flags
- Memory usage reduced from 4 bytes to 1 byte for state fields
- No API changes - this is purely internal

### New Features

- **Generated Code Filtering**: New `FilterGeneratedCode()` function to exclude auto-generated files
- **Nix Support**: Development environment via `flake.nix`

---

## Quick Reference

| Aspect               | v1.x               | v2.0                                       |
| -------------------- | ------------------ | ------------------------------------------ |
| ErrorHandler         | `func(error)`      | `func(ErrorContext, error)`                |
| Error information    | Error message only | Operation, Path, Event, Retryable          |
| Error categorization | Not available      | `IsTransientError()`, `IsPermanentError()` |
| Public API stability | Stable             | Stable (only ErrorHandler changed)         |

---

## Need Help?

If you encounter issues during migration:

1. Check the [examples](./examples/) directory for updated code samples
2. Review the [API documentation](https://pkg.go.dev/github.com/larsartmann/go-filewatcher)
3. Open an issue on GitHub with the `migration` label

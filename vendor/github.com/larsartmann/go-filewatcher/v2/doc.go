// Package filewatcher provides a high-level, composable file system watcher
// built on top of fsnotify.
//
// # Quick Start
//
// Create a watcher, start watching, and process events:
//
//	watcher, err := filewatcher.New(
//	    []string{"./src"},
//	    filewatcher.WithExtensions(".go"),
//	    filewatcher.WithDebounce(500*time.Millisecond),
//	    filewatcher.WithIgnoreDirs("vendor", "node_modules"),
//	)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer watcher.Close()
//
//	events, err := watcher.Watch(ctx)
//	for event := range events {
//	    fmt.Printf("%s: %s\n", event.Op, event.Path)
//	}
//
// # Design Principles
//
// Following go-cqrs-lite conventions:
//   - Functional options for configuration
//   - Sentinel errors with stdlib errors package
//   - Explicit error handling; panics only for programmer errors (invalid arguments)
//   - Context as first parameter
//   - Channel-based event streaming
//   - Middleware chains for cross-cutting concerns
//   - Composition over inheritance
//
// # Filters
//
// Built-in filters can be combined with AND/OR/NOT logic:
//
//	filter := filewatcher.FilterAnd(
//	    filewatcher.FilterExtensions(".go"),
//	    filewatcher.FilterNot(filewatcher.FilterIgnoreDirs("vendor")),
//	    filewatcher.FilterOperations(filewatcher.Write, filewatcher.Create),
//	)
//
// # Middleware
//
// Middleware wraps event handlers for cross-cutting concerns:
//
//	watcher, _ := filewatcher.New(paths,
//	    filewatcher.WithMiddleware(
//	        filewatcher.MiddlewareRecovery(),
//	        filewatcher.MiddlewareLogging(nil),
//	    ),
//	)
//
// # Debounce
//
// Two debounce modes:
//   - WithDebounce: global debounce (all events coalesced)
//   - WithPerPathDebounce: per-path debounce (independent per file)
//
// # Filesystem Compatibility
//
// Different platforms and filesystems have different semantics for filename
// case and Unicode normalization. The watcher handles this transparently:
//
//	watcher, _ := filewatcher.New(paths,
//	    // Auto-detects: case-insensitive on Windows/macOS, case-sensitive on Linux.
//	    // Override with CaseSensitive or CaseInsensitive for non-default filesystems.
//	    filewatcher.WithCaseSensitivity(filewatcher.CaseSensitivityAuto),
//	)
//
// Internally, all path comparisons (watch dedup, Remove matching, debounce keys,
// exclude paths, gitignore matching, poll loop snapshots) use a canonical pathKey
// that applies NFC Unicode normalization and optional case-folding. This fixes
// the invisible mismatches that occur when macOS stores filenames as NFD
// (decomposed) but user-configured paths are NFC (composed).
//
// Example: the filename "café" is stored on macOS as the bytes
// 63 61 66 65 CC 81 (NFD: "cafe" + combining acute U+0301), while user input
// is typically 63 61 66 C3 A9 (NFC: precomposed é U+00E9). Without NFC
// normalization, these two byte sequences never compare equal, causing
// exclude-path matching, debounce deduplication, and gitignore prefix checks
// to silently fail.
//
// For filter-level case-insensitivity without changing the watcher-wide mode,
// wrap any Filter with FilterCaseInsensitive (see the example). For NFC
// normalization without case-folding, use FilterCaseSensitive.
//
// Check the resolved mode at runtime via EffectiveCaseSensitivity() (returns
// the FilesystemCaseSensitivity enum) or Stats().CaseSensitivityMode.
package filewatcher

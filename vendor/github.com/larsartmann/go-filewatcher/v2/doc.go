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
package filewatcher

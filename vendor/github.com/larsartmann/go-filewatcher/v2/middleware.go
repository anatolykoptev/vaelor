package filewatcher

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/text/unicode/norm"
	"golang.org/x/time/rate"
)

const logFilePermission = 0o600 // rw------- (owner read/write only) for audit log files

// defaultDedupeWindow is the default time window for deduplicating events.
const defaultDedupeWindow = 100 * time.Millisecond

// dedupeCleanupMultiplier is the multiplier for cleanup ticker interval.
const dedupeCleanupMultiplier = 2

// dedupeCleanupInterval is the number of events between lazy cleanup passes
// in MiddlewareDeduplicate. Uses an event counter rather than map size to
// avoid triggering cleanup every event when the map size hovers at a multiple
// of the interval.
const dedupeCleanupInterval = 100

// dedupeCleanupMaxSize is the hard cap on the dedup map before a forced
// cleanup pass, regardless of the event counter.
const dedupeCleanupMaxSize = 10000

// Middleware wraps an event handler for cross-cutting concerns.
// Middleware is applied in reverse order (last added runs first),
// matching the go-cqrs-lite convention.
type Middleware func(Handler) Handler

// Handler processes a file event.
type Handler func(ctx context.Context, event Event) error

// MiddlewareLogging returns a middleware that logs all events to the
// provided slog logger. If logger is nil, it uses slog.Default().
func MiddlewareLogging(logger *slog.Logger) Middleware {
	if logger == nil {
		logger = slog.Default()
	}

	return func(next Handler) Handler {
		return func(ctx context.Context, event Event) error {
			logger.Info(
				"filewatcher event",
				slog.String("op", event.Op.String()),
				slog.String("path", event.Path),
			)

			return next(ctx, event)
		}
	}
}

// MiddlewareRecovery returns a middleware that recovers from panics in
// downstream handlers, logging the panic value and stack trace.
func MiddlewareRecovery() Middleware {
	return func(next Handler) Handler {
		return func(ctx context.Context, event Event) (err error) {
			defer func() {
				if r := recover(); r != nil {
					//nolint:err113 // panic value and stack are inherently dynamic
					err = fmt.Errorf("panic in handler: %v\n%s", r, debug.Stack())
				}
			}()

			return next(ctx, event)
		}
	}
}

// MiddlewareFilter returns a middleware that applies a Filter to events.
// Events that don't pass the filter are dropped.
func MiddlewareFilter(filter Filter) Middleware {
	return func(next Handler) Handler {
		return func(ctx context.Context, event Event) error {
			if !filter(event) {
				return nil
			}

			return next(ctx, event)
		}
	}
}

// MiddlewareOnError returns a middleware that calls the provided callback
// when an error occurs in downstream handlers.
func MiddlewareOnError(onError func(event Event, err error)) Middleware {
	return func(next Handler) Handler {
		return func(ctx context.Context, event Event) error {
			err := next(ctx, event)
			if err != nil {
				onError(event, err)
			}

			return err
		}
	}
}

// rateLimiterMiddleware creates a middleware that rate-limits events using the given limiter.
func rateLimiterMiddleware(limiter *rate.Limiter) Middleware {
	return func(next Handler) Handler {
		return func(ctx context.Context, event Event) error {
			if !limiter.Allow() {
				return nil
			}

			return next(ctx, event)
		}
	}
}

// MiddlewareRateLimit returns a middleware that limits the rate of events
// using a token bucket algorithm. It allows maxEvents events per second
// with burst=maxEvents. Events exceeding the limit are dropped.
//
// Deprecated: Use MiddlewareThrottle instead, which exposes the burst
// parameter directly. MiddlewareRateLimit(maxEvents) is exactly equivalent
// to MiddlewareThrottle(maxEvents, maxEvents).
func MiddlewareRateLimit(maxEvents int) Middleware {
	return MiddlewareThrottle(maxEvents, maxEvents)
}

// defaultRateLimitWindow is the default window for rate-limiting middlewares.
const defaultRateLimitWindow = time.Second

// defaultSlidingWindowEvents is the default max events per window for
// MiddlewareSlidingWindowRateLimit.
const defaultSlidingWindowEvents = 100

// defaultErrorRateLimit is the default max errors per window for
// MiddlewareErrorRateLimit.
const defaultErrorRateLimit = 10

// resolveRateLimitDefaults returns the provided max value and window,
// substituting package defaults when either is non-positive. defaultMax is
// used when maxValue is non-positive (callers differ in their default count).
// Centralizes the defaulting logic shared by MiddlewareSlidingWindowRateLimit
// and MiddlewareErrorRateLimit.
func resolveRateLimitDefaults(maxValue, defaultMax int, window time.Duration) (int, time.Duration) {
	if maxValue <= 0 {
		maxValue = defaultMax
	}

	if window <= 0 {
		window = defaultRateLimitWindow
	}

	return maxValue, window
}

// MiddlewareSlidingWindowRateLimit returns a middleware that limits events
// to maxEvents per window duration using a token bucket. This provides
// smooth rate limiting without the boundary-burst problem of fixed windows.
func MiddlewareSlidingWindowRateLimit(maxEvents int, window time.Duration) Middleware {
	maxEvents, window = resolveRateLimitDefaults(maxEvents, defaultSlidingWindowEvents, window)
	// Convert maxEvents/window to events per second
	eventsPerSec := float64(maxEvents) / window.Seconds()
	limiter := rate.NewLimiter(rate.Limit(eventsPerSec), maxEvents)

	return rateLimiterMiddleware(limiter)
}

// MiddlewareMetrics returns a middleware that counts processed events.
// The counter function is called with the event operation type after
// each successful event processing.
func MiddlewareMetrics(counter func(op Op)) Middleware {
	return func(next Handler) Handler {
		return func(ctx context.Context, event Event) error {
			err := next(ctx, event)
			if err == nil {
				counter(event.Op)
			}

			return err
		}
	}
}

// dedupeKey uniquely identifies an event for deduplication.
type dedupeKey struct {
	path string
	op   Op
}

// normalizeDedupePath returns a canonical key for deduplication.
// Always applies NFC normalization so that NFD (macOS decomposed) and NFC
// (composed) forms of the same path collide in the dedup map.
func normalizeDedupePath(path string) string {
	return norm.NFC.String(path)
}

// normalizeDedupePathCaseInsensitive lowercases in addition to NFC normalization,
// for case-insensitive filesystems (NTFS, APFS).
func normalizeDedupePathCaseInsensitive(path string) string {
	return strings.ToLower(norm.NFC.String(path))
}

// MiddlewareDeduplicate returns a middleware that drops duplicate events
// for the same file path and operation within a time window.
// This is useful for reducing noise from rapid successive file operations.
//
// Path normalization: NFC Unicode normalization is applied so that NFD
// (macOS decomposed) and NFC (composed) forms of the same path are treated
// as duplicates. For case-insensitive deduplication, use
// [MiddlewareDeduplicateCaseInsensitive].
//
// Example: A file saved twice in quick succession generates two events,
// but only the first is processed.
func MiddlewareDeduplicate(window time.Duration) Middleware {
	return newDeduplicateMiddleware(window, normalizeDedupePath)
}

// MiddlewareDeduplicateCaseInsensitive is like [MiddlewareDeduplicate] but
// also folds character case when comparing paths. Use this on case-insensitive
// filesystems (NTFS, APFS) where File.txt and file.txt refer to the same file.
func MiddlewareDeduplicateCaseInsensitive(window time.Duration) Middleware {
	return newDeduplicateMiddleware(window, normalizeDedupePathCaseInsensitive)
}

// newDeduplicateMiddleware builds a dedup middleware with the given path
// normalizer applied to the dedup key.
func newDeduplicateMiddleware(window time.Duration, normalizePath func(string) string) Middleware {
	if window <= 0 {
		window = defaultDedupeWindow
	}

	type seenEntry struct {
		timestamp time.Time
	}

	var (
		mu         sync.Mutex //nolint:varnamelen // conventional mutex name
		seen       = make(map[dedupeKey]seenEntry)
		eventCount int
	)

	return func(next Handler) Handler {
		return func(ctx context.Context, event Event) error {
			key := dedupeKey{path: normalizePath(event.Path), op: event.Op}

			mu.Lock()
			now := time.Now()

			// Lazy cleanup: remove expired entries periodically.
			// Uses an event counter (not map size) to avoid the quirk where
			// len(seen) hovering at a multiple of 100 triggers cleanup every event.
			eventCount++
			if eventCount%dedupeCleanupInterval == 0 || len(seen) > dedupeCleanupMaxSize {
				cutoff := now.Add(-window * dedupeCleanupMultiplier)
				for k, entry := range seen {
					if entry.timestamp.Before(cutoff) {
						delete(seen, k)
					}
				}
			}

			entry, exists := seen[key]
			if exists && now.Sub(entry.timestamp) < window {
				mu.Unlock()
				// Duplicate detected, drop this event
				return nil
			}

			seen[key] = seenEntry{timestamp: now}
			mu.Unlock()

			return next(ctx, event)
		}
	}
}

// defaultBatchWindow is the default time window for batching events.
const defaultBatchWindow = 100 * time.Millisecond

// defaultBatchSize is the default maximum number of events in a batch.
const defaultBatchSize = 100

// resolveBatchDefaults returns the provided window/maxSize, substituting the
// package defaults when either is non-positive. Centralizes the defaulting
// logic shared by MiddlewareBatch and MiddlewareErrorBatch.
func resolveBatchDefaults(window time.Duration, maxSize int) (time.Duration, int) {
	if window <= 0 {
		window = defaultBatchWindow
	}

	if maxSize <= 0 {
		maxSize = defaultBatchSize
	}

	return window, maxSize
}

// stopAndClearTimer stops the timer if it exists and resets it to nil.
// Centralizes the timer cleanup pattern used by batching middlewares.
func stopAndClearTimer(t **time.Timer) {
	if *t != nil {
		(*t).Stop()
		*t = nil
	}
}

// MiddlewareBatch returns a middleware that batches events over a window
// and emits them all at once. The flush function is called with all batched
// events when the window expires or the batch reaches max size.
//
// The flush function receives the batched events and should process them.
// If it returns an error, the error is passed to the next handler.
// If it returns nil, processing continues normally.
//

func MiddlewareBatch(window time.Duration, maxSize int, flush func([]Event) error) Middleware {
	window, maxSize = resolveBatchDefaults(window, maxSize)

	type batchState struct {
		mu       sync.Mutex
		events   []Event
		timer    *time.Timer
		flushErr error // stores the last timer-flush error; returned on next event
	}

	state := &batchState{
		events:   make([]Event, 0, maxSize),
		mu:       sync.Mutex{},
		timer:    nil,
		flushErr: nil,
	}

	return func(next Handler) Handler {
		return func(ctx context.Context, event Event) error {
			state.mu.Lock()

			// If a timer-flush error is pending, return it now so it routes
			// through the watcher's error handler instead of being swallowed.
			if state.flushErr != nil {
				err := state.flushErr
				state.flushErr = nil
				state.mu.Unlock()

				return err
			}

			state.events = append(state.events, event)

			// If batch is full, flush immediately
			if len(state.events) >= maxSize {
				events := state.events
				state.events = make([]Event, 0, maxSize)

				stopAndClearTimer(&state.timer)

				state.mu.Unlock()

				err := flush(events)
				if err != nil {
					return err
				}

				// The triggering event is already in the flushed batch — do not
				// emit it individually. The timer-expiry branch already follows
				// this pattern (flush only, no next call).
				return nil
			}

			// Start or reset timer
			if state.timer == nil {
				state.timer = time.AfterFunc(window, func() {
					state.mu.Lock()
					events := state.events
					state.events = make([]Event, 0, maxSize)
					state.timer = nil
					state.mu.Unlock()

					if len(events) > 0 {
						err := flush(events)
						if err != nil {
							// Store the error so the next event handler returns it,
							// routing it through the watcher's handleError pipeline.
							state.mu.Lock()
							state.flushErr = err
							state.mu.Unlock()
						}
					}
				})
			}

			state.mu.Unlock()

			return next(ctx, event)
		}
	}
}

// fileLogCache holds a lazily-opened file handle for file-logging middleware.
type fileLogCache struct {
	mu sync.Mutex
	f  *os.File
}

// close safely closes the cached file handle. Safe to call multiple times.
func (c *fileLogCache) close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.f == nil {
		return nil
	}

	err := c.f.Close()
	c.f = nil

	if err != nil {
		return fmt.Errorf("closing log file: %w", err)
	}

	return nil
}

// write appends a formatted event line to the cached file, opening it lazily
// on first write. Returns an error if the file cannot be opened or written.
func (c *fileLogCache) write(filePath string, event Event) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	var writeErr error

	if c.f == nil {
		c.f, writeErr = os.OpenFile(
			filePath,
			os.O_CREATE|os.O_WRONLY|os.O_APPEND,
			logFilePermission,
		)
	}

	if writeErr != nil {
		return fmt.Errorf("opening log file %q: %w", filePath, writeErr)
	}

	if c.f != nil {
		_, writeErr = fmt.Fprintf(
			c.f, "[%s] %s: %s\n",
			event.Timestamp.Format(time.RFC3339),
			event.Op,
			event.Path,
		)
		if writeErr != nil {
			return fmt.Errorf("writing event to log file %q: %w", filePath, writeErr)
		}
	}

	return nil
}

// MiddlewareWriteFileLog returns a middleware that appends event logs
// to a file at the given path. This is useful for audit trails.
// The file handle is cached for the lifetime of the middleware.
//
// Note: the file handle is opened lazily on the first event and is NOT
// closed automatically. For long-lived watchers or watchers that use Reset()
// cycles, prefer NewFileLogMiddleware + WithCleanup to avoid fd leaks:
//
//	mw, closeLog := filewatcher.NewFileLogMiddleware("audit.log")
//	watcher, _ := filewatcher.New(paths,
//	    filewatcher.WithMiddleware(mw),
//	    filewatcher.WithCleanup(closeLog),
//	)
func MiddlewareWriteFileLog(filePath string) Middleware {
	middleware, _ := NewFileLogMiddleware(filePath)

	return middleware
}

// NewFileLogMiddleware returns a file-logging middleware and a cleanup function
// that closes the underlying file handle. The cleanup function is safe to call
// multiple times.
//
// Pair with WithCleanup to ensure the file handle is released when the watcher
// closes:
//
//	mw, closeLog := filewatcher.NewFileLogMiddleware("audit.log")
//	watcher, _ := filewatcher.New(paths,
//	    filewatcher.WithMiddleware(mw),
//	    filewatcher.WithCleanup(closeLog),
//	)
func NewFileLogMiddleware(filePath string) (Middleware, func() error) {
	cached := &fileLogCache{mu: sync.Mutex{}, f: nil}

	closeOnce := &sync.Once{}

	closer := func() error {
		var closeErr error

		closeOnce.Do(func() {
			closeErr = cached.close()
		})

		return closeErr
	}

	middleware := func(next Handler) Handler {
		return func(ctx context.Context, event Event) error {
			writeErr := cached.write(filePath, event)

			err := next(ctx, event)
			if err != nil {
				return err
			}

			if writeErr != nil {
				return fmt.Errorf("writing to log file %q: %w", filePath, writeErr)
			}

			return nil
		}
	}

	return middleware, closer
}

// defaultThrottleEvents is the default maximum events per second for throttling.
const defaultThrottleEvents = 100

// MiddlewareThrottle returns a middleware that limits event processing to
// maxEvents per second with burst support. Up to burst events can be
// processed immediately; after that, only maxEvents per second are allowed.
// Excess events are dropped. Uses golang.org/x/time/rate for correctness.
func MiddlewareThrottle(maxEvents, burst int) Middleware {
	if maxEvents <= 0 {
		maxEvents = defaultThrottleEvents
	}

	if burst <= 0 {
		burst = maxEvents
	}

	limiter := rate.NewLimiter(rate.Limit(maxEvents), burst)

	return rateLimiterMiddleware(limiter)
}

// CircuitState represents the state of a circuit breaker.
type CircuitState int

const (
	// CircuitClosed means the circuit is healthy and all events pass through.
	CircuitClosed CircuitState = iota
	// CircuitOpen means the circuit has tripped and all events are dropped.
	CircuitOpen
	// CircuitHalfOpen means the circuit is testing if the downstream is healthy.
	CircuitHalfOpen
)

// String returns a human-readable name for the circuit state.
func (s CircuitState) String() string {
	switch s {
	case CircuitClosed:
		return "closed"
	case CircuitOpen:
		return "open"
	case CircuitHalfOpen:
		return "half-open"
	default:
		return categoryStringUnknown
	}
}

const circuitBreakerDefaultTimeout = 30 * time.Second

// defaultMaxFailures is the default failure threshold for breaker-style
// middlewares. Used when the caller passes a non-positive maxFailures.
const defaultMaxFailures = 5

// resolveMaxFailures returns maxFailures or the package default when non-positive.
// Centralizes the defaulting logic shared by MiddlewareCircuitBreaker
// and MiddlewareExponentialBackoff.
func resolveMaxFailures(maxFailures int) int {
	if maxFailures <= 0 {
		return defaultMaxFailures
	}

	return maxFailures
}

// MiddlewareCircuitBreaker returns a middleware that implements the circuit breaker pattern.
// After maxFailures consecutive failures from the downstream handler, the circuit opens
// and drops all events for the resetTimeout duration. After the timeout, the circuit
// enters half-open state and allows one event through. If it succeeds, the circuit closes;
// if it fails, the circuit opens again.
//

func MiddlewareCircuitBreaker(maxFailures int, resetTimeout time.Duration) Middleware {
	maxFailures = resolveMaxFailures(maxFailures)

	if resetTimeout <= 0 {
		resetTimeout = circuitBreakerDefaultTimeout
	}

	type circuitBreaker struct {
		mu           sync.Mutex
		state        CircuitState
		failures     int
		lastFailure  time.Time
		halfOpenSent bool
	}

	breaker := &circuitBreaker{
		mu:           sync.Mutex{},
		state:        CircuitClosed,
		failures:     0,
		lastFailure:  time.Time{},
		halfOpenSent: false,
	}

	return func(next Handler) Handler {
		return func(ctx context.Context, event Event) error {
			breaker.mu.Lock()

			switch breaker.state { //nolint:exhaustive // CircuitClosed needs no special handling
			case CircuitOpen:
				if time.Since(breaker.lastFailure) > resetTimeout {
					breaker.state = CircuitHalfOpen
					breaker.halfOpenSent = false
				} else {
					breaker.mu.Unlock()

					return nil
				}

			case CircuitHalfOpen:
				if breaker.halfOpenSent {
					breaker.mu.Unlock()

					return nil
				}

				breaker.halfOpenSent = true
			}

			breaker.mu.Unlock()

			err := next(ctx, event)

			breaker.mu.Lock()
			defer breaker.mu.Unlock()

			if err != nil {
				breaker.failures++
				breaker.lastFailure = time.Now()

				if breaker.failures >= maxFailures {
					breaker.state = CircuitOpen
				}

				return err
			}

			breaker.failures = 0
			breaker.state = CircuitClosed

			return nil
		}
	}
}

// MiddlewareErrorRateLimit returns a middleware that limits the rate of error dispatching.
// When the downstream handler returns errors, this middleware tracks the error rate
// and applies backoff when errors exceed maxErrors within the window duration.
// During backoff, events are still forwarded but errors are suppressed.
func MiddlewareErrorRateLimit(maxErrors int, window time.Duration) Middleware {
	maxErrors, window = resolveRateLimitDefaults(maxErrors, defaultErrorRateLimit, window)

	type errorRateState struct {
		mu      sync.Mutex
		errors  int
		start   time.Time
		limited bool
	}

	state := &errorRateState{
		mu:      sync.Mutex{},
		errors:  0,
		start:   time.Time{},
		limited: false,
	}

	return func(next Handler) Handler {
		return func(ctx context.Context, event Event) error {
			err := next(ctx, event)
			if err == nil {
				return nil
			}

			state.mu.Lock()
			defer state.mu.Unlock()

			now := time.Now()

			if state.start.IsZero() || now.Sub(state.start) > window {
				state.start = now
				state.errors = 1
				state.limited = false

				return err
			}

			state.errors++

			if state.errors >= maxErrors {
				state.limited = true

				return nil
			}

			return err
		}
	}
}

// MiddlewareErrorRecovery returns a middleware that applies a recovery strategy
// when the downstream handler returns an error. The strategy function receives
// the event and error, and returns a replacement error (or nil to suppress it).
func MiddlewareErrorRecovery(strategy func(event Event, err error) error) Middleware {
	return func(next Handler) Handler {
		return func(ctx context.Context, event Event) error {
			err := next(ctx, event)
			if err != nil && strategy != nil {
				return strategy(event, err)
			}

			return err
		}
	}
}

// MiddlewareErrorCorrelation returns a middleware that attaches a unique
// correlation ID to errors. When the downstream handler returns an error,
// the middleware wraps it with a correlation ID for distributed tracing.
func MiddlewareErrorCorrelation(idGenerator func() string) Middleware {
	if idGenerator == nil {
		idGenerator = func() string {
			return strconv.FormatInt(time.Now().UnixNano(), 10)
		}
	}

	return func(next Handler) Handler {
		return func(ctx context.Context, event Event) error {
			err := next(ctx, event)
			if err != nil {
				return fmt.Errorf("[correlation-id=%s]: %w", idGenerator(), err)
			}

			return nil
		}
	}
}

// MiddlewareErrorSanitization returns a middleware that sanitizes errors
// by stripping sensitive file paths. The replacement function receives the
// error string and returns a sanitized version.
func MiddlewareErrorSanitization(sanitize func(string) string) Middleware {
	if sanitize == nil {
		sanitize = func(msg string) string {
			return msg
		}
	}

	return func(next Handler) Handler {
		return func(ctx context.Context, event Event) error {
			err := next(ctx, event)
			if err != nil {
				return fmt.Errorf("sanitized: %s: %w", sanitize(err.Error()), err)
			}

			return nil
		}
	}
}

// BatchError represents an error paired with the event that caused it.
type BatchError struct {
	Event Event
	Error error
}

// MiddlewareErrorBatch returns a middleware that collects errors and flushes them
// in batches. The flush function receives all collected errors when the window
// expires or the batch reaches maxSize. Events are always forwarded regardless
// of errors.
func MiddlewareErrorBatch(window time.Duration, maxSize int, flush func([]BatchError)) Middleware {
	window, maxSize = resolveBatchDefaults(window, maxSize)

	type errorBatchState struct {
		mu     sync.Mutex
		errors []BatchError
		timer  *time.Timer
	}

	state := &errorBatchState{
		errors: make([]BatchError, 0, maxSize),
		mu:     sync.Mutex{},
		timer:  nil,
	}

	return func(next Handler) Handler {
		return func(ctx context.Context, event Event) error {
			err := next(ctx, event)
			if err == nil {
				return nil
			}

			state.mu.Lock()

			state.errors = append(state.errors, BatchError{Event: event, Error: err})

			if len(state.errors) >= maxSize {
				batch := state.errors
				state.errors = make([]BatchError, 0, maxSize)

				stopAndClearTimer(&state.timer)

				state.mu.Unlock()

				flush(batch)

				return err
			}

			if state.timer == nil {
				state.timer = time.AfterFunc(window, func() {
					state.mu.Lock()
					batch := state.errors
					state.errors = make([]BatchError, 0, maxSize)
					state.timer = nil
					state.mu.Unlock()

					if len(batch) > 0 {
						flush(batch)
					}
				})
			}

			state.mu.Unlock()

			return err
		}
	}
}

// defaultExponentialBackoffInitial is the starting backoff for
// MiddlewareExponentialBackoff when the caller does not specify one.
const defaultExponentialBackoffInitial = 100 * time.Millisecond

// defaultExponentialBackoffMax is the upper bound on the backoff window.
const defaultExponentialBackoffMax = 30 * time.Second

// MiddlewareExponentialBackoff returns a middleware that drops events for an
// exponentially increasing duration after consecutive failures. This protects
// downstream consumers from being overwhelmed during error storms.
//
// Behavior:
//   - On success: resets the failure counter and forwards the event.
//   - On failure: increments counter; if >= maxFailures, drops subsequent events
//     for the backoff window. The window doubles after each consecutive failure
//     burst, capped at maxBackoff.
//   - After the backoff window expires, the next event is forwarded as a probe.
//     If it succeeds, the counter resets; if it fails, the cycle repeats.
//
// This is similar to circuit breaker semantics but without explicit state names —
// the drop window simply grows. Use MiddlewareCircuitBreaker for strict
// open/closed/half-open semantics.
//

func MiddlewareExponentialBackoff(maxFailures int, initialBackoff, maxBackoff time.Duration) Middleware {
	maxFailures = resolveMaxFailures(maxFailures)

	if initialBackoff <= 0 {
		initialBackoff = defaultExponentialBackoffInitial
	}

	if maxBackoff <= 0 {
		maxBackoff = defaultExponentialBackoffMax
	}

	type backoffState struct {
		mu          sync.Mutex
		failures    int
		dropUntil   time.Time
		currentWait time.Duration
	}

	state := &backoffState{
		mu:          sync.Mutex{},
		failures:    0,
		dropUntil:   time.Time{},
		currentWait: initialBackoff,
	}

	return func(next Handler) Handler {
		return func(ctx context.Context, event Event) error {
			state.mu.Lock()

			now := time.Now()
			if !state.dropUntil.IsZero() && now.Before(state.dropUntil) {
				state.mu.Unlock()

				return nil // drop event during backoff
			}

			state.mu.Unlock()

			err := next(ctx, event)

			state.mu.Lock()
			defer state.mu.Unlock()

			if err == nil {
				state.failures = 0
				state.currentWait = initialBackoff
				state.dropUntil = time.Time{}

				return nil
			}

			state.failures++
			if state.failures < maxFailures {
				return err
			}

			// Failure threshold exceeded — apply exponential backoff.
			wait := state.currentWait
			state.dropUntil = time.Now().Add(wait)

			// Double the wait for the next cycle, capped at maxBackoff.
			state.currentWait *= 2
			if state.currentWait > maxBackoff {
				state.currentWait = maxBackoff
			}

			return err
		}
	}
}

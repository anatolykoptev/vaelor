package filewatcher

import (
	"errors"
	"fmt"
	"runtime/debug"
)

// Sentinel errors for common failure modes.
var (
	// ErrWatcherClosed is returned when operations are attempted on a closed watcher.
	ErrWatcherClosed = errors.New("watcher is closed")

	// ErrNoPaths is returned when no paths are provided to the watcher.
	ErrNoPaths = errors.New("at least one path is required")

	// ErrPathNotFound is returned when a specified path does not exist.
	ErrPathNotFound = errors.New("path not found")

	// ErrPathNotDir is returned when a path is not a directory.
	ErrPathNotDir = errors.New("path is not a directory")

	// ErrWatcherRunning is returned when Watch() is called on an already running watcher.
	ErrWatcherRunning = errors.New("watcher is already running")

	// ErrUnknownOp is returned when parsing an unknown operation string.
	ErrUnknownOp = errors.New("unknown operation")

	// ErrFsnotifyFailed is returned when the underlying fsnotify watcher fails.
	ErrFsnotifyFailed = errors.New("fsnotify operation failed")

	// ErrWalkFailed is returned when directory traversal fails.
	ErrWalkFailed = errors.New("directory walk failed")

	// ErrPathResolveFailed is returned when path resolution fails.
	ErrPathResolveFailed = errors.New("path resolution failed")

	// ErrEventProcessingFailed is returned when event processing fails.
	ErrEventProcessingFailed = errors.New("event processing failed")

	// ErrMiddlewareFailed is returned when middleware execution fails.
	ErrMiddlewareFailed = errors.New("middleware execution failed")
)

// ErrorCode is a string identifier for categorizing watcher errors.
// Use these constants for programmatic error matching instead of
// comparing error messages.
type ErrorCode string

const (
	categoryStringTransient = "transient"
	categoryStringPermanent = "permanent"
	categoryStringUnknown   = "unknown"
)

const (
	// ErrorCodeWatcherClosed indicates an operation on a closed watcher.
	ErrorCodeWatcherClosed ErrorCode = "WATCHER_CLOSED"
	// ErrorCodeNoPaths indicates no paths were provided.
	ErrorCodeNoPaths ErrorCode = "NO_PATHS"
	// ErrorCodePathNotFound indicates a specified path does not exist.
	ErrorCodePathNotFound ErrorCode = "PATH_NOT_FOUND"
	// ErrorCodePathNotDir indicates a path is not a directory.
	ErrorCodePathNotDir ErrorCode = "PATH_NOT_DIR"
	// ErrorCodeAlreadyRunning indicates Watch() was called while running.
	ErrorCodeAlreadyRunning ErrorCode = "ALREADY_RUNNING"
	// ErrorCodeFsnotify indicates an fsnotify-level failure.
	ErrorCodeFsnotify ErrorCode = "FSNOTIFY_ERROR"
	// ErrorCodeWalk indicates a directory walk failure.
	ErrorCodeWalk ErrorCode = "WALK_ERROR"
	// ErrorCodePathResolve indicates a path resolution failure.
	ErrorCodePathResolve ErrorCode = "PATH_RESOLVE_ERROR"
	// ErrorCodeEventProcessing indicates an event processing failure.
	ErrorCodeEventProcessing ErrorCode = "EVENT_PROCESSING_ERROR"
	// ErrorCodeMiddleware indicates a middleware execution failure.
	ErrorCodeMiddleware ErrorCode = "MIDDLEWARE_ERROR"
	// ErrorCodeUnknown indicates an uncategorized error.
	ErrorCodeUnknown ErrorCode = "UNKNOWN"
)

// ErrorCategory classifies errors as transient (retryable) or permanent.
type ErrorCategory int

const (
	// CategoryUnknown indicates the error category could not be determined.
	CategoryUnknown ErrorCategory = iota

	// CategoryTransient indicates a temporary error that may resolve on retry.
	// Examples: temporary filesystem issues, resource contention.
	CategoryTransient

	// CategoryPermanent indicates a persistent error that won't resolve on retry.
	// Examples: invalid paths, permission denied, watcher closed.
	CategoryPermanent
)

// String returns a human-readable name for the error category.
func (c ErrorCategory) String() string {
	switch c {
	case CategoryUnknown:
		return categoryStringUnknown
	case CategoryTransient:
		return categoryStringTransient
	case CategoryPermanent:
		return categoryStringPermanent
	default:
		return categoryStringUnknown
	}
}

// WatcherError provides structured error information with context.
type WatcherError struct {
	// Op is the operation being performed when the error occurred.
	Op OpString

	// Path is the file path involved, if any.
	Path string

	// Err is the underlying error.
	Err error

	// Category indicates whether this is a transient or permanent error.
	Category ErrorCategory

	// Stack holds the goroutine stack trace captured at the time the error
	// was created. This is populated automatically by NewWatcherError.
	Stack []byte
}

// Error implements the error interface.
func (e *WatcherError) Error() string {
	if e.Path != "" {
		return fmt.Sprintf("%s: path %q: %v", e.Op.Get(), e.Path, e.Err)
	}

	return fmt.Sprintf("%s: %v", e.Op.Get(), e.Err)
}

// Unwrap returns the underlying error for errors.Is/As support.
func (e *WatcherError) Unwrap() error {
	return e.Err
}

// checkWatcherError extracts a WatcherError from an error if present.
// Returns (watcherErr, true) if found, (nil, false) otherwise.
func checkWatcherError(err error) (*WatcherError, bool) {
	watcherErr, ok := errors.AsType[*WatcherError](err)
	if ok {
		return watcherErr, true
	}

	return nil, false
}

// IsTransient returns true if this error is potentially retryable.
func (e *WatcherError) IsTransient() bool {
	return e.Category == CategoryTransient
}

// IsPermanent returns true if this error will not resolve on retry.
func (e *WatcherError) IsPermanent() bool {
	return e.Category == CategoryPermanent
}

// Code returns the ErrorCode for this error based on the underlying sentinel.
// This enables programmatic error matching without string comparisons.
func (e *WatcherError) Code() ErrorCode {
	return errorToCode(e.Err)
}

// errorToCode maps a sentinel error to an ErrorCode.
//
//nolint:cyclop // switch-based error mapping requires n cases
func errorToCode(err error) ErrorCode {
	if err == nil {
		return ErrorCodeUnknown
	}

	switch {
	case errors.Is(err, ErrWatcherClosed):
		return ErrorCodeWatcherClosed
	case errors.Is(err, ErrNoPaths):
		return ErrorCodeNoPaths
	case errors.Is(err, ErrPathNotFound):
		return ErrorCodePathNotFound
	case errors.Is(err, ErrPathNotDir):
		return ErrorCodePathNotDir
	case errors.Is(err, ErrWatcherRunning):
		return ErrorCodeAlreadyRunning
	case errors.Is(err, ErrFsnotifyFailed):
		return ErrorCodeFsnotify
	case errors.Is(err, ErrWalkFailed):
		return ErrorCodeWalk
	case errors.Is(err, ErrPathResolveFailed):
		return ErrorCodePathResolve
	case errors.Is(err, ErrEventProcessingFailed):
		return ErrorCodeEventProcessing
	case errors.Is(err, ErrMiddlewareFailed):
		return ErrorCodeMiddleware
	default:
		return ErrorCodeUnknown
	}
}

// NewWatcherError creates a new WatcherError with the given parameters.
// It automatically categorizes common error types.
func NewWatcherError(op OpString, path string, err error) *WatcherError {
	return &WatcherError{
		Op:       op,
		Path:     path,
		Err:      err,
		Category: categorizeError(err),
		Stack:    debug.Stack(),
	}
}

// categorizeError determines the category of an error based on its type.
func categorizeError(err error) ErrorCategory {
	if err == nil {
		return CategoryUnknown
	}

	// Permanent errors - these won't resolve on retry
	if matchesAnyError(
		err,
		ErrWatcherClosed,
		ErrNoPaths,
		ErrPathNotFound,
		ErrPathNotDir,
		ErrWatcherRunning,
		ErrUnknownOp,
	) {
		return CategoryPermanent
	}

	// Transient errors - these might resolve on retry
	if matchesAnyError(
		err,
		ErrFsnotifyFailed,
		ErrWalkFailed,
		ErrEventProcessingFailed,
		ErrPathResolveFailed,
		ErrMiddlewareFailed,
	) {
		return CategoryTransient
	}

	if we, ok := checkWatcherError(err); ok {
		return we.Category
	}

	return CategoryUnknown
}

// matchesAnyError checks if an error matches any of the given sentinels.
func matchesAnyError(err error, sentinels ...error) bool {
	for _, sentinel := range sentinels {
		if errors.Is(err, sentinel) {
			return true
		}
	}

	return false
}

// isErrorTransientOrPermanent checks if an error is transient or permanent.
// The isTransient parameter determines which category to check.
func isErrorTransientOrPermanent(err error, isTransient bool) bool {
	if err == nil {
		return false
	}

	if we, ok := checkWatcherError(err); ok {
		if isTransient {
			return we.IsTransient()
		}

		return we.IsPermanent()
	}

	expected := CategoryTransient
	if !isTransient {
		expected = CategoryPermanent
	}

	return categorizeError(err) == expected
}

// IsTransientError reports whether an error is potentially retryable.
func IsTransientError(err error) bool {
	return isErrorTransientOrPermanent(err, true)
}

// IsPermanentError reports whether an error will not resolve on retry.
func IsPermanentError(err error) bool {
	return isErrorTransientOrPermanent(err, false)
}

// ErrorContext provides context about what was happening when an error occurred.
// This is passed to error handlers for better observability.
type ErrorContext struct {
	// Operation is the high-level operation being performed.
	Operation string

	// Path is the file path involved, if any.
	Path string

	// Event holds the event being processed, if applicable.
	Event *Event

	// Retryable indicates whether this error might resolve on retry.
	Retryable bool
}

// ErrorHandler is called when errors occur during watching.
// The context provides additional information about what was happening.
type ErrorHandler func(ctx ErrorContext, err error)

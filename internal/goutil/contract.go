package goutil

import "fmt"

// Assert panics with msg if cond is false.
// Use for internal invariants that must never be violated by correct code.
// Prefer returning errors for recoverable conditions; use Assert only for
// logic bugs where the program state is corrupt and continuing is unsafe.
func Assert(cond bool, msg string) {
	if !cond {
		panic("assertion failed: " + msg)
	}
}

// Assertf panics with a formatted message if cond is false.
func Assertf(cond bool, format string, args ...any) {
	if !cond {
		panic("assertion failed: " + fmt.Sprintf(format, args...))
	}
}

// Fail unconditionally panics. Use in unreachable branches (e.g. exhaustive
// switch default cases) to make the invariant explicit and produce a
// useful stack trace instead of a silent nil-deref.
func Fail(msg string) {
	panic("invariant violated: " + msg)
}

package util

import "example.com/minirepo/retry"

// SafeCall wraps fn with the retry package's WithBackoff.
func SafeCall(fn func() error) error {
	return retry.WithBackoff(fn, 3)
}

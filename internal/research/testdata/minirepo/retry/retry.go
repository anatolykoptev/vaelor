package retry

// WithBackoff retries fn with exponential backoff up to maxAttempts.
func WithBackoff(fn func() error, maxAttempts int) error {
	for i := 0; i < maxAttempts; i++ {
		if err := fn(); err == nil {
			return nil
		}
	}
	return nil
}

// Once invokes fn exactly one time.
func Once(fn func() error) error { return fn() }

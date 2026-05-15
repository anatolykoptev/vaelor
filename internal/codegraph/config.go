package codegraph

import (
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"
)

// parseTimeoutSecs reads an env var as a positive integer number of seconds.
// Returns def unchanged if the variable is unset, empty, zero, negative, or
// non-numeric. Logs a warning on invalid (but non-empty) values.
func parseTimeoutSecs(key string, def time.Duration) time.Duration {
	raw := os.Getenv(key)
	if raw == "" {
		return def
	}
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || n <= 0 {
		slog.Warn("invalid timeout env, falling back to default",
			"key", key, "value", raw, "default_secs", int(def.Seconds()))
		return def
	}
	return time.Duration(n) * time.Second
}

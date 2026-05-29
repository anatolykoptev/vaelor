// Package mcpmeta provides a small response-envelope helper used by
// MCP tools to carry timing, optional next-call hints, and optional
// staleness warnings without forcing every tool to repeat boilerplate.
//
// The envelope is intentionally minimal: a tool may emit zero or more
// fields. Empty fields are omitted from the JSON payload so the
// caller sees only what is signal.
package mcpmeta

import (
	"encoding/json"
	"time"
)

// Envelope is the meta block attached to a tool response.
//
// Convention:
//   - DurationMS is always populated.
//   - Hint is populated only when a clear next-call is cheap and obvious.
//     A noisy hint trains the calling agent to ignore the field.
//   - StaleWarning is populated only when the indexed commit no longer
//     matches the on-disk HEAD. Silence is the calibrated signal.
type Envelope struct {
	DurationMS   int64  `json:"duration_ms"`
	Hint         string `json:"hint,omitempty"`
	StaleWarning string `json:"stale_warning,omitempty"`
	IndexedSHA   string `json:"indexed_sha,omitempty"`
	LiveSHA      string `json:"live_sha,omitempty"`
}

// Wrap builds an Envelope from a measured tool duration and an optional hint.
// Pass hint == "" when no next-call is obvious.
func Wrap(elapsed time.Duration, hint string) Envelope {
	return Envelope{
		DurationMS: elapsed.Milliseconds(),
		Hint:     hint,
	}
}

// MarshalJSON omits empty optional fields so callers see only signal.
func (e Envelope) MarshalJSON() ([]byte, error) {
	type alias Envelope
	return json.Marshal(alias(e))
}

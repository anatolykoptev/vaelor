// Package metrics provides lightweight atomic counters for operational observability.
// All operations are safe for concurrent use. No external dependencies.
package metrics

import (
	"sync"
	"sync/atomic"
)

// Counter name constants.
const (
	LLMCalls       = "llm_calls"
	LLMErrors      = "llm_errors"
	SearchRequests  = "search_requests"
	GitClones       = "git_clones"
	CacheHits       = "cache_hits"
	CacheMisses     = "cache_misses"
	GitHubAPICalls  = "github_api_calls"
)

// store holds all counters keyed by name.
var store sync.Map

// counter returns the *atomic.Int64 for name, creating it on first access.
func counter(name string) *atomic.Int64 {
	v, _ := store.LoadOrStore(name, new(atomic.Int64))
	return v.(*atomic.Int64) //nolint:forcetypeassert // we only ever store *atomic.Int64
}

// Incr increments the named counter by 1.
func Incr(name string) {
	counter(name).Add(1)
}

// Snapshot returns a copy of all counters with their current values.
// Only counters that have been written at least once are included.
func Snapshot() map[string]int64 {
	m := make(map[string]int64)
	store.Range(func(k, v any) bool {
		m[k.(string)] = v.(*atomic.Int64).Load() //nolint:forcetypeassert // invariant: only *atomic.Int64 stored
		return true
	})
	return m
}

// Reset clears all counters. Intended for use in tests only.
// MUST NOT be called concurrently with Incr.
func Reset() {
	store.Range(func(k, _ any) bool {
		store.Delete(k)
		return true
	})
}

// TrackOperation increments callCounter, runs fn, and increments errCounter if fn
// returns a non-nil error. The error from fn is always returned unchanged.
func TrackOperation(callCounter, errCounter string, fn func() error) error {
	Incr(callCounter)
	if err := fn(); err != nil {
		Incr(errCounter)
		return err
	}
	return nil
}

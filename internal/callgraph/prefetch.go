package callgraph

import (
	"context"
	"log/slog"
	"time"

	"golang.org/x/sync/singleflight"
)

// prefetchTimeout bounds a single background call-graph build. Long enough
// to cover a cold parse plus export-data priming on large modules, short
// enough that a wedged build cannot hold the dedup slot forever.
const prefetchTimeout = 30 * time.Minute

// prefetchGroup deduplicates background builds per cgCache key: the gate
// that calls Prefetch answers "building" to every concurrent caller, and
// they must share one build rather than fork one each. singleflight —
// the same primitive embeddings uses for schema dedup — auto-forgets the
// key when the build returns, so a later cold call can re-arm it.
var prefetchGroup singleflight.Group

// ReadyForBuild reports whether BuildFromRepo(input) would be served from
// cgCache without a cold parse+load. Long-running callers (review_delta,
// review_pr) check this before entering the analysis pipeline: a cold cache
// means the call would hold the MCP request for the whole build, so they
// answer with a building status and Prefetch instead (issue #905).
func ReadyForBuild(input TraceRepoInput) bool {
	_, _, ok := cgCache.getWithAt(cgCacheKey(input), input.Root)
	return ok
}

// Prefetch warms cgCache for input in the background, deduplicated per
// cache key. Returns immediately. The build runs detached from any request
// context so a cancelled or abandoned caller cannot leave the cache
// half-warm; failures and panics are logged, never propagated — a later
// call simply sees the cache still cold and re-arms the prefetch.
func Prefetch(input TraceRepoInput) {
	// DoChan runs the build detached; duplicate keys join the in-flight
	// call and their channel is simply discarded — callers never wait on
	// the result, the gate already answered them "building".
	prefetchGroup.DoChan(cgCacheKey(input), func() (any, error) {
		defer func() {
			if r := recover(); r != nil {
				slog.Error("callgraph: prefetch panicked",
					slog.String("root", input.Root), slog.Any("panic", r))
			}
		}()
		ctx, cancel := context.WithTimeout(context.Background(), prefetchTimeout)
		defer cancel()
		if _, err := BuildFromRepo(ctx, input); err != nil {
			slog.Warn("callgraph: prefetch failed",
				slog.String("root", input.Root), slog.Any("error", err))
			return nil, err
		}
		slog.Info("callgraph: prefetch complete", slog.String("root", input.Root))
		return nil, nil
	})
}

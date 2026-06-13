package embeddings

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/semaphore"

	"github.com/anatolykoptev/go-code/internal/repofind"
)

// RepoKeyFunc generates a graph key from a repo root path.
type RepoKeyFunc func(root string) string

// AutoIndexOpts controls the bounded worker pool and retry policy used by
// AutoIndex. A zero/invalid value falls back to DefaultAutoIndexOpts.
type AutoIndexOpts struct {
	// Concurrency is the maximum number of repos indexed in parallel.
	// Must be >=1; values <1 are normalized to 1 (serial fallback).
	Concurrency int
	// RetryMax is the maximum number of retries per repo on transient
	// failures. RetryMax=0 disables retry (single attempt only).
	RetryMax int
	// RetryBase is the initial backoff duration; doubles on each retry.
	RetryBase time.Duration
}

// Default tuning for AutoIndex.
//
// Concurrency=1 serializes autoindex embed calls onto the single-worker
// embed backend (code-rank-embed on embed.krolik.tools, pool_size=1). With
// concurrency=2 and the fleet (~48 repos), the backend queue depth was
// unbounded: second-repo batches queued behind first-repo's ~13s inference,
// causing context deadline exceeded on the 120s client timeout and triggering
// the full 3-retry stack (total wall time ≈ 360s per batch). Concurrency=1
// limits go-code's pressure to one in-flight embed batch at a time so queue
// depth stays 0 and individual requests complete within the raised timeout.
// Override via AUTOINDEX_CONCURRENCY env; raise only after embed backend
// is confirmed pool_size>1.
const (
	defaultAutoIndexConcurrency = 1
	defaultAutoIndexRetryMax    = 3
	defaultAutoIndexRetryBase   = 5 * time.Second
)

// DefaultAutoIndexOpts returns sane defaults: concurrency=1, retry_max=3,
// retry_base=5s (exponential backoff: 5s, 10s, 20s).
func DefaultAutoIndexOpts() AutoIndexOpts {
	return AutoIndexOpts{
		Concurrency: defaultAutoIndexConcurrency,
		RetryMax:    defaultAutoIndexRetryMax,
		RetryBase:   defaultAutoIndexRetryBase,
	}
}

// repoIndexer is the subset of *Pipeline that AutoIndex needs.
// Defined here for testability — tests inject a fake.
type repoIndexer interface {
	IndexRepo(ctx context.Context, repoKey, root string) (*IndexResult, error)
	IncrementalSync(ctx context.Context, repoKey, root string) (*IncrementalSyncResult, error)
}

// AutoIndex scans directories for Git repositories and indexes them with a
// bounded worker pool plus per-repo retry-with-backoff on transient errors.
//
// Each immediate subdirectory containing a .git folder is treated as a repo.
// keyFn should be codegraph.GraphNameFor (passed from caller to avoid an
// import cycle).
//
// Rollback to byte-identical legacy behavior: opts.Concurrency=1, RetryMax=0.
//
// Model-fingerprint guard: before indexing each repo, AutoIndex calls
// pipeline.InvalidateIfModelChanged. When the stored embed_model differs from
// the active model (e.g. jina-code-v2 → code-rank-embed), all stale vectors
// for that repo are purged atomically so IncrementalSync sees no prior SHA and
// runs a full re-embed with the new model. This prevents mixed-space garbage
// from old + new vectors being returned by semantic_search.
func AutoIndex(pipeline *Pipeline, dirs []string, keyFn RepoKeyFunc, opts AutoIndexOpts) {
	if pipeline == nil {
		return
	}
	ctx := context.Background()
	// Invalidate per-repo before handing off to the worker pool, so that the
	// first IncrementalSync call already sees a clean state. Invalidation is
	// cheap (single SELECT per repo, one transaction on mismatch) and must run
	// before the semaphore loop to avoid a race between invalidation and the
	// first in-flight indexRepo call.
	if pipeline.embedModel != "" {
		repos := discoverRepos(dirs, keyFn)
		for _, r := range repos {
			pipeline.InvalidateIfModelChanged(ctx, r.key)
		}
	}
	autoIndex(ctx, pipeline, dirs, keyFn, opts)
}

// autoIndex is the testable core: takes an indexer interface and a context.
func autoIndex(
	ctx context.Context,
	pipeline repoIndexer,
	dirs []string,
	keyFn RepoKeyFunc,
	opts AutoIndexOpts,
) {
	if pipeline == nil || len(dirs) == 0 {
		return
	}
	opts = normalizeOpts(opts)

	repos := discoverRepos(dirs, keyFn)
	if len(repos) == 0 {
		return
	}

	slog.Info("autoindex: indexing repos",
		slog.Int("repos", len(repos)),
		slog.Int("concurrency", opts.Concurrency),
		slog.Int("retry_max", opts.RetryMax),
	)

	sem := semaphore.NewWeighted(int64(opts.Concurrency))
	var wg sync.WaitGroup
	for _, r := range repos {
		wg.Add(1)
		go func(r repo) {
			defer wg.Done()
			// Count repos that had to WAIT for a semaphore slot (true queue
			// pressure). TryAcquire succeeds immediately when a slot is free;
			// only the blocking path (slot already held) increments the counter,
			// so the metric reflects actual contention, not mere entry.
			if !sem.TryAcquire(1) {
				recordAutoIndexDeferred(r.key)
				if err := sem.Acquire(ctx, 1); err != nil {
					// ctx cancelled before slot available — give up silently.
					return
				}
			}
			defer sem.Release(1)
			// Track in-flight autoindex jobs for operational visibility.
			autoindexInFlight.Inc()
			defer autoindexInFlight.Dec()
			indexWithRetry(ctx, pipeline, r, opts)
		}(r)
	}
	wg.Wait()
	slog.Info("autoindex: complete", slog.Int("repos", len(repos)))
}

type repo struct{ key, root string }

func discoverRepos(dirs []string, keyFn RepoKeyFunc) []repo {
	var repos []repo
	for _, root := range repofind.Discover(dirs) {
		repos = append(repos, repo{key: keyFn(root), root: root})
	}
	return repos
}

// indexWithRetry runs IncrementalSync with exponential backoff. Non-retryable
// errors (parse, schema, ctx cancellation) abort immediately. Retryable
// errors (deadline, 5xx, conn refused) trigger backoff up to RetryMax.
//
// IncrementalSync selects the git-diff path when a previous SHA is stored, or
// falls back to IndexRepo for first-time indexing. Partial failures are
// propagated as a non-nil error so the retry loop can reschedule the repo.
func indexWithRetry(ctx context.Context, pipeline repoIndexer, r repo, opts AutoIndexOpts) {
	start := time.Now()
	backoff := opts.RetryBase
	for attempt := 0; attempt <= opts.RetryMax; attempt++ {
		result, err := pipeline.IncrementalSync(ctx, r.key, r.root)
		if err == nil && (result == nil || len(result.Errors) == 0) {
			if result != nil {
				slog.Info("autoindex: done",
					slog.String("repo", r.key),
					slog.String("mode", string(result.Mode)),
					slog.Int("embedded", result.FilesEmbedded),
					slog.Int("skipped", result.FilesSkipped),
					slog.Int("changed", result.FilesChanged),
					slog.Int("attempts", attempt+1),
				)
			}
			recordAutoIndexDuration(r.key, "success", time.Since(start))
			return
		}
		// Treat per-file errors (result.Errors) as a retryable transient failure.
		// The first error in the slice is representative for classification.
		var classifyErr error
		if err != nil {
			classifyErr = err
		} else if result != nil && len(result.Errors) > 0 {
			classifyErr = result.Errors[0]
		}
		reason := classifyAutoIndexError(classifyErr)
		if reason == retryReasonNonRetryable || attempt == opts.RetryMax {
			slog.Warn("autoindex: failed",
				slog.String("repo", r.key),
				slog.Any("error", classifyErr),
				slog.Int("attempts", attempt+1),
				slog.String("reason", reason),
			)
			outcome := "failed"
			if reason == retryReasonNonRetryable {
				outcome = "non_retryable"
			}
			recordAutoIndexDuration(r.key, outcome, time.Since(start))
			return
		}
		recordAutoIndexRetry(r.key, reason)
		// Sleep for backoff, but respect ctx cancellation.
		select {
		case <-ctx.Done():
			recordAutoIndexDuration(r.key, "cancelled", time.Since(start))
			return
		case <-time.After(backoff):
		}
		backoff *= 2
	}
}

// Retry reasons used as low-cardinality Prometheus labels.
const (
	retryReasonDeadline     = "deadline_exceeded"
	retryReasonConnRefused  = "connection_refused"
	retryReason5xx          = "embed_5xx"
	retryReasonNonRetryable = "non_retryable"
)

// classifyAutoIndexError maps an IndexRepo error to a retry reason label.
// context.Canceled and parse/schema errors are non-retryable; transient
// network/embed failures are retryable.
func classifyAutoIndexError(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.Canceled) {
		return retryReasonNonRetryable
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return retryReasonDeadline
	}
	msg := err.Error()
	if strings.Contains(msg, "connection refused") {
		return retryReasonConnRefused
	}
	if strings.Contains(msg, "503") || strings.Contains(msg, "504") || strings.Contains(msg, "502") {
		return retryReason5xx
	}
	return retryReasonNonRetryable
}

func normalizeOpts(opts AutoIndexOpts) AutoIndexOpts {
	if opts.Concurrency < 1 {
		opts.Concurrency = 1
	}
	if opts.RetryMax < 0 {
		opts.RetryMax = 0
	}
	if opts.RetryBase <= 0 {
		opts.RetryBase = defaultAutoIndexRetryBase
	}
	return opts
}

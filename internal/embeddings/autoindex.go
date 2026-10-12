package embeddings

import (
	"context"
	"errors"
	"log/slog"
	"runtime/debug"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sync/semaphore"

	"github.com/anatolykoptev/vaelor/internal/repofind"
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
	// Periodic marks a pass run by AutoIndexLoop rather than at boot. It
	//   - logs a repo whose main has not moved (mode skip-sha-match) at debug
	//     instead of info (~90 unchanged repos per tick would drown the line
	//     that matters);
	//   - skips repos the loop must not touch (no resolvable git HEAD, never
	//     indexed, nothing embeddable), counting them in
	//     gocode_autoindex_loop_skipped_total instead of re-running a full
	//     fallback and warning about it every tick;
	//   - caps the files re-embedded per repo (MaxFilesPerRepo).
	Periodic bool
	// PassBudget bounds the wall-clock time of one periodic pass. A pass that
	// hits it stops; unfinished work continues on the next tick because the
	// incremental path is resumable (head_sha only advances on full success and
	// IndexFile hash-skips what is already embedded). <= 0 uses
	// DefaultPassBudget; values below MinPassBudget are raised to it.
	PassBudget time.Duration
	// MaxFilesPerRepo caps how many files are RE-EMBEDDED per repo per periodic
	// pass (files that hash-skip are free and do not count). <= 0 uses
	// DefaultMaxFilesPerRepo.
	MaxFilesPerRepo int
}

// Periodic-pass defaults. Sized from measured numbers on the prod embed backend
// (single worker): 658 rows in 404 s over 22 h = 1.63 rows/s, and ~2.7 embedded
// rows per changed file => about 1.7 s per re-embedded file. A 100-file cap is
// therefore about 170 s for one repo; the 10 minute pass budget fits roughly
// 350 re-embedded files, i.e. three or four repos at the cap. A merge touching
// 1000 files is spread over several ticks instead of occupying the backend for
// half an hour in one go.
const (
	DefaultPassBudget      = 10 * time.Minute
	MinPassBudget          = time.Minute
	DefaultMaxFilesPerRepo = 100
	// MinInterval is the smallest accepted AUTOINDEX_INTERVAL: a pass costs
	// seconds of CPU on an idle fleet (88 repos), so anything faster is churn.
	MinInterval = time.Minute
)

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

// indexingChecker is implemented by *Pipeline; fakes that do not model an
// in-flight index simply do not implement it.
//
// The IsIndexing check is a one-way guard, deliberately not a slot claim: the
// incremental path takes no single-flight slot, and a lazy semantic_search
// index that starts while an incremental sync is mid-flight embeds the same new
// symbols a second time (wasted backend time, no corruption: Upsert is keyed by
// (repo_key, file_path, symbol_name) and each writer records the SHA it saw at
// its own start). Claiming the slot around IncrementalSync would make its
// fallbackToFull -> IndexRepo lose the slot and silently no-op, so it needs to
// claim only the incremental branch; tracked as a follow-up.
type indexingChecker interface{ IsIndexing(repoKey string) bool }

// loopGate is implemented by *Pipeline. LoopSkipReason returns "" when the
// periodic loop should sync the repo, else why it must not (see Periodic).
type loopGate interface {
	LoopSkipReason(ctx context.Context, repoKey, root string) string
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

// AutoIndexLoop re-runs the AutoIndex pass every interval until ctx is done,
// so a local repo whose main branch moves while the service stays up is
// re-synced without a restart. AutoIndex alone ran once at boot: afterwards
// head_sha advanced only when a semantic_search/code_research call happened to
// land on the repo, and the stale_warning on every other tool stayed up for
// as long as the container did. Each pass is the same IncrementalSync the boot
// pass uses (git-diff of the changed files; an unchanged repo costs a SHA
// compare, a few queries and a timestamp bump), so a quiet fleet is cheap.
// Repos are synced one after another when opts.Concurrency is 1 (the default
// and the only safe value for a single-worker embed backend). Passes never
// overlap; a pass is bounded by opts.PassBudget and each repo by
// opts.MaxFilesPerRepo. interval <= 0 disables the loop. Call it after the
// boot AutoIndex returns.
func AutoIndexLoop(ctx context.Context, pipeline *Pipeline, dirs []string, keyFn RepoKeyFunc, opts AutoIndexOpts, interval time.Duration) {
	if pipeline == nil {
		return
	}
	autoIndexLoop(ctx, pipeline, dirs, keyFn, opts, interval)
}

// autoIndexLoop is the testable core of AutoIndexLoop.
func autoIndexLoop(
	ctx context.Context,
	pipeline repoIndexer,
	dirs []string,
	keyFn RepoKeyFunc,
	opts AutoIndexOpts,
	interval time.Duration,
) {
	if interval <= 0 || pipeline == nil || len(dirs) == 0 {
		return
	}
	opts.Periodic = true
	if opts.PassBudget <= 0 {
		opts.PassBudget = DefaultPassBudget
	}
	opts.PassBudget = max(opts.PassBudget, MinPassBudget)
	if opts.MaxFilesPerRepo <= 0 {
		opts.MaxFilesPerRepo = DefaultMaxFilesPerRepo
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(interval):
		}
		runLoopPass(ctx, pipeline, dirs, keyFn, opts)
	}
}

// runLoopPass runs one periodic pass under its budget. A panic anywhere in the
// pass is logged and counted and the loop carries on: without this a parser
// panic on a file an agent is editing would repeat on every tick.
func runLoopPass(ctx context.Context, pipeline repoIndexer, dirs []string, keyFn RepoKeyFunc, opts AutoIndexOpts) {
	defer func() {
		if rec := recover(); rec != nil {
			recordAutoIndexPanic()
			slog.Error("autoindex: periodic pass panicked; loop continues",
				slog.Any("panic", rec), slog.String("stack", string(debug.Stack())))
		}
	}()
	passCtx, cancel := context.WithTimeout(ctx, opts.PassBudget)
	defer cancel()
	autoIndex(passCtx, pipeline, dirs, keyFn, opts)
	if ctx.Err() == nil && errors.Is(passCtx.Err(), context.DeadlineExceeded) {
		recordAutoIndexPassBudgetExceeded()
		slog.Warn("autoindex: periodic pass hit its time budget; unfinished repos continue next tick",
			slog.Duration("budget", opts.PassBudget))
	}
}

var (
	// periodicPassCount numbers periodic passes for the start-offset rotation.
	periodicPassCount atomic.Int32
	closedChan        = func() <-chan struct{} { c := make(chan struct{}); close(c); return c }()
)

// passResult summarises one autoIndex pass.
type passResult struct {
	Repos, OK, Failed, Skipped int
	// Completed is true when the pass ran to its end (its context was not
	// cancelled or timed out), regardless of per-repo outcomes.
	Completed bool
}

// autoIndex is the testable core: takes an indexer interface and a context.
func autoIndex(
	ctx context.Context,
	pipeline repoIndexer,
	dirs []string,
	keyFn RepoKeyFunc,
	opts AutoIndexOpts,
) passResult {
	if pipeline == nil || len(dirs) == 0 {
		return passResult{}
	}
	opts = normalizeOpts(opts)

	repos := discoverRepos(dirs, keyFn)
	if len(repos) == 0 {
		// Nothing discovered (e.g. the mount is gone) is still a pass that ran:
		// the liveness gauge must keep advancing so it measures the loop, not
		// the content of the directory.
		recordAutoIndexPassDone()
		return passResult{Completed: ctx.Err() == nil}
	}
	if opts.Periodic && opts.MaxFilesPerRepo > 0 {
		ctx = withMaxEmbeddedFiles(ctx, opts.MaxFilesPerRepo)
	}

	slog.Info("autoindex: indexing repos",
		slog.Int("repos", len(repos)),
		slog.Int("concurrency", opts.Concurrency),
		slog.Int("retry_max", opts.RetryMax),
		slog.Bool("periodic", opts.Periodic),
	)

	var ok, failed, skipped atomic.Int32
	sem := semaphore.NewWeighted(int64(opts.Concurrency))
	var wg sync.WaitGroup
	if opts.Periodic && len(repos) > 1 {
		// Start each periodic pass at a different repo, so a pass cut off by its
		// budget does not spend every tick on the same first repos while the
		// last ones starve. Repos take the slot in the order listed (below).
		k := int(periodicPassCount.Add(1)) % len(repos)
		repos = append(slices.Clone(repos[k:]), repos[:k]...)
	}
	prevAcquired := closedChan
	for _, r := range repos {
		// Decide "got a slot without waiting" HERE, sequentially, so which repos
		// run first is the listed order and not a goroutine-scheduling accident.
		// Repos that must wait are counted now (true queue pressure) and queue on
		// the semaphore behind their predecessor: the chain of channels makes
		// them reach sem.Acquire one after another, and the semaphore is FIFO.
		gotSlot := sem.TryAcquire(1)
		if !gotSlot {
			recordAutoIndexDeferred(r.key)
		}
		acquired := make(chan struct{})
		wg.Add(1)
		go func(r repo, gotSlot bool, prev <-chan struct{}, acquired chan struct{}) {
			defer wg.Done()
			defer func() {
				if rec := recover(); rec != nil {
					failed.Add(1)
					recordAutoIndexPanic()
					slog.Error("autoindex: repo sync panicked; continuing with the next repo",
						slog.String("repo", r.key), slog.Any("panic", rec),
						slog.String("stack", string(debug.Stack())))
				}
			}()
			if !gotSlot {
				select {
				case <-prev:
				case <-ctx.Done():
				}
				err := sem.Acquire(ctx, 1)
				close(acquired)
				if err != nil {
					// ctx cancelled before a slot freed: wait for the next pass.
					skipped.Add(1)
					return
				}
			} else {
				close(acquired)
			}
			defer sem.Release(1)
			// A full index of this repo is already running (a lazy
			// semantic_search/code_research index, or a previous pass); see
			// indexingChecker for why this is a skip and not a slot claim.
			if c, isChecker := pipeline.(indexingChecker); isChecker && c.IsIndexing(r.key) {
				slog.Debug("autoindex: skipped, index already running", slog.String("repo", r.key))
				recordAutoIndexSkippedBusy(r.key)
				skipped.Add(1)
				return
			}
			if opts.Periodic {
				if g, isGate := pipeline.(loopGate); isGate {
					if reason := g.LoopSkipReason(ctx, r.key, r.root); reason != "" {
						recordLoopSkip(r.key, reason)
						skipped.Add(1)
						return
					}
				}
			}
			// Track in-flight autoindex jobs for operational visibility.
			autoindexInFlight.Inc()
			defer autoindexInFlight.Dec()
			switch indexWithRetry(ctx, pipeline, r, opts) {
			case outcomeOK:
				ok.Add(1)
			case outcomeFailed:
				failed.Add(1)
			default:
				skipped.Add(1)
			}
		}(r, gotSlot, prevAcquired, acquired)
		prevAcquired = acquired
	}
	wg.Wait()

	res := passResult{
		Repos: len(repos), OK: int(ok.Load()), Failed: int(failed.Load()), Skipped: int(skipped.Load()),
		Completed: ctx.Err() == nil,
	}
	if res.Failed > 0 && res.OK == 0 && res.Skipped == 0 {
		recordAutoIndexPassFailure()
	}
	// Stamp liveness only for a pass that ran to its end: a pass cut short by
	// shutdown or by its time budget did not complete.
	if res.Completed {
		recordAutoIndexPassDone()
	}
	slog.Info("autoindex: complete", slog.Int("repos", len(repos)),
		slog.Int("ok", res.OK), slog.Int("failed", res.Failed), slog.Int("skipped", res.Skipped))
	return res
}

// loopSkipLogged remembers which (repo, reason) pairs were already logged, so a
// repo the loop never touches costs one debug line per process, not one per tick.
var loopSkipLogged sync.Map

func recordLoopSkip(repoKey, reason string) {
	recordAutoIndexLoopSkipped(reason)
	if _, seen := loopSkipLogged.LoadOrStore(repoKey+"|"+reason, struct{}{}); !seen {
		slog.Debug("autoindex: periodic pass skips repo (logged once)",
			slog.String("repo", repoKey), slog.String("reason", reason))
	}
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
func indexWithRetry(ctx context.Context, pipeline repoIndexer, r repo, opts AutoIndexOpts) syncOutcome {
	start := time.Now()
	backoff := opts.RetryBase
	for attempt := 0; attempt <= opts.RetryMax; attempt++ {
		result, err := pipeline.IncrementalSync(ctx, r.key, r.root)
		if err == nil && (result == nil || len(result.Errors) == 0) {
			if result != nil {
				level := slog.LevelInfo
				if opts.Periodic && result.Mode == IncrementalSyncSkipSHAMatch {
					level = slog.LevelDebug
				}
				slog.Log(ctx, level, "autoindex: done",
					slog.String("repo", r.key),
					slog.String("mode", string(result.Mode)),
					slog.Int("embedded", result.FilesEmbedded),
					slog.Int("skipped", result.FilesSkipped),
					slog.Int("changed", result.FilesChanged),
					slog.Int("attempts", attempt+1),
				)
			}
			recordAutoIndexDuration(r.key, "success", time.Since(start))
			return outcomeOK
		}
		// The pass was cut (shutdown or time budget): not a failure of this
		// repo. Unfinished work resumes on the next pass.
		if ctx.Err() != nil {
			recordAutoIndexDuration(r.key, "cancelled", time.Since(start))
			return outcomeCancelled
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
			return outcomeFailed
		}
		recordAutoIndexRetry(r.key, reason)
		// Sleep for backoff, but respect ctx cancellation.
		select {
		case <-ctx.Done():
			recordAutoIndexDuration(r.key, "cancelled", time.Since(start))
			return outcomeCancelled
		case <-time.After(backoff):
		}
		backoff *= 2
	}
	return outcomeFailed
}

// syncOutcome is how one repo's sync ended within a pass.
type syncOutcome int

const (
	outcomeOK syncOutcome = iota
	outcomeFailed
	outcomeCancelled
)

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
	// All 5xx responses from the embed server are transient (cold-load thrash,
	// model-swap inference failures, temporary overload). Bare 500 "inference
	// failed" historically classified as non_retryable and caused mass repo
	// abandonment during the jina→code-rank-embed cutover (2026-06-13 incident).
	// 4xx remain non-retryable — a malformed request will not self-heal on retry.
	if strings.Contains(msg, "500") || strings.Contains(msg, "501") ||
		strings.Contains(msg, "502") || strings.Contains(msg, "503") ||
		strings.Contains(msg, "504") || strings.Contains(msg, "505") ||
		strings.Contains(msg, "server_error") {
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

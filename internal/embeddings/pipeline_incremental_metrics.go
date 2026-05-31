package embeddings

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// repoStateGetter is a narrow interface for reading persisted indexed_sha.
// *Store satisfies it; tests may substitute a fake implementation.
type repoStateGetter interface {
	GetRepoState(ctx context.Context, repoKey string) (string, error)
}

// embed_incremental_sync_total counts Pipeline.IncrementalSync invocations by
// mode (the IncrementalSyncMode code path taken) and outcome.
//
// outcome values: success | partial | error
//   - success: no per-file errors, SHA advanced (or same-SHA skip)
//   - partial: at least one per-file error; SHA NOT advanced
//   - error:   catastrophic top-level error returned to caller
//
// Cardinality: 5 modes × 3 outcomes = 15 series max.
var incrementalSyncTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "embed_incremental_sync_total",
		Help: "Pipeline.IncrementalSync invocations by mode and outcome.",
	},
	[]string{"mode", "outcome"},
)

// embed_incremental_files_total counts files processed by Pipeline.IndexFile
// by change kind (embedded | skipped | deleted).
//
// Counter increments BEFORE the SHA-advance gate, so partial-success runs
// (where some later file fails) are included; only top-level errors that
// abort before the per-file loop are excluded.
//
// Cardinality: 3 series.
var incrementalFilesTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "embed_incremental_files_total",
		Help: "Files processed by Pipeline.IndexFile by change kind. Includes files from partial-success runs (where some other file later failed); only excludes files where the parent IncrementalSync hit a top-level error before reaching the per-file loop.",
	},
	[]string{"kind"},
)

// embed_index_file_duration_seconds measures Pipeline.IndexFile wall-time per
// invocation, labelled by outcome (success | error).
//
// Buckets cover the observed range from 10ms (cache hit) to ~40s (large file embed).
// Cardinality: 2 series.
var indexFileDuration = promauto.NewHistogramVec(
	prometheus.HistogramOpts{
		Name:    "embed_index_file_duration_seconds",
		Help:    "Pipeline.IndexFile wall-time per file by outcome (success | error).",
		Buckets: prometheus.ExponentialBuckets(0.01, 2, 12), // 10ms → ~40s
	},
	[]string{"outcome"},
)

// embed_incremental_unsupported_files_total counts files in incremental diffs
// that were permanently skipped due to a non-source-code reason, labelled by
// reason:
//
//   - unsupported_ext: extension has no tree-sitter handler (e.g. .md, .yml)
//   - read_error:      permanent IO error (permission denied, stale mount, etc)
//
// A non-zero rate of "unsupported_ext" is expected and benign (documentation
// commits). A non-zero rate of "read_error" warrants operator investigation.
//
// Cardinality: 2 series.
var incrementalFilesUnsupportedTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "embed_incremental_unsupported_files_total",
		Help: "Files in incremental diffs permanently skipped by reason (unsupported_ext | read_error).",
	},
	[]string{"reason"},
)

// gocode_index_commits_behind is a per-repo gauge recording how many commits the
// persisted indexed_sha is behind the repo's main branch tip after each
// IncrementalSync run.
//
//   - 0: indexed_sha is at main-tip (fully up-to-date)
//   - N>0: indexed_sha is N commits behind main-tip (lag detected)
//
// Unlike the old gocode_index_freshness_lag (which read an in-memory error flag),
// this gauge is computed from the PERSISTED state stored in code_repo_state
// against the live git branch tip. It correctly detects staleness even when
// SetRepoState failed silently (Errors==0 but write missed) or when a frozen
// repo stopped producing per-file errors.
//
// Label "repo" uses the repoKey (e.g. "github.com/org/repo"). Cardinality is
// bounded by the number of indexed repos (typically 10-100).
var indexCommitsBehind = promauto.NewGaugeVec(
	prometheus.GaugeOpts{
		Name: "gocode_index_commits_behind",
		Help: "Commits the persisted indexed_sha is behind the repo's main branch; 0 = current.",
	},
	[]string{"repo"},
)

// embed_repo_state_write_failures_total counts SetRepoState write failures.
// Any non-zero rate means indexed_sha is silently not advancing — the next run
// will redo all work for affected files (cheap due to hash-skip, but the lag
// gauge will stay elevated until a write succeeds).
//
// Cardinality: 1 series.
var repoStateWriteFailuresTotal = promauto.NewCounter(
	prometheus.CounterOpts{
		Name: "embed_repo_state_write_failures_total",
		Help: "SetRepoState write failures; non-zero rate means indexed_sha is not persisting.",
	},
)

// embed_index_commits_behind_compute_errors_total counts errors that prevented
// gocode_index_commits_behind from being updated (git rev-list failure or
// Sscanf parse error). A non-zero rate means the gauge may be stale/missing
// for a repo — operators cannot distinguish "genuinely current" from
// "git errored" without this counter.
//
// Cardinality: 1 series (label "reason": "git_error" | "parse_error").
var indexCommitsBehindComputeErrors = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "embed_index_commits_behind_compute_errors_total",
		Help: "Errors preventing gocode_index_commits_behind update, by reason (git_error | parse_error).",
	},
	[]string{"reason"},
)

// recordRepoStateWriteFailure is the canonical handler for a SetRepoState
// failure: bumps embed_repo_state_write_failures_total and logs at Warn.
// All 5 SetRepoState call-sites (2 in IncrementalSync, 3 in indexRepo) call
// this instead of duplicating the counter-increment + log pattern.
func recordRepoStateWriteFailure(repoKey, context string, err error) {
	repoStateWriteFailuresTotal.Inc()
	slog.Warn("SetRepoState failed",
		slog.String("repo", repoKey),
		slog.String("context", context),
		slog.Any("error", err))
}

// recordCommitsBehind sets gocode_index_commits_behind{repo} to the number of
// commits the PERSISTED indexed_sha is behind the repo's main-branch tip.
//
// It reads the persisted state from the store (the value the app actually serves)
// and runs `git rev-list --count <stored>..<mainSHA>` to compute the delta.
//
// Edge cases:
//   - stored == "": bootstrap, repo never indexed. Sets 0 and returns (no bogus gauge).
//   - stored == mainSHA: 0 commits behind (up-to-date).
//   - git error or parse error: logs at Debug, bumps compute-error counter, skips Set.
func recordCommitsBehind(ctx context.Context, repoKey, root string, store repoStateGetter, mainSHA string) {
	stored, err := store.GetRepoState(ctx, repoKey)
	if err != nil {
		slog.Debug("recordCommitsBehind: GetRepoState failed",
			slog.String("repo", repoKey), slog.Any("error", err))
		return
	}
	// Bootstrap: never indexed yet. Report 0 — not meaningful but not bogus.
	if stored == "" {
		indexCommitsBehind.WithLabelValues(repoKey).Set(0)
		return
	}
	if stored == mainSHA {
		indexCommitsBehind.WithLabelValues(repoKey).Set(0)
		return
	}
	// Count commits between stored and mainSHA.
	spec := stored + ".." + mainSHA
	cmd := exec.CommandContext(ctx, "git", "-C", root, "rev-list", "--count", spec) //nolint:gosec // git subprocess with operator-controlled paths; same pattern as gitDiffNames
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, gitErr := cmd.Output()
	if gitErr != nil {
		slog.Debug("recordCommitsBehind: git rev-list failed",
			slog.String("repo", repoKey),
			slog.String("spec", spec),
			slog.String("stderr", strings.TrimSpace(stderr.String())),
			slog.Any("error", gitErr))
		indexCommitsBehindComputeErrors.WithLabelValues("git_error").Inc()
		return
	}
	var behind float64
	n, scanErr := fmt.Sscanf(strings.TrimSpace(string(out)), "%f", &behind)
	if scanErr != nil || n != 1 {
		slog.Debug("recordCommitsBehind: parse error",
			slog.String("repo", repoKey),
			slog.String("output", strings.TrimSpace(string(out))),
			slog.Any("error", scanErr))
		indexCommitsBehindComputeErrors.WithLabelValues("parse_error").Inc()
		return
	}
	indexCommitsBehind.WithLabelValues(repoKey).Set(behind)
}

// recordIncrementalSync increments embed_incremental_sync_total for the given
// result. Called at every return point of IncrementalSync.
// When err is non-nil (catastrophic failure), outcome = "error".
// When result.Errors is non-empty, outcome = "partial".
// Otherwise outcome = "success".
func recordIncrementalSync(result *IncrementalSyncResult, err error) {
	if result == nil {
		return
	}
	outcome := "success"
	switch {
	case err != nil:
		outcome = "error"
	case len(result.Errors) > 0:
		outcome = "partial"
	}
	incrementalSyncTotal.WithLabelValues(string(result.Mode), outcome).Inc()
}

func init() {
	// Pre-touch all counter label combinations so Prometheus exposes the series
	// immediately on startup (before any IncrementalSync call). Without this,
	// fresh-deploy dashboards show "no data" instead of 0.
	// See: observability-gaps.md "non-pre-touched counters" family.
	allModes := []IncrementalSyncMode{
		IncrementalSyncIncremental,
		IncrementalSyncSkipSHAMatch,
		IncrementalSyncFullFallbackBootstrap,
		IncrementalSyncFullFallbackNoGit,
		IncrementalSyncFullFallbackDiffError,
	}
	for _, mode := range allModes {
		for _, outcome := range []string{"success", "partial", "error"} {
			incrementalSyncTotal.WithLabelValues(string(mode), outcome)
		}
	}
	for _, kind := range []string{"embedded", "skipped", "deleted"} {
		incrementalFilesTotal.WithLabelValues(kind)
	}
	for _, outcome := range []string{"success", "error"} {
		indexFileDuration.WithLabelValues(outcome)
	}
	for _, reason := range []string{"unsupported_ext", "read_error"} {
		incrementalFilesUnsupportedTotal.WithLabelValues(reason)
	}
	for _, reason := range []string{"git_error", "parse_error"} {
		indexCommitsBehindComputeErrors.WithLabelValues(reason)
	}
}

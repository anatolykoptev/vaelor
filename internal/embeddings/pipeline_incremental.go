package embeddings

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"time"
)

// IncrementalSyncMode is the typed label for which code path IncrementalSync took.
// Values are stable wire strings used in Prometheus labels — do not rename.
type IncrementalSyncMode string

const (
	// IncrementalSyncIncremental is the normal git-diff path.
	IncrementalSyncIncremental IncrementalSyncMode = "incremental"
	// IncrementalSyncSkipSHAMatch is the fast-path when SHA is unchanged.
	IncrementalSyncSkipSHAMatch IncrementalSyncMode = "skip-sha-match"
	// IncrementalSyncFullFallbackBootstrap is triggered when no prior SHA exists in DB.
	IncrementalSyncFullFallbackBootstrap IncrementalSyncMode = "full-fallback-bootstrap"
	// IncrementalSyncFullFallbackNoGit is triggered when the path is not a git repo.
	IncrementalSyncFullFallbackNoGit IncrementalSyncMode = "full-fallback-no-git"
	// IncrementalSyncFullFallbackDiffError is triggered when git diff exec fails.
	IncrementalSyncFullFallbackDiffError IncrementalSyncMode = "full-fallback-diff-error"
)

// IncrementalSyncResult is the return value of Pipeline.IncrementalSync.
type IncrementalSyncResult struct {
	// Mode describes which code path was taken.
	Mode IncrementalSyncMode

	PrevSHA    string
	CurrentSHA string

	// FilesChanged is the count of files returned by git diff (0 on full-fallback paths).
	FilesChanged int
	// FilesEmbedded is the sum of FileIndexResult.Embedded across processed files
	// (or IndexResult.Indexed on full-fallback paths).
	FilesEmbedded int
	// FilesSkipped is the sum of FileIndexResult.Skipped (hash-matched symbols).
	FilesSkipped int
	// FilesDeleted is the sum of FileIndexResult.Deleted (tombstoned symbols).
	FilesDeleted int64

	// Errors collects per-file errors. When non-empty, SHA is NOT advanced so the
	// next boot retries the affected files (hash-skip from IndexFile makes retries cheap).
	Errors []error
}

// IncrementalSync reconciles a repo's embedding index via git-diff against the
// last-indexed SHA, calling Pipeline.IndexFile per changed file. Bumps
// SetRepoState only on full success (Errors empty).
//
// Falls back to full IndexRepo when:
//   - currentSHA == ""         (non-git path)
//   - prevSHA == ""            (never indexed — bootstrap)
//   - git diff exec failure    (corrupted state — safer to full re-scan)
//
// Same-SHA path (prevSHA == currentSHA) bumps indexed_at timestamp via
// SetRepoState but does no file work. Returns mode="skip-sha-match".
//
// Returns non-nil result describing the path taken. Returns error only on
// catastrophic failures (DB connection lost). Per-file errors are collected
// in Result.Errors, not returned — caller decides retry policy.
func (p *Pipeline) IncrementalSync(ctx context.Context, repoKey, root string) (*IncrementalSyncResult, error) {
	// Freshness lag gauge must be set on EVERY exit path, including early returns
	// for fallback paths (no-git, bootstrap, diff-error). Without this, a repo
	// that flips to a fallback path leaves a stale lag value — exactly when the
	// operator needs it accurate. We capture pointers so the defer reads the final
	// values at return time regardless of which path was taken.
	var finalResult *IncrementalSyncResult
	var finalErr error
	defer func() {
		if finalResult == nil {
			return
		}
		// lag=0: fully up-to-date (SHA advanced or same-SHA skip or successful fallback).
		// lag=1: SHA did not advance (partial embed failure or catastrophic fallback error).
		lag := 0.0
		if finalErr != nil || len(finalResult.Errors) > 0 {
			lag = 1.0
		}
		indexFreshnessLag.WithLabelValues(repoKey).Set(lag)
	}()

	// Step 1: resolve current main-branch SHA.
	// Treat both a real error (e.g. git repo with no main/master/HEAD ref) and an
	// empty return (non-git path) identically: we have no usable fingerprint, so
	// bulk fallback is the safe path. IndexRepo handles non-git via its own walk.
	// This ensures outcome="full-fallback-no-git" is always recorded by
	// fallbackToFull → recordIncrementalSync; the error branch previously bypassed
	// recordIncrementalSync entirely (code-quality MAJOR finding).
	currentSHA, _ := repoMainBranchSHA(root)
	if currentSHA == "" {
		// No fingerprint available — fall through to full index.
		res, fullErr := p.fallbackToFull(ctx, repoKey, root, IncrementalSyncFullFallbackNoGit)
		finalResult, finalErr = res, fullErr
		recordIncrementalSync(res, fullErr)
		return res, fullErr
	}

	// Step 2: fetch previous SHA. Swallow the error (treat "" on failure).
	prevSHA, _ := p.store.GetRepoState(ctx, repoKey)
	if prevSHA == "" {
		// Never indexed — bootstrap.
		res, fullErr := p.fallbackToFull(ctx, repoKey, root, IncrementalSyncFullFallbackBootstrap)
		finalResult, finalErr = res, fullErr
		recordIncrementalSync(res, fullErr)
		return res, fullErr
	}

	// Step 3: same-SHA fast path — bump timestamp only.
	if prevSHA == currentSHA {
		if err := p.store.SetRepoState(ctx, repoKey, currentSHA); err != nil {
			slog.Debug("incrementalSync: SetRepoState (same-SHA) failed",
				slog.String("repo", repoKey), slog.Any("error", err))
		}
		res := &IncrementalSyncResult{
			Mode:       IncrementalSyncSkipSHAMatch,
			PrevSHA:    prevSHA,
			CurrentSHA: currentSHA,
		}
		finalResult, finalErr = res, nil
		recordIncrementalSync(res, nil)
		return res, nil
	}

	// Step 4: compute diff.
	changedFiles, diffErr := gitDiffNames(ctx, root, prevSHA, currentSHA)
	if diffErr != nil {
		slog.Debug("incrementalSync: git diff failed, falling back to full",
			slog.String("repo", repoKey),
			slog.String("prev", prevSHA),
			slog.String("current", currentSHA),
			slog.Any("error", diffErr))
		res, fullErr := p.fallbackToFull(ctx, repoKey, root, IncrementalSyncFullFallbackDiffError)
		finalResult, finalErr = res, fullErr
		recordIncrementalSync(res, fullErr)
		return res, fullErr
	}

	// Step 5-7: index each changed file, collect errors.
	result := &IncrementalSyncResult{
		Mode:       IncrementalSyncIncremental,
		PrevSHA:    prevSHA,
		CurrentSHA: currentSHA,
	}

	for _, relPath := range changedFiles {
		result.FilesChanged++
		fileStart := time.Now()
		fr, fileErr := p.IndexFile(ctx, repoKey, root, relPath)
		elapsed := time.Since(fileStart)
		if fileErr != nil {
			indexFileDuration.WithLabelValues("error").Observe(elapsed.Seconds())
			result.Errors = append(result.Errors, fmt.Errorf("%s: %w", relPath, fileErr))
			// Continue — don't abort the batch. SHA will not advance if errors remain.
			continue
		}
		indexFileDuration.WithLabelValues("success").Observe(elapsed.Seconds())
		result.FilesEmbedded += fr.Embedded
		result.FilesSkipped += fr.Skipped
		result.FilesDeleted += fr.Deleted
	}

	// Record per-file kind counters.
	if result.FilesEmbedded > 0 {
		incrementalFilesTotal.WithLabelValues("embedded").Add(float64(result.FilesEmbedded))
	}
	if result.FilesSkipped > 0 {
		incrementalFilesTotal.WithLabelValues("skipped").Add(float64(result.FilesSkipped))
	}
	if result.FilesDeleted > 0 {
		incrementalFilesTotal.WithLabelValues("deleted").Add(float64(result.FilesDeleted))
	}

	// Step 7: advance SHA only on full success.
	if len(result.Errors) == 0 {
		if err := p.store.SetRepoState(ctx, repoKey, currentSHA); err != nil {
			slog.Debug("incrementalSync: SetRepoState failed",
				slog.String("repo", repoKey), slog.Any("error", err))
		}
	}

	// Wire finalResult so the deferred lag gauge sees the incremental result.
	// The defer at the top handles indexFreshnessLag.Set for ALL exit paths.
	finalResult = result

	recordIncrementalSync(result, nil)
	return result, nil
}

// fallbackToFull runs IndexRepo and maps its result to an IncrementalSyncResult.
// mode is the typed label recorded in result.Mode.
func (p *Pipeline) fallbackToFull(ctx context.Context, repoKey, root string, mode IncrementalSyncMode) (*IncrementalSyncResult, error) {
	full, err := p.IndexRepo(ctx, repoKey, root)
	if err != nil {
		return &IncrementalSyncResult{Mode: mode}, err
	}
	return &IncrementalSyncResult{
		Mode:          mode,
		FilesEmbedded: full.Indexed,
		FilesSkipped:  full.Skipped,
	}, nil
}

// gitDiffNames returns the list of files changed between prevSHA and currentSHA
// in the git repo at root. Returns an error if the git command fails.
// Stderr from git is captured and included in the error so operators can debug
// "bad object" / "ambiguous argument" / "unknown revision" failures.
func gitDiffNames(ctx context.Context, root, prevSHA, currentSHA string) ([]string, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", root, "diff", "--name-only", prevSHA, currentSHA)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git diff %s..%s: %w (stderr: %s)",
			prevSHA[:min(8, len(prevSHA))], currentSHA[:min(8, len(currentSHA))],
			err, strings.TrimSpace(stderr.String()))
	}

	var files []string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			files = append(files, line)
		}
	}
	return files, nil
}


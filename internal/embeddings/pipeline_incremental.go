package embeddings

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
)

// IncrementalSyncResult is the return value of Pipeline.IncrementalSync.
type IncrementalSyncResult struct {
	// Mode describes which code path was taken:
	//   "incremental"              — git-diff path (normal operation)
	//   "skip-sha-match"           — SHA unchanged; no work done
	//   "full-fallback-bootstrap"  — no prior SHA in DB; ran IndexRepo
	//   "full-fallback-no-git"     — path is not a git repo; ran IndexRepo
	//   "full-fallback-diff-error" — git diff exec failed; ran IndexRepo
	Mode string

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
	// Step 1: resolve current main-branch SHA.
	currentSHA, err := repoMainBranchSHA(root)
	if err != nil {
		return nil, fmt.Errorf("incrementalSync %s: resolve SHA: %w", repoKey, err)
	}
	if currentSHA == "" {
		// Non-git path — fall through to full index.
		return p.fallbackToFull(ctx, repoKey, root, "full-fallback-no-git")
	}

	// Step 2: fetch previous SHA. Swallow the error (treat "" on failure).
	prevSHA, _ := p.store.GetRepoState(ctx, repoKey)
	if prevSHA == "" {
		// Never indexed — bootstrap.
		return p.fallbackToFull(ctx, repoKey, root, "full-fallback-bootstrap")
	}

	// Step 3: same-SHA fast path — bump timestamp only.
	if prevSHA == currentSHA {
		if err := p.store.SetRepoState(ctx, repoKey, currentSHA); err != nil {
			slog.Debug("incrementalSync: SetRepoState (same-SHA) failed",
				slog.String("repo", repoKey), slog.Any("error", err))
		}
		return &IncrementalSyncResult{
			Mode:       "skip-sha-match",
			PrevSHA:    prevSHA,
			CurrentSHA: currentSHA,
		}, nil
	}

	// Step 4: compute diff.
	changedFiles, diffErr := gitDiffNames(ctx, root, prevSHA, currentSHA)
	if diffErr != nil {
		slog.Debug("incrementalSync: git diff failed, falling back to full",
			slog.String("repo", repoKey),
			slog.String("prev", prevSHA),
			slog.String("current", currentSHA),
			slog.Any("error", diffErr))
		return p.fallbackToFull(ctx, repoKey, root, "full-fallback-diff-error")
	}

	// Step 5-7: index each changed file, collect errors.
	result := &IncrementalSyncResult{
		Mode:       "incremental",
		PrevSHA:    prevSHA,
		CurrentSHA: currentSHA,
	}

	for _, relPath := range changedFiles {
		result.FilesChanged++
		fr, fileErr := p.IndexFile(ctx, repoKey, root, relPath)
		if fileErr != nil {
			result.Errors = append(result.Errors, fmt.Errorf("%s: %w", relPath, fileErr))
			// Continue — don't abort the batch. SHA will not advance if errors remain.
			continue
		}
		result.FilesEmbedded += fr.Embedded
		result.FilesSkipped += fr.Skipped
		result.FilesDeleted += fr.Deleted
	}

	// Step 7: advance SHA only on full success.
	if len(result.Errors) == 0 {
		if err := p.store.SetRepoState(ctx, repoKey, currentSHA); err != nil {
			slog.Debug("incrementalSync: SetRepoState failed",
				slog.String("repo", repoKey), slog.Any("error", err))
		}
	}

	return result, nil
}

// fallbackToFull runs IndexRepo and maps its result to an IncrementalSyncResult.
// mode is the string label recorded in result.Mode.
func (p *Pipeline) fallbackToFull(ctx context.Context, repoKey, root, mode string) (*IncrementalSyncResult, error) {
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

// min returns the smaller of a and b. Defined here for pre-1.21 compat;
// remove when GOVERSION >= 1.21 is guaranteed.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

package biomarkers

import (
	"context"
	"fmt"
	"math"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// priorDefectGitTimeout bounds a single git log call so a stuck repo
// (large pack, lock contention) can't stall the aggregator.
const priorDefectGitTimeout = 15 * time.Second

// defectMsgRE matches commit subjects that signal a defect repair.
//
// Patterns covered (case-insensitive, anchored loosely to allow
// Conventional-Commit prefixes like "fix(scope): msg"):
//   - fix / fixes / fixed (incl. "fix:" "fix(...)")
//   - hotfix
//   - bug / bugfix
//   - revert
//   - regress / regression
//
// Excluded: refactor, feat, chore, docs, style, test (no defect signal).
var defectMsgRE = regexp.MustCompile(
	`(?i)\b(fix(?:e[sd])?|hotfix|bug(?:fix)?|revert|regress(?:ion)?)\b`,
)

// batchDefectCacheKey is the context value used by ScoreFile callers to
// pre-populate prior_defect counts in one git invocation (see BatchPriorDefect).
// When present, PriorDefect.Score reads from the cache instead of spawning
// per-file git log.
type batchDefectCacheKey struct{}

// WithBatchDefectCache returns a context carrying pre-computed defect counts
// keyed by repo-relative path. Tools that score many files at once should
// call BatchPriorDefect first and attach the result here.
func WithBatchDefectCache(ctx context.Context, counts map[string]int) context.Context {
	return context.WithValue(ctx, batchDefectCacheKey{}, counts)
}

// batchDefectFromContext returns the cache attached via WithBatchDefectCache,
// or nil if absent.
func batchDefectFromContext(ctx context.Context) map[string]int {
	v, _ := ctx.Value(batchDefectCacheKey{}).(map[string]int)
	return v
}

// PriorDefect counts defect-commits on a file over the last 180 days,
// log-normalised so 1 fix ≈ 0.30, 5 ≈ 0.70, 20 ≈ 0.95.
type PriorDefect struct{}

// Name implements Biomarker.
func (PriorDefect) Name() string { return "prior_defect" }

// Score implements Biomarker.
func (PriorDefect) Score(ctx context.Context, repoRoot, relPath string) (float64, string, error) {
	var count int
	if cache := batchDefectFromContext(ctx); cache != nil {
		if c, ok := cache[relPath]; ok {
			count = c
		} else {
			// Path not in cache means caller did not include it in BatchPriorDefect.
			// Fall back to per-file git log so we still return a real answer.
			c, err := perFilePriorDefectCount(ctx, repoRoot, relPath)
			if err != nil {
				return 0, "", err
			}
			count = c
		}
	} else {
		c, err := perFilePriorDefectCount(ctx, repoRoot, relPath)
		if err != nil {
			return 0, "", err
		}
		count = c
	}
	if count <= 0 {
		return 0, "", nil
	}
	// log-normalise: log1p(count) / log1p(20) → 1 fix ≈ 0.30, 5 ≈ 0.70, 20 ≈ 0.95.
	score := math.Log1p(float64(count)) / math.Log1p(20.0)
	if score > 1 {
		score = 1
	}
	reason := fmt.Sprintf("%d defect-commits in last 180 days", count)
	return score, reason, nil
}

// perFilePriorDefectCount runs the original per-file git log. Returns the
// count and a wrapped error on git failure.
//
// NOTE: --follow is single-path only; BatchPriorDefect (multi-path) cannot
// use it. The batch path therefore misses pre-rename history. This is an
// accepted asymmetry: batched scoring trades rename-tracking for one git
// invocation. The per-file fallback (this function) does follow renames.
func perFilePriorDefectCount(ctx context.Context, repoRoot, relPath string) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, priorDefectGitTimeout)
	defer cancel()
	//nolint:gosec // repoRoot and relPath are trusted local paths.
	cmd := exec.CommandContext(ctx, "git", "-C", repoRoot,
		"log", "--since=180.days",
		"--no-merges",
		"--diff-filter=AMR",
		"--follow",
		"--pretty=format:%s",
		"--", relPath)
	out, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("git log: %w", err)
	}
	count := 0
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		if defectMsgRE.MatchString(line) {
			count++
		}
	}
	return count, nil
}

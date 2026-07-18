package biomarkers

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/anatolykoptev/vaelor/internal/compare"
)

// batchCreationCacheKey is the context value used by ScoreFile callers to
// pre-populate initial-creation line counts in one git invocation (see
// BatchInitialCreationLines). When present, ChurnRisk.Score reads from the
// cache instead of spawning per-file git log.
type batchCreationCacheKey struct{}

// WithBatchCreationCache returns a context carrying pre-computed initial-
// creation line counts keyed by repo-relative path. Tools that score many
// files at once should call BatchInitialCreationLines first and attach the
// result here.
func WithBatchCreationCache(ctx context.Context, counts map[string]int) context.Context {
	return context.WithValue(ctx, batchCreationCacheKey{}, counts)
}

// batchCreationFromContext returns the cache attached via
// WithBatchCreationCache, or nil if absent.
func batchCreationFromContext(ctx context.Context) map[string]int {
	v, _ := ctx.Value(batchCreationCacheKey{}).(map[string]int)
	return v
}

// ChurnRisk surfaces files whose post-creation rewrite churn exceeds their
// current size — Nagappan & Ball's relative-churn predictor.
//
// Window: 90 days (compare.CollectChurn called with a 90-day since).
// Initial file-creation additions are excluded via initialCreationLines
// (the first --diff-filter=A commit), so a freshly-added file doesn't
// register as risky — but post-creation GROWTH is correctly counted.
type ChurnRisk struct{}

// Name implements Biomarker.
func (ChurnRisk) Name() string { return "churn_risk" }

// Score implements Biomarker.
func (ChurnRisk) Score(ctx context.Context, repoRoot, relPath string) (float64, string, error) {
	stats, err := compare.CollectChurn(ctx, repoRoot, 90*24*time.Hour)
	if err != nil {
		return 0, "", fmt.Errorf("collect churn: %w", err)
	}
	cs, ok := stats[relPath]
	if !ok {
		return 0, "", nil
	}
	loc, err := countLines(filepath.Join(repoRoot, relPath))
	if err != nil || loc == 0 {
		return 0, "", nil
	}
	var created int
	if cache := batchCreationFromContext(ctx); cache != nil {
		if c, ok := cache[relPath]; ok {
			created = c
		} else {
			// Path not in cache → caller didn't batch it; fall back per-file.
			created = initialCreationLines(ctx, repoRoot, relPath)
		}
	} else {
		created = initialCreationLines(ctx, repoRoot, relPath)
	}
	// Subtract the one-time initial-creation additions, not the current LOC.
	// A file that grew post-creation keeps that growth churn in the signal.
	rawChurn := cs.Additions + cs.Deletions - created
	if rawChurn <= 0 {
		return 0, "", nil
	}
	rel := float64(rawChurn) / float64(loc)
	score := rel / 2.0
	if score > 1 {
		score = 1
	}
	if score < 0.1 {
		return 0, "below noise floor", nil
	}
	reason := fmt.Sprintf(
		"post-creation churn %.1fx (%d post-creation lines / %d LOC) in last 90 days",
		rel, rawChurn, loc,
	)
	return score, reason, nil
}

// churnRiskGitTimeout bounds the initial-creation git log call.
const churnRiskGitTimeout = 15 * time.Second

// numstatFields is the tab-separated field count in a git numstat line (add, del, path).
const numstatFields = 3

// initialCreationLines returns the additions of the commit that first
// added relPath within the --since=90.days window (git log --diff-filter=A
// --reverse, first numstat). Returns 0 when the file has no add-commit in
// the window (renamed, or created before the window) — callers treat 0 as
// "subtract nothing".
//
// Renames are NOT followed: --follow is incompatible with --diff-filter=A
// (a rename boundary is classified R, not A, so --follow yields empty).
// A renamed file's pre-rename creation is therefore not subtracted, which
// is consistent with CollectChurn also not following renames.
//
// On git error or timeout this returns 0 (subtract nothing), which
// over-counts churn for that file rather than under-counts — a safe
// degraded mode (flags a file as riskier, never hides risk).
func initialCreationLines(ctx context.Context, repoRoot, relPath string) int {
	ctx, cancel := context.WithTimeout(ctx, churnRiskGitTimeout)
	defer cancel()
	//nolint:gosec // trusted local paths
	cmd := exec.CommandContext(ctx, "git", "-C", repoRoot,
		"log", "--diff-filter=A", "--reverse", "--numstat",
		"--pretty=format:", "--since=90.days",
		"--", relPath)
	out, err := cmd.Output()
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		fields := strings.SplitN(line, "\t", numstatFields)
		if len(fields) == numstatFields {
			if n, err := strconv.Atoi(fields[0]); err == nil {
				return n // first add-commit's additions within the 90-day window
			}
		}
	}
	return 0
}

// countLines returns the number of newline-terminated lines in a file.
// Returns 0 for missing files (file may have been deleted after churn).
func countLines(path string) (int, error) {
	f, err := os.Open(path) //nolint:gosec // trusted internal call
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	defer f.Close()
	scan := bufio.NewScanner(f)
	scan.Buffer(make([]byte, 0, 64*1024), 8*1024*1024) //nolint:mnd
	n := 0
	for scan.Scan() {
		n++
	}
	return n, scan.Err()
}

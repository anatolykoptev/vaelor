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

// PriorDefect counts defect-commits on a file over the last 180 days,
// log-normalised so 1 fix ≈ 0.30, 5 ≈ 0.70, 20 ≈ 0.95.
type PriorDefect struct{}

// Name implements Biomarker.
func (PriorDefect) Name() string { return "prior_defect" }

// Score implements Biomarker.
func (PriorDefect) Score(ctx context.Context, repoRoot, relPath string) (float64, string, error) {
	ctx, cancel := context.WithTimeout(ctx, priorDefectGitTimeout)
	defer cancel()
	//nolint:gosec // repoRoot and relPath are trusted local paths.
	cmd := exec.CommandContext(ctx, "git", "-C", repoRoot,
		"log", "--since=180.days", "--pretty=format:%s", "--", relPath)
	out, err := cmd.Output()
	if err != nil {
		return 0, "", fmt.Errorf("git log: %w", err)
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
	if count == 0 {
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

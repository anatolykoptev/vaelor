package biomarkers

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/anatolykoptev/go-code/internal/compare"
)

// ChurnRisk surfaces files whose post-creation rewrite churn exceeds their
// current size. It implements a stable-size variant of Nagappan & Ball's
// relative-churn predictor.
//
// Window: 90 days (compare.CollectChurn called with a 90-day since).
//
// Saturation: rawChurn >= 2 × LOC → score 1.0.
//
// Known limitation (Phase 1 trade-off): the "exclude initial creation"
// term subtracts LOC unconditionally. For a file that grew substantially
// post-creation (e.g. created at 50 LOC, later additions brought it to
// 100 LOC), the formula yields rawChurn = 0 even though the post-creation
// modifications are real churn. A future per-commit accounting (split
// out the first commit's additions via `git log --diff-filter=A`) would
// fix this. Tracked as follow-up.
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
	// Exclude the one-time additions == LOC of initial file creation.
	// `max(0, (A+D) - LOC) / LOC` measures relative volume of post-creation
	// modifications — what Nagappan & Ball's "relative churn" captures.
	rawChurn := cs.Additions + cs.Deletions - loc
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

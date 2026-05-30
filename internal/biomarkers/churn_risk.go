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

	"github.com/anatolykoptev/go-code/internal/compare"
)

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
	created := initialCreationLines(ctx, repoRoot, relPath)
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

// initialCreationLines returns the additions of the commit that first
// added relPath (git log --diff-filter=A --reverse, first numstat).
// Returns 0 when the file has no add-commit in history (e.g. it always
// existed in the initial import) — callers treat 0 as "subtract nothing".
func initialCreationLines(ctx context.Context, repoRoot, relPath string) int {
	ctx, cancel := context.WithTimeout(ctx, churnRiskGitTimeout)
	defer cancel()
	//nolint:gosec // trusted local paths
	cmd := exec.CommandContext(ctx, "git", "-C", repoRoot,
		"log", "--diff-filter=A", "--reverse", "--numstat",
		"--pretty=format:", "--follow", "--", relPath)
	out, err := cmd.Output()
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		fields := strings.SplitN(line, "\t", 3)
		if len(fields) == 3 {
			if n, err := strconv.Atoi(fields[0]); err == nil {
				return n // first add-commit's additions
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

package biomarkers

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/anatolykoptev/go-code/internal/compare"
)

// ChurnRisk surfaces files whose recent line-volume churn exceeds their
// current size — Nagappan & Ball's "relative churn" predictor.
//
// Window: 90 days (matches compare.CollectChurn default — same git log call).
// Saturation: 2× rewrite over the window = score 1.0.
type ChurnRisk struct{}

// Name implements Biomarker.
func (ChurnRisk) Name() string { return "churn_risk" }

// Score implements Biomarker.
func (ChurnRisk) Score(ctx context.Context, repoRoot, relPath string) (float64, string, error) {
	stats, err := compare.CollectChurn(ctx, repoRoot)
	if err != nil {
		return 0, "", fmt.Errorf("collect churn: %w", err)
	}
	cs, ok := stats[relPath]
	if !ok || cs.Commits == 0 {
		return 0, "", nil
	}
	// Use symmetric churn volume: min(additions, deletions) × 2.
	// This measures code that was written and then replaced — pure creation
	// (Additions > 0, Deletions == 0) contributes zero, which is correct:
	// a file committed once and never edited is not "churned" in the
	// Nagappan & Ball sense. Raw Additions+Deletions double-counts creation.
	symChurn := cs.Deletions
	if cs.Additions < cs.Deletions {
		symChurn = cs.Additions
	}
	if symChurn == 0 {
		return 0, "", nil
	}
	loc, err := countLines(filepath.Join(repoRoot, relPath))
	if err != nil || loc == 0 {
		return 0, "", nil
	}
	rel := float64(2*symChurn) / float64(loc)
	score := rel / 2.0
	if score > 1 {
		score = 1
	}
	if score < 0.1 {
		// Below the noise floor: don't flag.
		return 0, "", nil
	}
	reason := fmt.Sprintf(
		"relative churn %.1fx (%d±/%d LOC) in last 90 days",
		rel, cs.Additions+cs.Deletions, loc,
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

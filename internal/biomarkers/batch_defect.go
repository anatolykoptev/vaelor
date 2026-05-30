package biomarkers

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// batchDefectGitTimeout bounds the single git log call covering all paths.
const batchDefectGitTimeout = 30 * time.Second

// BatchPriorDefect runs ONE `git log --since=180.days --name-only` for the
// given paths and returns a map of path -> count of defect-class commits
// (matching defectMsgRE: fix/hotfix/bug/revert/regress).
//
// Filters: --no-merges (merge commits aren't real fixes) + --diff-filter=AMR
// (skip Deleted/Copied/Type-changed — those don't represent "fixes" of the
// file). The per-file fallback path (perFilePriorDefectCount in
// prior_defect.go) shares most flags (--no-merges --diff-filter=AMR
// --since=180.days) but deliberately adds --follow for single-path rename
// tracking. Multi-path batch cannot use --follow, so renamed files score
// slightly higher via the per-file path than the batch path. This asymmetry
// is accepted: batch trades rename-tracking for one git invocation.
//
// Paths NOT touched by any matching commit return 0.
// Returns an empty (non-nil) map for empty paths.
func BatchPriorDefect(ctx context.Context, repoRoot string, paths []string) (map[string]int, error) {
	counts := make(map[string]int, len(paths))
	if len(paths) == 0 {
		return counts, nil
	}
	for _, p := range paths {
		counts[p] = 0
	}

	ctx, cancel := context.WithTimeout(ctx, batchDefectGitTimeout)
	defer cancel()

	// %x00 sentinel separates commits; %s is the subject. --name-only emits one path
	// per line after the subject.
	args := []string{"-C", repoRoot, "log", "--since=180.days",
		"--no-merges", "--diff-filter=AMR", "--name-only",
		"--pretty=format:%x00%s"}
	args = append(args, "--")
	args = append(args, paths...)
	//nolint:gosec // repoRoot + paths are trusted local inputs from resolveRoot.
	out, err := exec.CommandContext(ctx, "git", args...).Output()
	if err != nil {
		return counts, fmt.Errorf("batch git log: %w", err)
	}

	scanner := bufio.NewScanner(bytes.NewReader(out))
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024) //nolint:mnd
	curIsDefect := false
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "\x00") {
			subj := strings.TrimPrefix(line, "\x00")
			curIsDefect = defectMsgRE.MatchString(subj)
			continue
		}
		if line == "" {
			continue
		}
		if !curIsDefect {
			continue
		}
		if _, want := counts[line]; want {
			counts[line]++
		}
	}
	if err := scanner.Err(); err != nil {
		return counts, fmt.Errorf("scan batch git log: %w", err)
	}
	return counts, nil
}

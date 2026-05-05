package explore

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// collectRecentCommits fetches the last N commits and for each one counts the
// files changed using git diff-tree.  Unlike git log --shortstat (which, for a
// squash-merged commit, walks the entire component-commit range and returns
// the cumulative diff), diff-tree always inspects the single commit object, so
// the count is correct regardless of whether the commit was squash-merged or
// not.  Returns nil, nil when git is unavailable or the directory is not a git
// repository.
func collectRecentCommits(ctx context.Context, root string, limit int) ([]CommitSummary, error) {
	// First pass: collect commit hashes, messages, and dates.
	logCmd := exec.CommandContext(ctx, "git", "-C", root, "log",
		fmt.Sprintf("--max-count=%d", limit),
		"--no-merges",
		"--pretty=format:%h|%s|%ad",
		"--date=short",
	)
	logOut, err := logCmd.Output()
	if err != nil {
		// git unavailable or not a git repo — non-fatal
		return nil, nil
	}

	raw := strings.TrimSpace(string(logOut))
	if raw == "" {
		return nil, nil
	}

	var commits []CommitSummary
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 3)
		if len(parts) < 3 {
			continue
		}
		commits = append(commits, CommitSummary{
			Hash:    parts[0],
			Message: parts[1],
			Date:    parts[2],
		})
	}

	// Second pass: count files changed per commit using diff-tree.
	// diff-tree --name-only -r examines only the single commit object, so it is
	// correct for squash-merged commits (no cumulative range walk).
	for i := range commits {
		n, err2 := countDiffTreeFiles(ctx, root, commits[i].Hash)
		if err2 == nil {
			commits[i].Files = n
		}
	}

	return commits, nil
}

// countDiffTreeFiles returns the number of files touched by a single commit.
// It uses git diff-tree --no-commit-id --name-only -r which lists one path per
// line for every file that was added, modified, or deleted in that commit.
//
// For the initial (root) commit git diff-tree returns empty output with exit 0
// when called without --root, because there is no parent to diff against.  We
// detect the empty-but-success case and retry with --root, which instructs git
// to diff against the empty tree.
func countDiffTreeFiles(ctx context.Context, root, sha string) (int, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", root, "diff-tree",
		"--no-commit-id",
		"--name-only",
		"-r",
		sha,
	)
	out, err := cmd.Output()
	if err != nil {
		exploreFilesChangedMethodTotal.WithLabelValues("error").Inc()
		return 0, err
	}

	trimmed := strings.TrimSpace(string(out))
	if trimmed == "" {
		// Empty output with a successful exit means this is the initial
		// commit (no parent).  Re-run with --root to diff against the
		// empty tree.
		cmd2 := exec.CommandContext(ctx, "git", "-C", root, "diff-tree",
			"--no-commit-id",
			"--name-only",
			"--root",
			"-r",
			sha,
		)
		out2, err2 := cmd2.Output()
		if err2 != nil {
			exploreFilesChangedMethodTotal.WithLabelValues("error").Inc()
			return 0, err2
		}
		trimmed = strings.TrimSpace(string(out2))
		if trimmed == "" {
			exploreFilesChangedMethodTotal.WithLabelValues("empty_repo").Inc()
			return 0, nil
		}
		exploreFilesChangedMethodTotal.WithLabelValues("root_fallback").Inc()
		return len(strings.Split(trimmed, "\n")), nil
	}

	exploreFilesChangedMethodTotal.WithLabelValues("diff_tree").Inc()
	return len(strings.Split(trimmed, "\n")), nil
}

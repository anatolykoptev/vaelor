package explore

import (
	"bytes"
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
//
// For shallow clones (--depth=N) a commit at the shallow boundary has no parent
// object available locally; diff-tree also returns empty.  We disambiguate via
// isShallowBoundary: if the repo is shallow AND the commit has no visible parent
// we are at the boundary — returning 0 without the --root retry (which would
// incorrectly count every file in the tree as "changed").
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
		// Empty output with a successful exit: either (a) a true initial
		// commit with no parent, or (b) a commit at a shallow-clone boundary
		// where the parent object is absent.  The two cases look identical to
		// diff-tree, but --root must NOT be used for (b) because it would
		// diff against the empty tree and return ALL files in the checkout.
		if isShallowBoundary(ctx, root, sha) {
			exploreFilesChangedMethodTotal.WithLabelValues("shallow_boundary").Inc()
			return 0, nil
		}

		// True initial commit (or regular repo): diff against the empty tree.
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

// isShallowBoundary returns true when two conditions both hold:
//  1. The repository at root is a shallow clone
//     ("git rev-parse --is-shallow-repository" prints "true").
//  2. The commit sha is a shallow boundary: it declares a parent in its commit
//     object but the parent object is absent from the local store.
//
// When both are true the empty diff-tree output is caused by the missing parent
// object, not by sha being the true root commit of the project history.
//
// A true root commit (no parent anywhere, not just locally) has no "parent"
// line in its raw commit object; a shallow-boundary commit does.  This raw
// object check reliably distinguishes the two cases even in a depth-1 clone
// where rev-list traversal is blocked by the shallow graft.
func isShallowBoundary(ctx context.Context, root, sha string) bool {
	shallowOut, err := exec.CommandContext(ctx, "git", "-C", root,
		"rev-parse", "--is-shallow-repository").Output()
	if err != nil {
		return false
	}
	if !bytes.Equal(bytes.TrimSpace(shallowOut), []byte("true")) {
		return false
	}

	// Read the raw commit object and look for a "parent" header line.
	// A true root commit has no parent line at all; a shallow-boundary commit
	// has one (the parent SHA is recorded in the object even though the parent
	// object itself was not downloaded).
	catOut, err := exec.CommandContext(ctx, "git", "-C", root,
		"cat-file", "commit", sha).Output()
	if err != nil {
		return false
	}

	// The commit object format is a sequence of "key value\n" header lines
	// followed by a blank line and the commit message.  We only need to scan
	// the headers.
	for _, line := range strings.Split(string(catOut), "\n") {
		if line == "" {
			// Blank line marks the end of headers.
			break
		}
		if strings.HasPrefix(line, "parent ") {
			// sha has a declared parent — missing locally because of the
			// shallow graft.  This is a genuine shallow boundary.
			return true
		}
	}

	// No "parent" header: sha is a true root commit.  The empty diff-tree
	// output should be handled by the --root fallback.
	return false
}

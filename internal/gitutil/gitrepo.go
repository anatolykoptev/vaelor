// Package gitutil provides shared Git repository helpers used across
// multiple internal packages.
package gitutil

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// IsGitRepo reports whether root is the root of a git repository.
//
// It accepts both forms of the .git entry:
//   - a directory: standard single-worktree repository
//   - a regular file: linked worktree created by "git worktree add"
//     (the file contains a "gitdir: ..." pointer to the main repo)
//
// Anything other than a stat error at <root>/.git is treated as a valid
// git repo — downstream git commands will surface a more precise error if
// the entry is malformed.
func IsGitRepo(root string) bool {
	info, err := os.Stat(filepath.Join(root, ".git"))
	if err != nil {
		return false
	}
	return info.IsDir() || info.Mode().IsRegular()
}

// CommitsSince returns the number of commits touching each file under root
// since the given duration ago. Uses `git log --since=<dur> --name-only
// --pretty=format:` and counts file occurrences. Returns empty map on any
// error (best-effort — no error returned). Timeout: 5s.
func CommitsSince(ctx context.Context, root string, since time.Duration) map[string]int {
	sinceDate := time.Now().Add(-since).Format("2006-01-02")
	ctx5, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx5, "git", "-C", root,
		"log", fmt.Sprintf("--since=%s", sinceDate), "--name-only", "--pretty=format:")
	out, err := cmd.Output()
	if err != nil {
		return map[string]int{}
	}

	counts := map[string]int{}
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		counts[line]++
	}
	return counts
}

// originURLTimeout bounds the `git remote get-url origin` call.
const originURLTimeout = 5 * time.Second

// OriginURL returns the configured origin remote URL for the repo at root, or
// "" when there is no origin remote or git fails for any reason (best-effort,
// matching CommitsSince's degraded-mode convention — callers treat "" as
// "no shared identity").
func OriginURL(ctx context.Context, root string) string {
	ctx, cancel := context.WithTimeout(ctx, originURLTimeout)
	defer cancel()
	//nolint:gosec // root is a trusted local path from repofind.Discover.
	out, err := exec.CommandContext(ctx, "git", "-C", root, "remote", "get-url", "origin").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// FileDiffSince returns a unified diff of file (relative to root) between
// the commit at HEAD and the commit just before the since-duration window.
// Caps output at maxLines diff lines (excluding git diff header). Returns ""
// on any error or if file unchanged. Timeout: 10s.
func FileDiffSince(ctx context.Context, root, file string, since time.Duration, maxLines int) string {
	sinceDate := time.Now().Add(-since).Format("2006-01-02")
	ctx10, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	// Find the last commit before the since date to use as the "before" ref.
	// If no commit exists before the window (brand-new repo), fall back to the
	// oldest commit (root) so we still show what changed since the repo's start.
	refCmd := exec.CommandContext(ctx10, "git", "-C", root,
		"log", fmt.Sprintf("--until=%s", sinceDate), "--pretty=format:%H", "-1")
	refOut, err := refCmd.Output()
	var beforeRef string
	if err == nil && len(bytes.TrimSpace(refOut)) > 0 {
		beforeRef = strings.TrimSpace(string(refOut))
	} else {
		// Fall back: oldest commit reachable from HEAD.
		oldCmd := exec.CommandContext(ctx10, "git", "-C", root,
			"log", "--pretty=format:%H", "--reverse")
		oldOut, oldErr := oldCmd.Output()
		if oldErr != nil || len(bytes.TrimSpace(oldOut)) == 0 {
			return ""
		}
		// First line is the oldest commit.
		firstLine := strings.SplitN(strings.TrimSpace(string(oldOut)), "\n", 2)[0]
		if firstLine == "" {
			return ""
		}
		// Check if HEAD is the same as firstLine — single-commit repo, no diff.
		headCmd := exec.CommandContext(ctx10, "git", "-C", root, "rev-parse", "HEAD")
		headOut, headErr := headCmd.Output()
		if headErr != nil {
			return ""
		}
		if strings.TrimSpace(string(headOut)) == firstLine {
			return "" // single commit — nothing to diff
		}
		// Use parent of the first commit inside window — approximate with firstLine itself.
		// diff firstLine..HEAD shows all changes introduced after the initial commit.
		beforeRef = firstLine
	}

	diffCmd := exec.CommandContext(ctx10, "git", "-C", root,
		"diff", beforeRef, "HEAD", "--", file)
	diffOut, err := diffCmd.Output()
	if err != nil || len(diffOut) == 0 {
		return ""
	}

	// Cap at maxLines diff lines (lines starting with +/- after the header).
	lines := strings.Split(string(diffOut), "\n")
	var result []string
	diffLines := 0
	for _, l := range lines {
		result = append(result, l)
		if len(l) > 0 && (l[0] == '+' || l[0] == '-') && !strings.HasPrefix(l, "+++") && !strings.HasPrefix(l, "---") {
			diffLines++
			if diffLines >= maxLines {
				break
			}
		}
	}
	return strings.Join(result, "\n")
}

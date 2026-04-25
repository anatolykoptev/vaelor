package explore

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// collectRecentCommits fetches the last N commits using a single git log call
// with --shortstat. Returns nil, nil when git is unavailable or the directory
// is not a git repository.
func collectRecentCommits(ctx context.Context, root string, limit int) ([]CommitSummary, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", root, "log",
		fmt.Sprintf("--max-count=%d", limit),
		"--no-merges",
		"--pretty=format:COMMIT|%h|%s|%ad",
		"--date=short",
		"--shortstat",
	)
	out, err := cmd.Output()
	if err != nil {
		// git unavailable or not a git repo — non-fatal
		return nil, nil
	}

	raw := strings.TrimSpace(string(out))
	if raw == "" {
		return nil, nil
	}

	var commits []CommitSummary
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "COMMIT|") {
			parts := strings.SplitN(line, "|", 4)
			if len(parts) < 4 {
				continue
			}
			commits = append(commits, CommitSummary{
				Hash:    parts[1],
				Message: parts[2],
				Date:    parts[3],
			})
			continue
		}
		// shortstat line looks like: " 3 files changed, 42 insertions(+), 5 deletions(-)"
		// or " 1 file changed, 9 insertions(+)"
		if len(commits) > 0 && strings.Contains(line, "file") {
			fields := strings.Fields(line)
			if len(fields) > 0 {
				if n, err2 := strconv.Atoi(fields[0]); err2 == nil {
					commits[len(commits)-1].Files = n
				}
			}
		}
	}

	return commits, nil
}

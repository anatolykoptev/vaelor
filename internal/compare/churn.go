package compare

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// ChurnStats holds change frequency data for a single file.
type ChurnStats struct {
	// Commits is the number of commits that touched this file.
	Commits int `json:"commits"`

	// Additions is the total lines added across all commits.
	Additions int `json:"additions"`

	// Deletions is the total lines deleted across all commits.
	Deletions int `json:"deletions"`
}

// ChurnScore returns a weighted churn score as defined by Omen:
// sum(1.0 + (additions + deletions) / 100.0) per commit.
func (c ChurnStats) ChurnScore() float64 {
	return float64(c.Commits) + float64(c.Additions+c.Deletions)/100.0
}

// gitLogTimeout is the maximum time to wait for git log.
const gitLogTimeout = 30 * time.Second

// numstatFields is the expected number of tab-separated fields in a numstat line (add, del, path).
const numstatFields = 3

// CollectChurn runs git log --numstat on the given repo root and returns
// churn statistics per file (relative paths).
// Returns nil map and nil error for non-git directories.
func CollectChurn(ctx context.Context, root string) (map[string]ChurnStats, error) {
	key := churnCacheKey(root)
	if cached, ok := globalChurnCache.get(key); ok {
		return cached, nil
	}

	ctx, cancel := context.WithTimeout(ctx, gitLogTimeout)
	defer cancel()

	//nolint:gosec // root is a trusted local path from resolveRoot
	cmd := exec.CommandContext(ctx, "git", "-C", root, "log",
		"--numstat", "--format=", "--no-merges",
		"--diff-filter=AMRC")

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		// Not a git repo or git not available — non-fatal.
		if strings.Contains(stderr.String(), "not a git repository") {
			return nil, nil
		}
		return nil, fmt.Errorf("git log: %w: %s", err, stderr.String())
	}

	result := parseNumstatOutput(stdout.Bytes())
	globalChurnCache.set(key, result)
	return result, nil
}

// parseNumstatOutput parses the full output of git log --numstat --format=.
func parseNumstatOutput(data []byte) map[string]ChurnStats {
	result := make(map[string]ChurnStats)
	var currentFiles map[string]struct{}

	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := scanner.Text()

		if line == "" {
			// Commit boundary — flush current file set.
			for path := range currentFiles {
				stats := result[path]
				stats.Commits++
				result[path] = stats
			}
			currentFiles = make(map[string]struct{})
			continue
		}

		add, del, path, ok := parseNumstatLine(line)
		if !ok {
			continue
		}

		if currentFiles == nil {
			currentFiles = make(map[string]struct{})
		}
		currentFiles[path] = struct{}{}

		stats := result[path]
		stats.Additions += add
		stats.Deletions += del
		result[path] = stats
	}

	// Flush last commit block.
	for path := range currentFiles {
		stats := result[path]
		stats.Commits++
		result[path] = stats
	}

	return result
}

// parseNumstatLine parses a single numstat line: "ADD\tDEL\tPATH".
// Returns false for binary files (shown as "-\t-\t...") or malformed lines.
func parseNumstatLine(line string) (add, del int, path string, ok bool) {
	if line == "" {
		return 0, 0, "", false
	}

	parts := strings.SplitN(line, "\t", numstatFields)
	if len(parts) != numstatFields {
		return 0, 0, "", false
	}

	if parts[0] == "-" || parts[1] == "-" {
		return 0, 0, "", false
	}

	add, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, "", false
	}

	del, err = strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, "", false
	}

	path = parts[2]

	// Handle renames: {old => new}/file.go → new/file.go
	if strings.Contains(path, "{") {
		path = resolveRenamePath(path)
	}

	return add, del, path, true
}

// resolveRenamePath resolves git rename notation: "prefix/{old => new}/suffix" → "prefix/new/suffix".
func resolveRenamePath(path string) string {
	open := strings.Index(path, "{")
	closeBrace := strings.Index(path, "}")
	if open < 0 || closeBrace < 0 || closeBrace <= open {
		return path
	}

	prefix := path[:open]
	suffix := path[closeBrace+1:]
	inner := path[open+1 : closeBrace]

	arrow := strings.Index(inner, " => ")
	if arrow < 0 {
		return path
	}

	newPart := inner[arrow+4:]
	return prefix + newPart + suffix
}

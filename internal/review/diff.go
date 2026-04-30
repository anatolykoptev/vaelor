package review

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// FileDiff describes changes to a single file.
type FileDiff struct {
	Path       string      // relative file path
	Added      int         // lines added
	Removed    int         // lines removed
	LineRanges []LineRange // changed line ranges in the new version
}

// LineRange is an inclusive range of line numbers.
type LineRange struct {
	Start int
	End   int
}

// ChangedFiles returns files changed between base ref and head.
// If base is empty, falls back to staged changes (git diff --cached).
// If head is empty, "HEAD" is used (back-compat for delta reviews of the
// current checkout). For PR reviews, callers pass "FETCH_HEAD" after a
// `git fetch origin pull/N/head` so the diff reflects PR contents instead
// of the warm-clone HEAD (which usually points at main).
func ChangedFiles(ctx context.Context, repoRoot, base, head string) ([]FileDiff, error) {
	if base == "" {
		return diffCached(ctx, repoRoot)
	}
	if head == "" {
		head = "HEAD"
	}
	return diffBase(ctx, repoRoot, base, head)
}

func diffBase(ctx context.Context, root, base, head string) ([]FileDiff, error) {
	out, err := GitExec(ctx, root, "diff", "--numstat", base, head)
	if err != nil {
		return nil, fmt.Errorf("git diff: %w", err)
	}
	files := parseNumstat(out)

	uniOut, err := GitExec(ctx, root, "diff", "--unified=0", base, head)
	if err != nil {
		return nil, fmt.Errorf("git diff unified: %w", err)
	}
	rangeMap := parseUnifiedRanges(uniOut)
	for i := range files {
		files[i].LineRanges = rangeMap[files[i].Path]
	}
	return files, nil
}

func diffCached(ctx context.Context, root string) ([]FileDiff, error) {
	out, err := GitExec(ctx, root, "diff", "--cached", "--numstat")
	if err != nil {
		return nil, fmt.Errorf("git diff --cached: %w", err)
	}
	files := parseNumstat(out)

	uniOut, err := GitExec(ctx, root, "diff", "--cached", "--unified=0")
	if err != nil {
		return nil, fmt.Errorf("git diff --cached unified: %w", err)
	}
	rangeMap := parseUnifiedRanges(uniOut)
	for i := range files {
		files[i].LineRanges = rangeMap[files[i].Path]
	}
	return files, nil
}

// GitExec runs a git command in the given directory. Exported for use by tool_review_pr.go.
func GitExec(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%s: %s", err, stderr.String())
	}
	return stdout.String(), nil
}

func parseNumstat(out string) []FileDiff {
	var files []FileDiff
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) < 3 {
			continue
		}
		added, _ := strconv.Atoi(parts[0])
		removed, _ := strconv.Atoi(parts[1])
		files = append(files, FileDiff{
			Path:    parts[2],
			Added:   added,
			Removed: removed,
		})
	}
	return files
}

// parseUnifiedRanges extracts changed line ranges from unified diff output.
func parseUnifiedRanges(out string) map[string][]LineRange {
	result := make(map[string][]LineRange)
	var curFile string
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "+++ b/") {
			curFile = strings.TrimPrefix(line, "+++ b/")
		} else if strings.HasPrefix(line, "@@") && curFile != "" {
			if r, ok := parseHunkHeader(line); ok {
				result[curFile] = append(result[curFile], r)
			}
		}
	}
	return result
}

// parseHunkHeader parses "@@ -old,len +new,start @@ ..." into a LineRange.
func parseHunkHeader(line string) (LineRange, bool) {
	idx := strings.Index(line, "+")
	if idx < 0 {
		return LineRange{}, false
	}
	rest := line[idx+1:]
	end := strings.Index(rest, " ")
	if end < 0 {
		end = len(rest)
	}
	rest = rest[:end]

	parts := strings.SplitN(rest, ",", 2)
	if len(parts) == 0 || parts[0] == "" {
		return LineRange{}, false
	}
	start, err := strconv.Atoi(parts[0])
	if err != nil {
		return LineRange{}, false
	}
	length := 1
	if len(parts) > 1 {
		length, _ = strconv.Atoi(parts[1])
	}
	if length == 0 {
		return LineRange{}, false
	}
	return LineRange{Start: start, End: start + length - 1}, true
}

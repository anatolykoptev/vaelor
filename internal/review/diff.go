package review

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
	return ChangedFilesRewrite(ctx, repoRoot, nil, base, head)
}

// ChangedFilesRewrite is like ChangedFiles but applies pathRewrite when
// resolving gitdir paths inside worktree .git pointer files. Pass nil when
// no path-mapping is required.
func ChangedFilesRewrite(ctx context.Context, repoRoot string, pathRewrite func(string) string, base, head string) ([]FileDiff, error) {
	if base == "" {
		return diffCachedRewrite(ctx, repoRoot, pathRewrite)
	}
	if head == "" {
		head = "HEAD"
	}
	return diffBaseRewrite(ctx, repoRoot, pathRewrite, base, head)
}

func diffBaseRewrite(ctx context.Context, root string, pathRewrite func(string) string, base, head string) ([]FileDiff, error) {
	out, err := GitExecRewrite(ctx, root, pathRewrite, "diff", "--numstat", base, head)
	if err != nil {
		return nil, fmt.Errorf("git diff: %w", err)
	}
	files := parseNumstat(out)

	uniOut, err := GitExecRewrite(ctx, root, pathRewrite, "diff", "--unified=0", base, head)
	if err != nil {
		return nil, fmt.Errorf("git diff unified: %w", err)
	}
	rangeMap := parseUnifiedRanges(uniOut)
	for i := range files {
		files[i].LineRanges = rangeMap[files[i].Path]
	}
	return files, nil
}

func diffCachedRewrite(ctx context.Context, root string, pathRewrite func(string) string) ([]FileDiff, error) {
	out, err := GitExecRewrite(ctx, root, pathRewrite, "diff", "--cached", "--numstat")
	if err != nil {
		return nil, fmt.Errorf("git diff --cached: %w", err)
	}
	files := parseNumstat(out)

	uniOut, err := GitExecRewrite(ctx, root, pathRewrite, "diff", "--cached", "--unified=0")
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
//
// When dir is a git worktree (its .git entry is a file rather than a directory),
// GitExec automatically switches to --git-dir / --work-tree invocation style so
// that git can locate the worktree admin directory. The optional pathRewrite func
// is applied to the gitdir path extracted from the .git file; pass nil when no
// path-mapping is needed (e.g. non-containerised callers).
func GitExec(ctx context.Context, dir string, args ...string) (string, error) {
	return gitExec(ctx, dir, nil, args...)
}

// GitExecRewrite is like GitExec but applies pathRewrite to the gitdir path
// resolved from a worktree .git file. Use this when git runs inside a container
// where filesystem paths differ from the host paths embedded in .git files.
func GitExecRewrite(ctx context.Context, dir string, pathRewrite func(string) string, args ...string) (string, error) {
	return gitExec(ctx, dir, pathRewrite, args...)
}

// gitExec is the shared implementation for GitExec and GitExecRewrite.
func gitExec(ctx context.Context, dir string, pathRewrite func(string) string, args ...string) (string, error) {
	cmdArgs, env := resolveGitArgs(dir, pathRewrite, args)
	cmd := exec.CommandContext(ctx, "git", cmdArgs...) //nolint:gosec // cmdArgs is constructed from safe git subcommand args
	cmd.Dir = dir
	if len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%w: %s", err, stderr.String())
	}
	return stdout.String(), nil
}

// resolveGitArgs inspects dir/.git and, when it is a worktree pointer file,
// prepends --git-dir=<gitdir> --work-tree=<dir> to args so git can locate the
// worktree admin directory even when PATH_MAPPINGS remap the embedded path.
// Returns the final arg slice and any extra env vars needed.
func resolveGitArgs(dir string, pathRewrite func(string) string, args []string) ([]string, []string) {
	gitPath := filepath.Join(dir, ".git")
	info, err := os.Stat(gitPath)
	if err != nil || !info.Mode().IsRegular() {
		// Normal repo (directory) or stat error — let git resolve as usual.
		return args, nil
	}

	// Worktree: .git is a file containing "gitdir: <path>".
	data, err := os.ReadFile(gitPath)
	if err != nil {
		return args, nil
	}
	line := strings.TrimSpace(string(data))
	const prefix = "gitdir: "
	if !strings.HasPrefix(line, prefix) {
		return args, nil
	}
	gitdir := strings.TrimPrefix(line, prefix)

	// Apply container path mapping when provided.
	if pathRewrite != nil {
		gitdir = pathRewrite(gitdir)
	}

	// If the resolved gitdir still doesn't exist, fall back to letting git
	// try on its own — it may succeed in environments where the path is valid.
	if _, err := os.Stat(gitdir); err != nil { //nolint:gosec // gitdir is extracted from a .git pointer file written by git itself
		return args, nil
	}

	// Prepend --git-dir and --work-tree before the subcommand arguments.
	final := make([]string, 0, len(args)+2)
	final = append(final, "--git-dir="+gitdir, "--work-tree="+dir)
	final = append(final, args...)
	return final, nil
}

func parseNumstat(out string) []FileDiff {
	var files []FileDiff
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 3) //nolint:mnd // 3 = tab-separated fields in git numstat output
		if len(parts) < 3 {                    //nolint:mnd // same constant: must have all 3 fields
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

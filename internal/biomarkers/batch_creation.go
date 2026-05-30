package biomarkers

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

// batchCreationGitTimeout bounds the single git log call covering all paths.
const batchCreationGitTimeout = 30 * time.Second

// BatchInitialCreationLines runs ONE `git log --diff-filter=A --reverse
// --numstat` for all paths and returns path -> additions of the FIRST
// (oldest, via --reverse) add-commit within the 90-day window.
//
// Replaces N per-file initialCreationLines spawns with one git invocation
// (BUG-FH-2b: get_file_health cold 34s → bounded by a single git log).
//
// Mirrors initialCreationLines' single-path semantics: --diff-filter=A
// limits numstat rows to added files; the first row per path (oldest, via
// --reverse) is its creation. Renames are NOT followed (--follow is
// incompatible with --diff-filter=A), consistent with the per-file path.
//
// Paths with no add-commit in the window return 0. Empty input → empty
// (non-nil) map.
func BatchInitialCreationLines(ctx context.Context, repoRoot string, paths []string) (map[string]int, error) {
	result := make(map[string]int, len(paths))
	if len(paths) == 0 {
		return result, nil
	}
	want := make(map[string]struct{}, len(paths))
	for _, p := range paths {
		result[p] = 0
		want[p] = struct{}{}
	}

	ctx, cancel := context.WithTimeout(ctx, batchCreationGitTimeout)
	defer cancel()

	args := []string{"-C", repoRoot, "log", "--diff-filter=A", "--reverse",
		"--numstat", "--pretty=format:", "--since=90.days", "--"}
	args = append(args, paths...)
	//nolint:gosec // repoRoot + paths are trusted local inputs from resolveRoot.
	out, err := exec.CommandContext(ctx, "git", args...).Output()
	if err != nil {
		return result, fmt.Errorf("batch creation git log: %w", err)
	}

	seen := make(map[string]bool, len(paths))
	scanner := bufio.NewScanner(bytes.NewReader(out))
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024) //nolint:mnd
	for scanner.Scan() {
		fields := strings.SplitN(scanner.Text(), "\t", numstatFields)
		if len(fields) != numstatFields {
			continue
		}
		path := fields[2]
		if _, ok := want[path]; !ok {
			continue
		}
		if seen[path] {
			continue // first (oldest, --reverse) add wins
		}
		if n, err := strconv.Atoi(fields[0]); err == nil {
			result[path] = n
			seen[path] = true
		}
	}
	if err := scanner.Err(); err != nil {
		return result, fmt.Errorf("scan batch creation git log: %w", err)
	}
	return result, nil
}

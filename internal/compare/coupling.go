package compare

import (
	"bytes"
	"context"
	"os/exec"
	"sort"
	"strings"
)

// defaultMinCoChanges is the default minimum co-change count to include a pair.
const defaultMinCoChanges = 3

// maxCouplingPairs is the maximum number of pairs to return.
const maxCouplingPairs = 20

// maxFilesPerCommit is the max files in a commit before it's skipped (bulk/merge noise).
const maxFilesPerCommit = 20

// CoupledPair represents two files that frequently change together in git history.
type CoupledPair struct {
	FileA     string  `json:"fileA"`
	FileB     string  `json:"fileB"`
	CoChanges int     `json:"coChanges"`
	Ratio     float64 `json:"ratio"` // coChanges / max(changesA, changesB)
}

// CollectCoupling analyzes git log to find files that frequently change together.
// Returns pairs with at least minCoChanges co-occurrences, sorted by frequency desc.
// Looks at the last year of history. Returns nil if git is unavailable.
//
// Rename-aware: git log --name-only emits the path AS OF EACH COMMIT, so a
// rename within the window (e.g. cmd/go-code/ -> cmd/vaelor/) leaves pre-rename
// commits keyed on stale paths. CollectCoupling detects renames in the window
// and rewrites historical paths to their current (post-rename) location before
// counting pairs — otherwise suggest_reviewers, called with current PR paths,
// sees co-change=0 for recently-renamed files (bug #355).
func CollectCoupling(ctx context.Context, root string, minCoChanges int) []CoupledPair {
	key := couplingCacheKey(root, minCoChanges)
	if cached, ok := globalCouplingCache.get(key); ok {
		return cached
	}

	//nolint:gosec // root is a trusted local path from resolveRoot
	cmd := exec.CommandContext(ctx, "git", "-C", root,
		"log", "--name-only", "--format=%x00", "--since=1 year")

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil
	}

	// Build old->current rename map for the same window so pre-rename commits
	// can be rewritten to their current path. -M enables rename detection;
	// --diff-filter=R emits only rename records as "R<score>\told\tnew".
	renameMap := collectRenames(ctx, root)

	commits := parseCommits(stdout.String())

	// Count per-file changes and co-changes.
	fileChanges := make(map[string]int)
	coChanges := make(map[string]int)

	for _, files := range commits {
		// Rewrite historical paths to their current (post-rename) location
		// and dedup: a pure-rename commit lists both old and new path, which
		// after rewrite collapses to a single logical file.
		seen := make(map[string]struct{}, len(files))
		var resolved []string
		for _, f := range files {
			f = resolveRename(renameMap, f)
			if _, ok := seen[f]; ok {
				continue
			}
			seen[f] = struct{}{}
			resolved = append(resolved, f)
		}
		files = resolved

		// Filter out compiled artifacts from coupling analysis.
		var srcFiles []string
		for _, f := range files {
			if !IsCompiledArtifact(f) {
				srcFiles = append(srcFiles, f)
			}
		}
		files = srcFiles

		if len(files) < 2 || len(files) > maxFilesPerCommit {
			// Track single-file changes for ratio computation.
			for _, f := range files {
				fileChanges[f]++
			}
			continue
		}

		for _, f := range files {
			fileChanges[f]++
		}

		// Count all pairs within this commit.
		for i := 0; i < len(files); i++ {
			for j := i + 1; j < len(files); j++ {
				a, b := files[i], files[j]
				if a == b {
					continue
				}
				if a > b {
					a, b = b, a
				}
				key := a + "\x00" + b
				coChanges[key]++
			}
		}
	}

	pairs := buildPairs(coChanges, fileChanges, minCoChanges)

	sort.Slice(pairs, func(i, j int) bool {
		return pairs[i].CoChanges > pairs[j].CoChanges
	})

	if len(pairs) > maxCouplingPairs {
		pairs = pairs[:maxCouplingPairs]
	}

	globalCouplingCache.set(key, pairs)
	return pairs
}

// collectRenames builds an old->new path map for renames within the coupling
// window (--since=1 year, matching CollectCoupling). Returns nil if git is
// unavailable or no renames are detected. The map is resolved transitively in
// resolveRename so chained renames (A->B->C) collapse to the final path.
//
//nolint:gosec // root is a trusted local path from resolveRoot
func collectRenames(ctx context.Context, root string) map[string]string {
	cmd := exec.CommandContext(ctx, "git", "-C", root,
		"log", "--name-status", "-M", "--diff-filter=R",
		"--since=1 year", "--format=")

	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return nil
	}

	m := make(map[string]string)
	for _, line := range strings.Split(stdout.String(), "\n") {
		// Lines look like: R100\told/path.go\tnew/path.go
		if len(line) < 2 || line[0] != 'R' {
			continue
		}
		parts := strings.Split(line[1:], "\t")
		if len(parts) < 3 {
			continue
		}
		old, nw := strings.TrimSpace(parts[1]), strings.TrimSpace(parts[2])
		if old == "" || nw == "" || old == nw {
			continue
		}
		m[old] = nw
	}
	if len(m) == 0 {
		return nil
	}
	return m
}

// resolveRename rewrites a historical path to its current (post-rename)
// location, following chained renames (A->B->C) up to a small bound to avoid
// cycles in malformed history.
func resolveRename(renameMap map[string]string, path string) string {
	if renameMap == nil {
		return path
	}
	for i := 0; i < 16; i++ {
		nw, ok := renameMap[path]
		if !ok {
			return path
		}
		path = nw
	}
	return path
}

// parseCommits splits git log output into per-commit file lists.
// The null byte separator is emitted by --format=%x00.
func parseCommits(output string) [][]string {
	var commits [][]string
	var current []string

	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.Contains(line, "\x00") {
			// Null byte marks commit boundary.
			if len(current) > 0 {
				commits = append(commits, current)
				current = nil
			}
			continue
		}
		current = append(current, line)
	}

	if len(current) > 0 {
		commits = append(commits, current)
	}

	return commits
}

// buildPairs filters co-change map into CoupledPair slice with ratio computed.
func buildPairs(coChanges, fileChanges map[string]int, minCoChanges int) []CoupledPair {
	var pairs []CoupledPair

	for key, count := range coChanges {
		if count < minCoChanges {
			continue
		}

		parts := strings.SplitN(key, "\x00", 2)
		if len(parts) != 2 {
			continue
		}

		a, b := parts[0], parts[1]
		changesA := fileChanges[a]
		changesB := fileChanges[b]

		maxChanges := changesA
		if changesB > maxChanges {
			maxChanges = changesB
		}

		var ratio float64
		if maxChanges > 0 {
			ratio = float64(count) / float64(maxChanges)
		}

		pairs = append(pairs, CoupledPair{
			FileA:     a,
			FileB:     b,
			CoChanges: count,
			Ratio:     ratio,
		})
	}

	return pairs
}

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

	commits := parseCommits(stdout.String())

	// Count per-file changes and co-changes.
	fileChanges := make(map[string]int)
	coChanges := make(map[string]int)

	for _, files := range commits {
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

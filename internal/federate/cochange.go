package federate

import (
	"bufio"
	"bytes"
	"context"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"
)

// crossCoChangeGitTimeout bounds each per-repo git log.
const crossCoChangeGitTimeout = 30 * time.Second

// crossCoChangeSinceDays bounds history (matches CollectCoupling's 1-year window).
const crossCoChangeSinceDays = 365

// maxFilesPerCommitFederate skips bulk/merge commits (mirror compare.maxFilesPerCommit).
const maxFilesPerCommitFederate = 20

// CrossPair is two files in DIFFERENT repos that change together within a window.
type CrossPair struct {
	RepoA     string `json:"repoA"`
	FileA     string `json:"fileA"`
	RepoB     string `json:"repoB"`
	FileB     string `json:"fileB"`
	CoChanges int    `json:"coChanges"`
}

// touch is one (repo, file) change at a committer timestamp.
type touch struct {
	repo string
	file string
	ts   int64
}

// pairKey is the canonical identity of a cross-repo file-pair.
type pairKey struct {
	repoA, fileA, repoB, fileB string
}

// CrossRepoCoChange finds file-pairs spanning DIFFERENT repos that change
// within windowHours of each other, at least minPairs times.
//
// Per repo it runs one `git log --name-only --pretty=format:%x00%ct`, parses
// (timestamp, files), and buckets each (repo, file) touch into a window of
// windowHours. Two touches from different repos in the same bucket form a
// cross-repo co-occurrence. Pairs are counted and filtered by minPairs.
//
// Returns nil when fewer than 2 repos, a non-positive window, or no touches
// (best-effort, like CollectCoupling — a repo whose git log fails is skipped).
//
// The fixed ts/windowSec bucketing is a coarse approximation: two commits a
// second apart but across a bucket boundary are missed. Accepted for "roughly
// coordinated changes"; a sliding window would be heavier (YAGNI here).
func CrossRepoCoChange(ctx context.Context, repos []RepoRef, windowHours, minPairs int) []CrossPair {
	if len(repos) < 2 || windowHours <= 0 {
		return nil
	}
	var touches []touch
	for _, r := range repos {
		touches = append(touches, collectTouches(ctx, r)...)
	}
	if len(touches) == 0 {
		return nil
	}

	windowSec := int64(windowHours) * 3600
	buckets := make(map[int64][]touch)
	for _, t := range touches {
		b := t.ts / windowSec
		buckets[b] = append(buckets[b], t)
	}

	counts := make(map[pairKey]int)
	for _, group := range buckets {
		for i := 0; i < len(group); i++ {
			for j := i + 1; j < len(group); j++ {
				a, b := group[i], group[j]
				if a.repo == b.repo {
					continue // same-repo coupling is CollectCoupling's job
				}
				counts[canonicalPair(a, b)]++
			}
		}
	}

	var out []CrossPair
	for k, n := range counts {
		if n >= minPairs {
			out = append(out, CrossPair{
				RepoA: k.repoA, FileA: k.fileA,
				RepoB: k.repoB, FileB: k.fileB,
				CoChanges: n,
			})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].CoChanges != out[j].CoChanges {
			return out[i].CoChanges > out[j].CoChanges
		}
		if out[i].RepoA != out[j].RepoA {
			return out[i].RepoA < out[j].RepoA
		}
		if out[i].FileA != out[j].FileA {
			return out[i].FileA < out[j].FileA
		}
		if out[i].RepoB != out[j].RepoB {
			return out[i].RepoB < out[j].RepoB
		}
		return out[i].FileB < out[j].FileB
	})
	return out
}

// canonicalPair orders the two touches by (repo, file) so (chat/x, edge/y) and
// (edge/y, chat/x) collapse to one key.
func canonicalPair(a, b touch) pairKey {
	if a.repo > b.repo || (a.repo == b.repo && a.file > b.file) {
		a, b = b, a
	}
	return pairKey{repoA: a.repo, fileA: a.file, repoB: b.repo, fileB: b.file}
}

// collectTouches runs one git log for the repo and returns a touch per
// (file) per commit, tagged with the repo slug and committer timestamp.
// Bulk/merge commits (>maxFilesPerCommitFederate files) are skipped.
func collectTouches(ctx context.Context, r RepoRef) []touch {
	ctx, cancel := context.WithTimeout(ctx, crossCoChangeGitTimeout)
	defer cancel()
	//nolint:gosec // r.Root is a trusted local path from ResolveRepos.
	cmd := exec.CommandContext(ctx, "git", "-C", r.Root, "log",
		"--name-only", "--pretty=format:%x00%ct",
		"--since="+strconv.Itoa(crossCoChangeSinceDays)+" days")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return nil
	}

	var touches []touch
	var curTS int64
	var curFiles []string
	flush := func() {
		if len(curFiles) > maxFilesPerCommitFederate {
			curFiles = nil
			return
		}
		for _, f := range curFiles {
			touches = append(touches, touch{repo: r.Slug, file: f, ts: curTS})
		}
		curFiles = nil
	}
	scanner := bufio.NewScanner(bytes.NewReader(stdout.Bytes()))
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024) //nolint:mnd
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "\x00") {
			flush()
			tsStr := strings.TrimPrefix(line, "\x00")
			ts, _ := strconv.ParseInt(strings.TrimSpace(tsStr), 10, 64)
			curTS = ts
			continue
		}
		if line == "" {
			continue
		}
		curFiles = append(curFiles, line)
	}
	flush()
	return touches
}

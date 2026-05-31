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

	"github.com/anatolykoptev/go-code/internal/artifactfilter"
	"github.com/anatolykoptev/go-kit/score"
)

// crossCoChangeGitTimeout bounds each per-repo git log.
const crossCoChangeGitTimeout = 30 * time.Second

// crossCoChangeSinceDays bounds history (matches CollectCoupling's 1-year window).
const crossCoChangeSinceDays = 365

// maxFilesPerCommitFederate skips bulk/merge commits (mirror compare.maxFilesPerCommit).
const maxFilesPerCommitFederate = 20

// crossCoChangeMaxWindowHours caps the window so a pathological operator input
// (e.g. a 1-year window) can't collapse all history into one O(k²) bucket.
// One week is already generous for "coordinated change" — beyond it the
// co-change signal is meaningless.
const crossCoChangeMaxWindowHours = 24 * 7

// maxCrossPairs bounds the returned slice (mirrors compare.maxCouplingPairs).
const maxCrossPairs = 20

// CrossPair is two files in DIFFERENT repos that change together within a window.
type CrossPair struct {
	RepoA           string  `json:"repoA"`
	FileA           string  `json:"fileA"`
	RepoB           string  `json:"repoB"`
	FileB           string  `json:"fileB"`
	CoChanges       int     `json:"coChanges"`
	Score           float64 `json:"score"`
	G2              float64 `json:"g2"`
	Significance    string  `json:"significance"`
	Confidence      float64 `json:"confidence"`
	ConfidenceLevel string  `json:"confidenceLevel"`
}

// RepoTouch is one (repo, file) change at a committer timestamp.
// Exported so the resilience layer in the tool handler can pass pre-warmed
// touches to CrossRepoCoChangeFromTouches without re-running git log.
type RepoTouch struct {
	repo string
	file string
	ts   int64
}

// touch is an alias for RepoTouch kept for internal brevity.
type touch = RepoTouch

// pairKey is the canonical identity of a cross-repo file-pair.
type pairKey struct {
	repoA, fileA, repoB, fileB string
}

// fileKey identifies a (repo, file) across buckets.
type fileKey struct {
	repo, file string
}

// CrossRepoCoChange finds file-pairs spanning DIFFERENT repos that change
// within windowHours of each other, at least minPairs times, with raw lift
// optionally floored at minLift.
//
// Per repo it runs one `git log --name-only --pretty=format:%x00%ct`, parses
// (timestamp, files), and buckets each (repo, file) touch into a window of
// windowHours. Two touches from different repos in the same bucket form a
// cross-repo co-occurrence. Pairs are counted at bucket level (a window where
// both appear counts as +1 regardless of how many touches each file has in that
// window), which is what the G² statistic assumes.
//
// Ranking is by Wilson lower bound on directional confidence (support-aware,
// continuous, never saturates):
//
//	score = wilsonLowerBound(co, min(winA,winB), z)
//
// wilsonLowerBound(pos, n, z) continuously penalizes thin support: a perfect
// rare coincidence (co=2, winA=winB=2, score≈0.34) ranks well below a
// well-supported loose coupling (co=8, winA=winB=10, score≈0.49). The score
// grows monotonically with more evidence and is never distorted by file rarity.
//
// Ubiquitous files (touched in >85% of windows — CHANGELOGs, lockfiles,
// generated files) are dropped as stop-words by a binary pre-filter before
// scoring. The 85% threshold is intentionally high: genuine couplings often
// involve active files (touched in 60-70% of windows), so a lower threshold
// would wrongly suppress real signal.
//
// G²/significance are informational (un-capped): the Dunning log-likelihood
// and its chi-square label are available for diagnostic use but do not drive
// ranking. Confidence = co / min(winA, winB); confidenceLevel is derived
// from the Wilson score via score.ConfidenceFromScore.
//
// minLift is an optional raw-effect-size pre-filter (co·N / (winA·winB));
// default 0 = no filter. Raw lift is NOT emitted in the result (it is an
// internal filter only) because it is an uninformative foot-gun for consumers:
// low-support pairs can have arbitrarily high lift.
//
// Returns nil when fewer than 2 repos, a non-positive window, or no touches
// (best-effort, like CollectCoupling — a repo whose git log fails is skipped).
//
// The fixed ts/windowSec bucketing is a coarse approximation: two commits a
// second apart but across a bucket boundary are missed. Accepted for "roughly
// coordinated changes"; a sliding window would be heavier (YAGNI here).
func CrossRepoCoChange(ctx context.Context, repos []RepoRef, windowHours, minPairs int, minLift float64) []CrossPair {
	if len(repos) < 2 || windowHours <= 0 {
		return nil
	}
	if windowHours > crossCoChangeMaxWindowHours {
		windowHours = crossCoChangeMaxWindowHours
	}
	var touches []touch
	for _, r := range repos {
		touches = append(touches, collectTouchesCached(ctx, r)...)
	}
	return CrossRepoCoChangeFromTouches(ctx, touches, windowHours, minPairs, minLift)
}

// CrossRepoCoChangeFromTouches computes cross-repo pairs from a pre-collected
// slice of touches. Separated from CrossRepoCoChange so the resilience layer
// can feed pre-warmed touches without re-running git log.
// Exported for use by the deadline-race partial-result path in the tool handler.
func CrossRepoCoChangeFromTouches(ctx context.Context, touches []touch, windowHours, minPairs int, minLift float64) []CrossPair {
	if len(touches) == 0 {
		return nil
	}

	windowSec := int64(windowHours) * 3600
	// bucket -> set of distinct (repo,file) touched in that window.
	buckets := make(map[int64]map[fileKey]struct{})
	for _, t := range touches {
		b := t.ts / windowSec
		set := buckets[b]
		if set == nil {
			set = make(map[fileKey]struct{})
			buckets[b] = set
		}
		set[fileKey{repo: t.repo, file: t.file}] = struct{}{}
	}

	n := len(buckets) // total non-empty windows
	winCount := make(map[fileKey]int)
	counts := make(map[pairKey]int)
	for _, set := range buckets {
		if ctx.Err() != nil {
			break
		}
		files := make([]fileKey, 0, len(set))
		for fk := range set {
			files = append(files, fk)
			winCount[fk]++
		}
		for i := 0; i < len(files); i++ {
			for j := i + 1; j < len(files); j++ {
				if files[i].repo == files[j].repo {
					continue // same-repo coupling is CollectCoupling's job
				}
				counts[canonicalPairFK(files[i], files[j])]++
			}
		}
	}

	out := make([]CrossPair, 0)
	for k, co := range counts {
		if co < minPairs {
			continue
		}
		winA := winCount[fileKey{repo: k.repoA, file: k.fileA}]
		winB := winCount[fileKey{repo: k.repoB, file: k.fileB}]
		if winA == 0 || winB == 0 {
			continue
		}
		// Raw lift is used ONLY as an internal pre-filter; it is not stored in
		// CrossPair because emitting it is a foot-gun for consumers (low-support
		// pairs can have arbitrarily high lift and are sorted below well-supported
		// ones by the Wilson score anyway).
		lift := float64(co) * float64(n) / (float64(winA) * float64(winB))
		if minLift > 0 && lift < minLift {
			continue
		}
		minWin := winA
		if winB < minWin {
			minWin = winB
		}
		// Drop pairs involving a ubiquitous (stop-word) file — CHANGELOG /
		// lockfile noise that co-occurs by sheer frequency, not real coupling.
		if isUbiquitous(winA, n) || isUbiquitous(winB, n) {
			continue
		}
		g2 := logLikelihoodG2(co, winA, winB, n)
		wlb := wilsonLowerBound(co, minWin, wilsonZ)
		confidence := float64(co) / float64(minWin)
		out = append(out, CrossPair{
			RepoA: k.repoA, FileA: k.fileA,
			RepoB: k.repoB, FileB: k.fileB,
			CoChanges:       co,
			Score:           wlb,
			G2:              g2,
			Significance:    significanceLabel(g2),
			Confidence:      confidence,
			ConfidenceLevel: string(score.ConfidenceFromScore(wlb)),
		})
	}
	sortCrossPairs(out)
	if len(out) > maxCrossPairs {
		out = out[:maxCrossPairs]
	}
	return out
}

// canonicalPairFK orders two distinct-repo file keys canonically so
// (chat/x, edge/y) and (edge/y, chat/x) collapse to one key.
func canonicalPairFK(a, b fileKey) pairKey {
	if a.repo > b.repo || (a.repo == b.repo && a.file > b.file) {
		a, b = b, a
	}
	return pairKey{repoA: a.repo, fileA: a.file, repoB: b.repo, fileB: b.file}
}

// sortCrossPairs sorts by Score (Wilson lower bound) descending, then CoChanges
// descending, then lexicographically for determinism.
// Exact-float Score compare is safe: identical integer inputs produce bit-identical Score.
func sortCrossPairs(out []CrossPair) {
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
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
}

// collectTouchesCached returns touches for r, hitting globalTouchesCache first.
// On a cache miss it calls collectTouches and populates the cache.
func collectTouchesCached(ctx context.Context, r RepoRef) []touch {
	key := touchesCacheKey(r.Root)
	if cached, ok := globalTouchesCache.get(key); ok {
		return cached
	}
	result := collectTouches(ctx, r)
	globalTouchesCache.set(key, result)
	return result
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
	curValid := true
	flush := func() {
		if !curValid {
			curFiles = nil
			return
		}
		// Mirror compare.CollectCoupling: filter artifacts FIRST, then apply the
		// bulk-commit cap to the source-file count.  A 21-file commit with 5
		// artifacts (16 source ≤ 20) is kept; without this order it would be
		// wrongly discarded.
		var srcFiles []string
		for _, f := range curFiles {
			if !artifactfilter.IsCompiledArtifact(f) {
				srcFiles = append(srcFiles, f)
			}
		}
		curFiles = nil
		if len(srcFiles) > maxFilesPerCommitFederate {
			return
		}
		for _, f := range srcFiles {
			touches = append(touches, touch{repo: r.Slug, file: f, ts: curTS})
		}
	}
	scanner := bufio.NewScanner(bytes.NewReader(stdout.Bytes()))
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024) //nolint:mnd
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "\x00") {
			flush()
			tsStr := strings.TrimPrefix(line, "\x00")
			ts, err := strconv.ParseInt(strings.TrimSpace(tsStr), 10, 64)
			if err != nil {
				curValid = false
				curTS = 0
			} else {
				curValid = true
				curTS = ts
			}
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

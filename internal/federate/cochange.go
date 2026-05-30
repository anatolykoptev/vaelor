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

// minConfidentSupport is the minimum co-occurrence count for a pair to earn a
// "high" confidence label. Below it, two-or-fewer samples can't support a
// high-confidence claim even at confidence=1.0; the label is capped to medium.
const minConfidentSupport = 3

// CrossPair is two files in DIFFERENT repos that change together within a window.
type CrossPair struct {
	RepoA           string  `json:"repoA"`
	FileA           string  `json:"fileA"`
	RepoB           string  `json:"repoB"`
	FileB           string  `json:"fileB"`
	CoChanges       int     `json:"coChanges"`
	G2              float64 `json:"g2"`
	Significance    string  `json:"significance"`
	Confidence      float64 `json:"confidence"`
	ConfidenceLevel string  `json:"confidenceLevel"`
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
// Ranking is two-tier then by G²:
//  1. Well-supported pairs (co >= minConfidentSupport) always outrank
//     low-support pairs. This prevents a perfect rare coincidence (e.g. co=2,
//     winA=winB=2) from outranking a loose but genuine coupling (e.g. co=8,
//     winA=winB=10) — the coincidence can score a higher raw G² (rewards
//     perfection of association) even though it has far less evidence.
//  2. Within each tier, rank by G² (Dunning log-likelihood) descending.
//     G² grows with sample size, so genuine high-frequency couplings score
//     high while rare coincidences score near-zero within the same tier.
//
// On small histories G² can tie; order then falls to co-change count.
//
// minLift is an optional raw-effect-size pre-filter (co·N / (winA·winB));
// default 0 = no filter. Raw lift is NOT emitted in the result (it is an
// internal filter only) because it is an uninformative foot-gun for consumers:
// low-support pairs can have arbitrarily high lift.
//
// Significance is the chi-square label derived from G² (chi-square df=1 critical
// values: 3.84→weak, 6.63→moderate, 10.83→strong, above→very_strong). The label
// is support-capped: below minConfidentSupport co-occurrences, no pair earns
// "strong"/"very_strong" (G²/chi-square is unreliable at tiny expected counts).
//
// Confidence = co / min(winA, winB). Confidence label is capped to "medium"
// when co < minConfidentSupport, regardless of the ratio.
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
	// minLift <= 0 means no floor: return all pairs above minPairs.
	// Callers raise minLift explicitly to pre-filter by raw effect-size.
	var touches []touch
	for _, r := range repos {
		touches = append(touches, collectTouches(ctx, r)...)
	}
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
		// ones by the two-tier sort anyway).
		lift := float64(co) * float64(n) / (float64(winA) * float64(winB))
		if minLift > 0 && lift < minLift {
			continue
		}
		g2 := logLikelihoodG2(co, winA, winB, n)
		// Significance support-cap: G²/chi-square is unreliable at tiny expected
		// counts — a 2-window perfect-coincidence scores G²≈22 ("very_strong")
		// despite only 2 samples. Mirror the confidence cap: below
		// minConfidentSupport, no pair earns a "strong"/"very_strong" label.
		sig := significanceLabel(g2)
		if co < minConfidentSupport && (sig == "strong" || sig == "very_strong") {
			sig = "moderate"
		}
		minWin := winA
		if winB < minWin {
			minWin = winB
		}
		confidence := float64(co) / float64(minWin)
		level := score.ConfidenceFromScore(confidence)
		// A pair with fewer than minConfidentSupport co-occurrences cannot support
		// a "high" confidence claim even at confidence=1.0 (e.g. co=2, min=2).
		if co < minConfidentSupport && level == score.ConfidenceHigh {
			level = score.ConfidenceMedium
		}
		out = append(out, CrossPair{
			RepoA: k.repoA, FileA: k.fileA,
			RepoB: k.repoB, FileB: k.fileB,
			CoChanges:       co,
			G2:              g2,
			Significance:    sig,
			Confidence:      confidence,
			ConfidenceLevel: string(level),
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

// sortCrossPairs sorts by support tier then G², then co-change count, then
// lexicographically for determinism.
//
// Primary key: well-supported pairs (co >= minConfidentSupport) always rank
// above low-support pairs. G²/chi-square rewards perfection of association
// regardless of sample size, so a 2-window perfect coincidence (co=2,
// winA=winB=2) can score a higher raw G² than a loose genuine coupling (co=8,
// winA=winB=10). The support tier prevents a thin coincidence from outranking
// a well-supported pair. Within each tier, rank by G² descending.
//
// Exact-float G² compare is safe: identical integer inputs produce bit-identical G².
func sortCrossPairs(out []CrossPair) {
	sort.SliceStable(out, func(i, j int) bool {
		// Tier 1: well-supported pairs (co >= minConfidentSupport) always rank
		// above low-support pairs. G²/chi-square over-rewards perfect rare
		// coincidences (a 2-window always-together pair can score higher G²
		// than a loose-but-frequent genuine coupling); the support tier prevents
		// a thin coincidence from outranking a well-supported pair.
		si := out[i].CoChanges >= minConfidentSupport
		sj := out[j].CoChanges >= minConfidentSupport
		if si != sj {
			return si // supported (true) sorts before unsupported (false)
		}
		if out[i].G2 != out[j].G2 {
			return out[i].G2 > out[j].G2
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

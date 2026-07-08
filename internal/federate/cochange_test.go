package federate

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// commitAt makes a commit in dir touching `file` with a fixed author/committer
// date so tests are deterministic across time windows.
func commitAt(t *testing.T, dir, file, date, msg string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, file), []byte(date+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run := func(env []string, args ...string) {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(), env...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run(nil, "add", file)
	env := []string{
		"GIT_AUTHOR_DATE=" + date,
		"GIT_COMMITTER_DATE=" + date,
	}
	run(env, "commit", "-m", msg)
}

func configIdent(t *testing.T, dir string) {
	t.Helper()
	exec.Command("git", "-C", dir, "config", "user.email", "t@t.t").Run()
	exec.Command("git", "-C", dir, "config", "user.name", "t").Run()
}

func TestCrossRepoCoChange_PairsAcrossRepos(t *testing.T) {
	if testing.Short() {
		t.Skip("heavy integration test; runs in the nightly full suite (make test)")
	}
	parent := t.TempDir()
	chat := filepath.Join(parent, "oxpulse-chat")
	edge := filepath.Join(parent, "oxpulse-partner-edge")
	gitInit(t, chat)
	gitInit(t, edge)
	configIdent(t, chat)
	configIdent(t, edge)

	// Three coordinated change-windows: chat/rooms.rs + edge/install.sh
	// committed within the same hour each time.
	coChangeDates := []string{
		"2026-05-01T10:00:00",
		"2026-05-08T10:00:00",
		"2026-05-15T10:00:00",
	}
	for _, date := range coChangeDates {
		commitAt(t, chat, "rooms.rs", date, "signaling change")
		commitAt(t, edge, "install.sh", date, "sync installer")
	}
	// Background solo commits so total windows n=5, making rooms.rs and install.sh
	// appear in 3/5=60% of windows (below the 85% ubiquity threshold → not filtered).
	commitAt(t, chat, "bg.go", "2026-05-22T10:00:00", "background")
	commitAt(t, chat, "bg.go", "2026-05-29T10:00:00", "background")

	repos := []RepoRef{
		{Slug: "oxpulse-chat", Root: chat},
		{Slug: "oxpulse-partner-edge", Root: edge},
	}
	// minLift=0 → no floor; pairs returned if co ≥ minPairs.
	pairs := CrossRepoCoChange(context.Background(), repos, 24, 2, 0) // 24h window, min 2
	if len(pairs) == 0 {
		t.Fatal("expected at least one cross-repo pair")
	}
	top := pairs[0]
	if top.RepoA == top.RepoB {
		t.Fatalf("pair must span two repos, got both %s", top.RepoA)
	}
	if top.CoChanges < 2 {
		t.Fatalf("expected ≥2 co-changes, got %d", top.CoChanges)
	}
}

func TestCrossRepoCoChange_NoCrossRepoSignalWhenDisjoint(t *testing.T) {
	if testing.Short() {
		t.Skip("heavy integration test; runs in the nightly full suite (make test)")
	}
	parent := t.TempDir()
	chat := filepath.Join(parent, "oxpulse-chat")
	edge := filepath.Join(parent, "oxpulse-partner-edge")
	gitInit(t, chat)
	gitInit(t, edge)
	configIdent(t, chat)
	configIdent(t, edge)
	// Commits far apart in time → no shared window.
	commitAt(t, chat, "a.rs", "2026-01-01T10:00:00", "x")
	commitAt(t, edge, "b.sh", "2026-05-01T10:00:00", "y")

	pairs := CrossRepoCoChange(context.Background(), []RepoRef{
		{Slug: "oxpulse-chat", Root: chat},
		{Slug: "oxpulse-partner-edge", Root: edge},
	}, 24, 2, 0)
	if len(pairs) != 0 {
		t.Fatalf("disjoint timelines → no pairs, got %v", pairs)
	}
}

func TestCrossRepoCoChange_WindowWidthDiscriminates(t *testing.T) {
	if testing.Short() {
		t.Skip("heavy integration test; runs in the nightly full suite (make test)")
	}
	parent := t.TempDir()
	chat := filepath.Join(parent, "oxpulse-chat")
	edge := filepath.Join(parent, "oxpulse-partner-edge")
	gitInit(t, chat)
	gitInit(t, edge)
	configIdent(t, chat)
	configIdent(t, edge)

	// Two commits 3h apart on the same UTC day (mid-day, no midnight straddle):
	// same 24h bucket, different 1h buckets. Explicit +00:00 so the unix %ct is
	// deterministic regardless of the test host's timezone.
	commitAt(t, chat, "rooms.rs", "2026-05-01T10:00:00+00:00", "signaling")
	commitAt(t, edge, "install.sh", "2026-05-01T13:00:00+00:00", "sync")
	// Background solo commits so rooms.rs and install.sh appear in 1 of ≥2 total windows,
	// keeping their window fraction below the 85% ubiquity threshold.
	// Under a 24h window: 3 buckets total; rooms.rs/install.sh each in 1 bucket (33%).
	// Under a 1h window:  rooms.rs in 1 bucket, install.sh in a different bucket → 0 co-pairs.
	commitAt(t, chat, "bg.go", "2026-05-08T10:00:00+00:00", "background")
	commitAt(t, edge, "bg.sh", "2026-05-15T10:00:00+00:00", "background")

	repos := []RepoRef{
		{Slug: "oxpulse-chat", Root: chat},
		{Slug: "oxpulse-partner-edge", Root: edge},
	}
	// Under a 24h window they co-occur (same bucket).
	// minLift=0 → no floor; minPairs=1 → single co-occurrence returned.
	if got := CrossRepoCoChange(context.Background(), repos, 24, 1, 0); len(got) == 0 {
		t.Fatal("3h-apart commits must pair under a 24h window")
	}
	// Under a 1h window they do NOT (different buckets).
	if got := CrossRepoCoChange(context.Background(), repos, 1, 1, 0); len(got) != 0 {
		t.Fatalf("3h-apart commits must NOT pair under a 1h window, got %v", got)
	}
}

// weeks15 is a fixed set of 15 strictly-increasing weekly timestamps used by
// the realistic ranking regression test.
var weeks15 = []string{
	"2026-01-05T10:00:00+00:00",
	"2026-01-12T10:00:00+00:00",
	"2026-01-19T10:00:00+00:00",
	"2026-01-26T10:00:00+00:00",
	"2026-02-02T10:00:00+00:00",
	"2026-02-09T10:00:00+00:00",
	"2026-02-16T10:00:00+00:00",
	"2026-02-23T10:00:00+00:00",
	"2026-03-02T10:00:00+00:00",
	"2026-03-09T10:00:00+00:00",
	"2026-03-16T10:00:00+00:00",
	"2026-03-23T10:00:00+00:00",
	"2026-03-30T10:00:00+00:00",
	"2026-04-06T10:00:00+00:00",
	"2026-04-13T10:00:00+00:00",
}

// TestCrossRepoCoChange_RanksGenuineAboveNoiseAndCoincidence verifies that
// Wilson-LB-alone ranking correctly handles the high-frequency noise class:
// a ubiquitous pair (noisy.ts ↔ util.go, co-occurring in ALL 15 windows) is
// filtered out entirely by the isUbiquitous pre-filter (winA=winB=n=15 → 100%
// of windows, exceeds the 85% ubiquity threshold).
//
// Under pure Wilson-LB (no IDF multiplier):
//   - noisy.ts ↔ util.go:   ABSENT (filtered — ubiquitous stop-words)
//   - api.rs ↔ handler.go:  score≈0.720 (genuine, co=10, winA=winB=10, 67% of 15) → "high"
//   - blip.md ↔ once.txt:   score≈0.342 (rare coincidence, co=2, winA=winB=2, 13%) → "medium"
//
// Critical invariant: genuine (0.72) ranks ABOVE the coincidence (0.34) because
// Wilson-LB penalizes thin support continuously — the regression from Task-2's
// IDF multiplier (which inverted this order) is fixed.
//
// The distinct-levels assertion (high ≠ medium) confirms confidenceLevel discriminates.
func TestCrossRepoCoChange_RanksGenuineAboveNoiseAndCoincidence(t *testing.T) {
	if testing.Short() {
		t.Skip("heavy integration test; runs in the nightly full suite (make test)")
	}
	parent := t.TempDir()
	chat := filepath.Join(parent, "oxpulse-chat")
	edge := filepath.Join(parent, "oxpulse-partner-edge")
	gitInit(t, chat)
	gitInit(t, edge)
	configIdent(t, chat)
	configIdent(t, edge)

	// GENUINE coupling: api.rs ↔ handler.go change together in 10 of the 15 windows
	// (67% each, well below the 85% ubiquity threshold; wlb(10,10,z)≈0.720 → "high").
	for _, d := range weeks15[:10] {
		commitAt(t, chat, "api.rs", d, "genuine")
		commitAt(t, edge, "handler.go", d, "genuine")
	}
	// HIGH-FREQUENCY saturating noise: noisy.ts + util.go change in ALL 15 windows
	// (winA=winB=n=15 → 100% of windows → isUbiquitous → filtered out entirely).
	for _, d := range weeks15 {
		commitAt(t, chat, "noisy.ts", d, "noise")
		commitAt(t, edge, "util.go", d, "noise")
	}
	// RARE COINCIDENCE: blip.md ↔ once.txt co-occur in exactly 2 windows (13% of 15),
	// each appearing ONLY in those 2 windows (wlb(2,2,z)≈0.342 → "medium").
	for _, d := range weeks15[10:12] {
		commitAt(t, chat, "blip.md", d, "coincidence")
		commitAt(t, edge, "once.txt", d, "coincidence")
	}

	repos := []RepoRef{
		{Slug: "oxpulse-chat", Root: chat},
		{Slug: "oxpulse-partner-edge", Root: edge},
	}
	pairs := CrossRepoCoChange(context.Background(), repos, 24, 2, 0)
	if len(pairs) == 0 {
		t.Fatal("expected pairs")
	}

	// Ubiquitous stop-words (noisy.ts, util.go) must be filtered out entirely.
	for _, p := range pairs {
		if p.FileA == "noisy.ts" || p.FileB == "noisy.ts" ||
			p.FileA == "util.go" || p.FileB == "util.go" {
			t.Fatalf("ubiquitous files noisy.ts/util.go must be filtered out, found: %s↔%s", p.FileA, p.FileB)
		}
	}

	// Locate genuine and coincidence pairs.
	var genuineScore, coincidenceScore float64
	genuineIdx, coincidenceIdx := -1, -1
	for idx, p := range pairs {
		if (p.FileA == "api.rs" && p.FileB == "handler.go") || (p.FileA == "handler.go" && p.FileB == "api.rs") {
			genuineScore = p.Score
			genuineIdx = idx
		}
		if p.FileA == "blip.md" || p.FileB == "blip.md" {
			coincidenceScore = p.Score
			coincidenceIdx = idx
		}
	}
	if genuineIdx < 0 {
		t.Fatal("genuine api.rs↔handler.go pair not found")
	}
	if coincidenceIdx < 0 {
		t.Fatal("coincidence blip.md↔once.txt pair not found")
	}

	// Wilson-LB alone must rank genuine (co=10,n=10, wlb≈0.720) ABOVE coincidence (co=2,n=2, wlb≈0.342).
	// This is the inversion the IDF multiplier caused (it boosted rare files above well-supported ones).
	if genuineScore <= coincidenceScore {
		t.Fatalf("genuine (score=%.4f, rank=%d) must rank ABOVE coincidence (score=%.4f, rank=%d) under Wilson-LB",
			genuineScore, genuineIdx, coincidenceScore, coincidenceIdx)
	}
	if genuineIdx >= coincidenceIdx {
		t.Fatalf("genuine rank=%d must be lower index (higher rank) than coincidence rank=%d",
			genuineIdx, coincidenceIdx)
	}
	// confidenceLevel must discriminate: genuine wlb≈0.720 → "high", coincidence wlb≈0.342 → "medium".
	levels := map[string]bool{}
	for _, p := range pairs {
		levels[p.ConfidenceLevel] = true
	}
	if len(levels) == 1 {
		t.Fatalf("confidenceLevel must discriminate across pairs, got single level: %v", levels)
	}
	t.Logf("genuine score=%.4f rank=%d; coincidence score=%.4f rank=%d (noisy/util filtered)",
		genuineScore, genuineIdx, coincidenceScore, coincidenceIdx)
}

// TestCrossRepoCoChange_G2RanksBySignificance verifies that G² is populated as an
// informational field and correctly reflects statistical strength. Wilson-LB drives
// ranking; G² no longer drives ranking — it is diagnostic metadata.
//
// Wilson-LB score behavior (n=15, background windows inflate N):
//   - api.rs ↔ handler.go: co=8, winA=8, winB=8 → wlb≈0.490 (well-supported, ranks first)
//   - blip.md ↔ once.txt:  co=2, winA=2, winB=2 → wlb≈0.342 (thin support, ranks second)
//
// The test verifies:
//  1. G² is populated (>0) for all pairs — available for diagnostics.
//  2. Significance label is populated (non-empty) for all pairs.
//  3. Genuine G² > coincidence G² (G² grows with sample size — still true at n=15).
//  4. The Score field (Wilson-LB) drives ranking (top pair = genuine with highest Score).
//  5. G²/Significance are not capped — the coincidence CAN earn "strong"/"very_strong"
//     since significance caps were removed (informational, not a ranking signal).
func TestCrossRepoCoChange_G2RanksBySignificance(t *testing.T) {
	if testing.Short() {
		t.Skip("heavy integration test; runs in the nightly full suite (make test)")
	}
	parent := t.TempDir()
	chat := filepath.Join(parent, "oxpulse-chat")
	edge := filepath.Join(parent, "oxpulse-partner-edge")
	gitInit(t, chat)
	gitInit(t, edge)
	configIdent(t, chat)
	configIdent(t, edge)

	weeks := []string{
		"2026-01-05T10:00:00+00:00", "2026-01-12T10:00:00+00:00", "2026-01-19T10:00:00+00:00",
		"2026-01-26T10:00:00+00:00", "2026-02-02T10:00:00+00:00", "2026-02-09T10:00:00+00:00",
		"2026-02-16T10:00:00+00:00", "2026-02-23T10:00:00+00:00", "2026-03-02T10:00:00+00:00",
		"2026-03-09T10:00:00+00:00", "2026-03-16T10:00:00+00:00", "2026-03-23T10:00:00+00:00",
		"2026-03-30T10:00:00+00:00", "2026-04-06T10:00:00+00:00", "2026-04-13T10:00:00+00:00",
	}
	// Genuine, well-supported coupling: api.rs ↔ handler.go in 8 windows.
	for _, d := range weeks[:8] {
		commitAt(t, chat, "api.rs", d, "genuine")
		commitAt(t, edge, "handler.go", d, "genuine")
	}
	// Rare coincidence: blip.md ↔ once.txt in only 2 windows (each file ONLY there).
	for _, d := range weeks[10:12] {
		commitAt(t, chat, "blip.md", d, "coincidence")
		commitAt(t, edge, "once.txt", d, "coincidence")
	}
	// Background solo commits to inflate the total window count n=15.
	// With n=15, genuine G²≈20.7 >> coincidence G²≈11.8 (G² grows with sample size).
	for _, d := range weeks[8:10] {
		commitAt(t, chat, "bg.md", d, "bg")
	}
	for _, d := range weeks[12:] {
		commitAt(t, edge, "bg.sh", d, "bg")
	}

	repos := []RepoRef{
		{Slug: "oxpulse-chat", Root: chat},
		{Slug: "oxpulse-partner-edge", Root: edge},
	}
	pairs := CrossRepoCoChange(context.Background(), repos, 24, 2, 0)
	if len(pairs) == 0 {
		t.Fatal("expected pairs")
	}

	// Locate genuine and coincidence pairs.
	var genuineG2, coincidenceG2 float64
	var genuineScore float64
	genuineIdx := -1
	for idx, p := range pairs {
		if (p.FileA == "api.rs" && p.FileB == "handler.go") || (p.FileA == "handler.go" && p.FileB == "api.rs") {
			genuineG2 = p.G2
			genuineScore = p.Score
			genuineIdx = idx
		}
		if p.FileA == "blip.md" || p.FileB == "blip.md" {
			coincidenceG2 = p.G2
		}
	}
	if genuineIdx < 0 {
		t.Fatal("genuine api.rs↔handler.go pair not found in results")
	}

	// G² must be populated (>0) and informational for the genuine pair.
	if genuineG2 <= 0 {
		t.Fatalf("genuine pair must have G²>0, got %.4f", genuineG2)
	}
	// Significance label must be populated.
	for _, p := range pairs {
		if p.Significance == "" {
			t.Fatalf("significance label must be non-empty for all pairs: %+v", p)
		}
	}
	// G²: genuine > coincidence (G² grows with sample size — informational truth still holds).
	if coincidenceG2 > 0 && genuineG2 <= coincidenceG2 {
		t.Fatalf("genuine G²(%.2f) must exceed coincidence G²(%.2f) — G² informational check failed", genuineG2, coincidenceG2)
	}
	// Score field must be positive for genuine pair (Wilson-LB rank signal).
	if genuineScore <= 0 {
		t.Fatalf("genuine pair must have Score>0, got %.4f", genuineScore)
	}
	// Wilson-LB drives ranking: top pair (genuine, wlb≈0.490) must have the highest Score.
	for _, p := range pairs {
		if p.Score > pairs[0].Score {
			t.Fatalf("top pair must have max Score; pair %s↔%s has score %.4f > top %.4f",
				p.FileA, p.FileB, p.Score, pairs[0].Score)
		}
	}
	t.Logf("genuine G²=%.4f score=%.4f; coincidence G²=%.4f", genuineG2, genuineScore, coincidenceG2)
}

// TestCrossRepoCoChange_IDFDemotesUbiquitousFile verifies that the ubiquity
// pre-filter removes CHANGELOG.md pairs entirely (CHANGELOG.md in 100% of
// windows → isUbiquitous → filtered before scoring), and that the genuine
// rare pair (rooms.rs ↔ handler.go, 5 of 12 windows, ≈42% each) tops the results.
//
// Under the IDF multiplier (Task-2 regression), CHANGELOG pairs were present
// but low-scored. Under the Wilson-LB-alone design, they are absent entirely.
//
// It also verifies that confidenceLevel discriminates across the result set.
func TestCrossRepoCoChange_IDFDemotesUbiquitousFile(t *testing.T) {
	if testing.Short() {
		t.Skip("heavy integration test; runs in the nightly full suite (make test)")
	}
	parent := t.TempDir()
	chat := filepath.Join(parent, "oxpulse-chat")
	edge := filepath.Join(parent, "oxpulse-partner-edge")
	gitInit(t, chat)
	gitInit(t, edge)
	configIdent(t, chat)
	configIdent(t, edge)

	// 20 weeks to give CHANGELOG a larger n (CHANGELOG in 20/20=100% → filtered).
	weeks20 := make([]string, 20)
	base := time.Date(2026, 1, 5, 10, 0, 0, 0, time.UTC)
	for i := range weeks20 {
		weeks20[i] = base.AddDate(0, 0, i*7).Format("2006-01-02T15:04:05+00:00")
	}
	// CHANGELOG.md (edge) changes in EVERY window (20/20 = 100% → isUbiquitous → filtered).
	// api.rs (chat) co-occurs with it in 8 windows — those pairs must be absent from results.
	for _, d := range weeks20 {
		commitAt(t, edge, "CHANGELOG.md", d, "release")
	}
	for _, d := range weeks20[:8] {
		commitAt(t, chat, "api.rs", d, "feature")
	}
	// GENUINE strong: rooms.rs (chat) ↔ handler.go (edge) co-occur in 10 windows (50% of 20 —
	// active but not ubiquitous; wlb(10,10,z)≈0.720 → "high", ranks top of filtered results).
	for _, d := range weeks20[8:18] {
		commitAt(t, chat, "rooms.rs", d, "signaling")
		commitAt(t, edge, "handler.go", d, "signaling")
	}
	// GENUINE thin: config.rs (chat) ↔ deploy.sh (edge) co-occur in only 2 windows (10% of 20 —
	// not ubiquitous; wlb(2,2,z)≈0.342 → "medium", ranks below rooms.rs↔handler.go).
	// Provides a second pair so confidenceLevel discrimination is meaningful.
	for _, d := range weeks20[18:20] {
		commitAt(t, chat, "config.rs", d, "config")
		commitAt(t, edge, "deploy.sh", d, "deploy")
	}

	repos := []RepoRef{
		{Slug: "oxpulse-chat", Root: chat},
		{Slug: "oxpulse-partner-edge", Root: edge},
	}
	pairs := CrossRepoCoChange(context.Background(), repos, 24, 2, 0)
	if len(pairs) == 0 {
		t.Fatal("expected pairs")
	}

	// CHANGELOG.md is ubiquitous (100% of windows) — must be absent entirely.
	for _, p := range pairs {
		if p.FileA == "CHANGELOG.md" || p.FileB == "CHANGELOG.md" {
			t.Fatalf("ubiquitous CHANGELOG.md must be filtered out entirely, found: %s↔%s", p.FileA, p.FileB)
		}
	}

	// Genuine rare pair must top the results.
	top := pairs[0]
	isGenuine := (top.FileA == "rooms.rs" && top.FileB == "handler.go") ||
		(top.FileA == "handler.go" && top.FileB == "rooms.rs")
	if !isGenuine {
		t.Fatalf("genuine rare pair must top; got %s↔%s score=%.4f", top.FileA, top.FileB, top.Score)
	}

	// confidenceLevel must discriminate — NOT a single level for all pairs.
	levels := map[string]bool{}
	for _, p := range pairs {
		levels[p.ConfidenceLevel] = true
	}
	if len(levels) == 1 {
		t.Fatalf("confidenceLevel must discriminate, got single level for all: %v", levels)
	}
	t.Logf("genuine top score=%.4f; %d pairs (CHANGELOG filtered)", top.Score, len(pairs))
}

// TestCrossRepoCoChange_SupportTierBeatsRawG2 verifies that Wilson-LB-alone ranking
// correctly promotes a well-supported genuine coupling ABOVE a perfect rare
// coincidence — the inversion that the IDF multiplier (Task-2) introduced.
//
// Fixture (n=15 windows):
//   - Genuine LOOSE coupling: api.rs ↔ handler.go together in 8 windows;
//     each also changes alone in 2 more (winA=winB=10, co=8).
//     wlb(8,10,z) ≈ 0.490. Neither file is ubiquitous (10/15=67%).
//   - Perfect rare coincidence: blip.md ↔ once.txt in exactly 2 windows, never solo.
//     wlb(2,2,z) ≈ 0.342. Neither file is ubiquitous (2/15=13%).
//
// Wilson-LB alone: genuine 0.490 > coincidence 0.342 → correct order.
// IDF multiplier (Task-2) inverted this: genuine 0.199 < coincidence 0.690 → BUG.
//
// G²-only would also rank coincidence above genuine (G²≈11.78 > G²≈2.36), for the
// wrong reason (G² rewards perfection without penalizing thin support). This test
// documents that Wilson-LB diverges correctly from both G²-only and the IDF composite.
func TestCrossRepoCoChange_SupportTierBeatsRawG2(t *testing.T) {
	if testing.Short() {
		t.Skip("heavy integration test; runs in the nightly full suite (make test)")
	}
	parent := t.TempDir()
	chat := filepath.Join(parent, "oxpulse-chat")
	edge := filepath.Join(parent, "oxpulse-partner-edge")
	gitInit(t, chat)
	gitInit(t, edge)
	configIdent(t, chat)
	configIdent(t, edge)

	weeks := []string{
		"2026-01-05T10:00:00+00:00", "2026-01-12T10:00:00+00:00", "2026-01-19T10:00:00+00:00",
		"2026-01-26T10:00:00+00:00", "2026-02-02T10:00:00+00:00", "2026-02-09T10:00:00+00:00",
		"2026-02-16T10:00:00+00:00", "2026-02-23T10:00:00+00:00", "2026-03-02T10:00:00+00:00",
		"2026-03-09T10:00:00+00:00", "2026-03-16T10:00:00+00:00", "2026-03-23T10:00:00+00:00",
		"2026-03-30T10:00:00+00:00", "2026-04-06T10:00:00+00:00", "2026-04-13T10:00:00+00:00",
	}
	// Genuine LOOSE coupling: api.rs ↔ handler.go together in 8 windows, but
	// each also changes alone in 2 more windows (winA=winB=10, co=8).
	for _, d := range weeks[:8] {
		commitAt(t, chat, "api.rs", d, "genuine-together")
		commitAt(t, edge, "handler.go", d, "genuine-together")
	}
	for _, d := range weeks[8:10] {
		commitAt(t, chat, "api.rs", d, "api-solo") // api.rs alone (winA=10, no co bump)
	}
	for _, d := range weeks[10:12] {
		commitAt(t, edge, "handler.go", d, "handler-solo") // handler.go alone (winB=10, no co bump)
	}
	// Perfect rare coincidence: blip.md ↔ once.txt in exactly 2 windows, never solo.
	// G² (n=15, co=2, winA=winB=2) ≈ 11.78 — ~5× higher than the genuine pair.
	for _, d := range weeks[12:14] {
		commitAt(t, chat, "blip.md", d, "coincidence")
		commitAt(t, edge, "once.txt", d, "coincidence")
	}
	// One background commit to make n=15.
	commitAt(t, chat, "bg.md", weeks[14], "bg")

	repos := []RepoRef{
		{Slug: "oxpulse-chat", Root: chat},
		{Slug: "oxpulse-partner-edge", Root: edge},
	}
	pairs := CrossRepoCoChange(context.Background(), repos, 24, 2, 0)
	if len(pairs) == 0 {
		t.Fatal("expected pairs")
	}

	var genuineG2, coincidenceG2 float64
	var genuineScore, coincidenceScore float64
	genuineIdx, coincidenceIdx := -1, -1
	for idx, p := range pairs {
		if (p.FileA == "api.rs" && p.FileB == "handler.go") || (p.FileA == "handler.go" && p.FileB == "api.rs") {
			genuineG2 = p.G2
			genuineScore = p.Score
			genuineIdx = idx
		}
		if p.FileA == "blip.md" || p.FileB == "blip.md" {
			coincidenceG2 = p.G2
			coincidenceScore = p.Score
			coincidenceIdx = idx
		}
	}
	if genuineIdx < 0 {
		t.Fatal("genuine api.rs↔handler.go pair not found in results")
	}
	if coincidenceIdx < 0 {
		t.Fatal("coincidence blip.md↔once.txt pair not found in results")
	}

	t.Logf("genuine G²=%.4f score=%.4f rank=%d (co=8, winA=winB=10, loose)", genuineG2, genuineScore, genuineIdx)
	t.Logf("coincidence G²=%.4f score=%.4f rank=%d (co=2, winA=winB=2, perfect)", coincidenceG2, coincidenceScore, coincidenceIdx)

	// CORE INVARIANT: genuine (co=8,n=10) must rank ABOVE coincidence (co=2,n=2).
	// wlb(8,10)≈0.490 > wlb(2,2)≈0.342. This was inverted by the IDF multiplier (Task-2 regression).
	if genuineScore <= coincidenceScore {
		t.Fatalf("genuine score=%.4f must be > coincidence score=%.4f (Wilson-LB penalizes thin support)",
			genuineScore, coincidenceScore)
	}
	if genuineIdx >= coincidenceIdx {
		t.Fatalf("genuine rank=%d must be lower index (higher rank) than coincidence rank=%d",
			genuineIdx, coincidenceIdx)
	}

	// G² fields must be populated (informational, available for diagnostics).
	if genuineG2 <= 0 {
		t.Fatalf("genuine pair must have G²>0, got %.4f", genuineG2)
	}
	if coincidenceG2 <= 0 {
		t.Fatalf("coincidence pair must have G²>0, got %.4f", coincidenceG2)
	}
	// G²-only would wrongly rank coincidence above genuine (G²≈11.78 > G²≈2.36).
	// Wilson diverges correctly from G²-only.
	if coincidenceG2 <= genuineG2 {
		t.Logf("NOTE: defect condition not met (coincidenceG²=%.4f ≤ genuineG²=%.4f); test setup may need tuning", coincidenceG2, genuineG2)
	}
	// Significance fields must be populated for all pairs (un-capped, informational).
	for _, p := range pairs {
		if p.Significance == "" {
			t.Fatalf("significance label must be non-empty: %+v", p)
		}
	}
	// Score field must be positive for all pairs meeting minPairs.
	if genuineScore <= 0 {
		t.Fatalf("genuine pair must have Score>0, got %.4f", genuineScore)
	}
	// Score drives ranking: sort order must match score order.
	for i := 1; i < len(pairs); i++ {
		if pairs[i].Score > pairs[i-1].Score {
			t.Fatalf("pairs not sorted by Score: [%d] score=%.4f > [%d] score=%.4f",
				i, pairs[i].Score, i-1, pairs[i-1].Score)
		}
	}
	// Coincidence ConfidenceLevel: wlb(2,2,z)≈0.342 → must NOT be "high" (threshold ≥0.7).
	for _, p := range pairs {
		if (p.FileA == "blip.md" || p.FileB == "blip.md") && p.ConfidenceLevel == "high" {
			t.Fatalf("2-window coincidence wlb≈0.342 must not be labeled high-confidence: %+v", p)
		}
	}
}

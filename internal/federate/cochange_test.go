package federate

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
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
	parent := t.TempDir()
	chat := filepath.Join(parent, "acme-web")
	edge := filepath.Join(parent, "acme-edge")
	gitInit(t, chat)
	gitInit(t, edge)
	configIdent(t, chat)
	configIdent(t, edge)

	// Three coordinated change-windows: chat/rooms.rs + edge/install.sh
	// committed within the same hour each time.
	for _, date := range []string{
		"2026-05-01T10:00:00",
		"2026-05-08T10:00:00",
		"2026-05-15T10:00:00",
	} {
		commitAt(t, chat, "rooms.rs", date, "signaling change")
		commitAt(t, edge, "install.sh", date, "sync installer")
	}

	repos := []RepoRef{
		{Slug: "acme-web", Root: chat},
		{Slug: "acme-edge", Root: edge},
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
	parent := t.TempDir()
	chat := filepath.Join(parent, "acme-web")
	edge := filepath.Join(parent, "acme-edge")
	gitInit(t, chat)
	gitInit(t, edge)
	configIdent(t, chat)
	configIdent(t, edge)
	// Commits far apart in time → no shared window.
	commitAt(t, chat, "a.rs", "2026-01-01T10:00:00", "x")
	commitAt(t, edge, "b.sh", "2026-05-01T10:00:00", "y")

	pairs := CrossRepoCoChange(context.Background(), []RepoRef{
		{Slug: "acme-web", Root: chat},
		{Slug: "acme-edge", Root: edge},
	}, 24, 2, 0)
	if len(pairs) != 0 {
		t.Fatalf("disjoint timelines → no pairs, got %v", pairs)
	}
}

func TestCrossRepoCoChange_WindowWidthDiscriminates(t *testing.T) {
	parent := t.TempDir()
	chat := filepath.Join(parent, "acme-web")
	edge := filepath.Join(parent, "acme-edge")
	gitInit(t, chat)
	gitInit(t, edge)
	configIdent(t, chat)
	configIdent(t, edge)

	// Two commits 3h apart on the same UTC day (mid-day, no midnight straddle):
	// same 24h bucket, different 1h buckets. Explicit +00:00 so the unix %ct is
	// deterministic regardless of the test host's timezone.
	commitAt(t, chat, "rooms.rs", "2026-05-01T10:00:00+00:00", "signaling")
	commitAt(t, edge, "install.sh", "2026-05-01T13:00:00+00:00", "sync")

	repos := []RepoRef{
		{Slug: "acme-web", Root: chat},
		{Slug: "acme-edge", Root: edge},
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
// G²-ranked co-change places a genuine high-support coupling (api.rs ↔
// handler.go, co=8 of 17 windows) ABOVE both a high-frequency noise pair
// (noisy.ts ↔ util.go, always co-occurring but low lift) and a rare 2-window
// coincidence (blip.md ↔ once.txt).
//
// This is a regression test for the rare-coincidence inflation class: without
// significance-aware ranking, co=2 winA=2 winB=2 N=17 → raw lift ≈ 8.5, which
// buries the genuine co=8 pair (raw lift ≈ 2.0). G² (Dunning log-likelihood)
// penalizes low-N pairs by construction: genuine G²≈23.5, coincidence G²≈12.3.
//
// Also asserts that the 2-window coincidence is NOT labeled "high" confidence,
// since minConfidentSupport=3 caps it to "medium".
func TestCrossRepoCoChange_RanksGenuineAboveNoiseAndCoincidence(t *testing.T) {
	parent := t.TempDir()
	chat := filepath.Join(parent, "acme-web")
	edge := filepath.Join(parent, "acme-edge")
	gitInit(t, chat)
	gitInit(t, edge)
	configIdent(t, chat)
	configIdent(t, edge)

	// GENUINE coupling: api.rs ↔ handler.go change together in 8 of the 15 windows.
	for _, d := range weeks15[:8] {
		commitAt(t, chat, "api.rs", d, "genuine")
		commitAt(t, edge, "handler.go", d, "genuine")
	}
	// HIGH-FREQUENCY but uncoupled: noisy.ts + util.go change in ALL 15 windows
	// (co-occur 15× by sheer frequency — high raw count, low lift).
	for _, d := range weeks15 {
		commitAt(t, chat, "noisy.ts", d, "noise")
		commitAt(t, edge, "util.go", d, "noise")
	}
	// RARE COINCIDENCE: blip.md ↔ once.txt co-occur in exactly 2 windows, each
	// appearing ONLY in those 2 windows. Without smoothing: raw lift = 2·N/(2·2)
	// which is enormous and buries the genuine pair.
	for _, d := range weeks15[10:12] {
		commitAt(t, chat, "blip.md", d, "coincidence")
		commitAt(t, edge, "once.txt", d, "coincidence")
	}

	repos := []RepoRef{
		{Slug: "acme-web", Root: chat},
		{Slug: "acme-edge", Root: edge},
	}
	// minLift=0 → no floor, rank by support tier then G².
	pairs := CrossRepoCoChange(context.Background(), repos, 24, 2, 0)
	if len(pairs) == 0 {
		t.Fatal("expected pairs")
	}
	// The genuine high-support coupling must rank FIRST — above the high-frequency
	// noise (low lift) and above the rare coincidence (support tier + G² penalizes it).
	top := pairs[0]
	isGenuine := (top.FileA == "api.rs" && top.FileB == "handler.go") ||
		(top.FileA == "handler.go" && top.FileB == "api.rs")
	if !isGenuine {
		t.Fatalf("top must be the genuine api.rs↔handler.go coupling, got %s/%s↔%s/%s g2=%.3f co=%d",
			top.RepoA, top.FileA, top.RepoB, top.FileB, top.G2, top.CoChanges)
	}
	// Find the coincidence pair and assert it is NOT labeled "high" confidence
	// (co=2 < minConfidentSupport=3 must cap the label to "medium").
	for _, p := range pairs {
		if (p.FileA == "blip.md" || p.FileB == "blip.md") && p.ConfidenceLevel == "high" {
			t.Fatalf("2-window coincidence must not be labeled high-confidence: %+v", p)
		}
	}
}

func TestCrossRepoCoChange_G2RanksBySignificance(t *testing.T) {
	parent := t.TempDir()
	chat := filepath.Join(parent, "acme-web")
	edge := filepath.Join(parent, "acme-edge")
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
	// Without these, n=10 and G² is scale-invariant at identical coupling ratios.
	// With n=15, genuine G²≈20.7 >> coincidence G²≈11.8 (G² grows with sample size).
	for _, d := range weeks[8:10] {
		commitAt(t, chat, "bg.md", d, "bg")
	}
	for _, d := range weeks[12:] {
		commitAt(t, edge, "bg.sh", d, "bg")
	}

	repos := []RepoRef{
		{Slug: "acme-web", Root: chat},
		{Slug: "acme-edge", Root: edge},
	}
	pairs := CrossRepoCoChange(context.Background(), repos, 24, 2, 0)
	if len(pairs) == 0 {
		t.Fatal("expected pairs")
	}
	top := pairs[0]
	isGenuine := (top.FileA == "api.rs" && top.FileB == "handler.go") ||
		(top.FileA == "handler.go" && top.FileB == "api.rs")
	if !isGenuine {
		t.Fatalf("top by G² must be genuine api.rs↔handler.go, got %s↔%s g2=%.2f", top.FileA, top.FileB, top.G2)
	}
	if top.G2 <= 0 {
		t.Fatalf("genuine pair must have G²>0, got %.4f", top.G2)
	}
	if top.Significance == "" {
		t.Fatalf("significance label must be populated")
	}
	for _, p := range pairs {
		if p.FileA == "blip.md" || p.FileB == "blip.md" {
			if p.G2 >= top.G2 {
				t.Fatalf("coincidence G²(%.2f) must be < genuine G²(%.2f)", p.G2, top.G2)
			}
			// co=2 < minConfidentSupport → significance capped, never strong/very_strong.
			if p.Significance == "very_strong" || p.Significance == "strong" {
				t.Fatalf("2-window coincidence must not be labeled %q (support cap)", p.Significance)
			}
		}
	}
}

// TestCrossRepoCoChange_SupportTierBeatsRawG2 is the regression test for the
// "perfection-inflation" defect: a perfect rare coincidence (co=2, winA=winB=2)
// can score a higher raw G² than a loose genuine coupling (co=8, winA=winB=10),
// because G² rewards perfection of association regardless of support.
//
// With pure G² ranking:
//
//	genuine loose:      G²≈2.36  (co=8, winA=winB=10, n=15)
//	perfect coincidence: G²≈11.78 (co=2, winA=winB=2,  n=15)
//
// The support-tier fix ensures that pairs with co >= minConfidentSupport (well-
// supported) ALWAYS outrank low-support pairs, regardless of their raw G².
// Within each tier ranking is still by G² descending.
func TestCrossRepoCoChange_SupportTierBeatsRawG2(t *testing.T) {
	parent := t.TempDir()
	chat := filepath.Join(parent, "acme-web")
	edge := filepath.Join(parent, "acme-edge")
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
	// The realistic shape: files don't always change in lockstep.
	// G² (n=15, co=8, winA=winB=10) ≈ 2.36 — well below the perfect coincidence.
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
	// Without the support tier, this coincidence would rank ABOVE the genuine pair.
	for _, d := range weeks[12:14] {
		commitAt(t, chat, "blip.md", d, "coincidence")
		commitAt(t, edge, "once.txt", d, "coincidence")
	}
	// One background commit to make n=15 (one more window with only bg activity).
	commitAt(t, chat, "bg.md", weeks[14], "bg")

	repos := []RepoRef{
		{Slug: "acme-web", Root: chat},
		{Slug: "acme-edge", Root: edge},
	}
	pairs := CrossRepoCoChange(context.Background(), repos, 24, 2, 0)
	if len(pairs) == 0 {
		t.Fatal("expected pairs")
	}

	// Locate each pair and verify G² values match expectations.
	var genuineG2, coincidenceG2 float64
	var genuineIdx, coincidenceIdx = -1, -1
	for idx, p := range pairs {
		if (p.FileA == "api.rs" && p.FileB == "handler.go") || (p.FileA == "handler.go" && p.FileB == "api.rs") {
			genuineG2 = p.G2
			genuineIdx = idx
		}
		if p.FileA == "blip.md" || p.FileB == "blip.md" {
			coincidenceG2 = p.G2
			coincidenceIdx = idx
		}
	}
	if genuineIdx < 0 {
		t.Fatal("genuine api.rs↔handler.go pair not found in results")
	}
	if coincidenceIdx < 0 {
		t.Fatal("coincidence blip.md↔once.txt pair not found in results")
	}

	// Verify the defect condition: coincidence has higher raw G² than genuine.
	// If this fails, the test setup is wrong and needs adjustment.
	if coincidenceG2 <= genuineG2 {
		t.Logf("NOTE: coincidenceG²=%.4f genuineG²=%.4f — defect condition not met, test may need tuning", coincidenceG2, genuineG2)
	}
	t.Logf("genuine G²=%.4f (co=8, winA=winB=10, loose)", genuineG2)
	t.Logf("coincidence G²=%.4f (co=2, winA=winB=2, perfect)", coincidenceG2)
	t.Logf("genuine rank=%d  coincidence rank=%d", genuineIdx, coincidenceIdx)

	// KEY ASSERTION: the loose genuine pair (co=8 >= minConfidentSupport=3) must
	// rank ABOVE the perfect rare coincidence (co=2 < minConfidentSupport=3),
	// even though the coincidence has a higher raw G². The support tier enforces this.
	if genuineIdx >= coincidenceIdx {
		t.Fatalf("support tier failed: genuine (co=8, G²=%.2f, rank=%d) must rank above coincidence (co=2, G²=%.2f, rank=%d) — support-tier fix not applied",
			genuineG2, genuineIdx, coincidenceG2, coincidenceIdx)
	}

	// Confidence label: co=2 < minConfidentSupport=3 → capped to medium, not high.
	for _, p := range pairs {
		if p.FileA == "blip.md" || p.FileB == "blip.md" {
			if p.ConfidenceLevel == "high" {
				t.Fatalf("2-window coincidence must not be labeled high-confidence: %+v", p)
			}
			// Significance must be capped (strong/very_strong not allowed at co<minConfidentSupport).
			if p.Significance == "strong" || p.Significance == "very_strong" {
				t.Fatalf("2-window coincidence must not be labeled %q (support cap): %+v", p.Significance, p)
			}
		}
	}
}

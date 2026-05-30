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
// Laplace-smoothed lift ranks a genuine high-support coupling (api.rs ↔
// handler.go, co=8 of 15 windows) ABOVE both a high-frequency noise pair
// (noisy.ts ↔ util.go, always co-occurring but low lift) and a rare 2-window
// coincidence (blip.md ↔ once.txt, would dominate raw lift without smoothing).
//
// This is a regression test for the rare-coincidence inflation class: without
// smoothing, co=2 winA=2 winB=2 N=17 → raw lift ≈ 8.5, which buries the
// genuine co=8 pair (raw lift ≈ 2.0). With liftSmoothingAlpha=8 the smoothed
// lifts invert: genuine ≈ 3.9, coincidence ≈ 1.6.
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
	// minLift=0 → no floor, rank by smoothed lift only.
	pairs := CrossRepoCoChange(context.Background(), repos, 24, 2, 0)
	if len(pairs) == 0 {
		t.Fatal("expected pairs")
	}
	// The genuine high-support coupling must rank FIRST — above the high-frequency
	// noise (low lift) and above the rare coincidence (smoothing damps its lift).
	top := pairs[0]
	isGenuine := (top.FileA == "api.rs" && top.FileB == "handler.go") ||
		(top.FileA == "handler.go" && top.FileB == "api.rs")
	if !isGenuine {
		t.Fatalf("top must be the genuine api.rs↔handler.go coupling, got %s/%s↔%s/%s lift=%.3f co=%d",
			top.RepoA, top.FileA, top.RepoB, top.FileB, top.Lift, top.CoChanges)
	}
	// Find the coincidence pair and assert it is NOT labeled "high" confidence
	// (co=2 < minConfidentSupport=3 must cap the label to "medium").
	for _, p := range pairs {
		if (p.FileA == "blip.md" || p.FileB == "blip.md") && p.ConfidenceLevel == "high" {
			t.Fatalf("2-window coincidence must not be labeled high-confidence: %+v", p)
		}
	}
}

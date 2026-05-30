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
	chat := filepath.Join(parent, "oxpulse-chat")
	edge := filepath.Join(parent, "oxpulse-partner-edge")
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
		{Slug: "oxpulse-chat", Root: chat},
		{Slug: "oxpulse-partner-edge", Root: edge},
	}
	pairs := CrossRepoCoChange(context.Background(), repos, 24, 2) // 24h window, min 2
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
	}, 24, 2)
	if len(pairs) != 0 {
		t.Fatalf("disjoint timelines → no pairs, got %v", pairs)
	}
}

func TestCrossRepoCoChange_WindowWidthDiscriminates(t *testing.T) {
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

	repos := []RepoRef{
		{Slug: "oxpulse-chat", Root: chat},
		{Slug: "oxpulse-partner-edge", Root: edge},
	}
	// Under a 24h window they co-occur (same bucket).
	if got := CrossRepoCoChange(context.Background(), repos, 24, 1); len(got) == 0 {
		t.Fatal("3h-apart commits must pair under a 24h window")
	}
	// Under a 1h window they do NOT (different buckets).
	if got := CrossRepoCoChange(context.Background(), repos, 1, 1); len(got) != 0 {
		t.Fatalf("3h-apart commits must NOT pair under a 1h window, got %v", got)
	}
}

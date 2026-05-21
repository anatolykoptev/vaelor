package pinned

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func TestCollect_SampleRepo(t *testing.T) {
	root := filepath.Join("testdata", "sample-repo")
	got, err := Collect(root)
	if err != nil {
		t.Fatalf("Collect(%s) error: %v", root, err)
	}

	// Expect: 2 from Dockerfile (multi-stage), 1 from api.Dockerfile,
	//         2 from docker-compose.yml.
	const wantTotal = 5
	if len(got) != wantTotal {
		t.Fatalf("len=%d want %d\ngot=%#v", len(got), wantTotal, got)
	}

	// Group by Source to assert per-file counts.
	bySource := map[string]int{}
	for _, p := range got {
		bySource[p.Source]++
	}
	for src, n := range bySource {
		t.Logf("source=%s count=%d", src, n)
	}
	if bySource[filepath.Join(root, "Dockerfile")] != 2 {
		t.Errorf("Dockerfile count=%d want 2", bySource[filepath.Join(root, "Dockerfile")])
	}
	if bySource[filepath.Join(root, "api.Dockerfile")] != 1 {
		t.Errorf("api.Dockerfile count=%d want 1", bySource[filepath.Join(root, "api.Dockerfile")])
	}
	if bySource[filepath.Join(root, "docker-compose.yml")] != 2 {
		t.Errorf("docker-compose.yml count=%d want 2", bySource[filepath.Join(root, "docker-compose.yml")])
	}

	// Sanity: all images have non-empty Image OR non-empty Unresolved.
	for i, p := range got {
		if p.Image == "" && p.Unresolved == "" {
			t.Errorf("[%d] %#v: both Image and Unresolved empty", i, p)
		}
	}

	// Sanity: deterministic ordering for stable output.
	sortedByKey := append([]PinnedImage(nil), got...)
	sort.SliceStable(sortedByKey, func(i, j int) bool {
		if sortedByKey[i].Source != sortedByKey[j].Source {
			return sortedByKey[i].Source < sortedByKey[j].Source
		}
		return sortedByKey[i].Line < sortedByKey[j].Line
	})
	for i := range got {
		if got[i] != sortedByKey[i] {
			t.Errorf("Collect not stably sorted by (Source,Line) at idx %d:\n got=%#v\nwant=%#v", i, got[i], sortedByKey[i])
			break
		}
	}
}

func TestCollect_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	got, err := Collect(dir)
	if err != nil {
		t.Fatalf("Collect(empty) error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected empty, got %#v", got)
	}
}

// TestCollect_SkipsUnreadableSubdir — walk must not abort when one subdir
// cannot be entered (permission denied). Real-world repro: data/youtube/
// in /home/krolik/deploy/krolik-server produced 0 pinned because filepath.Walk
// returned the perm-denied error and aborted before reaching siblings.
func TestCollect_SkipsUnreadableSubdir(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permission tests cannot run as root")
	}
	root := t.TempDir()

	if err := os.WriteFile(filepath.Join(root, "docker-compose.yml"), []byte(`services:
  web:
    image: nginx:1.27
`), 0o644); err != nil {
		t.Fatal(err)
	}
	blocked := filepath.Join(root, "blocked")
	if err := os.Mkdir(blocked, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(blocked, "compose.yml"), []byte(`services:
  inner:
    image: alpine:3.20
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(blocked, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(blocked, 0o755) }) // allow t.TempDir cleanup

	graphiti := filepath.Join(root, "graphiti")
	if err := os.Mkdir(graphiti, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(graphiti, "docker-compose.yml"), []byte(`services:
  graph:
    image: neo4j:5.26.2
`), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := Collect(root)
	if err != nil {
		t.Fatalf("Collect: expected nil error, got %v", err)
	}

	images := make(map[string]string)
	for _, p := range got {
		images[p.Image] = p.Tag
	}
	if images["nginx"] != "1.27" {
		t.Errorf("missing nginx:1.27 (root compose not parsed); got %v", got)
	}
	if images["neo4j"] != "5.26.2" {
		t.Errorf("missing neo4j:5.26.2 (sibling subdir skipped); got %v", got)
	}
	if _, ok := images["alpine"]; ok {
		t.Errorf("blocked dir parsed unexpectedly: %v", got)
	}
}

// TestCollect_SkipsNestedGitAndAgentWorktrees — Collect must not aggregate
// images from nested git repos (submodules) or .claude/worktrees/ subdirs.
// Real-world repro: fleet_versions reported false drifts from agent-session
// worktrees and third-party submodules inside the deploy clone.
func TestCollect_SkipsNestedGitAndAgentWorktrees(t *testing.T) {
	root := t.TempDir()

	// Top-level (the legit operator-controlled repo):
	if err := os.WriteFile(filepath.Join(root, "docker-compose.yml"), []byte(`services:
  web:
    image: nginx:1.27
`), 0o644); err != nil {
		t.Fatal(err)
	}
	// Pretend the walk root itself is a git repo — should NOT be skipped.
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Submodule with its own pin (FILE-form .git pointer).
	sub := filepath.Join(root, "vendored-sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, ".git"), []byte("gitdir: ../.git/modules/vendored-sub\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "docker-compose.yml"), []byte(`services:
  third-party:
    image: vendor/thirdparty:0.1
`), 0o644); err != nil {
		t.Fatal(err)
	}

	// Nested git repo (DIR-form .git).
	nested := filepath.Join(root, "nested-repo")
	if err := os.MkdirAll(filepath.Join(nested, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nested, "docker-compose.yml"), []byte(`services:
  inner:
    image: alpine:3.20
`), 0o644); err != nil {
		t.Fatal(err)
	}

	// Agent worktree under .claude/worktrees/.
	agent := filepath.Join(root, ".claude", "worktrees", "agent-abc")
	if err := os.MkdirAll(agent, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agent, "docker-compose.yml"), []byte(`services:
  stale:
    image: stale/agent-state:v999
`), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := Collect(root)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	images := make(map[string]string)
	for _, p := range got {
		images[p.Image] = p.Tag
	}

	if images["nginx"] != "1.27" {
		t.Errorf("missing nginx:1.27 (root compose): %v", got)
	}
	if _, ok := images["vendor/thirdparty"]; ok {
		t.Errorf("submodule pin leaked: %v", got)
	}
	if _, ok := images["alpine"]; ok {
		t.Errorf("nested-repo pin leaked: %v", got)
	}
	if _, ok := images["stale/agent-state"]; ok {
		t.Errorf(".claude/worktrees pin leaked: %v", got)
	}
}

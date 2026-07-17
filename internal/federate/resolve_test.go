package federate

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func gitInit(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "-C", dir, "init").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
}

func TestResolveRepos_All(t *testing.T) {
	t.Parallel()
	parent := t.TempDir()
	gitInit(t, filepath.Join(parent, "acme-web"))
	gitInit(t, filepath.Join(parent, "acme-admin"))
	gitInit(t, filepath.Join(parent, "go-code"))

	got, err := ResolveRepos(context.Background(), "all", []string{parent})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("all → 3 repos, got %d: %v", len(got), got)
	}
}

func TestResolveRepos_Glob(t *testing.T) {
	t.Parallel()
	parent := t.TempDir()
	gitInit(t, filepath.Join(parent, "acme-web"))
	gitInit(t, filepath.Join(parent, "acme-admin"))
	gitInit(t, filepath.Join(parent, "go-code"))

	got, err := ResolveRepos(context.Background(), "acme-*", []string{parent})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("acme-* → 2 repos, got %d: %v", len(got), got)
	}
	for _, r := range got {
		if filepath.Base(r.Root) == "go-code" {
			t.Fatalf("go-code must not match acme-*: %v", got)
		}
	}
}

func TestResolveRepos_SinglePath(t *testing.T) {
	t.Parallel()
	parent := t.TempDir()
	root := filepath.Join(parent, "acme-web")
	gitInit(t, root)

	got, err := ResolveRepos(context.Background(), root, []string{parent})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Root != root {
		t.Fatalf("single path → that repo, got %v", got)
	}
}

func TestResolveRepos_PlainName(t *testing.T) {
	t.Parallel()
	parent := t.TempDir()
	gitInit(t, filepath.Join(parent, "acme-web"))
	gitInit(t, filepath.Join(parent, "go-code"))

	got, err := ResolveRepos(context.Background(), "go-code", []string{parent})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Slug != "go-code" {
		t.Fatalf("plain name → that one repo, got %v", got)
	}
}

func TestResolveRepos_GlobNoMatch(t *testing.T) {
	t.Parallel()
	parent := t.TempDir()
	gitInit(t, filepath.Join(parent, "go-code"))
	got, err := ResolveRepos(context.Background(), "nonexistent-*", []string{parent})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("no-match glob → empty, got %v", got)
	}
}

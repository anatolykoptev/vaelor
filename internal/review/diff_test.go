package review

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func setupGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test.com",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test.com")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %s: %s", args, err, out)
		}
	}
	run("init")
	run("config", "user.email", "test@test.com")
	run("config", "user.name", "test")
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\nfunc main() {}\n"), 0o644)
	run("add", ".")
	run("commit", "-m", "initial")
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\nfunc main() { run() }\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "util.go"), []byte("package main\nfunc run() {}\n"), 0o644)
	run("add", ".")
	run("commit", "-m", "second")
	return dir
}

func TestChangedFiles(t *testing.T) {
	dir := setupGitRepo(t)
	files, err := ChangedFiles(context.Background(), dir, "HEAD~1")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Fatalf("expected 2 changed files, got %d: %v", len(files), files)
	}
}

func TestChangedFilesStagedFallback(t *testing.T) {
	dir := setupGitRepo(t)
	os.WriteFile(filepath.Join(dir, "new.go"), []byte("package main\n"), 0o644)
	exec.Command("git", "-C", dir, "add", "new.go").Run()

	files, err := ChangedFiles(context.Background(), dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) < 1 {
		t.Fatal("expected at least 1 staged file")
	}
}

func TestParseHunkHeader(t *testing.T) {
	tests := []struct {
		line string
		want LineRange
		ok   bool
	}{
		{"@@ -10,3 +15,4 @@ func foo()", LineRange{15, 18}, true},
		{"@@ -5 +5,2 @@", LineRange{5, 6}, true},
		{"@@ -5,3 +5,0 @@", LineRange{}, false},
	}
	for _, tt := range tests {
		got, ok := parseHunkHeader(tt.line)
		if ok != tt.ok || got != tt.want {
			t.Errorf("parseHunkHeader(%q) = %v, %v; want %v, %v", tt.line, got, ok, tt.want, tt.ok)
		}
	}
}

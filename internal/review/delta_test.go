package review

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestDeltaReview(t *testing.T) {
	dir := setupGitRepoWithSymbols(t)
	result, err := DeltaReview(context.Background(), DeltaInput{
		Root:  dir,
		Base:  "HEAD~1",
		Depth: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ChangedFiles) == 0 {
		t.Error("expected changed files")
	}
	if result.Risk.RiskLevel == "" {
		t.Error("expected risk level")
	}
}

func setupGitRepoWithSymbols(t *testing.T) string {
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

	src := "package main\n\nfunc ProcessOrder(id int) error {\n\treturn nil\n}\n\nfunc Helper() string {\n\treturn \"help\"\n}\n"
	os.WriteFile(filepath.Join(dir, "main.go"), []byte(src), 0o644)
	run("add", ".")
	run("commit", "-m", "initial")

	src2 := "package main\n\nimport \"fmt\"\n\nfunc ProcessOrder(id int) error {\n\tif id <= 0 {\n\t\treturn fmt.Errorf(\"invalid id\")\n\t}\n\treturn nil\n}\n\nfunc Helper() string {\n\treturn \"help\"\n}\n"
	os.WriteFile(filepath.Join(dir, "main.go"), []byte(src2), 0o644)
	run("add", ".")
	run("commit", "-m", "validate id")

	return dir
}

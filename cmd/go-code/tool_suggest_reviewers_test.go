package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anatolykoptev/go-code/internal/analyze"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// mkSuggestReviewersRepo creates a temporary git repo with:
//   - two commits by Alice on a.go
//   - one commit by Bob on b.go
func mkSuggestReviewersRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(env []string, args ...string) {
		cmd := exec.CommandContext(context.Background(), "git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(), env...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run(nil, "init", "-b", "main")
	run(nil, "config", "user.email", "x@x")
	run(nil, "config", "user.name", "x")
	commit := func(author, path, body string) {
		if err := os.WriteFile(filepath.Join(dir, path), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		run(nil, "add", path)
		mail := strings.ToLower(author) + "@example.test"
		env := []string{
			"GIT_AUTHOR_NAME=" + author, "GIT_AUTHOR_EMAIL=" + mail,
			"GIT_COMMITTER_NAME=" + author, "GIT_COMMITTER_EMAIL=" + mail,
		}
		run(env, "commit", "-m", "msg")
	}
	commit("Alice", "a.go", "alpha\n")
	commit("Alice", "a.go", "alpha v2\n")
	commit("Bob", "b.go", "bravo\n")
	return dir
}

// extractText extracts the text content from the first content item in a CallToolResult.
func extractText(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	if len(result.Content) == 0 {
		t.Fatal("empty content in result")
	}
	tc, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected *mcp.TextContent, got %T", result.Content[0])
	}
	return tc.Text
}

func TestSuggestReviewers_RequiresRepo(t *testing.T) {
	args := SuggestReviewersArgs{
		Repo:  "",
		Paths: []string{"a.go"},
	}
	result, err := handleSuggestReviewersCore(t.Context(), args, analyze.Deps{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Errorf("expected IsError=true for empty repo, got false; content=%v", result.Content)
	}
}

func TestSuggestReviewers_RequiresPaths(t *testing.T) {
	dir := t.TempDir()
	args := SuggestReviewersArgs{
		Repo:  dir,
		Paths: nil,
	}
	result, err := handleSuggestReviewersCore(t.Context(), args, analyze.Deps{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Errorf("expected IsError=true for empty paths, got false; content=%v", result.Content)
	}
}

func TestSuggestReviewers_DirectAuthorshipWins(t *testing.T) {
	dir := mkSuggestReviewersRepo(t)

	args := SuggestReviewersArgs{
		Repo:  dir,
		Paths: []string{"a.go"},
	}
	result, err := handleSuggestReviewersCore(t.Context(), args, analyze.Deps{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %s", extractText(t, result))
	}

	text := extractText(t, result)

	var parsed SuggestReviewersResult
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		t.Fatalf("unmarshal result: %v\nraw: %s", err, text)
	}

	if len(parsed.Files) == 0 {
		t.Fatal("expected at least 1 file entry in result")
	}

	file := parsed.Files[0]
	if file.Path != "a.go" {
		t.Errorf("expected path=a.go, got %q", file.Path)
	}
	if len(file.Suggestions) == 0 {
		t.Fatal("expected at least 1 suggestion for a.go")
	}

	top := file.Suggestions[0]
	if top.Name != "Alice" {
		t.Errorf("expected top reviewer = Alice (direct=2), got %q (score=%f signal=%q)",
			top.Name, top.Score, top.Signal)
	}
	if !strings.Contains(top.Signal, "direct=2") {
		t.Errorf("expected signal to contain 'direct=2', got %q", top.Signal)
	}
	if !strings.Contains(top.Signal, "recent=true") {
		t.Errorf("expected signal to contain 'recent=true', got %q", top.Signal)
	}
}

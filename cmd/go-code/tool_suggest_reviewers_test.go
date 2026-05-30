package main

import (
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
		cmd := exec.CommandContext(t.Context(), "git", append([]string{"-C", dir}, args...)...)
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

// mkSuggestReviewersCouplingRepo sets up: Alice authors a.go directly
// twice (so she's the top direct candidate), then Bob authors TWO joint
// commits touching BOTH a.go and b.go (so a.go and b.go couple via 2
// co-changes, and Bob shows up as a partner author).
func mkSuggestReviewersCouplingRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(env []string, args ...string) {
		cmd := exec.CommandContext(t.Context(), "git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(), env...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run(nil, "init", "-b", "main")
	run(nil, "config", "user.email", "x@x")
	run(nil, "config", "user.name", "x")
	writeAdd := func(author string, contents map[string]string) {
		for path, body := range contents {
			if err := os.WriteFile(filepath.Join(dir, path), []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
			run(nil, "add", path)
		}
		mail := strings.ToLower(author) + "@example.test"
		env := []string{
			"GIT_AUTHOR_NAME=" + author, "GIT_AUTHOR_EMAIL=" + mail,
			"GIT_COMMITTER_NAME=" + author, "GIT_COMMITTER_EMAIL=" + mail,
		}
		run(env, "commit", "-m", "msg")
	}
	// Alice's direct work on a.go (2 commits, so she's top direct candidate).
	writeAdd("Alice", map[string]string{"a.go": "alpha\n"})
	writeAdd("Alice", map[string]string{"a.go": "alpha v2\n"})
	// Bob's two joint commits touching both a.go AND b.go (>=2 co-changes
	// satisfies CollectCoupling minCoChanges).
	writeAdd("Bob", map[string]string{"a.go": "alpha + joint\n", "b.go": "beta init\n"})
	writeAdd("Bob", map[string]string{"a.go": "alpha + joint v2\n", "b.go": "beta v2\n"})
	return dir
}

// TestSuggestReviewers_PerFileErrorSetsErrorField guards Important-2: a
// per-path fileAuthors failure must surface via PerFileSuggestions.Error
// instead of injecting a Suggestion with magic Name "<error>".
func TestSuggestReviewers_PerFileErrorSetsErrorField(t *testing.T) {
	dir := mkSuggestReviewersRepo(t)
	// Pass a path that will yield no authorship history; the score-loop
	// path either degrades to no Suggestions OR errors per-file. Either
	// way, the "<error>" sentinel name must NEVER appear in the output.
	res, err := handleSuggestReviewersCore(t.Context(), SuggestReviewersArgs{
		Repo:  dir,
		Paths: []string{"/totally/bogus/path.go"},
	}, analyze.Deps{})
	if err != nil || res.IsError {
		t.Fatalf("unexpected: err=%v isErr=%v", err, res.IsError)
	}
	body := extractText(t, res)
	if strings.Contains(body, `"name": "<error>"`) {
		t.Fatalf("must not emit <error> sentinel name, got body:\n%s", body)
	}
}

// TestSuggestReviewers_CoChangePartnerSignal guards the
// suggestReviewersWeightCoChange = 0.5 path — partner authors must
// surface in suggestions when their co-change ratio satisfies the
// minCoChanges floor (=2).
func TestSuggestReviewers_CoChangePartnerSignal(t *testing.T) {
	dir := mkSuggestReviewersCouplingRepo(t)
	res, err := handleSuggestReviewersCore(t.Context(), SuggestReviewersArgs{
		Repo:  dir,
		Paths: []string{"a.go"},
	}, analyze.Deps{})
	if err != nil || res.IsError {
		t.Fatalf("unexpected: err=%v res=%v", err, res)
	}
	body := extractText(t, res)
	// Bob touched a.go in joint commits — must appear in suggestions.
	if !strings.Contains(body, `"name": "Bob"`) {
		t.Fatalf("Bob should appear via co-change signal, body:\n%s", body)
	}
	// The co-change signal must be present for at least one candidate.
	if !strings.Contains(body, "co-change=") {
		t.Fatalf("co-change signal missing in body:\n%s", body)
	}
	// Bob's signal must show co-change >= 1 (the joint commits contribute
	// to partnerCounts via b.go authorship).
	if !strings.Contains(body, "co-change=2") && !strings.Contains(body, "co-change=1") {
		t.Fatalf("Bob's co-change signal expected co-change>=1, got body:\n%s", body)
	}
}

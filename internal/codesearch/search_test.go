package codesearch

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSearch_LiteralMatch(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "main.go", "package main\n\nfunc main() {\n\tfmt.Println(\"hello world\")\n}\n")
	writeFile(t, dir, "util.go", "package main\n\nfunc helper() string {\n\treturn \"hello world\"\n}\n")

	results, err := Search(context.Background(), SearchInput{
		Root:    dir,
		Pattern: "hello world",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 matches, got %d", len(results))
	}
}

func TestSearch_RegexMatch(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "main.go", "package main\n\nfunc handleUserCreate() {}\nfunc handleUserDelete() {}\nfunc otherFunc() {}\n")

	results, err := Search(context.Background(), SearchInput{
		Root:    dir,
		Pattern: "func handle[A-Z]\\w+",
		IsRegex: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 matches, got %d", len(results))
	}
}

func TestSearch_FileFilter(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "main.go", "TODO: fix this\n")
	writeFile(t, dir, "readme.md", "TODO: update docs\n")
	writeFile(t, dir, "test.py", "# TODO: add tests\n")

	results, err := Search(context.Background(), SearchInput{
		Root:     dir,
		Pattern:  "TODO",
		FileGlob: "*.go",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 match (only .go), got %d", len(results))
	}
}

func TestSearch_ContextLines(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "main.go", "line1\nline2\nMATCH\nline4\nline5\n")

	results, err := Search(context.Background(), SearchInput{
		Root:         dir,
		Pattern:      "MATCH",
		ContextLines: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatal("expected 1 match")
	}
	if len(results[0].Context) != 3 {
		t.Errorf("expected 3 context lines (before + match + after), got %d", len(results[0].Context))
	}
}

func TestSearch_MaxResults(t *testing.T) {
	dir := t.TempDir()
	content := strings.Repeat("match_line\n", 20)
	writeFile(t, dir, "main.go", content)

	results, err := Search(context.Background(), SearchInput{
		Root:       dir,
		Pattern:    "match_line",
		MaxResults: 5,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 5 {
		t.Fatalf("expected 5 matches (capped), got %d", len(results))
	}
}

func TestSearch_Empty(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "main.go", "nothing here\n")

	results, err := Search(context.Background(), SearchInput{
		Root:    dir,
		Pattern: "nonexistent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Fatalf("expected 0 matches, got %d", len(results))
	}
}

func TestSearch_CaseInsensitive(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "main.go", "Hello World\nhello world\nHELLO WORLD\n")

	results, err := Search(context.Background(), SearchInput{
		Root:          dir,
		Pattern:       "hello world",
		CaseSensitive: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 matches (case insensitive), got %d", len(results))
	}
}

// TestSearch_MatchDensityRanking verifies that files with more matches appear first.
// Three files: aa.go (1 match), bb.go (5 matches), cc.go (3 matches).
// With MaxResults=6, results should come from bb.go first (5), then cc.go (1).
func TestSearch_MatchDensityRanking(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "aa.go", "package main\n// TODO fix\nfunc aa() {}\n")
	writeFile(t, dir, "bb.go", "package main\n// TODO one\n// TODO two\n// TODO three\n// TODO four\n// TODO five\n")
	writeFile(t, dir, "cc.go", "package main\n// TODO alpha\n// TODO beta\n// TODO gamma\n")

	results, err := Search(context.Background(), SearchInput{
		Root:       dir,
		Pattern:    "TODO",
		MaxResults: 6,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 6 {
		t.Fatalf("expected 6 matches, got %d", len(results))
	}

	// First 5 results must come from bb.go (most matches).
	for i := 0; i < 5; i++ {
		if results[i].File != "bb.go" {
			t.Errorf("result[%d]: expected bb.go, got %s", i, results[i].File)
		}
	}
	// Result 6 must come from cc.go (next highest density).
	if results[5].File != "cc.go" {
		t.Errorf("result[5]: expected cc.go, got %s", results[5].File)
	}
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()

	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

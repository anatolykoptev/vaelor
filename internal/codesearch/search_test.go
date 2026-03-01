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

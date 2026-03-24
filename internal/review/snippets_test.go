package review

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/anatolykoptev/go-code/internal/parser"
)

func TestExtractSnippets(t *testing.T) {
	dir := t.TempDir()
	src := "package main\n\nimport \"fmt\"\n\nfunc Hello() {\n\tfmt.Println(\"hello\")\n}\n\nfunc World() {\n\tfmt.Println(\"world\")\n}\n"
	os.WriteFile(filepath.Join(dir, "main.go"), []byte(src), 0o644)

	changed := []ChangedSymbol{
		{
			Symbol:   &parser.Symbol{Name: "Hello", StartLine: 5, EndLine: 7, File: filepath.Join(dir, "main.go")},
			FileDiff: FileDiff{Path: "main.go"},
		},
	}

	snippets := ExtractSnippets(changed, dir)
	if len(snippets) != 1 {
		t.Fatalf("expected 1 snippet, got %d", len(snippets))
	}
	s := snippets[0]
	if s.Symbol != "Hello" {
		t.Errorf("expected Hello, got %s", s.Symbol)
	}
	if s.StartLine != 2 { // line 5 minus 3 context = line 2
		t.Errorf("expected start line 2, got %d", s.StartLine)
	}
	if s.Code == "" {
		t.Error("expected non-empty code")
	}
	// Should contain the function and some context.
	if !contains(s.Code, "Hello") || !contains(s.Code, "import") {
		t.Errorf("snippet should contain Hello and import context, got:\n%s", s.Code)
	}
}

func TestExtractSnippetsMaxLines(t *testing.T) {
	dir := t.TempDir()
	// Write a file with 50 lines.
	var lines string
	for i := 1; i <= 50; i++ {
		lines += "// line\n"
	}
	os.WriteFile(filepath.Join(dir, "big.go"), []byte(lines), 0o644)

	changed := []ChangedSymbol{
		{
			Symbol:   &parser.Symbol{Name: "BigFunc", StartLine: 1, EndLine: 50, File: filepath.Join(dir, "big.go")},
			FileDiff: FileDiff{Path: "big.go"},
		},
	}

	snippets := ExtractSnippets(changed, dir)
	if len(snippets) != 1 {
		t.Fatalf("expected 1 snippet, got %d", len(snippets))
	}
	// Should be clamped to maxSnippetLines (30).
	lineCount := countLines(snippets[0].Code)
	if lineCount > maxSnippetLines+1 { // +1 for trailing newline
		t.Errorf("expected at most %d lines, got %d", maxSnippetLines, lineCount)
	}
}

func contains(s, substr string) bool {
	return len(s) > 0 && len(substr) > 0 && (s == substr || len(s) > len(substr) && containsSubstr(s, substr))
}

func containsSubstr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func countLines(s string) int {
	n := 0
	for _, c := range s {
		if c == '\n' {
			n++
		}
	}
	return n
}

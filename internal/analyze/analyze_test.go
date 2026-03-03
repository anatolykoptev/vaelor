package analyze

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anatolykoptev/go-code/internal/goutil"
	"github.com/anatolykoptev/go-code/internal/ingest"
	"github.com/anatolykoptev/go-code/internal/parser"
)

// writeFile creates parent directories and writes a file, failing the test on error.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// makeFixtureRepo creates a small Go repo in a temp directory.
func makeFixtureRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()

	writeFile(t, filepath.Join(root, "main.go"), `package main

import "fmt"

// main is the entry point.
func main() {
	fmt.Println("hello")
}
`)
	writeFile(t, filepath.Join(root, "util", "util.go"), `package util

import "errors"

// Add adds two integers.
func Add(a, b int) int {
	return a + b
}

// Subtract subtracts b from a.
func Subtract(a, b int) int {
	if b == 0 {
		return a
	}
	_ = errors.New("example")
	return a - b
}
`)
	writeFile(t, filepath.Join(root, "util", "types.go"), `package util

// Config holds configuration.
type Config struct {
	Name string
	Port int
}
`)
	return root
}

// --- AnalyzeRepo tests ---

func TestAnalyzeRepo(t *testing.T) {
	root := makeFixtureRepo(t)
	ctx := context.Background()

	deps := Deps{
		MaxFileBytes: defaultMaxFileBytes,
	}

	result, err := AnalyzeRepo(ctx, RepoAnalysisInput{
		Root:  root,
		Query: "What functions are defined?",
	}, deps)
	if err != nil {
		t.Fatalf("AnalyzeRepo: %v", err)
	}

	if result.RepoName == "" {
		t.Error("expected non-empty repo name")
	}
	if result.FileCount == 0 {
		t.Error("expected file count > 0")
	}
	if result.Language != "go" {
		t.Errorf("expected dominant language 'go', got %q", result.Language)
	}
	if len(result.Packages) == 0 {
		t.Error("expected at least one package")
	}
	if len(result.Files) == 0 {
		t.Error("expected at least one analyzed file")
	}
	if len(result.Symbols) == 0 {
		t.Error("expected at least one symbol")
	}
	if result.FileTree == "" {
		t.Error("expected non-empty file tree")
	}
}

func TestAnalyzeRepo_LanguageFilter(t *testing.T) {
	root := makeFixtureRepo(t)
	// Add a Python file to the fixture.
	writeFile(t, filepath.Join(root, "script.py"), "def hello(): pass\n")

	ctx := context.Background()
	deps := Deps{
		MaxFileBytes: defaultMaxFileBytes,
	}

	result, err := AnalyzeRepo(ctx, RepoAnalysisInput{
		Root:     root,
		Query:    "List all files",
		Language: "python",
	}, deps)
	if err != nil {
		t.Fatalf("AnalyzeRepo with language filter: %v", err)
	}
	// Only Python files should be analyzed.
	if result.Language != "python" {
		t.Errorf("expected dominant language 'python', got %q", result.Language)
	}
}

// --- SearchSymbols tests ---

func TestSearchSymbols_MatchAll(t *testing.T) {
	root := makeFixtureRepo(t)
	ctx := context.Background()

	symbols, err := SearchSymbols(ctx, SymbolSearchInput{
		Root:     root,
		Query:    "*",
		Language: "go",
	})
	if err != nil {
		t.Fatalf("SearchSymbols: %v", err)
	}
	if len(symbols) == 0 {
		t.Error("expected at least one symbol, got none")
	}
}

func TestSearchSymbols_PrefixWildcard(t *testing.T) {
	root := makeFixtureRepo(t)
	ctx := context.Background()

	symbols, err := SearchSymbols(ctx, SymbolSearchInput{
		Root:     root,
		Query:    "Add*",
		Language: "go",
	})
	if err != nil {
		t.Fatalf("SearchSymbols: %v", err)
	}
	for _, sym := range symbols {
		if !strings.HasPrefix(strings.ToLower(sym.Name), "add") {
			t.Errorf("unexpected symbol %q matched Add*", sym.Name)
		}
	}
}

func TestSearchSymbols_ExactMatch(t *testing.T) {
	root := makeFixtureRepo(t)
	ctx := context.Background()

	symbols, err := SearchSymbols(ctx, SymbolSearchInput{
		Root:  root,
		Query: "Config",
	})
	if err != nil {
		t.Fatalf("SearchSymbols: %v", err)
	}
	found := false
	for _, sym := range symbols {
		if sym.Name == "Config" {
			found = true
		}
	}
	if !found {
		t.Error("expected to find Config symbol")
	}
}

func TestSearchSymbols_KindFilter(t *testing.T) {
	root := makeFixtureRepo(t)
	ctx := context.Background()

	symbols, err := SearchSymbols(ctx, SymbolSearchInput{
		Root:  root,
		Query: "*",
		Kind:  parser.KindFunction,
	})
	if err != nil {
		t.Fatalf("SearchSymbols: %v", err)
	}
	for _, sym := range symbols {
		if sym.Kind != parser.KindFunction {
			t.Errorf("expected only functions, got %q (kind=%s)", sym.Name, sym.Kind)
		}
	}
}

func TestSearchSymbols_NoMatch(t *testing.T) {
	root := makeFixtureRepo(t)
	ctx := context.Background()

	symbols, err := SearchSymbols(ctx, SymbolSearchInput{
		Root:  root,
		Query: "NonExistentXYZ123",
	})
	if err != nil {
		t.Fatalf("SearchSymbols: %v", err)
	}
	if len(symbols) != 0 {
		t.Errorf("expected no matches, got %d", len(symbols))
	}
}

func TestSearchSymbols_Limit(t *testing.T) {
	root := makeFixtureRepo(t)
	ctx := context.Background()

	symbols, err := SearchSymbols(ctx, SymbolSearchInput{
		Root:  root,
		Query: "*",
		Limit: 2,
	})
	if err != nil {
		t.Fatalf("SearchSymbols: %v", err)
	}
	if len(symbols) > 2 {
		t.Errorf("expected at most 2 symbols, got %d", len(symbols))
	}
}

// --- BuildDepGraph tests ---

func TestBuildDepGraph_Formats(t *testing.T) {
	root := makeFixtureRepo(t)
	ctx := context.Background()

	tests := []struct {
		name    string
		format  string
		wantStr string
	}{
		{"mermaid format", "mermaid", "graph TD"},
		{"dot format", "dot", "digraph deps"},
		{"json format", "json", "{"},
		{"default format (mermaid)", "", "graph TD"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out, err := BuildDepGraph(ctx, DepGraphInput{
				Root:   root,
				Format: tc.format,
			})
			if err != nil {
				t.Fatalf("BuildDepGraph: %v", err)
			}
			if !strings.Contains(out, tc.wantStr) {
				t.Errorf("output %q does not contain %q", out, tc.wantStr)
			}
		})
	}
}

func TestBuildDepGraph_Focus(t *testing.T) {
	root := makeFixtureRepo(t)
	ctx := context.Background()

	out, err := BuildDepGraph(ctx, DepGraphInput{
		Root:   root,
		Format: "mermaid",
		Focus:  "util",
	})
	if err != nil {
		t.Fatalf("BuildDepGraph with focus: %v", err)
	}
	if !strings.Contains(out, "graph TD") {
		t.Errorf("expected mermaid header in focused output, got: %q", out)
	}
}

// --- helper unit tests ---

func TestWildcardToRegexp(t *testing.T) {
	tests := []struct {
		pattern string
		match   []string
		noMatch []string
	}{
		{
			pattern: "*",
			match:   []string{"anything", "Foo", "bar123"},
			noMatch: []string{},
		},
		{
			pattern: "Get*",
			match:   []string{"GetUser", "GetAll", "get_config"},
			noMatch: []string{"SetUser", "PostData"},
		},
		{
			pattern: "Config",
			match:   []string{"Config"},
			noMatch: []string{"UserConfig", "Configured"},
		},
		{
			pattern: "",
			match:   []string{"anything"},
			noMatch: []string{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.pattern, func(t *testing.T) {
			re, err := wildcardToRegexp(tc.pattern)
			if err != nil {
				t.Fatalf("wildcardToRegexp(%q): %v", tc.pattern, err)
			}
			for _, s := range tc.match {
				if !re.MatchString(s) {
					t.Errorf("pattern %q: expected %q to match", tc.pattern, s)
				}
			}
			for _, s := range tc.noMatch {
				if re.MatchString(s) {
					t.Errorf("pattern %q: expected %q NOT to match", tc.pattern, s)
				}
			}
		})
	}
}

func TestDominantLanguage(t *testing.T) {
	files := []*ingest.File{
		{Language: "go"}, {Language: "go"}, {Language: "go"}, {Language: "python"},
	}
	if got := dominantLanguage(files); got != "go" {
		t.Errorf("dominantLanguage = %q, want %q", got, "go")
	}
}

func TestMermaidID(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"internal/analyze", "internal_analyze"},
		{"github.com/foo/bar", "github_com_foo_bar"},
		{"simple", "simple"},
	}
	for _, tc := range tests {
		got := mermaidID(tc.input)
		if got != tc.want {
			t.Errorf("mermaidID(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestExtractQueryTerms(t *testing.T) {
	terms := extractQueryTerms("What functions are defined in util?")
	expected := []string{"what", "functions", "are", "defined", "util"}
	if len(terms) != len(expected) {
		t.Errorf("expected %d terms, got %d: %v", len(expected), len(terms), terms)
	}
	for i, term := range terms {
		if term != expected[i] {
			t.Errorf("term[%d]: got %q, want %q", i, term, expected[i])
		}
	}
}

func TestExtractQueryTerms_CamelCase(t *testing.T) {
	terms := extractQueryTerms("handleUserAuth middleware")
	want := map[string]bool{
		"handle":          true,
		"user":            true,
		"auth":            true,
		"handleuserauth":  true,
		"middleware":       true,
	}
	got := make(map[string]bool)
	for _, term := range terms {
		got[term] = true
	}
	for w := range want {
		if !got[w] {
			t.Errorf("expected term %q, got terms: %v", w, terms)
		}
	}
}

func TestExtractQueryTerms_SnakeCase(t *testing.T) {
	terms := extractQueryTerms("parse_file_content")
	want := map[string]bool{
		"parse":              true,
		"file":               true,
		"content":            true,
		"parse_file_content": true,
	}
	got := make(map[string]bool)
	for _, term := range terms {
		got[term] = true
	}
	for w := range want {
		if !got[w] {
			t.Errorf("expected term %q, got terms: %v", w, terms)
		}
	}
}

func TestExtractQueryTerms_MixedIdentifiers(t *testing.T) {
	terms := extractQueryTerms("What does BuildLLMContext do?")
	want := map[string]bool{
		"build":           true,
		"llm":             true,
		"context":         true,
		"buildllmcontext": true,
		"what":            true,
		"does":            true,
	}
	got := make(map[string]bool)
	for _, term := range terms {
		got[term] = true
	}
	for w := range want {
		if !got[w] {
			t.Errorf("expected term %q, got terms: %v", w, terms)
		}
	}
}

func TestBuildDepGraph_InvalidFormat(t *testing.T) {
	root := makeFixtureRepo(t)
	ctx := context.Background()

	_, err := BuildDepGraph(ctx, DepGraphInput{
		Root:   root,
		Format: "invalid_format_xyz",
	})
	if err == nil {
		t.Error("expected error for invalid format, got nil")
	}
	if err != nil && !strings.Contains(err.Error(), "unsupported format") {
		t.Errorf("expected 'unsupported format' error, got: %v", err)
	}
}

func TestIsStdlibImport(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"fmt", true},
		{"net/http", true},
		{"context", true},
		{"strings", true},
		{"github.com/foo/bar", false},
		{"golang.org/x/tools", false},
		{"example.com/pkg", false},
	}
	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			if got := goutil.IsStdlibImport(tc.path); got != tc.want {
				t.Errorf("IsStdlibImport(%q) = %v, want %v", tc.path, got, tc.want)
			}
		})
	}
}

// TestAnalyzeRepo_FusionRanking verifies that fusion ranking (BM25F + PPR + exact match)
// promotes files containing queried symbols and their callers above unrelated files.
func TestAnalyzeRepo_FusionRanking(t *testing.T) {
	root := t.TempDir()

	// core/core.go defines Process — the query target.
	writeFile(t, filepath.Join(root, "core", "core.go"), `package core

// Process does the main work.
func Process(data string) string {
	return data + " processed"
}
`)
	// handler/handler.go calls core.Process — should rank high via call edge + PPR.
	writeFile(t, filepath.Join(root, "handler", "handler.go"), `package handler

import "example.com/myapp/core"

// Handle calls Process from core.
func Handle(input string) string {
	return core.Process(input)
}
`)
	// util/util.go has no reference to Process.
	writeFile(t, filepath.Join(root, "util", "util.go"), `package util

// Format formats a string.
func Format(s string) string {
	return "[" + s + "]"
}
`)
	// go.mod so ingest recognizes it as Go.
	writeFile(t, filepath.Join(root, "go.mod"), "module example.com/myapp\n\ngo 1.26\n")

	ctx := context.Background()
	result, err := AnalyzeRepo(ctx, RepoAnalysisInput{
		Root:  root,
		Query: "Process",
	}, Deps{MaxFileBytes: defaultMaxFileBytes})
	if err != nil {
		t.Fatalf("AnalyzeRepo: %v", err)
	}

	// Build position map: relPath → rank index.
	pos := make(map[string]int)
	for i, f := range result.Files {
		pos[f.RelPath] = i
	}

	corePos, coreOk := pos["core/core.go"]
	handlerPos, handlerOk := pos["handler/handler.go"]
	utilPos, utilOk := pos["util/util.go"]

	if !coreOk || !handlerOk || !utilOk {
		t.Fatalf("missing files in result: core=%v handler=%v util=%v", coreOk, handlerOk, utilOk)
	}

	// core/core.go defines Process — must rank first.
	if corePos > handlerPos {
		t.Errorf("core/core.go (pos %d) should rank above handler/handler.go (pos %d)", corePos, handlerPos)
	}

	// handler/handler.go calls Process — must rank above unrelated util.
	if handlerPos > utilPos {
		t.Errorf("handler/handler.go (pos %d) should rank above util/util.go (pos %d)", handlerPos, utilPos)
	}
}

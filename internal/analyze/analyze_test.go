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
		"handle":         true,
		"user":           true,
		"auth":           true,
		"handleuserauth": true,
		"middleware":     true,
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

// TestSearchSymbols_ExactMatchBeatsContains verifies that an exact name match ranks
// above a "contains" match when both are in the same file and limit truncates.
func TestSearchSymbols_ExactMatchBeatsContains(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "proc.go"), `package proc

// Process does the main work.
func Process(data string) string { return data }

// ProcessData wraps Process with extra logic.
func ProcessData(data string) string { return Process(data) }
`)
	symbols, err := SearchSymbols(context.Background(), SymbolSearchInput{
		Root:  root,
		Query: "Process",
		Limit: 1,
	})
	if err != nil {
		t.Fatalf("SearchSymbols: %v", err)
	}
	if len(symbols) != 1 {
		t.Fatalf("expected 1 symbol, got %d", len(symbols))
	}
	if symbols[0].Name != "Process" {
		t.Errorf("expected exact match 'Process', got %q", symbols[0].Name)
	}
}

// TestSearchSymbols_ExportedBeatsUnexported verifies that an exported symbol ranks
// above an unexported one with the same name pattern.
func TestSearchSymbols_ExportedBeatsUnexported(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "handler.go"), `package handler

// handler is unexported.
func handler() string { return "private" }

// Handler is exported.
func Handler() string { return "public" }
`)
	symbols, err := SearchSymbols(context.Background(), SymbolSearchInput{
		Root:  root,
		Query: "*Handler*",
		Limit: 1,
	})
	if err != nil {
		t.Fatalf("SearchSymbols: %v", err)
	}
	if len(symbols) != 1 {
		t.Fatalf("expected 1 symbol, got %d", len(symbols))
	}
	if symbols[0].Name != "Handler" {
		t.Errorf("expected exported 'Handler', got %q", symbols[0].Name)
	}
}

// TestSearchSymbols_KindWeight verifies that a struct outranks a function
// when both match the query prefix and limit truncates.
func TestSearchSymbols_KindWeight(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "config.go"), `package config

// ConfigHelper creates a default Config.
func ConfigHelper() *Config { return nil }

// Config holds configuration.
type Config struct {
	Name string
}
`)
	symbols, err := SearchSymbols(context.Background(), SymbolSearchInput{
		Root:  root,
		Query: "Config*",
		Limit: 1,
	})
	if err != nil {
		t.Fatalf("SearchSymbols: %v", err)
	}
	if len(symbols) != 1 {
		t.Fatalf("expected 1 symbol, got %d", len(symbols))
	}
	if symbols[0].Name != "Config" {
		t.Errorf("expected struct 'Config' (kind weight 25 > 20), got %q", symbols[0].Name)
	}
}

// TestSearchSymbols_WildcardSkipsMatchQuality verifies that wildcard queries
// score symbols by visibility+kind only, without panic.
func TestSearchSymbols_WildcardSkipsMatchQuality(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "mix.go"), `package mix

type Exported struct{}
var unexported int
`)
	symbols, err := SearchSymbols(context.Background(), SymbolSearchInput{
		Root:  root,
		Query: "*",
	})
	if err != nil {
		t.Fatalf("SearchSymbols wildcard: %v", err)
	}
	if len(symbols) < 2 {
		t.Fatalf("expected at least 2 symbols, got %d", len(symbols))
	}
	// Exported struct should come first: visibility 30 + kind 25 = 55
	// vs unexported var: visibility 10 + kind 5 = 15
	if symbols[0].Name != "Exported" {
		t.Errorf("expected 'Exported' first (higher visibility+kind), got %q", symbols[0].Name)
	}
}

// TestSearchSymbols_RankedOrder verifies that fusion ranking promotes symbols from
// structurally important files. zz_util/ defines Process (alphabetically last) and
// aa_main/ calls it — with limit=1 we must get the definition, not the caller.
func TestSearchSymbols_RankedOrder(t *testing.T) {
	root := t.TempDir()

	// zz_util is alphabetically last but defines Process — should rank first by exact match.
	writeFile(t, filepath.Join(root, "zz_util", "util.go"), `package zz_util

// Process does the main work.
func Process(data string) string {
	return data + " processed"
}
`)
	// aa_main is alphabetically first and calls Process.
	writeFile(t, filepath.Join(root, "aa_main", "main.go"), `package aa_main

import "example.com/myapp/zz_util"

// Run calls Process from zz_util.
func Run(input string) string {
	return zz_util.Process(input)
}
`)
	writeFile(t, filepath.Join(root, "go.mod"), "module example.com/myapp\n\ngo 1.26\n")

	symbols, err := SearchSymbols(context.Background(), SymbolSearchInput{
		Root:  root,
		Query: "Process",
		Limit: 1,
	})
	if err != nil {
		t.Fatalf("SearchSymbols: %v", err)
	}
	if len(symbols) != 1 {
		t.Fatalf("expected 1 symbol, got %d", len(symbols))
	}
	if symbols[0].Name != "Process" {
		t.Errorf("expected symbol Process, got %q", symbols[0].Name)
	}
	// The result must come from zz_util (definition), not aa_main.
	if !strings.Contains(symbols[0].File, "zz_util") {
		t.Errorf("expected symbol from zz_util/, got file %q", symbols[0].File)
	}
}

// TestSearchSymbols_WildcardStillWorks verifies that wildcard "*" queries don't panic
// or return zero results when ranking gets empty queryTerms (extractQueryTerms("*") → []).
func TestSearchSymbols_WildcardStillWorks(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "pkg", "a.go"), `package pkg

func Alpha() {}
func Beta() {}
`)
	writeFile(t, filepath.Join(root, "pkg", "b.go"), `package pkg

func Gamma() {}
`)
	symbols, err := SearchSymbols(context.Background(), SymbolSearchInput{
		Root:  root,
		Query: "*",
	})
	if err != nil {
		t.Fatalf("SearchSymbols wildcard: %v", err)
	}
	if len(symbols) < 3 {
		t.Errorf("expected at least 3 symbols for wildcard, got %d", len(symbols))
	}
}

// TestSearchSymbols_EmptyQueryNoPanic verifies empty query doesn't panic during ranking.
func TestSearchSymbols_EmptyQueryNoPanic(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "x.go"), `package x

func Foo() {}
`)
	symbols, err := SearchSymbols(context.Background(), SymbolSearchInput{
		Root:  root,
		Query: "",
	})
	if err != nil {
		t.Fatalf("SearchSymbols empty query: %v", err)
	}
	if len(symbols) == 0 {
		t.Error("expected symbols for empty query (match-all), got none")
	}
}

// TestSearchSymbols_RankedMultipleMatches verifies that when the same symbol name
// exists in multiple files, the ranked file's symbols come first even with a larger limit.
func TestSearchSymbols_RankedMultipleMatches(t *testing.T) {
	root := t.TempDir()

	// 5 files each define "Handler" — only one imports "core" which defines "Handler" too.
	writeFile(t, filepath.Join(root, "core", "core.go"), `package core

// Handler is the core handler.
func Handler() string { return "core" }
`)
	writeFile(t, filepath.Join(root, "aa", "aa.go"), `package aa

// Handler in aa.
func Handler() string { return "aa" }
`)
	writeFile(t, filepath.Join(root, "bb", "bb.go"), `package bb

import "example.com/test/core"

// Handler in bb calls core.
func Handler() string { return core.Handler() }
`)
	writeFile(t, filepath.Join(root, "cc", "cc.go"), `package cc

// Handler in cc.
func Handler() string { return "cc" }
`)
	writeFile(t, filepath.Join(root, "dd", "dd.go"), `package dd

// Handler in dd.
func Handler() string { return "dd" }
`)
	writeFile(t, filepath.Join(root, "go.mod"), "module example.com/test\n\ngo 1.26\n")

	symbols, err := SearchSymbols(context.Background(), SymbolSearchInput{
		Root:  root,
		Query: "Handler",
		Limit: 3,
	})
	if err != nil {
		t.Fatalf("SearchSymbols: %v", err)
	}
	if len(symbols) != 3 {
		t.Fatalf("expected 3 symbols, got %d", len(symbols))
	}

	// core and bb should be in the top 3 (core defines it, bb references it).
	files := make(map[string]bool)
	for _, sym := range symbols {
		dir := filepath.Dir(sym.File)
		files[filepath.Base(dir)] = true
	}
	if !files["core"] {
		t.Errorf("core/core.go should be in top 3, got files: %v", files)
	}
	if !files["bb"] {
		t.Errorf("bb/bb.go should be in top 3 (imports core), got files: %v", files)
	}
}

// TestSearchSymbols_LimitExceedsTotal verifies that limit > total symbols is safe.
func TestSearchSymbols_LimitExceedsTotal(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "one.go"), `package one

func Only() {}
`)
	symbols, err := SearchSymbols(context.Background(), SymbolSearchInput{
		Root:  root,
		Query: "*",
		Limit: 500,
	})
	if err != nil {
		t.Fatalf("SearchSymbols: %v", err)
	}
	if len(symbols) == 0 {
		t.Error("expected at least 1 symbol")
	}
	if len(symbols) > 10 {
		t.Errorf("expected few symbols from single file, got %d", len(symbols))
	}
}

// TestSearchSymbols_KindFilterWithRanking verifies kind filter still works with ranking.
func TestSearchSymbols_KindFilterWithRanking(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "types.go"), `package types

type Server struct{}
func NewServer() *Server { return nil }
type Client struct{}
func NewClient() *Client { return nil }
`)
	symbols, err := SearchSymbols(context.Background(), SymbolSearchInput{
		Root:  root,
		Query: "*",
		Kind:  parser.KindStruct,
	})
	if err != nil {
		t.Fatalf("SearchSymbols: %v", err)
	}
	for _, sym := range symbols {
		if sym.Kind != parser.KindStruct {
			t.Errorf("expected only structs with kind filter, got %q (kind=%s)", sym.Name, sym.Kind)
		}
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

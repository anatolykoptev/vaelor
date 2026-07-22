package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/anatolykoptev/vaelor/internal/semhealth"
)

func TestLanguageOfFile(t *testing.T) {
	cases := map[string]string{
		"foo.go":        "go",
		"main.py":       "python",
		"api.ts":        "typescript",
		"cmp.tsx":       "typescript",
		"app.js":        "javascript",
		"lib.rs":        "rust",
		"Main.java":     "java",
		"main.c":        "c",
		"main.cpp":      "cpp",
		"script.rb":     "ruby",
		"Ctrl.cs":       "csharp",
		"index.php":     "php",
		"unknown.xyz":   "",
		"noext":         "",
		"COMPONENT.VUE": "vue",
	}
	for file, want := range cases {
		if got := languageOfFile(file); got != want {
			t.Errorf("languageOfFile(%q)=%q want %q", file, got, want)
		}
	}
}

func TestNormalizePathPrefix(t *testing.T) {
	cases := map[string]string{
		"internal/query":   "internal/query/",
		"/internal/query":  "internal/query/",
		"./internal/query": "internal/query/",
		"internal/query/":  "internal/query/",
		"":                 "",
		".":                "",
		"/":                "",
	}
	for in, want := range cases {
		if got := normalizePathPrefix(in); got != want {
			t.Errorf("normalizePathPrefix(%q)=%q want %q", in, got, want)
		}
	}
}

func TestPathHasPrefix(t *testing.T) {
	prefix := "internal/query/"
	if !pathHasPrefix("internal/query/ranking.go", prefix) {
		t.Errorf("file under prefix should match")
	}
	if pathHasPrefix("internal/parser/node.go", prefix) {
		t.Errorf("file outside prefix should not match")
	}
	if !pathHasPrefix("internal/query", prefix) {
		t.Errorf("exact dir (no trailing slash) should match its own prefix")
	}
}

func TestFilterDupGroups_Threshold(t *testing.T) {
	groups := []semhealth.DupGroup{
		{Tier: dupTierVeryClose, AvgSimilarity: 0.90, Symbols: []semhealth.DupSymbol{{File: "a.go", Line: 1}}},
		{Tier: dupTierRelated, AvgSimilarity: 0.82, Symbols: []semhealth.DupSymbol{{File: "a.go", Line: 1}}},
		{Tier: dupTierRelated, AvgSimilarity: 0.80, Symbols: []semhealth.DupSymbol{{File: "a.go", Line: 1}}},
	}
	// threshold 0.85: keeps 0.90 (very-close) and 0.82? no — 0.82 < 0.85 dropped.
	got := filterDupGroups(groups, dupFilterOpts{Threshold: 0.85})
	if len(got) != 1 {
		t.Fatalf("threshold 0.85: expected 1 group, got %d", len(got))
	}
	if got[0].AvgSimilarity != 0.90 {
		t.Errorf("kept group should be the 0.90 one, got %v", got[0].AvgSimilarity)
	}
}

func TestFilterDupGroups_ExactTierAlwaysPassesThreshold(t *testing.T) {
	groups := []semhealth.DupGroup{
		{Tier: dupTierExact, AvgSimilarity: 1.0, Symbols: []semhealth.DupSymbol{{File: "a.go", Line: 1}}},
	}
	got := filterDupGroups(groups, dupFilterOpts{Threshold: 0.99})
	if len(got) != 1 {
		t.Errorf("exact tier must always pass threshold, got %d", len(got))
	}
}

func TestFilterDupGroups_Language(t *testing.T) {
	groups := []semhealth.DupGroup{
		{Tier: dupTierExact, AvgSimilarity: 1.0, Symbols: []semhealth.DupSymbol{
			{File: "a.go", Line: 1}, {File: "b.go", Line: 1},
		}},
		{Tier: dupTierExact, AvgSimilarity: 1.0, Symbols: []semhealth.DupSymbol{
			{File: "a.py", Line: 1}, {File: "b.py", Line: 1},
		}},
		{Tier: dupTierExact, AvgSimilarity: 1.0, Symbols: []semhealth.DupSymbol{
			{File: "a.go", Line: 1}, {File: "b.py", Line: 1}, // mixed → dropped
		}},
	}
	got := filterDupGroups(groups, dupFilterOpts{Language: "go"})
	if len(got) != 1 {
		t.Fatalf("language=go: expected 1 group, got %d", len(got))
	}
	if got[0].Symbols[0].File != "a.go" {
		t.Errorf("kept group should be the go one, got %v", got[0].Symbols[0].File)
	}
}

func TestFilterDupGroups_Path(t *testing.T) {
	groups := []semhealth.DupGroup{
		{Tier: dupTierExact, AvgSimilarity: 1.0, Symbols: []semhealth.DupSymbol{
			{File: "internal/query/a.go", Line: 1}, {File: "internal/query/b.go", Line: 1},
		}},
		{Tier: dupTierExact, AvgSimilarity: 1.0, Symbols: []semhealth.DupSymbol{
			{File: "internal/parser/a.go", Line: 1}, {File: "internal/parser/b.go", Line: 1},
		}},
	}
	got := filterDupGroups(groups, dupFilterOpts{Path: "internal/query"})
	if len(got) != 1 {
		t.Fatalf("path=internal/query: expected 1 group, got %d", len(got))
	}
	if got[0].Symbols[0].File != "internal/query/a.go" {
		t.Errorf("kept group should be under internal/query, got %v", got[0].Symbols[0].File)
	}
}

func TestFilterDupGroups_NoFiltersReturnsAll(t *testing.T) {
	groups := []semhealth.DupGroup{
		{Tier: dupTierExact, AvgSimilarity: 1.0, Symbols: []semhealth.DupSymbol{{File: "a.go", Line: 1}}},
	}
	got := filterDupGroups(groups, dupFilterOpts{})
	if len(got) != 1 {
		t.Errorf("no filters should return all groups, got %d", len(got))
	}
}

func TestSymbolLineCount_Braced(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "a.go")
	body := `package a

func foo() {
	if true {
		return 1
	}
	bar()
}

var x = 1
`
	if err := os.WriteFile(file, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	// foo starts at line 3; body spans lines 3-8 (closing brace at line 8).
	n := symbolLineCount(dir, "a.go", 3)
	if n < 5 || n > 7 {
		t.Errorf("foo body line count: expected ~5-7, got %d", n)
	}
}

func TestSymbolLineCount_NoFile(t *testing.T) {
	if n := symbolLineCount(t.TempDir(), "missing.go", 1); n != 0 {
		t.Errorf("missing file should return 0, got %d", n)
	}
}

func TestFilterDupGroups_MinLines(t *testing.T) {
	dir := t.TempDir()
	// short.go: 2-line function. long.go: 6-line function.
	short := "package a\nfunc s() {\n}\n"
	long := "package a\nfunc l() {\n\ta\n\tb\n\tc\n\td\n}\n"
	if err := os.WriteFile(filepath.Join(dir, "short.go"), []byte(short), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "long.go"), []byte(long), 0o644); err != nil {
		t.Fatal(err)
	}
	groups := []semhealth.DupGroup{
		{Tier: dupTierExact, AvgSimilarity: 1.0, Symbols: []semhealth.DupSymbol{{File: "short.go", Line: 2}}},
		{Tier: dupTierExact, AvgSimilarity: 1.0, Symbols: []semhealth.DupSymbol{{File: "long.go", Line: 2}}},
	}
	got := filterDupGroups(groups, dupFilterOpts{MinLines: 4, Root: dir})
	if len(got) != 1 {
		t.Fatalf("min_lines=4: expected 1 group (long only), got %d", len(got))
	}
	if got[0].Symbols[0].File != "long.go" {
		t.Errorf("min_lines=4 should keep the long symbol only, got %v", got[0].Symbols[0].File)
	}
}

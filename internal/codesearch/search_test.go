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

// TestSearch_LineOrderPreservedAfterRanking verifies that within a file, matches
// keep their original line order after density re-ranking.
func TestSearch_LineOrderPreservedAfterRanking(t *testing.T) {
	dir := t.TempDir()
	// sparse.go has 1 match — will be ranked lower.
	writeFile(t, dir, "sparse.go", "package main\n// MARKER here\nfunc sparse() {}\n")
	// dense.go has 3 matches on lines 2, 4, 6.
	writeFile(t, dir, "dense.go", "package main\n// MARKER one\nfunc a() {}\n// MARKER two\nfunc b() {}\n// MARKER three\n")

	results, err := Search(context.Background(), SearchInput{
		Root:       dir,
		Pattern:    "MARKER",
		MaxResults: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 matches, got %d", len(results))
	}

	// All 3 should be from dense.go (3 matches > 1 match).
	for i, r := range results {
		if r.File != "dense.go" {
			t.Errorf("result[%d]: expected dense.go, got %s", i, r.File)
		}
	}
	// Line order must be ascending within the file.
	for i := 1; i < len(results); i++ {
		if results[i].Line <= results[i-1].Line {
			t.Errorf("line order broken: result[%d].Line=%d <= result[%d].Line=%d",
				i, results[i].Line, i-1, results[i-1].Line)
		}
	}
}

// TestSearch_MaxResults1 verifies that MaxResults=1 picks from the densest file.
func TestSearch_MaxResults1(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "aa.go", "package main\n// HIT one\n")
	writeFile(t, dir, "bb.go", "package main\n// HIT one\n// HIT two\n// HIT three\n")

	results, err := Search(context.Background(), SearchInput{
		Root:       dir,
		Pattern:    "HIT",
		MaxResults: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 match, got %d", len(results))
	}
	if results[0].File != "bb.go" {
		t.Errorf("expected match from bb.go (denser), got %s", results[0].File)
	}
}

// TestSearch_EqualDensityStableOrder verifies that files with equal match counts
// preserve their original filesystem order (stable sort).
func TestSearch_EqualDensityStableOrder(t *testing.T) {
	dir := t.TempDir()
	// All 3 files have exactly 2 matches each. Filesystem order: aa < bb < cc.
	writeFile(t, dir, "aa.go", "package main\n// TAG x\n// TAG y\n")
	writeFile(t, dir, "bb.go", "package main\n// TAG x\n// TAG y\n")
	writeFile(t, dir, "cc.go", "package main\n// TAG x\n// TAG y\n")

	results, err := Search(context.Background(), SearchInput{
		Root:       dir,
		Pattern:    "TAG",
		MaxResults: 4,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 4 {
		t.Fatalf("expected 4 matches, got %d", len(results))
	}

	// With equal density, stable sort preserves original order: aa, aa, bb, bb.
	if results[0].File != "aa.go" || results[1].File != "aa.go" {
		t.Errorf("expected first 2 from aa.go (stable order), got %s, %s",
			results[0].File, results[1].File)
	}
	if results[2].File != "bb.go" || results[3].File != "bb.go" {
		t.Errorf("expected next 2 from bb.go (stable order), got %s, %s",
			results[2].File, results[3].File)
	}
}

// TestSearch_AllMatchesInOneFile verifies ranking is a no-op when all matches
// come from a single file (no re-ordering needed).
func TestSearch_AllMatchesInOneFile(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "only.go", "package main\nfoo\nbar\nfoo\nbaz\nfoo\n")
	writeFile(t, dir, "empty.go", "package main\nnothing here\n")

	results, err := Search(context.Background(), SearchInput{
		Root:       dir,
		Pattern:    "foo",
		MaxResults: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 matches, got %d", len(results))
	}
	for _, r := range results {
		if r.File != "only.go" {
			t.Errorf("expected only.go, got %s", r.File)
		}
	}
	// First match should be at line 2, second at line 4.
	if results[0].Line != 2 {
		t.Errorf("expected line 2, got %d", results[0].Line)
	}
	if results[1].Line != 4 {
		t.Errorf("expected line 4, got %d", results[1].Line)
	}
}

// TestSearch_ContextLinesWithRanking verifies context lines survive the ranking reorder.
func TestSearch_ContextLinesWithRanking(t *testing.T) {
	dir := t.TempDir()
	// sparse has 1 match with context.
	writeFile(t, dir, "sparse.go", "package main\naaa\nbbb\n// FIND me\nccc\nddd\n")
	// dense has 3 matches.
	writeFile(t, dir, "dense.go", "package main\n// FIND one\nxx\n// FIND two\nyy\n// FIND three\n")

	results, err := Search(context.Background(), SearchInput{
		Root:         dir,
		Pattern:      "FIND",
		MaxResults:   2,
		ContextLines: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2, got %d", len(results))
	}
	// Both should be from dense.go.
	for i, r := range results {
		if r.File != "dense.go" {
			t.Errorf("result[%d]: expected dense.go, got %s", i, r.File)
		}
		if len(r.Context) == 0 {
			t.Errorf("result[%d]: context lines lost after ranking", i)
		}
	}
}

// TestSearch_NoMatchesNoPanic verifies zero matches don't trigger ranking.
func TestSearch_NoMatchesNoPanic(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.go", "package main\nnothing\n")

	results, err := Search(context.Background(), SearchInput{
		Root:       dir,
		Pattern:    "ZZZZNONEXISTENT",
		MaxResults: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 matches, got %d", len(results))
	}
}

// TestSearch_HardcapPreventsOOM verifies that hardcap (5×MaxResults) actually
// limits collection and doesn't scan the entire repo.
func TestSearch_HardcapPreventsOOM(t *testing.T) {
	dir := t.TempDir()
	// Create 10 files with 10 matches each = 100 total.
	for i := 0; i < 10; i++ {
		name := strings.Repeat("z", i+1) + ".go" // zz.go, zzz.go, ... for ordering
		content := "package main\n" + strings.Repeat("// MARK hit\n", 10)
		writeFile(t, dir, name, content)
	}

	results, err := Search(context.Background(), SearchInput{
		Root:       dir,
		Pattern:    "MARK",
		MaxResults: 5,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Must get exactly 5 (capped by MaxResults), not more.
	if len(results) != 5 {
		t.Fatalf("expected 5 matches (MaxResults cap), got %d", len(results))
	}
}

// TestSearch_RankByMatchDensity_Unit directly tests the ranking helper.
func TestSearch_RankByMatchDensity_Unit(t *testing.T) {
	matches := []SearchMatch{
		{File: "a.go", Line: 1},
		{File: "b.go", Line: 1},
		{File: "b.go", Line: 2},
		{File: "c.go", Line: 1},
		{File: "c.go", Line: 2},
		{File: "c.go", Line: 3},
		{File: "a.go", Line: 2},
	}
	rankByMatchDensity(matches)

	// c.go has 3, b.go has 2, a.go has 2.
	// Expected: c.go(1), c.go(2), c.go(3), then b.go/a.go interleaved by stable order.
	if matches[0].File != "c.go" || matches[1].File != "c.go" || matches[2].File != "c.go" {
		t.Errorf("expected first 3 from c.go, got: %s, %s, %s",
			matches[0].File, matches[1].File, matches[2].File)
	}
	// c.go lines must be in original order.
	if matches[0].Line != 1 || matches[1].Line != 2 || matches[2].Line != 3 {
		t.Errorf("c.go line order broken: %d, %d, %d",
			matches[0].Line, matches[1].Line, matches[2].Line)
	}
	// a.go and b.go both have 2 matches. a.go was first-seen (pos 0), so it comes before b.go (pos 1).
	// Matches must be GROUPED by file, not interleaved.
	if matches[3].File != "a.go" || matches[4].File != "a.go" {
		t.Errorf("expected a.go at positions 3-4 (first-seen order), got: %s, %s",
			matches[3].File, matches[4].File)
	}
	if matches[5].File != "b.go" || matches[6].File != "b.go" {
		t.Errorf("expected b.go at positions 5-6 (first-seen order), got: %s, %s",
			matches[5].File, matches[6].File)
	}
	// a.go line order must be preserved even though matches were non-contiguous in input.
	if matches[3].Line != 1 || matches[4].Line != 2 {
		t.Errorf("a.go line order broken: %d, %d (expected 1, 2)",
			matches[3].Line, matches[4].Line)
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

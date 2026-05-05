package main

import (
	"strings"
	"testing"

	"github.com/anatolykoptev/go-code/internal/analyze"
	"github.com/anatolykoptev/go-code/internal/parser"
)

// fixture builds a small RepoAnalysisResult that exercises all branches we
// changed: a struct symbol (trivial signature, must be dropped), a function
// symbol (real signature, must be kept), and one file containing them.
func fixtureResult() *analyze.RepoAnalysisResult {
	structSym := &parser.Symbol{
		Kind:      "struct",
		Name:      "User",
		File:      "user.go",
		StartLine: 10,
		EndLine:   15,
		Signature: "type User struct",
	}
	funcSym := &parser.Symbol{
		Kind:      "function",
		Name:      "Login",
		File:      "user.go",
		StartLine: 20,
		EndLine:   30,
		Signature: "func Login(ctx context.Context, name string) error",
	}
	return &analyze.RepoAnalysisResult{
		RepoName:  "demo",
		Language:  "go",
		FileCount: 1,
		Packages:  []string{"demo"},
		Symbols:   []*parser.Symbol{structSym, funcSym},
		Files: []analyze.AnalyzedFile{
			{
				RelPath:   "user.go",
				Language:  "go",
				Size:      120,
				Lines:     30,
				Relevance: 0.5,
				Symbols:   []*parser.Symbol{structSym, funcSym},
			},
		},
	}
}

// TestFormatAnalysisXML_NoDuplicateSymbols asserts the top-level <symbols>
// section is omitted when <files> is present (module/deep modes). This is the
// largest single source of bloat — the same symbols are otherwise listed twice.
func TestFormatAnalysisXML_NoDuplicateSymbols(t *testing.T) {
	out := formatAnalysisXML(fixtureResult(), analyze.DepthModule, nil)
	if strings.Contains(out, "<symbols>") || strings.Contains(out, "<symbols ") {
		t.Fatalf("module depth must not emit top-level <symbols> when <files> is present:\n%s", out)
	}
	if !strings.Contains(out, `<file path="user.go"`) {
		t.Fatalf("expected file entry in output, got:\n%s", out)
	}
}

// TestFormatAnalysisXML_OverviewKeepsTopSymbols asserts overview depth (which
// has no <files> section) still emits top-level <symbols> — agents calling
// overview rely on it as the only symbol view.
func TestFormatAnalysisXML_OverviewKeepsTopSymbols(t *testing.T) {
	out := formatAnalysisXML(fixtureResult(), analyze.DepthOverview, nil)
	if !strings.Contains(out, "<symbols>") {
		t.Fatalf("overview depth must keep top-level <symbols> (no <files> fallback):\n%s", out)
	}
}

// TestFormatAnalysisXML_DropsTrivialSignature asserts signatures that merely
// restate kind+name (e.g. "type User struct") are not emitted, while real
// function signatures are.
func TestFormatAnalysisXML_DropsTrivialSignature(t *testing.T) {
	out := formatAnalysisXML(fixtureResult(), analyze.DepthModule, nil)
	if strings.Contains(out, "type User struct") {
		t.Fatalf("trivial struct signature should be dropped:\n%s", out)
	}
	if !strings.Contains(out, "func Login(ctx context.Context, name string) error") {
		t.Fatalf("real function signature must be kept:\n%s", out)
	}
}

// TestFormatAnalysisXML_OmitsUnavailableArchCentral asserts an arch_central
// section with Available=false is no longer emitted — agents read absence
// as "no graph snapshot" without needing the empty tag.
func TestFormatAnalysisXML_OmitsUnavailableArchCentral(t *testing.T) {
	extras := &repoAnalysisExtras{} // no ArchCentral set
	out := formatAnalysisXML(fixtureResult(), analyze.DepthModule, extras)
	if strings.Contains(out, "arch_central") {
		t.Fatalf("unavailable arch_central must be omitted:\n%s", out)
	}
}

// TestFormatAnalysisXML_SizeRegression is a soft size budget for the fixture.
// Pre-slim output for this fixture was ~1100 bytes; post-slim is ~700.
// The bound is loose to avoid flakiness while still catching regressions
// like accidental restoration of the duplicate <symbols> block.
func TestFormatAnalysisXML_SizeRegression(t *testing.T) {
	out := formatAnalysisXML(fixtureResult(), analyze.DepthModule, nil)
	if len(out) > 900 {
		t.Fatalf("module-depth fixture exceeded slim budget: got %d bytes\n%s", len(out), out)
	}
}

// cmd/go-code/tool_debug_investigate_body_test.go
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anatolykoptev/go-code/internal/analyze"
	"github.com/anatolykoptev/go-code/internal/investigate"
)

// ---------- extractBodySource ----------

// TestExtractBodySource_HappyPath: write tempfile with 50 lines, request lines 10-20.
func TestExtractBodySource_HappyPath(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "body*.go")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	for i := 1; i <= 50; i++ {
		fmt.Fprintf(f, "line %d\n", i)
	}
	f.Close()

	got, err := extractBodySource(f.Name(), 10, 20, 200)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	lines := strings.Split(strings.TrimRight(got, "\n"), "\n")
	if len(lines) != 11 {
		t.Fatalf("expected 11 lines (10..20 inclusive), got %d: %q", len(lines), got)
	}
	if !strings.Contains(lines[0], "line 10") {
		t.Errorf("first line should be 'line 10', got %q", lines[0])
	}
	if !strings.Contains(lines[10], "line 20") {
		t.Errorf("last line should be 'line 20', got %q", lines[10])
	}
}

// TestExtractBodySource_LineOutOfRange_TrimToFile: request 10-100 from 30-line file.
func TestExtractBodySource_LineOutOfRange_TrimToFile(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "body*.go")
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 30; i++ {
		fmt.Fprintf(f, "line %d\n", i)
	}
	f.Close()

	got, err := extractBodySource(f.Name(), 10, 100, 200)
	if err != nil {
		t.Fatalf("should trim gracefully, not error: %v", err)
	}
	lines := strings.Split(strings.TrimRight(got, "\n"), "\n")
	if len(lines) != 21 { // 10..30 inclusive
		t.Fatalf("expected 21 lines (10..30 trimmed), got %d", len(lines))
	}
	if !strings.Contains(lines[0], "line 10") {
		t.Errorf("first line should be 'line 10', got %q", lines[0])
	}
}

// TestExtractBodySource_FileNotFound_ReturnsError: non-existent file.
func TestExtractBodySource_FileNotFound_ReturnsError(t *testing.T) {
	got, err := extractBodySource("/nonexistent/file.go", 1, 10, 200)
	if err == nil {
		t.Fatal("expected error for non-existent file, got nil")
	}
	if got != "" {
		t.Errorf("expected empty string on error, got %q", got)
	}
}

// TestExtractBodySource_CapMaxLines: 500-line file, request 1-500 with maxLines=50.
func TestExtractBodySource_CapMaxLines(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "body*.go")
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 500; i++ {
		fmt.Fprintf(f, "line %d\n", i)
	}
	f.Close()

	got, err := extractBodySource(f.Name(), 1, 500, 50)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	lines := strings.Split(strings.TrimRight(got, "\n"), "\n")
	// 50 lines + possibly a truncation indicator line = up to 51
	if len(lines) < 50 || len(lines) > 52 {
		t.Fatalf("expected ~50 lines (capped), got %d", len(lines))
	}
	// Truncation indicator should be present
	if !strings.Contains(got, "...") {
		t.Errorf("expected truncation indicator '...' in output, got:\n%s", got)
	}
}

// TestExtractBodySource_LargeFile_FailsClean: 2 MiB file → reject.
func TestExtractBodySource_LargeFile_FailsClean(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "large*.go")
	if err != nil {
		t.Fatal(err)
	}
	// Write 2 MiB of data (1024*1024*2 bytes)
	chunk := make([]byte, 4096)
	for i := range chunk {
		chunk[i] = 'x'
	}
	chunk[4095] = '\n'
	for written := 0; written < 2*1024*1024; written += len(chunk) {
		f.Write(chunk)
	}
	f.Close()

	got, err := extractBodySource(f.Name(), 1, 10, 200)
	if err == nil {
		t.Fatal("expected error for oversized file, got nil")
	}
	if got != "" {
		t.Errorf("expected empty string on error, got %q (len=%d)", got[:min(len(got), 50)], len(got))
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// ---------- runBodyExtractionPhase ----------

// TestRunBodyExtractionPhase_Top3Only: 5 hypotheses, only top-3 get body.
func TestRunBodyExtractionPhase_Top3Only(t *testing.T) {
	dir := t.TempDir()
	// Create 5 source files
	for i := 1; i <= 5; i++ {
		p := filepath.Join(dir, fmt.Sprintf("f%d.go", i))
		if err := os.WriteFile(p, []byte("package main\nfunc Fn() {}\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	hyps := make([]investigate.Hypothesis, 5)
	for i := 0; i < 5; i++ {
		hyps[i] = investigate.Hypothesis{
			Subject: fmt.Sprintf("Fn%d", i+1),
			File:    filepath.Join(dir, fmt.Sprintf("f%d.go", i+1)),
			Line:    1,
			EndLine: 3,
		}
	}

	result := runBodyExtractionPhase(hyps, 3, nil)

	for i := 0; i < 3; i++ {
		if result[i].BodySource == "" {
			t.Errorf("hypothesis[%d] (top-3) should have BodySource, got empty", i)
		}
	}
	for i := 3; i < 5; i++ {
		if result[i].BodySource != "" {
			t.Errorf("hypothesis[%d] (beyond top-3) should have empty BodySource, got %q", i, result[i].BodySource)
		}
	}
}

// TestRunBodyExtractionPhase_EmptyFile_NoCrash: hypothesis with File="" → skipped.
func TestRunBodyExtractionPhase_EmptyFile_NoCrash(t *testing.T) {
	hyps := []investigate.Hypothesis{
		{Subject: "NoFile", File: "", Line: 1, EndLine: 5},
	}

	// Should not panic or error
	result := runBodyExtractionPhase(hyps, 3, nil)

	if len(result) != 1 {
		t.Fatalf("expected 1 hypothesis, got %d", len(result))
	}
	if result[0].BodySource != "" {
		t.Errorf("expected empty BodySource for no-file hypothesis, got %q", result[0].BodySource)
	}
}

// ---------- LLM payload ----------

// TestLLMPayload_IncludesBodyExcerpts: hypothesis with body → prompt contains body_excerpts.
func TestLLMPayload_IncludesBodyExcerpts(t *testing.T) {
	hyps := []investigate.Hypothesis{
		{
			Subject:    "TestFn in test.go",
			File:       "test.go",
			Line:       10,
			EndLine:    15,
			SpanCount:  5,
			BodySource: "func TestFn() {\n    // some code\n}\n",
		},
	}

	excerpts := collectBodyExcerpts(hyps)
	if len(excerpts) == 0 {
		t.Fatal("expected at least one body excerpt")
	}
	first := excerpts[0]
	if first["file"] != "test.go" {
		t.Errorf("expected file=test.go, got %v", first["file"])
	}
	src, ok := first["source"].(string)
	if !ok || src == "" {
		t.Errorf("expected non-empty source in excerpt, got %v", first["source"])
	}
	if !strings.Contains(src, "TestFn") {
		t.Errorf("source should contain function body, got %q", src)
	}
}

// ---------- format ----------

// TestFormat_BodyExcerpt_RendersInXML: hypothesis with body → XML contains <body_excerpt>.
func TestFormat_BodyExcerpt_RendersInXML(t *testing.T) {
	r := &investigate.InvestigationResult{
		Service: "test-svc",
		Hypotheses: []investigate.Hypothesis{
			{
				Subject:    "Fn1 in test.go",
				File:       "test.go",
				Line:       10,
				EndLine:    15,
				BodySource: "func Fn1() {\n    panic(\"oops\")\n}",
			},
		},
	}

	out := formatInvestigationResult(r)
	if !strings.Contains(out, "<body_excerpt") {
		t.Errorf("expected <body_excerpt> in XML output, got:\n%s", out)
	}
	if !strings.Contains(out, "Fn1") {
		t.Errorf("expected body content 'Fn1' in CDATA, got:\n%s", out)
	}
}

// ---------- runBodyExtractionPhaseWithMappings ----------

// TestRunBodyExtractionPhaseWithMappings_AppliesPathMapping: hypothesis with host path
// → mapping rewrites to tempfile path → BodySource populated.
func TestRunBodyExtractionPhaseWithMappings_AppliesPathMapping(t *testing.T) {
	dir := t.TempDir()

	// Write a 30-line tempfile at the "container" path (dir/foo.go).
	lines := make([]byte, 0, 1024)
	for i := 1; i <= 30; i++ {
		lines = append(lines, []byte(fmt.Sprintf("line %d\n", i))...)
	}
	containerPath := filepath.Join(dir, "foo.go")
	if err := os.WriteFile(containerPath, lines, 0644); err != nil {
		t.Fatal(err)
	}

	// Hypothesis uses the "host" path (External prefix).
	hostPrefix := "/host/src/testXXX"
	hostFile := hostPrefix + "/foo.go"
	hyps := []investigate.Hypothesis{
		{Subject: "TestFn", File: hostFile, Line: 1, EndLine: 10},
	}

	mappings := []analyze.PathMapping{
		{External: hostPrefix, Internal: dir},
	}

	var diags investigate.Diagnostics
	result := runBodyExtractionPhaseWithMappings(hyps, 1, "", mappings, &diags)

	if result[0].BodySource == "" {
		t.Errorf("expected BodySource to be populated via path mapping, got empty")
	}
	if len(diags.Warnings) != 0 {
		t.Errorf("expected no warnings, got: %v", diags.Warnings)
	}
}

// TestRunBodyExtractionPhaseWithMappings_AppendsWarningOnError: nonexistent host path
// → BodySource empty, warning appended to diagnostics.
func TestRunBodyExtractionPhaseWithMappings_AppendsWarningOnError(t *testing.T) {
	hyps := []investigate.Hypothesis{
		{Subject: "BadFn", File: "/nonexistent/path/bad.go", Line: 1, EndLine: 5},
	}

	var diags investigate.Diagnostics
	result := runBodyExtractionPhaseWithMappings(hyps, 1, "", nil, &diags)

	if result[0].BodySource != "" {
		t.Errorf("expected empty BodySource on error, got %q", result[0].BodySource)
	}
	if len(diags.Warnings) == 0 {
		t.Error("expected at least one warning in diagnostics, got none")
	}
}

func TestBuildBodyPathCandidates_RelativePathPrefersHostMount(t *testing.T) {
	mappings := []analyze.PathMapping{{External: "/host", Internal: "/host"}}
	got := buildBodyPathCandidates("crates/server/src/main.rs", "", mappings)
	// Must include /host/<relative> candidate
	found := false
	for _, p := range got {
		if p == "/host/crates/server/src/main.rs" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected /host/<relative> in candidates, got %v", got)
	}
}

func TestBuildBodyPathCandidates_AbsoluteUnchanged(t *testing.T) {
	got := buildBodyPathCandidates("/abs/path/foo.rs", "", nil)
	if len(got) != 1 || got[0] != "/abs/path/foo.rs" {
		t.Fatalf("expected single absolute candidate, got %v", got)
	}
}

func TestResolveEndLine_KnownEndLine_Returns(t *testing.T) {
	h := &investigate.Hypothesis{Line: 10, EndLine: 50}
	if got := resolveEndLine(h); got != 50 {
		t.Fatalf("want 50, got %d", got)
	}
}

func TestResolveEndLine_ZeroEndLine_AddsWindow(t *testing.T) {
	// Sprint B3: when EndLine is unknown (Tier-1 OTEL code.*), expand
	// to defaultBodyWindow lines so body excerpt covers the function
	// body, not just the annotation line.
	h := &investigate.Hypothesis{Line: 100, EndLine: 0}
	if got := resolveEndLine(h); got != 100+defaultBodyWindow {
		t.Fatalf("want %d, got %d", 100+defaultBodyWindow, got)
	}
}

func TestRunBodyExtractionPhase_OTELLineOnly_ReadsHandlerBody(t *testing.T) {
	// Simulate Rust handler file: line 1 is annotation, lines 2-10 are body.
	dir := t.TempDir()
	file := filepath.Join(dir, "handler.rs")
	content := "#[tracing::instrument(name = \"foo\", skip_all)]\nfn handle() {\n    let x = 1;\n    let y = 2;\n    return x + y;\n}\n"
	if err := os.WriteFile(file, []byte(content), 0644); err != nil { t.Fatal(err) }
	hyps := []investigate.Hypothesis{{File: file, Line: 1, EndLine: 0}}
	out := runBodyExtractionPhase(hyps, 1, nil)
	if out[0].BodySource == "" {
		t.Fatal("expected body extracted, got empty")
	}
	if !strings.Contains(out[0].BodySource, "fn handle()") {
		t.Fatalf("expected handler body, got: %q", out[0].BodySource)
	}
	if !strings.Contains(out[0].BodySource, "return x + y") {
		t.Fatalf("expected full body inc. return, got: %q", out[0].BodySource)
	}
}

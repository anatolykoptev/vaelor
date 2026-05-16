package codegraph

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/anatolykoptev/go-code/internal/ingest"
)

// makeTestFile creates a real temp file with the given content and returns an
// ingest.File pointing to it.
func makeTestFile(t *testing.T, dir, name, content string) *ingest.File {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile %s: %v", name, err)
	}
	return &ingest.File{
		Path:     path,
		RelPath:  name,
		Language: "go",
	}
}

// TestIndexParseFile_ReadErrorMissing asserts that when the file does not exist,
// indexParseFile returns skipReason == skipReasonReadMissing ("read_error_missing").
func TestIndexParseFile_ReadErrorMissing(t *testing.T) {
	dir := t.TempDir()
	f := &ingest.File{
		Path:     filepath.Join(dir, "nonexistent.go"),
		RelPath:  "nonexistent.go",
		Language: "go",
	}

	result := indexParseFile(dir, f)
	if result.skipReason != skipReasonReadMissing {
		t.Errorf("expected skipReason %q, got %q", skipReasonReadMissing, result.skipReason)
	}
	if result.file != nil {
		t.Error("expected file == nil on read error")
	}
}

// TestIndexParseFile_ReadErrorPerm asserts that when the file exists but is
// unreadable, indexParseFile returns skipReason == skipReasonReadPerm.
// Skipped when running as root (root ignores permissions).
func TestIndexParseFile_ReadErrorPerm(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("running as root — file permission tricks don't apply")
	}

	dir := t.TempDir()
	f := makeTestFile(t, dir, "locked.go", "package main\n")
	// Remove read permission.
	if err := os.Chmod(f.Path, 0o000); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(f.Path, 0o644) })

	result := indexParseFile(dir, f)
	if result.skipReason != skipReasonReadPerm {
		t.Errorf("expected skipReason %q, got %q", skipReasonReadPerm, result.skipReason)
	}
	if result.file != nil {
		t.Error("expected file == nil on read error")
	}
}

// TestIndexParseFile_SuccessfulParse asserts the happy path: a valid Go file
// returns a non-nil file with no skip reason.
func TestIndexParseFile_SuccessfulParse(t *testing.T) {
	dir := t.TempDir()
	f := makeTestFile(t, dir, "main.go", "package main\nfunc main() {}\n")

	result := indexParseFile(dir, f)
	if result.skipReason != "" {
		t.Errorf("unexpected skip reason %q for valid file", result.skipReason)
	}
	if result.file == nil {
		t.Error("expected non-nil file on successful parse")
	}
}

// TestClassifyReadError asserts that classifyReadError maps error types correctly.
func TestClassifyReadError(t *testing.T) {
	dir := t.TempDir()

	// ENOENT — read from nonexistent path.
	_, enoent := os.ReadFile(filepath.Join(dir, "missing.txt"))
	if got := classifyReadError(enoent); got != skipReasonReadMissing {
		t.Errorf("ENOENT: expected %q, got %q", skipReasonReadMissing, got)
	}

	// Other — use a directory path (ReadFile on a dir returns EISDIR on Linux,
	// which is neither ENOENT nor EACCES).
	_, eisdir := os.ReadFile(dir) // reading a directory
	if got := classifyReadError(eisdir); got != skipReasonReadOther {
		t.Errorf("EISDIR: expected %q, got %q", skipReasonReadOther, got)
	}
}

// TestIngestAndParse_ReadErrorMissingCountedSeparately asserts that the
// parseSkipped map produced by ingestAndParse counts ENOENT files under
// "read_error_missing" (not the old umbrella "read_error" key).
// We rely on indexParseParallel directly, passing a file that does not exist.
func TestIngestAndParse_ReadErrorMissingCountedSeparately(t *testing.T) {
	dir := t.TempDir()

	// Mix of: one existing file, one missing file.
	existing := makeTestFile(t, dir, "real.go", "package main\nfunc Foo() {}\n")
	missing := &ingest.File{
		Path:     filepath.Join(dir, "gone.go"),
		RelPath:  "gone.go",
		Language: "go",
	}

	results := indexParseParallel(t.Context(), dir, []*ingest.File{existing, missing})

	skipped := make(map[string]int)
	for _, r := range results {
		if r.skipReason != "" {
			skipped[r.skipReason]++
		}
	}

	if skipped[skipReasonReadMissing] != 1 {
		t.Errorf("expected 1 read_error_missing, got %d (skipped=%v)", skipped[skipReasonReadMissing], skipped)
	}
	if _, hasOld := skipped["read_error"]; hasOld {
		t.Errorf("old umbrella 'read_error' key must not appear; skipped=%v", skipped)
	}
}

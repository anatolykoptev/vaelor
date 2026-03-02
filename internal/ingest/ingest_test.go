package ingest

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeFile is a helper that creates parent directories and writes content.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// TestIngestRepo verifies end-to-end filesystem walking with filtering.
func TestIngestRepo(t *testing.T) {
	root := t.TempDir()

	// Regular source files.
	writeFile(t, filepath.Join(root, "main.go"), "package main\n")
	writeFile(t, filepath.Join(root, "app.py"), "print('hi')\n")
	writeFile(t, filepath.Join(root, "index.ts"), "const x = 1\n")

	// File inside a subdirectory.
	writeFile(t, filepath.Join(root, "internal", "handler.go"), "package internal\n")

	// Files that should be skipped.
	writeFile(t, filepath.Join(root, ".git", "HEAD"), "ref: refs/heads/main\n")
	writeFile(t, filepath.Join(root, "node_modules", "lib", "index.js"), "module.exports = {}\n")
	writeFile(t, filepath.Join(root, "logo.png"), "\x89PNG\r\n")
	writeFile(t, filepath.Join(root, "go.sum"), "github.com/foo v1.0.0 h1:abc\n")

	// .gitignore that ignores *.log and the secrets/ directory.
	writeFile(t, filepath.Join(root, ".gitignore"), "*.log\nsecrets/\n")
	writeFile(t, filepath.Join(root, "app.log"), "log entry\n")
	writeFile(t, filepath.Join(root, "secrets", "key.txt"), "s3cr3t\n")

	result, err := IngestRepo(context.Background(), IngestOpts{Root: root})
	if err != nil {
		t.Fatalf("IngestRepo: %v", err)
	}

	// Build a set of relative paths for easy assertion.
	relPaths := make(map[string]bool, len(result.Files))
	for _, f := range result.Files {
		relPaths[f.RelPath] = true
	}

	t.Run("accepted files present", func(t *testing.T) {
		want := []string{"main.go", "app.py", "index.ts", filepath.Join("internal", "handler.go")}
		for _, p := range want {
			if !relPaths[p] {
				t.Errorf("expected %q in result, not found; got %v", p, keys(relPaths))
			}
		}
	})

	t.Run("git dir skipped", func(t *testing.T) {
		for p := range relPaths {
			if strings.HasPrefix(p, ".git") {
				t.Errorf("unexpected .git file in result: %q", p)
			}
		}
	})

	t.Run("node_modules skipped", func(t *testing.T) {
		for p := range relPaths {
			if strings.HasPrefix(p, "node_modules") {
				t.Errorf("unexpected node_modules file in result: %q", p)
			}
		}
	})

	t.Run("binary extension skipped", func(t *testing.T) {
		if relPaths["logo.png"] {
			t.Error("logo.png (binary) should be skipped")
		}
	})

	t.Run("go.sum skipped", func(t *testing.T) {
		if relPaths["go.sum"] {
			t.Error("go.sum should be in ignoreFiles and skipped")
		}
	})

	t.Run("gitignore pattern *.log respected", func(t *testing.T) {
		if relPaths["app.log"] {
			t.Error("app.log matches *.log gitignore pattern, should be skipped")
		}
	})

	t.Run("gitignore directory pattern respected", func(t *testing.T) {
		for p := range relPaths {
			if strings.HasPrefix(p, "secrets") {
				t.Errorf("secrets/ matches gitignore dir pattern, should be skipped: %q", p)
			}
		}
	})

	t.Run("language detected", func(t *testing.T) {
		for _, f := range result.Files {
			if f.RelPath == "main.go" && f.Language != "go" {
				t.Errorf("main.go: expected language=go, got %q", f.Language)
			}
			if f.RelPath == "app.py" && f.Language != "python" {
				t.Errorf("app.py: expected language=python, got %q", f.Language)
			}
		}
	})

	t.Run("sizes recorded", func(t *testing.T) {
		for _, f := range result.Files {
			if f.Size <= 0 {
				t.Errorf("file %q has zero or negative size", f.RelPath)
			}
		}
	})

	t.Run("total bytes positive", func(t *testing.T) {
		if result.TotalBytes <= 0 {
			t.Errorf("TotalBytes should be positive, got %d", result.TotalBytes)
		}
	})

	t.Run("skipped count positive", func(t *testing.T) {
		if result.SkippedCount <= 0 {
			t.Errorf("expected some files skipped, got SkippedCount=%d", result.SkippedCount)
		}
	})
}

// TestIngestRepoLanguageFilter verifies the Languages filter works.
func TestIngestRepoLanguageFilter(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "main.go"), "package main\n")
	writeFile(t, filepath.Join(root, "app.py"), "print('hi')\n")
	writeFile(t, filepath.Join(root, "index.ts"), "const x = 1\n")

	result, err := IngestRepo(context.Background(), IngestOpts{
		Root:      root,
		Languages: []string{"go"},
	})
	if err != nil {
		t.Fatalf("IngestRepo: %v", err)
	}

	for _, f := range result.Files {
		if f.Language != "go" {
			t.Errorf("language filter: unexpected file %q with language=%q", f.RelPath, f.Language)
		}
	}
	if len(result.Files) != 1 {
		t.Errorf("expected 1 Go file, got %d", len(result.Files))
	}
}

// TestIngestRepoMaxFileBytes verifies large files are skipped and counted.
func TestIngestRepoMaxFileBytes(t *testing.T) {
	root := t.TempDir()
	// Small file — should be included.
	writeFile(t, filepath.Join(root, "small.go"), "package main\n")
	// Large file — should be skipped.
	writeFile(t, filepath.Join(root, "large.go"), strings.Repeat("x", 200))

	result, err := IngestRepo(context.Background(), IngestOpts{
		Root:         root,
		MaxFileBytes: 100,
	})
	if err != nil {
		t.Fatalf("IngestRepo: %v", err)
	}

	for _, f := range result.Files {
		if f.RelPath == "large.go" {
			t.Error("large.go should have been skipped due to MaxFileBytes")
		}
	}
	if result.SkippedCount == 0 {
		t.Error("expected SkippedCount > 0 for oversized file")
	}
}

// TestIngestRepoFocus verifies the Focus filter restricts files.
func TestIngestRepoFocus(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "main.go"), "package main\n")
	writeFile(t, filepath.Join(root, "internal", "handler.go"), "package internal\n")

	result, err := IngestRepo(context.Background(), IngestOpts{
		Root:  root,
		Focus: "internal",
	})
	if err != nil {
		t.Fatalf("IngestRepo: %v", err)
	}

	for _, f := range result.Files {
		if !strings.HasPrefix(f.RelPath, "internal") {
			t.Errorf("focus=internal: unexpected file %q outside focus", f.RelPath)
		}
	}
}

// TestIngestRepoContextCancel verifies context cancellation stops the walk.
func TestIngestRepoContextCancel(t *testing.T) {
	root := t.TempDir()
	for range 50 {
		writeFile(t, filepath.Join(root, "sub", "file"+strings.Repeat("x", 5)+".go"),
			"package sub\n")
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()

	// The call may or may not error — we just need it not to panic.
	_, _ = IngestRepo(ctx, IngestOpts{Root: root})
}

// TestShouldIgnoreDir verifies the default ignore directory list.
func TestShouldIgnoreDir(t *testing.T) {
	cases := []struct {
		name   string
		want   bool
	}{
		{".git", true},
		{"node_modules", true},
		{"vendor", true},
		{"__pycache__", true},
		{".venv", true},
		{"dist", true},
		{"target", true},
		{".cargo", true},
		{"testdata", true},
		{"src", false},
		{"internal", false},
		{"cmd", false},
		{"pkg", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := shouldIgnoreDir(tc.name); got != tc.want {
				t.Errorf("shouldIgnoreDir(%q) = %v, want %v", tc.name, got, tc.want)
			}
		})
	}
}

// TestIsBinaryData verifies null-byte detection.
func TestIsBinaryData(t *testing.T) {
	cases := []struct {
		name string
		data []byte
		want bool
	}{
		{"empty", []byte{}, false},
		{"ascii text", []byte("package main\nfunc main() {}\n"), false},
		{"null byte at start", []byte{0x00, 0x41, 0x42}, true},
		{"null byte in middle", []byte("hello\x00world"), true},
		{"png header", []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}, false},
		{"binary exe prefix", []byte{0x4d, 0x5a, 0x00, 0x00}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isBinaryData(tc.data); got != tc.want {
				t.Errorf("isBinaryData(%q) = %v, want %v", tc.name, got, tc.want)
			}
		})
	}
}

// TestRenderTree verifies the tree output format and content.
func TestRenderTree(t *testing.T) {
	files := []*File{
		{RelPath: "cmd/main.go"},
		{RelPath: "internal/handler.go"},
		{RelPath: "internal/config.go"},
		{RelPath: "go.mod"},
	}

	output := RenderTree(files)
	t.Log("\n" + output)

	wantSubstrings := []string{
		"cmd/",
		"main.go",
		"internal/",
		"handler.go",
		"config.go",
		"go.mod",
		"├──",
		"└──",
	}
	for _, s := range wantSubstrings {
		if !strings.Contains(output, s) {
			t.Errorf("RenderTree output missing %q\nGot:\n%s", s, output)
		}
	}

	// Directories should appear before their children.
	cmdIdx := strings.Index(output, "cmd/")
	mainIdx := strings.Index(output, "main.go")
	if cmdIdx == -1 || mainIdx == -1 || cmdIdx > mainIdx {
		t.Errorf("cmd/ should appear before main.go")
	}
}

// TestRenderTreeTruncation verifies long trees are capped at treeMaxLines.
func TestRenderTreeTruncation(t *testing.T) {
	// Create 200 files each in its own unique subdirectory to guarantee the
	// rendered tree has well over treeMaxLines (100) output lines.
	files := make([]*File, 200)
	for i := range files {
		files[i] = &File{RelPath: fmt.Sprintf("dir%03d/file%03d.go", i, i)}
	}

	output := RenderTree(files)
	lines := strings.Split(output, "\n")
	// treeMaxLines + 1 truncation line.
	if len(lines) > treeMaxLines+1 {
		t.Errorf("expected at most %d lines, got %d", treeMaxLines+1, len(lines))
	}
	if !strings.Contains(output, "more files") {
		t.Error("truncated tree should contain '... (N more files)' line")
	}
}

// TestRenderTreeEmpty verifies empty input produces empty output.
func TestRenderTreeEmpty(t *testing.T) {
	output := RenderTree(nil)
	if output != "" {
		t.Errorf("RenderTree(nil) should return empty string, got %q", output)
	}
}

// TestParseGitignore verifies patterns are read from .gitignore.
func TestParseGitignore(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, ".gitignore"), "# comment\n*.log\n\nsecrets/\n!important.log\n")

	patterns := parseGitignore(root)

	if len(patterns) != 3 {
		t.Fatalf("expected 3 patterns, got %d: %v", len(patterns), patterns)
	}
	if patterns[0] != "*.log" {
		t.Errorf("patterns[0] = %q, want *.log", patterns[0])
	}
	if patterns[1] != "secrets/" {
		t.Errorf("patterns[1] = %q, want secrets/", patterns[1])
	}
	if patterns[2] != "!important.log" {
		t.Errorf("patterns[2] = %q, want !important.log", patterns[2])
	}
}

// TestMatchGitignore verifies gitignore pattern matching.
func TestMatchGitignore(t *testing.T) {
	patterns := []string{"*.log", "secrets/", "docs/api/"}

	cases := []struct {
		relPath string
		isDir   bool
		want    bool
	}{
		{"app.log", false, true},
		{"sub/app.log", false, true},
		{"main.go", false, false},
		{"secrets", true, true},
		{"secrets/key.txt", false, false}, // file under dir pattern — dir blocks descent
		{"docs/api", true, true},
		{"docs/api/ref.md", false, false},
		{"readme.md", false, false},
	}

	for _, tc := range cases {
		t.Run(tc.relPath, func(t *testing.T) {
			got := matchGitignore(tc.relPath, tc.isDir, patterns)
			if got != tc.want {
				t.Errorf("matchGitignore(%q, isDir=%v) = %v, want %v", tc.relPath, tc.isDir, got, tc.want)
			}
		})
	}
}

// TestMatchGitignoreAnchored verifies leading "/" anchors to root only.
func TestMatchGitignoreAnchored(t *testing.T) {
	patterns := []string{"/go-content"}

	cases := []struct {
		relPath string
		isDir   bool
		want    bool
	}{
		{"go-content", false, true},         // root-level binary: matched
		{"go-content", true, true},          // root-level dir: matched
		{"cmd/go-content", true, false},     // nested dir: NOT matched
		{"cmd/go-content/main.go", false, false}, // file in nested dir: NOT matched
	}

	for _, tc := range cases {
		t.Run(tc.relPath, func(t *testing.T) {
			got := matchGitignore(tc.relPath, tc.isDir, patterns)
			if got != tc.want {
				t.Errorf("matchGitignore(%q, isDir=%v, anchored) = %v, want %v", tc.relPath, tc.isDir, got, tc.want)
			}
		})
	}
}

// TestIngestRepoExcludeTests verifies that ExcludeTests=true filters out _test.go files.
func TestIngestRepoExcludeTests(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "main.go"), "package main\n")
	writeFile(t, filepath.Join(root, "main_test.go"), "package main\n")
	writeFile(t, filepath.Join(root, "internal", "handler.go"), "package internal\n")
	writeFile(t, filepath.Join(root, "internal", "handler_test.go"), "package internal\n")

	result, err := IngestRepo(context.Background(), IngestOpts{
		Root:         root,
		ExcludeTests: true,
	})
	if err != nil {
		t.Fatalf("IngestRepo: %v", err)
	}

	for _, f := range result.Files {
		if strings.HasSuffix(f.RelPath, "_test.go") {
			t.Errorf("ExcludeTests=true but got test file: %s", f.RelPath)
		}
	}
	if len(result.Files) != 2 {
		t.Errorf("expected 2 non-test files, got %d", len(result.Files))
	}
}

// TestIngestRepoFocusKeywords verifies keyword-based focus (spaces = keywords, not path).
func TestIngestRepoFocusKeywords(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "cmd", "server", "main.go"), "package main\n")
	writeFile(t, filepath.Join(root, "internal", "auth", "middleware.go"), "package auth\n")
	writeFile(t, filepath.Join(root, "internal", "handler", "routes.go"), "package handler\n")
	writeFile(t, filepath.Join(root, "pkg", "models", "user.go"), "package models\n")

	result, err := IngestRepo(context.Background(), IngestOpts{
		Root:  root,
		Focus: "auth middleware",
	})
	if err != nil {
		t.Fatalf("IngestRepo: %v", err)
	}

	if len(result.Files) != 1 {
		t.Fatalf("keyword focus: expected 1 file, got %d", len(result.Files))
	}

	for _, f := range result.Files {
		rp := strings.ToLower(f.RelPath)
		if !strings.Contains(rp, "auth") || !strings.Contains(rp, "middleware") {
			t.Errorf("keyword focus: unexpected file %q (should contain auth AND middleware)", f.RelPath)
		}
	}
}

// TestIngestRepoFocusKeywordsNoMatch verifies keyword focus returns 0 when no file matches all keywords.
func TestIngestRepoFocusKeywordsNoMatch(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "internal", "auth", "login.go"), "package auth\n")
	writeFile(t, filepath.Join(root, "internal", "handler", "middleware.go"), "package handler\n")

	result, err := IngestRepo(context.Background(), IngestOpts{
		Root:  root,
		Focus: "auth middleware",
	})
	if err != nil {
		t.Fatalf("IngestRepo: %v", err)
	}

	// "auth" is in one path, "middleware" in another — no single file has both.
	if len(result.Files) != 0 {
		names := make([]string, len(result.Files))
		for i, f := range result.Files {
			names[i] = f.RelPath
		}
		t.Errorf("expected 0 files, got %d: %v", len(result.Files), names)
	}
}

// TestIngestRepoFocusPathUnchanged verifies path-based focus still works exactly as before.
func TestIngestRepoFocusPathUnchanged(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "cmd", "main.go"), "package main\n")
	writeFile(t, filepath.Join(root, "internal", "auth", "handler.go"), "package auth\n")

	// Path focus (no spaces) — existing behavior.
	result, err := IngestRepo(context.Background(), IngestOpts{
		Root:  root,
		Focus: "internal",
	})
	if err != nil {
		t.Fatalf("IngestRepo: %v", err)
	}
	if len(result.Files) != 1 {
		t.Errorf("path focus: expected 1 file, got %d", len(result.Files))
	}
	for _, f := range result.Files {
		if !strings.HasPrefix(f.RelPath, "internal") {
			t.Errorf("path focus: file %q not under internal/", f.RelPath)
		}
	}
}

// keys returns sorted keys from a string bool map (for diagnostics).
func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

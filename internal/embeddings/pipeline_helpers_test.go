package embeddings

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anatolykoptev/vaelor/internal/parser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsTestFile(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		// Go
		{"handler_test.go", true},
		{"pkg/api/handler_test.go", true},
		{"handler.go", false},
		{"main.go", false},

		// Python
		{"test_handler.py", true},
		{"pkg/test_utils.py", true},
		{"handler_test.py", true},
		{"handler.py", false},
		{"testing.py", false},

		// JS/TS
		{"app.test.js", true},
		{"app.test.ts", true},
		{"app.test.tsx", true},
		{"app.spec.js", true},
		{"app.spec.ts", true},
		{"app.spec.tsx", true},
		{"src/app.test.ts", true},
		{"app.js", false},
		{"app.ts", false},

		// Rust
		{"tests.rs", true},
		{"src/tests/mod.rs", true},
		{"src/tests/helper.rs", true},
		{"main.rs", false},
		{"lib.rs", false},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := isTestFile(tt.path)
			assert.Equal(t, tt.want, got)
		})
	}
}

func writeTestFile(t *testing.T, dir, name, content string) {
	t.Helper()
	p := filepath.Join(dir, name)
	require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
	require.NoError(t, os.WriteFile(p, []byte(content), 0o644))
}

func TestCollectSymbols_SkipsTestFiles(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "handler.go", `package main

func Handle() {}
`)
	writeTestFile(t, dir, "handler_test.go", `package main

import "testing"

func TestHandle(t *testing.T) {}
`)
	writeTestFile(t, dir, "main.go", `package main

func main() {}
`)

	syms, files, err := collectSymbols(context.Background(), dir, false)
	require.NoError(t, err)
	assert.Equal(t, 2, len(syms), "expected 2 symbols, got %d", len(syms))
	for _, f := range files {
		assert.NotContains(t, f.RelPath, "_test.go",
			"test file should not be in results: %s", f.RelPath)
	}
}

func TestBuildEmbedText_IncludesFilePath(t *testing.T) {
	sym := &parser.Symbol{
		Language: "go", Kind: parser.KindFunction,
		Name: "Handle", Signature: "func Handle()", Body: "{ return nil }",
	}
	text := buildEmbedText(sym, "pkg/api/handler.go")
	assert.Contains(t, text, "pkg/api/handler.go")
	assert.Contains(t, text, "Handle")
	assert.Contains(t, text, "func Handle()")
}

func TestBuildEmbedText_SmartTruncation(t *testing.T) {
	longBody := strings.Repeat("x := 1\n", 300)
	sym := &parser.Symbol{
		Language: "go", Kind: parser.KindFunction,
		Name: "Big", Signature: "func Big()", Body: longBody,
	}
	text := buildEmbedText(sym, "main.go")
	assert.LessOrEqual(t, len(text), maxEmbedText)
	assert.Contains(t, text, "func Big()")
	assert.Contains(t, text, "main.go")
	// Should end at line boundary
	lastNewline := strings.LastIndex(text, "\n")
	assert.Greater(t, lastNewline, 0, "should contain newlines")
	assert.Equal(t, lastNewline, len(text)-1, "should end at line boundary")
}

func TestBuildEmbedText_ShortBody(t *testing.T) {
	sym := &parser.Symbol{
		Language: "go", Kind: parser.KindFunction,
		Name: "Tiny", Signature: "func Tiny()", Body: "return 1",
	}
	text := buildEmbedText(sym, "small.go")
	assert.Contains(t, text, "small.go")
	assert.Contains(t, text, "return 1")
	assert.Less(t, len(text), maxEmbedText)
}

func TestBuildEmbedTextIncludesDocComment(t *testing.T) {
	sym := &parser.Symbol{
		Name:       "Retry",
		Kind:       parser.KindFunction,
		Signature:  "func Retry(fn func() error) error",
		DocComment: "Retry executes fn with exponential backoff up to 3 attempts.",
		Body:       "for i := 0; i < 3; i++ { _ = fn() }",
		Language:   "go",
	}
	text := buildEmbedText(sym, "foo/bar.go")
	if !strings.Contains(text, "exponential backoff") {
		t.Errorf("buildEmbedText must include DocComment, got:\n%s", text)
	}
	// Sanity: body and signature still present.
	if !strings.Contains(text, "Retry") || !strings.Contains(text, "for i :=") {
		t.Errorf("buildEmbedText must still include name/body, got:\n%s", text)
	}
}

func TestBuildEmbedTextOmitsEmptyDocComment(t *testing.T) {
	sym := &parser.Symbol{
		Name:      "Foo",
		Kind:      parser.KindFunction,
		Signature: "func Foo() {}",
		Body:      "{}",
		Language:  "go",
		// DocComment is empty
	}
	text := buildEmbedText(sym, "foo.go")
	// Must not produce double newlines or placeholder text for missing doc.
	if strings.Contains(text, "\n\n\n") {
		t.Errorf("empty DocComment must not produce empty lines, got:\n%s", text)
	}
}

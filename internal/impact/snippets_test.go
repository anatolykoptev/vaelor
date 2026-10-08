package impact

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTruncateSnippetLine_NeverSplitsARune(t *testing.T) {
	line := "x" + strings.Repeat("世", 200) // 601 bytes, multi-byte runes straddle byte 300
	got := truncateSnippetLine(line)
	if !utf8.ValidString(got) {
		t.Fatalf("truncated line is not valid UTF-8: %q", got)
	}
}

func TestAttachCallSnippets_Hardening(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret.txt")
	write := func(path, content string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(outside, "a\nb\nc\n")
	write(filepath.Join(root, "crlf.go"), "one\r\ntwo\r\nthree\r\n")
	write(filepath.Join(root, "bin.go"), "x\x00y\nz\n")
	write(filepath.Join(root, "eof.go"), "a\nb\n")

	callers := []AffectedSymbol{
		{Name: "esc", File: "../" + filepath.Base(filepath.Dir(outside)) + "/secret.txt", CallLine: 2},
		{Name: "abs", File: outside, CallLine: 2},
		{Name: "crlf", File: "crlf.go", CallLine: 2},
		{Name: "bin", File: "bin.go", CallLine: 1},
		{Name: "eof", File: "eof.go", CallLine: 3}, // one past the last real line
		{Name: "noline", File: "crlf.go", CallLine: 0},
	}
	missed := AttachCallSnippets(callers, root)

	byName := map[string]string{}
	for _, c := range callers {
		byName[c.Name] = c.Snippet
	}
	for _, name := range []string{"esc", "abs", "bin", "eof", "noline"} {
		if byName[name] != "" {
			t.Errorf("%s: expected no snippet, got %q", name, byName[name])
		}
	}
	if strings.Contains(byName["crlf"], "\r") || !strings.Contains(byName["crlf"], "two") {
		t.Errorf("crlf: want a snippet around \"two\" without \\r, got %q", byName["crlf"])
	}
	if missed != 5 {
		t.Errorf("missed = %d, want 5", missed)
	}
}

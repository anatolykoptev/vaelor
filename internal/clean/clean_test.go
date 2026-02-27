package clean

import (
	"strings"
	"testing"
)

func TestStripComments_CStyle(t *testing.T) {
	input := `package main
// This is a comment
func main() {
    x := 1 // inline comment
    /* block
    comment */
    y := 2
    // TODO: fix this
    // nolint:errcheck
    /// doc comment
}
`
	result := CleanSource(input, "go", CleanOpts{StripComments: true})

	// Code lines must be present.
	for _, want := range []string{"package main", "func main()", "x := 1", "y := 2"} {
		if !strings.Contains(result, want) {
			t.Errorf("expected %q in result, got:\n%s", want, result)
		}
	}

	// Regular comment must be gone.
	if strings.Contains(result, "This is a comment") {
		t.Errorf("expected regular comment to be stripped, got:\n%s", result)
	}
	if strings.Contains(result, "inline comment") {
		t.Errorf("expected inline comment to be stripped, got:\n%s", result)
	}
	if strings.Contains(result, "block") && strings.Contains(result, "comment") {
		// Both words appear on consecutive originally-blank lines after stripping —
		// we want to confirm neither "block\n    comment" block text survives.
		// A more precise check: the words must not appear on any non-code line.
		for _, line := range strings.Split(result, "\n") {
			trimmed := strings.TrimSpace(line)
			if trimmed == "block" || trimmed == "comment" {
				t.Errorf("block comment text survived stripping on line %q", line)
			}
		}
	}

	// Preservation keywords must survive.
	if !strings.Contains(result, "TODO: fix this") {
		t.Errorf("expected TODO comment to be preserved, got:\n%s", result)
	}
	if !strings.Contains(result, "nolint:errcheck") {
		t.Errorf("expected nolint comment to be preserved, got:\n%s", result)
	}
	if !strings.Contains(result, "/// doc comment") {
		t.Errorf("expected doc comment (///) to be preserved, got:\n%s", result)
	}
}

func TestStripComments_CStyle_BlockSameLine(t *testing.T) {
	input := `int x = /* the value */ 42;`
	result := CleanSource(input, "c", CleanOpts{StripComments: true})
	if !strings.Contains(result, "42") {
		t.Errorf("expected code after inline block comment, got: %q", result)
	}
	if strings.Contains(result, "the value") {
		t.Errorf("expected inline block comment text stripped, got: %q", result)
	}
}

func TestStripComments_CStyle_DocBlock(t *testing.T) {
	input := `/** This documents the API */
func Foo() {}`
	result := CleanSource(input, "go", CleanOpts{StripComments: true})
	if !strings.Contains(result, "/** This documents the API */") {
		t.Errorf("expected /** doc comment preserved, got:\n%s", result)
	}
}

func TestStripComments_Python(t *testing.T) {
	input := `import os
# This is a comment
def main():
    x = 1  # inline
    # TODO: fix
    # noqa: E501
    pass
`
	result := CleanSource(input, "python", CleanOpts{StripComments: true})

	// Code lines must survive.
	for _, want := range []string{"import os", "def main():", "x = 1", "pass"} {
		if !strings.Contains(result, want) {
			t.Errorf("expected %q in result, got:\n%s", want, result)
		}
	}

	// Regular comments must be gone.
	if strings.Contains(result, "This is a comment") {
		t.Errorf("expected regular comment stripped, got:\n%s", result)
	}
	if strings.Contains(result, "inline") {
		t.Errorf("expected inline comment stripped, got:\n%s", result)
	}

	// Preservation keywords must survive.
	if !strings.Contains(result, "TODO: fix") {
		t.Errorf("expected TODO preserved, got:\n%s", result)
	}
	if !strings.Contains(result, "noqa: E501") {
		t.Errorf("expected noqa preserved, got:\n%s", result)
	}
}

func TestStripComments_UnknownLanguage(t *testing.T) {
	input := "// comment\ncode line\n"
	result := CleanSource(input, "haskell", CleanOpts{StripComments: true})
	if result != input {
		t.Errorf("unknown language should return source unchanged, got:\n%q", result)
	}
}

func TestCleanSource_Full(t *testing.T) {
	input := `package main

// Regular comment

func main() {
    // another comment
    x := 1


    y := 2
}
`
	opts := CleanOpts{
		StripComments:     true,
		StripBlankLines:   true,
		TruncateLongLines: true,
		MaxLineChars:      200,
	}
	result := CleanSource(input, "go", opts)

	// Comments stripped.
	if strings.Contains(result, "Regular comment") || strings.Contains(result, "another comment") {
		t.Errorf("comments should be stripped, got:\n%s", result)
	}
	// No consecutive blank lines.
	if strings.Contains(result, "\n\n\n") {
		t.Errorf("consecutive blank lines should be collapsed, got:\n%q", result)
	}
	// Code survives.
	if !strings.Contains(result, "func main()") {
		t.Errorf("expected func main() in result, got:\n%s", result)
	}
}

func TestCleanSource_MaxFileChars(t *testing.T) {
	const limit = 20
	input := "abcdefghijklmnopqrstuvwxyz"
	result := CleanSource(input, "go", CleanOpts{MaxFileChars: limit})
	if !strings.HasSuffix(result, "... [truncated]\n") {
		t.Errorf("expected truncation marker, got: %q", result)
	}
	if !strings.HasPrefix(result, input[:limit]) {
		t.Errorf("expected first %d chars preserved, got: %q", limit, result)
	}
}

func TestCleanSource_InvalidUTF8(t *testing.T) {
	input := string([]byte{0xff, 0xfe, 0x00})
	result := CleanSource(input, "go", CleanOpts{StripComments: true})
	if result != "[binary or invalid UTF-8 content omitted]" {
		t.Errorf("expected binary omission message, got: %q", result)
	}
}

func TestStripComments_CStyle_StringLiteral(t *testing.T) {
	// The // inside a string should not be treated as a comment start.
	input := `s := "http://example.com"` + "\n"
	result := CleanSource(input, "go", CleanOpts{StripComments: true})
	if !strings.Contains(result, `"http://example.com"`) {
		t.Errorf("URL inside string literal should not be stripped, got: %q", result)
	}
}

func TestStripComments_Multiline_BlockComment(t *testing.T) {
	input := `code before
/* this is
   a multi-line
   block comment */
code after
`
	result := CleanSource(input, "go", CleanOpts{StripComments: true})
	if !strings.Contains(result, "code before") {
		t.Errorf("expected 'code before' in result, got:\n%s", result)
	}
	if !strings.Contains(result, "code after") {
		t.Errorf("expected 'code after' in result, got:\n%s", result)
	}
	if strings.Contains(result, "multi-line") {
		t.Errorf("expected block comment body stripped, got:\n%s", result)
	}
}

func TestStripComments_Python_HashInString(t *testing.T) {
	// # inside a Python string should not be stripped.
	input := `s = "color: #fff"` + "\n"
	result := CleanSource(input, "python", CleanOpts{StripComments: true})
	if !strings.Contains(result, `"color: #fff"`) {
		t.Errorf("# inside string literal should not be stripped, got: %q", result)
	}
}

// TestCollapseBlankLines tests the helper directly.
func TestCollapseBlankLines(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "no blanks",
			input: "a\nb\nc\n",
			want:  "a\nb\nc\n",
		},
		{
			name:  "single blank preserved",
			input: "a\n\nb\n",
			want:  "a\n\nb\n",
		},
		{
			name:  "double blank collapsed",
			input: "a\n\n\nb\n",
			want:  "a\n\nb\n",
		},
		{
			name:  "triple blank collapsed",
			input: "a\n\n\n\nb\n",
			want:  "a\n\nb\n",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := collapseBlankLines(tc.input)
			if got != tc.want {
				t.Errorf("collapseBlankLines(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

// TestTruncateLongLines tests the helper directly.
func TestTruncateLongLines(t *testing.T) {
	input := "short\n" + strings.Repeat("x", 10) + "\n"
	result := truncateLongLines(input, 5)
	lines := strings.Split(result, "\n")
	if lines[0] != "short" {
		t.Errorf("short line should be unchanged, got: %q", lines[0])
	}
	if !strings.HasSuffix(lines[1], " …") {
		t.Errorf("long line should end with marker, got: %q", lines[1])
	}
}

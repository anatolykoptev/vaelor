package clean

import (
	"strings"
	"testing"
)

// C-style (Go, C, JavaScript, …) comment stripping.

func TestStripComments_CStyle(t *testing.T) {
	t.Parallel()
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

	for _, want := range []string{"package main", "func main()", "x := 1", "y := 2"} {
		if !strings.Contains(result, want) {
			t.Errorf("expected %q in result, got:\n%s", want, result)
		}
	}
	if strings.Contains(result, "This is a comment") {
		t.Errorf("expected regular comment stripped, got:\n%s", result)
	}
	if strings.Contains(result, "inline comment") {
		t.Errorf("expected inline comment stripped, got:\n%s", result)
	}
	for _, line := range strings.Split(result, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "block" || trimmed == "comment" {
			t.Errorf("block comment text survived stripping on line %q", line)
		}
	}
	if !strings.Contains(result, "TODO: fix this") {
		t.Errorf("expected TODO preserved, got:\n%s", result)
	}
	if !strings.Contains(result, "nolint:errcheck") {
		t.Errorf("expected nolint preserved, got:\n%s", result)
	}
	if !strings.Contains(result, "/// doc comment") {
		t.Errorf("expected doc comment (///) preserved, got:\n%s", result)
	}
}

func TestStripComments_CStyle_BlockSameLine(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
	input := "/** This documents the API */\nfunc Foo() {}"
	result := CleanSource(input, "go", CleanOpts{StripComments: true})
	if !strings.Contains(result, "/** This documents the API */") {
		t.Errorf("expected /** doc comment preserved, got:\n%s", result)
	}
}

func TestStripComments_CStyle_StringLiteral(t *testing.T) {
	t.Parallel()
	input := `s := "http://example.com"` + "\n"
	result := CleanSource(input, "go", CleanOpts{StripComments: true})
	if !strings.Contains(result, `"http://example.com"`) {
		t.Errorf("URL inside string literal should not be stripped, got: %q", result)
	}
}

func TestStripComments_Multiline_BlockComment(t *testing.T) {
	t.Parallel()
	input := "code before\n/* this is\n   a multi-line\n   block comment */\ncode after\n"
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

// Python / hash-style comment stripping.

func TestStripComments_Python(t *testing.T) {
	t.Parallel()
	input := "import os\n# This is a comment\ndef main():\n    x = 1  # inline\n    # TODO: fix\n    # noqa: E501\n    pass\n"
	result := CleanSource(input, "python", CleanOpts{StripComments: true})

	for _, want := range []string{"import os", "def main():", "x = 1", "pass"} {
		if !strings.Contains(result, want) {
			t.Errorf("expected %q in result, got:\n%s", want, result)
		}
	}
	if strings.Contains(result, "This is a comment") {
		t.Errorf("expected regular comment stripped, got:\n%s", result)
	}
	if strings.Contains(result, "inline") {
		t.Errorf("expected inline comment stripped, got:\n%s", result)
	}
	if !strings.Contains(result, "TODO: fix") {
		t.Errorf("expected TODO preserved, got:\n%s", result)
	}
	if !strings.Contains(result, "noqa: E501") {
		t.Errorf("expected noqa preserved, got:\n%s", result)
	}
}

func TestStripComments_Python_HashInString(t *testing.T) {
	t.Parallel()
	input := `s = "color: #fff"` + "\n"
	result := CleanSource(input, "python", CleanOpts{StripComments: true})
	if !strings.Contains(result, `"color: #fff"`) {
		t.Errorf("# inside string literal should not be stripped, got: %q", result)
	}
}

func TestStripComments_UnknownLanguage(t *testing.T) {
	t.Parallel()
	input := "// comment\ncode line\n"
	result := CleanSource(input, "haskell", CleanOpts{StripComments: true})
	if result != input {
		t.Errorf("unknown language should return source unchanged, got:\n%q", result)
	}
}

package clean

import (
	"strings"
	"testing"
)

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

	if strings.Contains(result, "Regular comment") || strings.Contains(result, "another comment") {
		t.Errorf("comments should be stripped, got:\n%s", result)
	}
	if strings.Contains(result, "\n\n\n") {
		t.Errorf("consecutive blank lines should be collapsed, got:\n%q", result)
	}
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

// TestCollapseBlankLines tests the helper directly.
func TestCollapseBlankLines(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "no blanks", input: "a\nb\nc\n", want: "a\nb\nc\n"},
		{name: "single blank preserved", input: "a\n\nb\n", want: "a\n\nb\n"},
		{name: "double blank collapsed", input: "a\n\n\nb\n", want: "a\n\nb\n"},
		{name: "triple blank collapsed", input: "a\n\n\n\nb\n", want: "a\n\nb\n"},
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

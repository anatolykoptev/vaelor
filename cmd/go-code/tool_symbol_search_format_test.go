package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/anatolykoptev/go-code/internal/parser"
)

// TestFormatSymbolSearchXML_DropsEmptyBodyAndRelpath asserts:
//   - empty <body></body> tag is omitted (Body is *xmlCDATA, nil + omitempty)
//   - sym.File is rewritten to a path relative to root (no /tmp/... leak)
//
// Pre-fix output had <body></body> on every entry without IncludeBody=true,
// and absolute paths like /tmp/go-code-workspace/<slug>/cmd/main.go which
// the consuming agent could not use.
func TestFormatSymbolSearchXML_DropsEmptyBodyAndRelpath(t *testing.T) {
	root := "/tmp/workspace/anatolykoptev_demo"
	syms := []*parser.Symbol{
		{
			Kind:      "function",
			Name:      "Login",
			File:      root + "/internal/auth/login.go",
			StartLine: 10,
			EndLine:   25,
			Signature: "func Login(ctx context.Context) error",
		},
	}

	out := formatSymbolSearchXML("Login", syms, root)

	if strings.Contains(out, "<body></body>") || strings.Contains(out, "<body/>") {
		t.Fatalf("empty <body> tag must be omitted:\n%s", out)
	}
	if strings.Contains(out, root) {
		t.Fatalf("absolute workspace path must be rewritten to relpath:\n%s", out)
	}
	if !strings.Contains(out, `file="internal/auth/login.go"`) {
		t.Fatalf("expected relative path attribute, got:\n%s", out)
	}
	if !strings.Contains(out, "func Login(ctx context.Context) error") {
		t.Fatalf("function signature must be present:\n%s", out)
	}
}

// TestSymbolSearch_ZeroResult_IncludesIgnoredPathsHint asserts that the
// zero-result message (built from indexedPathsHint) includes the "excluded"
// keyword and mentions known ignored directories.
func TestSymbolSearch_ZeroResult_IncludesIgnoredPathsHint(t *testing.T) {
	hint := indexedPathsHint()
	msg := fmt.Sprintf("No symbols found matching %q.\n\n%s", "MyMissingSym", hint)
	for _, want := range []string{"excluded", ".claude", "vendor", "testdata"} {
		if !strings.Contains(msg, want) {
			t.Errorf("zero-result message missing %q; got:\n%s", want, msg)
		}
	}
}

// TestFormatSymbolSearchXML_KeepsBodyWhenPresent asserts the <body> tag is
// emitted when IncludeBody filled in sym.Body.
func TestFormatSymbolSearchXML_KeepsBodyWhenPresent(t *testing.T) {
	root := "/repo"
	syms := []*parser.Symbol{
		{
			Kind: "function", Name: "X",
			File: root + "/x.go", StartLine: 1, EndLine: 3,
			Signature: "func X()",
			Body:      "func X() { println(1) }",
		},
	}
	out := formatSymbolSearchXML("X", syms, root)
	if !strings.Contains(out, "println(1)") {
		t.Fatalf("body content must be present when sym.Body is set:\n%s", out)
	}
}

package parser_test

import (
	"slices"
	"testing"

	"github.com/anatolykoptev/go-code/internal/parser"
)

func TestDetectLanguage(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"main.go", "go"},
		{"server.go", "go"},
		{"script.py", "python"},
		{"app.ts", "typescript"},
		{"component.tsx", "typescript"},
		{"index.js", "javascript"},
		{"module.mjs", "javascript"},
		{"module.cjs", "javascript"},
		{"module.cts", "typescript"},
		{"module.mts", "typescript"},
		{"main.rs", "rust"},
		{"Main.java", "java"},
		{"main.c", "c"},
		{"header.h", "c"},
		{"main.cpp", "cpp"},
		{"main.cc", "cpp"},
		{"main.cxx", "cpp"},
		{"main.hpp", "cpp"},
		{"script.rb", "ruby"},
		{"Program.cs", "csharp"},
		{"unknown.xyz", ""},
		{"no_extension", ""},
	}

	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			got := parser.DetectLanguageFromPath(tc.path)
			if got != tc.want {
				t.Errorf("DetectLanguageFromPath(%q) = %q, want %q", tc.path, got, tc.want)
			}
		})
	}
}

func TestSupportedLanguages(t *testing.T) {
	langs := parser.SupportedLanguages()
	mustHave := []string{"go", "python", "typescript", "javascript", "rust", "java", "c", "cpp", "ruby", "csharp", "php", "svelte", "astro"}
	for _, want := range mustHave {
		if !slices.Contains(langs, want) {
			t.Errorf("SupportedLanguages missing %q; got %v", want, langs)
		}
	}
}

func TestParseUnsupportedExtension(t *testing.T) {
	_, err := parser.ParseFile("file.unknown", []byte("content"), parser.ParseOpts{})
	if err == nil {
		t.Error("expected error for unsupported extension, got nil")
	}
}

func TestParseFileAliases(t *testing.T) {
	src := []byte("function hello() { return 42; }\n")
	cases := []struct {
		ext      string
		wantLang string
	}{
		{".mjs", "javascript"},
		{".cjs", "javascript"},
		{".cts", "typescript"},
		{".mts", "typescript"},
		{".js", "javascript"},
		{".ts", "typescript"},
	}
	for _, tc := range cases {
		t.Run(tc.ext, func(t *testing.T) {
			result, err := parser.ParseFile("module"+tc.ext, src, parser.ParseOpts{})
			if err != nil {
				t.Fatalf("ParseFile(%q): unexpected error: %v", tc.ext, err)
			}
			if len(result.Symbols) == 0 {
				t.Errorf("ParseFile(%q): expected at least one symbol, got none", tc.ext)
			}
			if result.Language != tc.wantLang {
				t.Errorf("ParseFile(%q): Language = %q, want %q", tc.ext, result.Language, tc.wantLang)
			}
		})
	}
}

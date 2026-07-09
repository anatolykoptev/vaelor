package clean

import (
	"strings"
	"testing"
)

// Svelte and Astro use C-style (// and /* */) comment syntax.

func TestStripComments_Svelte_LineComment(t *testing.T) {
	t.Parallel()
	input := "let x = 1; // line comment\nlet y = 2;\n"
	result := CleanSource(input, "svelte", CleanOpts{StripComments: true})
	if strings.Contains(result, "line comment") {
		t.Errorf("svelte: expected line comment stripped, got: %q", result)
	}
	if !strings.Contains(result, "let x = 1") {
		t.Errorf("svelte: expected code before comment preserved, got: %q", result)
	}
	if !strings.Contains(result, "let y = 2") {
		t.Errorf("svelte: expected second code line preserved, got: %q", result)
	}
}

func TestStripComments_Svelte_BlockComment(t *testing.T) {
	t.Parallel()
	input := "let x = 1; /* block */ let y = 2;\n"
	result := CleanSource(input, "svelte", CleanOpts{StripComments: true})
	if strings.Contains(result, "block") {
		t.Errorf("svelte: expected block comment stripped, got: %q", result)
	}
	if !strings.Contains(result, "let x = 1") {
		t.Errorf("svelte: expected code before block comment preserved, got: %q", result)
	}
	if !strings.Contains(result, "let y = 2") {
		t.Errorf("svelte: expected code after block comment preserved, got: %q", result)
	}
}

func TestStripComments_Svelte_TodoPreserved(t *testing.T) {
	t.Parallel()
	input := "// TODO: fix\nlet x = 1;\n"
	result := CleanSource(input, "svelte", CleanOpts{StripComments: true})
	if !strings.Contains(result, "TODO: fix") {
		t.Errorf("svelte: expected TODO comment preserved, got: %q", result)
	}
}

func TestStripComments_Svelte_FixmePreserved(t *testing.T) {
	t.Parallel()
	input := "// FIXME(name): bug\nlet x = 1;\n"
	result := CleanSource(input, "svelte", CleanOpts{StripComments: true})
	if !strings.Contains(result, "FIXME(name): bug") {
		t.Errorf("svelte: expected FIXME comment preserved, got: %q", result)
	}
}

func TestStripComments_Astro_LineComment(t *testing.T) {
	t.Parallel()
	input := "let x = 1; // line comment\nlet y = 2;\n"
	result := CleanSource(input, "astro", CleanOpts{StripComments: true})
	if strings.Contains(result, "line comment") {
		t.Errorf("astro: expected line comment stripped, got: %q", result)
	}
	if !strings.Contains(result, "let x = 1") {
		t.Errorf("astro: expected code before comment preserved, got: %q", result)
	}
	if !strings.Contains(result, "let y = 2") {
		t.Errorf("astro: expected second code line preserved, got: %q", result)
	}
}

func TestStripComments_Astro_BlockComment(t *testing.T) {
	t.Parallel()
	input := "let x = 1; /* block */ let y = 2;\n"
	result := CleanSource(input, "astro", CleanOpts{StripComments: true})
	if strings.Contains(result, "block") {
		t.Errorf("astro: expected block comment stripped, got: %q", result)
	}
	if !strings.Contains(result, "let x = 1") {
		t.Errorf("astro: expected code before block comment preserved, got: %q", result)
	}
	if !strings.Contains(result, "let y = 2") {
		t.Errorf("astro: expected code after block comment preserved, got: %q", result)
	}
}

func TestStripComments_Astro_TodoPreserved(t *testing.T) {
	t.Parallel()
	input := "// TODO: fix\nlet x = 1;\n"
	result := CleanSource(input, "astro", CleanOpts{StripComments: true})
	if !strings.Contains(result, "TODO: fix") {
		t.Errorf("astro: expected TODO comment preserved, got: %q", result)
	}
}

func TestStripComments_Astro_FixmePreserved(t *testing.T) {
	t.Parallel()
	input := "// FIXME(name): bug\nlet x = 1;\n"
	result := CleanSource(input, "astro", CleanOpts{StripComments: true})
	if !strings.Contains(result, "FIXME(name): bug") {
		t.Errorf("astro: expected FIXME comment preserved, got: %q", result)
	}
}

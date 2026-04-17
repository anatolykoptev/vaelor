package preproc

import (
	"slices"
	"strings"
	"testing"
)

func TestExtractAstro_Empty(t *testing.T) {
	vs := ExtractAstro([]byte(""))
	if len(vs.Code) != 0 {
		t.Errorf("empty: expected empty Code, got %q", vs.Code)
	}
	if vs.Lang != "astro" {
		t.Errorf("lang=%q", vs.Lang)
	}
}

func TestExtractAstro_FrontmatterOnly(t *testing.T) {
	// Line 1: ---
	// Line 2: const title = "Hello";
	// Line 3: const desc = "World";
	// Line 4: ---
	src := "---\nconst title = \"Hello\";\nconst desc = \"World\";\n---\n<html><body>{title}</body></html>\n"
	vs := ExtractAstro([]byte(src))
	if !strings.Contains(string(vs.Code), "title") {
		t.Errorf("missing frontmatter content: %q", string(vs.Code))
	}
	if strings.Contains(string(vs.Code), "html") {
		t.Errorf("should not contain HTML body")
	}
	assertLineMap(t, "fm-only", vs)

	// afterDashes points to line 2.
	assertLineMapAt(t, "fm-only", vs, 1, 2)
	assertLineMapAt(t, "fm-only", vs, 2, 3)
}

func TestExtractAstro_ScriptOnly(t *testing.T) {
	// Line 1: <html>
	// Line 2: <head>
	// Line 3: <script>
	// Line 4: console.log("hi");
	// Line 5: </script>
	src := "<html>\n<head>\n<script>\nconsole.log(\"hi\");\n</script>\n</head>\n</html>\n"
	vs := ExtractAstro([]byte(src))
	if !strings.Contains(string(vs.Code), "console") {
		t.Errorf("missing script content: %q", string(vs.Code))
	}
	assertLineMap(t, "script-only", vs)

	// contentStart is \n after '>' on line 3 → maps to line 3.
	assertLineMapAt(t, "script-only", vs, 1, 3)
	// line 4: console.log
	assertLineMapAt(t, "script-only", vs, 2, 4)
}

func TestExtractAstro_FrontmatterAndScript(t *testing.T) {
	src := "---\nimport Foo from './Foo.astro';\n---\n<html>\n<script>\ndocument.title = \"hi\";\n</script>\n</html>\n"
	vs := ExtractAstro([]byte(src))
	if !strings.Contains(string(vs.Code), "Foo") {
		t.Errorf("missing frontmatter import")
	}
	if !strings.Contains(string(vs.Code), "document") {
		t.Errorf("missing script content")
	}
	if !slices.Contains(vs.LineMap, 0) {
		t.Errorf("expected padding line between frontmatter and script")
	}
	assertLineMap(t, "fm+script", vs)
}

func TestExtractAstro_MultipleScripts(t *testing.T) {
	src := "---\nconst x = 1;\n---\n<script>\nlet a = 2;\n</script>\n<p>text</p>\n<script>\nlet b = 3;\n</script>\n"
	vs := ExtractAstro([]byte(src))
	if !strings.Contains(string(vs.Code), "let a") {
		t.Errorf("missing first script")
	}
	if !strings.Contains(string(vs.Code), "let b") {
		t.Errorf("missing second script")
	}
	padCount := 0
	for _, v := range vs.LineMap {
		if v == 0 {
			padCount++
		}
	}
	if padCount < 2 {
		t.Errorf("expected at least 2 padding lines, got %d", padCount)
	}
}

func TestExtractAstro_CRLF(t *testing.T) {
	src := "---\r\nconst x = 1;\r\n---\r\n<p>hi</p>\r\n"
	vs := ExtractAstro([]byte(src))
	if !strings.Contains(string(vs.Code), "x = 1") {
		t.Errorf("CRLF: missing content, got: %q", string(vs.Code))
	}
}

// TestExtractAstro_AttrGtEntity verifies that &gt; inside a script attribute value
// does not confuse the tag-close search. The raw bytes of &gt; contain no '>' byte,
// so the scanner correctly finds the actual closing '>' of the opening tag.
func TestExtractAstro_AttrGtEntity(t *testing.T) {
	src := `<script src="x&gt;y.js">let x = 1;</script>`
	vs := ExtractAstro([]byte(src))
	code := string(vs.Code)
	if code != "let x = 1;" {
		t.Errorf("AttrGtEntity: Code = %q, want %q", code, "let x = 1;")
	}
}

// TestExtractAstro_AttrLiteralGt documents that a literal '>' byte inside an
// attribute value fools the raw-byte scanner. This is a known limitation.
func TestExtractAstro_AttrLiteralGt(t *testing.T) {
	src := `<script title="<<<>>>" src="ok.js">let x = 1;</script>`
	vs := ExtractAstro([]byte(src))
	if vs == nil {
		t.Fatal("AttrLiteralGt: returned nil VirtualSource")
	}
}

// TestExtractAstro_BoundSingleLine verifies that a <script> whose opening tag
// spans two lines is not extracted. The bound caps '>' search to the first
// newline; since no '>' appears before the newline, gtIdx < 0 → break.
// Removing the newline-cap would let the scanner find '>' on the next line.
func TestExtractAstro_BoundSingleLine(t *testing.T) {
	src := "<html>\n<script\nsrc=\"long-attr\">\nlet x = 1;\n</script>\n</html>\n"
	vs := ExtractAstro([]byte(src))
	code := string(vs.Code)
	if code != "" {
		t.Errorf("BoundSingleLine: expected empty Code (multi-line open tag), got %q", code)
	}
}

// TestExtractAstro_BoundMaxBytes verifies that an opening <script> tag whose
// attribute value is >512 bytes without any newline or '>' does not let the
// scanner run past the 512-byte window. Without the cap the scanner would
// eventually find '>' and incorrectly extract content.
func TestExtractAstro_BoundMaxBytes(t *testing.T) {
	pad := strings.Repeat("a", 600)
	src := `<script src="` + pad + `">let x = 1;</script>`
	vs := ExtractAstro([]byte(src))
	code := string(vs.Code)
	if code != "" {
		t.Errorf("BoundMaxBytes: expected empty Code (no '>' within 512 bytes), got %q", code)
	}
}

func TestExtractAstro_NoFrontmatterNoScript(t *testing.T) {
	src := "<html><body>plain</body></html>\n"
	vs := ExtractAstro([]byte(src))
	if len(vs.Code) != 0 {
		t.Errorf("no fm/script: expected empty Code, got %q", vs.Code)
	}
}

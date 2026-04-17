package preproc

import (
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
	foundPadding := false
	for _, v := range vs.LineMap {
		if v == 0 {
			foundPadding = true
			break
		}
	}
	if !foundPadding {
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

func TestExtractAstro_NoFrontmatterNoScript(t *testing.T) {
	src := "<html><body>plain</body></html>\n"
	vs := ExtractAstro([]byte(src))
	if len(vs.Code) != 0 {
		t.Errorf("no fm/script: expected empty Code, got %q", vs.Code)
	}
}

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
// Pins current limited behavior. If the scanner gains escape handling, update this assertion.
func TestExtractAstro_AttrLiteralGt(t *testing.T) {
	// The scanner sees the first '>' inside "<<<>>>" as the tag close, so content
	// starts after that '>', yielding the remaining attr garbage plus the real JS.
	src := `<script title="<<<>>>" src="ok.js">let x = 1;</script>`
	vs := ExtractAstro([]byte(src))
	// Current behavior: scanner picks up '>' inside attr value; code starts with '>'.
	wantCode := `>>" src="ok.js">let x = 1;`
	if string(vs.Code) != wantCode {
		t.Errorf("AttrLiteralGt: Code = %q, want %q", string(vs.Code), wantCode)
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
	pad := strings.Repeat("a", tagOpenScanLimit+88) // pad past the bound
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

// ---- TemplateRef / scanTemplateRefs tests ----

func TestScanTemplateRefs_CapitalisedTags(t *testing.T) {
	src := "---\nimport Breadcrumbs from './Breadcrumbs.astro';\n---\n<Header />\n<main>\n  <Breadcrumbs items={items} />\n  <Footer />\n</main>\n"
	refs := scanTemplateRefs([]byte(src))
	names := refNames(refs)
	want := []string{"Header", "Breadcrumbs", "Footer"}
	if !stringSlicesMatch(names, want) {
		t.Errorf("got %v, want %v", names, want)
	}
}

func TestScanTemplateRefs_HTMLTagsSkipped(t *testing.T) {
	src := "---\n---\n<div><span /><p>text</p></div>\n"
	refs := scanTemplateRefs([]byte(src))
	if len(refs) != 0 {
		t.Errorf("expected 0 refs for HTML-only, got %v", refNames(refs))
	}
}

func TestScanTemplateRefs_Mixed(t *testing.T) {
	src := "---\n---\n<Header /><div><Footer /></div>\n"
	refs := scanTemplateRefs([]byte(src))
	names := refNames(refs)
	want := []string{"Header", "Footer"}
	if !stringSlicesMatch(names, want) {
		t.Errorf("got %v, want %v", names, want)
	}
}

func TestScanTemplateRefs_NamespacedSkipped(t *testing.T) {
	src := "---\n---\n<astro:fragment><Foo /></astro:fragment>\n"
	refs := scanTemplateRefs([]byte(src))
	names := refNames(refs)
	// astro:fragment is skipped; Foo inside is kept
	if len(refs) != 1 || refs[0].Name != "Foo" {
		t.Errorf("got %v, want [Foo]", names)
	}
}

func TestScanTemplateRefs_Conditional(t *testing.T) {
	src := "---\n---\n{cond && <Foo />}\n"
	refs := scanTemplateRefs([]byte(src))
	if len(refs) != 1 || refs[0].Name != "Foo" {
		t.Errorf("got %v, want [Foo]", refNames(refs))
	}
}

func TestScanTemplateRefs_SelfClosingAndNested(t *testing.T) {
	src := "---\n---\n<Foo /><Bar><Baz /></Bar>\n"
	refs := scanTemplateRefs([]byte(src))
	names := refNames(refs)
	want := []string{"Foo", "Bar", "Baz"}
	if !stringSlicesMatch(names, want) {
		t.Errorf("got %v, want %v", names, want)
	}
}

func TestScanTemplateRefs_SkipsScriptContent(t *testing.T) {
	src := "---\n---\n<script>const X = <Header />;</script>\n<Real />\n"
	refs := scanTemplateRefs([]byte(src))
	names := refNames(refs)
	// Header inside <script> must be skipped; Real outside is kept
	if len(refs) != 1 || refs[0].Name != "Real" {
		t.Errorf("got %v, want [Real]", names)
	}
}

func TestScanTemplateRefs_SkipsHTMLComment(t *testing.T) {
	src := "---\n---\n<!-- <Hidden /> -->\n<Visible />\n"
	refs := scanTemplateRefs([]byte(src))
	if len(refs) != 1 || refs[0].Name != "Visible" {
		t.Errorf("got %v, want [Visible]", refNames(refs))
	}
}

func TestScanTemplateRefs_PositionTracking(t *testing.T) {
	// Breadcrumbs is on line 4, col 1 (after 3-line frontmatter).
	src := "---\nimport B from './B.astro';\n---\n<Breadcrumbs />\n"
	refs := scanTemplateRefs([]byte(src))
	if len(refs) != 1 {
		t.Fatalf("expected 1 ref, got %d", len(refs))
	}
	if refs[0].Line != 4 {
		t.Errorf("Line = %d, want 4", refs[0].Line)
	}
	if refs[0].Col != 1 {
		t.Errorf("Col = %d, want 1", refs[0].Col)
	}
}

func TestScanTemplateRefs_MultipleOccurrences(t *testing.T) {
	src := "---\n---\n<Foo /><Foo /><Foo />\n"
	refs := scanTemplateRefs([]byte(src))
	if len(refs) != 3 {
		t.Errorf("expected 3 refs (no dedup), got %d", len(refs))
	}
}

func TestScanTemplateRefs_NoFrontmatter(t *testing.T) {
	src := "<Bar /><div />\n"
	refs := scanTemplateRefs([]byte(src))
	if len(refs) != 1 || refs[0].Name != "Bar" {
		t.Errorf("got %v, want [Bar]", refNames(refs))
	}
}

func TestExtractAstroWithRefs_ReturnsVSAndRefs(t *testing.T) {
	src := "---\nconst x = 1;\n---\n<MyComp />\n"
	vs, refs := ExtractAstroWithRefs([]byte(src))
	if vs == nil {
		t.Fatal("VirtualSource is nil")
	}
	if !strings.Contains(string(vs.Code), "x = 1") {
		t.Errorf("VirtualSource missing frontmatter content")
	}
	if len(refs) != 1 || refs[0].Name != "MyComp" {
		t.Errorf("refs = %v, want [MyComp]", refNames(refs))
	}
}

func refNames(refs []TemplateRef) []string {
	names := make([]string, len(refs))
	for i, r := range refs {
		names[i] = r.Name
	}
	return names
}

func stringSlicesMatch(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

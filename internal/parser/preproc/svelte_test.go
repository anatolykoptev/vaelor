package preproc

import (
	"slices"
	"strings"
	"testing"
)

func TestExtractSvelte_Empty(t *testing.T) {
	vs := ExtractSvelte([]byte(""))
	if len(vs.Code) != 0 {
		t.Errorf("empty src: expected empty Code, got %q", vs.Code)
	}
	if len(vs.LineMap) != 0 {
		t.Errorf("empty src: expected empty LineMap, got len=%d", len(vs.LineMap))
	}
	if vs.Lang != "svelte" {
		t.Errorf("lang=%q want svelte", vs.Lang)
	}
}

func TestExtractSvelte_NoScript(t *testing.T) {
	src := "<div>hello world</div>\n"
	vs := ExtractSvelte([]byte(src))
	if len(vs.Code) != 0 {
		t.Errorf("no script: expected empty Code, got %q", vs.Code)
	}
}

func TestExtractSvelte_SingleBlock(t *testing.T) {
	// Line 1: <script>
	// Line 2: let x = 1;
	// Line 3: let y = 2;
	// Line 4: </script>
	src := "<script>\nlet x = 1;\nlet y = 2;\n</script>\n<div>{x}</div>\n"
	vs := ExtractSvelte([]byte(src))
	if !strings.Contains(string(vs.Code), "let x = 1;") {
		t.Errorf("Code missing expected content, got: %q", string(vs.Code))
	}
	assertLineMap(t, "single-block", vs)

	// contentStart points to the \n that follows '>' on line 1 → maps to line 1.
	// After that \n we enter line 2 (let x = 1;).
	assertLineMapAt(t, "single-block", vs, 1, 1) // \n on line 1
	assertLineMapAt(t, "single-block", vs, 2, 2) // let x = 1; → line 2
	assertLineMapAt(t, "single-block", vs, 3, 3) // let y = 2; → line 3
}

func TestExtractSvelte_LangTs(t *testing.T) {
	src := "<script lang=\"ts\">\nconst greeting: string = \"hello\";\n</script>\n"
	vs := ExtractSvelte([]byte(src))
	if !strings.Contains(string(vs.Code), "const greeting") {
		t.Errorf("Code missing content: %q", string(vs.Code))
	}
	if vs.Lang != "svelte" {
		t.Errorf("lang=%q", vs.Lang)
	}
}

func TestExtractSvelte_MultipleBlocks(t *testing.T) {
	src := "<script context=\"module\">\nexport const preload = true;\n</script>\n\n<script>\nlet count = 0;\n</script>\n"
	vs := ExtractSvelte([]byte(src))
	if !strings.Contains(string(vs.Code), "preload") {
		t.Errorf("missing module content")
	}
	if !strings.Contains(string(vs.Code), "count") {
		t.Errorf("missing instance content")
	}
	assertLineMap(t, "multi-block", vs)

	// Should have a padding line (0) between blocks.
	if !slices.Contains(vs.LineMap, 0) {
		t.Errorf("expected at least one padding line (0) in LineMap, got: %v", vs.LineMap)
	}
}

func TestExtractSvelte_Svelte5Module(t *testing.T) {
	src := "<script module>\nexport const ssr = false;\n</script>\n<script>\nlet data = {};\n</script>\n"
	vs := ExtractSvelte([]byte(src))
	if !strings.Contains(string(vs.Code), "ssr") {
		t.Errorf("missing module content")
	}
	if !strings.Contains(string(vs.Code), "data") {
		t.Errorf("missing instance content")
	}
}

func TestExtractSvelte_CRLF(t *testing.T) {
	src := "<script>\r\nlet a = 1;\r\nlet b = 2;\r\n</script>\r\n"
	vs := ExtractSvelte([]byte(src))
	if !strings.Contains(string(vs.Code), "let a") {
		t.Errorf("CRLF: missing content, got: %q", string(vs.Code))
	}
	assertLineMap(t, "crlf", vs)
}

func TestExtractSvelte_MissingCloseTag(t *testing.T) {
	src := "<script>\nlet x = 1;\n"
	vs := ExtractSvelte([]byte(src))
	if !strings.Contains(string(vs.Code), "let x") {
		t.Errorf("missing close: expected content, got: %q", string(vs.Code))
	}
}

// TestExtractSvelte_AttrGtEntity verifies that the scanner correctly handles
// an HTML entity &gt; inside an attribute value without treating the 't' in
// "&gt;" as a tag-closing '>'. The raw bytes of &gt; contain no '>' byte, so
// the scanner finds the actual tag-closing '>' after the attribute list.
func TestExtractSvelte_AttrGtEntity(t *testing.T) {
	// The attribute src="x&gt;y.js" contains the raw bytes '&','g','t',';' — no
	// raw '>' byte — so the first '>' in the line is the tag-closing one.
	src := `<script lang="ts" src="x&gt;y.js">let x: number = 1;</script>`
	vs := ExtractSvelte([]byte(src))
	code := string(vs.Code)
	if code != "let x: number = 1;" {
		t.Errorf("AttrGtEntity: Code = %q, want %q", code, "let x: number = 1;")
	}
}

// TestExtractSvelte_AttrLiteralGt documents that a literal '>' byte inside an
// attribute value fools the raw-byte scanner: it picks up the attribute-internal
// '>' as the tag close. This is a known limitation of the byte scanner — it does
// not parse HTML attributes.
// Pins current limited behavior. If the scanner gains escape handling, update this assertion.
func TestExtractSvelte_AttrLiteralGt(t *testing.T) {
	// The scanner sees the first '>' inside "<<<>>>" as the tag close, so content
	// starts after that '>', yielding the remaining attr garbage plus the real JS.
	src := `<script title="<<<>>>" src="ok.js">let x = 1;</script>`
	vs := ExtractSvelte([]byte(src))
	// Current behavior: scanner picks up '>' inside attr value; code starts with '>'.
	wantCode := `>>" src="ok.js">let x = 1;`
	if string(vs.Code) != wantCode {
		t.Errorf("AttrLiteralGt: Code = %q, want %q", string(vs.Code), wantCode)
	}
}

// TestExtractSvelte_BoundSingleLine verifies that a <script> whose opening tag
// spans two lines (i.e. the closing '>' is on the second line) is not extracted.
// The bound logic caps the '>' search to the first newline; since no '>' appears
// on the same line as "<script", gtIdx < 0 → break → zero scripts returned.
// Removing the newline-cap logic would allow the scanner to find '>' on the next
// line and incorrectly extract the block.
func TestExtractSvelte_BoundSingleLine(t *testing.T) {
	// Opening tag spans two lines — '>' is on line 2, not line 1.
	src := "<script\nsrc=\"really-long-attr\">\nlet x = 1;\n</script>\n"
	vs := ExtractSvelte([]byte(src))
	code := string(vs.Code)
	if code != "" {
		t.Errorf("BoundSingleLine: expected empty Code (multi-line open tag), got %q", code)
	}
}

// TestExtractSvelte_BoundMaxBytes verifies that a malformed <script> tag whose
// attribute value stretches beyond 512 bytes without a newline or '>' does not
// cause the scanner to run away past the bound. The scanner hits the 512-byte
// limit, finds no '>', and breaks. Removing the 512-byte cap would let the
// scanner scan arbitrarily far and eventually find the '>' after the long pad.
func TestExtractSvelte_BoundMaxBytes(t *testing.T) {
	// Construct: <script src="<600 bytes of 'a'>">let x = 1;</script>
	// The opening tag has no newline and no '>' within the first 512 bytes.
	pad := strings.Repeat("a", tagOpenScanLimit+88) // pad past the bound
	src := `<script src="` + pad + `">let x = 1;</script>`
	vs := ExtractSvelte([]byte(src))
	code := string(vs.Code)
	if code != "" {
		t.Errorf("BoundMaxBytes: expected empty Code (no '>' within 512 bytes), got %q", code)
	}
}

func TestExtractSvelte_OffsetLineMap(t *testing.T) {
	// prefix = 3 lines; <script> tag is on line 4, content \n is still line 4.
	prefix := "line1\nline2\nline3\n"               // lines 1-3
	script := "<script>\nconst a = 1;\n</script>\n" // <script> on line 4
	src := prefix + script
	vs := ExtractSvelte([]byte(src))

	// contentStart is the \n after '>' on line 4 → maps to 4.
	assertLineMapAt(t, "offset", vs, 1, 4)
	// After that \n → line 5 (const a = 1;).
	assertLineMapAt(t, "offset", vs, 2, 5)
}

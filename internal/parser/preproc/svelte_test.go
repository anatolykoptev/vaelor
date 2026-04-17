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

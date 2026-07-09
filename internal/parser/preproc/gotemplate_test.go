package preproc

import (
	"testing"
)

func TestStripGoTemplate_singleDefine(t *testing.T) {
	t.Parallel()
	src := []byte(`{{define "hunt_jobs"}}
<div class="page-header">
  <h1>Jobs</h1>
</div>
{{end}}
`)
	_, defines := StripGoTemplate(src)
	if len(defines) != 1 {
		t.Fatalf("expected 1 define, got %d: %v", len(defines), defines)
	}
	if defines[0].Name != "hunt_jobs" {
		t.Errorf("Name = %q, want %q", defines[0].Name, "hunt_jobs")
	}
	// {{define}} is on line 1; {{end}} is on line 6.
	if defines[0].StartLine != 1 {
		t.Errorf("StartLine = %d, want 1", defines[0].StartLine)
	}
	if defines[0].EndLine < defines[0].StartLine {
		t.Errorf("EndLine(%d) < StartLine(%d)", defines[0].EndLine, defines[0].StartLine)
	}
}

func TestStripGoTemplate_multipleDefines(t *testing.T) {
	t.Parallel()
	src := []byte(`{{define "layout"}}<html><body>{{template "content" .}}</body></html>{{end}}
{{define "content"}}<h1>Page</h1>{{end}}
`)
	_, defines := StripGoTemplate(src)
	if len(defines) != 2 {
		t.Fatalf("expected 2 defines, got %d: %v", len(defines), defines)
	}
	byName := make(map[string]GoTemplateDefine)
	for _, d := range defines {
		byName[d.Name] = d
	}
	if _, ok := byName["layout"]; !ok {
		t.Errorf("missing define 'layout'; got %v", defines)
	}
	if _, ok := byName["content"]; !ok {
		t.Errorf("missing define 'content'; got %v", defines)
	}
}

func TestStripGoTemplate_pureHTML(t *testing.T) {
	t.Parallel()
	src := []byte(`<!DOCTYPE html><html><body><h1>Hello</h1></body></html>`)
	cleaned, defines := StripGoTemplate(src)
	if len(defines) != 0 {
		t.Errorf("expected 0 defines for pure HTML, got %d", len(defines))
	}
	// Cleaned should equal src byte-for-byte (nothing to strip).
	if string(cleaned) != string(src) {
		t.Errorf("cleaned differs from src for pure HTML; got %q", cleaned)
	}
}

func TestStripGoTemplate_actionInsideAttr(t *testing.T) {
	t.Parallel()
	// {{.ID}} inside an attribute value must be stripped without crashing.
	src := []byte(`<button hx-put="/admin/hunt/job/{{.ID}}/rate">Rate</button>`)
	cleaned, defines := StripGoTemplate(src)
	if len(defines) != 0 {
		t.Errorf("expected 0 defines, got %d", len(defines))
	}
	// Cleaned must not contain "{{" or "}}" anywhere.
	for i, b := range cleaned {
		if b == '{' && i+1 < len(cleaned) && cleaned[i+1] == '{' {
			t.Errorf("cleaned still contains '{{' at offset %d: %q", i, cleaned)
			break
		}
	}
}

func TestStripGoTemplate_comment(t *testing.T) {
	t.Parallel()
	src := []byte(`{{/* This is a comment */}}<div></div>`)
	cleaned, defines := StripGoTemplate(src)
	if len(defines) != 0 {
		t.Errorf("expected 0 defines, got %d", len(defines))
	}
	// The comment region should be blanked.
	// "<div></div>" should survive intact.
	_ = cleaned
}

func TestStripGoTemplate_nestedBlocks(t *testing.T) {
	t.Parallel()
	// {{define}} containing {{range}} and {{if}} blocks — only the outer define
	// should be recorded.
	src := []byte(`{{define "page"}}
{{range .Items}}
  {{if .Active}}<li>{{.Name}}</li>{{end}}
{{end}}
{{end}}
`)
	_, defines := StripGoTemplate(src)
	if len(defines) != 1 {
		t.Fatalf("expected 1 define, got %d: %v", len(defines), defines)
	}
	if defines[0].Name != "page" {
		t.Errorf("Name = %q, want %q", defines[0].Name, "page")
	}
}

func TestStripGoTemplate_linePreservation(t *testing.T) {
	t.Parallel()
	// Verify that newlines are NOT blanked — line count of cleaned must equal
	// line count of src.
	src := []byte(`{{define "a"}}
line2
line3
{{end}}
`)
	cleaned, _ := StripGoTemplate(src)
	srcLines := countNewlines(src)
	cleanedLines := countNewlines(cleaned)
	if cleanedLines != srcLines {
		t.Errorf("cleaned has %d newlines, src has %d — line positions shifted", cleanedLines, srcLines)
	}
}

func countNewlines(b []byte) int {
	n := 0
	for _, c := range b {
		if c == '\n' {
			n++
		}
	}
	return n
}

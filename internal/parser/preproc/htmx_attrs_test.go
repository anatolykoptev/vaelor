package preproc

import "testing"

// TestScanHtmxRefs_basicGet verifies a single hx-get attribute is extracted
// with correct method, URL, and line number.
func TestScanHtmxRefs_basicGet(t *testing.T) {
	t.Parallel()
	src := []byte(`<button hx-get="/admin/contacts">Load</button>`)
	refs := ScanHtmxRefs(src)
	if len(refs) != 1 {
		t.Fatalf("ScanHtmxRefs: got %d refs, want 1", len(refs))
	}
	if refs[0].Method != "GET" {
		t.Errorf("Method = %q, want %q", refs[0].Method, "GET")
	}
	if refs[0].URL != "/admin/contacts" {
		t.Errorf("URL = %q, want %q", refs[0].URL, "/admin/contacts")
	}
	if refs[0].StartLine != 1 {
		t.Errorf("StartLine = %d, want 1", refs[0].StartLine)
	}
}

// TestScanHtmxRefs_allVerbs verifies all five HTTP verbs are correctly mapped
// from their hx-* attribute names to uppercase method strings.
func TestScanHtmxRefs_allVerbs(t *testing.T) {
	t.Parallel()
	src := []byte(`<button hx-get="/a">A</button>
<button hx-post="/b">B</button>
<button hx-put="/c">C</button>
<button hx-delete="/d">D</button>
<button hx-patch="/e">E</button>`)
	refs := ScanHtmxRefs(src)
	if len(refs) != 5 {
		t.Fatalf("ScanHtmxRefs: got %d refs, want 5", len(refs))
	}
	want := []struct {
		method string
		url    string
		line   int
	}{
		{"GET", "/a", 1},
		{"POST", "/b", 2},
		{"PUT", "/c", 3},
		{"DELETE", "/d", 4},
		{"PATCH", "/e", 5},
	}
	for i, w := range want {
		if refs[i].Method != w.method {
			t.Errorf("refs[%d].Method = %q, want %q", i, refs[i].Method, w.method)
		}
		if refs[i].URL != w.url {
			t.Errorf("refs[%d].URL = %q, want %q", i, refs[i].URL, w.url)
		}
		if refs[i].StartLine != w.line {
			t.Errorf("refs[%d].StartLine = %d, want %d", i, refs[i].StartLine, w.line)
		}
	}
}

// TestScanHtmxRefs_goTemplateInUrl verifies that Go template expressions
// inside hx-* URLs are preserved as-is in the returned HtmxRef.URL so that
// the route normaliser can collapse them to wildcards.
func TestScanHtmxRefs_goTemplateInUrl(t *testing.T) {
	t.Parallel()
	src := []byte(`<button hx-put="/admin/hunt/job/{{.ID}}/rate">Rate</button>
<a hx-get="/admin/hunt/jobs?page={{add .Page 1}}">Next</a>`)
	refs := ScanHtmxRefs(src)
	if len(refs) != 2 {
		t.Fatalf("ScanHtmxRefs: got %d refs, want 2", len(refs))
	}
	if refs[0].Method != "PUT" {
		t.Errorf("refs[0].Method = %q, want PUT", refs[0].Method)
	}
	if refs[0].URL != "/admin/hunt/job/{{.ID}}/rate" {
		t.Errorf("refs[0].URL = %q, want /admin/hunt/job/{{.ID}}/rate", refs[0].URL)
	}
	if refs[1].Method != "GET" {
		t.Errorf("refs[1].Method = %q, want GET", refs[1].Method)
	}
	// URL with query string — template action preserved raw.
	if refs[1].URL != "/admin/hunt/jobs?page={{add .Page 1}}" {
		t.Errorf("refs[1].URL = %q, want /admin/hunt/jobs?page={{add .Page 1}}", refs[1].URL)
	}
}

// TestScanHtmxRefs_inlineJS verifies that hx-on::after-request (JavaScript
// event handler, not a URL-emitting attribute) produces zero refs.
func TestScanHtmxRefs_inlineJS(t *testing.T) {
	t.Parallel()
	src := []byte(`<button hx-on::after-request="if (event.detail.successful) location.reload();">X</button>`)
	refs := ScanHtmxRefs(src)
	if len(refs) != 0 {
		t.Errorf("ScanHtmxRefs: got %d refs, want 0 (hx-on is not URL-emitting)", len(refs))
	}
}

// TestScanHtmxRefs_scriptStyleSkip verifies that hx-* attributes inside
// <script> and <style> blocks are not extracted; only the real DOM attribute
// is returned.
func TestScanHtmxRefs_scriptStyleSkip(t *testing.T) {
	t.Parallel()
	src := []byte(`<script>var s = '<button hx-get="/fake">x</button>';</script>
<style>.x { content: '<button hx-get="/fake">'; }</style>
<button hx-get="/real">X</button>`)
	refs := ScanHtmxRefs(src)
	if len(refs) != 1 {
		t.Fatalf("ScanHtmxRefs: got %d refs, want 1; refs = %v", len(refs), refs)
	}
	if refs[0].URL != "/real" {
		t.Errorf("URL = %q, want /real", refs[0].URL)
	}
}

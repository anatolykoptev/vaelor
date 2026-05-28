package routes

import "testing"

// TestMatchHTML_emitRoutes verifies that ExtractAll("html", ...) returns
// client-side htmx routes with correct Side, Framework, Method, and Path
// fields for a typical Go template file with hx-get and hx-post attributes.
func TestMatchHTML_emitRoutes(t *testing.T) {
	t.Parallel()
	src := []byte(`{{define "contacts"}}
<button hx-get="/admin/contacts" hx-target="#main">Load</button>
<button hx-post="/admin/contacts/save" hx-target="#main">Save</button>
{{end}}`)

	result := ExtractAll("html", src)
	if len(result) != 2 {
		t.Fatalf("ExtractAll(html): got %d routes, want 2; routes = %v", len(result), result)
	}

	r0 := result[0]
	if r0.Side != "client" {
		t.Errorf("routes[0].Side = %q, want client", r0.Side)
	}
	if r0.Framework != "htmx" {
		t.Errorf("routes[0].Framework = %q, want htmx", r0.Framework)
	}
	if r0.Method != "GET" {
		t.Errorf("routes[0].Method = %q, want GET", r0.Method)
	}
	if r0.Path != "/admin/contacts" {
		t.Errorf("routes[0].Path = %q, want /admin/contacts", r0.Path)
	}

	r1 := result[1]
	if r1.Side != "client" {
		t.Errorf("routes[1].Side = %q, want client", r1.Side)
	}
	if r1.Framework != "htmx" {
		t.Errorf("routes[1].Framework = %q, want htmx", r1.Framework)
	}
	if r1.Method != "POST" {
		t.Errorf("routes[1].Method = %q, want POST", r1.Method)
	}
	if r1.Path != "/admin/contacts/save" {
		t.Errorf("routes[1].Path = %q, want /admin/contacts/save", r1.Path)
	}
}

// TestMatchHTML_pathNormalisationMatchesGo verifies that a Go template action
// {{.ID}} in an htmx URL produces the same normalised path as the Go-side
// server route {id} in mux pattern syntax. Both must use NormalizePath's
// brace-to-wildcard convention: /admin/hunt/job/*/rate.
func TestMatchHTML_pathNormalisationMatchesGo(t *testing.T) {
	t.Parallel()

	// htmx client side: hx-put with Go template action in the path.
	htmxSrc := []byte(`<button hx-put="/admin/hunt/job/{{.ID}}/rate">Rate</button>`)
	htmxRoutes := ExtractAll("html", htmxSrc)
	if len(htmxRoutes) != 1 {
		t.Fatalf("ExtractAll(html): got %d routes, want 1", len(htmxRoutes))
	}

	// Go server side: Go 1.22+ mux pattern with {id} placeholder.
	// http.HandleFunc("PUT /admin/hunt/job/{id}/rate", handler)
	goSrc := []byte(`http.HandleFunc("PUT /admin/hunt/job/{id}/rate", handleRate)`)
	goRoutes := ExtractAll("go", goSrc)
	if len(goRoutes) != 1 {
		t.Fatalf("ExtractAll(go): got %d routes, want 1", len(goRoutes))
	}

	htmxPath := htmxRoutes[0].Path
	goPath := goRoutes[0].Path
	if htmxPath != goPath {
		t.Errorf("path mismatch: htmx=%q go=%q — both should normalise to the same wildcard form",
			htmxPath, goPath)
	}
}

// TestMatchHTML_pathConditionalCollapse verifies that a balanced {{if}}...{{end}}
// block in an htmx URL is collapsed to a single "*", not one "*" per action.
// MAJOR-2 regression contract: before the fix, /x/{{if .Y}}foo{{else}}bar{{end}}/y
// incorrectly became /x/*foo*bar*/y (three "*" tokens + literal text).
func TestMatchHTML_pathConditionalCollapse(t *testing.T) {
	t.Parallel()
	src := []byte(`<button hx-get="/x/{{if .Y}}foo{{else}}bar{{end}}/y">X</button>`)
	got := ExtractAll("html", src)
	if len(got) != 1 {
		t.Fatalf("ExtractAll(html): got %d routes, want 1; routes = %v", len(got), got)
	}
	const want = "/x/*/y"
	if got[0].Path != want {
		t.Errorf("Path = %q, want %q — {{if}}...{{end}} must collapse to a single *", got[0].Path, want)
	}
}

// TestMatchHTML_pathRangeCollapse verifies that a balanced {{range}}...{{end}}
// block in an htmx URL is collapsed to a single "*".
func TestMatchHTML_pathRangeCollapse(t *testing.T) {
	t.Parallel()
	src := []byte(`<button hx-get="/items/{{range .IDs}}{{.}}{{end}}/details">X</button>`)
	got := ExtractAll("html", src)
	if len(got) != 1 {
		t.Fatalf("ExtractAll(html): got %d routes, want 1; routes = %v", len(got), got)
	}
	const want = "/items/*/details"
	if got[0].Path != want {
		t.Errorf("Path = %q, want %q — {{range}}...{{end}} must collapse to a single *", got[0].Path, want)
	}
}

// TestMatchHTML_queryStringParity documents the query-string parity contract:
// the "?" portion of an htmx URL is preserved in Route.Path (with template
// actions inside the query string collapsed to "*"). Server-side mux routes
// never include query strings, so cross-stack comparison must use path-prefix
// matching (Phase B responsibility). This test locks the current behaviour.
func TestMatchHTML_queryStringParity(t *testing.T) {
	t.Parallel()
	src := []byte(`<button hx-get="/search?q={{.Query}}&page={{add .Page 1}}">Search</button>`)
	got := ExtractAll("html", src)
	if len(got) != 1 {
		t.Fatalf("ExtractAll(html): got %d routes, want 1; routes = %v", len(got), got)
	}
	// Query string is preserved; template actions inside it are collapsed to *.
	// Phase B must compare path prefixes only (strip "?" and everything after).
	const want = "/search?q=*&page=*"
	if got[0].Path != want {
		t.Errorf("Path = %q, want %q — query string must be preserved with actions collapsed", got[0].Path, want)
	}
}

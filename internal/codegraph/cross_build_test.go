package codegraph

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anatolykoptev/go-code/internal/ingest"
	"github.com/anatolykoptev/go-code/internal/parser"
	"github.com/anatolykoptev/go-code/internal/polyglot"
	"github.com/anatolykoptev/go-code/internal/routes"
)

func TestBuildLayerVertices(t *testing.T) {
	t.Parallel()

	layers := []polyglot.Layer{
		{Name: "backend", Role: "server", RootDir: "backend", Language: "go", Files: 20},
		{Name: "frontend", Role: "client", RootDir: "frontend", Language: "typescript", Files: 15},
	}

	vertices, _ := buildCrossLanguageGraph("", layers, nil, nil, nil)

	layerCount := 0
	for _, v := range vertices {
		if v.Label == "Layer" {
			layerCount++
		}
	}

	if layerCount != 2 {
		t.Errorf("expected 2 Layer vertices, got %d", layerCount)
	}

	// Verify props on first Layer vertex.
	found := false
	for _, v := range vertices {
		if v.Label == "Layer" && v.Props["name"] == "backend" {
			found = true
			if v.Props["role"] != "server" {
				t.Errorf("expected role=server, got %q", v.Props["role"])
			}
			if v.Props["language"] != "go" {
				t.Errorf("expected language=go, got %q", v.Props["language"])
			}
			if v.Props["root_dir"] != "backend" {
				t.Errorf("expected root_dir=backend, got %q", v.Props["root_dir"])
			}
		}
	}
	if !found {
		t.Error("Layer vertex 'backend' not found")
	}
}

func TestBuildRouteVerticesAndEdges(t *testing.T) {
	t.Parallel()

	routeList := []routes.Route{
		{
			Method:    "GET",
			Path:      "/api/users",
			Handler:   "handleGetUsers",
			Framework: "chi",
			File:      "backend/handler.go",
			Line:      10,
			Side:      "server",
		},
		{
			Method:    "GET",
			Path:      "/api/users",
			Handler:   "fetchUsers",
			Framework: "fetch",
			File:      "frontend/api.ts",
			Line:      25,
			Side:      "client",
		},
	}

	vertices, edges := buildCrossLanguageGraph("", nil, routeList, nil, nil)

	// Should have 1 deduplicated Route vertex (same method+path).
	routeVertexCount := 0
	for _, v := range vertices {
		if v.Label == "Route" {
			routeVertexCount++
		}
	}
	if routeVertexCount != 1 {
		t.Errorf("expected 1 Route vertex (deduplicated), got %d", routeVertexCount)
	}

	// Should have 1 HANDLES + 1 FETCHES edge.
	handlesCount := 0
	fetchesCount := 0
	for _, e := range edges {
		switch e.EdgeLabel {
		case "HANDLES":
			handlesCount++
			want := "handleGetUsers" + compositeKeyDelim + "backend/handler.go"
			if e.FromKey != want {
				t.Errorf("HANDLES FromKey = %q, want %q", e.FromKey, want)
			}
			wantToKey := "GET" + compositeKeyDelim + "/api/users"
			if e.ToKey != wantToKey {
				t.Errorf("HANDLES ToKey = %q, want %q", e.ToKey, wantToKey)
			}
		case "FETCHES":
			fetchesCount++
			// FETCHES uses composite "handler\x00file" key so buildEdgeUnwindBatch
			// can pre-split it into the Symbol's name and file properties.
			want := "fetchUsers" + compositeKeyDelim + "frontend/api.ts"
			if e.FromKey != want {
				t.Errorf("FETCHES FromKey = %q, want %q", e.FromKey, want)
			}
		}
	}

	if handlesCount != 1 {
		t.Errorf("expected 1 HANDLES edge, got %d", handlesCount)
	}
	if fetchesCount != 1 {
		t.Errorf("expected 1 FETCHES edge, got %d", fetchesCount)
	}
}

// TestHtmxFetchesEdgeFromKey verifies that FETCHES edges for htmx (client-side)
// routes use the composite "handler:file" Symbol key so that AGE's
// unwindEdgeMatch("Symbol", "fk") can split it into name + file properties.
// Root cause: index_layers.go was passing r.Handler (bare name) which caused
// split("hunt_jobs", ':')[1] → NULL → MATCH (f:Symbol {file: NULL}) → no match.
func TestHtmxFetchesEdgeFromKey(t *testing.T) {
	t.Parallel()

	route := routes.Route{
		Method:    "GET",
		Path:      "/admin/hunt/jobs",
		Framework: "htmx",
		Side:      "client",
		Handler:   "hunt_jobs",
		File:      "internal/admin/templates/hunt_jobs.html",
		Line:      12,
	}

	got := htmxFetchesFromKey(route)
	want := "hunt_jobs" + compositeKeyDelim + "internal/admin/templates/hunt_jobs.html"
	if got != want {
		t.Errorf("htmxFetchesFromKey = %q, want %q", got, want)
	}
}

// TestHtmxFetchesEdgeFromKey_EmptyHandler verifies that an empty Handler
// returns an empty string (callers skip the edge when FromKey is "").
func TestHtmxFetchesEdgeFromKey_EmptyHandler(t *testing.T) {
	t.Parallel()

	route := routes.Route{
		Method:    "GET",
		Path:      "/admin/hunt/jobs",
		Framework: "htmx",
		Side:      "client",
		Handler:   "",
		File:      "internal/admin/templates/hunt_jobs.html",
	}

	got := htmxFetchesFromKey(route)
	if got != "" {
		t.Errorf("htmxFetchesFromKey with empty Handler = %q, want \"\"", got)
	}
}

// TestBuildCrossLanguageGraph_HtmxFetchesCompositeKey verifies that
// buildCrossLanguageGraph produces a FETCHES edge whose FromKey is
// the composite "handler:file" form (not bare handler name).
// This is what AGE's unwindEdgeMatch("Symbol", "fk") requires.
func TestBuildCrossLanguageGraph_HtmxFetchesCompositeKey(t *testing.T) {
	t.Parallel()

	routeList := []routes.Route{
		{
			Method:    "GET",
			Path:      "/admin/hunt/jobs",
			Framework: "htmx",
			Side:      "client",
			Handler:   "hunt_jobs",
			File:      "internal/admin/templates/hunt_jobs.html",
			Line:      12,
		},
	}

	_, edges := buildCrossLanguageGraph("", nil, routeList, nil, nil)

	var fetchEdge *edgeData
	for i := range edges {
		if edges[i].EdgeLabel == "FETCHES" {
			e := edges[i]
			fetchEdge = &e
			break
		}
	}
	if fetchEdge == nil {
		t.Fatal("expected 1 FETCHES edge, got 0")
	}

	want := "hunt_jobs" + compositeKeyDelim + "internal/admin/templates/hunt_jobs.html"
	if fetchEdge.FromKey != want {
		t.Errorf("FETCHES FromKey = %q, want %q", fetchEdge.FromKey, want)
	}
}

// TestHandlesEdgeFromKey verifies that HANDLES edges for Go (server-side)
// routes use the composite "handler:file" Symbol key so that AGE's
// unwindEdgeMatch("Symbol", "fk") can split it into name + file properties.
// Root cause: index_layers.go was passing r.Handler (bare name) which caused
// split("handleHuntJobs", ':')[1] → NULL → MATCH (s:Symbol {file: NULL}) → no match.
// go-nerv pattern: handler defined in same file as registration (internal/admin/handler.go).
func TestHandlesEdgeFromKey(t *testing.T) {
	t.Parallel()

	route := routes.Route{
		Method:    "GET",
		Path:      "/admin/hunt/jobs",
		Framework: "go",
		Side:      "server",
		Handler:   "handleHuntJobsList",
		File:      "internal/admin/handler.go",
		Line:      42,
	}

	got := handlesFromKey(route)
	want := "handleHuntJobsList" + compositeKeyDelim + "internal/admin/handler.go"
	if got != want {
		t.Errorf("handlesFromKey = %q, want %q", got, want)
	}
}

// TestHandlesEdgeFromKey_EmptyHandler verifies that an empty Handler
// returns an empty string (callers skip the edge when FromKey is "").
func TestHandlesEdgeFromKey_EmptyHandler(t *testing.T) {
	t.Parallel()

	route := routes.Route{
		Method:    "GET",
		Path:      "/admin/hunt/jobs",
		Framework: "go",
		Side:      "server",
		Handler:   "",
		File:      "internal/admin/handler.go",
	}

	got := handlesFromKey(route)
	if got != "" {
		t.Errorf("handlesFromKey with empty Handler = %q, want \"\"", got)
	}
}

// TestBuildCrossLanguageGraph_HandlesCompositeKey verifies that
// buildCrossLanguageGraph produces a HANDLES edge whose FromKey is
// the composite "handler:file" form (not bare handler name).
// This is what AGE's unwindEdgeMatch("Symbol", "fk") requires.
// Constraint: assumes handler defined in same file as route registration (typical Go pattern).
func TestBuildCrossLanguageGraph_HandlesCompositeKey(t *testing.T) {
	t.Parallel()

	routeList := []routes.Route{
		{
			Method:    "GET",
			Path:      "/admin/hunt/jobs",
			Framework: "go",
			Side:      "server",
			Handler:   "handleHuntJobsList",
			File:      "internal/admin/handler.go",
			Line:      42,
		},
	}

	_, edges := buildCrossLanguageGraph("", nil, routeList, nil, nil)

	var handlesEdge *edgeData
	for i := range edges {
		if edges[i].EdgeLabel == "HANDLES" {
			e := edges[i]
			handlesEdge = &e
			break
		}
	}
	if handlesEdge == nil {
		t.Fatal("expected 1 HANDLES edge, got 0")
	}

	want := "handleHuntJobsList" + compositeKeyDelim + "internal/admin/handler.go"
	if handlesEdge.FromKey != want {
		t.Errorf("HANDLES FromKey = %q, want %q", handlesEdge.FromKey, want)
	}
}

func TestMatchKeyLayer(t *testing.T) {
	t.Parallel()

	got := matchKey("Layer", "backend")
	want := "name: 'backend'"
	if got != want {
		t.Errorf("matchKey(Layer, backend) = %q, want %q", got, want)
	}
}

func TestMatchKeyRoute(t *testing.T) {
	t.Parallel()

	key := "GET" + compositeKeyDelim + "/api/users"
	got := matchKey("Route", key)
	if !strings.Contains(got, "method: 'GET'") {
		t.Errorf("matchKey(Route) = %q, missing method: 'GET'", got)
	}
	if !strings.Contains(got, "path: '/api/users'") {
		t.Errorf("matchKey(Route) = %q, missing path: '/api/users'", got)
	}
	// The NUL delimiter must not appear in the Cypher output.
	if strings.Contains(got, "\x00") {
		t.Errorf("matchKey(Route) leaked \\x00 into Cypher: %q", got)
	}
}

// TestBuildCrossLanguageData_AbsoluteSymbolFileResolves is an integration test
// for the buildCrossLanguageData → buildFileSymbols → relFileSymbols re-key seam.
//
// The existing unit tests hand-build fileSymbols with already-relative keys and
// call buildCrossLanguageGraph directly, bypassing the re-key loop. This test
// drives the real buildCrossLanguageData entrypoint so that:
//
//  1. parser.Symbol.File is an ABSOLUTE path (as produced by the real parser).
//  2. routes.Route.File is a RELATIVE path (as produced by extractRoutes).
//  3. The re-key loop (relPath(absPath, root)) must bridge the two — a divergence
//     in how routes vs symbols derive their paths causes every empty-Handler route
//     to silently drop while looking "explained" (route_handler_unresolved bumped).
//
// Mutation proof: keying relFileSymbols by the absolute path instead of the
// relative path causes resolveEnclosingSymbol to miss the lookup (route.File is
// relative, key is absolute → no match → unresolved) and the new test fails.
func TestBuildCrossLanguageData_AbsoluteSymbolFileResolves(t *testing.T) {
	// Build a real temp directory tree so extractRoutes can actually read the file.
	root := t.TempDir()
	const relFile = "src/routes.ts"
	// Arrow-callback route inside a named function — Handler will be empty after
	// extraction (TS arrow callbacks don't capture a named handler), so the
	// enclosing-fn resolver must fire.
	src := `import express from 'express';
function setupRoutes(app) {
  app.get('/api/users', (req, res) => {
    res.json([]);
  });
}
`
	absPath := writeTestFile(t, root, relFile, src)

	files := []*ingest.File{
		{
			Path:     absPath, // absolute — matches real ingest output
			RelPath:  relFile, // relative — set by ingest at construction time
			Language: "typescript",
		},
	}

	// Symbol with ABSOLUTE File path — as produced by the real parser.
	symbols := []*parser.Symbol{
		{
			Name:      "setupRoutes",
			Kind:      parser.KindFunction,
			File:      absPath, // absolute — the seam being tested
			StartLine: 2,
			EndLine:   6,
		},
	}

	vertices, edges := buildCrossLanguageData(root, files, symbols)

	// Sanity: at least one Route vertex for /api/users.
	var routeVertex *vertexData
	for i := range vertices {
		if vertices[i].Label == "Route" && vertices[i].Props["path"] == "/api/users" {
			v := vertices[i]
			routeVertex = &v
			break
		}
	}
	if routeVertex == nil {
		t.Fatalf("expected Route vertex for /api/users; vertices=%v", vertices)
	}

	// Core assertion: a HANDLES edge must be built whose FromKey is
	// "setupRoutes:src/routes.ts" — the relative form. This proves that
	// Symbol.File (absolute) was re-keyed to relative and matched route.File
	// (already relative). If the re-key loop used the absolute path as key,
	// resolveEnclosingSymbol would return ("", false) and no edge would be built.
	wantFromKey := "setupRoutes" + compositeKeyDelim + relFile
	var handlesEdge *edgeData
	for i := range edges {
		if edges[i].EdgeLabel == "HANDLES" && edges[i].FromKey == wantFromKey {
			e := edges[i]
			handlesEdge = &e
			break
		}
	}
	if handlesEdge == nil {
		t.Errorf("HANDLES edge with FromKey %q not found; edges=%v\n"+
			"This means the absolute→relative re-key seam in buildCrossLanguageData "+
			"is broken: Symbol.File (absolute) was not normalised to relative before "+
			"being looked up against route.File (relative).",
			wantFromKey, edges)
	}
}

// writeTestFile creates a file at dir/rel with the given content.
func writeTestFile(t *testing.T, dir, rel, content string) string {
	t.Helper()
	p := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestExtractRoutes_JunkPathDropped verifies that a TypeScript file containing
// only junk-path routes (headers.get / bare hex token) produces zero routes
// after the receiver allow-list filter in the TS matcher.
// Note: query-string paths still produce a client route from fetch() — they are
// dropped at the ingest guard (extractRoutes junk filter), not in the matcher.
func TestExtractRoutes_JunkPathDropped(t *testing.T) {
	root := t.TempDir()
	absPath := writeTestFile(t, root, "src/api.ts",
		`// junk: non-router receiver
const token = headers.get('Authorization');
const v = map.get('/A');
`)
	f := &ingest.File{
		Path:     absPath,
		RelPath:  "src/api.ts",
		Language: "typescript",
	}

	got := extractRoutes(root, []*ingest.File{f}, "test-repo")
	if len(got) != 0 {
		t.Errorf("extractRoutes: expected 0 routes from junk-only file, got %d: %+v", len(got), got)
	}
}

// TestExtractRoutes_TestFileSkipped verifies that a *.test.ts file is entirely
// skipped by extractRoutes (the test-file guard), and that the rejection counter
// is bumped with reason "test_file".
func TestExtractRoutes_TestFileSkipped(t *testing.T) {
	root := t.TempDir()
	absPath := writeTestFile(t, root, "src/api.test.ts",
		`// test fixture — should be skipped at ingest
app.get('/api/partner/register', handler);
`)
	f := &ingest.File{
		Path:     absPath,
		RelPath:  "src/api.test.ts",
		Language: "typescript",
	}

	// Read the rejection counter before to detect the increment.
	rejectBefore := readCounter(t,
		routeRejectedTotal.WithLabelValues("test-repo", "test_file"))

	got := extractRoutes(root, []*ingest.File{f}, "test-repo")
	if len(got) != 0 {
		t.Errorf("extractRoutes: expected 0 routes from test file, got %d", len(got))
	}

	rejectAfter := readCounter(t,
		routeRejectedTotal.WithLabelValues("test-repo", "test_file"))
	if rejectAfter-rejectBefore < 1 {
		t.Errorf("routeRejectedTotal{test_file} did not increment: before=%.0f after=%.0f",
			rejectBefore, rejectAfter)
	}
}

// TestExtractRoutes_QueryStringJunkDropped verifies that a route whose path
// contains '?' is dropped by the ingest junk filter (not the matcher — fetch()
// still captures it as a client route, but extractRoutes post-filters it).
func TestExtractRoutes_QueryStringJunkDropped(t *testing.T) {
	root := t.TempDir()
	absPath := writeTestFile(t, root, "src/xss_fixture.ts",
		`// XSS fixture
const r = await fetch('/api/leak?c='+cookie);
`)
	f := &ingest.File{
		Path:     absPath,
		RelPath:  "src/xss_fixture.ts",
		Language: "typescript",
	}

	rejectBefore := readCounter(t,
		routeRejectedTotal.WithLabelValues("test-repo", "junk"))

	got := extractRoutes(root, []*ingest.File{f}, "test-repo")
	// The fetch() call still produces a client route in Match(), but extractRoutes
	// must drop it because the path is junk (/api/leak?c=).
	for _, r := range got {
		if strings.Contains(r.RawPath, "?") {
			t.Errorf("junk query-string route survived extractRoutes: %+v", r)
		}
	}

	rejectAfter := readCounter(t,
		routeRejectedTotal.WithLabelValues("test-repo", "junk"))
	if rejectAfter-rejectBefore < 1 {
		t.Errorf("routeRejectedTotal{junk} did not increment: before=%.0f after=%.0f",
			rejectBefore, rejectAfter)
	}
}

// TestResolveEnclosingSymbol_Innermost verifies that the resolver returns the
// innermost (smallest-span) function symbol whose [startLine, endLine] range
// contains the query line, and falls back to the outer function when the query
// line is outside the inner span.
func TestResolveEnclosingSymbol_Innermost(t *testing.T) {
	t.Parallel()

	// outer: lines 1-20, inner: lines 5-10.
	fileSymbols := map[string][]symbolSpan{
		"src/handler.ts": {
			{name: "outerFn", startLine: 1, endLine: 20},
			{name: "innerFn", startLine: 5, endLine: 10},
		},
	}

	// Line 7 is inside both spans — must return inner (smaller span).
	got, ok := resolveEnclosingSymbol(fileSymbols, "src/handler.ts", 7)
	if !ok {
		t.Fatal("resolveEnclosingSymbol: expected ok=true for line 7, got false")
	}
	if got != "innerFn" {
		t.Errorf("resolveEnclosingSymbol(line=7) = %q, want innerFn", got)
	}

	// Line 15 is inside outer only — must return outer.
	got, ok = resolveEnclosingSymbol(fileSymbols, "src/handler.ts", 15)
	if !ok {
		t.Fatal("resolveEnclosingSymbol: expected ok=true for line 15, got false")
	}
	if got != "outerFn" {
		t.Errorf("resolveEnclosingSymbol(line=15) = %q, want outerFn", got)
	}

	// Line 25 is outside all spans — must return ok=false.
	_, ok = resolveEnclosingSymbol(fileSymbols, "src/handler.ts", 25)
	if ok {
		t.Errorf("resolveEnclosingSymbol(line=25) = ok=true, want false")
	}
}

// TestBuildCrossLang_ArrowCallbackResolvesEnclosingFn verifies that a TS route
// with an empty Handler and a Line inside an enclosing function's span produces
// a HANDLES edge whose FromKey is "<enclosingFn>:<file>" (hybrid resolver path).
func TestBuildCrossLang_ArrowCallbackResolvesEnclosingFn(t *testing.T) {
	t.Parallel()

	routeList := []routes.Route{
		{
			Method:    "GET",
			Path:      "/api/items",
			Handler:   "", // empty — arrow callback, no named handler captured
			Framework: "express",
			Side:      "server",
			File:      "src/routes.ts",
			Line:      8,
		},
	}
	fileSymbols := map[string][]symbolSpan{
		"src/routes.ts": {
			{name: "setupRoutes", startLine: 1, endLine: 20},
		},
	}

	_, edges := buildCrossLanguageGraph("", nil, routeList, nil, fileSymbols)

	var handlesEdge *edgeData
	for i := range edges {
		if edges[i].EdgeLabel == "HANDLES" {
			e := edges[i]
			handlesEdge = &e
			break
		}
	}
	if handlesEdge == nil {
		t.Fatal("expected 1 HANDLES edge via enclosing-fn resolver, got 0")
	}

	want := "setupRoutes" + compositeKeyDelim + "src/routes.ts"
	if handlesEdge.FromKey != want {
		t.Errorf("HANDLES FromKey = %q, want %q", handlesEdge.FromKey, want)
	}
}

// TestBuildCrossLang_NamedHandlerUnchanged verifies that a Go route with an
// explicit Handler name bypasses the enclosing-fn resolver entirely and
// produces the same "handler:file" edge as before (go-nerv regression guard).
func TestBuildCrossLang_NamedHandlerUnchanged(t *testing.T) {
	t.Parallel()

	routeList := []routes.Route{
		{
			Method:    "GET",
			Path:      "/admin/users",
			Handler:   "myHandler",
			Framework: "chi",
			Side:      "server",
			File:      "internal/admin/handler.go",
			Line:      42,
		},
	}
	// Provide fileSymbols with an overlapping span — resolver must NOT be called.
	fileSymbols := map[string][]symbolSpan{
		"internal/admin/handler.go": {
			{name: "shouldNotBeUsed", startLine: 1, endLine: 100},
		},
	}

	_, edges := buildCrossLanguageGraph("", nil, routeList, nil, fileSymbols)

	var handlesEdge *edgeData
	for i := range edges {
		if edges[i].EdgeLabel == "HANDLES" {
			e := edges[i]
			handlesEdge = &e
			break
		}
	}
	if handlesEdge == nil {
		t.Fatal("expected 1 HANDLES edge for named handler, got 0")
	}

	want := "myHandler" + compositeKeyDelim + "internal/admin/handler.go"
	if handlesEdge.FromKey != want {
		t.Errorf("HANDLES FromKey = %q, want %q (named-handler path must be unchanged)", handlesEdge.FromKey, want)
	}
}

// TestBuildCrossLang_NoEnclosingFn_Unresolved verifies that a route with an
// empty Handler and a Line NOT inside any symbol span produces no edge and
// bumps routeHandlerUnresolvedTotal.
func TestBuildCrossLang_NoEnclosingFn_Unresolved(t *testing.T) {
	t.Parallel()

	routeList := []routes.Route{
		{
			Method:    "GET",
			Path:      "/orphan",
			Handler:   "", // empty — no enclosing fn either
			Framework: "express",
			Side:      "server",
			File:      "src/orphan.ts",
			Line:      99, // outside all symbol spans
		},
	}
	fileSymbols := map[string][]symbolSpan{
		"src/orphan.ts": {
			{name: "aFn", startLine: 1, endLine: 10},
		},
	}

	unresolvedBefore := readCounter(t,
		routeHandlerUnresolvedTotal.WithLabelValues("test-repo"))

	_, edges := buildCrossLanguageGraph("test-repo", nil, routeList, nil, fileSymbols)

	// No edge must be built.
	for _, e := range edges {
		if e.EdgeLabel == "HANDLES" || e.EdgeLabel == "FETCHES" {
			t.Errorf("unexpected edge %q built for unresolvable route", e.EdgeLabel)
		}
	}

	unresolvedAfter := readCounter(t,
		routeHandlerUnresolvedTotal.WithLabelValues("test-repo"))
	if unresolvedAfter-unresolvedBefore < 1 {
		t.Errorf("routeHandlerUnresolvedTotal did not increment: before=%.0f after=%.0f",
			unresolvedBefore, unresolvedAfter)
	}
}

// TestColonInPath_ResolvesCorrectVertex verifies that a route whose path
// contains ':' (e.g. /peer1:unknown) produces a composite key that can be
// split back into the original method and path intact.
//
// With the old ':' delimiter, strings.SplitN("GET:/peer1:unknown", ":", 2)
// returns ["GET", "/peer1:unknown"] — which happens to work for the Route case
// but strings.Index("GET:/peer1:unknown", ":") is fragile when the same logic
// is applied to AGE's split() which splits on first ':' only. The NUL delimiter
// removes the ambiguity entirely.
func TestColonInPath_ResolvesCorrectVertex(t *testing.T) {
	t.Parallel()

	method := "GET"
	path := "/peer1:unknown" // colon inside the path

	// Build the composite key via the same logic as buildCrossLanguageGraph.
	key := method + compositeKeyDelim + path

	// Split must yield the exact original method and path.
	parts := strings.SplitN(key, compositeKeyDelim, 2)
	if len(parts) != 2 {
		t.Fatalf("SplitN produced %d parts, want 2", len(parts))
	}
	if parts[0] != method {
		t.Errorf("method = %q, want %q", parts[0], method)
	}
	if parts[1] != path {
		t.Errorf("path = %q, want %q", parts[1], path)
	}

	// matchKey must reconstruct the Cypher property filter with the full path
	// including the colon — and the NUL delimiter must NOT appear in the output.
	cypher := matchKey("Route", key)
	if strings.Contains(cypher, "\x00") {
		t.Errorf("matchKey emitted \\x00 into Cypher: %q", cypher)
	}
	if !strings.Contains(cypher, "method: 'GET'") {
		t.Errorf("matchKey missing method: 'GET' in %q", cypher)
	}
	if !strings.Contains(cypher, "path: '/peer1:unknown'") {
		t.Errorf("matchKey missing path: '/peer1:unknown' in %q", cypher)
	}
}

// TestCompositeKeyRoundTrip verifies that buildKey(method,path) → splitKey
// restores the original method+path even when the path contains ':'.
func TestCompositeKeyRoundTrip(t *testing.T) {
	t.Parallel()

	cases := []struct{ method, path string }{
		{"GET", "/api/users"},
		{"POST", "/peer1:unknown"},
		{"DELETE", "/a:b:c"},
		{"GET", "/:id/profile"},
	}
	for _, c := range cases {
		key := c.method + compositeKeyDelim + c.path
		parts := strings.SplitN(key, compositeKeyDelim, 2)
		if len(parts) != 2 {
			t.Errorf("method=%q path=%q: SplitN got %d parts", c.method, c.path, len(parts))
			continue
		}
		if parts[0] != c.method || parts[1] != c.path {
			t.Errorf("round-trip fail: got method=%q path=%q, want method=%q path=%q",
				parts[0], parts[1], c.method, c.path)
		}
	}
}

// TestSymbolCompositeKeyRoundTrip verifies that Symbol "name\x00file" round-trips
// correctly even when name or file contains ':'.
func TestSymbolCompositeKeyRoundTrip(t *testing.T) {
	t.Parallel()

	cases := []struct{ name, file string }{
		{"handleFoo", "internal/admin/handler.go"},
		{"hunt_jobs", "internal/admin/templates/hunt_jobs.html"},
		{"setup:Special", "src/weird:name.go"}, // extreme: both parts contain ':'
	}
	for _, c := range cases {
		key := c.name + compositeKeyDelim + c.file
		parts := strings.SplitN(key, compositeKeyDelim, 2)
		if len(parts) != 2 {
			t.Errorf("name=%q file=%q: SplitN got %d parts", c.name, c.file, len(parts))
			continue
		}
		if parts[0] != c.name || parts[1] != c.file {
			t.Errorf("round-trip fail: got name=%q file=%q, want name=%q file=%q",
				parts[0], parts[1], c.name, c.file)
		}
	}
}

// TestNULNotInCypher verifies that the \x00 compositeKeyDelim does NOT leak
// into the Cypher strings emitted by matchKey or unwindEdgeMatch.
// The delimiter must be split away before Cypher is built.
func TestNULNotInCypher(t *testing.T) {
	t.Parallel()

	// Symbol key: name\x00file
	symKey := "myHandler" + compositeKeyDelim + "internal/admin/handler.go"
	gotSym := matchKey("Symbol", symKey)
	if strings.Contains(gotSym, "\x00") {
		t.Errorf("matchKey(Symbol) leaked \\x00: %q", gotSym)
	}
	if !strings.Contains(gotSym, "name: 'myHandler'") {
		t.Errorf("matchKey(Symbol) missing name: %q", gotSym)
	}
	if !strings.Contains(gotSym, "file: 'internal/admin/handler.go'") {
		t.Errorf("matchKey(Symbol) missing file: %q", gotSym)
	}

	// Route key: method\x00path
	routeKey := "GET" + compositeKeyDelim + "/peer1:unknown"
	gotRoute := matchKey("Route", routeKey)
	if strings.Contains(gotRoute, "\x00") {
		t.Errorf("matchKey(Route) leaked \\x00: %q", gotRoute)
	}
	if !strings.Contains(gotRoute, "path: '/peer1:unknown'") {
		t.Errorf("matchKey(Route) path wrong: %q", gotRoute)
	}

	// unwindEdgeMatch must emit AGE split() on the delimiter, not ':'
	// We can't easily assert the Cypher content without running AGE, but we
	// can assert the generated Cypher does not contain a literal \x00 character
	// (the delimiter is embedded in the split() call as an escaped literal,
	// but the \x00 itself is not a printable character — check it's absent).
	gotUnwindSym := unwindEdgeMatch("Symbol", "fk")
	if strings.Contains(gotUnwindSym, "\x00") {
		t.Errorf("unwindEdgeMatch(Symbol) contains \\x00 char: %q", gotUnwindSym)
	}
	gotUnwindRoute := unwindEdgeMatch("Route", "tk")
	if strings.Contains(gotUnwindRoute, "\x00") {
		t.Errorf("unwindEdgeMatch(Route) contains \\x00 char: %q", gotUnwindRoute)
	}
}

// TestExtractRoutes_RealRouteKept verifies that a legitimate route survives
// extractRoutes and that routesExtractedTotal is bumped.
func TestExtractRoutes_RealRouteKept(t *testing.T) {
	root := t.TempDir()
	absPath := writeTestFile(t, root, "src/server.ts",
		`import express from 'express';
const app = express();
app.get('/api/partner/register', handleRegister);
`)
	f := &ingest.File{
		Path:     absPath,
		RelPath:  "src/server.ts",
		Language: "typescript",
	}

	extractedBefore := readCounter(t,
		routesExtractedTotal.WithLabelValues("test-repo", "express", "server"))

	got := extractRoutes(root, []*ingest.File{f}, "test-repo")
	if len(got) == 0 {
		t.Fatal("extractRoutes: expected at least 1 route for /api/partner/register, got 0")
	}

	found := false
	for _, r := range got {
		if r.Path == "/api/partner/register" {
			found = true
		}
	}
	if !found {
		t.Errorf("route /api/partner/register not found in: %+v", got)
	}

	extractedAfter := readCounter(t,
		routesExtractedTotal.WithLabelValues("test-repo", "express", "server"))
	if extractedAfter-extractedBefore < 1 {
		t.Errorf("routesExtractedTotal did not increment: before=%.0f after=%.0f",
			extractedBefore, extractedAfter)
	}
}

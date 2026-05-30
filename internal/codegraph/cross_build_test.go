package codegraph

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anatolykoptev/go-code/internal/ingest"
	"github.com/anatolykoptev/go-code/internal/polyglot"
	"github.com/anatolykoptev/go-code/internal/routes"
)

func TestBuildLayerVertices(t *testing.T) {
	t.Parallel()

	layers := []polyglot.Layer{
		{Name: "backend", Role: "server", RootDir: "backend", Language: "go", Files: 20},
		{Name: "frontend", Role: "client", RootDir: "frontend", Language: "typescript", Files: 15},
	}

	vertices, _ := buildCrossLanguageGraph(layers, nil, nil)

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

	vertices, edges := buildCrossLanguageGraph(nil, routeList, nil)

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
			if e.FromKey != "handleGetUsers:backend/handler.go" {
				t.Errorf("HANDLES FromKey = %q, want handleGetUsers:backend/handler.go", e.FromKey)
			}
			if e.ToKey != "GET:/api/users" {
				t.Errorf("HANDLES ToKey = %q, want GET:/api/users", e.ToKey)
			}
		case "FETCHES":
			fetchesCount++
			// FETCHES uses composite "handler:file" key so AGE can split on ':'
			// into the Symbol's name and file properties (Wave 5 fix).
			if e.FromKey != "fetchUsers:frontend/api.ts" {
				t.Errorf("FETCHES FromKey = %q, want fetchUsers:frontend/api.ts", e.FromKey)
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
	want := "hunt_jobs:internal/admin/templates/hunt_jobs.html"
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

	_, edges := buildCrossLanguageGraph(nil, routeList, nil)

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

	want := "hunt_jobs:internal/admin/templates/hunt_jobs.html"
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
	want := "handleHuntJobsList:internal/admin/handler.go"
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

	_, edges := buildCrossLanguageGraph(nil, routeList, nil)

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

	want := "handleHuntJobsList:internal/admin/handler.go"
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

	got := matchKey("Route", "GET:/api/users")
	if !strings.Contains(got, "method: 'GET'") {
		t.Errorf("matchKey(Route, GET:/api/users) = %q, missing method: 'GET'", got)
	}
	if !strings.Contains(got, "path: '/api/users'") {
		t.Errorf("matchKey(Route, GET:/api/users) = %q, missing path: '/api/users'", got)
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
	absPath := writeTestFile(t, root, "src/xss_test_fixture.ts",
		`// XSS test fixture
const r = await fetch('/api/leak?c='+cookie);
`)
	f := &ingest.File{
		Path:     absPath,
		RelPath:  "src/xss_test_fixture.ts",
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

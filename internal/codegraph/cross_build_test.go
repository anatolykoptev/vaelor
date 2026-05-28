package codegraph

import (
	"strings"
	"testing"

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
			if e.FromKey != "handleGetUsers" {
				t.Errorf("HANDLES FromKey = %q, want handleGetUsers", e.FromKey)
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

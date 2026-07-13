package routes

import "testing"

// TestPythonMatcher_LineCapture verifies that Route.Line is set for Flask,
// FastAPI, and client-side requests/httpx/aiohttp routes.
// Line=0 was the pre-fix state (FU-CG.7).
func TestPythonMatcher_LineCapture(t *testing.T) {
	t.Parallel()

	// Line 1: blank
	// Line 2: @app.route("/flask")
	// Line 3: blank
	// Line 4: @router.get("/fastapi")
	// Line 5: blank
	// Line 6: requests.get("/client")
	source := `
@app.route("/flask")

@router.get("/fastapi")

requests.get("/client")
`

	matcher := &PythonMatcher{}
	routes := matcher.Match([]byte(source))

	if len(routes) != 3 {
		t.Fatalf("got %d routes, want 3", len(routes))
	}

	want := map[string]uint32{
		"/flask":   2,
		"/fastapi": 4,
		"/client":  6,
	}

	for _, r := range routes {
		wantLine, ok := want[r.Path]
		if !ok {
			t.Errorf("unexpected route path %q", r.Path)
			continue
		}
		if r.Line == 0 {
			t.Errorf("route %q: Line = 0, want %d (FU-CG.7)", r.Path, wantLine)
		} else if r.Line != wantLine {
			t.Errorf("route %q: Line = %d, want %d", r.Path, r.Line, wantLine)
		}
	}
}

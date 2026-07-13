package routes

import "testing"

// TestGoMatcher_LineCapture verifies that Route.Line is set for the four Go
// route patterns: net/http server handlers, chi/gin/echo router methods, and
// client-side http.Get / http.NewRequest calls.
//
// Line=0 was the pre-fix state for the Go client patterns; the server patterns
// already populated Handler, but still benefit from Line for T1+ coupling.
func TestGoMatcher_LineCapture(t *testing.T) {
	t.Parallel()

	// Line 1: blank
	// Line 2: http.HandleFunc
	// Line 3: router method
	// Line 4: blank
	// Line 5: http.Get
	// Line 6: http.NewRequest
	source := `
http.HandleFunc("/api/server", handleServer)

r.Get("/api/chi", handleChi)

resp, _ := http.Get("/api/client")
req, _ := http.NewRequest("POST", "/api/newreq", nil)
`

	matcher := &GoMatcher{}
	routes := matcher.Match([]byte(source))

	if len(routes) != 4 {
		t.Fatalf("got %d routes, want 4", len(routes))
	}

	want := map[string]uint32{
		"/api/server": 2,
		"/api/chi":    4,
		"/api/client": 6,
		"/api/newreq": 7,
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

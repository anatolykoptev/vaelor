package routes

import "testing"

// TestCSharpMatcher_LineCapture verifies that Route.Line is set for both ASP.NET
// attribute routes and Minimal API routes.
// Line=0 was the pre-fix state (FU-CG.7).
func TestCSharpMatcher_LineCapture(t *testing.T) {
	t.Parallel()

	// Line 1: blank
	// Line 2: [HttpGet("/api/attr")]
	// Line 3: blank
	// Line 4: app.MapGet("/api/minimal", ...)
	source := `
[HttpGet("/api/attr")]

app.MapGet("/api/minimal", Handler);
`

	matcher := &CSharpMatcher{}
	routes := matcher.Match([]byte(source))

	if len(routes) != 2 {
		t.Fatalf("got %d routes, want 2", len(routes))
	}

	want := map[string]uint32{
		"/api/attr":    2,
		"/api/minimal": 4,
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

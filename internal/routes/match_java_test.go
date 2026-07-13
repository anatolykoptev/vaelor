package routes

import "testing"

// TestJavaMatcher_LineCapture verifies that Route.Line is set for both
// @GetMapping/@PostMapping and @RequestMapping routes.
// Line=0 was the pre-fix state (FU-CG.7).
func TestJavaMatcher_LineCapture(t *testing.T) {
	t.Parallel()

	// Line 1: blank
	// Line 2: @GetMapping("/api/mapping")
	// Line 3: blank
	// Line 4: @RequestMapping(value = "/api/request")
	source := `
@GetMapping("/api/mapping")

@RequestMapping(value = "/api/request")
`

	matcher := &JavaMatcher{}
	routes := matcher.Match([]byte(source))

	if len(routes) != 2 {
		t.Fatalf("got %d routes, want 2", len(routes))
	}

	want := map[string]uint32{
		"/api/mapping": 2,
		"/api/request": 4,
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

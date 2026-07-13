package routes

import "testing"

// TestRubyMatcher_LineCapture verifies that Route.Line is set for Sinatra-style
// route declarations.
// Line=0 was the pre-fix state (FU-CG.7).
func TestRubyMatcher_LineCapture(t *testing.T) {
	t.Parallel()

	// Line 1: blank
	// Line 2: get '/sinatra' do
	// Line 3: end
	source := `
get '/sinatra' do
end
`

	matcher := &RubyMatcher{}
	routes := matcher.Match([]byte(source))

	if len(routes) != 1 {
		t.Fatalf("got %d routes, want 1", len(routes))
	}

	if routes[0].Line == 0 {
		t.Errorf("route %q: Line = 0, want 2 (FU-CG.7)", routes[0].Path)
	} else if routes[0].Line != 2 {
		t.Errorf("route %q: Line = %d, want 2", routes[0].Path, routes[0].Line)
	}
}

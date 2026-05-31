package routes

import "testing"

// TestTypeScriptMatcher_ReceiverAllowList verifies that the receiver allow-list
// filters out non-router identifiers (headers.get, map.get) while keeping
// legitimate router calls (app.get, router.post).
func TestTypeScriptMatcher_ReceiverAllowList(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		source         string
		wantRouteCount int
		wantPath       string // first route path, empty when wantRouteCount==0
	}{
		{
			name:           "headers.get is NOT a route",
			source:         `const token = headers.get('Authorization');`,
			wantRouteCount: 0,
		},
		{
			name:           "map.get is NOT a route",
			source:         `const v = map.get('/A');`,
			wantRouteCount: 0,
		},
		{
			name:           "set.delete is NOT a route",
			source:         `set.delete('/key');`,
			wantRouteCount: 0,
		},
		{
			name:           "app.get IS a route",
			source:         `app.get('/api/x', handler);`,
			wantRouteCount: 1,
			wantPath:       "/api/x",
		},
		{
			name:           "router.post IS a route",
			source:         `router.post('/api/users', createUser);`,
			wantRouteCount: 1,
			wantPath:       "/api/users",
		},
		{
			name:           "fastify.get IS a route",
			source:         `fastify.get('/health', healthHandler);`,
			wantRouteCount: 1,
			wantPath:       "/health",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			matcher := &TypeScriptMatcher{}
			got := matcher.Match([]byte(tt.source))
			if len(got) != tt.wantRouteCount {
				t.Fatalf("Match(%q): got %d routes, want %d", tt.source, len(got), tt.wantRouteCount)
			}
			if tt.wantRouteCount > 0 && got[0].Path != tt.wantPath {
				t.Errorf("route[0].Path = %q, want %q", got[0].Path, tt.wantPath)
			}
		})
	}
}

// TestTypeScriptMatcher_XSSFixtureSuppressed verifies that the XSS test fixture
// `fetch('/api/leak?c='+cookie)` does not produce a server-side route (it should
// still produce a client route, but IsJunkPath will drop it at ingest time).
// This test validates the receiver filter only (ingest-level junk is in index_layers).
func TestTypeScriptMatcher_XSSFixtureSuppressed(t *testing.T) {
	t.Parallel()
	source := `fetch('/api/leak?c='+cookie);`
	matcher := &TypeScriptMatcher{}
	got := matcher.Match([]byte(source))
	// fetch() → client-side route; the receiver guard does not apply to fetch.
	// The path /api/leak?c= is junk but filtered at ingest, not in Match.
	// Assert: no SERVER-side route is produced.
	for _, r := range got {
		if r.Side == "server" {
			t.Errorf("XSS fixture produced server route: %+v", r)
		}
	}
}

// TestTypeScriptMatcher_LineCapture verifies that Line is set to the 1-based
// line number of the match within the source, for both server and client routes.
func TestTypeScriptMatcher_LineCapture(t *testing.T) {
	t.Parallel()

	// Line 1: blank
	// Line 2: server route → Line = 2
	// Line 3: blank
	// Line 4: client route → Line = 4
	source := `
app.get('/api/x', handler);

const r = await fetch('/api/y');
`
	matcher := &TypeScriptMatcher{}
	got := matcher.Match([]byte(source))

	var serverRoute, clientRoute *Route
	for i := range got {
		switch got[i].Side {
		case "server":
			serverRoute = &got[i]
		case "client":
			if got[i].Framework == "fetch" {
				clientRoute = &got[i]
			}
		}
	}

	if serverRoute == nil {
		t.Fatal("expected server route from app.get, got none")
	}
	if clientRoute == nil {
		t.Fatal("expected client route from fetch, got none")
	}

	// Source starts with '\n', so:
	// offset 0 = '\n' (end of line 1)  → app.get starts at offset 1 → line 2
	// fetch is on line 4
	if serverRoute.Line != 2 {
		t.Errorf("server route Line = %d, want 2", serverRoute.Line)
	}
	if clientRoute.Line != 4 {
		t.Errorf("client (fetch) route Line = %d, want 4", clientRoute.Line)
	}
}

// TestTypeScriptMatcher_LineCapture_MultiLine verifies line numbers across a
// larger multi-line source to confirm the lineAt helper counts correctly.
func TestTypeScriptMatcher_LineCapture_MultiLine(t *testing.T) {
	t.Parallel()

	source := `import express from 'express';
const app = express();
// some comment

app.get('/api/first', firstHandler);
app.post('/api/second', secondHandler);

axios.get('/api/third');
`
	matcher := &TypeScriptMatcher{}
	got := matcher.Match([]byte(source))

	// Find by path.
	lineOf := make(map[string]uint32)
	for _, r := range got {
		lineOf[r.Path] = r.Line
	}

	wantLines := map[string]uint32{
		"/api/first":  5,
		"/api/second": 6,
		"/api/third":  8,
	}
	for path, wantLine := range wantLines {
		if lineOf[path] != wantLine {
			t.Errorf("route %q: Line = %d, want %d", path, lineOf[path], wantLine)
		}
	}
}

// TestTypeScriptMatcher_NestDecorator_Line verifies Line is captured for NestJS
// decorator routes too.
func TestTypeScriptMatcher_NestDecorator_Line(t *testing.T) {
	t.Parallel()

	source := `@Controller('/api')
export class UsersController {
  @Get('/users')
  findAll() {}
}
`
	matcher := &TypeScriptMatcher{}
	got := matcher.Match([]byte(source))
	if len(got) == 0 {
		t.Fatal("expected NestJS route, got none")
	}
	if got[0].Line == 0 {
		t.Errorf("NestJS decorator route Line = 0, want non-zero")
	}
	// @Get('/users') is on line 3.
	if got[0].Line != 3 {
		t.Errorf("NestJS decorator route Line = %d, want 3", got[0].Line)
	}
}

// TestTypeScriptMatcher_SuffixReceivers verifies that *Router/*Routes suffixed
// identifiers are accepted (FIX 2: recover custom-router-name routes).
// Also verifies that headers.get and map.get still produce 0 routes.
func TestTypeScriptMatcher_SuffixReceivers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		source         string
		wantRouteCount int
		wantPath       string
	}{
		{
			name:           "adminRouter.get IS a route",
			source:         `adminRouter.get('/admin/users', listAdminUsers);`,
			wantRouteCount: 1,
			wantPath:       "/admin/users",
		},
		{
			name:           "apiRouter.post IS a route",
			source:         `apiRouter.post('/items', createItem);`,
			wantRouteCount: 1,
			wantPath:       "/items",
		},
		{
			name:           "userRoutes.get IS a route",
			source:         `userRoutes.get('/profile', getProfile);`,
			wantRouteCount: 1,
			wantPath:       "/profile",
		},
		{
			name:           "headers.get is still NOT a route",
			source:         `const tok = headers.get('Authorization');`,
			wantRouteCount: 0,
		},
		{
			name:           "map.get is still NOT a route",
			source:         `const v = map.get('/A');`,
			wantRouteCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			matcher := &TypeScriptMatcher{}
			got := matcher.Match([]byte(tt.source))
			if len(got) != tt.wantRouteCount {
				t.Fatalf("Match(%q): got %d routes, want %d", tt.source, len(got), tt.wantRouteCount)
			}
			if tt.wantRouteCount > 0 && got[0].Path != tt.wantPath {
				t.Errorf("route[0].Path = %q, want %q", got[0].Path, tt.wantPath)
			}
		})
	}
}

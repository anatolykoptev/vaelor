package routes

import (
	"testing"
)

func TestGoMatcherEdgeCases(t *testing.T) {
	cases := []struct {
		name    string
		source  string
		wantN   int
		checks  []struct{ method, path, handler, side string }
	}{
		{
			name: "Go 1.22 pattern with method",
			source: `mux.HandleFunc("POST /api/users", s.CreateUser)`,
			wantN: 1,
			checks: []struct{ method, path, handler, side string }{
				{"POST", "/api/users", "CreateUser", "server"},
			},
		},
		{
			name: "Go 1.22 pattern without method (just path)",
			source: `mux.HandleFunc("/legacy/path", legacyHandler)`,
			wantN: 1,
			checks: []struct{ method, path, handler, side string }{
				{"*", "/legacy/path", "legacyHandler", "server"},
			},
		},
		{
			name: "anonymous handler (should still match path)",
			source: `http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {})`,
			wantN: 0, // anonymous func doesn't match [A-Za-z_] handler pattern
		},
		{
			name: "http.Handle with named handler variable",
			source: `http.Handle("/static/", fileServer)`,
			wantN: 1,
			checks: []struct{ method, path, handler, side string }{
				{"*", "/static", "fileServer", "server"},
			},
		},
		{
			name: "chi route with path params",
			source: `r.Get("/users/{id}/posts/{postID}", GetUserPosts)`,
			wantN: 1,
			checks: []struct{ method, path, handler, side string }{
				{"GET", "/users/*/posts/*", "GetUserPosts", "server"},
			},
		},
		{
			name: "client http.NewRequestWithContext with variable URL",
			source: `http.NewRequestWithContext(ctx, "DELETE", "/api/items/"+id, nil)`,
			wantN: 0, // URL is concatenated, not a string literal
		},
		{
			name: "multiple routes in one source",
			source: `
				r.Get("/api/users", ListUsers)
				r.Post("/api/users", CreateUser)
				r.Delete("/api/users/{id}", DeleteUser)
			`,
			wantN: 3,
		},
		{
			name: "Go 1.22 with host pattern",
			source: `mux.HandleFunc("GET example.com/api/data", s.GetData)`,
			wantN: 1,
			checks: []struct{ method, path, handler, side string }{
				{"GET", "/api/data", "GetData", "server"},
			},
		},
		{
			name: "http.Get client call with full URL",
			source: `http.Get("https://api.example.com/v1/data")`,
			wantN: 1,
			checks: []struct{ method, path, handler, side string }{
				{"GET", "/v1/data", "", "client"},
			},
		},
		{
			name: "double method handler - HandleFunc and router",
			source: `
				mux.HandleFunc("GET /api/items", s.ListItems)
				r.Get("/api/items", ListItems)
			`,
			wantN: 2,
		},
	}

	m := &GoMatcher{}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			routes := m.Match([]byte(tc.source))
			if len(routes) != tc.wantN {
				t.Errorf("got %d routes, want %d", len(routes), tc.wantN)
				for i, r := range routes {
					t.Logf("  route[%d]: %s %s → %s (side=%s)", i, r.Method, r.Path, r.Handler, r.Side)
				}
				return
			}
			for i, check := range tc.checks {
				if i >= len(routes) {
					break
				}
				r := routes[i]
				if check.method != "" && r.Method != check.method {
					t.Errorf("route[%d] method: got %q, want %q", i, r.Method, check.method)
				}
				if check.path != "" && r.Path != check.path {
					t.Errorf("route[%d] path: got %q, want %q", i, r.Path, check.path)
				}
				if check.handler != "" && r.Handler != check.handler {
					t.Errorf("route[%d] handler: got %q, want %q", i, r.Handler, check.handler)
				}
				if check.side != "" && r.Side != check.side {
					t.Errorf("route[%d] side: got %q, want %q", i, r.Side, check.side)
				}
			}
		})
	}
}

func TestTypeScriptMatcherEdgeCases(t *testing.T) {
	m := &TypeScriptMatcher{}
	cases := []struct {
		name   string
		source string
		wantN  int
		checks []struct{ method, path, handler, side string }
	}{
		{
			name:   "express route with arrow function",
			source: `app.get('/api/users', (req, res) => { res.json(users) })`,
			wantN:  1, // route is valid, handler name is just empty
			checks: []struct{ method, path, handler, side string }{
				{"GET", "/api/users", "", "server"},
			},
		},
		{
			name:   "express route with named handler",
			source: `app.get('/api/users', getUsers)`,
			wantN:  1,
			checks: []struct{ method, path, handler, side string }{
				{"GET", "/api/users", "getUsers", "server"},
			},
		},
		{
			name:   "fetch with template literal",
			source: "fetch(`/api/users/${userId}`)",
			wantN:  0, // template literal, not string literal
		},
		{
			name:   "fetch with string literal",
			source: `fetch('/api/users')`,
			wantN:  1,
			checks: []struct{ method, path, handler, side string }{
				{"GET", "/api/users", "", "client"},
			},
		},
		{
			name:   "axios POST",
			source: `axios.post('/api/create', data)`,
			wantN:  1,
			checks: []struct{ method, path, handler, side string }{
				{"POST", "/api/create", "", "client"},
			},
		},
		{
			name: "NestJS decorator",
			source: `@Get('/users/:id')
			async getUser(@Param('id') id: string) {}`,
			wantN: 1,
			checks: []struct{ method, path, handler, side string }{
				{"GET", "/users/*", "", "server"},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			routes := m.Match([]byte(tc.source))
			if len(routes) != tc.wantN {
				t.Errorf("got %d routes, want %d", len(routes), tc.wantN)
				for i, r := range routes {
					t.Logf("  route[%d]: %s %s → %s (side=%s)", i, r.Method, r.Path, r.Handler, r.Side)
				}
				return
			}
			for i, check := range tc.checks {
				if i >= len(routes) {
					break
				}
				r := routes[i]
				if check.method != "" && r.Method != check.method {
					t.Errorf("route[%d] method: got %q, want %q", i, r.Method, check.method)
				}
				if check.path != "" && r.Path != check.path {
					t.Errorf("route[%d] path: got %q, want %q", i, r.Path, check.path)
				}
				if check.handler != "" && r.Handler != check.handler {
					t.Errorf("route[%d] handler: got %q, want %q", i, r.Handler, check.handler)
				}
				if check.side != "" && r.Side != check.side {
					t.Errorf("route[%d] side: got %q, want %q", i, r.Side, check.side)
				}
			}
		})
	}
}

func TestPythonMatcherEdgeCases(t *testing.T) {
	m := &PythonMatcher{}
	cases := []struct {
		name   string
		source string
		wantN  int
		checks []struct{ method, path, side string }
	}{
		{
			name:   "Flask route decorator",
			source: `@app.route('/api/users', methods=['GET', 'POST'])`,
			wantN:  1,
			checks: []struct{ method, path, side string }{
				{"*", "/api/users", "server"},
			},
		},
		{
			name:   "FastAPI with path params",
			source: `@router.get('/users/{user_id}/posts')`,
			wantN:  1,
			checks: []struct{ method, path, side string }{
				{"GET", "/users/*/posts", "server"},
			},
		},
		{
			name:   "requests client call",
			source: `requests.get('https://api.example.com/v1/data')`,
			wantN:  1,
			checks: []struct{ method, path, side string }{
				{"GET", "/v1/data", "client"},
			},
		},
		{
			name:   "httpx async client",
			source: `httpx.post('/api/submit', json=data)`,
			wantN:  1,
			checks: []struct{ method, path, side string }{
				{"POST", "/api/submit", "client"},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			routes := m.Match([]byte(tc.source))
			if len(routes) != tc.wantN {
				t.Errorf("got %d routes, want %d", len(routes), tc.wantN)
				for i, r := range routes {
					t.Logf("  route[%d]: %s %s (side=%s)", i, r.Method, r.Path, r.Side)
				}
				return
			}
			for i, check := range tc.checks {
				if i >= len(routes) { break }
				r := routes[i]
				if check.method != "" && r.Method != check.method {
					t.Errorf("route[%d] method: got %q, want %q", i, r.Method, check.method)
				}
				if check.path != "" && r.Path != check.path {
					t.Errorf("route[%d] path: got %q, want %q", i, r.Path, check.path)
				}
				if check.side != "" && r.Side != check.side {
					t.Errorf("route[%d] side: got %q, want %q", i, r.Side, check.side)
				}
			}
		})
	}
}

func TestNormalizePathEdgeCases(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"/api/users", "/api/users"},
		{"/api/users/", "/api/users"},
		{"api/users", "/api/users"},
		{"/api/users/:id", "/api/users/*"},
		{"/api/users/{userId}", "/api/users/*"},
		{"https://example.com/api/data", "/api/data"},
		{"http://localhost:3000/api/test", "/api/test"},
		{"/", "/"},
		{"", "/"},
		{"/api//double//slash", "/api/double/slash"},
		{"/api/:id/posts/:postId/comments", "/api/*/posts/*/comments"},
		{"https://example.com", "/"},
		{"https://example.com/", "/"},
	}

	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			got := NormalizePath(tc.input)
			if got != tc.want {
				t.Errorf("NormalizePath(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

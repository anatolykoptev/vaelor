package routes

import (
	"testing"
)

func TestGoServerRoutes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		source    string
		wantCount int
		wantFirst Route
	}{
		{
			name:      "http.HandleFunc",
			source:    `http.HandleFunc("/api/users", handleUsers)`,
			wantCount: 1,
			wantFirst: Route{
				Method:    "*",
				Path:      "/api/users",
				RawPath:   "/api/users",
				Handler:   "handleUsers",
				Framework: "net/http",
				Side:      "server",
			},
		},
		{
			name: "chi router",
			source: `
r.Get("/api/items", listItems)
r.Post("/api/items", createItem)
`,
			wantCount: 2,
			wantFirst: Route{
				Method:    "GET",
				Path:      "/api/items",
				RawPath:   "/api/items",
				Handler:   "listItems",
				Framework: "chi",
				Side:      "server",
			},
		},
		{
			name: "gin router",
			source: `
r.GET("/api/products", getProducts)
r.DELETE("/api/products/:id", deleteProduct)
`,
			wantCount: 2,
			wantFirst: Route{
				Method:    "GET",
				Path:      "/api/products",
				RawPath:   "/api/products",
				Handler:   "getProducts",
				Framework: "chi",
				Side:      "server",
			},
		},
		{
			name:      "echo router",
			source:    `e.GET("/health", healthCheck)`,
			wantCount: 1,
			wantFirst: Route{
				Method:    "GET",
				Path:      "/health",
				RawPath:   "/health",
				Handler:   "healthCheck",
				Framework: "chi",
				Side:      "server",
			},
		},
		{
			name:      "no routes",
			source:    `fmt.Println("hello world")`,
			wantCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			matcher := &GoMatcher{}
			routes := matcher.Match([]byte(tt.source))

			if len(routes) != tt.wantCount {
				t.Fatalf("got %d routes, want %d", len(routes), tt.wantCount)
			}

			if tt.wantCount == 0 {
				return
			}

			got := routes[0]
			if got.Method != tt.wantFirst.Method {
				t.Errorf("Method = %q, want %q", got.Method, tt.wantFirst.Method)
			}
			if got.Path != tt.wantFirst.Path {
				t.Errorf("Path = %q, want %q", got.Path, tt.wantFirst.Path)
			}
			if got.RawPath != tt.wantFirst.RawPath {
				t.Errorf("RawPath = %q, want %q", got.RawPath, tt.wantFirst.RawPath)
			}
			if got.Handler != tt.wantFirst.Handler {
				t.Errorf("Handler = %q, want %q", got.Handler, tt.wantFirst.Handler)
			}
			if got.Framework != tt.wantFirst.Framework {
				t.Errorf("Framework = %q, want %q", got.Framework, tt.wantFirst.Framework)
			}
			if got.Side != tt.wantFirst.Side {
				t.Errorf("Side = %q, want %q", got.Side, tt.wantFirst.Side)
			}
		})
	}
}

func TestGoClientRoutes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
		want   Route
	}{
		{
			name:   "http.Get",
			source: `resp, err := http.Get("https://api.example.com/users")`,
			want: Route{
				Method:    "GET",
				Path:      "/users",
				RawPath:   "https://api.example.com/users",
				Handler:   "",
				Framework: "net/http",
				Side:      "client",
			},
		},
		{
			name:   "http.NewRequest",
			source: `req, err := http.NewRequest("POST", "https://api.example.com/items", body)`,
			want: Route{
				Method:    "POST",
				Path:      "/items",
				RawPath:   "https://api.example.com/items",
				Handler:   "",
				Framework: "net/http",
				Side:      "client",
			},
		},
		{
			name:   "http.NewRequestWithContext",
			source: `req, err := http.NewRequestWithContext(ctx, "PUT", "https://api.example.com/items/123", body)`,
			want: Route{
				Method:    "PUT",
				Path:      "/items/123",
				RawPath:   "https://api.example.com/items/123",
				Handler:   "",
				Framework: "net/http",
				Side:      "client",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			matcher := &GoMatcher{}
			routes := matcher.Match([]byte(tt.source))

			if len(routes) != 1 {
				t.Fatalf("got %d routes, want 1", len(routes))
			}

			got := routes[0]
			if got.Side != "client" {
				t.Errorf("Side = %q, want %q", got.Side, "client")
			}
			if got.Method != tt.want.Method {
				t.Errorf("Method = %q, want %q", got.Method, tt.want.Method)
			}
			if got.Path != tt.want.Path {
				t.Errorf("Path = %q, want %q", got.Path, tt.want.Path)
			}
			if got.RawPath != tt.want.RawPath {
				t.Errorf("RawPath = %q, want %q", got.RawPath, tt.want.RawPath)
			}
			if got.Framework != tt.want.Framework {
				t.Errorf("Framework = %q, want %q", got.Framework, tt.want.Framework)
			}
		})
	}
}

func TestNormalizePath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  string
	}{
		{"/api/users/:id", "/api/users/*"},
		{"/api/users/{id}", "/api/users/*"},
		{"/api/v1/items", "/api/v1/items"},
		{"/health", "/health"},
		{"https://api.example.com/users", "/users"},
		{"http://localhost:8080/api/v1//items", "/api/v1/items"},
		{"api/users", "/api/users"},
		{"/api//double//slash", "/api/double/slash"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()

			got := NormalizePath(tt.input)
			if got != tt.want {
				t.Errorf("NormalizePath(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestExtractAll(t *testing.T) {
	t.Parallel()

	source := []byte(`
http.HandleFunc("/api/health", healthHandler)
r.Get("/api/users", listUsers)
resp, err := http.Get("https://example.com/status")
`)

	routes := ExtractAll("go", source)
	if len(routes) < 3 {
		t.Errorf("ExtractAll returned %d routes, want at least 3", len(routes))
	}

	// Verify we get both server and client routes.
	var hasServer, hasClient bool
	for _, r := range routes {
		switch r.Side {
		case "server":
			hasServer = true
		case "client":
			hasClient = true
		}
	}

	if !hasServer {
		t.Error("ExtractAll: no server-side routes found")
	}
	if !hasClient {
		t.Error("ExtractAll: no client-side routes found")
	}
}

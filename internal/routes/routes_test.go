package routes

import (
	"net/http"
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

// --- TypeScript / JavaScript ---

func TestTypeScriptServerRoutes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		source    string
		wantCount int
		wantFirst Route
	}{
		{
			name: "express routes",
			source: `
app.get("/api/users", listUsers);
app.post("/api/users", createUser);
`,
			wantCount: 2,
			wantFirst: Route{
				Method:    "GET",
				Path:      "/api/users",
				RawPath:   "/api/users",
				Framework: "express",
				Side:      "server",
			},
		},
		{
			name:      "fastify route",
			source:    `fastify.get("/health", healthHandler);`,
			wantCount: 1,
			wantFirst: Route{
				Method:    "GET",
				Path:      "/health",
				RawPath:   "/health",
				Framework: "express",
				Side:      "server",
			},
		},
		{
			name: "nestjs decorators",
			source: `
@Get("/api/items")
findAll() {}
@Post("/api/items")
create() {}
`,
			wantCount: 2,
			wantFirst: Route{
				Method:    "GET",
				Path:      "/api/items",
				RawPath:   "/api/items",
				Framework: "express",
				Side:      "server",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			matcher := &TypeScriptMatcher{}
			routes := matcher.Match([]byte(tt.source))

			if len(routes) != tt.wantCount {
				t.Fatalf("got %d routes, want %d", len(routes), tt.wantCount)
			}

			got := routes[0]
			if got.Method != tt.wantFirst.Method {
				t.Errorf("Method = %q, want %q", got.Method, tt.wantFirst.Method)
			}
			if got.Path != tt.wantFirst.Path {
				t.Errorf("Path = %q, want %q", got.Path, tt.wantFirst.Path)
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

func TestTypeScriptClientRoutes(t *testing.T) {
	t.Parallel()

	source := []byte(`
const resp = await fetch("/api/users");
const data = await axios.get("/api/items");
await axios.post("/api/orders", body);
`)

	matcher := &TypeScriptMatcher{}
	routes := matcher.Match(source)

	if len(routes) != 3 {
		t.Fatalf("got %d routes, want 3", len(routes))
	}

	// All should be client-side.
	for i, r := range routes {
		if r.Side != "client" {
			t.Errorf("route[%d].Side = %q, want %q", i, r.Side, "client")
		}
	}

	// fetch defaults to "GET", axios.get has "GET", axios.post has "POST".
	wantMethods := []string{"GET", "GET", "POST"}
	for i, wm := range wantMethods {
		if routes[i].Method != wm {
			t.Errorf("route[%d].Method = %q, want %q", i, routes[i].Method, wm)
		}
	}
}

// --- Python ---

func TestPythonServerRoutes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		source    string
		wantCount int
		wantFirst Route
	}{
		{
			name:      "flask route",
			source:    `@app.route("/api/users", methods=["GET", "POST"])`,
			wantCount: 1,
			wantFirst: Route{
				Method:    "*",
				Path:      "/api/users",
				RawPath:   "/api/users",
				Framework: "python",
				Side:      "server",
			},
		},
		{
			name: "fastapi routes",
			source: `
@router.get("/api/items")
async def list_items():
    pass

@app.post("/api/items")
async def create_item():
    pass
`,
			wantCount: 2,
			wantFirst: Route{
				Method:    "GET",
				Path:      "/api/items",
				RawPath:   "/api/items",
				Framework: "python",
				Side:      "server",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			matcher := &PythonMatcher{}
			routes := matcher.Match([]byte(tt.source))

			if len(routes) != tt.wantCount {
				t.Fatalf("got %d routes, want %d", len(routes), tt.wantCount)
			}

			got := routes[0]
			if got.Method != tt.wantFirst.Method {
				t.Errorf("Method = %q, want %q", got.Method, tt.wantFirst.Method)
			}
			if got.Path != tt.wantFirst.Path {
				t.Errorf("Path = %q, want %q", got.Path, tt.wantFirst.Path)
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

func TestPythonClientRoutes(t *testing.T) {
	t.Parallel()

	source := []byte(`
resp = requests.get("https://api.example.com/users")
data = httpx.post("https://api.example.com/items", json=payload)
`)

	matcher := &PythonMatcher{}
	routes := matcher.Match(source)

	if len(routes) != 2 {
		t.Fatalf("got %d routes, want 2", len(routes))
	}

	for i, r := range routes {
		if r.Side != "client" {
			t.Errorf("route[%d].Side = %q, want %q", i, r.Side, "client")
		}
	}

	if routes[0].Method != http.MethodGet {
		t.Errorf("route[0].Method = %q, want %q", routes[0].Method, "GET")
	}
	if routes[1].Method != http.MethodPost {
		t.Errorf("route[1].Method = %q, want %q", routes[1].Method, "POST")
	}
}

// --- Java ---

func TestJavaServerRoutes(t *testing.T) {
	t.Parallel()

	source := []byte(`
@GetMapping("/api/users")
public List<User> getUsers() { ... }

@PostMapping("/api/users")
public User createUser(@RequestBody User user) { ... }

@RequestMapping(value = "/api/health")
public String health() { ... }
`)

	matcher := &JavaMatcher{}
	routes := matcher.Match(source)

	if len(routes) != 3 {
		t.Fatalf("got %d routes, want 3", len(routes))
	}

	wantMethods := []string{"GET", "POST", "*"}
	for i, wm := range wantMethods {
		if routes[i].Method != wm {
			t.Errorf("route[%d].Method = %q, want %q", i, routes[i].Method, wm)
		}
		if routes[i].Side != "server" {
			t.Errorf("route[%d].Side = %q, want %q", i, routes[i].Side, "server")
		}
		if routes[i].Framework != "spring" {
			t.Errorf("route[%d].Framework = %q, want %q", i, routes[i].Framework, "spring")
		}
	}
}

// --- Rust ---

func TestRustServerRoutes(t *testing.T) {
	t.Parallel()

	source := []byte(`
#[get("/api/users")]
async fn list_users() -> impl Responder { ... }

.route("/api/items", web::post())
`)

	matcher := &RustMatcher{}
	routes := matcher.Match(source)

	if len(routes) != 2 {
		t.Fatalf("got %d routes, want 2", len(routes))
	}

	if routes[0].Method != http.MethodGet {
		t.Errorf("route[0].Method = %q, want %q", routes[0].Method, "GET")
	}
	if routes[0].Path != "/api/users" {
		t.Errorf("route[0].Path = %q, want %q", routes[0].Path, "/api/users")
	}
	if routes[0].Framework != "rust" {
		t.Errorf("route[0].Framework = %q, want %q", routes[0].Framework, "rust")
	}

	if routes[1].Method != http.MethodPost {
		t.Errorf("route[1].Method = %q, want %q", routes[1].Method, "POST")
	}
	if routes[1].Path != "/api/items" {
		t.Errorf("route[1].Path = %q, want %q", routes[1].Path, "/api/items")
	}

	for i, r := range routes {
		if r.Side != "server" {
			t.Errorf("route[%d].Side = %q, want %q", i, r.Side, "server")
		}
	}
}

// --- Ruby ---

func TestRubyServerRoutes(t *testing.T) {
	t.Parallel()

	source := []byte(`
get '/api/users' do
  json User.all
end

post '/api/users' do
  user = User.create(params)
  json user
end
`)

	matcher := &RubyMatcher{}
	routes := matcher.Match(source)

	if len(routes) < 2 {
		t.Fatalf("got %d routes, want at least 2", len(routes))
	}

	if routes[0].Method != http.MethodGet {
		t.Errorf("route[0].Method = %q, want %q", routes[0].Method, "GET")
	}
	if routes[0].Path != "/api/users" {
		t.Errorf("route[0].Path = %q, want %q", routes[0].Path, "/api/users")
	}
	if routes[0].Framework != "ruby" {
		t.Errorf("route[0].Framework = %q, want %q", routes[0].Framework, "ruby")
	}

	if routes[1].Method != http.MethodPost {
		t.Errorf("route[1].Method = %q, want %q", routes[1].Method, "POST")
	}

	for i, r := range routes {
		if r.Side != "server" {
			t.Errorf("route[%d].Side = %q, want %q", i, r.Side, "server")
		}
	}
}

// --- C# ---

func TestCSharpServerRoutes(t *testing.T) {
	t.Parallel()

	source := []byte(`
[HttpGet("/api/users")]
public IActionResult GetUsers() { ... }

[HttpPost("/api/users")]
public IActionResult CreateUser([FromBody] User user) { ... }

app.MapGet("/api/health", () => Results.Ok("healthy"));
`)

	matcher := &CSharpMatcher{}
	routes := matcher.Match(source)

	if len(routes) != 3 {
		t.Fatalf("got %d routes, want 3", len(routes))
	}

	wantMethods := []string{"GET", "POST", "GET"}
	for i, wm := range wantMethods {
		if routes[i].Method != wm {
			t.Errorf("route[%d].Method = %q, want %q", i, routes[i].Method, wm)
		}
		if routes[i].Side != "server" {
			t.Errorf("route[%d].Side = %q, want %q", i, routes[i].Side, "server")
		}
		if routes[i].Framework != "aspnet" {
			t.Errorf("route[%d].Framework = %q, want %q", i, routes[i].Framework, "aspnet")
		}
	}
}

package routes

import (
	"net/http"
	"testing"
)

// TestRustMatcher_AxumRoutes verifies that the axum builder pattern
// .route("/path", method(handler)) is matched correctly.
// acme-edge (axum 0.8) uses this pattern exclusively;
// the old Actix-only matcher produced Route=0 for that repo.
func TestRustMatcher_AxumRoutes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		source    string
		wantCount int
		wantFirst Route
	}{
		{
			name:      "axum post handler",
			source:    `.route("/relay/connect", post(relay_connect))`,
			wantCount: 1,
			wantFirst: Route{
				Method:    "POST",
				Path:      "/relay/connect",
				RawPath:   "/relay/connect",
				Handler:   "relay_connect",
				Framework: "axum",
				Side:      "server",
			},
		},
		{
			name:      "axum get with closure (no named handler)",
			source:    `.route("/metrics", get(move || { async move { "ok" } }))`,
			wantCount: 1,
			wantFirst: Route{
				Method:    "GET",
				Path:      "/metrics",
				RawPath:   "/metrics",
				Handler:   "",
				Framework: "axum",
				Side:      "server",
			},
		},
		{
			name:      "axum any handler (WebSocket upgrade)",
			source:    `.route("/sfu/ws/{room_id}", any(client_ws_upgrade))`,
			wantCount: 1,
			wantFirst: Route{
				Method:    "*",
				Path:      "/sfu/ws/*",
				RawPath:   "/sfu/ws/{room_id}",
				Handler:   "client_ws_upgrade",
				Framework: "axum",
				Side:      "server",
			},
		},
		{
			name: "axum router chain — multiple routes",
			source: `Router::new()
        .route("/sfu/ws/{room_id}", any(client_ws_upgrade))
        .route("/relay/connect", post(relay_connect))
        .with_state(state)`,
			wantCount: 2,
			wantFirst: Route{
				Method:    "*",
				Path:      "/sfu/ws/*",
				RawPath:   "/sfu/ws/{room_id}",
				Handler:   "client_ws_upgrade",
				Framework: "axum",
				Side:      "server",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			matcher := &RustMatcher{}
			routes := matcher.Match([]byte(tt.source))

			if len(routes) != tt.wantCount {
				t.Fatalf("Match: got %d routes, want %d", len(routes), tt.wantCount)
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

// TestRustMatcher_AxumRouteHTTPMethods verifies all standard HTTP methods
// extracted from axum builder patterns.
func TestRustMatcher_AxumRouteHTTPMethods(t *testing.T) {
	t.Parallel()

	cases := []struct {
		axumFn string
		want   string
	}{
		{"get", http.MethodGet},
		{"post", http.MethodPost},
		{"put", http.MethodPut},
		{"delete", http.MethodDelete},
		{"patch", http.MethodPatch},
		{"head", http.MethodHead},
		{"options", http.MethodOptions},
		{"any", "*"},
	}

	for _, c := range cases {
		t.Run(c.axumFn, func(t *testing.T) {
			t.Parallel()
			src := `.route("/api/test", ` + c.axumFn + `(handler))`
			matcher := &RustMatcher{}
			routes := matcher.Match([]byte(src))
			if len(routes) != 1 {
				t.Fatalf("got %d routes, want 1 for axum fn %q", len(routes), c.axumFn)
			}
			if routes[0].Method != c.want {
				t.Errorf("Method = %q, want %q", routes[0].Method, c.want)
			}
		})
	}
}

// TestRustMatcher_LineCapture verifies that Route.Line is set to the correct
// 1-based line number for all three Rust route patterns:
//   - Rocket/Actix attribute macros (#[get(...)])
//   - Actix builder (.route("...", web::get()))
//   - Axum builder (.route("...", post(handler)))
//
// Line=0 was the pre-fix state; this test was RED before match_rust.go fix.
func TestRustMatcher_LineCapture(t *testing.T) {
	t.Parallel()

	// Line 1: blank
	// Line 2: Rocket macro
	// Line 3: fn body (not a route)
	// Line 4: blank
	// Line 5: Actix builder
	// Line 6: blank
	// Line 7: Axum builder
	source := `
#[get("/api/macro")]
async fn macro_handler() {}

.route("/api/actix", web::post())

.route("/api/axum", post(axum_handler))
`

	matcher := &RustMatcher{}
	routes := matcher.Match([]byte(source))

	// We expect 3 routes: macro + actix + axum.
	if len(routes) != 3 {
		t.Fatalf("got %d routes, want 3 (macro + actix + axum)", len(routes))
	}

	// Collect by path.
	lineOf := make(map[string]uint32)
	for _, r := range routes {
		lineOf[r.Path] = r.Line
	}

	wantLines := map[string]uint32{
		"/api/macro": 2,
		"/api/actix": 5,
		"/api/axum":  7,
	}
	for path, want := range wantLines {
		got := lineOf[path]
		if got == 0 {
			t.Errorf("route %q: Line = 0, want %d (was Line=0 before fix)", path, want)
		} else if got != want {
			t.Errorf("route %q: Line = %d, want %d", path, got, want)
		}
	}
}

// TestRustMatcher_LineCapture_AxumOnly verifies that axum routes have Line != 0
// when no Actix/Rocket routes are present.
// This is the acme-edge scenario: axum-only codebase, was producing Route=0.
func TestRustMatcher_LineCapture_AxumOnly(t *testing.T) {
	t.Parallel()

	// Mirrors acme-edge crates/sfu/src/relay/handler.rs (lines 30-31):
	source := `    let app = Router::new()
        .route("/relay/connect", post(relay_connect))
        .with_state((secret, signing_public_key, task_tx, seen_jtis));`

	matcher := &RustMatcher{}
	routes := matcher.Match([]byte(source))

	if len(routes) != 1 {
		t.Fatalf("got %d routes, want 1", len(routes))
	}

	if routes[0].Line == 0 {
		t.Errorf("axum route Line = 0, want non-zero (acme-edge had Route=0 before fix)")
	}
	if routes[0].Path != "/relay/connect" {
		t.Errorf("Path = %q, want /relay/connect", routes[0].Path)
	}
	if routes[0].Method != "POST" {
		t.Errorf("Method = %q, want POST", routes[0].Method)
	}
	if routes[0].Handler != "relay_connect" {
		t.Errorf("Handler = %q, want relay_connect", routes[0].Handler)
	}
}

// TestRustMatcher_AcmeEdgeRoutes feeds all three real acme-edge routes in a
// single Router builder source and asserts that exactly three routes are extracted
// with the correct method, path, Side, Framework, and a non-zero Line.
// This directly guards the FU-CG.5 fix: before that fix, axum routes produced Line=0
// and the graph indexer silently dropped them.
func TestRustMatcher_AcmeEdgeRoutes(t *testing.T) {
	t.Parallel()

	source := `    let app = Router::new()
        .route("/sfu/ws/{room_id}", any(ws_handler))
        .route("/metrics", get(metrics_handler))
        .route("/relay/connect", post(relay_connect));`

	matcher := &RustMatcher{}
	routes := matcher.Match([]byte(source))

	if len(routes) != 3 {
		t.Fatalf("got %d routes, want 3 (all three real acme-edge routes)", len(routes))
	}

	type want struct {
		method  string
		path    string
		handler string
	}
	wantRoutes := []want{
		{method: "*", path: "/sfu/ws/*", handler: "ws_handler"},
		{method: "GET", path: "/metrics", handler: "metrics_handler"},
		{method: "POST", path: "/relay/connect", handler: "relay_connect"},
	}

	for i, w := range wantRoutes {
		r := routes[i]
		if r.Method != w.method {
			t.Errorf("routes[%d].Method = %q, want %q", i, r.Method, w.method)
		}
		if r.Path != w.path {
			t.Errorf("routes[%d].Path = %q, want %q", i, r.Path, w.path)
		}
		if r.Handler != w.handler {
			t.Errorf("routes[%d].Handler = %q, want %q", i, r.Handler, w.handler)
		}
		if r.Side != "server" {
			t.Errorf("routes[%d].Side = %q, want server", i, r.Side)
		}
		if r.Framework != "axum" {
			t.Errorf("routes[%d].Framework = %q, want axum", i, r.Framework)
		}
		if r.Line == 0 {
			t.Errorf("routes[%d] %q: Line = 0, want non-zero (FU-CG.5)", i, r.Path)
		}
	}
}

// TestRustMatcher_AxumMultiLineMetrics verifies the multi-line closure form of
// an axum route — the real acme-edge /metrics endpoint splits across lines:
//
//	.route(
//	    "/metrics",
//	    get(move || async move { render_metrics() }),
//	)
//
// The regex must capture path=/metrics, method=GET, Handler="" (closure),
// and Line set to the .route( line.  A future regex refactor that accidentally
// breaks multi-line matching will fail here.
func TestRustMatcher_AxumMultiLineMetrics(t *testing.T) {
	t.Parallel()

	source := `    .route(
        "/metrics",
        get(move || async move { render_metrics() }),
    )`

	matcher := &RustMatcher{}
	routes := matcher.Match([]byte(source))

	if len(routes) != 1 {
		t.Fatalf("got %d routes, want 1 (multi-line /metrics closure)", len(routes))
	}

	r := routes[0]
	if r.Method != "GET" {
		t.Errorf("Method = %q, want GET", r.Method)
	}
	if r.Path != "/metrics" {
		t.Errorf("Path = %q, want /metrics", r.Path)
	}
	if r.Handler != "" {
		t.Errorf("Handler = %q, want empty (closure, not a named fn)", r.Handler)
	}
	if r.Side != "server" {
		t.Errorf("Side = %q, want server", r.Side)
	}
	if r.Framework != "axum" {
		t.Errorf("Framework = %q, want axum", r.Framework)
	}
	if r.Line == 0 {
		t.Errorf("Line = 0, want line of the .route( call")
	}
}

// TestRustMatcher_ExistingPatternsStillWork verifies the pre-existing Actix/Rocket
// patterns still extract routes correctly after the axum additions (regression guard).
func TestRustMatcher_ExistingPatternsStillWork(t *testing.T) {
	t.Parallel()

	source := `
#[get("/api/users")]
async fn list_users() -> impl Responder { todo!() }

#[post("/api/users")]
async fn create_user() -> impl Responder { todo!() }

.route("/api/items", web::get())
`

	matcher := &RustMatcher{}
	routes := matcher.Match([]byte(source))

	if len(routes) != 3 {
		t.Fatalf("got %d routes, want 3 (2 macros + 1 actix builder)", len(routes))
	}

	// Macro routes.
	if routes[0].Method != "GET" || routes[0].Path != "/api/users" {
		t.Errorf("macro route[0]: got Method=%q Path=%q, want GET /api/users", routes[0].Method, routes[0].Path)
	}
	if routes[1].Method != "POST" || routes[1].Path != "/api/users" {
		t.Errorf("macro route[1]: got Method=%q Path=%q, want POST /api/users", routes[1].Method, routes[1].Path)
	}
	// Actix builder.
	if routes[2].Method != "GET" || routes[2].Path != "/api/items" {
		t.Errorf("actix builder route[2]: got Method=%q Path=%q, want GET /api/items", routes[2].Method, routes[2].Path)
	}

	for i, r := range routes {
		if r.Side != "server" {
			t.Errorf("route[%d].Side = %q, want server", i, r.Side)
		}
		if r.Line == 0 {
			t.Errorf("route[%d] %q: Line = 0, want non-zero", i, r.Path)
		}
	}
}

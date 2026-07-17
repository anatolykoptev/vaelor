package routes

import "regexp"

// RustMatcher extracts HTTP routes from Rust source code.
// Supported frameworks:
//   - Actix-web / Rocket: attribute macros (#[get("/path")], #[post("/path")], etc.)
//   - Actix-web: builder pattern (.route("/path", web::get()))
//   - Axum: builder pattern (.route("/path", get(handler)), .route("/path", any(handler)))
//
// acme-edge uses axum 0.8 exclusively; adding the axum
// matcher resolves the Route=0 gap observed in its AGE graph (FU-CG.5).
type RustMatcher struct{}

func init() {
	Register(&RustMatcher{})
}

// Language returns the language identifier for this matcher.
func (r *RustMatcher) Language() string { return "rust" }

// Server-side patterns.
var (
	// Rocket / Actix attribute macros: #[get("/path")], #[post("/path")], etc.
	// Captures: 1=method, 2=path.
	rustMacroRouteRe = regexp.MustCompile(
		`#\[(get|post|put|delete|patch|head|options)\(\s*"([^"]+)"`,
	)

	// Actix builder: .route("/path", web::get()).
	// Captures: 1=path, 2=method.
	rustActixRouteRe = regexp.MustCompile(
		`\.route\(\s*"([^"]+)"\s*,\s*web::(get|post|put|delete|patch|head|options)\(\)`,
	)

	// Axum builder: .route("/path", method(handler)).
	// method is one of get/post/put/delete/patch/head/options/any.
	// The optional handler group (group 3) captures a bare identifier immediately
	// following the opening paren.  Closures (move ||, |req|, async move) set
	// group 3 to a keyword that is filtered out by axumHandler below.
	// Captures: 1=path, 2=method-fn, 3=optional-ident (may be a keyword for closures).
	//
	// Known unsupported axum forms (acceptable gaps for a regex extractor — none
	// used by acme-edge; revisit if a target repo needs them):
	//   - method-chaining: .route("/p", get(h).post(h2)) captures only the first method
	//   - .nest("/prefix", router) — nested sub-router routes
	//   - .route_service("/p", svc) — tower Service routes
	rustAxumRouteRe = regexp.MustCompile(
		`\.route\(\s*"([^"]+)"\s*,\s*(get|post|put|delete|patch|head|options|any)\(([A-Za-z_][A-Za-z0-9_]*)?`,
	)
)

// axumMethod maps the axum routing helper name to a normalised HTTP method.
// "any" becomes "*" (matches all methods — used for WebSocket upgrades etc.).
func axumMethod(fn string) string {
	if fn == "any" {
		return "*"
	}
	return normalizeMethod(fn)
}

// rustKeywords is the set of Rust keywords that may appear as the first token
// inside a closure argument (e.g. "move" in get(move || ...), "async" in
// get(async move { ... })).  When the captured optional-handler ident is one
// of these, we treat Handler as empty (it is not a function name).
var rustKeywords = map[string]bool{
	"move": true, "async": true, "fn": true, "let": true, "return": true,
	"if": true, "else": true, "match": true, "loop": true, "while": true,
	"for": true, "in": true, "pub": true, "use": true, "self": true,
}

// axumHandler returns the handler name captured from an axum route match, or
// empty string if the captured token is a Rust keyword (closure preamble).
func axumHandler(raw string) string {
	if rustKeywords[raw] {
		return ""
	}
	return raw
}

// Match scans Rust source and returns all detected routes.
// Each returned Route has its Line field set to the 1-based source line number,
// a hard prerequisite for the CG-T3 enclosing-function resolver.
func (r *RustMatcher) Match(source []byte) []Route {
	var routes []Route

	// Server: Rocket / Actix attribute macros.
	for _, loc := range rustMacroRouteRe.FindAllSubmatchIndex(source, -1) {
		// loc layout: [full0,full1, g1s,g1e, g2s,g2e]
		method := normalizeMethod(string(source[loc[2]:loc[3]]))
		raw := string(source[loc[4]:loc[5]])
		routes = append(routes, Route{
			Method:    method,
			Path:      NormalizePath(raw),
			RawPath:   raw,
			Framework: "rust",
			Side:      "server",
			Line:      lineAt(source, loc[0]),
		})
	}

	// Server: Actix builder pattern.
	for _, loc := range rustActixRouteRe.FindAllSubmatchIndex(source, -1) {
		// loc layout: [full0,full1, g1s,g1e, g2s,g2e]
		raw := string(source[loc[2]:loc[3]])
		method := normalizeMethod(string(source[loc[4]:loc[5]]))
		routes = append(routes, Route{
			Method:    method,
			Path:      NormalizePath(raw),
			RawPath:   raw,
			Framework: "rust",
			Side:      "server",
			Line:      lineAt(source, loc[0]),
		})
	}

	// Server: Axum builder pattern.
	for _, loc := range rustAxumRouteRe.FindAllSubmatchIndex(source, -1) {
		// loc layout: [full0,full1, g1s,g1e, g2s,g2e, g3s,g3e]
		raw := string(source[loc[2]:loc[3]])
		fn := string(source[loc[4]:loc[5]])
		var handler string
		if loc[6] >= 0 {
			// Filter out Rust keywords that appear as closure preambles
			// (e.g. "move" in get(move || ...), "async" in get(async move { ... })).
			handler = axumHandler(string(source[loc[6]:loc[7]]))
		}
		routes = append(routes, Route{
			Method:    axumMethod(fn),
			Path:      NormalizePath(raw),
			RawPath:   raw,
			Handler:   handler,
			Framework: "axum",
			Side:      "server",
			Line:      lineAt(source, loc[0]),
		})
	}

	return routes
}

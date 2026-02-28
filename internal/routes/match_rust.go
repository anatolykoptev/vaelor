package routes

import "regexp"

// RustMatcher extracts HTTP routes from Rust source code (Actix, Rocket).
type RustMatcher struct{}

func init() {
	Register(&RustMatcher{})
}

// Language returns the language identifier for this matcher.
func (r *RustMatcher) Language() string { return "rust" }

// Server-side patterns.
var (
	// Rocket / Actix macros: #[get("/path")], #[post("/path")], etc.
	rustMacroRouteRe = regexp.MustCompile(
		`#\[(get|post|put|delete|patch)\(\s*"([^"]+)"`,
	)

	// Actix builder: .route("/path", web::get()).
	rustActixRouteRe = regexp.MustCompile(
		`\.route\(\s*"([^"]+)"\s*,\s*web::(get|post|put|delete|patch)\(\)`,
	)
)

// Match scans Rust source and returns all detected routes.
func (r *RustMatcher) Match(source []byte) []Route {
	var routes []Route

	// Server: Rocket / Actix attribute macros.
	for _, m := range rustMacroRouteRe.FindAllSubmatch(source, -1) {
		method := normalizeMethod(string(m[1]))
		raw := string(m[2])
		routes = append(routes, Route{
			Method:    method,
			Path:      NormalizePath(raw),
			RawPath:   raw,
			Framework: "rust",
			Side:      "server",
		})
	}

	// Server: Actix builder pattern.
	for _, m := range rustActixRouteRe.FindAllSubmatch(source, -1) {
		raw := string(m[1])
		method := normalizeMethod(string(m[2]))
		routes = append(routes, Route{
			Method:    method,
			Path:      NormalizePath(raw),
			RawPath:   raw,
			Framework: "rust",
			Side:      "server",
		})
	}

	return routes
}

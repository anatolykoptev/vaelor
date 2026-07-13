package routes

import "regexp"

// PythonMatcher extracts HTTP routes from Python source code.
type PythonMatcher struct{}

func init() {
	Register(&PythonMatcher{})
}

// Language returns the language identifier for this matcher.
func (p *PythonMatcher) Language() string { return "python" }

// Server-side patterns.
var (
	// Flask: @app.route("/path", methods=["GET", "POST"]).
	pyFlaskRouteRe = regexp.MustCompile(
		`@\w+\.route\(\s*["']([^"']+)["']`,
	)

	// FastAPI: @router.get("/path"), @app.post("/path"), etc.
	pyFastAPIRe = regexp.MustCompile(
		`@\w+\.(get|post|put|delete|patch)\(\s*["']([^"']+)["']`,
	)
)

// Client-side patterns.
var (
	// requests.get("url"), httpx.post("url"), aiohttp.get("url").
	pyHTTPClientRe = regexp.MustCompile(
		`(?:requests|httpx|aiohttp)\.(get|post|put|delete|patch)\(\s*["']([^"']+)["']`,
	)
)

// Match scans Python source and returns all detected routes.
// Each returned Route has its Line field set to the 1-based line number of the
// match in source — a hard prerequisite for the enclosing-function resolver.
func (p *PythonMatcher) Match(source []byte) []Route {
	var routes []Route

	// Server: Flask @app.route.
	for _, loc := range pyFlaskRouteRe.FindAllSubmatchIndex(source, -1) {
		// loc layout: [full0,full1, g1s,g1e]
		raw := string(source[loc[2]:loc[3]])
		routes = append(routes, Route{
			Method:    "*",
			Path:      NormalizePath(raw),
			RawPath:   raw,
			Framework: "python",
			Side:      "server",
			Line:      lineAt(source, loc[0]),
		})
	}

	// Server: FastAPI @router.get / @app.post.
	for _, loc := range pyFastAPIRe.FindAllSubmatchIndex(source, -1) {
		// loc layout: [full0,full1, g1s,g1e, g2s,g2e]
		method := normalizeMethod(string(source[loc[2]:loc[3]]))
		raw := string(source[loc[4]:loc[5]])
		routes = append(routes, Route{
			Method:    method,
			Path:      NormalizePath(raw),
			RawPath:   raw,
			Framework: "python",
			Side:      "server",
			Line:      lineAt(source, loc[0]),
		})
	}

	// Client: requests / httpx / aiohttp.
	for _, loc := range pyHTTPClientRe.FindAllSubmatchIndex(source, -1) {
		// loc layout: [full0,full1, g1s,g1e, g2s,g2e]
		method := normalizeMethod(string(source[loc[2]:loc[3]]))
		raw := string(source[loc[4]:loc[5]])
		routes = append(routes, Route{
			Method:    method,
			Path:      NormalizePath(raw),
			RawPath:   raw,
			Framework: "python",
			Side:      "client",
			Line:      lineAt(source, loc[0]),
		})
	}

	return routes
}

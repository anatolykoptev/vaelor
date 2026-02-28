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
func (p *PythonMatcher) Match(source []byte) []Route {
	var routes []Route

	// Server: Flask @app.route.
	for _, m := range pyFlaskRouteRe.FindAllSubmatch(source, -1) {
		raw := string(m[1])
		routes = append(routes, Route{
			Method:    "*",
			Path:      NormalizePath(raw),
			RawPath:   raw,
			Framework: "python",
			Side:      "server",
		})
	}

	// Server: FastAPI @router.get / @app.post.
	for _, m := range pyFastAPIRe.FindAllSubmatch(source, -1) {
		method := normalizeMethod(string(m[1]))
		raw := string(m[2])
		routes = append(routes, Route{
			Method:    method,
			Path:      NormalizePath(raw),
			RawPath:   raw,
			Framework: "python",
			Side:      "server",
		})
	}

	// Client: requests / httpx / aiohttp.
	for _, m := range pyHTTPClientRe.FindAllSubmatch(source, -1) {
		method := normalizeMethod(string(m[1]))
		raw := string(m[2])
		routes = append(routes, Route{
			Method:    method,
			Path:      NormalizePath(raw),
			RawPath:   raw,
			Framework: "python",
			Side:      "client",
		})
	}

	return routes
}

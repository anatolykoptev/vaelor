package routes

import "regexp"

// TypeScriptMatcher extracts HTTP routes from TypeScript and JavaScript source code.
type TypeScriptMatcher struct{}

func init() {
	m := &TypeScriptMatcher{}
	Register(m)

	// Also register for plain JavaScript — same patterns apply.
	registryMu.Lock()
	matchers["javascript"] = append(matchers["javascript"], m)
	registryMu.Unlock()
}

// Language returns the language identifier for this matcher.
func (ts *TypeScriptMatcher) Language() string { return "typescript" }

// Server-side patterns.
var (
	// Express / Fastify / Hono: app.get("/path", handler), router.post("/path", handler).
	// Captures: 1=receiver, 2=method, 3=path, 4=optional handler name.
	// Arrow functions and inline callbacks don't match group 4, leaving Handler empty.
	tsRouterMethodRe = regexp.MustCompile(
		`(\w+)\.(get|post|put|delete|patch)\(\s*["']([^"']+)["']\s*(?:,\s*([A-Za-z_]\w*))?`,
	)

	// NestJS decorators: @Get("/path"), @Post("/path"), etc.
	tsNestDecoratorRe = regexp.MustCompile(
		`@(Get|Post|Put|Delete|Patch)\(\s*["']([^"']+)["']`,
	)
)

// Client-side patterns.
var (
	// fetch("/path") or fetch("https://...").
	tsFetchRe = regexp.MustCompile(
		`fetch\(\s*["']([^"']+)["']`,
	)

	// axios.get("/path"), axios.post("/path"), etc.
	tsAxiosRe = regexp.MustCompile(
		`axios\.(get|post|put|delete|patch)\(\s*["']([^"']+)["']`,
	)
)

// Match scans TypeScript/JavaScript source and returns all detected routes.
func (ts *TypeScriptMatcher) Match(source []byte) []Route {
	var routes []Route

	// Server: Express/Fastify/Hono router methods.
	// Skip client-side libraries (axios, fetch) that share the same pattern.
	for _, m := range tsRouterMethodRe.FindAllSubmatch(source, -1) {
		receiver := string(m[1])
		if receiver == "axios" {
			continue // handled by tsAxiosRe as client route
		}
		method := normalizeMethod(string(m[2]))
		raw := string(m[3])
		var handler string
		if len(m) > 4 && len(m[4]) > 0 {
			handler = string(m[4])
		}
		routes = append(routes, Route{
			Method:    method,
			Path:      NormalizePath(raw),
			RawPath:   raw,
			Handler:   handler,
			Framework: "express",
			Side:      "server",
		})
	}

	// Server: NestJS decorators.
	for _, m := range tsNestDecoratorRe.FindAllSubmatch(source, -1) {
		method := normalizeMethod(string(m[1]))
		raw := string(m[2])
		routes = append(routes, Route{
			Method:    method,
			Path:      NormalizePath(raw),
			RawPath:   raw,
			Framework: "express",
			Side:      "server",
		})
	}

	// Client: fetch() — defaults to GET per Fetch API spec.
	for _, m := range tsFetchRe.FindAllSubmatch(source, -1) {
		raw := string(m[1])
		routes = append(routes, Route{
			Method:    "GET",
			Path:      NormalizePath(raw),
			RawPath:   raw,
			Framework: "fetch",
			Side:      "client",
		})
	}

	// Client: axios.method().
	for _, m := range tsAxiosRe.FindAllSubmatch(source, -1) {
		method := normalizeMethod(string(m[1]))
		raw := string(m[2])
		routes = append(routes, Route{
			Method:    method,
			Path:      NormalizePath(raw),
			RawPath:   raw,
			Framework: "express",
			Side:      "client",
		})
	}

	return routes
}

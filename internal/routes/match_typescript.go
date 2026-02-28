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
	// Captures the receiver name in group 1 so we can filter out client-side
	// libraries like axios that use the same .get()/.post() pattern.
	tsRouterMethodRe = regexp.MustCompile(
		`(\w+)\.(get|post|put|delete|patch)\(\s*["']([^"']+)["']`,
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
		routes = append(routes, Route{
			Method:    method,
			Path:      NormalizePath(raw),
			RawPath:   raw,
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

	// Client: fetch().
	for _, m := range tsFetchRe.FindAllSubmatch(source, -1) {
		raw := string(m[1])
		routes = append(routes, Route{
			Method:    "*",
			Path:      NormalizePath(raw),
			RawPath:   raw,
			Framework: "express",
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

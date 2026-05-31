package routes

import (
	"bytes"
	"regexp"
)

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

// lineAt returns the 1-based line number of the byte at offset within source.
// It counts the number of newlines before offset and adds 1.
func lineAt(source []byte, offset int) uint32 {
	if offset <= 0 {
		return 1
	}
	if offset > len(source) {
		offset = len(source)
	}
	return uint32(bytes.Count(source[:offset], []byte{'\n'})) + 1
}

// Match scans TypeScript/JavaScript source and returns all detected routes.
// Each returned Route has its Line field set to the 1-based line number of
// the match in source — a hard prerequisite for the enclosing-function resolver.
func (ts *TypeScriptMatcher) Match(source []byte) []Route {
	var routes []Route

	// Server: Express/Fastify/Hono router methods.
	// We use FindAllSubmatchIndex to obtain byte offsets for Line capture.
	// Skip receivers that are not on the allow-list (kills headers.get/map.get).
	// Skip axios (handled below as client route).
	for _, loc := range tsRouterMethodRe.FindAllSubmatchIndex(source, -1) {
		// loc layout: [full0,full1, g1s,g1e, g2s,g2e, g3s,g3e, g4s,g4e]
		receiver := string(source[loc[2]:loc[3]])
		if receiver == "axios" {
			continue // handled by tsAxiosRe as client route
		}
		if !isRouterReceiver(receiver) {
			continue // not a router identifier (e.g. headers, map, set)
		}
		method := normalizeMethod(string(source[loc[4]:loc[5]]))
		raw := string(source[loc[6]:loc[7]])
		var handler string
		if loc[8] >= 0 {
			handler = string(source[loc[8]:loc[9]])
		}
		routes = append(routes, Route{
			Method:    method,
			Path:      NormalizePath(raw),
			RawPath:   raw,
			Handler:   handler,
			Framework: "express",
			Side:      "server",
			Line:      lineAt(source, loc[0]),
		})
	}

	// Server: NestJS decorators.
	for _, loc := range tsNestDecoratorRe.FindAllSubmatchIndex(source, -1) {
		// loc: [full0,full1, g1s,g1e, g2s,g2e]
		method := normalizeMethod(string(source[loc[2]:loc[3]]))
		raw := string(source[loc[4]:loc[5]])
		routes = append(routes, Route{
			Method:    method,
			Path:      NormalizePath(raw),
			RawPath:   raw,
			Framework: "express",
			Side:      "server",
			Line:      lineAt(source, loc[0]),
		})
	}

	// Client: fetch() — defaults to GET per Fetch API spec.
	for _, loc := range tsFetchRe.FindAllSubmatchIndex(source, -1) {
		// loc: [full0,full1, g1s,g1e]
		raw := string(source[loc[2]:loc[3]])
		routes = append(routes, Route{
			Method:    "GET",
			Path:      NormalizePath(raw),
			RawPath:   raw,
			Framework: "fetch",
			Side:      "client",
			Line:      lineAt(source, loc[0]),
		})
	}

	// Client: axios.method().
	for _, loc := range tsAxiosRe.FindAllSubmatchIndex(source, -1) {
		// loc: [full0,full1, g1s,g1e, g2s,g2e]
		method := normalizeMethod(string(source[loc[2]:loc[3]]))
		raw := string(source[loc[4]:loc[5]])
		routes = append(routes, Route{
			Method:    method,
			Path:      NormalizePath(raw),
			RawPath:   raw,
			Framework: "express",
			Side:      "client",
			Line:      lineAt(source, loc[0]),
		})
	}

	return routes
}

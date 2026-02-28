package routes

import (
	"regexp"
	"strings"
)

// GoMatcher extracts HTTP routes from Go source code.
type GoMatcher struct{}

func init() {
	Register(&GoMatcher{})
}

// Language returns the language identifier for this matcher.
func (g *GoMatcher) Language() string { return "go" }

// Server-side patterns.
var (
	// http.HandleFunc("/path", handler) and http.Handle("/path", handler).
	handleFuncRe = regexp.MustCompile(
		`(?:http\.)?(?:HandleFunc|Handle)\(\s*"([^"]+)"\s*,\s*([A-Za-z_][A-Za-z0-9_.]*)\s*\)`,
	)

	// r.Get("/path", handler), r.Post("/path", handler), etc.
	// Covers chi (Get, Post, Put, Patch, Delete, Options, Head)
	// and gin/echo (GET, POST, PUT, PATCH, DELETE, OPTIONS, HEAD).
	routerMethodRe = regexp.MustCompile(
		`\w+\.(Get|Post|Put|Patch|Delete|Options|Head|GET|POST|PUT|PATCH|DELETE|OPTIONS|HEAD)\(\s*"([^"]+)"\s*,\s*([A-Za-z_][A-Za-z0-9_.]*)\s*\)`,
	)
)

// Client-side patterns.
var (
	// http.Get("url"), http.Post("url", ...), http.Head("url").
	httpShortcutRe = regexp.MustCompile(
		`http\.(Get|Post|Head)\(\s*"([^"]+)"`,
	)

	// http.NewRequest("METHOD", "url", ...) and
	// http.NewRequestWithContext(ctx, "METHOD", "url", ...).
	httpNewRequestRe = regexp.MustCompile(
		`http\.NewRequest(?:WithContext)?\([^,]*,?\s*"([A-Z]+)"\s*,\s*"([^"]+)"`,
	)
)

// Match scans Go source code and returns all detected routes.
func (g *GoMatcher) Match(source []byte) []Route {
	var routes []Route

	// Server: http.HandleFunc / http.Handle.
	for _, m := range handleFuncRe.FindAllSubmatch(source, -1) {
		raw := string(m[1])
		routes = append(routes, Route{
			Method:    "*",
			Path:      NormalizePath(raw),
			RawPath:   raw,
			Handler:   string(m[2]),
			Framework: "net/http",
			Side:      "server",
		})
	}

	// Server: router method calls (chi / gin / echo style).
	for _, m := range routerMethodRe.FindAllSubmatch(source, -1) {
		method := normalizeMethod(string(m[1]))
		raw := string(m[2])
		routes = append(routes, Route{
			Method:    method,
			Path:      NormalizePath(raw),
			RawPath:   raw,
			Handler:   string(m[3]),
			Framework: "chi",
			Side:      "server",
		})
	}

	// Client: http.Get / http.Post / http.Head.
	for _, m := range httpShortcutRe.FindAllSubmatch(source, -1) {
		method := normalizeMethod(string(m[1]))
		raw := string(m[2])
		routes = append(routes, Route{
			Method:    method,
			Path:      NormalizePath(raw),
			RawPath:   raw,
			Framework: "net/http",
			Side:      "client",
		})
	}

	// Client: http.NewRequest / http.NewRequestWithContext.
	for _, m := range httpNewRequestRe.FindAllSubmatch(source, -1) {
		method := normalizeMethod(string(m[1]))
		raw := string(m[2])
		routes = append(routes, Route{
			Method:    method,
			Path:      NormalizePath(raw),
			RawPath:   raw,
			Framework: "net/http",
			Side:      "client",
		})
	}

	return routes
}

// normalizeMethod converts mixed-case HTTP method names to uppercase.
func normalizeMethod(m string) string {
	return strings.ToUpper(m)
}

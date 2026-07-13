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
	// Also matches Go 1.22+ pattern: HandleFunc("GET /path", handler).
	handleFuncRe = regexp.MustCompile(
		`(?:http\.)?(?:HandleFunc|Handle)\(\s*"([^"]+)"\s*,\s*([A-Za-z_][A-Za-z0-9_.]*)\s*\)`,
	)

	// r.Get("/path", handler), r.Post("/path", handler), etc.
	// Covers chi (Get, Post, Put, Patch, Delete, Options, Head)
	// and gin/echo (GET, POST, PUT, PATCH, DELETE, OPTIONS, HEAD).
	routerMethodRe = regexp.MustCompile(
		`\w+\.(Get|Post|Put|Patch|Delete|Options|Head|GET|POST|PUT|PATCH|DELETE|OPTIONS|HEAD)\(\s*"([^"]+)"\s*,\s*([A-Za-z_][A-Za-z0-9_.]*)\s*\)`,
	)

	// go122MethodRe matches Go 1.22+ mux patterns like "GET /api/users".
	go122MethodRe = regexp.MustCompile(`^(GET|POST|PUT|PATCH|DELETE|OPTIONS|HEAD)\s+(.+)$`)
)

// Client-side patterns.
var (
	// http.Get("url"), http.Post("url", ...), http.Head("url").
	httpShortcutRe = regexp.MustCompile(
		`http\.(Get|Post|Head)\(\s*"([^"]+)"`,
	)

	// http.NewRequest("METHOD", "url", ...) and
	// http.NewRequestWithContext(ctx, "METHOD", "url", ...).
	// The URL must be a complete string literal (followed by , or )), not a
	// concatenated expression (followed by +).
	httpNewRequestRe = regexp.MustCompile(
		`http\.NewRequest(?:WithContext)?\([^,]*,?\s*"([A-Z]+)"\s*,\s*"([^"]+)"\s*[,)]`,
	)
)

// Match scans Go source code and returns all detected routes.
// Each returned Route has its Line field set to the 1-based line number of the
// match in source — a hard prerequisite for the enclosing-function resolver.
func (g *GoMatcher) Match(source []byte) []Route {
	var routes []Route

	// Server: http.HandleFunc / http.Handle.
	for _, loc := range handleFuncRe.FindAllSubmatchIndex(source, -1) {
		// loc layout: [full0,full1, g1s,g1e, g2s,g2e]
		raw := string(source[loc[2]:loc[3]])
		method := "*"
		path := raw
		// Go 1.22+ pattern: "GET /path" — extract method from the string.
		if parts := go122MethodRe.FindStringSubmatch(raw); parts != nil {
			method = parts[1]
			path = parts[2]
			// Go 1.22 host patterns: "GET example.com/api/data" — strip
			// the host portion so only the path remains.
			if !strings.HasPrefix(path, "/") {
				if slashIdx := strings.Index(path, "/"); slashIdx >= 0 {
					path = path[slashIdx:]
				}
			}
		}
		routes = append(routes, Route{
			Method:    method,
			Path:      NormalizePath(path),
			RawPath:   raw,
			Handler:   stripReceiver(string(source[loc[4]:loc[5]])),
			Framework: "net/http",
			Side:      "server",
			Line:      lineAt(source, loc[0]),
		})
	}

	// Server: router method calls (chi / gin / echo style).
	for _, loc := range routerMethodRe.FindAllSubmatchIndex(source, -1) {
		// loc layout: [full0,full1, g1s,g1e, g2s,g2e, g3s,g3e]
		method := normalizeMethod(string(source[loc[2]:loc[3]]))
		raw := string(source[loc[4]:loc[5]])
		routes = append(routes, Route{
			Method:    method,
			Path:      NormalizePath(raw),
			RawPath:   raw,
			Handler:   stripReceiver(string(source[loc[6]:loc[7]])),
			Framework: "chi",
			Side:      "server",
			Line:      lineAt(source, loc[0]),
		})
	}

	// Client: http.Get / http.Post / http.Head.
	for _, loc := range httpShortcutRe.FindAllSubmatchIndex(source, -1) {
		// loc layout: [full0,full1, g1s,g1e, g2s,g2e]
		method := normalizeMethod(string(source[loc[2]:loc[3]]))
		raw := string(source[loc[4]:loc[5]])
		routes = append(routes, Route{
			Method:    method,
			Path:      NormalizePath(raw),
			RawPath:   raw,
			Framework: "net/http",
			Side:      "client",
			Line:      lineAt(source, loc[0]),
		})
	}

	// Client: http.NewRequest / http.NewRequestWithContext.
	for _, loc := range httpNewRequestRe.FindAllSubmatchIndex(source, -1) {
		// loc layout: [full0,full1, g1s,g1e, g2s,g2e]
		method := normalizeMethod(string(source[loc[2]:loc[3]]))
		raw := string(source[loc[4]:loc[5]])
		routes = append(routes, Route{
			Method:    method,
			Path:      NormalizePath(raw),
			RawPath:   raw,
			Framework: "net/http",
			Side:      "client",
			Line:      lineAt(source, loc[0]),
		})
	}

	return routes
}

// normalizeMethod converts mixed-case HTTP method names to uppercase.
func normalizeMethod(m string) string {
	return strings.ToUpper(m)
}

// stripReceiver removes a receiver prefix from a handler name.
// For example, "s.HandleGetAccount" becomes "HandleGetAccount".
func stripReceiver(name string) string {
	if idx := strings.LastIndex(name, "."); idx >= 0 {
		return name[idx+1:]
	}
	return name
}

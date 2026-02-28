package routes

import "regexp"

// CSharpMatcher extracts HTTP routes from C# source code (ASP.NET).
type CSharpMatcher struct{}

func init() {
	Register(&CSharpMatcher{})
}

// Language returns the language identifier for this matcher.
func (cs *CSharpMatcher) Language() string { return "csharp" }

// Server-side patterns.
var (
	// ASP.NET attributes: [HttpGet("/path")], [HttpPost("/path")], etc.
	csAttributeRe = regexp.MustCompile(
		`\[Http(Get|Post|Put|Delete|Patch)\(\s*"([^"]+)"`,
	)

	// Minimal API: app.MapGet("/path", ...), app.MapPost("/path", ...).
	csMinimalAPIRe = regexp.MustCompile(
		`\w+\.Map(Get|Post|Put|Delete|Patch)\(\s*"([^"]+)"`,
	)
)

// Match scans C# source and returns all detected routes.
func (cs *CSharpMatcher) Match(source []byte) []Route {
	var routes []Route

	// Server: ASP.NET attributes.
	for _, m := range csAttributeRe.FindAllSubmatch(source, -1) {
		method := normalizeMethod(string(m[1]))
		raw := string(m[2])
		routes = append(routes, Route{
			Method:    method,
			Path:      NormalizePath(raw),
			RawPath:   raw,
			Framework: "aspnet",
			Side:      "server",
		})
	}

	// Server: Minimal API.
	for _, m := range csMinimalAPIRe.FindAllSubmatch(source, -1) {
		method := normalizeMethod(string(m[1]))
		raw := string(m[2])
		routes = append(routes, Route{
			Method:    method,
			Path:      NormalizePath(raw),
			RawPath:   raw,
			Framework: "aspnet",
			Side:      "server",
		})
	}

	return routes
}

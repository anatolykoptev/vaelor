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
// Each returned Route has its Line field set to the 1-based line number of the
// match in source — a hard prerequisite for the enclosing-function resolver.
func (cs *CSharpMatcher) Match(source []byte) []Route {
	var routes []Route

	// Server: ASP.NET attributes.
	for _, loc := range csAttributeRe.FindAllSubmatchIndex(source, -1) {
		// loc layout: [full0,full1, g1s,g1e, g2s,g2e]
		method := normalizeMethod(string(source[loc[2]:loc[3]]))
		raw := string(source[loc[4]:loc[5]])
		routes = append(routes, Route{
			Method:    method,
			Path:      NormalizePath(raw),
			RawPath:   raw,
			Framework: "aspnet",
			Side:      "server",
			Line:      lineAt(source, loc[0]),
		})
	}

	// Server: Minimal API.
	for _, loc := range csMinimalAPIRe.FindAllSubmatchIndex(source, -1) {
		// loc layout: [full0,full1, g1s,g1e, g2s,g2e]
		method := normalizeMethod(string(source[loc[2]:loc[3]]))
		raw := string(source[loc[4]:loc[5]])
		routes = append(routes, Route{
			Method:    method,
			Path:      NormalizePath(raw),
			RawPath:   raw,
			Framework: "aspnet",
			Side:      "server",
			Line:      lineAt(source, loc[0]),
		})
	}

	return routes
}

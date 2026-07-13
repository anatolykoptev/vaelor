package routes

import "regexp"

// JavaMatcher extracts HTTP routes from Java source code (Spring).
type JavaMatcher struct{}

func init() {
	Register(&JavaMatcher{})
}

// Language returns the language identifier for this matcher.
func (j *JavaMatcher) Language() string { return "java" }

// Server-side patterns.
var (
	// @GetMapping("/path"), @PostMapping("/path"), etc.
	javaMappingRe = regexp.MustCompile(
		`@(Get|Post|Put|Delete|Patch)Mapping\(\s*"([^"]+)"`,
	)

	// @RequestMapping(value = "/path", method = RequestMethod.GET).
	javaRequestMappingRe = regexp.MustCompile(
		`@RequestMapping\([^)]*(?:value\s*=\s*)?"([^"]+)"`,
	)
)

// Match scans Java source and returns all detected routes.
// Each returned Route has its Line field set to the 1-based line number of the
// match in source — a hard prerequisite for the enclosing-function resolver.
func (j *JavaMatcher) Match(source []byte) []Route {
	var routes []Route

	// Server: @GetMapping / @PostMapping / etc.
	for _, loc := range javaMappingRe.FindAllSubmatchIndex(source, -1) {
		// loc layout: [full0,full1, g1s,g1e, g2s,g2e]
		method := normalizeMethod(string(source[loc[2]:loc[3]]))
		raw := string(source[loc[4]:loc[5]])
		routes = append(routes, Route{
			Method:    method,
			Path:      NormalizePath(raw),
			RawPath:   raw,
			Framework: "spring",
			Side:      "server",
			Line:      lineAt(source, loc[0]),
		})
	}

	// Server: @RequestMapping.
	for _, loc := range javaRequestMappingRe.FindAllSubmatchIndex(source, -1) {
		// loc layout: [full0,full1, g1s,g1e]
		raw := string(source[loc[2]:loc[3]])
		routes = append(routes, Route{
			Method:    "*",
			Path:      NormalizePath(raw),
			RawPath:   raw,
			Framework: "spring",
			Side:      "server",
			Line:      lineAt(source, loc[0]),
		})
	}

	return routes
}

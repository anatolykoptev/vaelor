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
func (j *JavaMatcher) Match(source []byte) []Route {
	var routes []Route

	// Server: @GetMapping / @PostMapping / etc.
	for _, m := range javaMappingRe.FindAllSubmatch(source, -1) {
		method := normalizeMethod(string(m[1]))
		raw := string(m[2])
		routes = append(routes, Route{
			Method:    method,
			Path:      NormalizePath(raw),
			RawPath:   raw,
			Framework: "spring",
			Side:      "server",
		})
	}

	// Server: @RequestMapping.
	for _, m := range javaRequestMappingRe.FindAllSubmatch(source, -1) {
		raw := string(m[1])
		routes = append(routes, Route{
			Method:    "*",
			Path:      NormalizePath(raw),
			RawPath:   raw,
			Framework: "spring",
			Side:      "server",
		})
	}

	return routes
}

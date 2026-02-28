package routes

import "regexp"

// RubyMatcher extracts HTTP routes from Ruby source code (Sinatra, Rails).
type RubyMatcher struct{}

func init() {
	Register(&RubyMatcher{})
}

// Language returns the language identifier for this matcher.
func (rb *RubyMatcher) Language() string { return "ruby" }

// Server-side patterns.
var (
	// Sinatra: get '/path' do, post '/path' do.
	rubySinatraRe = regexp.MustCompile(
		`(?:get|post|put|delete|patch)\s+['"]([^'"]+)['"]`,
	)
)

// Match scans Ruby source and returns all detected routes.
func (rb *RubyMatcher) Match(source []byte) []Route {
	var routes []Route

	// Server: Sinatra-style routes.
	for _, m := range rubySinatraRe.FindAllSubmatch(source, -1) {
		raw := string(m[1])
		// Extract the method keyword from the full match.
		full := string(m[0])
		method := extractRubyMethod(full)
		routes = append(routes, Route{
			Method:    normalizeMethod(method),
			Path:      NormalizePath(raw),
			RawPath:   raw,
			Framework: "ruby",
			Side:      "server",
		})
	}

	return routes
}

// rubyMethodRe extracts the HTTP method keyword from a Sinatra route declaration.
var rubyMethodRe = regexp.MustCompile(`^(get|post|put|delete|patch)`)

// extractRubyMethod returns the HTTP method from a Sinatra route match.
func extractRubyMethod(match string) string {
	m := rubyMethodRe.FindString(match)
	if m == "" {
		return "*"
	}
	return m
}

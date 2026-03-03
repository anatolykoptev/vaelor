package routes

import (
	"regexp"
	"strings"
)

// PHPHookMatcher extracts WordPress hook registrations and invocations
// from PHP source code as Route entries.
//
// Hook registrations (add_action/add_filter) map to server-side routes.
// Hook invocations (do_action/apply_filters) map to client-side routes.
type PHPHookMatcher struct{}

func init() {
	Register(&PHPHookMatcher{})
}

// Language returns the language identifier for this matcher.
func (m *PHPHookMatcher) Language() string { return "php" }

// Regex patterns for WordPress hook functions.
var (
	// add_action('hook_name', 'callback_func') / add_filter('hook_name', 'callback_func')
	phpHookStringCallbackRe = regexp.MustCompile(
		`(add_action|add_filter)\s*\(\s*['"]([^'"]+)['"]\s*,\s*['"](\w+)['"]`,
	)

	// add_action('hook_name', [$this, 'method']) / add_filter(...)
	phpHookInstanceCallbackRe = regexp.MustCompile(
		`(add_action|add_filter)\s*\(\s*['"]([^'"]+)['"]\s*,\s*\[\s*\$\w+\s*,\s*['"](\w+)['"]\s*\]`,
	)

	// add_action('hook_name', array( $this, 'method' )) / add_filter(...)
	// Traditional PHP array() syntax — very common in WordPress plugins.
	phpHookArrayCallbackRe = regexp.MustCompile(
		`(add_action|add_filter)\s*\(\s*['"]([^'"]+)['"]\s*,\s*array\s*\(\s*\$\w+\s*,\s*['"](\w+)['"]\s*\)`,
	)

	// add_action('hook_name', [ClassName::class, 'method']) / add_filter(...)
	phpHookStaticCallbackRe = regexp.MustCompile(
		`(add_action|add_filter)\s*\(\s*['"]([^'"]+)['"]\s*,\s*\[\s*(\w+)::class\s*,\s*['"](\w+)['"]\s*\]`,
	)

	// do_action('hook_name', ...) / apply_filters('hook_name', ...)
	phpHookFireRe = regexp.MustCompile(
		`(do_action|apply_filters)\s*\(\s*['"]([^'"]+)['"]`,
	)
)

// hookMethod maps a WP hook registration function to a Route method.
func hookMethod(fn string) string {
	if strings.Contains(fn, "filter") || strings.Contains(fn, "filters") {
		return "FILTER"
	}
	return "ACTION"
}

// Match scans PHP source code and returns all detected WordPress hook routes.
func (m *PHPHookMatcher) Match(source []byte) []Route {
	var out []Route

	// String callbacks: add_action('hook', 'func')
	for _, match := range phpHookStringCallbackRe.FindAllSubmatch(source, -1) {
		fn := string(match[1])
		hookName := string(match[2])
		handler := string(match[3])
		out = append(out, Route{
			Method:    hookMethod(fn),
			Path:      hookName,
			RawPath:   hookName,
			Handler:   handler,
			Framework: "wordpress",
			Side:      "server",
		})
	}

	// Instance callbacks: add_action('hook', [$this, 'method'])
	for _, match := range phpHookInstanceCallbackRe.FindAllSubmatch(source, -1) {
		fn := string(match[1])
		hookName := string(match[2])
		handler := string(match[3])
		out = append(out, Route{
			Method:    hookMethod(fn),
			Path:      hookName,
			RawPath:   hookName,
			Handler:   handler,
			Framework: "wordpress",
			Side:      "server",
		})
	}

	// Instance callbacks with array() syntax: add_action('hook', array($this, 'method'))
	for _, match := range phpHookArrayCallbackRe.FindAllSubmatch(source, -1) {
		fn := string(match[1])
		hookName := string(match[2])
		handler := string(match[3])
		out = append(out, Route{
			Method:    hookMethod(fn),
			Path:      hookName,
			RawPath:   hookName,
			Handler:   handler,
			Framework: "wordpress",
			Side:      "server",
		})
	}

	// Static callbacks: add_action('hook', [ClassName::class, 'method'])
	for _, match := range phpHookStaticCallbackRe.FindAllSubmatch(source, -1) {
		fn := string(match[1])
		hookName := string(match[2])
		handler := string(match[4])
		out = append(out, Route{
			Method:    hookMethod(fn),
			Path:      hookName,
			RawPath:   hookName,
			Handler:   handler,
			Framework: "wordpress",
			Side:      "server",
		})
	}

	// Hook invocations: do_action('hook') / apply_filters('filter', $val)
	for _, match := range phpHookFireRe.FindAllSubmatch(source, -1) {
		fn := string(match[1])
		hookName := string(match[2])
		out = append(out, Route{
			Method:    hookMethod(fn),
			Path:      hookName,
			RawPath:   hookName,
			Framework: "wordpress",
			Side:      "client",
		})
	}

	return dedupRoutes(out)
}

// dedupRoutes removes duplicate routes with same Method+Path+Handler+Side.
func dedupRoutes(in []Route) []Route {
	type key struct{ method, path, handler, side string }
	seen := make(map[key]bool, len(in))
	out := make([]Route, 0, len(in))
	for _, r := range in {
		k := key{r.Method, r.Path, r.Handler, r.Side}
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, r)
	}
	return out
}

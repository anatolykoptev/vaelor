package routes

import (
	"regexp"
	"strings"
)

// tsRouterReceivers are the identifiers that legitimately register HTTP routes
// in JS/TS frameworks.  The TS matcher only treats `<recv>.get/post/...` as a
// route when recv is one of these — excluding map.get, headers.get, set.delete,
// etc. that share the same syntactic pattern but are not HTTP registration calls.
var tsRouterReceivers = map[string]bool{
	"app":      true,
	"router":   true,
	"r":        true,
	"fastify":  true,
	"server":   true,
	"route":    true,
	"api":      true,
	"instance": true,
}

// httpHeaderNames is the set of well-known HTTP header names that appear as
// bare single-segment paths when the TS matcher over-captures headers.get(…).
// Conservative: only names that look like route paths (all-word chars or hyphen).
var httpHeaderNames = map[string]bool{
	"authorization":     true,
	"content-type":      true,
	"content-length":    true,
	"accept":            true,
	"accept-encoding":   true,
	"accept-language":   true,
	"cache-control":     true,
	"cookie":            true,
	"set-cookie":        true,
	"x-request-id":      true,
	"x-forwarded-for":   true,
	"x-real-ip":         true,
	"host":              true,
	"origin":            true,
	"referer":           true,
	"user-agent":        true,
	"if-none-match":     true,
	"if-modified-since": true,
	"etag":              true,
	"location":          true,
}

// bareHexRe matches a path whose sole segment (after the leading slash) is a
// run of hex characters 24 or more chars long (bare hash / UUID token).
// Threshold 24 prevents matching version prefixes like "/v1" or short IDs.
var bareHexRe = regexp.MustCompile(`^/[0-9a-fA-F]{24,}$`)

// IsJunkPath reports whether an extracted route path is too low-quality to be
// a real HTTP route.  It is shared by ingest (codegraph.extractRoutes) and may
// be used by the coupling verifier for lockstep precision.
//
// Rules (conservative — false-positives cost real routes; false-negatives only
// allow a few junk survivors):
//
//  1. Contains '?' — a query-string fragment, not a route template.
//     e.g. "/api/leak?c=" from fetch('/api/leak?c='+cookie)
//
//  2. Sole path segment matches a known HTTP header name (case-insensitive).
//     e.g. "/Authorization" from headers.get('Authorization').
//
//  3. Sole path segment is exactly 1 character.
//     e.g. "/A", "/a" from map.get('/A').
//
//  4. Sole path segment is 24+ lowercase/uppercase hex chars (bare token or UUID).
//     e.g. "/a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4".
func IsJunkPath(path string) bool {
	// Rule 1: query string.
	if strings.Contains(path, "?") {
		return true
	}

	// For rules 2-4 we only examine single-segment paths: those with exactly
	// one non-empty segment after the leading slash.
	trimmed := strings.TrimPrefix(path, "/")
	if strings.Contains(trimmed, "/") {
		// Multi-segment paths can't be junk by these rules.
		return false
	}
	if trimmed == "" {
		// Root "/" is a real (if generic) path.
		return false
	}
	seg := trimmed

	// Rule 2: known HTTP header name.
	if httpHeaderNames[strings.ToLower(seg)] {
		return true
	}

	// Rule 3: single character.
	if len([]rune(seg)) == 1 {
		return true
	}

	// Rule 4: bare hex token.
	if bareHexRe.MatchString(path) {
		return true
	}

	return false
}

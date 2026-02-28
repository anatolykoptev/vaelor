package routes

import (
	"regexp"
	"strings"
	"sync"
)

// Route represents an HTTP route extracted from source code.
type Route struct {
	Method    string // "GET", "POST", "*"
	Path      string // normalized path
	RawPath   string // original path from source
	Handler   string // symbol name
	Framework string // "net/http", "chi", "express", etc.
	File      string
	Line      uint32
	Side      string // "server" or "client"
}

// RouteMatcher extracts routes from source code for a specific language.
type RouteMatcher interface {
	Language() string
	Match(source []byte) []Route
}

var (
	registryMu sync.RWMutex
	matchers   = make(map[string][]RouteMatcher)
)

// Register adds a matcher to the global registry.
func Register(m RouteMatcher) {
	registryMu.Lock()
	defer registryMu.Unlock()

	lang := m.Language()
	matchers[lang] = append(matchers[lang], m)
}

// ExtractAll runs all registered matchers for the given language and returns
// the combined set of discovered routes.
func ExtractAll(language string, source []byte) []Route {
	registryMu.RLock()
	ms := matchers[language]
	registryMu.RUnlock()

	var all []Route
	for _, m := range ms {
		all = append(all, m.Match(source)...)
	}

	return all
}

// paramColonRe matches colon-style path parameters like :id, :userId.
var paramColonRe = regexp.MustCompile(`/:([A-Za-z_][A-Za-z0-9_]*)`)

// paramBraceRe matches brace-style path parameters like {id}, {userId}.
var paramBraceRe = regexp.MustCompile(`/\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// doubleSlashRe matches consecutive slashes.
var doubleSlashRe = regexp.MustCompile(`//+`)

// NormalizePath normalizes a route path:
//   - Strips URL scheme and host (if the path contains "://")
//   - Ensures a leading slash
//   - Replaces path parameters (:id, {id}) with *
//   - Cleans double slashes
func NormalizePath(raw string) string {
	p := raw

	// Strip scheme + host.
	if idx := strings.Index(p, "://"); idx >= 0 {
		// Find the first slash after the host.
		rest := p[idx+len("://"):]
		if slashIdx := strings.Index(rest, "/"); slashIdx >= 0 {
			p = rest[slashIdx:]
		} else {
			p = "/"
		}
	}

	// Ensure leading slash.
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}

	// Replace :param with *.
	p = paramColonRe.ReplaceAllString(p, "/*")

	// Replace {param} with *.
	p = paramBraceRe.ReplaceAllString(p, "/*")

	// Clean double slashes.
	p = doubleSlashRe.ReplaceAllString(p, "/")

	// Remove trailing slash (except root).
	if len(p) > 1 {
		p = strings.TrimRight(p, "/")
	}

	return p
}

// cmd/go-code/http_resolve.go
package main

import (
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"strings"

	"github.com/anatolykoptev/go-code/internal/sourcemap"
	"github.com/anatolykoptev/go-kit/ratelimit"
)

// resolveRequest is the JSON body for POST /resolve.
type resolveRequest struct {
	URL    string `json:"url"`
	Line   int    `json:"line"`
	Column int    `json:"column"`
}

// resolveHTTPHandler returns an http.HandlerFunc that resolves a minified JS
// stack frame to its original source location using the provided Resolver.
//
// Security assumption: the runtime compose binds go-code's MCP port to
// 127.0.0.1, so POST /resolve is only reachable from the local host. The
// endpoint is not authenticated; it relies on the SOURCEMAP_ALLOWED_HOSTS
// allowlist and the optional per-IP rate limiter. If /resolve is ever exposed
// beyond loopback, add authentication and configure SOURCEMAP_RATE_LIMIT.
//
// Only POST is accepted; the request URL must match one of allowedHosts.
// Response codes: 200 OK, 400 bad JSON, 403 disallowed host, 405 wrong method,
// 429 rate limit exceeded, 502 resolve error.
func resolveHTTPHandler(allowedHosts []string, resolver *sourcemap.Resolver, limiter *ratelimit.KeyLimiter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		if limiter != nil && !limiter.Allow(resolveClientIP(r)) {
			http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
			return
		}

		var req resolveRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
			return
		}

		if !sourcemap.IsAllowedURL(req.URL, allowedHosts) {
			http.Error(w, "forbidden: host not in allowlist", http.StatusForbidden)
			return
		}

		frame, err := resolver.Resolve(r.Context(), req.URL, req.Line, req.Column)
		if err != nil {
			http.Error(w, "resolve error: "+err.Error(), http.StatusBadGateway)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(frame); err != nil {
			slog.Warn("resolve: encode response", "err", err, "url", req.URL)
		}
	}
}

// resolveClientIP returns the client IP for per-IP rate limiting. It prefers
// the first X-Forwarded-For address, falling back to RemoteAddr. Loopback
// deployments (the current default) have no proxy and RemoteAddr is 127.0.0.1;
// if /resolve is exposed behind a proxy, ensure X-Forwarded-For is sanitized
// before trusting it.
func resolveClientIP(r *http.Request) string {
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		if host, _, _ := net.SplitHostPort(strings.TrimSpace(strings.Split(fwd, ",")[0])); host != "" {
			return host
		}
		return strings.TrimSpace(strings.Split(fwd, ",")[0])
	}
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	return host
}

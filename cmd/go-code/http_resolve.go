// cmd/go-code/http_resolve.go
package main

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/anatolykoptev/go-code/internal/sourcemap"
)

// resolveRequest is the JSON body for POST /resolve.
type resolveRequest struct {
	URL    string `json:"url"`
	Line   int    `json:"line"`
	Column int    `json:"column"`
}

// resolveHTTPHandler returns an http.HandlerFunc that resolves a minified JS
// stack frame to its original source location using the provided Resolver.
// Only POST is accepted; the request URL must match one of allowedHosts.
// Response codes: 200 OK, 400 bad JSON, 403 disallowed host, 405 wrong method, 502 resolve error.
func resolveHTTPHandler(allowedHosts []string, resolver *sourcemap.Resolver) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
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

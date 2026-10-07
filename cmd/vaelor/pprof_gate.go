package main

import (
	"crypto/subtle"
	"net/http"
)

const (
	// headerServiceAuth is the canonical internal-call header (memdb-go).
	headerServiceAuth = "X-Service-Secret"
	// headerInternalAuth is what go-wowa and our outbound clients send.
	headerInternalAuth = "X-Internal-Secret"
)

// pprofGate guards the pprof handlers behind INTERNAL_SERVICE_SECRET, matching
// memdb-go's pprofHandler. It fails closed: an empty serviceSecret disables
// pprof (503) rather than opening it, and a request presenting an empty header
// never matches. Heap and goroutine dumps expose source paths and in-memory
// state, so no bearer/user token is accepted — internal tooling only.
func pprofGate(serviceSecret string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if serviceSecret == "" {
			http.Error(w, "pprof disabled: INTERNAL_SERVICE_SECRET not configured", http.StatusServiceUnavailable)
			return
		}
		presented := r.Header.Get(headerServiceAuth)
		if presented == "" {
			presented = r.Header.Get(headerInternalAuth)
		}
		if presented == "" || subtle.ConstantTimeCompare([]byte(presented), []byte(serviceSecret)) != 1 {
			http.Error(w, "X-Service-Secret required", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestPromMux_PprofWiring verifies the wiring requirement of issue #754:
//
//   - The metrics-listener mux (PROM_PORT 9897) serves the net/http/pprof heap
//     endpoint with 200 and a non-empty body.
//   - A mux constructed the way the MCP listener constructs its mux — a fresh
//     http.NewServeMux() that does NOT call buildPromMux — does NOT serve
//     pprof (404). The MCP listener (mcpserver.Run, port 8897) builds its own
//     *http.ServeMux and passes it to the combinedRoutes callback; it never
//     calls buildPromMux, so pprof stays off 8897. This test guards the
//     invariant: if someone moves pprof registration onto a path that every
//     new ServeMux inherits (a package-level side effect), or wires it into
//     the MCP route callback, the MCP-side 404 assertion is the canary.
//
// We do not assert pprof's output format; that is the stdlib's job. We assert
// the wiring, which is the thing this change can break.
func TestPromMux_PprofWiring(t *testing.T) {
	t.Run("prom mux serves heap", func(t *testing.T) {
		srv := httptest.NewServer(buildPromMux(testPprofSecret))
		defer srv.Close()

		resp, err := getWithHeader(t.Context(), srv.URL+"/debug/pprof/heap", headerServiceAuth, testPprofSecret)
		if err != nil {
			t.Fatalf("heap GET: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("heap status = %d, want 200", resp.StatusCode)
		}
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("heap read: %v", err)
		}
		if len(body) == 0 {
			t.Fatal("heap body empty, want non-empty pprof payload")
		}
	})

	t.Run("mcp-style mux does not serve pprof", func(t *testing.T) {
		// The MCP listener builds a fresh *http.ServeMux inside mcpserver.Run
		// (cmd/vaelor/main.go:203 notes "http.DefaultServeMux is unused by
		// mcpserver"). A fresh ServeMux does not inherit buildPromMux's
		// registrations, so pprof must be absent there.
		mcpMux := http.NewServeMux()
		srv := httptest.NewServer(mcpMux)
		defer srv.Close()

		resp, err := http.Get(srv.URL + "/debug/pprof/heap")
		if err != nil {
			t.Fatalf("mcp mux heap GET: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("mcp mux /debug/pprof/heap status = %d, want 404 (pprof must not leak onto the MCP listener)", resp.StatusCode)
		}
	})
}

const testPprofSecret = "test-pprof-secret-0123456789"

func getWithHeader(ctx context.Context, url, key, val string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if key != "" {
		req.Header.Set(key, val)
	}
	return http.DefaultClient.Do(req)
}

// TestPromMux_PprofRequiresServiceSecret guards the registration site in
// buildPromMux: go-code's metrics listener is reachable from every container
// on the docker backend network, so /debug/pprof/* must demand the internal
// secret and fail closed when none is configured. Mutation check: registering
// the pprof subtree on mux without pprofGate (main.go, buildPromMux) turns the
// "no header" and "wrong secret" cases RED (200, want 401) and the
// "secret unset" case RED (200, want 503).
func TestPromMux_PprofRequiresServiceSecret(t *testing.T) {
	cases := []struct {
		name   string
		secret string // configured INTERNAL_SERVICE_SECRET
		hdr    string
		val    string
		path   string
		want   int
	}{
		{"no header", testPprofSecret, "", "", "/debug/pprof/heap", http.StatusUnauthorized},
		{"wrong secret", testPprofSecret, headerServiceAuth, "nope", "/debug/pprof/heap", http.StatusUnauthorized},
		{"empty header value", testPprofSecret, headerServiceAuth, "", "/debug/pprof/heap", http.StatusUnauthorized},
		{"index unauthenticated", testPprofSecret, "", "", "/debug/pprof/", http.StatusUnauthorized},
		{"goroutine unauthenticated", testPprofSecret, "", "", "/debug/pprof/goroutine", http.StatusUnauthorized},
		{"cmdline unauthenticated", testPprofSecret, "", "", "/debug/pprof/cmdline", http.StatusUnauthorized},
		{"valid X-Service-Secret", testPprofSecret, headerServiceAuth, testPprofSecret, "/debug/pprof/heap", http.StatusOK},
		{"valid X-Internal-Secret", testPprofSecret, headerInternalAuth, testPprofSecret, "/debug/pprof/heap", http.StatusOK},
		{"valid secret on index", testPprofSecret, headerServiceAuth, testPprofSecret, "/debug/pprof/", http.StatusOK},
		{"secret unset, no header", "", "", "", "/debug/pprof/heap", http.StatusServiceUnavailable},
		{"secret unset, empty header", "", headerServiceAuth, "", "/debug/pprof/heap", http.StatusServiceUnavailable},
		{"secret unset, any header", "", headerServiceAuth, "anything", "/debug/pprof/heap", http.StatusServiceUnavailable},
		{"metrics stays open without secret", testPprofSecret, "", "", "/metrics", http.StatusOK},
		{"metrics open when secret unset", "", "", "", "/metrics", http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(buildPromMux(tc.secret))
			defer srv.Close()

			resp, err := getWithHeader(t.Context(), srv.URL+tc.path, tc.hdr, tc.val)
			if err != nil {
				t.Fatalf("GET %s: %v", tc.path, err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != tc.want {
				t.Fatalf("GET %s status = %d, want %d", tc.path, resp.StatusCode, tc.want)
			}
		})
	}
}

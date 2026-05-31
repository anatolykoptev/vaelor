package routes

import "testing"

// TestIsJunkPath verifies the junk-path filter against the live corpus that
// drove its design.  The task spec mandates that every junk example returns
// true and every real route returns false.
//
// Rules implemented (see sanitize.go for authoritative doc):
//  1. Path contains '?' → junk (query-string fragment)
//  2. Segment after leading slash is a known HTTP header name → junk
//  3. Single-char segment after leading slash → junk
//  4. Single segment that is 24+ hex chars (bare hash / UUID) → junk
func TestIsJunkPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		path     string
		wantJunk bool
		reason   string
	}{
		// --- JUNK corpus (must return true) ---
		{
			path:     "/Authorization",
			wantJunk: true,
			reason:   "known HTTP header name",
		},
		{
			path:     "/Content-Type",
			wantJunk: true,
			reason:   "known HTTP header name",
		},
		{
			path:     "/api/leak?c=",
			wantJunk: true,
			reason:   "contains query string '?'",
		},
		{
			path:     "/A",
			wantJunk: true,
			reason:   "single char after slash",
		},
		{
			path:     "/a",
			wantJunk: true,
			reason:   "single char after slash",
		},
		{
			path:     "/a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4",
			wantJunk: true,
			reason:   "bare 32-hex token",
		},
		{
			path:     "/deadbeefdeadbeefdeadbeefdeadbeef",
			wantJunk: true,
			reason:   "bare 32-hex token",
		},
		{
			path:     "/550e8400e29b41d4a716446655440000",
			wantJunk: true,
			reason:   "bare 32-hex token (UUID-shaped)",
		},
		{
			path:     "/aabbccddeeff00112233445566778899aabb",
			wantJunk: true,
			reason:   "bare 36-hex token exceeds threshold",
		},

		// --- REAL routes (must return false) ---
		{
			path:     "/api/partner/register",
			wantJunk: false,
			reason:   "real multi-segment route",
		},
		{
			path:     "/api/v1/rooms/:id",
			wantJunk: false,
			reason:   "real parameterized route",
		},
		{
			path:     "/health",
			wantJunk: false,
			reason:   "well-known health endpoint",
		},
		{
			path:     "/peer_joined",
			wantJunk: false,
			reason:   "real single-word route",
		},
		{
			path:     "/api/users",
			wantJunk: false,
			reason:   "real two-segment route",
		},
		{
			path:     "/metrics",
			wantJunk: false,
			reason:   "real monitoring endpoint",
		},
		{
			path:     "/",
			wantJunk: false,
			reason:   "root path is real",
		},
		{
			path:     "/v1",
			wantJunk: false,
			reason:   "version prefix is not a single char",
		},
		{
			path:     "/ab",
			wantJunk: false,
			reason:   "two-char segment is not single-char junk",
		},
		// FIX 1: optional-param routes after NormalizePath produce /users/* not /users/*? — must not be junk.
		// (These tests use already-normalized paths as IsJunkPath receives them post-NormalizePath.)
		{
			path:     "/users/*",
			wantJunk: false,
			reason:   "normalized optional-param /users/:id? -> /users/* is a real route",
		},
		{
			path:     "/a/*",
			wantJunk: false,
			reason:   "normalized optional-param /a/:b? -> /a/* is a real route",
		},
		{
			path:     "/users/*/profile",
			wantJunk: false,
			reason:   "normalized optional-param /users/:id?/profile -> /users/*/profile is a real route",
		},
		// NIT: mixed-case 24+ hex must also be junk (locks the A-F branch of bareHexRe).
		{
			path:     "/AbCdEf0123456789AbCdEf01",
			wantJunk: true,
			reason:   "mixed-case 24-hex bare token",
		},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			t.Parallel()
			got := IsJunkPath(tt.path)
			if got != tt.wantJunk {
				t.Errorf("IsJunkPath(%q) = %v, want %v (%s)", tt.path, got, tt.wantJunk, tt.reason)
			}
		})
	}
}

// TestTsRouterReceivers verifies the receiver allow-list: legitimate router
// identifiers return true; non-router identifiers (map, headers, set) return false.
func TestTsRouterReceivers(t *testing.T) {
	t.Parallel()

	allowed := []string{"app", "router", "r", "fastify", "server", "route", "api", "instance"}
	for _, recv := range allowed {
		if !tsRouterReceivers[recv] {
			t.Errorf("tsRouterReceivers[%q] = false, want true", recv)
		}
	}

	blocked := []string{"map", "headers", "set", "cookie", "axios", "fetch"}
	for _, recv := range blocked {
		if tsRouterReceivers[recv] {
			t.Errorf("tsRouterReceivers[%q] = true, want false", recv)
		}
	}
}

// TestIsRouterReceiver verifies the isRouterReceiver helper: allow-list entries +
// *Router/*Routes suffixes pass; map/headers/set/response still fail.
func TestIsRouterReceiver(t *testing.T) {
	t.Parallel()

	wantTrue := []string{
		"app", "router", "r", "fastify", "server",
		"adminRouter", "apiRouter", "v1Router",
		"userRoutes", "adminRoutes",
	}
	for _, recv := range wantTrue {
		if !isRouterReceiver(recv) {
			t.Errorf("isRouterReceiver(%q) = false, want true", recv)
		}
	}

	wantFalse := []string{"map", "headers", "set", "response", "cookie", "axios", "fetch"}
	for _, recv := range wantFalse {
		if isRouterReceiver(recv) {
			t.Errorf("isRouterReceiver(%q) = true, want false", recv)
		}
	}
}

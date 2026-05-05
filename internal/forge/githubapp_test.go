package forge

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// testRSAPEM is a 2048-bit RSA key generated for testing only.
// It is NOT used for any production secret.
const testRSAPEM = `-----BEGIN RSA PRIVATE KEY-----
MIIEowIBAAKCAQEAmZnGHJaeP/bZNLiU3rPcF3eBidyM1s2i1vXRlRnSBDjcTZ5f
no7azJ//97wpKb1eVobg6T8WJF0TwyUj/3JSGoJWZdwLlnBy4WPzi2FnZ3PAkZMw
xwG+RsHkk3vjAhfgMLii3jzlfT0RxrZISb2Pk1CUPn4QvxLKRu0ARBU9rMxW7Z/K
2AWlIguNa2NrMk728hSqS10GQm593QcoJVNuMm+v/5NnY1riiG58YNFnWshF6TDJ
lAwKV1SW1rKIEfhU7xQEgzFp0wtVsLtpi1tb/JHE7E52pstXhj0aTzd5Pc/94EXs
rXUfWFva5AbkapJGabbjLOCiSJ9KphxdwDaBBQIDAQABAoIBAAhhugwW0wFiDsW/
+c2ySmUUbiLwAFZ7Z7KrvNlSTKHS2YC5zvV3zaxDYeQqpiNjNEnr98t6mBJ5asno
FbALlLviF2VdDdvSfI5cli5prQsZ56z593wwlenGDFtY9BEJ7P+zn52ZfJtqMPVj
PowZlkNfbwt+9Rp8I8Idjjlo4FH0yeFVDTx70wIRsaZZda4VAsfeHbNbI6585lkp
vyCc+f73E/3PZlXFeCF/VUc+wVAWL5OjLcBaQrzn608D68Oez+2UCuatFlgIN31A
YeL7MkgX+e/A+U8jDIgUE8Rr8gFRSKIg3ZECKmzTrOpjo1qPtv+fd1h6+6Sc8yfY
ANHkAyECgYEA2S1ChzGK0cRaPJKc2cMk3mp+EvVqr2qno6vACXeXAVyusgJy4aU4
j0mV3K8dFIuqRT2iy0BfHWJNbQVxtZYfGAi/K3W7m512n1SndBRV2INP3TM/KeaY
rZLaG3QlL32BE8vveJXxv2KMq0YNuHvOr4exiACAtnC3rhJoRKvjhCECgYEAtQ8O
pJbsiVIU6Iv1eJLsr+a/cUX1jstHlr0SxYgX+PIYeFnDgeIC6DZ9zPJ3kB0W53ex
L0UNwOdO+11OuaQyv9n3rv/lXeLu/s3nmmKGXNoAdToKn954TWuMqwgeMa4tTzun
qzPBDHVvbQfuRgdGwvjSn6ofhfAFjM6/l+EHYGUCgYBxPUQ/MfnsPrG+e8QFV9dV
kbmDMSwbo0Ud9mP/i7fVIfqFHvm/5mKDdB8MHtLO77QsvmKwEDSIIcW1Xu1XfZtg
8M6dXpogHg7ILV/TCvdoGa/+6sW4l2BswPGw9vKcvJgdNmz7N1QCMuSeObzVwNiY
dex/uaNjfYqI3Vg41lefgQKBgCra3YRnlKUMIJbKSde4Lv2TiEyvWmfqBY/QQNkw
VTw/UTtrQ7NCY53DCBOycEpUGE/BLNcbaR33oeItO60FCF4QoWdyej+2rwrwgZkx
KMxhbSpSCqG8bo0kn677xOnNaDwQyqbjIRZp1W3hKqy4nC8Z5gCUq9Fv9mBVr1Or
l6thAoGBAMAuFPW2wfa4MwIv0/HRWVteNta1ePvlq79YdOZtNVYAgaynLYbFJrgO
5Dpkgj8MoxYUdEaUR4Y+XgY+Z+SHbOvt5Sfx1fd5lghYedOqGoMq94sD+pemTjWb
Z3TpeQ54OM69P2vfcc+g/0llZ1bOzMQBw0sFLDrTVVCUgKEwyWSD
-----END RSA PRIVATE KEY-----`

// accessTokenHandler returns an HTTP handler that serves the GitHub App
// installation access token endpoint. tokenVal is the token it issues.
func accessTokenHandler(tokenVal string, expiry time.Time) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		// Verify JWT bearer (just check "Bearer " prefix; we don't validate signature in tests)
		auth := r.Header.Get(ghHeaderAuth)
		if !strings.HasPrefix(auth, "Bearer ") {
			http.Error(w, "missing bearer", http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"token":      tokenVal,
			"expires_at": expiry.UTC().Format(time.RFC3339),
		})
	}
}

// newAppTokenSourceForTest creates an AppTokenSource pointing at the given
// httptest server URL (avoids the live api.github.com).
func newAppTokenSourceForTest(t *testing.T, srv *httptest.Server, pemData []byte) (*AppTokenSource, error) {
	t.Helper()
	return newAppTokenSourceWithBase(AppAuthConfig{
		AppID:          1,
		InstallationID: 2,
		PrivateKeyPEM:  pemData,
	}, srv.URL)
}

// ── parse / constructor ────────────────────────────────────────────────────

func TestNewAppTokenSource_RejectsInvalidPEM(t *testing.T) {
	cases := []struct {
		name string
		pem  []byte
	}{
		{"empty", []byte{}},
		{"garbage", []byte("not a pem block at all")},
		{"wrong type", []byte("-----BEGIN CERTIFICATE-----\naGVsbG8=\n-----END CERTIFICATE-----\n")},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewAppTokenSource(AppAuthConfig{
				AppID:          1,
				InstallationID: 2,
				PrivateKeyPEM:  tc.pem,
			})
			if err == nil {
				t.Fatal("expected error for invalid PEM, got nil")
			}
		})
	}
}

func TestNewAppTokenSource_AcceptsValidPEM(t *testing.T) {
	src, err := NewAppTokenSource(AppAuthConfig{
		AppID:          3613880,
		InstallationID: 129820682,
		PrivateKeyPEM:  []byte(testRSAPEM),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if src == nil {
		t.Fatal("expected non-nil AppTokenSource")
	}
}

// ── JWT signing ────────────────────────────────────────────────────────────

func TestAppTokenSource_GeneratesJWT(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Not called in this test.
		http.Error(w, "unexpected call", http.StatusInternalServerError)
	}))
	defer srv.Close()

	src, err := newAppTokenSourceWithBase(AppAuthConfig{
		AppID:          42,
		InstallationID: 99,
		PrivateKeyPEM:  []byte(testRSAPEM),
	}, srv.URL)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	jwt, err := src.signJWT()
	if err != nil {
		t.Fatalf("signJWT: %v", err)
	}

	// JWT is header.payload.signature — three dot-separated segments.
	parts := strings.Split(jwt, ".")
	if len(parts) != 3 {
		t.Fatalf("JWT has %d segments, want 3: %q", len(parts), jwt)
	}
	for i, p := range parts {
		if p == "" {
			t.Errorf("JWT segment %d is empty", i)
		}
	}
}

// ── token fetch & cache ────────────────────────────────────────────────────

func TestAppTokenSource_FetchesToken(t *testing.T) {
	const wantToken = "ghs_test_token_abc"
	expiry := time.Now().Add(time.Hour)

	mux := http.NewServeMux()
	mux.HandleFunc("/app/installations/2/access_tokens", accessTokenHandler(wantToken, expiry))

	srv := httptest.NewServer(mux)
	defer srv.Close()

	src, err := newAppTokenSourceForTest(t, srv, []byte(testRSAPEM))
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	tok, err := src.Token(context.Background())
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	if tok != wantToken {
		t.Errorf("Token = %q, want %q", tok, wantToken)
	}
}

func TestAppTokenSource_CachesToken(t *testing.T) {
	callCount := 0
	expiry := time.Now().Add(time.Hour)

	mux := http.NewServeMux()
	mux.HandleFunc("/app/installations/2/access_tokens",
		func(w http.ResponseWriter, r *http.Request) {
			callCount++
			accessTokenHandler("tok", expiry)(w, r)
		},
	)

	srv := httptest.NewServer(mux)
	defer srv.Close()

	src, err := newAppTokenSourceForTest(t, srv, []byte(testRSAPEM))
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	for range 5 {
		if _, err := src.Token(context.Background()); err != nil {
			t.Fatalf("Token: %v", err)
		}
	}

	if callCount != 1 {
		t.Errorf("server called %d times, want 1 (token should be cached)", callCount)
	}
}

func TestAppTokenSource_RefreshesNearExpiry(t *testing.T) {
	callCount := 0
	// Issue tokens that expire in 4 minutes — well inside the 5-minute refresh leeway.
	// So every Token() call should trigger a new fetch.
	makeExpiry := func() time.Time { return time.Now().Add(4 * time.Minute) }

	mux := http.NewServeMux()
	mux.HandleFunc("/app/installations/2/access_tokens",
		func(w http.ResponseWriter, r *http.Request) {
			callCount++
			accessTokenHandler(fmt.Sprintf("tok%d", callCount), makeExpiry())(w, r)
		},
	)

	srv := httptest.NewServer(mux)
	defer srv.Close()

	src, err := newAppTokenSourceForTest(t, srv, []byte(testRSAPEM))
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	tok1, err := src.Token(context.Background())
	if err != nil {
		t.Fatalf("first Token: %v", err)
	}
	tok2, err := src.Token(context.Background())
	if err != nil {
		t.Fatalf("second Token: %v", err)
	}

	if tok1 == tok2 {
		t.Error("expected different tokens on second call (near-expiry refresh), got same")
	}
	if callCount != 2 {
		t.Errorf("server called %d times, want 2", callCount)
	}
}

func TestAppTokenSource_ConcurrentSafe(t *testing.T) {
	expiry := time.Now().Add(time.Hour)
	callCount := 0
	var mu sync.Mutex

	mux := http.NewServeMux()
	mux.HandleFunc("/app/installations/2/access_tokens",
		func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			callCount++
			mu.Unlock()
			accessTokenHandler("shared-tok", expiry)(w, r)
		},
	)

	srv := httptest.NewServer(mux)
	defer srv.Close()

	src, err := newAppTokenSourceForTest(t, srv, []byte(testRSAPEM))
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	const goroutines = 20
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for range goroutines {
		go func() {
			defer wg.Done()
			if _, err := src.Token(context.Background()); err != nil {
				t.Errorf("Token: %v", err)
			}
		}()
	}
	wg.Wait()

	mu.Lock()
	got := callCount
	mu.Unlock()

	// Due to the mutex in Token(), only one fetch should reach the server.
	// Allow ≤2 in case goroutines enter simultaneously before cache is warm.
	if got > 2 {
		t.Errorf("server called %d times under concurrency, want ≤2", got)
	}
}

func TestAppTokenSource_ServesStaleOnRefreshError(t *testing.T) {
	// First call succeeds and caches. Subsequent call fails at fetch.
	// Token must return the stale cached value (still valid clock-wise).
	callCount := 0
	// Expiry is just past refreshLeeway boundary — stale but not expired.
	expiry := time.Now().Add(refreshLeeway - time.Second)

	mux := http.NewServeMux()
	mux.HandleFunc("/app/installations/2/access_tokens",
		func(w http.ResponseWriter, r *http.Request) {
			callCount++
			if callCount == 1 {
				accessTokenHandler("stale-token", expiry)(w, r)
				return
			}
			// Simulate server failure on subsequent requests.
			http.Error(w, "server error", http.StatusInternalServerError)
		},
	)

	srv := httptest.NewServer(mux)
	defer srv.Close()

	src, err := newAppTokenSourceForTest(t, srv, []byte(testRSAPEM))
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	// Warm the cache.
	tok1, err := src.Token(context.Background())
	if err != nil {
		t.Fatalf("first Token: %v", err)
	}
	if tok1 != "stale-token" {
		t.Fatalf("first token = %q, want stale-token", tok1)
	}

	// Second call triggers refresh (near expiry) → server fails → stale served.
	tok2, err := src.Token(context.Background())
	if err != nil {
		t.Fatalf("second Token should serve stale, got error: %v", err)
	}
	if tok2 != "stale-token" {
		t.Errorf("second token = %q, want stale-token (fallback to cache)", tok2)
	}
}

// ── fallback to PAT ────────────────────────────────────────────────────────

func TestAppTokenSource_FallsBackToPAT_WhenAppEnvMissing(t *testing.T) {
	// When AppConfig is zero, NewGitHubForge must use PAT transport.
	// Verify by checking that the Authorization header is "token <pat>".
	const pat = "ghp_test_pat_xyz"
	var gotAuth string

	mux := http.NewServeMux()
	mux.HandleFunc("GET /repos/foo/bar", func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get(ghHeaderAuth)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"full_name":      "foo/bar",
			"default_branch": "main",
		})
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	// AppConfig zero-value → PAT fallback.
	g := newGitHubForgeWithBase(pat, AppConfig{}, srv.URL)
	_, err := g.FetchRepoMeta(context.Background(), "foo/bar")
	if err != nil {
		t.Fatalf("FetchRepoMeta: %v", err)
	}

	wantAuth := "token " + pat
	if gotAuth != wantAuth {
		t.Errorf("Authorization = %q, want %q", gotAuth, wantAuth)
	}
}

func TestNewGitHubForge_AppAuthInjectsInstallationToken(t *testing.T) {
	// When valid AppConfig is provided, requests must carry the installation token,
	// NOT the PAT — proving they use a separate rate-limit pool.
	const installationToken = "ghs_installation_xyz"
	const pat = "ghp_should_not_appear"

	expiry := time.Now().Add(time.Hour)

	mux := http.NewServeMux()
	// GitHub App token endpoint.
	mux.HandleFunc("/app/installations/2/access_tokens",
		accessTokenHandler(installationToken, expiry),
	)

	var gotAuth string
	mux.HandleFunc("GET /repos/foo/bar", func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get(ghHeaderAuth)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"full_name":      "foo/bar",
			"default_branch": "main",
		})
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	g := newGitHubForgeWithBase(pat, AppConfig{
		AppID:          1,
		InstallationID: 2,
		KeyPEM:         []byte(testRSAPEM),
	}, srv.URL)

	_, err := g.FetchRepoMeta(context.Background(), "foo/bar")
	if err != nil {
		t.Fatalf("FetchRepoMeta: %v", err)
	}

	wantAuth := "token " + installationToken
	if gotAuth != wantAuth {
		t.Errorf("Authorization = %q, want %q (PAT must NOT appear)", gotAuth, wantAuth)
	}
	if strings.Contains(gotAuth, pat) {
		t.Errorf("PAT leaked into Authorization header: %q", gotAuth)
	}
}

func TestNewGitHubForge_FallsBackToPAT_OnInvalidKey(t *testing.T) {
	// Invalid KeyPEM → App init fails → newGitHubForgeWithBase falls back to PAT.
	const pat = "ghp_fallback_pat"
	var gotAuth string

	mux := http.NewServeMux()
	mux.HandleFunc("GET /repos/foo/bar", func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get(ghHeaderAuth)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"full_name":      "foo/bar",
			"default_branch": "main",
		})
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	// Replace logGitHubAppFallback to suppress test output noise.
	orig := logGitHubAppFallback
	logGitHubAppFallback = func(_ error) {}
	defer func() { logGitHubAppFallback = orig }()

	g := newGitHubForgeWithBase(pat, AppConfig{
		AppID:          1,
		InstallationID: 2,
		KeyPEM:         []byte("garbage pem"),
	}, srv.URL)

	_, err := g.FetchRepoMeta(context.Background(), "foo/bar")
	if err != nil {
		t.Fatalf("FetchRepoMeta: %v", err)
	}

	wantAuth := "token " + pat
	if gotAuth != wantAuth {
		t.Errorf("Authorization = %q, want %q after key-parse failure", gotAuth, wantAuth)
	}
}

// ── endpointLabel ──────────────────────────────────────────────────────────

func TestEndpointLabel(t *testing.T) {
	cases := []struct {
		path string
		want string
	}{
		{"/repos/foo/bar/pulls/1/reviews", "pulls"},
		{"/repos/foo/bar/issues/5/comments", "issues"},
		{"/repos/foo/bar/contents/README.md", "contents"},
		{"/repos/foo/bar/git/trees/main", "git"},
		{"/repos/foo/bar/commits", "commits"},
		{"/repos/foo/bar/readme", "readme"},
		{"/repos/foo/bar/something-new", "other"},
		{"/search/code", "search"},
		{"/search/repositories", "search"},
		{"/app/installations/123/access_tokens", "app"},
		{"/unknown/path", "other"},
		{"/repos/foo/bar", "repos"},
		{"", "other"},
	}

	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			got := endpointLabel(tc.path)
			if got != tc.want {
				t.Errorf("endpointLabel(%q) = %q, want %q", tc.path, got, tc.want)
			}
		})
	}
}

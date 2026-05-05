package forge

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

const (
	// endpointOther is the catch-all label for unlisted GitHub API endpoints.
	endpointOther = "other"

	// endpointPathParts is the max segments to split when parsing API paths.
	endpointPathParts = 5

	// endpointRepoSegmentIdx is the index of the resource segment in /repos/<owner>/<repo>/<segment>/...
	endpointRepoSegmentIdx = 4

	// authModeApp identifies installation-token-authenticated calls.
	authModeApp = "app"
	// authModeAppJWT identifies the App's own /access_tokens POST authenticated by JWT.
	authModeAppJWT = "app_jwt"
	// authModePAT identifies PAT-authenticated calls.
	authModePAT = "pat"
	// authModeNone identifies unauthenticated calls (no PAT, no App).
	authModeNone = "none"

	// statusTransportError is the synthetic status label for non-HTTP failures
	// (DNS, TLS handshake, connection reset) where resp == nil.
	statusTransportError = "transport_error"

	// jwtExpiry is below GitHub's 10-minute hard cap to tolerate clock skew.
	jwtExpiry = 9 * time.Minute
	// jwtIATBackdate compensates for clock drift between us and GitHub.
	jwtIATBackdate = 60 * time.Second

	// retryBackoff is the pause before retrying a transient failure.
	retryBackoff = 250 * time.Millisecond

	// authErrorBodyLimit caps the bytes copied from a 401/403 error body into
	// the *authError message — enough for the GitHub error JSON, no more.
	authErrorBodyLimit = 512
)

// refreshLeeway is how long before token expiry we proactively refresh.
const refreshLeeway = 5 * time.Minute

// AppAuthConfig holds the credentials for GitHub App authentication.
type AppAuthConfig struct {
	// AppID is the numeric GitHub App ID (e.g. 3613880).
	AppID int64
	// InstallationID is the numeric installation ID for the target account.
	InstallationID int64
	// PrivateKeyPEM is the PEM-encoded RSA private key generated in App settings.
	PrivateKeyPEM []byte
}

// authError represents a GitHub auth rejection (401/403). Used to gate retries
// in Token(): transient failures retry once, auth failures don't.
type authError struct {
	status int
	body   string
}

func (e *authError) Error() string {
	return fmt.Sprintf("github app auth %d: %s", e.status, e.body)
}

// AppTokenSource returns installation access tokens, refreshing automatically.
// Concurrency-safe: a singleflight group de-duplicates concurrent refreshes
// so the network call happens at most once even under N goroutines.
type AppTokenSource struct {
	cfg     AppAuthConfig
	key     *rsa.PrivateKey
	apiBase string
	http    *http.Client

	sf singleflight.Group

	mu      sync.RWMutex
	token   string
	expires time.Time
}

// NewAppTokenSource parses the PEM key and returns a ready AppTokenSource.
// Returns an error if the PEM is missing, malformed, or not an RSA key.
func NewAppTokenSource(cfg AppAuthConfig) (*AppTokenSource, error) {
	return newAppTokenSourceWithBase(cfg, ghDefaultAPIBase)
}

// newAppTokenSourceWithBase is the internal constructor used in tests.
func newAppTokenSourceWithBase(cfg AppAuthConfig, apiBase string) (*AppTokenSource, error) {
	key, err := parseRSAPrivateKey(cfg.PrivateKeyPEM)
	if err != nil {
		return nil, fmt.Errorf("github app: parse private key: %w", err)
	}
	return &AppTokenSource{
		cfg:     cfg,
		key:     key,
		apiBase: apiBase,
		http:    &http.Client{Timeout: ghDefaultTimeout},
	}, nil
}

// Token returns a valid installation access token, refreshing when within
// refreshLeeway of expiry. Falls back to the cached token on network errors
// if the cache is still valid.
//
// The hot path takes only a read lock. Concurrent refreshes are coalesced
// via singleflight so the HTTP roundtrip happens at most once.
func (a *AppTokenSource) Token(ctx context.Context) (string, error) {
	if tok, ok := a.cachedToken(); ok {
		return tok, nil
	}

	v, err, _ := a.sf.Do("refresh", func() (any, error) {
		// Double-check after entering singleflight: another caller may have
		// just refreshed the token while we were waiting.
		if tok, ok := a.cachedToken(); ok {
			return tok, nil
		}

		tok, exp, err := a.fetchInstallationToken(ctx)
		if err != nil {
			// Serve stale token if still technically valid (clock skew tolerance).
			a.mu.RLock()
			cached, expires := a.token, a.expires
			a.mu.RUnlock()
			if cached != "" && time.Now().Before(expires) {
				slog.Warn("github app: token refresh failed, serving cached token", //nolint:gosec // G706: error from internal HTTP call, not user input
					slog.Any("error", err),
					slog.Time("expires", expires),
				)
				return cached, nil
			}
			return "", err
		}

		a.mu.Lock()
		a.token = tok
		a.expires = exp
		a.mu.Unlock()
		return tok, nil
	})
	if err != nil {
		return "", err
	}
	return v.(string), nil
}

// cachedToken returns the cached token under a read lock when it's still
// outside the refresh leeway window.
func (a *AppTokenSource) cachedToken() (string, bool) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.token != "" && time.Now().Add(refreshLeeway).Before(a.expires) {
		return a.token, true
	}
	return "", false
}

// fetchInstallationToken signs a JWT, then POSTs to the installation access
// tokens endpoint. Retries once on transient errors (network, 5xx); does not
// retry on auth errors (401/403).
func (a *AppTokenSource) fetchInstallationToken(ctx context.Context) (string, time.Time, error) {
	jwt, err := a.signJWT()
	if err != nil {
		return "", time.Time{}, fmt.Errorf("sign jwt: %w", err)
	}

	url := fmt.Sprintf("%s/app/installations/%d/access_tokens", a.apiBase, a.cfg.InstallationID)

	do := func() (string, time.Time, error) {
		return a.postAccessTokens(ctx, url, jwt)
	}

	tok, exp, err := do()
	if err == nil {
		return tok, exp, nil
	}
	// Don't retry on auth errors.
	var authErr *authError
	if errors.As(err, &authErr) {
		return "", time.Time{}, err
	}
	// Transient — backoff and retry once.
	select {
	case <-ctx.Done():
		return "", time.Time{}, ctx.Err()
	case <-time.After(retryBackoff):
	}
	return do()
}

// postAccessTokens performs a single POST to the App installation access-tokens
// endpoint and emits the auth-mode metric. It returns *authError for 401/403
// so callers can skip retry on credential failures.
func (a *AppTokenSource) postAccessTokens(ctx context.Context, url, jwt string) (string, time.Time, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set(ghHeaderAccept, ghMediaTypeJSON)
	req.Header.Set(ghHeaderAPIVersion, ghAPIVersion)
	req.Header.Set(ghHeaderAuth, "Bearer "+jwt)

	resp, err := a.http.Do(req) //nolint:gosec // URL constructed from validated config
	if err != nil {
		githubAPICallsTotal.WithLabelValues(endpointLabel(req.URL.Path), statusTransportError, authModeAppJWT).Inc()
		return "", time.Time{}, fmt.Errorf("post access_tokens: %w", err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()
	githubAPICallsTotal.WithLabelValues(endpointLabel(req.URL.Path), strconv.Itoa(resp.StatusCode), authModeAppJWT).Inc()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, authErrorBodyLimit))
		return "", time.Time{}, &authError{status: resp.StatusCode, body: string(body)}
	}
	if resp.StatusCode != http.StatusCreated {
		return "", time.Time{}, fmt.Errorf("access_tokens returned %d (not 201)", resp.StatusCode)
	}

	var body struct {
		Token     string `json:"token"`
		ExpiresAt string `json:"expires_at"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", time.Time{}, fmt.Errorf("decode access_tokens response: %w", err)
	}
	if body.Token == "" {
		return "", time.Time{}, errors.New("access_tokens response missing token field")
	}

	exp, perr := time.Parse(time.RFC3339, body.ExpiresAt)
	if perr != nil {
		// Use 1h default if expiry can't be parsed — but log loudly so we don't
		// silently mask a GitHub API change.
		slog.Warn("github app: malformed expires_at; assuming 1h",
			slog.String("value", body.ExpiresAt),
			slog.Any("error", perr),
		)
		exp = time.Now().Add(time.Hour)
	}

	return body.Token, exp, nil
}

// signJWT builds and RS256-signs a JWT for GitHub App authentication.
// Claims: iss=AppID, iat=now-60s (clock skew), exp=now+9min (below GitHub's
// 10-minute hard cap to tolerate clock skew between us and api.github.com).
func (a *AppTokenSource) signJWT() (string, error) {
	now := time.Now()
	header := base64.RawURLEncoding.EncodeToString(mustJSON(map[string]string{
		"alg": "RS256",
		"typ": "JWT",
	}))
	payload := base64.RawURLEncoding.EncodeToString(mustJSON(map[string]int64{
		"iat": now.Add(-jwtIATBackdate).Unix(),
		"exp": now.Add(jwtExpiry).Unix(),
		"iss": a.cfg.AppID,
	}))

	signingInput := header + "." + payload
	hash := sha256.Sum256([]byte(signingInput))
	sig, err := rsa.SignPKCS1v15(rand.Reader, a.key, crypto.SHA256, hash[:])
	if err != nil {
		return "", fmt.Errorf("rsa sign: %w", err)
	}

	return signingInput + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

// parseRSAPrivateKey decodes a PEM block and parses the RSA private key.
func parseRSAPrivateKey(pemBytes []byte) (*rsa.PrivateKey, error) {
	if len(pemBytes) == 0 {
		return nil, errors.New("empty PEM data")
	}
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, errors.New("no PEM block found")
	}
	switch block.Type {
	case "RSA PRIVATE KEY":
		key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("parse PKCS1: %w", err)
		}
		return key, nil
	case "PRIVATE KEY":
		iface, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("parse PKCS8: %w", err)
		}
		key, ok := iface.(*rsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("PKCS8 key is %T, not RSA", iface)
		}
		return key, nil
	default:
		return nil, fmt.Errorf("unsupported PEM block type %q", block.Type)
	}
}

// mustJSON marshals v or panics — used only for hard-coded JWT header/payload.
func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic("mustJSON: " + err.Error())
	}
	return b
}

// recordTransportResult emits a counter sample for one round-trip outcome.
// resp may be nil; err is the transport-level error (nil on success).
func recordTransportResult(path, mode string, resp *http.Response, err error) {
	endpoint := endpointLabel(path)
	if err != nil && resp == nil {
		githubAPICallsTotal.WithLabelValues(endpoint, statusTransportError, mode).Inc()
		return
	}
	if resp != nil {
		githubAPICallsTotal.WithLabelValues(endpoint, strconv.Itoa(resp.StatusCode), mode).Inc()
	}
}

// appRoundTripper is an http.RoundTripper that injects App installation tokens.
type appRoundTripper struct {
	src  *AppTokenSource
	next http.RoundTripper
}

func (rt *appRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	tok, err := rt.src.Token(req.Context())
	if err != nil {
		return nil, fmt.Errorf("github app token: %w", err)
	}
	// Clone the request so we don't mutate the caller's headers.
	r2 := req.Clone(req.Context())
	// "Bearer" is the modern scheme for installation tokens; "token" is
	// deprecated for App auth.
	r2.Header.Set(ghHeaderAuth, "Bearer "+tok)
	resp, err := rt.next.RoundTrip(r2)
	recordTransportResult(req.URL.Path, authModeApp, resp, err)
	return resp, err
}

// patRoundTripper is an http.RoundTripper that injects a static PAT.
type patRoundTripper struct {
	token string
	next  http.RoundTripper
}

func (rt *patRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	r2 := req.Clone(req.Context())
	mode := authModeNone
	if rt.token != "" {
		// Classic PATs use "token" prefix; "Bearer" is reserved for App tokens.
		r2.Header.Set(ghHeaderAuth, "token "+rt.token)
		mode = authModePAT
	}
	resp, err := rt.next.RoundTrip(r2)
	recordTransportResult(req.URL.Path, mode, resp, err)
	return resp, err
}

// endpointLabel extracts a bounded label from a GitHub API path.
// Maps the first meaningful path segment after /repos/<owner>/<repo>/ to a
// known label, or "other" for anything not in the bounded set.
//
// Examples:
//
//	/repos/foo/bar/pulls/1/reviews → "pulls"
//	/search/code                   → "search"
//	/app/installations/123/access_tokens → "app"
func endpointLabel(path string) string {
	// Trim leading slash for consistent splitting.
	path = strings.TrimPrefix(path, "/")
	parts := strings.SplitN(path, "/", endpointPathParts)
	if len(parts) == 0 {
		return endpointOther
	}
	switch parts[0] {
	case "repos":
		// /repos/<owner>/<repo>/<segment>/...
		if len(parts) >= endpointRepoSegmentIdx {
			return boundedSegment(parts[endpointRepoSegmentIdx-1])
		}
		return "repos"
	case "search":
		return "search"
	case "app":
		return "app"
	default:
		return endpointOther
	}
}

// boundedSegment returns the label if it's in the known set, else endpointOther.
func boundedSegment(seg string) string {
	switch seg {
	case "pulls", "issues", "contents", "git", "commits",
		"branches", "releases", "statuses", "check-runs",
		"readme", "languages", "topics", "contributors":
		return seg
	default:
		return endpointOther
	}
}

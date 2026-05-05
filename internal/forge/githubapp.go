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
)

const (
	// endpointOther is the catch-all label for unlisted GitHub API endpoints.
	endpointOther = "other"

	// endpointPathParts is the max segments to split when parsing API paths.
	endpointPathParts = 5

	// endpointRepoSegmentIdx is the index of the resource segment in /repos/<owner>/<repo>/<segment>/...
	endpointRepoSegmentIdx = 4
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

// AppTokenSource returns installation access tokens, refreshing automatically.
// Concurrency-safe.
type AppTokenSource struct {
	cfg     AppAuthConfig
	key     *rsa.PrivateKey
	apiBase string
	http    *http.Client

	mu      sync.Mutex
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
func (a *AppTokenSource) Token(ctx context.Context) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.token != "" && time.Now().Add(refreshLeeway).Before(a.expires) {
		return a.token, nil
	}

	tok, exp, err := a.fetchInstallationToken(ctx)
	if err != nil {
		// Serve stale token if still technically valid (clock skew tolerance).
		if a.token != "" && time.Now().Before(a.expires) {
			slog.Warn("github app: token refresh failed, serving cached token", //nolint:gosec // G706: error from internal HTTP call, not user input
				slog.Any("error", err),
				slog.Time("expires", a.expires),
			)
			return a.token, nil
		}
		return "", err
	}

	a.token = tok
	a.expires = exp
	return tok, nil
}

// fetchInstallationToken signs a JWT, then POSTs to the installation access
// tokens endpoint. Retries once on 5xx / network errors; no retry on 401/403.
func (a *AppTokenSource) fetchInstallationToken(ctx context.Context) (string, time.Time, error) {
	jwt, err := a.signJWT()
	if err != nil {
		return "", time.Time{}, fmt.Errorf("sign jwt: %w", err)
	}

	url := fmt.Sprintf("%s/app/installations/%d/access_tokens", a.apiBase, a.cfg.InstallationID)

	do := func() (string, time.Time, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
		if err != nil {
			return "", time.Time{}, fmt.Errorf("build request: %w", err)
		}
		req.Header.Set(ghHeaderAccept, ghMediaTypeJSON)
		req.Header.Set(ghHeaderAPIVersion, ghAPIVersion)
		req.Header.Set(ghHeaderAuth, "Bearer "+jwt)

		resp, err := a.http.Do(req) //nolint:gosec // URL constructed from validated config
		if err != nil {
			return "", time.Time{}, fmt.Errorf("post access_tokens: %w", err)
		}
		defer func() {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
		}()

		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			return "", time.Time{}, fmt.Errorf("github app auth rejected (status %d): check App ID, Installation ID, and key", resp.StatusCode)
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

		exp, err := time.Parse(time.RFC3339, body.ExpiresAt)
		if err != nil {
			// Use 1h default if expiry can't be parsed.
			exp = time.Now().Add(time.Hour)
		}

		return body.Token, exp, nil
	}

	tok, exp, err := do()
	if err != nil {
		// One retry on transient errors (network, 5xx). Not on auth errors (already
		// returned above).
		tok, exp, err = do()
	}
	return tok, exp, err
}

// signJWT builds and RS256-signs a JWT for GitHub App authentication.
// Claims: iss=AppID, iat=now-60s (clock skew), exp=now+10min (GitHub max).
func (a *AppTokenSource) signJWT() (string, error) {
	now := time.Now()
	header := base64.RawURLEncoding.EncodeToString(mustJSON(map[string]string{
		"alg": "RS256",
		"typ": "JWT",
	}))
	payload := base64.RawURLEncoding.EncodeToString(mustJSON(map[string]int64{
		"iat": now.Add(-60 * time.Second).Unix(),
		"exp": now.Add(10 * time.Minute).Unix(),
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
	r2.Header.Set(ghHeaderAuth, "token "+tok)
	resp, err := rt.next.RoundTrip(r2)
	if resp != nil {
		githubAPICallsTotal.WithLabelValues(
			endpointLabel(req.URL.Path),
			strconv.Itoa(resp.StatusCode),
		).Inc()
	}
	return resp, err
}

// patRoundTripper is an http.RoundTripper that injects a static PAT.
type patRoundTripper struct {
	token string
	next  http.RoundTripper
}

func (rt *patRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	r2 := req.Clone(req.Context())
	if rt.token != "" {
		r2.Header.Set(ghHeaderAuth, "token "+rt.token)
	}
	resp, err := rt.next.RoundTrip(r2)
	if resp != nil {
		githubAPICallsTotal.WithLabelValues(
			endpointLabel(req.URL.Path),
			strconv.Itoa(resp.StatusCode),
		).Inc()
	}
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

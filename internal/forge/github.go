package forge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	kitcache "github.com/anatolykoptev/go-kit/cache"
)

// AppConfig carries optional GitHub App credentials. When all three fields are
// non-zero, App authentication is used in place of a static PAT.
type AppConfig struct {
	AppID          int64
	InstallationID int64
	// KeyPEM is the PEM-encoded RSA private key. Empty means App auth disabled.
	KeyPEM []byte
}

// IsConfigured reports whether all three App credentials are present.
// When false, callers should fall back to a static PAT.
func (a AppConfig) IsConfigured() bool {
	return a.AppID != 0 && a.InstallationID != 0 && len(a.KeyPEM) > 0
}

const (
	ghDefaultAPIBase = "https://api.github.com"
	ghDefaultTimeout = 15 * time.Second

	ghHeaderAccept     = "Accept"
	ghHeaderAuth       = "Authorization"
	ghHeaderAPIVersion = "X-GitHub-Api-Version"
	ghAPIVersion       = "2022-11-28"
	ghMediaTypeJSON    = "application/vnd.github+json"
	ghMediaTypeRaw     = "application/vnd.github.raw+json"
	ghMediaTypeText    = "application/vnd.github.text-match+json"

	ghSearchPerPageCode  = 100
	ghSearchPerPageRepos = 30
	ghSearchPerPageIssue = 100
)

// GitHubForge implements Forge for github.com.
type GitHubForge struct {
	token   string
	apiBase string
	http    *http.Client
	cache   *kitcache.Cache
}

// GitHubForgeOption configures a GitHubForge.
type GitHubForgeOption func(*GitHubForge)

// WithCache sets the cache used by GitHubForge.
func WithCache(c *kitcache.Cache) GitHubForgeOption {
	return func(g *GitHubForge) {
		g.cache = c
	}
}

// NewGitHubForge creates a GitHubForge targeting api.github.com.
//
// Authentication priority (first match wins):
//  1. App auth — when app.AppID, app.InstallationID, and app.KeyPEM are all
//     non-zero. Uses installation access tokens (separate 5000/h pool from the
//     user's gh CLI PAT). Falls back to PAT on key-parse error (logged as warning).
//  2. PAT — when token is non-empty.
//  3. Unauthenticated — 60 req/h rate limit.
func NewGitHubForge(token string, app AppConfig, opts ...GitHubForgeOption) *GitHubForge {
	return newGitHubForgeWithBase(token, app, ghDefaultAPIBase, opts...)
}

// newGitHubForgeWithBase creates a GitHubForge with an explicit API base URL.
// Used in tests to point at an httptest server.
func newGitHubForgeWithBase(token string, app AppConfig, base string, opts ...GitHubForgeOption) *GitHubForge {
	if base == "" {
		base = ghDefaultAPIBase
	}

	// Try App auth first when all three credentials are present.
	var transport http.RoundTripper
	if app.AppID != 0 && app.InstallationID != 0 && len(app.KeyPEM) > 0 {
		src, err := newAppTokenSourceWithBase(AppAuthConfig{
			AppID:          app.AppID,
			InstallationID: app.InstallationID,
			PrivateKeyPEM:  app.KeyPEM,
		}, base)
		if err == nil {
			transport = &appRoundTripper{src: src, next: http.DefaultTransport}
		} else {
			// Key parse failed — fall through to PAT, warn loudly.
			// This matches the spec: "log warning, fall back to PAT, don't crash".
			logGitHubAppFallback(err)
			transport = &patRoundTripper{token: token, next: http.DefaultTransport}
		}
	} else {
		transport = &patRoundTripper{token: token, next: http.DefaultTransport}
	}

	g := &GitHubForge{
		token:   token,
		apiBase: base,
		http:    &http.Client{Timeout: ghDefaultTimeout, Transport: transport},
	}
	for _, opt := range opts {
		opt(g)
	}
	return g
}

// logGitHubAppFallback emits a warning when App auth is requested but fails
// to initialise. Uses slog so MCP servers running over stdio JSON-RPC don't
// have stdout polluted with log lines. Defined as a var so tests can capture it.
var logGitHubAppFallback = func(err error) {
	slog.Warn("github app init failed; falling back to PAT", slog.Any("error", err))
}

// Kind implements Forge.
func (g *GitHubForge) Kind() ForgeKind { return GitHub }

// FetchRepoMeta implements Forge.
func (g *GitHubForge) FetchRepoMeta(ctx context.Context, slug string) (_ *RepoMeta, err error) {
	slug = strings.TrimPrefix(slug, "https://github.com/")
	slug = strings.TrimSuffix(slug, ".git")

	apiURL := fmt.Sprintf("%s/repos/%s", g.apiBase, slug)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}

	g.setHeaders(req)

	resp, err := g.http.Do(req) //nolint:gosec // URL constructed from validated slug
	if err != nil {
		return nil, fmt.Errorf("fetch repo meta: %w", err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		err = errors.Join(err, resp.Body.Close())
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github api returned %d for %s", resp.StatusCode, slug)
	}

	var meta RepoMeta
	if err := json.NewDecoder(resp.Body).Decode(&meta); err != nil {
		return nil, fmt.Errorf("decode repo meta: %w", err)
	}

	return &meta, nil
}

// FetchREADME implements Forge.
// Returns empty string (not an error) when no README exists.
func (g *GitHubForge) FetchREADME(ctx context.Context, slug string) (_ string, err error) {
	slug = strings.TrimPrefix(slug, "https://github.com/")
	slug = strings.TrimSuffix(slug, ".git")

	apiURL := fmt.Sprintf("%s/repos/%s/readme", g.apiBase, slug)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}

	req.Header.Set(ghHeaderAccept, ghMediaTypeRaw)
	req.Header.Set(ghHeaderAPIVersion, ghAPIVersion)
	// Authorization is injected by the transport (appRoundTripper / patRoundTripper).

	resp, err := g.http.Do(req) //nolint:gosec // URL constructed from validated slug
	if err != nil {
		return "", fmt.Errorf("fetch readme: %w", err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		err = errors.Join(err, resp.Body.Close())
	}()

	if resp.StatusCode == http.StatusNotFound {
		return "", nil
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("github api returned %d fetching readme for %s", resp.StatusCode, slug)
	}

	var sb strings.Builder
	buf := make([]byte, 32*1024) //nolint:mnd // 32 KB read buffer
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			sb.Write(buf[:n])
		}
		if readErr != nil {
			break
		}
	}

	return sb.String(), nil
}

// setHeaders sets common Accept/Version headers for GitHub API requests.
// Authorization is injected by the transport (appRoundTripper / patRoundTripper).
func (g *GitHubForge) setHeaders(req *http.Request) {
	req.Header.Set(ghHeaderAccept, ghMediaTypeJSON)
	req.Header.Set(ghHeaderAPIVersion, ghAPIVersion)
}

// Post* write methods live in poster_github.go.

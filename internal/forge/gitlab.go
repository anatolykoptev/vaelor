package forge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	glDefaultAPIBase    = "https://gitlab.com"
	glDefaultTimeout    = 15 * time.Second
	glDefaultBranch     = "main"

	glHeaderPrivateToken = "PRIVATE-TOKEN"
	glSearchPerPage      = 30
)

// GitLabForge implements Forge for gitlab.com and self-hosted GitLab instances.
type GitLabForge struct {
	token   string
	apiBase string
	http    *http.Client
}

// NewGitLabForge creates a GitLabForge targeting gitlab.com.
// token may be empty for unauthenticated requests (lower rate limits).
// If apiBase is empty, defaults to "https://gitlab.com".
func NewGitLabForge(token, apiBase string) *GitLabForge {
	return newGitLabForgeWithBase(token, apiBase)
}

// newGitLabForgeWithBase creates a GitLabForge with an explicit API base URL.
// Used in tests to point at an httptest server.
func newGitLabForgeWithBase(token, base string) *GitLabForge {
	if base == "" {
		base = glDefaultAPIBase
	}
	return &GitLabForge{
		token:   token,
		apiBase: base,
		http:    &http.Client{Timeout: glDefaultTimeout},
	}
}

// Kind implements Forge.
func (g *GitLabForge) Kind() ForgeKind { return GitLab }

// setHeaders sets common auth headers for GitLab API requests.
func (g *GitLabForge) setHeaders(req *http.Request) {
	req.Header.Set("Accept", "application/json")
	if g.token != "" {
		req.Header.Set(glHeaderPrivateToken, g.token)
	}
}

// encodeSlug URL-encodes a "group/sub/repo" path for use in GitLab API paths.
// GitLab requires slashes to be encoded as %2F in project identifiers.
func encodeSlug(slug string) string {
	// url.PathEscape encodes "/" as "%2F" which is what GitLab expects.
	// We build it manually to avoid importing net/url only for this.
	var sb strings.Builder
	for i, ch := range slug {
		if ch == '/' {
			if i > 0 {
				sb.WriteString("%2F")
			} else {
				sb.WriteRune(ch)
			}
		} else {
			sb.WriteRune(ch)
		}
	}
	return sb.String()
}

// glProjectResponse is the subset of GitLab's project API response we use.
type glProjectResponse struct {
	PathWithNamespace string `json:"path_with_namespace"`
	Description       string `json:"description"`
	DefaultBranch     string `json:"default_branch"`
	StarCount         int    `json:"star_count"`
	ForksCount        int    `json:"forks_count"`
	HTTPURLToRepo     string `json:"http_url_to_repo"`
	Visibility        string `json:"visibility"`
}

// FetchRepoMeta implements Forge.
func (g *GitLabForge) FetchRepoMeta(ctx context.Context, slug string) (_ *RepoMeta, err error) {
	slug = cleanGitLabSlug(g.apiBase, slug)

	apiURL := fmt.Sprintf("%s/api/v4/projects/%s", g.apiBase, encodeSlug(slug))

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
		return nil, fmt.Errorf("gitlab api returned %d for %s", resp.StatusCode, slug)
	}

	var raw glProjectResponse
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("decode repo meta: %w", err)
	}

	return &RepoMeta{
		FullName:      raw.PathWithNamespace,
		Description:   raw.Description,
		DefaultBranch: raw.DefaultBranch,
		Stars:         raw.StarCount,
		Forks:         raw.ForksCount,
		CloneURL:      raw.HTTPURLToRepo,
		Private:       raw.Visibility != "public",
	}, nil
}

// FetchREADME implements Forge.
// Returns empty string (not an error) when no README exists.
func (g *GitLabForge) FetchREADME(ctx context.Context, slug string) (_ string, err error) {
	slug = cleanGitLabSlug(g.apiBase, slug)

	// Step 1: get the default branch via project metadata.
	meta, err := g.FetchRepoMeta(ctx, slug)
	if err != nil {
		return "", fmt.Errorf("fetch default branch for readme: %w", err)
	}

	branch := meta.DefaultBranch
	if branch == "" {
		branch = glDefaultBranch
	}

	// Step 2: fetch raw README content.
	apiURL := fmt.Sprintf("%s/api/v4/projects/%s/repository/files/README.md/raw?ref=%s",
		g.apiBase, encodeSlug(slug), branch)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return "", fmt.Errorf("build readme request: %w", err)
	}

	g.setHeaders(req)

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
		return "", fmt.Errorf("gitlab api returned %d fetching readme for %s", resp.StatusCode, slug)
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

// cleanGitLabSlug strips common GitLab URL prefixes and .git suffix from a slug.
func cleanGitLabSlug(apiBase, slug string) string {
	// Strip the instance base URL (handles self-hosted too).
	instanceURL := strings.TrimSuffix(apiBase, "/")
	slug = strings.TrimPrefix(slug, instanceURL+"/")
	slug = strings.TrimPrefix(slug, "https://gitlab.com/")
	slug = strings.TrimSuffix(slug, ".git")
	return slug
}

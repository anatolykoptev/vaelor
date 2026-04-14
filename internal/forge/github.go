package forge

import (
	"bytes"
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
	ghDefaultAPIBase  = "https://api.github.com"
	ghDefaultTimeout  = 15 * time.Second

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
}

// NewGitHubForge creates a GitHubForge targeting api.github.com.
// token may be empty for unauthenticated requests (lower rate limits).
func NewGitHubForge(token string) *GitHubForge {
	return newGitHubForgeWithBase(token, ghDefaultAPIBase)
}

// newGitHubForgeWithBase creates a GitHubForge with an explicit API base URL.
// Used in tests to point at an httptest server.
func newGitHubForgeWithBase(token, base string) *GitHubForge {
	if base == "" {
		base = ghDefaultAPIBase
	}
	return &GitHubForge{
		token:   token,
		apiBase: base,
		http:    &http.Client{Timeout: ghDefaultTimeout},
	}
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
	if g.token != "" {
		req.Header.Set(ghHeaderAuth, "Bearer "+g.token)
	}

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

// setHeaders sets common Accept/Auth/Version headers for GitHub API requests.
func (g *GitHubForge) setHeaders(req *http.Request) {
	req.Header.Set(ghHeaderAccept, ghMediaTypeJSON)
	req.Header.Set(ghHeaderAPIVersion, ghAPIVersion)
	if g.token != "" {
		req.Header.Set(ghHeaderAuth, "Bearer "+g.token)
	}
}

func (g *GitHubForge) PostReview(ctx context.Context, slug string, pr int, p ReviewPayload) (string, error) {
	type ghInline struct {
		Path string `json:"path"`
		Line int    `json:"line"`
		Body string `json:"body"`
		Side string `json:"side"`
	}
	type ghBody struct {
		Body     string     `json:"body"`
		Event    string     `json:"event"`
		Comments []ghInline `json:"comments,omitempty"`
	}
	body := ghBody{Body: p.Body, Event: p.Event}
	for _, c := range p.Comments {
		body.Comments = append(body.Comments, ghInline{Path: c.Path, Line: c.Line, Body: c.Body, Side: "RIGHT"})
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("marshal review: %w", err)
	}
	url := fmt.Sprintf("%s/repos/%s/pulls/%d/reviews", g.apiBase, slug, pr)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	g.setHeaders(req)
	req.Header.Set("Content-Type", "application/json")
	resp, err := g.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("post review: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("post review: %s: %s", resp.Status, string(b))
	}
	var out struct {
		HTMLURL string `json:"html_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("decode: %w", err)
	}
	return out.HTMLURL, nil
}

func (g *GitHubForge) PostIssueComment(ctx context.Context, slug string, number int, body string) (string, error) {
	raw, _ := json.Marshal(map[string]string{"body": body})
	url := fmt.Sprintf("%s/repos/%s/issues/%d/comments", g.apiBase, slug, number)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	g.setHeaders(req)
	req.Header.Set("Content-Type", "application/json")
	resp, err := g.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("post comment: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("post comment: %s: %s", resp.Status, string(b))
	}
	var out struct {
		HTMLURL string `json:"html_url"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return out.HTMLURL, nil
}

// Package github provides a minimal GitHub API client for go-code.
//
// It handles repository metadata fetching, README retrieval, and
// rate-limit-aware HTTP requests with token authentication.
package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const (
	defaultAPIBase  = "https://api.github.com"
	defaultTimeout  = 15 * time.Second

	headerAccept        = "Accept"
	headerAuth          = "Authorization"
	headerAPIVersion    = "X-GitHub-Api-Version"
	apiVersion          = "2022-11-28"
	mediaTypeGitHubJSON = "application/vnd.github+json"
)

// Client is a GitHub API client.
type Client struct {
	http    *http.Client
	token   string
	apiBase string
}

// RepoMeta contains key metadata about a GitHub repository.
type RepoMeta struct {
	// FullName is the "owner/repo" slug.
	FullName string `json:"full_name"`

	// Description is the repository description.
	Description string `json:"description"`

	// DefaultBranch is the default branch name.
	DefaultBranch string `json:"default_branch"`

	// Language is the primary programming language.
	Language string `json:"language"`

	// Stars is the stargazer count.
	Stars int `json:"stargazers_count"`

	// Forks is the fork count.
	Forks int `json:"forks_count"`

	// CloneURL is the HTTPS clone URL.
	CloneURL string `json:"clone_url"`

	// Private indicates whether the repo is private.
	Private bool `json:"private"`

	// Size is the approximate disk size in kilobytes.
	Size int `json:"size"`
}

// NewClient creates a new GitHub API client.
// token may be empty for unauthenticated requests (lower rate limits).
func NewClient(token string) *Client {
	return &Client{
		token:   token,
		apiBase: defaultAPIBase,
		http: &http.Client{
			Timeout: defaultTimeout,
		},
	}
}

// FetchRepoMeta fetches repository metadata from the GitHub API.
func (c *Client) FetchRepoMeta(ctx context.Context, slug string) (_ *RepoMeta, err error) {
	slug = strings.TrimPrefix(slug, "https://github.com/")
	slug = strings.TrimSuffix(slug, ".git")

	url := fmt.Sprintf("%s/repos/%s", c.apiBase, slug)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}

	c.setHeaders(req)

	resp, err := c.http.Do(req) //nolint:gosec // URL is constructed from a validated slug
	if err != nil {
		return nil, fmt.Errorf("fetch repo meta: %w", err)
	}
	defer func() { err = errors.Join(err, resp.Body.Close()) }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github api returned %d for %s", resp.StatusCode, slug)
	}

	var meta RepoMeta
	if err := json.NewDecoder(resp.Body).Decode(&meta); err != nil {
		return nil, fmt.Errorf("decode repo meta: %w", err)
	}

	return &meta, nil
}

// FetchREADME fetches the raw README content for a repository.
// Returns empty string if no README is found.
func (c *Client) FetchREADME(ctx context.Context, slug string) (_ string, err error) {
	slug = strings.TrimPrefix(slug, "https://github.com/")
	slug = strings.TrimSuffix(slug, ".git")

	url := fmt.Sprintf("%s/repos/%s/readme", c.apiBase, slug)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}

	// Request raw content directly.
	req.Header.Set(headerAccept, "application/vnd.github.raw+json")
	if c.token != "" {
		req.Header.Set(headerAuth, "Bearer "+c.token)
	}
	req.Header.Set(headerAPIVersion, apiVersion)

	resp, err := c.http.Do(req) //nolint:gosec // URL is constructed from a validated slug
	if err != nil {
		return "", fmt.Errorf("fetch readme: %w", err)
	}
	defer func() { err = errors.Join(err, resp.Body.Close()) }()

	if resp.StatusCode == http.StatusNotFound {
		return "", nil
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("github api returned %d fetching readme for %s", resp.StatusCode, slug)
	}

	var sb strings.Builder
	buf := make([]byte, 32*1024) //nolint:mnd // 32KB read buffer, not a magic number in context
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

// setHeaders sets common headers for GitHub API requests.
func (c *Client) setHeaders(req *http.Request) {
	req.Header.Set(headerAccept, mediaTypeGitHubJSON)
	req.Header.Set(headerAPIVersion, apiVersion)
	if c.token != "" {
		req.Header.Set(headerAuth, "Bearer "+c.token)
	}
}

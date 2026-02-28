package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

const (
	searchPerPageCode  = 100
	searchPerPageRepos = 30
	searchPerPageIssue = 100
	githubHost         = "github.com"
	mediaTypeTextMatch = "application/vnd.github.text-match+json"
)

// CodeResult represents a GitHub Code Search result.
type CodeResult struct {
	Name    string
	Path    string
	URL     string
	Repo    string
	Content string // joined text-match fragments
}

// IssueItem represents a GitHub issue or PR from search.
type IssueItem struct {
	Number    int
	Title     string
	URL       string
	State     string
	Author    string
	Labels    []string
	Body      string
	Comments  int
	CreatedAt string
	MergedAt  string
	Repo      string
}

// RepoSearchResult represents a repo from GitHub Search API.
type RepoSearchResult struct {
	FullName    string
	Description string
	Stars       int
	Language    string
	Topics      []string
	LastPush    string
	Archived    bool
	HTMLURL     string
}

// ExtractOwnerRepo parses a GitHub URL into owner and repo parts.
// Returns ok=false if the URL is not a valid github.com repo URL.
func ExtractOwnerRepo(rawURL string) (owner, repo string, ok bool) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host != githubHost {
		return "", "", false
	}

	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}

	repoName := strings.TrimSuffix(parts[1], ".git")
	return parts[0], repoName, true
}

// SearchCode searches GitHub's code search API.
// When repos is non-empty, the search is restricted to those repositories.
func (c *Client) SearchCode(ctx context.Context, query string, repos []string) ([]CodeResult, error) {
	var sb strings.Builder
	sb.WriteString(query)
	for _, r := range repos {
		sb.WriteString(" repo:")
		sb.WriteString(r)
	}
	q := sb.String()

	apiURL := fmt.Sprintf("%s/search/code?q=%s&per_page=%d",
		c.apiBase, url.QueryEscape(q), searchPerPageCode)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build search code request: %w", err)
	}

	req.Header.Set(headerAccept, mediaTypeTextMatch)
	req.Header.Set(headerAPIVersion, apiVersion)
	if c.token != "" {
		req.Header.Set(headerAuth, "Bearer "+c.token)
	}

	resp, err := c.http.Do(req) //nolint:gosec // URL built from validated inputs
	if err != nil {
		return nil, fmt.Errorf("search code: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github search/code returned %d", resp.StatusCode)
	}

	var raw struct {
		Items []struct {
			Name     string `json:"name"`
			Path     string `json:"path"`
			HTMLURL  string `json:"html_url"`
			Repo     struct{ FullName string `json:"full_name"` } `json:"repository"`
			TextMatches []struct{ Fragment string `json:"fragment"` } `json:"text_matches"`
		} `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("decode search code response: %w", err)
	}

	results := make([]CodeResult, 0, len(raw.Items))
	for _, item := range raw.Items {
		var frags []string
		for _, tm := range item.TextMatches {
			if tm.Fragment != "" {
				frags = append(frags, tm.Fragment)
			}
		}
		results = append(results, CodeResult{
			Name:    item.Name,
			Path:    item.Path,
			URL:     item.HTMLURL,
			Repo:    item.Repo.FullName,
			Content: strings.Join(frags, "\n---\n"),
		})
	}
	return results, nil
}

// SearchIssues searches GitHub issues and pull requests.
func (c *Client) SearchIssues(ctx context.Context, query string) ([]IssueItem, error) {
	apiURL := fmt.Sprintf("%s/search/issues?q=%s&per_page=%d&sort=updated&order=desc",
		c.apiBase, url.QueryEscape(query), searchPerPageIssue)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build search issues request: %w", err)
	}

	c.setHeaders(req)

	resp, err := c.http.Do(req) //nolint:gosec // URL built from validated inputs
	if err != nil {
		return nil, fmt.Errorf("search issues: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github search/issues returned %d", resp.StatusCode)
	}

	var raw struct {
		Items []struct {
			Number    int    `json:"number"`
			Title     string `json:"title"`
			HTMLURL   string `json:"html_url"`
			State     string `json:"state"`
			User      struct{ Login string `json:"login"` } `json:"user"`
			Labels    []struct{ Name string `json:"name"` } `json:"labels"`
			Body      string `json:"body"`
			Comments  int    `json:"comments"`
			CreatedAt string `json:"created_at"`
			Repo      struct{ FullName string `json:"full_name"` } `json:"repository"`
			PR        *struct{ MergedAt string `json:"merged_at"` } `json:"pull_request"`
		} `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("decode search issues response: %w", err)
	}

	results := make([]IssueItem, 0, len(raw.Items))
	for _, item := range raw.Items {
		var labels []string
		for _, l := range item.Labels {
			labels = append(labels, l.Name)
		}
		var mergedAt string
		if item.PR != nil {
			mergedAt = item.PR.MergedAt
		}
		results = append(results, IssueItem{
			Number:    item.Number,
			Title:     item.Title,
			URL:       item.HTMLURL,
			State:     item.State,
			Author:    item.User.Login,
			Labels:    labels,
			Body:      item.Body,
			Comments:  item.Comments,
			CreatedAt: item.CreatedAt,
			MergedAt:  mergedAt,
			Repo:      item.Repo.FullName,
		})
	}
	return results, nil
}

// SearchRepos searches GitHub repositories.
// sort may be "stars", "forks", "updated", or "" for relevance.
func (c *Client) SearchRepos(ctx context.Context, query, sort string) ([]RepoSearchResult, error) {
	apiURL := fmt.Sprintf("%s/search/repositories?q=%s&per_page=%d",
		c.apiBase, url.QueryEscape(query), searchPerPageRepos)
	if sort != "" {
		apiURL += "&sort=" + url.QueryEscape(sort)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build search repos request: %w", err)
	}

	c.setHeaders(req)

	resp, err := c.http.Do(req) //nolint:gosec // URL built from validated inputs
	if err != nil {
		return nil, fmt.Errorf("search repos: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github search/repositories returned %d", resp.StatusCode)
	}

	var raw struct {
		Items []struct {
			FullName    string   `json:"full_name"`
			Description string   `json:"description"`
			Stars       int      `json:"stargazers_count"`
			Language    string   `json:"language"`
			Topics      []string `json:"topics"`
			PushedAt    string   `json:"pushed_at"`
			Archived    bool     `json:"archived"`
			HTMLURL     string   `json:"html_url"`
		} `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("decode search repos response: %w", err)
	}

	results := make([]RepoSearchResult, 0, len(raw.Items))
	for _, item := range raw.Items {
		results = append(results, RepoSearchResult{
			FullName:    item.FullName,
			Description: item.Description,
			Stars:       item.Stars,
			Language:    item.Language,
			Topics:      item.Topics,
			LastPush:    item.PushedAt,
			Archived:    item.Archived,
			HTMLURL:     item.HTMLURL,
		})
	}
	return results, nil
}

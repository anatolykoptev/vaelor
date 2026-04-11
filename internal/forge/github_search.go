package forge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// SearchCode implements Forge.
// When repos is non-empty, the search is restricted to those repositories.
func (g *GitHubForge) SearchCode(ctx context.Context, query string, repos []string) (_ []CodeResult, err error) {
	var sb strings.Builder
	sb.WriteString(query)
	for _, r := range repos {
		sb.WriteString(" repo:")
		sb.WriteString(r)
	}

	apiURL := fmt.Sprintf("%s/search/code?q=%s&per_page=%d",
		g.apiBase, url.QueryEscape(sb.String()), ghSearchPerPageCode)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build search code request: %w", err)
	}

	req.Header.Set(ghHeaderAccept, ghMediaTypeText)
	req.Header.Set(ghHeaderAPIVersion, ghAPIVersion)
	if g.token != "" {
		req.Header.Set(ghHeaderAuth, "Bearer "+g.token)
	}

	resp, err := g.http.Do(req) //nolint:gosec // URL built from validated inputs
	if err != nil {
		return nil, fmt.Errorf("search code: %w", err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		err = errors.Join(err, resp.Body.Close())
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github search/code returned %d", resp.StatusCode)
	}

	var raw struct {
		Items []struct {
			Name    string `json:"name"`
			Path    string `json:"path"`
			HTMLURL string `json:"html_url"`
			Repo    struct {
				FullName string `json:"full_name"`
			} `json:"repository"`
			TextMatches []struct {
				Fragment string `json:"fragment"`
			} `json:"text_matches"`
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

// SearchIssues implements Forge.
func (g *GitHubForge) SearchIssues(ctx context.Context, query string) (_ []IssueItem, err error) {
	apiURL := fmt.Sprintf("%s/search/issues?q=%s&per_page=%d&sort=updated&order=desc",
		g.apiBase, url.QueryEscape(query), ghSearchPerPageIssue)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build search issues request: %w", err)
	}

	g.setHeaders(req)

	resp, err := g.http.Do(req) //nolint:gosec // URL built from validated inputs
	if err != nil {
		return nil, fmt.Errorf("search issues: %w", err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		err = errors.Join(err, resp.Body.Close())
	}()

	if resp.StatusCode == http.StatusUnprocessableEntity {
		return nil, errors.New("github issues search failed (repo may not exist or query is invalid)")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github search/issues returned %d", resp.StatusCode)
	}

	var raw struct {
		Items []struct {
			Number  int    `json:"number"`
			Title   string `json:"title"`
			HTMLURL string `json:"html_url"`
			State   string `json:"state"`
			User    struct {
				Login string `json:"login"`
			} `json:"user"`
			Labels []struct {
				Name string `json:"name"`
			} `json:"labels"`
			Body      string `json:"body"`
			Comments  int    `json:"comments"`
			CreatedAt string `json:"created_at"`
			Repo      struct {
				FullName string `json:"full_name"`
			} `json:"repository"`
			PR *struct {
				MergedAt string `json:"merged_at"`
			} `json:"pull_request"`
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

// SearchRepos implements Forge.
// sort may be "stars", "forks", "updated", or "" for relevance.
func (g *GitHubForge) SearchRepos(ctx context.Context, query, sort string) (_ []RepoSearchResult, err error) {
	apiURL := fmt.Sprintf("%s/search/repositories?q=%s&per_page=%d",
		g.apiBase, url.QueryEscape(query), ghSearchPerPageRepos)
	if sort != "" {
		apiURL += "&sort=" + url.QueryEscape(sort)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build search repos request: %w", err)
	}

	g.setHeaders(req)

	resp, err := g.http.Do(req) //nolint:gosec // URL built from validated inputs
	if err != nil {
		return nil, fmt.Errorf("search repos: %w", err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		err = errors.Join(err, resp.Body.Close())
	}()

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

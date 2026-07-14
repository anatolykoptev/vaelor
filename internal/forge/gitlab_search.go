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
// GitLab code search is per-project, so we iterate over repos.
// When repos is empty we return an empty result — GitLab has no global code search in v4 REST.
func (g *GitLabForge) SearchCode(ctx context.Context, query string, repos []string, opts ...SearchCodeOptions) (CodeSearchResult, error) {
	var opt SearchCodeOptions
	if len(opts) > 0 {
		opt = opts[0]
	}
	if len(repos) == 0 {
		return CodeSearchResult{}, nil
	}

	perPage := opt.PerPage
	if perPage <= 0 {
		perPage = glSearchPerPage
	}
	page := opt.Page
	if page <= 0 {
		page = 1
	}

	var results []CodeResult
	for _, repo := range repos {
		hits, err := g.searchCodeInRepo(ctx, repo, query, perPage, page)
		if err != nil {
			return CodeSearchResult{}, err
		}
		results = append(results, hits...)
		if opt.MaxResults > 0 && len(results) >= opt.MaxResults {
			results = results[:opt.MaxResults]
			break
		}
	}
	return CodeSearchResult{Results: results}, nil
}

// searchCodeInRepo searches blobs in a single GitLab project.
func (g *GitLabForge) searchCodeInRepo(ctx context.Context, repo, query string, perPage, page int) (_ []CodeResult, err error) {
	apiURL := fmt.Sprintf("%s/api/v4/projects/%s/search?scope=blobs&search=%s&per_page=%d&page=%d",
		g.apiBase, encodeSlug(repo), url.QueryEscape(query), perPage, page)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build search code request: %w", err)
	}

	g.setHeaders(req)

	resp, err := g.http.Do(req) //nolint:gosec // URL built from validated inputs
	if err != nil {
		return nil, fmt.Errorf("search code in %s: %w", repo, err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		err = errors.Join(err, resp.Body.Close())
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("gitlab search/blobs returned %d for %s", resp.StatusCode, repo)
	}

	var raw []struct {
		Filename string `json:"filename"`
		Path     string `json:"path"`
		Data     string `json:"data"`
		Ref      string `json:"ref"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("decode search code response: %w", err)
	}

	results := make([]CodeResult, 0, len(raw))
	for _, item := range raw {
		results = append(results, CodeResult{
			Name:    item.Filename,
			Path:    item.Path,
			Repo:    repo,
			Content: item.Data,
		})
	}
	return results, nil
}

// SearchIssues implements Forge.
// Extracts a repo slug from "repo:owner/name" token in query if present,
// then searches that project's issues. Falls back to empty when no repo specified.
func (g *GitLabForge) SearchIssues(ctx context.Context, query string) (_ []IssueItem, err error) {
	repo, cleanQuery := extractRepoFromQuery(query)
	if repo == "" {
		return nil, nil
	}

	apiURL := fmt.Sprintf("%s/api/v4/projects/%s/search?scope=issues&search=%s",
		g.apiBase, encodeSlug(repo), url.QueryEscape(cleanQuery))

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

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("gitlab search/issues returned %d", resp.StatusCode)
	}

	var raw []struct {
		IID    int    `json:"iid"`
		Title  string `json:"title"`
		WebURL string `json:"web_url"`
		State  string `json:"state"`
		Author struct {
			Username string `json:"username"`
		} `json:"author"`
		Labels []struct {
			Title string `json:"title"`
		} `json:"labels"`
		Description    string `json:"description"`
		UserNotesCount int    `json:"user_notes_count"`
		CreatedAt      string `json:"created_at"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("decode search issues response: %w", err)
	}

	results := make([]IssueItem, 0, len(raw))
	for _, item := range raw {
		labels := make([]string, 0, len(item.Labels))
		for _, l := range item.Labels {
			labels = append(labels, l.Title)
		}
		results = append(results, IssueItem{
			Number:    item.IID,
			Title:     item.Title,
			URL:       item.WebURL,
			State:     item.State,
			Author:    item.Author.Username,
			Labels:    labels,
			Body:      item.Description,
			Comments:  item.UserNotesCount,
			CreatedAt: item.CreatedAt,
			Repo:      repo,
		})
	}
	return results, nil
}

// SearchRepos implements Forge.
// Uses the GitLab projects listing API, sorted by star_count descending.
func (g *GitLabForge) SearchRepos(ctx context.Context, query, _ string) (_ []RepoSearchResult, err error) {
	apiURL := fmt.Sprintf("%s/api/v4/projects?search=%s&order_by=star_count&sort=desc&per_page=%d",
		g.apiBase, url.QueryEscape(query), glSearchPerPage)

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
		return nil, fmt.Errorf("gitlab projects search returned %d", resp.StatusCode)
	}

	var raw []struct {
		PathWithNamespace string `json:"path_with_namespace"`
		Description       string `json:"description"`
		StarCount         int    `json:"star_count"`
		LastActivityAt    string `json:"last_activity_at"`
		Archived          bool   `json:"archived"`
		WebURL            string `json:"web_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("decode search repos response: %w", err)
	}

	results := make([]RepoSearchResult, 0, len(raw))
	for _, item := range raw {
		results = append(results, RepoSearchResult{
			FullName:    item.PathWithNamespace,
			Description: item.Description,
			Stars:       item.StarCount,
			LastPush:    item.LastActivityAt,
			Archived:    item.Archived,
			HTMLURL:     item.WebURL,
		})
	}
	return results, nil
}

// extractRepoFromQuery parses a "repo:owner/name" token from query.
// Returns the repo slug and the query with that token removed.
func extractRepoFromQuery(query string) (repo, clean string) {
	parts := strings.Fields(query)
	var keep []string
	for _, p := range parts {
		if strings.HasPrefix(p, "repo:") {
			repo = strings.TrimPrefix(p, "repo:")
		} else {
			keep = append(keep, p)
		}
	}
	return repo, strings.Join(keep, " ")
}

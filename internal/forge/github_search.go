package forge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/anatolykoptev/go-code/internal/cache"
	"golang.org/x/sync/errgroup"
)

const (
	codeSearchCacheTTL = 15 * time.Minute
	repoStarsCacheTTL  = time.Hour
	repoStarsWorkers   = 4
)

// SearchCode implements Forge.
// It supports repo restriction, language/extension filters, exclude_repos,
// min_stars, sort/order, pagination and result caching.
func (g *GitHubForge) SearchCode(ctx context.Context, query string, repos []string, opts ...SearchCodeOptions) (CodeSearchResult, error) {
	var opt SearchCodeOptions
	if len(opts) > 0 {
		opt = opts[0]
	}

	page := normalizePage(opt.Page)
	perPage, maxResults := resolveSearchParams(opt.PerPage, opt)

	q, err := buildGitHubCodeSearchQuery(query, repos, opt.ExcludeRepos, opt.FileExtensions, opt.Language)
	if err != nil {
		return CodeSearchResult{}, err
	}

	sort, order, err := validateGitHubCodeSearchSortOrder(opt.Sort, opt.Order)
	if err != nil {
		return CodeSearchResult{}, err
	}

	key := cache.Key("github:code:search", q, sort, order, strconv.Itoa(perPage), strconv.Itoa(page), strconv.Itoa(maxResults), strconv.Itoa(opt.MinStars))

	result, err := cacheGetOrLoadJSONWithTTL(g.cache, ctx, key, codeSearchCacheTTL, func(ctx context.Context) (CodeSearchResult, error) {
		return g.collectCodeSearchResults(ctx, q, sort, order, perPage, page, maxResults, opt.MinStars)
	})
	if err != nil {
		return CodeSearchResult{}, err
	}
	result.Query = q
	return result, nil
}

// ghCodeSearchResponse is the GitHub Code Search API response.
type ghCodeSearchResponse struct {
	TotalCount        int                `json:"total_count"`
	IncompleteResults bool               `json:"incomplete_results"`
	Items             []ghCodeSearchItem `json:"items"`
}

type ghCodeSearchItem struct {
	Name       string `json:"name"`
	Path       string `json:"path"`
	HTMLURL    string `json:"html_url"`
	Repository struct {
		FullName string `json:"full_name"`
	} `json:"repository"`
	TextMatches []struct {
		Fragment string `json:"fragment"`
	} `json:"text_matches"`
}

// collectCodeSearchResults fetches code search pages, applies min_stars
// filtering, and stops when maxResults is reached or there are no more results.
func (g *GitHubForge) collectCodeSearchResults(ctx context.Context, q, sort, order string, perPage, page, maxResults, minStars int) (CodeSearchResult, error) {
	var result CodeSearchResult
	currentPage := page

	for {
		if ctx.Err() != nil {
			return CodeSearchResult{}, ctx.Err()
		}

		data, err := g.fetchCodeSearchPage(ctx, q, sort, order, perPage, currentPage)
		if err != nil {
			return CodeSearchResult{}, err
		}

		if result.Total == 0 {
			result.Total = data.TotalCount
		}
		if data.IncompleteResults {
			result.Incomplete = true
		}

		if len(data.Items) == 0 {
			break
		}

		pageResults, err := g.filterCodeSearchPage(ctx, data.Items, minStars)
		if err != nil {
			return CodeSearchResult{}, err
		}

		result.Results = append(result.Results, pageResults...)

		if maxResults > 0 && len(result.Results) >= maxResults {
			result.Results = result.Results[:maxResults]
			break
		}

		if maxResults == 0 {
			break
		}

		if len(data.Items) < perPage {
			break
		}

		currentPage++
	}

	return result, nil
}

// fetchCodeSearchPage calls the GitHub Code Search API for a single page.
func (g *GitHubForge) fetchCodeSearchPage(ctx context.Context, q, sort, order string, perPage, page int) (ghCodeSearchResponse, error) {
	apiURL := buildGitHubCodeSearchURL(g.apiBase, q, sort, order, perPage, page)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return ghCodeSearchResponse{}, fmt.Errorf("build search code request: %w", err)
	}

	req.Header.Set(ghHeaderAccept, ghMediaTypeText)
	req.Header.Set(ghHeaderAPIVersion, ghAPIVersion)

	resp, err := g.doGitHubRequest(ctx, req)
	if err != nil {
		return ghCodeSearchResponse{}, err
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()

	var data ghCodeSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return ghCodeSearchResponse{}, fmt.Errorf("decode search code response: %w", err)
	}
	return data, nil
}

// filterCodeSearchPage converts API items into CodeResult and applies min_stars filtering.
func (g *GitHubForge) filterCodeSearchPage(ctx context.Context, items []ghCodeSearchItem, minStars int) ([]CodeResult, error) {
	results := convertCodeSearchItems(items)
	if minStars <= 0 {
		return results, nil
	}
	return g.filterByMinStars(ctx, results, minStars)
}

// convertCodeSearchItems converts GitHub code search API items into CodeResult values.
func convertCodeSearchItems(items []ghCodeSearchItem) []CodeResult {
	results := make([]CodeResult, 0, len(items))
	for _, item := range items {
		if item.HTMLURL == "" {
			continue
		}
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
			Repo:    item.Repository.FullName,
			Content: strings.Join(frags, "\n---\n"),
		})
	}
	return results
}

// fetchRepoStars returns the stargazers count for a repo, using cache.
func (g *GitHubForge) fetchRepoStars(ctx context.Context, repo string) (int, error) {
	key := cache.Key("github:repo:stars", repo)
	return cacheGetOrLoadJSONWithTTL(g.cache, ctx, key, repoStarsCacheTTL, func(ctx context.Context) (int, error) {
		return g.fetchRepoStarsImpl(ctx, repo)
	})
}

// fetchRepoStarsImpl performs the actual GitHub API call for repo star counts.
func (g *GitHubForge) fetchRepoStarsImpl(ctx context.Context, repo string) (int, error) {
	parts := strings.Split(repo, "/")
	if len(parts) != 2 {
		return 0, fmt.Errorf("invalid repo %q", repo)
	}

	apiURL := fmt.Sprintf("%s/repos/%s", g.apiBase, repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return 0, fmt.Errorf("build repo stars request: %w", err)
	}

	g.setHeaders(req)

	resp, err := g.doGitHubRequest(ctx, req)
	if err != nil {
		var apiErr *githubAPIError
		if errors.As(err, &apiErr) && apiErr.statusCode == http.StatusNotFound {
			return 0, nil
		}
		return 0, err
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()

	var data struct {
		StargazersCount int `json:"stargazers_count"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return 0, fmt.Errorf("decode repo stars response: %w", err)
	}
	return data.StargazersCount, nil
}

// filterByMinStars filters results so only results from repos with at least
// minStars remain.
func (g *GitHubForge) filterByMinStars(ctx context.Context, results []CodeResult, minStars int) ([]CodeResult, error) {
	if minStars <= 0 {
		return results, nil
	}

	uniqueRepos := make(map[string]struct{})
	for _, r := range results {
		if r.Repo != "" {
			uniqueRepos[r.Repo] = struct{}{}
		}
	}

	starCache := make(map[string]int, len(uniqueRepos))
	var mu sync.Mutex

	ggrp, ctx := errgroup.WithContext(ctx)
	ggrp.SetLimit(repoStarsWorkers)

	for repo := range uniqueRepos {
		repo := repo
		ggrp.Go(func() error {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			n, err := g.fetchRepoStars(ctx, repo)
			if err != nil {
				return err
			}
			mu.Lock()
			starCache[repo] = n
			mu.Unlock()
			return nil
		})
	}

	if err := ggrp.Wait(); err != nil {
		return nil, err
	}

	filtered := make([]CodeResult, 0, len(results))
	for _, r := range results {
		n, ok := starCache[r.Repo]
		if !ok {
			continue
		}
		if n >= minStars {
			filtered = append(filtered, r)
		}
	}
	return filtered, nil
}

// SearchIssues implements Forge.
func (g *GitHubForge) SearchIssues(ctx context.Context, query string) (_ []IssueItem, err error) {
	apiURL := fmt.Sprintf("%s/search/issues?q=%s&per_page=%d&sort=updated&order=desc",
		g.apiBase, urlQueryEscape(query), ghSearchPerPageIssue)

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
		g.apiBase, urlQueryEscape(query), ghSearchPerPageRepos)
	if sort != "" {
		apiURL += "&sort=" + urlQueryEscape(sort)
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

// urlQueryEscape is a small helper that wraps url.QueryEscape without importing
// net/url in this file. It is used by SearchIssues and SearchRepos.
func urlQueryEscape(s string) string {
	return url.QueryEscape(s)
}

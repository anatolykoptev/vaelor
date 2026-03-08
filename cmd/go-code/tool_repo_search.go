package main

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	kitcache "github.com/anatolykoptev/go-kit/cache"

	"github.com/anatolykoptev/go-code/internal/analyze"
	"github.com/anatolykoptev/go-code/internal/cache"
	"github.com/anatolykoptev/go-code/internal/forge"
	"github.com/anatolykoptev/go-code/internal/websearch"
	mcpserver "github.com/anatolykoptev/go-mcpserver"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	maxReposToEnrich = 12
	maxReadmeRunes   = 3000
)

const systemPromptRepoSearch = `You are recommending GitHub repositories based on search results.
For each relevant repository, explain:
1. What it does and why it's relevant to the query
2. Key stats (stars, language, last activity)
3. Notable features from the README

Rank by relevance to the query. Be concise. Include GitHub URLs.`

// RepoSearchInput is the input schema for the repo_search tool.
type RepoSearchInput struct {
	Query    string `json:"query" jsonschema_description:"What repositories to find. Supports GitHub syntax: 'language:go topic:ai', 'stars:>100'"`
	Language string `json:"language,omitempty" jsonschema_description:"Filter by programming language"`
	Sort     string `json:"sort,omitempty" jsonschema_description:"Sort by: stars, forks, updated"`
}

// repoHit holds a search result before enrichment.
type repoHit struct {
	Owner string
	Repo  string
	URL   string
}

// enrichedRepo holds enriched repo data.
type enrichedRepo struct {
	Owner       string
	Repo        string
	Description string
	Stars       int
	Language    string
	Topics      []string
	LastPush    string
	Archived    bool
	Readme      string
}

// registerRepoSearch registers the repo_search MCP tool.
func registerRepoSearch(server *mcp.Server, _ Config, deps analyze.Deps) {
	mcpserver.AddTool(server, &mcp.Tool{
		Name: "repo_search",
		Description: "Discover GitHub repositories for a task or technology. " +
			"Searches web + GitHub API, enriches with metadata (stars, language, topics), " +
			"fetches READMEs, and returns LLM-summarized recommendations.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input RepoSearchInput) (*mcp.CallToolResult, error) {
		if input.Query == "" {
			return errResult("query is required"), nil
		}

		// Apply language filter to query if provided.
		query := input.Query
		if input.Language != "" {
			query = fmt.Sprintf("%s language:%s", query, input.Language)
		}

		// Check cache.
		cacheKey := cache.Key("repo_search", query, input.Sort)
		if cached, ok, _ := kitcache.GetJSON[string](deps.ToolCache, ctx, cacheKey); ok {
			return textResult(cached), nil
		}

		// Step 1: Parallel search.
		repos := parallelRepoSearch(ctx, query, input.Sort, deps)
		if len(repos) == 0 {
			return textResult("No repositories found for: " + input.Query), nil
		}

		// Step 2: Dedup by owner/repo.
		unique := deduplicateRepoResults(repos)

		// Step 3: Parallel enrich.
		enriched := enrichRepoResults(ctx, unique, deps)

		// Step 4: Build text for LLM.
		repoText := buildRepoSearchContext(enriched)

		// Step 5: LLM summarize.
		prompt := fmt.Sprintf("Query: %s\n\nRepositories found:\n%s", input.Query, repoText)
		summary, err := deps.LLM.Complete(ctx, systemPromptRepoSearch, prompt)
		if err != nil {
			slog.Warn("repo_search: LLM summarization failed, returning raw data", "err", err)
			result := fmt.Sprintf("# Repository Search: %s\n\n%s", input.Query, repoText)
			return textResult(result), nil
		}

		result := fmt.Sprintf("# Repository Search: %s\n\n%s", input.Query, summary)
		if err := kitcache.SetJSONWithTTL(deps.ToolCache, ctx, cacheKey, result, 24*time.Hour); err != nil {
			slog.Warn("repo_search: failed to cache result", "key", cacheKey, "err", err)
		}
		return textResult(result), nil
	})
}

// parallelRepoSearch runs three searches concurrently and merges results.
func parallelRepoSearch(ctx context.Context, query, sort string, deps analyze.Deps) []repoHit {
	type searchFunc func() []repoHit

	searches := []searchFunc{
		func() []repoHit { return forgeAPIRepoHits(ctx, query, sort, deps.Forges) },
		func() []repoHit { return webSearchRepoHits(ctx, query, deps.WebSearch) },
	}

	var mu sync.Mutex
	var wg sync.WaitGroup
	var all []repoHit

	wg.Add(len(searches))
	for _, fn := range searches {
		fn := fn
		go func() {
			defer wg.Done()
			hits := fn()
			mu.Lock()
			all = append(all, hits...)
			mu.Unlock()
		}()
	}
	wg.Wait()

	return all
}

// forgeAPIRepoHits calls SearchRepos on all configured forges and aggregates hits.
func forgeAPIRepoHits(ctx context.Context, query, sort string, reg *forge.Registry) []repoHit {
	if reg == nil {
		return nil
	}
	var hits []repoHit
	for _, kind := range []forge.ForgeKind{forge.GitHub, forge.GitLab} {
		f := reg.Get(kind)
		if f == nil {
			continue
		}
		results, err := f.SearchRepos(ctx, query, sort)
		if err != nil {
			slog.Warn("repo_search: forge API search failed", "forge", kind, "query", query, "err", err)
			continue
		}
		for _, r := range results {
			owner, repo, ok := forge.ExtractOwnerRepo(r.HTMLURL)
			if !ok {
				// Fall back to FullName parsing.
				parts := strings.SplitN(r.FullName, "/", 2)
				if len(parts) != 2 {
					continue
				}
				hits = append(hits, repoHit{Owner: parts[0], Repo: parts[1], URL: r.HTMLURL})
				continue
			}
			hits = append(hits, repoHit{Owner: owner, Repo: repo, URL: r.HTMLURL})
		}
	}
	return hits
}

// webSearchRepoHits queries go-search smart_search (depth=fast) and extracts repo URLs.
func webSearchRepoHits(ctx context.Context, query string, client *websearch.Client) []repoHit {
	if client == nil {
		return nil
	}
	results, err := client.Search(ctx, query+" github repository")
	if err != nil {
		slog.Warn("repo_search: web search failed", "query", query, "err", err)
		return nil
	}
	hits := make([]repoHit, 0, len(results))
	for _, r := range results {
		owner, repo, ok := forge.ExtractOwnerRepo(r.URL)
		if !ok {
			continue
		}
		hits = append(hits, repoHit{Owner: owner, Repo: repo, URL: r.URL})
	}
	return hits
}

// deduplicateRepoResults deduplicates hits by lowercase owner/repo and limits to maxReposToEnrich.
func deduplicateRepoResults(hits []repoHit) []repoHit {
	seen := make(map[string]struct{}, len(hits))
	out := make([]repoHit, 0, min(len(hits), maxReposToEnrich))
	for _, h := range hits {
		key := strings.ToLower(h.Owner) + "/" + strings.ToLower(h.Repo)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, h)
		if len(out) >= maxReposToEnrich {
			break
		}
	}
	return out
}

// enrichRepoResults enriches each repo hit with metadata and README in parallel.
func enrichRepoResults(ctx context.Context, repos []repoHit, deps analyze.Deps) []enrichedRepo {
	type result struct {
		idx  int
		repo enrichedRepo
	}

	results := make(chan result, len(repos))
	var wg sync.WaitGroup

	for i, hit := range repos {
		wg.Add(1)
		i, hit := i, hit
		go func() {
			defer wg.Done()
			enriched := enrichSingleRepo(ctx, hit, deps)
			results <- result{idx: i, repo: enriched}
		}()
	}

	wg.Wait()
	close(results)

	enriched := make([]enrichedRepo, len(repos))
	for r := range results {
		enriched[r.idx] = r.repo
	}
	return enriched
}

// enrichSingleRepo fetches metadata and README for one repo.
func enrichSingleRepo(ctx context.Context, hit repoHit, deps analyze.Deps) enrichedRepo {
	slug := hit.Owner + "/" + hit.Repo
	out := enrichedRepo{Owner: hit.Owner, Repo: hit.Repo}

	// Detect the forge from the hit URL; fall back to GitHub.
	f := deps.Forges.ForURL(hit.URL)
	if f == nil {
		f = deps.Forges.Get(forge.GitHub)
	}
	if f == nil {
		return out
	}

	var wg sync.WaitGroup
	wg.Add(2) //nolint:mnd // exactly 2 concurrent fetches: meta + readme

	go func() {
		defer wg.Done()
		meta, err := f.FetchRepoMeta(ctx, slug)
		if err != nil {
			slog.Debug("repo_search: failed to fetch repo meta", "slug", slug, "err", err)
			return
		}
		out.Description = meta.Description
		out.Stars = meta.Stars
		out.Language = meta.Language
	}()

	go func() {
		defer wg.Done()
		readme, err := f.FetchREADME(ctx, slug)
		if err != nil {
			slog.Debug("repo_search: failed to fetch README", "slug", slug, "err", err)
			return
		}
		out.Readme = truncateRunes(readme, maxReadmeRunes)
	}()

	wg.Wait()
	return out
}

// truncateRunes truncates s to at most n runes, appending "..." if truncated.
func truncateRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	count := 0
	for i := range s {
		if count == n {
			return s[:i] + "..."
		}
		count++
	}
	return s
}

// buildRepoSearchContext formats enriched repos as text context for the LLM.
func buildRepoSearchContext(enriched []enrichedRepo) string {
	var sb strings.Builder
	for _, r := range enriched {
		if r.Owner == "" && r.Repo == "" {
			continue
		}
		slug := r.Owner + "/" + r.Repo
		lang := r.Language
		if lang == "" {
			lang = "unknown"
		}
		fmt.Fprintf(&sb, "## %s (%s, %d stars)\n", slug, lang, r.Stars)
		if r.Description != "" {
			fmt.Fprintf(&sb, "Description: %s\n", r.Description)
		}
		if len(r.Topics) > 0 {
			fmt.Fprintf(&sb, "Topics: %s\n", strings.Join(r.Topics, ", "))
		}
		if r.LastPush != "" {
			fmt.Fprintf(&sb, "Last push: %s\n", r.LastPush)
		}
		if r.Archived {
			sb.WriteString("Status: ARCHIVED\n")
		}
		if r.Readme != "" {
			fmt.Fprintf(&sb, "README excerpt:\n%s\n", r.Readme)
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

package main

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/anatolykoptev/go-code/internal/analyze"
	"github.com/anatolykoptev/go-code/internal/forge"
)

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

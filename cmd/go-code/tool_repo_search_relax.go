package main

import (
	"context"
	"strings"
	"sync"

	"github.com/anatolykoptev/vaelor/internal/forge"
	"github.com/anatolykoptev/vaelor/internal/websearch"
)

// webSearchQueries generates multiple search variations for better coverage.
// Instead of a single "query github repository", tries several angles.
func webSearchQueries(query string) []string {
	queries := []string{
		query + " github repository",
		query + " github",
	}

	// If query has 3+ words, also try a shorter "open source" variant.
	words := strings.Fields(query)
	if len(words) >= 3 {
		queries = append(queries, query+" open source")
	}

	return queries
}

// webSearchMultiQuery runs multiple web search queries and merges results.
func webSearchMultiQuery(ctx context.Context, query string, client *websearch.Client) []repoHit {
	if client == nil {
		return nil
	}

	queries := webSearchQueries(query)
	type result struct {
		hits []repoHit
	}

	var mu sync.Mutex
	var wg sync.WaitGroup
	var all []repoHit

	wg.Add(len(queries))
	for _, q := range queries {
		go func(q string) {
			defer wg.Done()
			results, err := client.Search(ctx, q)
			if err != nil {
				return
			}
			var hits []repoHit
			for _, r := range results {
				owner, repo, ok := forge.ExtractOwnerRepo(r.URL)
				if !ok {
					continue
				}
				hits = append(hits, repoHit{Owner: owner, Repo: repo, URL: r.URL})
			}
			mu.Lock()
			all = append(all, hits...)
			mu.Unlock()
		}(q)
	}
	wg.Wait()

	return all
}

// relaxQuery generates progressively simpler subqueries from the original.
// Returns nil if the query cannot be relaxed (single word or only GitHub syntax).
func relaxQuery(query string) []string {
	// Split into regular words and GitHub syntax (language:X, stars:>100, etc.)
	var words, syntax []string
	for _, w := range strings.Fields(query) {
		if strings.Contains(w, ":") {
			syntax = append(syntax, w)
		} else {
			words = append(words, w)
		}
	}

	if len(words) <= 1 {
		return nil
	}

	suffixStr := ""
	if len(syntax) > 0 {
		suffixStr = " " + strings.Join(syntax, " ")
	}

	var relaxed []string
	// Strategy 1: drop last word.
	relaxed = append(relaxed, strings.Join(words[:len(words)-1], " ")+suffixStr)

	// Strategy 2: first word only (the most specific term, usually the project name).
	if len(words) > 2 {
		relaxed = append(relaxed, words[0]+suffixStr)
	}

	return relaxed
}

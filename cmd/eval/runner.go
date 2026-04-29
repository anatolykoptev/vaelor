// Package main — eval harness for go-code retrieval quality.
//
// This file: concurrent dispatch of golden queries against a running go-code.
package main

import (
	"context"
	"sort"
	"sync"
)

// runnerCfg controls dispatch behavior.
type runnerCfg struct {
	Workers int
	TopK    int // top_k passed to semantic_search; metrics still computed at @10 / @20
}

// runEval dispatches every record through client.Search with runnerCfg.Workers
// goroutines. Results are returned in a deterministic order (per repo, then
// preserving file order) so two runs with the same golden + same target
// produce byte-identical JSON modulo the timestamp.
func runEval(ctx context.Context, client *MCPClient, golden *GoldenSet, cfg runnerCfg) []QueryResult {
	// Flatten + index for stable order.
	flat := golden.FlatQueries()
	if cfg.Workers <= 0 {
		cfg.Workers = 1
	}
	if cfg.TopK <= 0 {
		cfg.TopK = 20
	}

	// Pre-allocate so workers can write by index — no shared slice append.
	results := make([]QueryResult, len(flat))
	jobs := make(chan int, len(flat))
	for i := range flat {
		jobs <- i
	}
	close(jobs)

	var wg sync.WaitGroup
	for w := 0; w < cfg.Workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range jobs {
				results[idx] = runSingle(ctx, client, flat[idx], cfg.TopK)
			}
		}()
	}
	wg.Wait()

	// Repos sorted alphabetically; within a repo, original file order is
	// preserved by FlatQueries — no additional sort needed.
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].Repo != results[j].Repo {
			return results[i].Repo < results[j].Repo
		}
		// Tie-break on the query string for repeatability when the same repo
		// has identical-prefix queries.
		return results[i].Query < results[j].Query
	})
	return results
}

// runSingle executes one query and computes metrics on the response.
func runSingle(ctx context.Context, client *MCPClient, rec GoldenRecord, topK int) QueryResult {
	out := QueryResult{
		Repo:     rec.Repo,
		Query:    rec.Query,
		Expected: rec.ExpectedTop3,
	}
	hits, err := client.Search(ctx, rec.Repo, rec.Query, topK)
	if err != nil {
		out.Error = err.Error()
		return out
	}

	const (
		recall10K = 10
		recall20K = 20
	)
	out.Retrieved = retrievedKeys(hits)
	out.NDCG10 = NDCG10(hits, rec.ExpectedTop3)
	out.Recall10 = RecallAtK(hits, rec.ExpectedTop3, recall10K)
	out.Recall20 = RecallAtK(hits, rec.ExpectedTop3, recall20K)
	out.MRR = MRR(hits, rec.ExpectedTop3)
	return out
}

// retrievedKeys flattens the top hits into "<file>:<symbol>" form for the
// per_query JSON output. Capped at 20 to keep reports compact.
func retrievedKeys(hits []SearchHit) []string {
	const cap = 20
	n := len(hits)
	if n > cap {
		n = cap
	}
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, hits[i].File+":"+hits[i].Symbol)
	}
	return out
}

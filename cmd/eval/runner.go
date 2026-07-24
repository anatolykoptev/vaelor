// Package main — eval harness for go-code retrieval quality.
//
// This file: concurrent dispatch of golden queries against a running go-code.
package main

import (
	"context"
	"sort"
	"sync"
	"time"
)

// runnerCfg controls dispatch behavior.
type runnerCfg struct {
	Workers int
	TopK    int    // top_k passed to semantic_search; metrics still computed at @10 / @20
	Mode    string // eval mode: modeSemanticSearch (default) | modeRepoAnalyze
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
	if cfg.Mode == "" {
		cfg.Mode = modeSemanticSearch
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
				results[idx] = runSingle(ctx, client, flat[idx], cfg)
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

// runSingle executes one query and computes metrics on the response. The
// wall-clock latency of the tool call is recorded in Latency / LatencyMS.
//
// In semantic_search mode (default, byte-identical to pre-mode behavior):
// calls semantic_search, parses ranked (file,symbol) hits, and computes
// symbol-level nDCG@10 / Recall@10/@20 / MRR against rec.ExpectedTop3. When
// rec.Language is non-empty it is passed as the `language` filter; when empty
// no filter is sent.
//
// In repo_analyze mode: calls repo_analyze (deep mode), parses the ranked
// FILE list, and computes file-level nDCG@10 / Recall@10/@20 / MRR against
// the file-relevance target derived from rec.ExpectedTop3 (the set of files
// containing the labeled symbols). Latency and the language filter apply
// identically to semantic_search mode.
func runSingle(ctx context.Context, client *MCPClient, rec GoldenRecord, cfg runnerCfg) QueryResult {
	out := QueryResult{
		Repo:     rec.Repo,
		Query:    rec.Query,
		Language: rec.Language,
		Expected: rec.ExpectedTop3,
	}

	const (
		recall10K = 10
		recall20K = 20
	)

	if cfg.Mode == modeRepoAnalyze {
		searchStart := time.Now()
		rankedFiles, err := client.RepoAnalyze(ctx, rec.Repo, rec.Query, rec.Language)
		out.Latency = time.Since(searchStart)
		out.LatencyMS = float64(out.Latency) / float64(time.Millisecond)
		if err != nil {
			out.Error = err.Error()
			return out
		}
		// File-level relevance: a returned file is relevant iff it is in the
		// set of files containing the golden's expected_top_3 symbols. Reuse
		// the rank-based metric functions via synthetic file-only hits and
		// "<file>:" expected labels (matchExpected case-3 suffix match).
		fileHits := fileHitsFromPaths(rankedFiles)
		fileExp := fileLevelExpected(rec.ExpectedTop3)
		out.Retrieved = retrievedFileKeys(rankedFiles)
		out.NDCG10 = NDCG10(fileHits, fileExp)
		out.Recall10 = RecallAtK(fileHits, fileExp, recall10K)
		out.Recall20 = RecallAtK(fileHits, fileExp, recall20K)
		out.MRR = MRR(fileHits, fileExp)
		return out
	}

	searchStart := time.Now()
	hits, err := client.Search(ctx, rec.Repo, rec.Query, rec.Language, cfg.TopK)
	out.Latency = time.Since(searchStart)
	out.LatencyMS = float64(out.Latency) / float64(time.Millisecond)
	if err != nil {
		out.Error = err.Error()
		return out
	}

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

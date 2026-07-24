// Package main — eval harness for go-code retrieval quality.
//
// This file: concurrent dispatch of golden queries against a running go-code.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"
)

// runnerCfg controls dispatch behavior.
type runnerCfg struct {
	Workers       int
	TopK          int           // top_k passed to semantic_search; metrics still computed at @10 / @20
	Mode          string        // eval mode: modeSemanticSearch (default) | modeRepoAnalyze
	RetryAttempts int           // max attempts per query on transient tool signals (1 = no retry)
	RetryBase     time.Duration // base exponential backoff for transient retries
	RetryCap      time.Duration // max backoff between transient retries
}

// defaultRetryBase / defaultRetryCap are the production backoff parameters.
// Tests inject tiny values via runnerCfg so they don't sleep real seconds.
const (
	defaultRetryBase = 2 * time.Second
	defaultRetryCap  = 15 * time.Second
)

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

// runWithRetry wraps a tool call with exponential backoff retry on
// ErrTransient. It returns the number of retries made (0 = first attempt
// succeeded) and the final error (nil on success). A non-transient error or
// success ends the loop immediately. On budget exhaustion the last transient
// error is returned so the caller can format a "transient after N retries"
// message. Context cancellation during a backoff sleep is respected and
// returned immediately.
func runWithRetry(ctx context.Context, attempt func() error, cfg runnerCfg) (int, error) {
	backoff := cfg.RetryBase
	if backoff <= 0 {
		backoff = defaultRetryBase
	}
	cap := cfg.RetryCap
	if cap <= 0 {
		cap = defaultRetryCap
	}
	maxAttempts := cfg.RetryAttempts
	if maxAttempts <= 0 {
		maxAttempts = 1
	}

	var lastErr error
	for i := 0; i < maxAttempts; i++ {
		err := attempt()
		if err == nil {
			return i, nil
		}
		lastErr = err
		if !errors.Is(err, ErrTransient{}) {
			return i, err
		}
		if i >= maxAttempts-1 {
			return i, lastErr
		}
		select {
		case <-time.After(backoff):
		case <-ctx.Done():
			return i, ctx.Err()
		}
		backoff *= 2
		if backoff > cap {
			backoff = cap
		}
	}
	return maxAttempts - 1, lastErr
}

// runSingle executes one query and computes metrics on the response. The
// wall-clock latency of the FINAL (successful) tool attempt is recorded in
// Latency / LatencyMS.
//
// Both modes wrap the tool call in runWithRetry so transient tool signals
// (soft-deadline timeout, indexing status) are retried with exponential
// backoff instead of becoming permanent hard errors. On budget exhaustion the
// result's Error is set to "transient after N retries: <reason>" — never a
// silent empty-success. The Retries field records how many retry attempts
// were made (0 = first attempt succeeded).
//
// In semantic_search mode (default): calls semantic_search, parses ranked
// (file,symbol) hits, and computes symbol-level nDCG@10 / Recall@10/@20 / MRR
// against rec.ExpectedTop3. When rec.Language is non-empty it is passed as the
// `language` filter; when empty no filter is sent.
//
// In repo_analyze mode: calls repo_analyze (deep mode), parses the ranked
// FILE list, and computes file-level nDCG@10 / Recall@10/@20 / MRR against
// the file-relevance target derived from rec.ExpectedTop3. Latency and the
// language filter apply identically to semantic_search mode.
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
		var rankedFiles []string
		var lastLatency time.Duration
		retries, err := runWithRetry(ctx, func() error {
			start := time.Now()
			var e error
			rankedFiles, e = client.RepoAnalyze(ctx, rec.Repo, rec.Query, rec.Language)
			lastLatency = time.Since(start)
			return e
		}, cfg)
		out.Retries = retries
		out.Latency = lastLatency
		out.LatencyMS = float64(lastLatency) / float64(time.Millisecond)
		if err != nil {
			out.Error = formatRetryError(retries, err)
			return out
		}
		fileHits := fileHitsFromPaths(rankedFiles)
		fileExp := fileLevelExpected(rec.ExpectedTop3)
		out.Retrieved = retrievedFileKeys(rankedFiles)
		out.NDCG10 = NDCG10(fileHits, fileExp)
		out.Recall10 = RecallAtK(fileHits, fileExp, recall10K)
		out.Recall20 = RecallAtK(fileHits, fileExp, recall20K)
		out.MRR = MRR(fileHits, fileExp)
		return out
	}

	var hits []SearchHit
	var lastLatency time.Duration
	retries, err := runWithRetry(ctx, func() error {
		start := time.Now()
		var e error
		hits, e = client.Search(ctx, rec.Repo, rec.Query, rec.Language, cfg.TopK)
		lastLatency = time.Since(start)
		return e
	}, cfg)
	out.Retries = retries
	out.Latency = lastLatency
	out.LatencyMS = float64(lastLatency) / float64(time.Millisecond)
	if err != nil {
		out.Error = formatRetryError(retries, err)
		return out
	}

	out.Retrieved = retrievedKeys(hits)
	out.NDCG10 = NDCG10(hits, rec.ExpectedTop3)
	out.Recall10 = RecallAtK(hits, rec.ExpectedTop3, recall10K)
	out.Recall20 = RecallAtK(hits, rec.ExpectedTop3, recall20K)
	out.MRR = MRR(hits, rec.ExpectedTop3)
	return out
}

// formatRetryError formats the final error for a QueryResult. Transient errors
// that exhausted the retry budget get a "transient after N retries: <reason>"
// message; all other errors keep their raw message.
func formatRetryError(retries int, err error) string {
	var te ErrTransient
	if errors.As(err, &te) {
		return fmt.Sprintf("transient after %d retries: %s", retries, te.Reason)
	}
	return err.Error()
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

// warmupRepos probes each DISTINCT resolved repo in the golden set with one
// query (the repo's first golden query), retrying transient signals until the
// repo returns non-transient (hits or a definitive ready-empty) or the
// per-repo warmup-timeout elapses. This ensures the measured pass runs against
// warm indexes and the measured latency isn't polluted by first-hit indexing.
//
// Warmup uses a LONGER budget than per-query retry (the per-repo timeout, not
// the per-query retry budget). Each repo is logged:
//
//	warmup <repo> -> ready (n hits) | timeout | error
//
// A timeout or error on one repo does NOT abort the warmup of the others; the
// measured run proceeds regardless (warmup is best-effort).
func warmupRepos(ctx context.Context, client *MCPClient, golden *GoldenSet, cfg runnerCfg, perRepoTimeout time.Duration) {
	type repoProbe struct {
		repo string
		rec  GoldenRecord
	}
	seen := make(map[string]bool)
	var probes []repoProbe
	for _, rec := range golden.FlatQueries() {
		if rec.Repo == "" || seen[rec.Repo] {
			continue
		}
		seen[rec.Repo] = true
		probes = append(probes, repoProbe{repo: rec.Repo, rec: rec})
	}

	for _, p := range probes {
		repoCtx, cancel := context.WithTimeout(ctx, perRepoTimeout)
		n, err := warmupOneRepo(repoCtx, client, p.rec, cfg)
		cancel()
		switch {
		case err == nil:
			slog.Info("warmup",
				slog.String("repo", p.repo),
				slog.String("status", "ready"),
				slog.Int("hits", n),
			)
		case errors.Is(err, context.DeadlineExceeded):
			slog.Info("warmup",
				slog.String("repo", p.repo),
				slog.String("status", "timeout"),
			)
		default:
			slog.Info("warmup",
				slog.String("repo", p.repo),
				slog.String("status", "error"),
				slog.Any("error", err),
			)
		}
	}
}

// warmupOneRepo issues probe queries for a single repo, retrying on
// ErrTransient with exponential backoff, until the repo returns non-transient
// or ctx expires. Returns the hit/file count on success. Uses the same retry
// backoff parameters as the per-query retry loop but loops until the context
// deadline (a longer budget than per-query maxAttempts).
func warmupOneRepo(ctx context.Context, client *MCPClient, rec GoldenRecord, cfg runnerCfg) (int, error) {
	backoff := cfg.RetryBase
	if backoff <= 0 {
		backoff = defaultRetryBase
	}
	cap := cfg.RetryCap
	if cap <= 0 {
		cap = defaultRetryCap
	}

	for {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		if cfg.Mode == modeRepoAnalyze {
			files, err := client.RepoAnalyze(ctx, rec.Repo, rec.Query, rec.Language)
			if err == nil {
				return len(files), nil
			}
			if !errors.Is(err, ErrTransient{}) {
				return 0, err
			}
		} else {
			hits, err := client.Search(ctx, rec.Repo, rec.Query, rec.Language, cfg.TopK)
			if err == nil {
				return len(hits), nil
			}
			if !errors.Is(err, ErrTransient{}) {
				return 0, err
			}
		}
		select {
		case <-time.After(backoff):
		case <-ctx.Done():
			return 0, ctx.Err()
		}
		backoff *= 2
		if backoff > cap {
			backoff = cap
		}
	}
}

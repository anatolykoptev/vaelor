// Package main — eval harness for go-code retrieval quality.
//
// This file: JSON output schema and writer.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"time"
)

// QueryResult is one row of harness output: the query, the retrieved hits
// (top-K, capped to 20 to keep the file readable), and per-query metrics.
type QueryResult struct {
	Repo      string   `json:"repo"`
	Query     string   `json:"query"`
	Expected  []string `json:"expected_top_3"`
	Retrieved []string `json:"retrieved_top_20"`
	NDCG10    float64  `json:"ndcg10"`
	Recall10  float64  `json:"recall10"`
	Recall20  float64  `json:"recall20"`
	MRR       float64  `json:"mrr"`
	Error     string   `json:"error,omitempty"`
}

// Aggregates is the mean of each metric across all queries.
type Aggregates struct {
	NDCG10   float64 `json:"ndcg10"`
	Recall10 float64 `json:"recall10"`
	Recall20 float64 `json:"recall20"`
	MRR      float64 `json:"mrr"`
	Queries  int     `json:"queries"`
	Errors   int     `json:"errors"`
}

// PerRepoAggregate is the mean of each metric for a single repo.
type PerRepoAggregate struct {
	Aggregates
	Repo string `json:"repo"`
}

// DeltaBlock is the A/B comparison output (only set when --baseline given).
// p-values are formatted as strings so the JSON stays human-readable.
type DeltaBlock struct {
	NDCG10   string `json:"ndcg10"`
	Recall10 string `json:"recall10"`
	Recall20 string `json:"recall20"`
	MRR      string `json:"mrr"`
}

// Metadata captures the run context for reproducibility.
type Metadata struct {
	Timestamp time.Time `json:"timestamp"`
	TargetURL string    `json:"target_url"`
	GitSHA    string    `json:"git_sha,omitempty"`
	GoldenDir string    `json:"golden_dir"`
	TopK      int       `json:"top_k"`
}

// Report is the full harness output. Delta is omitted when --baseline is unset.
// Gate is populated when --baseline and --splade-weight are both provided.
// GraphGate is populated when --baseline and --graph-weight are both provided.
type Report struct {
	Metadata   Metadata           `json:"metadata"`
	PerQuery   []QueryResult      `json:"per_query"`
	PerRepo    []PerRepoAggregate `json:"per_repo"`
	Aggregates Aggregates         `json:"aggregates"`
	Delta      *DeltaBlock        `json:"delta,omitempty"`
	Gate       *GateResult        `json:"splade_gate,omitempty"`
	GraphGate  *GateResult        `json:"graph_gate,omitempty"`
}

// computeAggregates returns mean metrics across all non-error queries.
//
// Errors are counted separately and excluded from the mean: a stuck query
// shouldn't tank the aggregate of a 200-record corpus.
func computeAggregates(results []QueryResult) Aggregates {
	agg := Aggregates{}
	var n int
	for _, r := range results {
		if r.Error != "" {
			agg.Errors++
			continue
		}
		agg.NDCG10 += r.NDCG10
		agg.Recall10 += r.Recall10
		agg.Recall20 += r.Recall20
		agg.MRR += r.MRR
		n++
	}
	agg.Queries = n
	if n == 0 {
		return agg
	}
	fn := float64(n)
	agg.NDCG10 /= fn
	agg.Recall10 /= fn
	agg.Recall20 /= fn
	agg.MRR /= fn
	return agg
}

// computePerRepo groups results by repo and computes per-repo aggregates,
// returned in deterministic alphabetical order.
func computePerRepo(results []QueryResult) []PerRepoAggregate {
	byRepo := make(map[string][]QueryResult)
	for _, r := range results {
		byRepo[r.Repo] = append(byRepo[r.Repo], r)
	}
	repos := make([]string, 0, len(byRepo))
	for k := range byRepo {
		repos = append(repos, k)
	}
	sort.Strings(repos)

	out := make([]PerRepoAggregate, 0, len(repos))
	for _, repo := range repos {
		out = append(out, PerRepoAggregate{
			Repo:       repo,
			Aggregates: computeAggregates(byRepo[repo]),
		})
	}
	return out
}

// writeReport marshals report as pretty-JSON to path. Truncates an existing
// file with the same name. Empty path writes to stdout.
func writeReport(path string, report Report) error {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal report: %w", err)
	}
	if path == "" || path == "-" {
		_, err = os.Stdout.Write(append(data, '\n'))
		return err
	}
	const reportPerm = 0o600
	if err := os.WriteFile(path, append(data, '\n'), reportPerm); err != nil {
		return fmt.Errorf("write report: %w", err)
	}
	return nil
}

// readReport loads a previously-written Report (used for --baseline).
func readReport(path string) (Report, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- path is a CLI flag
	if err != nil {
		return Report{}, fmt.Errorf("read baseline: %w", err)
	}
	var r Report
	if err := json.Unmarshal(data, &r); err != nil {
		return Report{}, fmt.Errorf("parse baseline: %w", err)
	}
	return r, nil
}

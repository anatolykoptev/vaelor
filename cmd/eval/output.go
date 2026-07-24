// Package main — eval harness for go-code retrieval quality.
//
// This file: JSON output schema and writer.
package main

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sort"
	"time"
)

// QueryResult is one row of harness output: the query, the retrieved hits
// (top-K, capped to 20 to keep the file readable), and per-query metrics.
//
// Latency records the wall-clock duration of the semantic_search call. It is
// serialized as LatencyMS (milliseconds, float64) for human-readable JSON; the
// time.Duration field is kept for internal type-safe arithmetic.
type QueryResult struct {
	Repo      string        `json:"repo"`
	Query     string        `json:"query"`
	Language  string        `json:"language,omitempty"`
	Expected  []string      `json:"expected_top_3"`
	Retrieved []string      `json:"retrieved_top_20"`
	NDCG10    float64       `json:"ndcg10"`
	Recall10  float64       `json:"recall10"`
	Recall20  float64       `json:"recall20"`
	MRR       float64       `json:"mrr"`
	Latency   time.Duration `json:"-"`
	LatencyMS float64       `json:"latency_ms"`
	Retries   int           `json:"retries,omitempty"`
	Error     string        `json:"error,omitempty"`
}

// LatencyStats holds the mean and p50/p95/p99 latency percentiles in
// milliseconds. All four are computed over non-error queries only.
type LatencyStats struct {
	MeanMS float64 `json:"mean_ms"`
	P50MS  float64 `json:"p50_ms"`
	P95MS  float64 `json:"p95_ms"`
	P99MS  float64 `json:"p99_ms"`
}

// Aggregates is the mean of each metric across all queries plus latency
// percentiles. Latency percentiles are 0 when no non-error queries exist.
type Aggregates struct {
	NDCG10         float64 `json:"ndcg10"`
	Recall10       float64 `json:"recall10"`
	Recall20       float64 `json:"recall20"`
	MRR            float64 `json:"mrr"`
	Queries        int     `json:"queries"`
	Errors         int     `json:"errors"`
	QueriesRetried int     `json:"queries_retried,omitempty"`
	LatencyStats
}

// PerRepoAggregate is the mean of each metric for a single repo.
type PerRepoAggregate struct {
	Aggregates
	Repo string `json:"repo"`
}

// PerLanguageAggregate is the mean of each metric for a single language bucket.
// Records without a `language` field aggregate under "unspecified".
type PerLanguageAggregate struct {
	Aggregates
	Language string `json:"language"`
}

// DeltaBlock is the A/B comparison output (only set when --baseline given).
// p-values are formatted as strings so the JSON stays human-readable.
type DeltaBlock struct {
	NDCG10    string `json:"ndcg10"`
	Recall10  string `json:"recall10"`
	Recall20  string `json:"recall20"`
	MRR       string `json:"mrr"`
	LatencyMS string `json:"latency_ms"`
}

// Metadata captures the run context for reproducibility.
type Metadata struct {
	Timestamp   time.Time `json:"timestamp"`
	TargetURL   string    `json:"target_url"`
	GitSHA      string    `json:"git_sha,omitempty"`
	GoldenDir   string    `json:"golden_dir"`
	TopK        int       `json:"top_k"`
	KeywordArm  string    `json:"keyword_arm,omitempty"`
	FusionMode  string    `json:"fusion_mode,omitempty"`
	RepoMapPath string    `json:"repo_map,omitempty"`
	// Mode is the eval mode (semantic_search | repo_analyze). Omitted for the
	// default (semantic_search) so default runs stay byte-identical to the
	// pre-mode harness.
	Mode string `json:"mode,omitempty"`
}

// Report is the full harness output. Delta is omitted when --baseline is unset.
// Gate is populated when --baseline and --splade-weight are both provided.
// GraphGate is populated when --baseline and --graph-weight are both provided.
// KeywordArmGate is populated when --baseline and --keyword-arm are both
// provided. FusionGate is populated when --fusion-mode is provided (always
// reports NOT_EXERCISED until the harness calls repo_analyze — separate task).
type Report struct {
	Metadata       Metadata               `json:"metadata"`
	PerQuery       []QueryResult          `json:"per_query"`
	PerRepo        []PerRepoAggregate     `json:"per_repo"`
	PerLanguage    []PerLanguageAggregate `json:"per_language,omitempty"`
	Aggregates     Aggregates             `json:"aggregates"`
	Delta          *DeltaBlock            `json:"delta,omitempty"`
	Gate           *GateResult            `json:"splade_gate,omitempty"`
	GraphGate      *GateResult            `json:"graph_gate,omitempty"`
	KeywordArmGate *GateResult            `json:"keyword_arm_gate,omitempty"`
	FusionGate     *GateResult            `json:"fusion_gate,omitempty"`
}

// computeAggregates returns mean metrics across all non-error queries plus
// latency percentiles (p50/p95/p99/mean in ms).
//
// Errors are counted separately and excluded from the mean: a stuck query
// shouldn't tank the aggregate of a 200-record corpus. Latency percentiles
// use the nearest-rank method over sorted per-query latencies.
func computeAggregates(results []QueryResult) Aggregates {
	agg := Aggregates{}
	var n int
	latencies := make([]float64, 0, len(results))
	for _, r := range results {
		if r.Retries > 0 {
			agg.QueriesRetried++
		}
		if r.Error != "" {
			agg.Errors++
			continue
		}
		agg.NDCG10 += r.NDCG10
		agg.Recall10 += r.Recall10
		agg.Recall20 += r.Recall20
		agg.MRR += r.MRR
		latencies = append(latencies, r.LatencyMS)
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
	agg.LatencyStats = computeLatencyStats(latencies)
	return agg
}

// computeLatencyStats computes mean + p50/p95/p99 over a slice of per-query
// latencies in milliseconds. The input slice is sorted in place; an empty or
// nil slice returns zero stats.
func computeLatencyStats(latenciesMS []float64) LatencyStats {
	if len(latenciesMS) == 0 {
		return LatencyStats{}
	}
	sorted := make([]float64, len(latenciesMS))
	copy(sorted, latenciesMS)
	sort.Float64s(sorted)

	var sum float64
	for _, v := range sorted {
		sum += v
	}
	return LatencyStats{
		MeanMS: sum / float64(len(sorted)),
		P50MS:  percentile(sorted, 50),
		P95MS:  percentile(sorted, 95),
		P99MS:  percentile(sorted, 99),
	}
}

// percentile returns the p-th percentile from a SORTED ascending slice using
// the nearest-rank method: rank = ceil(p/100 * n), 1-based; returns the
// element at that rank. For n=1 returns the single element for any p.
func percentile(sorted []float64, p float64) float64 {
	n := len(sorted)
	if n == 0 {
		return 0
	}
	if n == 1 {
		return sorted[0]
	}
	// Nearest-rank: rank = ceil(p/100 * n), 1-based → index = rank - 1.
	rank := int(math.Ceil(p / 100.0 * float64(n)))
	if rank < 1 {
		rank = 1
	}
	if rank > n {
		rank = n
	}
	return sorted[rank-1]
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

// computePerLanguage groups results by language bucket and computes
// per-language aggregates, returned in deterministic alphabetical order.
// Records with an empty Language field aggregate under "unspecified".
func computePerLanguage(results []QueryResult) []PerLanguageAggregate {
	byLang := make(map[string][]QueryResult)
	for _, r := range results {
		lang := r.Language
		if lang == "" {
			lang = "unspecified"
		}
		byLang[lang] = append(byLang[lang], r)
	}
	langs := make([]string, 0, len(byLang))
	for k := range byLang {
		langs = append(langs, k)
	}
	sort.Strings(langs)

	out := make([]PerLanguageAggregate, 0, len(langs))
	for _, lang := range langs {
		out = append(out, PerLanguageAggregate{
			Language:   lang,
			Aggregates: computeAggregates(byLang[lang]),
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

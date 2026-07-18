// cmd/go-code/tool_debug_investigate_metrics.go
package main

import (
	"context"
	"fmt"
	"math"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/anatolykoptev/vaelor/internal/investigate"
	"github.com/anatolykoptev/vaelor/internal/promclient"
)

// promQueryConcurrency is the maximum number of concurrent Prometheus queries
// issued within a single compute*Spikes call. Bounded to avoid overwhelming
// Prometheus with unbounded fan-out on large metric registries.
const promQueryConcurrency = 10

// MetricSpike is re-exported from investigate for use within this package.
type MetricSpike = investigate.MetricSpike

// failureMetricRegex matches metric names that strongly suggest a failure
// counter. All alternatives require a _total suffix to avoid matching gauges
// (e.g. auth_outcome is a gauge; signaling_call_outcome_total is a counter).
// Hand-curated; expand as new counter conventions appear.
var failureMetricRegex = regexp.MustCompile(`(?i)(_failed_total|_failures_total|_failure_total|_errors?_total|_dropped_total|_outcome_total)$`)

// discoverFailureMetrics is a pure filter — it returns names from metricNames
// that match failure-counter naming conventions. No network call; callers must
// pass the result of promclient.Client.MetricNames.
func discoverFailureMetrics(metricNames []string) []string {
	var out []string
	for _, name := range metricNames {
		if failureMetricRegex.MatchString(name) {
			out = append(out, name)
		}
	}
	return out
}

// rankSpikes returns the top-k spikes by score, descending. Stable for ties.
func rankSpikes(spikes []MetricSpike, k int) []MetricSpike {
	sort.SliceStable(spikes, func(i, j int) bool { return spikes[i].Score > spikes[j].Score })
	if k > 0 && len(spikes) > k {
		return spikes[:k]
	}
	return spikes
}

// bucketRatio buckets w/b ratio into anomaly score. Returns (score, ratio).
// If baseline is empty but window has data, returns scoreBaselineEmpty.
func bucketRatio(window, baseline float64) (float64, float64) {
	if baseline <= 0 {
		if window > 0 {
			return scoreBaselineEmpty, math.Inf(1)
		}
		return scoreNominal, 0
	}
	ratio := window / baseline
	switch {
	case ratio > ratioCritical:
		return scoreCritical, ratio
	case ratio > ratioElevated:
		return scoreElevated, ratio
	case ratio > ratioMild:
		return scoreMild, ratio
	default:
		return scoreNominal, ratio
	}
}

// isEmpty reports whether a QueryRangeResponse carries no usable data.
// Prometheus may return a non-empty Result with all-zero values when the
// label set matches but there is no actual signal — those count as empty
// for the purpose of the service= → job= fallback.
func isEmpty(resp *promclient.QueryRangeResponse) bool {
	if resp == nil || len(resp.Data.Result) == 0 {
		return true
	}
	for _, series := range resp.Data.Result {
		for _, v := range series.Values {
			if len(v) < 2 {
				continue
			}
			s, ok := v[1].(string)
			if !ok {
				continue
			}
			f, err := strconv.ParseFloat(s, 64)
			if err != nil {
				continue
			}
			if f != 0 {
				return false
			}
		}
	}
	return true
}

// queryRangeWithJobFallback runs query (which must contain service=%q) against
// [start, end]. If the response is empty, it retries with job=%q substituted
// for service=%q. Returns (series, usedJobLabel, queries, error).
// queries counts attempted Prometheus RPCs (success + error), matching the
// pre-parallelization accounting convention.
func queryRangeWithJobFallback(ctx context.Context, prom *promclient.Client, query, service string, start, end time.Time) (*promclient.QueryRangeResponse, bool, int, error) {
	queries := 0
	series, err := prom.QueryRange(ctx, query, start, end, 60*time.Second)
	queries++
	if err != nil {
		return nil, false, queries, err
	}
	if !isEmpty(series) {
		return series, false, queries, nil
	}
	// Fall back to job= label.
	jobQuery := strings.ReplaceAll(query, fmt.Sprintf("service=%q", service), fmt.Sprintf("job=%q", service))
	jobSeries, jerr := prom.QueryRange(ctx, jobQuery, start, end, 60*time.Second)
	queries++
	if jerr != nil {
		return series, false, queries, nil // original empty result is fine; ignore fallback error
	}
	return jobSeries, true, queries, nil
}

// analyzeFailureCandidate queries window + baseline for one failure metric and
// returns a spike if anomalous, along with the count of attempted Prometheus RPCs.
// Thread-safe: no shared state.
func analyzeFailureCandidate(ctx context.Context, prom *promclient.Client, service, name string, start, end, baselineStart, baselineEnd time.Time) (*MetricSpike, int) {
	query := fmt.Sprintf(`sum(rate(%s{service=%q}[1m]))`, name, service)
	windowSeries, usedJob, wq, werr := queryRangeWithJobFallback(ctx, prom, query, service, start, end)
	if werr != nil {
		return nil, wq
	}
	baseSeries, _, bq, berr := queryRangeWithJobFallback(ctx, prom, query, service, baselineStart, baselineEnd)
	queries := wq + bq
	if berr != nil {
		return nil, queries
	}
	score, ratio := bucketRatio(maxSampleValue(windowSeries), maxSampleValue(baseSeries))
	if score <= scoreNominal {
		return nil, queries // not anomalous
	}
	// Display label uses %s (no quotes) because formatInvestigationResult renders
	// it as an XML attribute with %q; PromQL queries above use %q for label values.
	labelStr := fmt.Sprintf(`{service=%s}`, service)
	if usedJob {
		labelStr = fmt.Sprintf(`{job=%s}`, service)
	}
	return &MetricSpike{
		Kind:       "failure",
		MetricName: name,
		Labels:     labelStr,
		Ratio:      ratio,
		Score:      score,
	}, queries
}

// computeFailureSpikes returns MetricSpike entries for discovered failure-counter
// metrics showing anomalous window vs baseline ratios.
// Candidates run in parallel (bounded by promQueryConcurrency); per-candidate
// window + baseline queries run serially within each goroutine.
// metricNames is the pre-fetched list from promclient.Client.MetricNames.
// spikeAnalyzer analyzes one metric candidate over a window vs baseline and
// returns a spike (or nil) plus the count of Prometheus RPCs attempted.
type spikeAnalyzer func(ctx context.Context, prom *promclient.Client, service, name string, start, end, baselineStart, baselineEnd time.Time) (*MetricSpike, int)

// computeSpikes runs analyze over candidates in parallel (bounded by
// promQueryConcurrency), deriving the baseline window (1h before start, same
// duration) once, and merges the non-nil spikes on the calling goroutine.
// It is the shared engine behind computeFailureSpikes / computeLatencySpikes /
// computeSaturationSpikes — they differ only in candidate discovery + analyzer.
func computeSpikes(ctx context.Context, prom *promclient.Client, service string, candidates []string, start, end time.Time, diags *investigate.Diagnostics, analyze spikeAnalyzer) []MetricSpike {
	if len(candidates) == 0 {
		return nil
	}

	windowDur := end.Sub(start)
	baselineEnd := start.Add(-1 * time.Hour)
	baselineStart := baselineEnd.Add(-windowDur)

	type result struct {
		spike   *MetricSpike
		queries int
	}
	results := make([]result, len(candidates))

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(promQueryConcurrency)
	for i, name := range candidates {
		i, name := i, name
		g.Go(func() error {
			spike, queries := analyze(gctx, prom, service, name, start, end, baselineStart, baselineEnd)
			results[i] = result{spike: spike, queries: queries}
			return nil // best-effort phase: errors absorbed per-candidate
		})
	}
	_ = g.Wait() // SetLimit only; never errors (best-effort phase)

	// Merge results on single goroutine - safe, no races.
	var spikes []MetricSpike
	for _, r := range results {
		diags.MetricsQueried += r.queries
		if r.spike != nil {
			spikes = append(spikes, *r.spike)
		}
	}
	return spikes
}

func computeFailureSpikes(ctx context.Context, prom *promclient.Client, service string, metricNames []string, start, end time.Time, diags *investigate.Diagnostics) []MetricSpike {
	return computeSpikes(ctx, prom, service, discoverFailureMetrics(metricNames), start, end, diags, analyzeFailureCandidate)
}
func computeAnomalyScore(ctx context.Context, prom *promclient.Client, service string, metricNames []string, start, end time.Time, diags *investigate.Diagnostics) (float64, []MetricSpike) {
	fail := computeFailureSpikes(ctx, prom, service, metricNames, start, end, diags)
	lat := computeLatencySpikes(ctx, prom, service, metricNames, start, end, diags)
	sat := computeSaturationSpikes(ctx, prom, service, metricNames, start, end, diags)

	all := slices.Concat(fail, lat, sat)
	if len(all) == 0 {
		// Nothing discovered — fall back to legacy http_requests_total path.
		return computeAnomalyScoreLegacy(ctx, prom, service, start, end, diags), nil
	}
	top := rankSpikes(all, 5)
	return top[0].Score, top
}

// computeAnomalyScoreLegacy queries Prometheus for the error-rate ratio between
// the investigation window and a baseline (same duration, 1h earlier) using the
// hardcoded http_requests_total metric.
// Used as fallback when no failure/latency/saturation spikes are discovered.
func computeAnomalyScoreLegacy(ctx context.Context, prom *promclient.Client, service string, start, end time.Time, diags *investigate.Diagnostics) float64 {
	windowDur := end.Sub(start)
	baselineEnd := start.Add(-1 * time.Hour)
	baselineStart := baselineEnd.Add(-windowDur)

	errMetricQuery := fmt.Sprintf(
		`sum(rate(http_requests_total{service=%q,code=~"5..|4.."}[1m]))`,
		service)

	windowSeries, werr := prom.QueryRange(ctx, errMetricQuery, start, end, 60*time.Second)
	baseSeries, berr := prom.QueryRange(ctx, errMetricQuery, baselineStart, baselineEnd, 60*time.Second)
	diags.MetricsQueried += 2

	if werr != nil {
		diags.Warnings = append(diags.Warnings, fmt.Sprintf("prom window: %v", werr))
	}
	if berr != nil {
		diags.Warnings = append(diags.Warnings, fmt.Sprintf("prom baseline: %v", berr))
	}

	if werr != nil || berr != nil {
		return scoreDefault
	}

	wMax := maxSampleValue(windowSeries)
	bMax := maxSampleValue(baseSeries)
	if bMax > 0 {
		ratio := wMax / bMax
		switch {
		case ratio > ratioCritical:
			return scoreCritical
		case ratio > ratioElevated:
			return scoreElevated
		case ratio > ratioMild:
			return scoreMild
		default:
			return scoreNominal
		}
	} else if wMax > 0 {
		// Baseline empty but window has errors — modest anomaly.
		return scoreBaselineEmpty
	}
	return scoreDefault
}

// maxSampleValue returns the maximum sample value across all series in a
// Prometheus matrix response. Returns 0 if the response is empty or all
// values fail to parse.
func maxSampleValue(resp *promclient.QueryRangeResponse) float64 {
	if resp == nil {
		return 0
	}
	var max float64
	for _, series := range resp.Data.Result {
		for _, v := range series.Values {
			if len(v) < 2 {
				continue
			}
			s, ok := v[1].(string)
			if !ok {
				continue
			}
			f, err := strconv.ParseFloat(s, 64)
			if err != nil {
				continue
			}
			if f > max {
				max = f
			}
		}
	}
	return max
}

// latencyHistogramRegex matches Prometheus histogram bucket metrics following
// common latency naming conventions. _ms_bucket without a semantic prefix
// (like _duration_) is intentionally excluded — it is too broad and matches
// non-histogram counters (e.g. request_parse_ms_bucket).
var latencyHistogramRegex = regexp.MustCompile(`(?i)(_seconds_bucket|_duration_seconds_bucket|_duration_ms_bucket|_latency_seconds_bucket)$`)

// discoverLatencyHistograms is a pure filter — it returns names from metricNames
// that represent histogram buckets with latency semantics.
// No network call; callers must pass the result of promclient.Client.MetricNames.
func discoverLatencyHistograms(metricNames []string) []string {
	var out []string
	for _, name := range metricNames {
		if latencyHistogramRegex.MatchString(name) {
			out = append(out, name)
		}
	}
	return out
}

// bucketLatencyRatio buckets a window/baseline ratio for latency spikes.
// Returns (score, ratio). Returns (0, ratio) when ratio is not anomalous.
func bucketLatencyRatio(ratio float64) (float64, float64) {
	switch {
	case ratio > ratioLatencyCritical:
		return scoreLatencyCritical, ratio
	case ratio > ratioLatencyElevated:
		return scoreLatencyElevated, ratio
	default:
		return 0, ratio
	}
}

// analyzeLatencyCandidate queries window + baseline for one histogram bucket and
// returns a latency spike if anomalous, along with the count of attempted Prometheus RPCs.
// Thread-safe: no shared state.
func analyzeLatencyCandidate(ctx context.Context, prom *promclient.Client, service, bucketName string, start, end, baselineStart, baselineEnd time.Time) (*MetricSpike, int) {
	baseName := strings.TrimSuffix(bucketName, "_bucket")
	query := fmt.Sprintf(
		`histogram_quantile(0.99, sum by (le) (rate(%s{service=%q}[1m])))`,
		bucketName, service)
	windowSeries, usedJob, wq, werr := queryRangeWithJobFallback(ctx, prom, query, service, start, end)
	if werr != nil {
		return nil, wq
	}
	baseSeries, _, bq, berr := queryRangeWithJobFallback(ctx, prom, query, service, baselineStart, baselineEnd)
	queries := wq + bq
	if berr != nil {
		return nil, queries
	}
	wMax := maxSampleValue(windowSeries)
	bMax := maxSampleValue(baseSeries)
	if bMax <= 0 {
		return nil, queries // no baseline data — skip
	}
	ratio := wMax / bMax
	score, _ := bucketLatencyRatio(ratio)
	if score == 0 {
		return nil, queries
	}
	// Display label uses %s (no quotes) because formatInvestigationResult renders
	// it as an XML attribute with %q; PromQL queries above use %q for label values.
	labelStr := fmt.Sprintf(`{service=%s}`, service)
	if usedJob {
		labelStr = fmt.Sprintf(`{job=%s}`, service)
	}
	return &MetricSpike{
		Kind:       "latency",
		MetricName: baseName + "_p99",
		Labels:     labelStr,
		Ratio:      ratio,
		Score:      score,
	}, queries
}

// computeLatencySpikes queries histogram_quantile(0.99, ...) for discovered
// histogram metrics, comparing window vs baseline.
// Candidates run in parallel (bounded by promQueryConcurrency); per-candidate
// window + baseline queries run serially within each goroutine.
// metricNames is the pre-fetched list from promclient.Client.MetricNames.
func computeLatencySpikes(ctx context.Context, prom *promclient.Client, service string, metricNames []string, start, end time.Time, diags *investigate.Diagnostics) []MetricSpike {
	return computeSpikes(ctx, prom, service, discoverLatencyHistograms(metricNames), start, end, diags, analyzeLatencyCandidate)
}

// saturationQueueRegex matches gauge metrics representing queue depths and
// active worker counts — key saturation indicators.
var saturationQueueRegex = regexp.MustCompile(`(?i)(_active|_pending|_queue_size|_queue_depth)$`)

// sentinelSaturationMetrics are queried directly without discovery — they are
// universally present in Go services instrumented with the default Prometheus
// collector.
var sentinelSaturationMetrics = []string{
	"process_resident_memory_bytes",
	"go_goroutines",
}

// discoverQueueMetrics is a pure filter — it returns names from metricNames
// that match queue / active-worker saturation patterns.
// No network call; callers must pass the result of promclient.Client.MetricNames.
func discoverQueueMetrics(metricNames []string) []string {
	var out []string
	for _, name := range metricNames {
		if saturationQueueRegex.MatchString(name) {
			out = append(out, name)
		}
	}
	return out
}

// bucketSaturationRatio buckets a window/baseline max ratio for saturation
// spikes. Returns (score, ratio). Returns (0, ratio) when not anomalous.
func bucketSaturationRatio(ratio float64) (float64, float64) {
	switch {
	case ratio > ratioSatCritical:
		return scoreCritical, ratio
	case ratio > ratioSatElevated:
		return scoreElevated, ratio
	case ratio > ratioSatMild:
		return scoreMild, ratio
	default:
		return 0, ratio
	}
}

// queryMaxValueWithFallback returns the max sample value for a query,
// trying service= label first and falling back to job= if empty.
// Returns (maxValue, usedJobLabel, queries).
// queries counts attempted Prometheus RPCs (success + error), matching the
// pre-parallelization accounting convention.
func queryMaxValueWithFallback(ctx context.Context, prom *promclient.Client, query, service string, start, end time.Time) (float64, bool, int) {
	queries := 0
	series, err := prom.QueryRange(ctx, query, start, end, 60*time.Second)
	queries++
	if err != nil {
		return 0, false, queries
	}
	if !isEmpty(series) {
		return maxSampleValue(series), false, queries
	}
	// Fall back to job= label.
	jobQuery := strings.ReplaceAll(query, fmt.Sprintf("service=%q", service), fmt.Sprintf("job=%q", service))
	jobSeries, jerr := prom.QueryRange(ctx, jobQuery, start, end, 60*time.Second)
	queries++
	if jerr != nil {
		return 0, false, queries
	}
	return maxSampleValue(jobSeries), true, queries
}

// analyzeSaturationCandidate queries window + baseline for one saturation metric
// and returns a spike if anomalous, along with the count of attempted Prometheus RPCs.
// Thread-safe: no shared state.
func analyzeSaturationCandidate(ctx context.Context, prom *promclient.Client, service, metricName string, start, end, baselineStart, baselineEnd time.Time) (*MetricSpike, int) {
	query := fmt.Sprintf(`%s{service=%q}`, metricName, service)
	wMax, usedJob, wq := queryMaxValueWithFallback(ctx, prom, query, service, start, end)
	bMax, _, bq := queryMaxValueWithFallback(ctx, prom, query, service, baselineStart, baselineEnd)
	queries := wq + bq
	if bMax <= 0 {
		return nil, queries
	}
	ratio := wMax / bMax
	score, _ := bucketSaturationRatio(ratio)
	if score == 0 {
		return nil, queries
	}
	// Display label uses %s (no quotes) because formatInvestigationResult renders
	// it as an XML attribute with %q; PromQL queries above use %q for label values.
	labelStr := fmt.Sprintf(`{service=%s}`, service)
	if usedJob {
		labelStr = fmt.Sprintf(`{job=%s}`, service)
	}
	return &MetricSpike{
		Kind:       "saturation",
		MetricName: metricName,
		Labels:     labelStr,
		Ratio:      ratio,
		Score:      score,
	}, queries
}

// computeSaturationSpikes detects memory / goroutine / queue saturation by
// comparing window max vs baseline max. Sentinels are queried unconditionally;
// wildcard queue metrics are filtered from metricNames.
// Both the sentinel loop and the wildcard loop run in parallel (bounded by
// promQueryConcurrency) using errgroup.
// metricNames is the pre-fetched list from promclient.Client.MetricNames.
func computeSaturationSpikes(ctx context.Context, prom *promclient.Client, service string, metricNames []string, start, end time.Time, diags *investigate.Diagnostics) []MetricSpike {
	// Saturation combines always-present sentinels + discovered queue/active
	// gauges into one candidate list, then shares the parallel fan-out.
	candidates := make([]string, 0, len(sentinelSaturationMetrics)+len(metricNames))
	candidates = append(candidates, sentinelSaturationMetrics...)
	candidates = append(candidates, discoverQueueMetrics(metricNames)...)
	return computeSpikes(ctx, prom, service, candidates, start, end, diags, analyzeSaturationCandidate)
}

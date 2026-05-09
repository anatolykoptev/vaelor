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

	"github.com/anatolykoptev/go-code/internal/investigate"
	"github.com/anatolykoptev/go-code/internal/promclient"
)

// MetricSpike is re-exported from investigate for use within this package.
type MetricSpike = investigate.MetricSpike

// failureMetricRegex matches metric names that strongly suggest a failure
// counter. All alternatives require a _total suffix to avoid matching gauges
// (e.g. auth_outcome is a gauge; signaling_call_outcome_total is a counter).
// Hand-curated; expand as new counter conventions appear.
var failureMetricRegex = regexp.MustCompile(`(?i)(_failed_total|_failures_total|_failure_total|_errors?_total|_dropped_total|_outcome_total)$`)

// discoverFailureMetrics returns Prometheus metric names matching common
// failure-counter naming conventions. Service-name filtering happens at
// query time (we don't filter by service here — metric NAME is global).
func discoverFailureMetrics(ctx context.Context, prom *promclient.Client, _ string) ([]string, error) {
	type resp struct {
		Status string   `json:"status"`
		Data   []string `json:"data"`
	}
	var r resp
	if err := prom.GetJSON(ctx, "/api/v1/label/__name__/values", &r); err != nil {
		return nil, err
	}
	if r.Status != "success" {
		return nil, fmt.Errorf("label values status %q", r.Status)
	}
	var out []string
	for _, name := range r.Data {
		if failureMetricRegex.MatchString(name) {
			out = append(out, name)
		}
	}
	return out, nil
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
// for service=%q. Returns (series, usedJobLabel, error).
func queryRangeWithJobFallback(ctx context.Context, prom *promclient.Client, query, service string, start, end time.Time) (*promclient.QueryRangeResponse, bool, error) {
	series, err := prom.QueryRange(ctx, query, start, end, 60*time.Second)
	if err != nil {
		return nil, false, err
	}
	if !isEmpty(series) {
		return series, false, nil
	}
	// Fall back to job= label.
	jobQuery := strings.ReplaceAll(query, fmt.Sprintf("service=%q", service), fmt.Sprintf("job=%q", service))
	jobSeries, jerr := prom.QueryRange(ctx, jobQuery, start, end, 60*time.Second)
	if jerr != nil {
		return series, false, nil // original empty result is fine; ignore fallback error
	}
	return jobSeries, true, nil
}

// computeFailureSpikes returns MetricSpike entries for discovered failure-counter
// metrics showing anomalous window vs baseline ratios.
func computeFailureSpikes(ctx context.Context, prom *promclient.Client, service string, start, end time.Time, diags *investigate.Diagnostics) []MetricSpike {
	candidates, err := discoverFailureMetrics(ctx, prom, service)
	if err != nil {
		diags.Warnings = append(diags.Warnings, fmt.Sprintf("discover failure metrics: %v", err))
		return nil
	}
	if len(candidates) == 0 {
		return nil
	}

	windowDur := end.Sub(start)
	baselineEnd := start.Add(-1 * time.Hour)
	baselineStart := baselineEnd.Add(-windowDur)

	var spikes []MetricSpike
	for _, name := range candidates {
		query := fmt.Sprintf(`sum(rate(%s{service=%q}[1m]))`, name, service)
		windowSeries, usedJob, werr := queryRangeWithJobFallback(ctx, prom, query, service, start, end)
		if werr != nil {
			continue
		}
		baseSeries, _, berr := queryRangeWithJobFallback(ctx, prom, query, service, baselineStart, baselineEnd)
		if berr != nil {
			continue
		}
		score, ratio := bucketRatio(maxSampleValue(windowSeries), maxSampleValue(baseSeries))
		if score <= scoreNominal {
			continue // not anomalous
		}
		labelStr := fmt.Sprintf(`{service=%q}`, service)
		if usedJob {
			labelStr = fmt.Sprintf(`{job=%q}`, service)
		}
		spikes = append(spikes, MetricSpike{
			Kind:       "failure",
			MetricName: name,
			Labels:     labelStr,
			Ratio:      ratio,
			Score:      score,
		})
	}
	diags.MetricsQueried += 2 * len(candidates)
	return spikes
}

// computeAnomalyScore merges failure, latency, and saturation spikes,
// returns the top score and top-5 spikes across all kinds.
// Falls back to computeAnomalyScoreLegacy only when ALL three axes are empty.
func computeAnomalyScore(ctx context.Context, prom *promclient.Client, service string, start, end time.Time, diags *investigate.Diagnostics) (float64, []MetricSpike) {
	fail := computeFailureSpikes(ctx, prom, service, start, end, diags)
	lat := computeLatencySpikes(ctx, prom, service, start, end, diags)
	sat := computeSaturationSpikes(ctx, prom, service, start, end, diags)

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
// Used as fallback when discoverFailureMetrics returns 0 results.
func computeAnomalyScoreLegacy(ctx context.Context, prom *promclient.Client, service string, start, end time.Time, diags *investigate.Diagnostics) float64 {
	windowDur := end.Sub(start)
	baselineEnd := start.Add(-1 * time.Hour)
	baselineStart := baselineEnd.Add(-windowDur)

	errMetricQuery := fmt.Sprintf(
		`sum(rate(http_requests_total{service=%q,code=~"5..|4.."}[1m]))`,
		service)

	windowSeries, werr := prom.QueryRange(ctx, errMetricQuery, start, end, 60*time.Second)
	baseSeries, berr := prom.QueryRange(ctx, errMetricQuery, baselineStart, baselineEnd, 60*time.Second)
	diags.MetricsQueried = 2

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

// discoverLatencyHistograms returns Prometheus metric names that represent
// histogram buckets with latency semantics.
func discoverLatencyHistograms(ctx context.Context, prom *promclient.Client) ([]string, error) {
	type resp struct {
		Status string   `json:"status"`
		Data   []string `json:"data"`
	}
	var r resp
	if err := prom.GetJSON(ctx, "/api/v1/label/__name__/values", &r); err != nil {
		return nil, err
	}
	if r.Status != "success" {
		return nil, fmt.Errorf("label values status %q", r.Status)
	}
	var out []string
	for _, name := range r.Data {
		if latencyHistogramRegex.MatchString(name) {
			out = append(out, name)
		}
	}
	return out, nil
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

// computeLatencySpikes queries histogram_quantile(0.99, ...) for discovered
// histogram metrics, comparing window vs baseline.
func computeLatencySpikes(ctx context.Context, prom *promclient.Client, service string, start, end time.Time, diags *investigate.Diagnostics) []MetricSpike {
	candidates, err := discoverLatencyHistograms(ctx, prom)
	if err != nil || len(candidates) == 0 {
		return nil
	}

	windowDur := end.Sub(start)
	baselineEnd := start.Add(-1 * time.Hour)
	baselineStart := baselineEnd.Add(-windowDur)

	var spikes []MetricSpike
	for _, bucketName := range candidates {
		// Derive base name by stripping _bucket suffix.
		baseName := strings.TrimSuffix(bucketName, "_bucket")
		query := fmt.Sprintf(
			`histogram_quantile(0.99, sum by (le) (rate(%s{service=%q}[1m])))`,
			bucketName, service)

		windowSeries, usedJob, werr := queryRangeWithJobFallback(ctx, prom, query, service, start, end)
		if werr != nil {
			continue
		}
		baseSeries, _, berr := queryRangeWithJobFallback(ctx, prom, query, service, baselineStart, baselineEnd)
		if berr != nil {
			continue
		}

		wMax := maxSampleValue(windowSeries)
		bMax := maxSampleValue(baseSeries)
		if bMax <= 0 {
			continue // no baseline data — skip
		}
		ratio := wMax / bMax
		score, _ := bucketLatencyRatio(ratio)
		if score == 0 {
			continue
		}
		labelStr := fmt.Sprintf(`{service=%q}`, service)
		if usedJob {
			labelStr = fmt.Sprintf(`{job=%q}`, service)
		}
		spikes = append(spikes, MetricSpike{
			Kind:       "latency",
			MetricName: baseName + "_p99",
			Labels:     labelStr,
			Ratio:      ratio,
			Score:      score,
		})
	}
	diags.MetricsQueried += 2 * len(candidates)
	return spikes
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

// discoverQueueMetrics returns Prometheus metric names matching common
// queue / active-worker saturation patterns.
func discoverQueueMetrics(ctx context.Context, prom *promclient.Client) ([]string, error) {
	type resp struct {
		Status string   `json:"status"`
		Data   []string `json:"data"`
	}
	var r resp
	if err := prom.GetJSON(ctx, "/api/v1/label/__name__/values", &r); err != nil {
		return nil, err
	}
	if r.Status != "success" {
		return nil, fmt.Errorf("label values status %q", r.Status)
	}
	var out []string
	for _, name := range r.Data {
		if saturationQueueRegex.MatchString(name) {
			out = append(out, name)
		}
	}
	return out, nil
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
// Returns (maxValue, usedJobLabel).
func queryMaxValueWithFallback(ctx context.Context, prom *promclient.Client, query, service string, start, end time.Time) (float64, bool) {
	series, err := prom.QueryRange(ctx, query, start, end, 60*time.Second)
	if err != nil {
		return 0, false
	}
	if !isEmpty(series) {
		return maxSampleValue(series), false
	}
	// Fall back to job= label.
	jobQuery := strings.ReplaceAll(query, fmt.Sprintf("service=%q", service), fmt.Sprintf("job=%q", service))
	jobSeries, jerr := prom.QueryRange(ctx, jobQuery, start, end, 60*time.Second)
	if jerr != nil {
		return 0, false
	}
	return maxSampleValue(jobSeries), true
}

// computeSaturationSpikes detects memory / goroutine / queue saturation by
// comparing window max vs baseline max. Sentinels are queried unconditionally;
// wildcard queue metrics are discovered via Prometheus label API.
func computeSaturationSpikes(ctx context.Context, prom *promclient.Client, service string, start, end time.Time, diags *investigate.Diagnostics) []MetricSpike {
	windowDur := end.Sub(start)
	baselineEnd := start.Add(-1 * time.Hour)
	baselineStart := baselineEnd.Add(-windowDur)

	var spikes []MetricSpike

	// Sentinels — always queried.
	for _, metricName := range sentinelSaturationMetrics {
		query := fmt.Sprintf(`%s{service=%q}`, metricName, service)
		wMax, usedJob := queryMaxValueWithFallback(ctx, prom, query, service, start, end)
		bMax, _ := queryMaxValueWithFallback(ctx, prom, query, service, baselineStart, baselineEnd)
		if bMax <= 0 {
			continue
		}
		ratio := wMax / bMax
		score, _ := bucketSaturationRatio(ratio)
		if score == 0 {
			continue
		}
		labelStr := fmt.Sprintf(`{service=%q}`, service)
		if usedJob {
			labelStr = fmt.Sprintf(`{job=%q}`, service)
		}
		spikes = append(spikes, MetricSpike{
			Kind:       "saturation",
			MetricName: metricName,
			Labels:     labelStr,
			Ratio:      ratio,
			Score:      score,
		})
	}
	diags.MetricsQueried += 2 * len(sentinelSaturationMetrics)

	// Wildcard queue/active metrics.
	wildcards, err := discoverQueueMetrics(ctx, prom)
	if err != nil {
		wildcards = nil
	}
	for _, metricName := range wildcards {
		query := fmt.Sprintf(`%s{service=%q}`, metricName, service)
		wMax, usedJob := queryMaxValueWithFallback(ctx, prom, query, service, start, end)
		bMax, _ := queryMaxValueWithFallback(ctx, prom, query, service, baselineStart, baselineEnd)
		if bMax <= 0 {
			continue
		}
		ratio := wMax / bMax
		score, _ := bucketSaturationRatio(ratio)
		if score == 0 {
			continue
		}
		labelStr := fmt.Sprintf(`{service=%q}`, service)
		if usedJob {
			labelStr = fmt.Sprintf(`{job=%q}`, service)
		}
		spikes = append(spikes, MetricSpike{
			Kind:       "saturation",
			MetricName: metricName,
			Labels:     labelStr,
			Ratio:      ratio,
			Score:      score,
		})
	}
	diags.MetricsQueried += 2 * len(wildcards)

	return spikes
}

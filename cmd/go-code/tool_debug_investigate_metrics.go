// cmd/go-code/tool_debug_investigate_metrics.go
package main

import (
	"context"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"time"

	"github.com/anatolykoptev/go-code/internal/investigate"
	"github.com/anatolykoptev/go-code/internal/promclient"
)

// MetricSpike is re-exported from investigate for use within this package.
type MetricSpike = investigate.MetricSpike

// failureMetricRegex matches metric names that strongly suggest a failure
// counter. Patterns: *_failed_total, *_error*, *_dropped_total, *_failure*,
// *_outcome*. Hand-curated; expand as new conventions appear.
var failureMetricRegex = regexp.MustCompile(`(?i)(_failed_total|_failures?_total|_errors?_total|_dropped_total|_failure(_|$)|_outcome($|_total))`)

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

// computeAnomalyScore queries Prometheus for discovered failure metrics,
// comparing the investigation window against a baseline 1h earlier.
// Returns the top spike score and the full spike slice.
// Falls back to computeAnomalyScoreLegacy when no failure metrics are discovered.
func computeAnomalyScore(ctx context.Context, prom *promclient.Client, service string, start, end time.Time, diags *investigate.Diagnostics) (float64, []MetricSpike) {
	candidates, err := discoverFailureMetrics(ctx, prom, service)
	if err != nil {
		diags.Warnings = append(diags.Warnings, fmt.Sprintf("discover failure metrics: %v", err))
		return scoreDefault, nil
	}
	if len(candidates) == 0 {
		// nothing matched — fall back to legacy http_requests_total path.
		return computeAnomalyScoreLegacy(ctx, prom, service, start, end, diags), nil
	}

	windowDur := end.Sub(start)
	baselineEnd := start.Add(-1 * time.Hour)
	baselineStart := baselineEnd.Add(-windowDur)

	var spikes []MetricSpike
	for _, name := range candidates {
		// Query with service label filter; if metric has no service label,
		// the result will be empty and we silently skip.
		query := fmt.Sprintf(`sum(rate(%s{service=%q}[1m]))`, name, service)
		windowSeries, werr := prom.QueryRange(ctx, query, start, end, 60*time.Second)
		if werr != nil {
			continue
		}
		baseSeries, berr := prom.QueryRange(ctx, query, baselineStart, baselineEnd, 60*time.Second)
		if berr != nil {
			continue
		}
		score, ratio := bucketRatio(maxSampleValue(windowSeries), maxSampleValue(baseSeries))
		if score <= scoreNominal {
			continue // not anomalous
		}
		spikes = append(spikes, MetricSpike{
			MetricName: name,
			Labels:     fmt.Sprintf(`{service=%q}`, service),
			Ratio:      ratio,
			Score:      score,
		})
	}
	diags.MetricsQueried += 2 * len(candidates)

	if len(spikes) == 0 {
		return scoreDefault, nil
	}
	top := rankSpikes(spikes, 5)
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

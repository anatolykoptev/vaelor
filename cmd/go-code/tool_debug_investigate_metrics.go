// cmd/go-code/tool_debug_investigate_metrics.go
package main

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"time"

	"github.com/anatolykoptev/go-code/internal/investigate"
	"github.com/anatolykoptev/go-code/internal/promclient"
)

// MetricSpike captures a single failure-metric showing anomaly above baseline.
type MetricSpike struct {
	MetricName string  // full Prometheus metric name (e.g. signaling_call_outcome_total)
	Labels     string  // label-set rendered for human reading: {outcome="failed"}
	Ratio      float64 // window_max / baseline_max
	Score      float64 // bucketed anomaly score 0..1
}

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

// computeAnomalyScore queries Prometheus for the error-rate ratio between the
// investigation window and a baseline (same duration, 1h earlier) using the
// hardcoded http_requests_total metric.
// It is the fallback path for Phase 4; auto-discovery is wired in Phase A3.
func computeAnomalyScore(ctx context.Context, prom *promclient.Client, service string, start, end time.Time, diags *investigate.Diagnostics) float64 {
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

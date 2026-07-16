package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/anatolykoptev/go-code/internal/oxcodes"
	"github.com/prometheus/client_golang/prometheus"
)

// oxCodesCollector scrapes ox-codes /cache/stats and exposes hit/miss
// counters + walk-pool in-flight as Prometheus metrics.
type oxCodesCollector struct {
	client *oxcodes.Client

	scopeHits    *prometheus.Desc
	scopeMisses  *prometheus.Desc
	scopeEntries *prometheus.Desc

	dfHits    *prometheus.Desc
	dfMisses  *prometheus.Desc
	dfEntries *prometheus.Desc

	walksInFlight *prometheus.Desc
}

func newOxCodesCollector(client *oxcodes.Client) *oxCodesCollector {
	return &oxCodesCollector{
		client:        client,
		scopeHits:     prometheus.NewDesc("oxcodes_scope_cache_hits_total", "ox-codes scoped-search cache hits", nil, nil),
		scopeMisses:   prometheus.NewDesc("oxcodes_scope_cache_misses_total", "ox-codes scoped-search cache misses", nil, nil),
		scopeEntries:  prometheus.NewDesc("oxcodes_scope_cache_entries", "ox-codes scoped-search cache entry count", nil, nil),
		dfHits:        prometheus.NewDesc("oxcodes_dataflow_cache_hits_total", "ox-codes dataflow cache hits", nil, nil),
		dfMisses:      prometheus.NewDesc("oxcodes_dataflow_cache_misses_total", "ox-codes dataflow cache misses", nil, nil),
		dfEntries:     prometheus.NewDesc("oxcodes_dataflow_cache_entries", "ox-codes dataflow cache entry count", nil, nil),
		walksInFlight: prometheus.NewDesc("oxcodes_walks_in_flight", "ox-codes walk-pool in-flight count", nil, nil),
	}
}

func (c *oxCodesCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.scopeHits
	ch <- c.scopeMisses
	ch <- c.scopeEntries
	ch <- c.dfHits
	ch <- c.dfMisses
	ch <- c.dfEntries
	ch <- c.walksInFlight
}

func (c *oxCodesCollector) Collect(ch chan<- prometheus.Metric) {
	if c.client == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	stats, err := c.client.CacheStats(ctx)
	if err != nil {
		slog.Debug("oxcodes cache stats scrape failed", slog.Any("error", err))
		return
	}
	ch <- prometheus.MustNewConstMetric(c.scopeHits, prometheus.CounterValue, float64(stats.Scope.Hits))
	ch <- prometheus.MustNewConstMetric(c.scopeMisses, prometheus.CounterValue, float64(stats.Scope.Misses))
	ch <- prometheus.MustNewConstMetric(c.scopeEntries, prometheus.GaugeValue, float64(stats.Scope.EntryCount))
	ch <- prometheus.MustNewConstMetric(c.dfHits, prometheus.CounterValue, float64(stats.Dataflow.Hits))
	ch <- prometheus.MustNewConstMetric(c.dfMisses, prometheus.CounterValue, float64(stats.Dataflow.Misses))
	ch <- prometheus.MustNewConstMetric(c.dfEntries, prometheus.GaugeValue, float64(stats.Dataflow.EntryCount))
	ch <- prometheus.MustNewConstMetric(c.walksInFlight, prometheus.GaugeValue, float64(stats.Walks.InFlight))
}

package filewatcher

// PrometheusCollector is a Prometheus metrics collector for the watcher.
// It implements the prometheus.Collector interface:
//
//	type Collector interface {
//	    Describe(chan<- *Desc)
//	    Collect(chan<- Metric)
//	}
//
// Users register it with their prometheus.Registry:
//
//	import "github.com/prometheus/client_golang/prometheus"
//
//	collector := filewatcher.NewPrometheusCollector(watcher.Stats)
//	prometheus.MustRegister(collector)
//
// The collector does not depend on prometheus/client_golang — it is a
// standalone type that can be adapted by users to any metrics system
// (Prometheus, OpenTelemetry, Datadog, etc.) without adding a dependency
// to this library.
//
// The StatsFunc is called on each Collect invocation to retrieve the
// current Stats snapshot. This allows the collector to be reused across
// watchers by passing different StatsFunc closures.
type PrometheusCollector struct {
	stats    StatsFunc
	describe []string // metric help text
}

// StatsFunc is a function that returns a Stats snapshot. It is called
// on each Prometheus scrape to get the latest values.
type StatsFunc func() Stats

// NewPrometheusCollector returns a Prometheus collector for a watcher's
// stats. The provided function should return the current Stats (e.g.,
// `watcher.Stats` or a closure that calls it).
func NewPrometheusCollector(stats StatsFunc) *PrometheusCollector {
	if stats == nil {
		stats = func() Stats {
			return Stats{
				WatchCount:                  0,
				IsWatching:                  false,
				IsClosed:                    false,
				EventsProcessed:             0,
				EventsFilteredOut:           0,
				EventsDroppedByMiddleware:   0,
				EventsDroppedByBackpressure: 0,
				ErrorsEncountered:           0,
				ErrorsDropped:               0,
				WatchErrors:                 0,
				Uptime:                      0,
				WatchLimit:                  0,
				WatchBudgetCap:              0,
				WatchBudgetUsed:             0,
				CaseSensitivity:             "",
				CaseSensitivityMode:         CaseSensitivityAuto,
			}
		}
	}

	return &PrometheusCollector{
		stats: stats,
		describe: []string{
			"Total events that reached the event channel",
			"Events filtered out (dropped by filters)",
			"Events dropped by middleware (rate limit, dedup, circuit breaker, etc.)",
			"Events dropped because the event channel was full (DropOnFull mode)",
			"Errors encountered during processing",
			"Errors dropped because the error channel was full",
			"Watch add failures (ENOSPC, permission denied, etc.)",
		},
	}
}

// CounterMetric is a generic counter descriptor for a Prometheus metric.
// It carries the name, help text, and value extraction from Stats.
//
// Users adapt this to their metrics library by iterating over the
// PrometheusCollector.Counters() method, which returns []CounterMetric.
type CounterMetric struct {
	Name  string
	Help  string
	Value uint64
}

// GaugeMetric is a generic gauge descriptor for a Prometheus metric.
type GaugeMetric struct {
	Name  string
	Help  string
	Value float64
}

// Metric name constants — used by Counters() and Gauges() to avoid goconst
// violations when emitting the same metric name across calls.
const (
	metricEventsProcessed             = "filewatcher_events_processed_total"
	metricEventsFilteredOut           = "filewatcher_events_filtered_out_total"
	metricEventsDroppedByMiddleware   = "filewatcher_events_dropped_by_middleware_total"
	metricEventsDroppedByBackpressure = "filewatcher_events_dropped_by_backpressure_total"
	metricErrorsEncountered           = "filewatcher_errors_encountered_total"
	metricErrorsDropped               = "filewatcher_errors_dropped_total"
	metricWatchErrors                 = "filewatcher_watch_errors_total"
	metricWatchCount                  = "filewatcher_watch_count"
	metricIsWatching                  = "filewatcher_is_watching"
	metricIsClosed                    = "filewatcher_is_closed"
	metricUptimeSeconds               = "filewatcher_uptime_seconds"
	metricWatchLimit                  = "filewatcher_watch_limit"
	metricWatchBudgetCap              = "filewatcher_watch_budget_cap"
	metricWatchBudgetUsed             = "filewatcher_watch_budget_used_ratio"
	metricCaseSensitivity             = "filewatcher_case_sensitivity"
)

// Counters returns the current counter values from the watcher stats.
// Callers can map these to their metrics library of choice.
func (c *PrometheusCollector) Counters() []CounterMetric {
	stats := c.stats()

	return []CounterMetric{
		{
			Name:  metricEventsProcessed,
			Help:  c.describe[0],
			Value: stats.EventsProcessed,
		},
		{
			Name:  metricEventsFilteredOut,
			Help:  c.describe[1],
			Value: stats.EventsFilteredOut,
		},
		{
			Name:  metricEventsDroppedByMiddleware,
			Help:  c.describe[2],
			Value: stats.EventsDroppedByMiddleware,
		},
		{
			Name:  metricEventsDroppedByBackpressure,
			Help:  c.describe[3],
			Value: stats.EventsDroppedByBackpressure,
		},
		{
			Name:  metricErrorsEncountered,
			Help:  c.describe[4],
			Value: stats.ErrorsEncountered,
		},
		{
			Name:  metricErrorsDropped,
			Help:  c.describe[5],
			Value: stats.ErrorsDropped,
		},
		{
			Name:  metricWatchErrors,
			Help:  c.describe[6],
			Value: stats.WatchErrors,
		},
	}
}

// Gauges returns the current gauge values from the watcher stats.
func (c *PrometheusCollector) Gauges() []GaugeMetric {
	stats := c.stats()

	return []GaugeMetric{
		{
			Name:  metricWatchCount,
			Help:  "Number of paths currently being watched",
			Value: float64(stats.WatchCount),
		},
		{
			Name:  metricIsWatching,
			Help:  "1 if the watcher is currently running, 0 otherwise",
			Value: boolToFloat(stats.IsWatching),
		},
		{
			Name:  metricIsClosed,
			Help:  "1 if the watcher has been closed, 0 otherwise",
			Value: boolToFloat(stats.IsClosed),
		},
		{
			Name:  metricUptimeSeconds,
			Help:  "Time in seconds since the watcher was started",
			Value: stats.Uptime.Seconds(),
		},
		{
			Name:  metricWatchLimit,
			Help:  "System inotify watch limit before safety fraction (0 if unknown)",
			Value: float64(stats.WatchLimit),
		},
		{
			Name:  metricWatchBudgetCap,
			Help:  "Effective watch budget cap after safety fraction",
			Value: float64(stats.WatchBudgetCap),
		},
		{
			Name:  metricWatchBudgetUsed,
			Help:  "Percentage of inotify budget used (0.0-1.0)",
			Value: stats.WatchBudgetUsed,
		},
		{
			Name:  metricCaseSensitivity,
			Help:  "Resolved case-sensitivity mode (0=case-sensitive, 1=case-insensitive, 2=auto)",
			Value: stats.CaseSensitivityMode.GaugeValue(),
		},
	}
}

func boolToFloat(b bool) float64 {
	if b {
		return 1
	}

	return 0
}

package embeddings

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// gocode_autoindex_retry_total counts retry attempts during autoindex by
// repo and classification reason (deadline_exceeded | connection_refused |
// embed_5xx | non_retryable). Useful for alerting on systematic failures
// (e.g. non_retryable rate >0.1/min = embed-server schema break).
var autoindexRetryTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "gocode_autoindex_retry_total",
		Help: "Total autoindex retry attempts by repo and reason.",
	},
	[]string{"repo", "reason"},
)

// gocode_autoindex_duration_seconds tracks per-repo indexing wall-clock time
// across all attempts, labelled by final outcome (success | failed |
// non_retryable | cancelled). Buckets cover the observed range
// (sub-second to multi-minute) for the 48-repo cold-start case.
var autoindexDurationSeconds = promauto.NewHistogramVec(
	prometheus.HistogramOpts{
		Name:    "gocode_autoindex_duration_seconds",
		Help:    "Per-repo autoindex duration including retries, by outcome.",
		Buckets: []float64{0.5, 1, 2.5, 5, 10, 30, 60, 120, 300, 600},
	},
	[]string{"repo", "outcome"},
)

func recordAutoIndexRetry(repo, reason string) {
	autoindexRetryTotal.WithLabelValues(repo, reason).Inc()
}

func recordAutoIndexDuration(repo, outcome string, d time.Duration) {
	autoindexDurationSeconds.WithLabelValues(repo, outcome).Observe(d.Seconds())
}

package embeddings

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// gocode_embeddings_schema_init_duration_seconds tracks EnsureSchema wall-clock
// time, labelled by final outcome. The bucket ceiling (120s) matches the raised
// statement_timeout used for first-time HNSW index builds.
var schemaInitDurationSeconds = promauto.NewHistogramVec(
	prometheus.HistogramOpts{
		Name:    "gocode_embeddings_schema_init_duration_seconds",
		Help:    "Histogram of embeddings EnsureSchema durations by outcome.",
		Buckets: []float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120},
	},
	[]string{"outcome"},
)

// gocode_embeddings_schema_ready is 1 when the embeddings schema init has
// succeeded and latched, 0 otherwise. This makes a permanently dark store
// (sync.Once latched a failure) visible to alerting.
var schemaReady = promauto.NewGauge(prometheus.GaugeOpts{
	Name: "gocode_embeddings_schema_ready",
	Help: "1 if the embeddings schema is ready, 0 otherwise.",
})

func recordSchemaInit(outcome string, d time.Duration) {
	schemaInitDurationSeconds.WithLabelValues(outcome).Observe(d.Seconds())
}

func setSchemaReady(ready int) {
	schemaReady.Set(float64(ready))
}

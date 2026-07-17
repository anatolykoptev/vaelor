package designmd

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// gocode_designmd_schema_init_duration_seconds tracks designmd EnsureSchema
// wall-clock time, labelled by final outcome.
var schemaInitDurationSeconds = promauto.NewHistogramVec(
	prometheus.HistogramOpts{
		Name:    "gocode_designmd_schema_init_duration_seconds",
		Help:    "Histogram of designmd EnsureSchema durations by outcome.",
		Buckets: []float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120},
	},
	[]string{"outcome"},
)

// gocode_designmd_schema_ready is 1 when the designmd schema init has succeeded
// and latched, 0 otherwise.
var schemaReady = promauto.NewGauge(prometheus.GaugeOpts{
	Name: "gocode_designmd_schema_ready",
	Help: "1 if the designmd schema is ready, 0 otherwise.",
})

func recordSchemaInit(outcome string, d time.Duration) {
	schemaInitDurationSeconds.WithLabelValues(outcome).Observe(d.Seconds())
}

func setSchemaReady(ready int) {
	schemaReady.Set(float64(ready))
}

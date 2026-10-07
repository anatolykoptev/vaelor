package jeff

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Metrics follow the embed_* / rerank_* convention: one counter per
// request outcome. Labels: ok | transport_error | http_error | decode_error.
var jeffRequestsTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Namespace: "jeff",
		Name:      "requests_total",
		Help:      "jeff /v1/systemone requests partitioned by outcome.",
	},
	[]string{"outcome"},
)

package cache

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// cacheValidatorEvictTotal counts L1 entries evicted because the caller's
// Validator hook (passed to GetIfValid) reported them stale.
//
// reason is a free-form, low-cardinality label: the package emits "stale" by
// default. Future per-caller reasons (e.g. "modtime_changed", "etag_drift")
// can be added by exposing a recordValidatorEvict variant if a real need
// surfaces — keep cardinality bounded by validating reason against a known set.
var cacheValidatorEvictTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "cache_validator_evict_total",
		Help: "Total L1 entries evicted because GetIfValid's Validator returned false.",
	},
	[]string{"reason"},
)

// recordValidatorEvict increments the validator-evict counter for the given
// reason. Reason should be a stable, low-cardinality label.
func recordValidatorEvict(reason string) {
	cacheValidatorEvictTotal.WithLabelValues(reason).Inc()
}

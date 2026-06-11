package main

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Resolve-outcome label values for repoResolveTotal. Bounded enum — every
// resolveRoot call increments exactly one of these so a resolver regression
// (e.g. bare names silently degrading to a CWD-relative stat miss) surfaces as
// a metric shift, not a fleet of agents falling back to Grep/Read unnoticed.
const (
	resolveOutcomeAbsolute = "hit_absolute"  // caller passed an absolute / mapped local path that stat'd OK
	resolveOutcomeBareRoot = "hit_bare_root" // bare name matched a checkout under LocalRepoDirs
	resolveOutcomeRemote   = "hit_remote"    // dispatched to a clone or remote-slug local checkout
	resolveOutcomeWP       = "hit_wp"        // dispatched to a WordPress.org plugin fetch
	resolveOutcomeMiss     = "miss"          // no source produced a usable root (error returned)
)

// repoResolveTotal counts every resolveRoot invocation by the dispatch outcome.
//
//   - outcome: hit_absolute | hit_bare_root | hit_remote | hit_wp | miss
//
// A spike in {outcome="miss"} means callers are passing identifiers the
// resolver cannot map — the silent-degradation class this counter exists to
// catch. Cardinality: 5 series.
var repoResolveTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "gocode_repo_resolve_total",
		Help: "resolveRoot invocations by dispatch outcome (hit_absolute, hit_bare_root, hit_remote, hit_wp, miss).",
	},
	[]string{"outcome"},
)

package embeddings

import (
	"os"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// staleDemoteEnabled is set at init from STALE_DEMOTE env (default on).
// Exported via StaleDemoteEnabled() to avoid a package-level var race.
var staleDemoteEnabled = func() bool {
	v := os.Getenv("STALE_DEMOTE")
	return v != "off"
}()

// StaleDemoteEnabled reports whether the stale-demote safety-net is active.
// Callers outside this package (e.g. tool_semantic_search.go) read this flag
// rather than duplicating the env check.
func StaleDemoteEnabled() bool { return staleDemoteEnabled }

// staleDemotedTotal counts search results demoted to the stale block per call.
//
// A non-zero rate means orphan rows (updated_at older than current index
// generation) are surfacing in search results — Bug B's hard-delete missed
// them and the stale-demote safety-net caught them. Operator action: run the
// orphan-reconcile sweep (see docs/orphan-reconcile.md).
//
// Counter is pre-touched at 0 so /metrics always exposes the series.
var staleDemotedTotal = promauto.NewCounter(
	prometheus.CounterOpts{
		Name: "gocode_semantic_stale_demoted_total",
		Help: "Search results demoted to the stale block (updated_at < indexed_at). " +
			"Non-zero rate signals missed orphans that Bug B hard-delete should have removed.",
	},
)

func init() { staleDemotedTotal.Add(0) } // pre-touch

// staleDemoteEpsilon is the grace window between indexed_at and the cut-off.
// An embedding row is stale when its updated_at < (indexed_at - epsilon).
//
// Why 1 minute: SetRepoState is called after the last file is embedded.
// Under normal operation all embedded rows have updated_at within seconds of
// indexed_at. A 60 s window avoids false demotions from clock skew or minor
// delays without hiding genuine orphans (which freeze at the previous
// generation's timestamp, typically hours to days older).
const staleDemoteEpsilon = time.Minute

// ApplyStaleDemote partitions results into fresh-then-stale using a binary
// criterion: a result is stale when its UpdatedAt is older than
// (generation - staleDemoteEpsilon).
//
// Guarantees:
//   - When generation.IsZero() or !enabled: returns results unchanged
//     (byte-identical — no allocation, same slice header).
//   - When all results are fresh: returns results unchanged (byte-identical).
//   - Stable within each partition (fresh order preserved, stale order preserved).
//   - Each demoted result increments gocode_semantic_stale_demoted_total.
//
// The function does NOT filter results out; it demotes them to the bottom.
// This is a safety-net: a missed orphan surfaces at rank N+1 or later rather
// than at rank 1-5, and the counter fires so the operator knows to sweep.
func ApplyStaleDemote(results []SearchResult, generation time.Time, enabled bool) []SearchResult {
	if !enabled || generation.IsZero() {
		return results
	}
	cutoff := generation.Add(-staleDemoteEpsilon)

	// Fast path: scan once — if no stale rows, return unchanged (byte-identical).
	staleCount := 0
	for i := range results {
		if results[i].UpdatedAt.Before(cutoff) {
			staleCount++
		}
	}
	if staleCount == 0 {
		return results
	}

	// Partition: stable fresh-then-stale.
	out := make([]SearchResult, 0, len(results))
	for _, r := range results {
		if !r.UpdatedAt.Before(cutoff) {
			out = append(out, r)
		}
	}
	for _, r := range results {
		if r.UpdatedAt.Before(cutoff) {
			staleDemotedTotal.Add(1)
			out = append(out, r)
		}
	}
	return out
}

package embeddings

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// gocode_repo_info is an info-style gauge (always 1) that maps the opaque
// repo label (FNV-32a hash, e.g. "code_3b3bc83f") used on all other
// gocode_* metrics back to a human-readable path or URL. Without it,
// dashboards and runbooks cannot resolve which repository a metric refers to.
//
// Labels:
//   - repo  — the FNV-32a graph key (e.g. "code_a3f2b1c0"); matches the
//     repo label on gocode_repo_embeddings_present,
//     gocode_autoindex_duration_seconds, etc.
//   - path  — the absolute host filesystem path or remote git URL that the
//     repo key was derived from. For local repos this is the on-disk root
//     (e.g. "/host/src/oxpulse-chat"); for remote clones it is the upstream
//     URL passed to the embedder.
//
// Cardinality: one series per registered repo (~60 on current fleet) — well
// within safe cardinality bounds.
//
// Set by SetRepoInfoGauge; called at the entry of IncrementalSync and
// IndexRepo so every code path (autoindex, on-demand, background) registers
// the mapping before metrics start flowing.
var repoInfo = promauto.NewGaugeVec(
	prometheus.GaugeOpts{
		Name: "gocode_repo_info",
		Help: "Info gauge (always 1) mapping repo label to human-readable path/URL. " +
			"Join with other gocode_* metrics on the repo label to resolve opaque hashes.",
	},
	[]string{"repo", "path"},
)

// SetRepoInfoGauge records the (repo key → path) mapping in the
// gocode_repo_info gauge. The gauge value is always 1; presence of the
// series is the signal.
//
// Call at the entry of any code path that receives a (repoKey, root) pair —
// IncrementalSync and IndexRepo cover all autoindex and on-demand flows.
// Idempotent: repeated calls with the same labels are no-ops on the
// Prometheus side (GaugeVec.WithLabelValues reuses the existing metric).
func SetRepoInfoGauge(repo, path string) {
	repoInfo.WithLabelValues(repo, path).Set(1)
}

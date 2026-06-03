package embeddings

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// gocode_index_orphans_deleted_total counts intra-key orphan embedding rows
// deleted during a full indexRepo walk. An orphan is a (file_path, symbol_name)
// row in code_embeddings for a repo_key that is no longer present in the freshly-
// parsed symbol set — i.e. a symbol was deleted, renamed, or moved between files.
//
// This counter increments only on the full-walk path (not on the same-SHA fast-
// path or incremental git-diff path, where the complete parsed set is unavailable
// and deletion would be unsafe). A non-zero rate indicates symbols are being
// cleaned up as expected on each full re-index.
//
// Cardinality: 1 series (unlabelled). repo_key cardinality is acceptable at ~100
// repos, but the repo label is omitted to keep alert rules simple; the delete
// count and the gocode_orphan_repo_keys gauge together provide enough signal.
var indexOrphansDeletedTotal = promauto.NewCounter(
	prometheus.CounterOpts{
		Name: "gocode_index_orphans_deleted_total",
		Help: "Intra-key orphan embedding rows deleted during full indexRepo walks (deleted/renamed symbols).",
	},
)

// gocode_orphan_repo_keys is a gauge recording the number of distinct repo_keys
// present in code_embeddings but absent from code_repo_state. Non-zero indicates
// stale worktree snapshots or deregistered repos whose embedding rows were not
// cleaned up. The orphan_sweep MCP tool resets this to zero.
//
// The gauge is set by the operator-initiated orphan_sweep tool, not on every
// indexRepo call (which would require an extra COUNT query per boot).
var orphanRepoKeysGauge = promauto.NewGauge(
	prometheus.GaugeOpts{
		Name: "gocode_orphan_repo_keys",
		Help: "Distinct repo_keys in code_embeddings with no matching code_repo_state row; 0 = clean.",
	},
)

// SetOrphanRepoKeysGauge sets the gocode_orphan_repo_keys gauge. Called by the
// orphan_sweep MCP tool after a sweep to report the post-sweep count.
func SetOrphanRepoKeysGauge(n float64) { orphanRepoKeysGauge.Set(n) }

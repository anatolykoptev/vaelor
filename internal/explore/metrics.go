package explore

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// exploreFilesChangedMethodTotal counts every countDiffTreeFiles invocation
// labelled by the code path taken to compute the file count.
//
//   - method: diff_tree       — normal path (git diff-tree returned output on first run)
//   - method: root_fallback   — initial commit; retried with --root flag
//   - method: empty_repo      — --root retry also returned empty output (empty repo)
//   - method: error           — git command returned a non-zero exit code
//   - method: shallow_boundary — commit at the shallow-clone boundary; parent absent,
//     --root retry skipped to avoid counting all-files
//
// Cardinality: 5 series.
var exploreFilesChangedMethodTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "gocode_explore_files_changed_method_total",
		Help: "countDiffTreeFiles invocations by the git code path used (diff_tree, root_fallback, empty_repo, shallow_boundary, error).",
	},
	[]string{"method"},
)

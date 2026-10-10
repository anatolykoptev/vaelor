package scip

import (
	"log/slog"
	"path/filepath"
	"strings"
	"sync/atomic"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Skip reasons for vaelor_scip_skipped_total.
const (
	// SkipReasonUntrustedRepo: the indexer executes repo-controlled code
	// (build scripts, proc-macros, build tools, the Python interpreter) and the
	// repo is not an operator-managed local checkout.
	SkipReasonUntrustedRepo = "untrusted_repo_executes_code"
	// SkipReasonSymlinkEscape: a symlink pointing outside the repo root was
	// left out of the indexing copy.
	SkipReasonSymlinkEscape = "symlink_outside_root"
)

// scipSkippedTotal counts SCIP work that was deliberately not done for safety
// reasons. The call graph silently stays at tree-sitter tier in these cases,
// so the counter is how an operator sees the degradation.
//
//   - lang: repo language (rust, python, java, …) or "-" for non-language skips
//   - reason: untrusted_repo_executes_code | symlink_outside_root
var scipSkippedTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "vaelor_scip_skipped_total",
		Help: "SCIP indexing steps skipped for safety, by language and reason (untrusted_repo_executes_code, symlink_outside_root).",
	},
	[]string{"lang", "reason"},
)

// RecordSkipped bumps vaelor_scip_skipped_total.
func RecordSkipped(lang, reason string) {
	scipSkippedTotal.WithLabelValues(lang, reason).Inc()
}

// readOnlyLangs are the languages whose indexer only parses source and never
// runs repo-controlled code. Evidence (scip-typescript 0.4.0): the only
// child_process use is `pnpm ls` / `yarn` behind the --pnpm-workspaces and
// --yarn-workspaces flags, which the registry never passes; the rest is the
// TypeScript compiler API. Every other indexer is deny-by-default:
//   - rust-analyzer runs build.rs of the workspace AND of every dependency,
//     expands proc-macros (a proc-macro still ran with procMacro.enable=false
//     and buildScripts.enable=false in `rust-analyzer scip`), and honours
//     repo .cargo/config.toml (build.rustc = arbitrary binary).
//   - scip-python (pyright) execs `python -c "import sys, json"` with the
//     repo as cwd, so a repo-level json.py/sys-shadowing module executes.
//   - scip-java drives gradle/maven, which run build scripts and plugins.
var readOnlyLangs = map[string]bool{
	"typescript": true,
	"javascript": true,
}

// trustedRoots holds the resolved directories of operator-managed local
// checkouts. Anything outside them (clone cache, PR worktrees, arbitrary
// paths) is untrusted. Empty by default: deny-all until configured.
var trustedRoots atomic.Pointer[[]string]

// SetTrustedRoots declares the directories whose repos the operator vouches
// for (the auto-index / local checkout dirs). Symlinks are resolved so a
// link cannot smuggle an untrusted tree in. Safe for concurrent use; call at
// startup.
func SetTrustedRoots(dirs []string) {
	resolved := make([]string, 0, len(dirs))
	for _, d := range dirs {
		if d == "" {
			continue
		}
		if r, err := filepath.EvalSymlinks(d); err == nil {
			d = r
		}
		resolved = append(resolved, filepath.Clean(d))
	}
	trustedRoots.Store(&resolved)
}

// IsTrustedRoot reports whether root lies inside a configured trusted dir.
func IsTrustedRoot(root string) bool {
	p := trustedRoots.Load()
	if p == nil {
		return false
	}
	r, err := filepath.EvalSymlinks(root)
	if err != nil {
		return false
	}
	for _, t := range *p {
		if within(t, r) {
			return true
		}
	}
	return false
}

// AllowIndexer reports whether the indexer for lang may run against root.
// Indexers that execute repo-controlled code run only on trusted roots; on
// anything else the caller must degrade to tree-sitter. A refusal is logged
// and counted here so no caller can forget to.
func AllowIndexer(lang, root string) bool {
	if readOnlyLangs[lang] || IsTrustedRoot(root) {
		return true
	}
	RecordSkipped(lang, SkipReasonUntrustedRepo)
	slog.Warn("scip: skipping indexer that executes repo code on untrusted repo",
		"lang", lang, "reason", SkipReasonUntrustedRepo, "root", root)
	return false
}

// within reports whether path is dir itself or lies beneath it.
func within(dir, path string) bool {
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// TrustedRootsExcluding returns dirs minus any that overlap an untrusted
// location (the clone workspace, the temp dir PR worktrees live in): a clone
// directory nested in a trusted dir would otherwise inherit its trust, and a
// trusted dir nested in the clone directory would trust fetched content.
// Overlaps fail closed: the whole trusted dir is dropped, and logged.
func TrustedRootsExcluding(dirs []string, untrusted ...string) []string {
	resolve := func(d string) string {
		if r, err := filepath.EvalSymlinks(d); err == nil {
			d = r
		}
		return filepath.Clean(d)
	}
	var out []string
outer:
	for _, d := range dirs {
		if d == "" {
			continue
		}
		rd := resolve(d)
		for _, u := range untrusted {
			if u == "" {
				continue
			}
			ru := resolve(u)
			if within(rd, ru) || within(ru, rd) {
				slog.Error("scip: trusted dir overlaps an untrusted location; not trusting it",
					"trusted", d, "untrusted", u)
				continue outer
			}
		}
		out = append(out, d)
	}
	return out
}

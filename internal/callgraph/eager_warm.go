package callgraph

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// eagerWarmParallelism caps concurrent `go list` prewarm subprocesses.
// The deploy box is 4-core; each prewarm uses 1-2 cores at peak. Cap=2
// keeps total CPU under 50% during the burst so MCP serve stays responsive.
const eagerWarmParallelism = 2

// eagerWarmTimeout bounds a single repo's prewarm. 5 minutes is generous
// for the largest in-house repo (vendor export listings complete <1m
// typical; non-vendored repos may additionally download modules).
const eagerWarmTimeout = 5 * time.Minute

// prewarmRepoFn is the unit of work executed per repo: it returns the
// import paths of the packages that produced no export data (cgo-requiring,
// broken replace targets, missing deps) — a nonempty list with a nil error
// is a PARTIAL warm, visible as outcome="partial". Production wires it to
// runGoListPrewarm; tests swap it for a stub.
var prewarmRepoFn = runGoListPrewarm

// recordEagerWarmFn is the metric-bump hook for eager-warm outcomes.
// Tests may replace it to intercept recorded outcomes without relying on
// Prometheus counter state, which is global and not reset between tests.
var recordEagerWarmFn = recordEagerWarm

// EagerWarmRepos enumerates immediate subdirectories of each path in dirs
// that contain a go.mod, then runs the GOCACHE prewarm for each in parallel
// (bounded by eagerWarmParallelism). Returns when all warmups have settled.
//
// Intended to be invoked once at process startup from a goroutine so it does
// not block the MCP server bring-up. The caller is responsible for goroutine
// dispatch — this function blocks until completion to make tests deterministic.
func EagerWarmRepos(ctx context.Context, dirs []string) {
	roots := discoverGoRepos(dirs)
	if len(roots) == 0 {
		slog.Info("eager warm: no Go repos discovered", "dirs", dirs)
		return
	}
	slog.Info("eager warm: starting", "repo_count", len(roots), "parallelism", eagerWarmParallelism)

	sem := make(chan struct{}, eagerWarmParallelism)
	var wg sync.WaitGroup
	for _, root := range roots {
		wg.Add(1)
		sem <- struct{}{}
		go func(r string) {
			defer wg.Done()
			defer func() { <-sem }()

			// vendor/modules.txt decides the -mod flag; it is no longer a
			// skip gate — non-vendored repos warm via the module proxy
			// (-mod=mod). Lstat+Stat distinguish a healthy vendor/ from a
			// dangling symlink (Lstat succeeds, Stat ENOENTs the target) —
			// a broken configuration the operator should fix, logged WARN.
			modFlag := "-mod=mod"
			vendorPath := filepath.Join(r, "vendor")
			if _, lstatErr := os.Lstat(vendorPath); lstatErr == nil {
				if _, statErr := os.Stat(vendorPath); statErr != nil {
					recordEagerWarmFn("failed")
					slog.Warn("eager warm: vendor/ is a broken symlink or unreadable", "root", r, "stat_err", statErr)
					return
				}
				modFlag = "-mod=vendor"
			} else if !os.IsNotExist(lstatErr) {
				// Non-ENOENT Lstat error (EPERM, etc.) — real IO problem.
				recordEagerWarmFn("failed")
				slog.Warn("eager warm: stat vendor/ failed", "root", r, "stat_err", lstatErr)
				return
			}

			recordEagerWarmFn("started")
			errored, err := prewarmRepoFn(ctx, r, modFlag)
			switch {
			case err != nil:
				// WARN, not Debug: a repo that never warms is invisible
				// otherwise — that is how vaelor itself stayed cold across
				// every deploy while only the counter knew (issue #736).
				recordEagerWarmFn("failed")
				slog.Warn("eager warm: prewarm failed", "root", r, "err", err)
			case len(errored) > 0:
				// The run tolerated per-package failures (-e): export data
				// exists for the rest, but those packages will never
				// resolve typed edges until fixed — name the repo and the
				// packages so the operator can tell "cgo repo, expected"
				// from "vendor drift, fix it".
				recordEagerWarmFn("partial")
				slog.Warn("eager warm: prewarm partial — some packages produced no export data",
					"root", r, "errored_packages", len(errored),
					"first", first(errored, 3))
			default:
				recordEagerWarmFn("completed")
				slog.Info("eager warm: prewarm complete", "root", r)
			}
		}(root)
	}
	wg.Wait()
	slog.Info("eager warm: done")
}

// discoverGoRepos returns repos under each dir that contain a go.mod at
// their top level. Symlinks and non-directory entries are skipped. dirs
// entries are trimmed; empty entries are ignored.
func discoverGoRepos(dirs []string) []string {
	var roots []string
	for _, dir := range dirs {
		dir = strings.TrimSpace(dir)
		if dir == "" {
			continue
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			slog.Warn("eager warm: read dir failed", "dir", dir, "err", err)
			continue
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			root := filepath.Join(dir, e.Name())
			if _, statErr := os.Stat(filepath.Join(root, "go.mod")); statErr != nil {
				continue
			}
			roots = append(roots, root)
		}
	}
	return roots
}

// runGoListPrewarm warms GOCACHE for root by generating export data for
// every package `go list` can reach:
//
//	go list -e -export -deps -test -f '{{if or .Error .DepsErrors}}ERR{{end}}' -mod=<flag> ./...
//
// The command and its environment come from goanalysis.ExportListArgs /
// GoEnv, the same ones the typed load primes with, and the same charged,
// one-at-a-time build (primeCharged): -test and CGO_ENABLED are
// part of the build-cache key, and a prewarm that differs from the load
// warms nothing the load reuses.
//
// Flag rationale (issue #736):
//
//   - -export forces export-data generation — the exact build-cache keys
//     packages.Load's NeedDeps driver consumes on the request path.
//   - -deps covers the transitive import set, not just the repo's own
//     packages — a dep without export data is just as slow on first load.
//   - -e makes per-package failures NON-FATAL: cgo packages under
//     CGO_ENABLED=0 (tree-sitter repos — vaelor itself was the repo never
//     warming, since `go build -mod=vendor` exited 1 on it) and broken
//     replace targets land in .Error/.DepsErrors while every other package
//     still warms. This is the canonical "prime the export cache" recipe —
//     gopls-style warmers use the same shape.
//   - -f prints "ERR" for exactly the packages that produced no export
//     data; the count is returned for the partial outcome.
//   - -mod is per-repo: vendor/modules.txt → -mod=vendor, otherwise
//     -mod=mod (module proxy; previously those repos were skipped
//     entirely — 23 of 39).
//
// Returns (errored import paths, nil) on tolerated-failure runs and
// (nil, err) only when the go command itself fails (timeout, tool missing).
func runGoListPrewarm(ctx context.Context, root, _ string) ([]string, error) {
	warmCtx, cancel := context.WithTimeout(ctx, eagerWarmTimeout)
	defer cancel()
	return primeCharged(warmCtx, root)
}

// first returns up to n elements of s — for compact WARN logs.
func first(s []string, n int) []string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

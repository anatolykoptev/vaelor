package callgraph

import (
	"context"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// eagerWarmParallelism caps concurrent `go build` prewarm subprocesses.
// The deploy box is 4-core; each prewarm uses 1-2 cores at peak. Cap=2
// keeps total CPU under 50% during the burst so MCP serve stays responsive.
const eagerWarmParallelism = 2

// eagerWarmTimeout bounds a single repo's prewarm. 5 minutes is generous
// for the largest in-house repo with CGO_ENABLED=0 (vendor builds <1m typical).
const eagerWarmTimeout = 5 * time.Minute

// warmGoBuildFn is the unit of work executed per repo. Production wires it
// to runGoBuildPrewarm; tests swap it for a stub to assert dispatch behavior
// without paying the packages.Load cost.
var warmGoBuildFn = runGoBuildPrewarm

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

			// Check for vendor/ before dispatching: repos without vendor/ use
			// the module proxy workflow and -mod=vendor would always fail with
			// "inconsistent vendoring". Skip them with a distinct counter outcome
			// so started/completed ratios remain meaningful.
			//
			// We use Lstat (not Stat) first to detect the path's own existence
			// without following symlinks. If vendor/ is a dangling symlink, Lstat
			// succeeds but Stat returns ENOENT for the target — that is a broken
			// configuration the operator should fix, not a silent skip.
			vendorPath := filepath.Join(r, "vendor")
			if _, lstatErr := os.Lstat(vendorPath); lstatErr != nil {
				if os.IsNotExist(lstatErr) {
					recordEagerWarmFn("skipped_no_vendor")
					slog.Debug("eager warm: skipping repo without vendor/", "root", r)
					return
				}
				// Non-ENOENT Lstat error (EPERM, etc.) — real IO problem.
				recordEagerWarmFn("failed")
				slog.Warn("eager warm: stat vendor/ failed", "root", r, "stat_err", lstatErr)
				return
			}
			// vendor/ exists as a filesystem entry. Verify it resolves (detects
			// dangling symlinks: Lstat succeeds but Stat returns ENOENT for target).
			if _, statErr := os.Stat(vendorPath); statErr != nil {
				recordEagerWarmFn("failed")
				slog.Warn("eager warm: vendor/ is a broken symlink or unreadable", "root", r, "stat_err", statErr)
				return
			}

			recordEagerWarmFn("started")
			if err := warmGoBuildFn(ctx, r); err != nil {
				// recordEagerWarmFn (f20d840): testable outcome hook. slog.Debug (main
				// a487fbe): build-failure noise was deliberately demoted. f20d840's WARN
				// contract governs the broken-symlink path (line ~82, kept Warn), NOT this
				// build-failure path — so both intents are preserved.
				recordEagerWarmFn("failed")
				slog.Debug("eager warm: prewarm failed", "root", r, "err", err)
				return
			}
			recordEagerWarmFn("completed")
			slog.Info("eager warm: prewarm complete", "root", r)
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

// runGoBuildPrewarm runs `go build -mod=vendor ./...` against root with the
// same env as the on-demand warm path (CGO_ENABLED=0, GOWORK=off, dedicated
// GOCACHE). Bounded by eagerWarmTimeout to avoid hung builds blocking the
// startup goroutine indefinitely.
//
// The caller (EagerWarmRepos goroutine) is responsible for checking whether
// vendor/ exists before calling this function. runGoBuildPrewarm assumes vendor/
// is present and simply executes the build. If vendor/ is absent or broken the
// build command will fail and the error is returned to the caller.
func runGoBuildPrewarm(ctx context.Context, root string) error {
	warmCtx, cancel := context.WithTimeout(ctx, eagerWarmTimeout)
	defer cancel()
	cmd := exec.CommandContext(warmCtx, "go", "build", "-mod=vendor", "./...")
	cmd.Dir = root
	cmd.Env = buildPrewarmEnv()
	return cmd.Run()
}

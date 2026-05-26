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
			recordEagerWarm("started")
			if err := warmGoBuildFn(ctx, r); err != nil {
				recordEagerWarm("failed")
				slog.Debug("eager warm: prewarm failed", "root", r, "err", err)
				return
			}
			recordEagerWarm("completed")
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
// Repos without a vendor/ directory are silently skipped (DEBUG log, nil
// return). Missing vendor is an operator decision — module proxy workflow or
// archived repo — not a build failure. Running -mod=vendor without vendor/
// always fails with "inconsistent vendoring"; the WARN it previously produced
// was noise with no remediation path.
func runGoBuildPrewarm(ctx context.Context, root string) error {
	if _, err := os.Stat(filepath.Join(root, "vendor")); err != nil {
		slog.Debug("eager warm: skipping repo without vendor/", "root", root)
		return nil
	}
	warmCtx, cancel := context.WithTimeout(ctx, eagerWarmTimeout)
	defer cancel()
	cmd := exec.CommandContext(warmCtx, "go", "build", "-mod=vendor", "./...")
	cmd.Dir = root
	cmd.Env = buildPrewarmEnv()
	return cmd.Run()
}

package main

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/anatolykoptev/go-kit/env"
	"github.com/anatolykoptev/vaelor/internal/codegraph"
	"github.com/anatolykoptev/vaelor/internal/embeddings"
	"github.com/anatolykoptev/vaelor/internal/workspace"
)

// autoIndexDirs returns the (optionally translated) directories to scan for
// auto-indexing. It checks AUTOINDEX_TRANSLATE (VAELOR_ prefix with GO_CODE_
// fallback via getenvRebrand): when "true", it applies PATH_MAPPINGS to
// cfg.AutoIndexDirs via workspace.TranslateDirs so host-side paths become
// container-internal paths. Default is false — existing behavior preserved
// until the operator opts in.
//
// Both the eager-warm path (main.go) and the embeddings auto-index path
// (register.go) call this helper so the env-check is not duplicated.
func autoIndexDirs(cfg Config) []string {
	if translateAuto, _ := strconv.ParseBool(getenvRebrand("AUTOINDEX_TRANSLATE")); translateAuto {
		return workspace.TranslateDirs(cfg.AutoIndexDirs, cfg.PathMappings)
	}
	return cfg.AutoIndexDirs
}

// autoIndexRunners are the two background entry points startAutoIndex launches.
// Production uses defaultAutoIndexRunners; tests pass recorders so the wiring
// (the loop starts, after boot, with the configured interval and the run
// context) is asserted without a database.
type autoIndexRunners struct {
	boot func(*embeddings.Pipeline, []string, embeddings.RepoKeyFunc, embeddings.AutoIndexOpts)
	loop func(context.Context, *embeddings.Pipeline, []string, embeddings.RepoKeyFunc, embeddings.AutoIndexOpts, time.Duration)
}

var defaultAutoIndexRunners = autoIndexRunners{boot: embeddings.AutoIndex, loop: embeddings.AutoIndexLoop}

// startAutoIndex runs the boot index pass in the background and, once it has
// returned, the periodic re-sync loop. Without the loop head_sha only advanced
// at boot or on a lucky semantic_search, and every other tool kept warning
// "index built against main X" until the next restart. It logs the effective
// settings once, so "disabled" and "never wired" are distinguishable. The
// returned channel closes when the background goroutine has finished.
func startAutoIndex(ctx context.Context, cfg Config, pipeline *embeddings.Pipeline, run autoIndexRunners) <-chan struct{} {
	done := make(chan struct{})
	if pipeline == nil || len(cfg.AutoIndexDirs) == 0 {
		close(done)
		return done
	}
	opts := embeddings.AutoIndexOpts{
		Concurrency:     cfg.AutoIndexConcurrency,
		RetryMax:        cfg.AutoIndexRetryMax,
		RetryBase:       cfg.AutoIndexRetryBase,
		PassBudget:      cfg.AutoIndexPassBudget,
		MaxFilesPerRepo: cfg.AutoIndexMaxFilesPerRepo,
	}
	dirs := autoIndexDirs(cfg)
	if cfg.AutoIndexInterval > 0 {
		slog.Info("autoindex: periodic re-sync enabled",
			slog.Duration("interval", cfg.AutoIndexInterval),
			slog.Duration("pass_budget", cfg.AutoIndexPassBudget),
			slog.Int("max_files_per_repo", cfg.AutoIndexMaxFilesPerRepo))
	} else {
		slog.Info("autoindex: periodic re-sync disabled (AUTOINDEX_INTERVAL=0); index refreshes at boot and on semantic_search only")
	}
	go func() {
		defer close(done)
		run.boot(pipeline, dirs, codegraph.GraphNameFor, opts)
		run.loop(ctx, pipeline, dirs, codegraph.GraphNameFor, opts, cfg.AutoIndexInterval)
	}()
	return done
}

// parseAutoIndexDuration reads a duration env var strictly. Unset or empty
// gives def. A value must carry a unit ("15m"; a bare "15" is an error, not
// 15 seconds), must not be negative, and must be at least floor. "0" is the one
// unitless value accepted, and only where allowZero (it switches a feature off).
func parseAutoIndexDuration(name string, def, floor time.Duration, allowZero bool) (time.Duration, error) {
	raw, ok := env.Lookup(name)
	raw = strings.TrimSpace(raw)
	if !ok || raw == "" {
		return def, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("%s=%q: %w (give a unit, e.g. 15m)", name, raw, err)
	}
	switch {
	case d < 0:
		return 0, fmt.Errorf("%s=%q: must not be negative", name, raw)
	case d == 0 && allowZero:
		return 0, nil
	case d < floor:
		return 0, fmt.Errorf("%s=%q: below the %s floor", name, raw, floor)
	}
	return d, nil
}

// parseAutoIndexMaxFiles reads AUTOINDEX_MAX_FILES strictly (>= 1).
func parseAutoIndexMaxFiles(def int) (int, error) {
	raw, ok := env.Lookup("AUTOINDEX_MAX_FILES")
	raw = strings.TrimSpace(raw)
	if !ok || raw == "" {
		return def, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		return 0, fmt.Errorf("AUTOINDEX_MAX_FILES=%q: want an integer >= 1", raw)
	}
	return n, nil
}

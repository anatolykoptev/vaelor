package main

import (
	"context"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/anatolykoptev/go-kit/env"
	"github.com/anatolykoptev/vaelor/internal/goanalysis"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// goCacheSubdirPattern matches the two-lowercase-hex-character subdirectories
// the go tool scatters cache entries into ($GOCACHE/xx/<entry>) — the same
// boundary cmd/go's cache.Trim uses. Root files (README, trim.txt, locked,
// lastverify) and anything deeper than xx/<file> are never eligible.
var goCacheSubdirPattern = regexp.MustCompile(`^[0-9a-f]{2}$`)

const (
	// defaultGoCacheTrimMaxAge bounds how long an unused cache entry may sit.
	// Go bumps an entry's mtime on use (at most once an hour), so mtime is
	// last-use: 48h idle means evictable, and a delete is a cache miss, not
	// damage. Override via VAELOR_GOCACHE_MAX_AGE.
	defaultGoCacheTrimMaxAge = 48 * time.Hour

	// goCacheTrimFirstDelay keeps the first run off the startup path — the
	// eager pre-warm and first tool calls are re-populating entries right
	// after boot, and trimming must not compete with them.
	goCacheTrimFirstDelay = time.Minute

	// goCacheTrimInterval is the cadence between runs. Go's own trim uses a
	// 5-day cutoff fired at most daily and only from inside some go commands
	// ($GOCACHE/trim.txt) — on the production volume it had not run in 4 days
	// while the cache grew to 21.6GB (disk alert at 91%).
	goCacheTrimInterval = 24 * time.Hour

	// goEnvTimeout bounds the `go env GOCACHE` probe used when GOCACHE is not
	// in the process environment.
	goEnvTimeout = 5 * time.Second
)

var (
	goCacheTrimFilesTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "vaelor_gocache_trim_files_total",
		Help: "GOCACHE entries deleted by the background trimmer (regular files inside two-hex subdirs older than VAELOR_GOCACHE_MAX_AGE).",
	})
	goCacheTrimBytesTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "vaelor_gocache_trim_bytes_total",
		Help: "Bytes freed by the background GOCACHE trimmer.",
	})
	goCacheTrimErrorsTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "vaelor_gocache_trim_errors_total",
		Help: "Read/stat/delete errors hit by the background GOCACHE trimmer.",
	})
)

// goCacheDisabledOnce gates the single INFO line emitted when no cache dir
// resolves — a dir created later (fresh volume, first go command) still gets
// picked up by a later run, without a log line per tick.
var goCacheDisabledOnce sync.Once

// startGoCacheTrimLoop is the background GOCACHE trimmer goroutine, started
// from runMCPServe next to the other background loops. First run after
// goCacheTrimFirstDelay, then every goCacheTrimInterval, until ctx (the
// server's signal context) cancels.
func startGoCacheTrimLoop(ctx context.Context) {
	maxAge := goCacheTrimMaxAge()
	slog.Info("gocache trim: scheduled",
		slog.Duration("max_age", maxAge),
		slog.Duration("first_run_in", goCacheTrimFirstDelay),
		slog.Duration("interval", goCacheTrimInterval))

	timer := time.NewTimer(goCacheTrimFirstDelay)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			slog.Info("gocache trim: stopped")
			return
		case <-timer.C:
			runGoCacheTrim(ctx, maxAge)
			timer.Reset(goCacheTrimInterval)
		}
	}
}

// goCacheTrimMaxAge resolves VAELOR_GOCACHE_MAX_AGE (a Go duration). Invalid
// and non-positive values warn and fall back to the default — a typo or a
// zero/negative bound must never disable the limit or nuke the whole cache.
func goCacheTrimMaxAge() time.Duration {
	d, err := env.DurationE("VAELOR_GOCACHE_MAX_AGE", defaultGoCacheTrimMaxAge)
	if err != nil || d <= 0 {
		slog.Warn("gocache trim: invalid VAELOR_GOCACHE_MAX_AGE, using default",
			slog.Duration("default", defaultGoCacheTrimMaxAge), slog.Any("error", err))
		return defaultGoCacheTrimMaxAge
	}
	return d
}

// runGoCacheTrim executes one trim pass. A run that resolves no dirs is a
// logged-once no-op, not an error.
func runGoCacheTrim(ctx context.Context, maxAge time.Duration) {
	dirs := goCacheTrimDirs(ctx)
	if len(dirs) == 0 {
		goCacheDisabledOnce.Do(func() {
			slog.Info("gocache trim: disabled — GOCACHE, `go env GOCACHE` and the analysis cache all resolve to empty, off or missing dirs")
		})
		return
	}
	trimGoCacheDirs(dirs, maxAge)
}

// trimGoCacheDirs runs one pass over every resolved cache dir and reports the
// aggregate: one INFO line per run plus the counters, with the first per-file
// error logged — a write failure must never be silent.
func trimGoCacheDirs(dirs []string, maxAge time.Duration) {
	start := time.Now()
	var total goCacheTrimStats
	for _, dir := range dirs {
		st := trimGoCacheDir(dir, maxAge, start)
		total.files += st.files
		total.bytes += st.bytes
		total.errs += st.errs
		if total.firstErr == nil {
			total.firstErr = st.firstErr
		}
	}
	goCacheTrimFilesTotal.Add(float64(total.files))
	goCacheTrimBytesTotal.Add(float64(total.bytes))
	goCacheTrimErrorsTotal.Add(float64(total.errs))
	if total.firstErr != nil {
		slog.Warn("gocache trim: first error this run", slog.Any("error", total.firstErr))
	}
	slog.Info("gocache trim: run complete",
		slog.Int("dirs", len(dirs)),
		slog.Int("files_removed", total.files),
		slog.Int64("bytes_freed", total.bytes),
		slog.Int("errors", total.errs),
		slog.Duration("duration", time.Since(start)),
		slog.Duration("max_age", maxAge))
}

// goCacheTrimStats accumulates one dir's (or one run's) outcome.
type goCacheTrimStats struct {
	files    int
	bytes    int64
	errs     int
	firstErr error
}

// goCacheTrimDirs resolves the directories eligible for trimming: the ambient
// cache per spec (GOCACHE env, else `go env GOCACHE`) UNION the dir every
// analysis go command is pinned to by goanalysis.GoEnv — GoEnv appends
// GOCACHE=<pin> after os.Environ so it wins for trusted and untrusted roots
// alike. The union matters: with GOCACHE unset, the ambient dir is the user
// cache the server never fills, while the pinned dir keeps growing (the
// production incident). Deduped; empty/"off"/missing/non-dir entries drop.
func goCacheTrimDirs(ctx context.Context) []string {
	var candidates []string
	if d := os.Getenv("GOCACHE"); d != "" {
		candidates = append(candidates, d)
	} else {
		candidates = append(candidates, goEnvGOCACHE(ctx))
	}
	candidates = append(candidates, goanalysis.GoCacheDir())
	return filterGoCacheDirs(candidates)
}

// goEnvGOCACHE resolves the ambient build cache the way the go tool does when
// GOCACHE is not in the environment (go env file or the per-OS user cache
// dir). A failed probe resolves to "" — the pinned analysis cache is still a
// candidate.
func goEnvGOCACHE(ctx context.Context) string {
	gctx, cancel := context.WithTimeout(ctx, goEnvTimeout)
	defer cancel()
	out, err := exec.CommandContext(gctx, "go", "env", "GOCACHE").Output()
	if err != nil {
		slog.Debug("gocache trim: go env GOCACHE failed", slog.Any("error", err))
		return ""
	}
	return strings.TrimSpace(string(out))
}

// filterGoCacheDirs drops empty candidates, "off" (go's cache-disabled
// sentinel), missing paths and non-directories, and dedups the rest
// (canonicalised via filepath.Clean so "x" and "x/" collide).
func filterGoCacheDirs(candidates []string) []string {
	var dirs []string
	seen := make(map[string]struct{}, len(candidates))
	for _, c := range candidates {
		c = strings.TrimSpace(c)
		if c == "" || c == "off" {
			continue
		}
		c = filepath.Clean(c)
		if _, dup := seen[c]; dup {
			continue
		}
		if info, err := os.Stat(c); err != nil || !info.IsDir() {
			continue
		}
		seen[c] = struct{}{}
		dirs = append(dirs, c)
	}
	return dirs
}

// trimGoCacheDir deletes regular files inside dir's two-hex subdirectories
// whose mtime predates now-maxAge — the same rule as Go's cache.Trim (mtime
// is last-use; a delete is a cache miss). Root files, non-hex entries,
// symlinks and nested directories are never touched: DirEntry.Type() reports
// lstat mode, so a symlinked "ab" is neither a dir nor a regular file and a
// symlinked entry is never followed to its target.
func trimGoCacheDir(dir string, maxAge time.Duration, now time.Time) goCacheTrimStats {
	var st goCacheTrimStats
	entries, err := os.ReadDir(dir)
	if err != nil {
		st.errs++
		st.firstErr = err
		return st
	}
	cutoff := now.Add(-maxAge)
	for _, e := range entries {
		if !e.IsDir() || !goCacheSubdirPattern.MatchString(e.Name()) {
			continue
		}
		trimGoCacheSubdir(filepath.Join(dir, e.Name()), cutoff, &st)
	}
	return st
}

// trimGoCacheSubdir applies the age rule inside one hex subdir — no descent
// beyond it, no symlink following (entries are filtered on lstat type before
// Info() is ever called).
func trimGoCacheSubdir(sub string, cutoff time.Time, st *goCacheTrimStats) {
	entries, err := os.ReadDir(sub)
	if err != nil {
		st.errs++
		if st.firstErr == nil {
			st.firstErr = err
		}
		return
	}
	for _, e := range entries {
		if !e.Type().IsRegular() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			st.errs++
			if st.firstErr == nil {
				st.firstErr = err
			}
			continue
		}
		if !info.ModTime().Before(cutoff) {
			continue
		}
		if err := os.Remove(filepath.Join(sub, e.Name())); err != nil {
			st.errs++
			if st.firstErr == nil {
				st.firstErr = err
			}
			continue
		}
		st.files++
		st.bytes += info.Size()
	}
}

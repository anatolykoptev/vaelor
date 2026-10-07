// Package memlimit derives the process memory limit from the container and
// applies it as the Go runtime's soft memory limit.
//
// Without a soft limit the GC lets the heap grow to GOGC-times the live set
// regardless of how close the cgroup limit is, so a transient spike (a typed
// go/packages load) is killed by the kernel instead of triggering a collection.
package memlimit

import (
	"fmt"
	"log/slog"
	"os"
	"runtime/debug"
	"strconv"
	"strings"
)

const (
	// EnvLimit overrides cgroup detection (bytes, or a number with a KiB/MiB/GiB suffix).
	EnvLimit = "VAELOR_MEMORY_LIMIT"

	// headroomPercent is the share of the limit given to the Go heap; the rest
	// covers non-heap memory (stacks, cgo, child processes such as `go list`).
	headroomPercent = 85

	cgroupV2Max = "/sys/fs/cgroup/memory.max"
	cgroupV1Max = "/sys/fs/cgroup/memory/memory.limit_in_bytes"

	// cgroup v1 reports "no limit" as a huge page-rounded number.
	noLimitFloor = int64(1) << 60
)

// env abstracts the process environment and filesystem for tests.
type env struct {
	getenv   func(string) string
	readFile func(string) ([]byte, error)
}

var osEnv = env{getenv: os.Getenv, readFile: os.ReadFile}

// Detect returns the container memory limit in bytes and where it came from, or
// 0 when no limit is detectable. VAELOR_MEMORY_LIMIT wins over the cgroup.
func Detect() (limit int64, source string) { return osEnv.detect() }

func (e env) detect() (int64, string) {
	if v := strings.TrimSpace(e.getenv(EnvLimit)); v != "" {
		if n, err := ParseSize(v); err == nil && n > 0 {
			return n, EnvLimit
		}
		slog.Warn("memlimit: ignoring unparsable "+EnvLimit, "value", v)
	}
	for _, path := range []string{cgroupV2Max, cgroupV1Max} {
		raw, err := e.readFile(path)
		if err != nil {
			continue
		}
		s := strings.TrimSpace(string(raw))
		if s == "" || s == "max" {
			continue
		}
		if n, err := strconv.ParseInt(s, 10, 64); err == nil && n > 0 && n < noLimitFloor {
			return n, path
		}
	}
	return 0, ""
}

// ParseSize parses a byte count with an optional binary or decimal suffix.
func ParseSize(s string) (int64, error) {
	s = strings.TrimSpace(s)
	mult := int64(1)
	for _, u := range []struct {
		suffix string
		mult   int64
	}{
		{"GiB", 1 << 30}, {"MiB", 1 << 20}, {"KiB", 1 << 10},
		{"GB", 1 << 30}, {"MB", 1 << 20}, {"KB", 1 << 10},
		{"G", 1 << 30}, {"M", 1 << 20}, {"K", 1 << 10},
	} {
		if strings.HasSuffix(s, u.suffix) {
			mult = u.mult
			s = strings.TrimSpace(strings.TrimSuffix(s, u.suffix))
			break
		}
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("memlimit: parse %q: %w", s, err)
	}
	return n * mult, nil
}

// Apply sets the runtime soft memory limit to 85% of the detected limit and logs
// it once. It does nothing — and says so — when GOMEMLIMIT is set explicitly
// (the operator already chose) or when no limit is detectable (a soft limit
// derived from nothing would throttle the GC against an imaginary ceiling).
// It returns the limit applied, or 0.
func Apply() int64 { return osEnv.apply(debug.SetMemoryLimit) }

func (e env) apply(set func(int64) int64) int64 {
	if v := e.getenv("GOMEMLIMIT"); v != "" {
		slog.Info("memlimit: GOMEMLIMIT set explicitly, leaving the runtime limit alone", "GOMEMLIMIT", v)
		return 0
	}
	limit, source := e.detect()
	if limit == 0 {
		return 0
	}
	soft := limit / 100 * headroomPercent
	set(soft)
	slog.Info("memlimit: runtime soft memory limit set",
		"limit_bytes", limit, "source", source, "soft_limit_bytes", soft)
	return soft
}

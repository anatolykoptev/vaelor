// Package memlimit detects the container memory limit (VAELOR_MEMORY_LIMIT, else
// the cgroup) so memory budgets can be derived from it.
//
// It deliberately does NOT set the Go runtime soft limit (GOMEMLIMIT): the
// deployment already does, explicitly, and a limit derived here would be a
// second, silently different source of truth.
package memlimit

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
)

const (
	// EnvLimit overrides cgroup detection (bytes, or a number with a KiB/MiB/GiB suffix).
	EnvLimit = "VAELOR_MEMORY_LIMIT"

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

// ParseSize parses a byte count with an optional binary suffix (KiB, MiB, GiB,
// or K, M, G for the same). Decimal KB/MB/GB are rejected rather than silently
// read as powers of two.
func ParseSize(s string) (int64, error) {
	s = strings.TrimSpace(s)
	mult := int64(1)
	for _, u := range []struct {
		suffix string
		mult   int64
	}{
		{"GiB", 1 << 30}, {"MiB", 1 << 20}, {"KiB", 1 << 10},
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

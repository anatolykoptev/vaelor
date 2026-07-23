package codegraph

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Memory thresholds for graph build gating.
// Tuned for the host-a production box: 4-core ARM, 24GB RAM, ~15 containers.
// The build itself uses ~200MB Go-heap + postgres working set for AGE inserts.
// With 3GB floor we leave headroom for other containers and the OS page cache.
const (
	// minMemAvailableBytes is the minimum /proc/meminfo MemAvailable (bytes)
	// required to start a graph build. Below this the OOM killer is likely
	// to target postgres (2GB cgroup) during AGE bulk inserts.
	minMemAvailableBytes uint64 = 3 * 1024 * 1024 * 1024 // 3 GiB

	// maxPSIAvg10 is the maximum /proc/pressure/memory "some avg10" value
	// (0-100) tolerated before refusing a build. >20 means processes are
	// spending >20% of wall-clock time waiting on memory for 10+ seconds.
	maxPSIAvg10 float64 = 20.0
)

// MemoryStatus describes the host's memory pressure at a point in time.
type MemoryStatus struct {
	AvailableBytes uint64
	PSIAvg10       float64
	PSIAvg60       float64
	Sufficient     bool
	Reason         string // non-empty when Sufficient=false
}

// CheckMemoryPressure reads /proc/meminfo and /proc/pressure/memory to decide
// whether it is safe to start a memory-intensive graph build.
//
// On non-Linux platforms (or when the files are unreadable, e.g. inside a
// container without /proc mounted) it returns Sufficient=true — the guard is
// a no-op so tests and dev environments are not blocked.
func CheckMemoryPressure() MemoryStatus {
	if runtime.GOOS != "linux" {
		return MemoryStatus{Sufficient: true}
	}

	avail, err := readMemAvailable()
	if err != nil {
		// /proc not mounted (e.g. macOS dev, restricted container) — don't block.
		slog.Debug("codegraph: memguard: cannot read /proc/meminfo, skipping", slog.Any("error", err))
		return MemoryStatus{Sufficient: true}
	}

	psi, err := readMemoryPSI()
	if err != nil {
		// PSI may be unavailable on older kernels or cgroup v1 — proceed with
		// meminfo-only gate.
		slog.Debug("codegraph: memguard: cannot read /proc/pressure/memory, using meminfo only", slog.Any("error", err))
		psi = memoryPSI{} // zero values → no PSI gate
	}

	return checkMemoryFromRaw(avail, psi)
}

// memoryPSI holds parsed /proc/pressure/memory fields.
type memoryPSI struct {
	avg10 float64
	avg60 float64
}

// readMemAvailable parses /proc/meminfo and returns MemAvailable in bytes.
func readMemAvailable() (uint64, error) {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, fmt.Errorf("read /proc/meminfo: %w", err)
	}
	return parseMemAvailableFromBytes(data)
}

// parseMemAvailableFromBytes extracts MemAvailable (in bytes) from raw
// /proc/meminfo content. Exported via lowercase for test access within the
// package.
func parseMemAvailableFromBytes(data []byte) (uint64, error) {
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, "MemAvailable:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return 0, fmt.Errorf("parse MemAvailable: unexpected format %q", line)
		}
		val, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			return 0, fmt.Errorf("parse MemAvailable value: %w", err)
		}
		// /proc/meminfo reports in kB.
		return val * 1024, nil
	}
	return 0, fmt.Errorf("MemAvailable line not found in /proc/meminfo")
}

// readMemoryPSI parses /proc/pressure/memory and extracts avg10/avg60 from the
// "some" line (percentage of time at least one task was stalled on memory).
func readMemoryPSI() (memoryPSI, error) {
	data, err := os.ReadFile("/proc/pressure/memory")
	if err != nil {
		return memoryPSI{}, fmt.Errorf("read /proc/pressure/memory: %w", err)
	}
	return parseMemoryPSIFromBytes(data)
}

// parseMemoryPSIFromBytes extracts the "some" avg10/avg60 from raw
// /proc/pressure/memory content.
func parseMemoryPSIFromBytes(data []byte) (memoryPSI, error) {
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, "some ") {
			continue
		}
		return parsePSILine(line)
	}
	return memoryPSI{}, fmt.Errorf("'some' line not found in /proc/pressure/memory")
}

// checkMemoryFromRaw applies the same threshold logic as CheckMemoryPressure
// but on synthetic values — used by tests to verify the gate logic without
// depending on the real /proc state.
func checkMemoryFromRaw(avail uint64, psi memoryPSI) MemoryStatus {
	st := MemoryStatus{
		AvailableBytes: avail,
		PSIAvg10:       psi.avg10,
		PSIAvg60:       psi.avg60,
	}
	if avail < minMemAvailableBytes {
		st.Sufficient = false
		st.Reason = fmt.Sprintf(
			"low memory: MemAvailable=%.1f GiB (need %.1f GiB)",
			float64(avail)/float64(1024*1024*1024),
			float64(minMemAvailableBytes)/float64(1024*1024*1024),
		)
		return st
	}
	if psi.avg10 > maxPSIAvg10 {
		st.Sufficient = false
		st.Reason = fmt.Sprintf(
			"high memory pressure: PSI some avg10=%.1f (need <%.1f)",
			psi.avg10, maxPSIAvg10,
		)
		return st
	}
	st.Sufficient = true
	return st
}

// parsePSILine extracts avg10 and avg60 from a PSI "some" or "full" line.
// Format: "some avg10=0.00 avg60=0.01 avg300=0.06 total=1220065"
func parsePSILine(line string) (memoryPSI, error) {
	var psi memoryPSI
	for _, field := range strings.Fields(line) {
		parts := strings.SplitN(field, "=", 2)
		if len(parts) != 2 {
			continue
		}
		switch parts[0] {
		case "avg10":
			v, err := strconv.ParseFloat(parts[1], 64)
			if err != nil {
				return psi, fmt.Errorf("parse avg10: %w", err)
			}
			psi.avg10 = v
		case "avg60":
			v, err := strconv.ParseFloat(parts[1], 64)
			if err != nil {
				return psi, fmt.Errorf("parse avg60: %w", err)
			}
			psi.avg60 = v
		}
	}
	return psi, nil
}

// memGuardCheckInterval is the polling interval for the background watchdog.
// A var (not const) so tests can shorten it. Guarded by memGuardMu because
// MemGuardWatchdog reads it from a goroutine while a parallel test
// (TestMemGuardWatchdog_CancelsContext) writes it — without synchronization
// that is a data race detected by `go test -race`.
var (
	memGuardCheckInterval = 10 * time.Second
	memGuardMu            sync.RWMutex
)

// memGuardInterval returns the current watchdog polling interval under the
// read lock, so MemGuardWatchdog's goroutine observes a consistent value even
// when a test is mutating memGuardCheckInterval concurrently.
func memGuardInterval() time.Duration {
	memGuardMu.RLock()
	defer memGuardMu.RUnlock()
	return memGuardCheckInterval
}

// setMemGuardInterval sets the watchdog polling interval under the write lock
// and returns the previous value so callers can restore it. Used only by tests
// that need to shorten the interval.
func setMemGuardInterval(d time.Duration) time.Duration {
	memGuardMu.Lock()
	defer memGuardMu.Unlock()
	prev := memGuardCheckInterval
	memGuardCheckInterval = d
	return prev
}

// memGuardWatchdog runs in a goroutine alongside IndexRepo, periodically
// checking memory pressure. If pressure exceeds thresholds, it cancels the
// context via the cancel function so the build aborts gracefully instead of
// pushing the host into OOM.
//
// The watchdog is a second line of defense: the pre-build CheckMemoryPressure
// gate catches the common case (host already under pressure). The watchdog
// catches the case where pressure develops DURING the build (e.g. another
// container spikes while we're inserting).
func MemGuardWatchdog(ctx context.Context, cancel context.CancelFunc) {
	ticker := time.NewTicker(memGuardInterval())
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			st := CheckMemoryPressure()
			if !st.Sufficient {
				slog.Warn("codegraph: memguard watchdog: aborting build due to memory pressure",
					slog.String("reason", st.Reason),
					slog.Uint64("available_bytes", st.AvailableBytes),
					slog.Float64("psi_avg10", st.PSIAvg10))
				cancel()
				return
			}
		}
	}
}

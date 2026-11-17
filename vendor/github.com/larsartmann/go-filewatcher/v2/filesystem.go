package filewatcher

import (
	"fmt"
	"path/filepath"
	"runtime"
	"strings"

	"golang.org/x/text/unicode/norm"
)

// FilesystemCaseSensitivity controls whether path comparisons treat character case.
//
// Different filesystems behave differently: NTFS (Windows) and APFS (macOS) are
// case-insensitive by default, while ext4/XFS/btrfs (Linux) are case-sensitive.
// Choosing the wrong mode can cause duplicate watches, missed Remove() calls,
// or inconsistent debouncing on paths that differ only in case.
type FilesystemCaseSensitivity int

const (
	// CaseSensitivityAuto selects the platform default: case-insensitive on
	// Windows and macOS, case-sensitive everywhere else.
	CaseSensitivityAuto FilesystemCaseSensitivity = iota
	// CaseSensitive treats paths that differ only in case as distinct.
	CaseSensitive
	// CaseInsensitive treats paths that differ only in case as identical.
	CaseInsensitive
)

// String representations for case-sensitivity modes.
// Named constants satisfy goconst (these strings appear in String(), tests, and Stats).
const (
	caseSensitiveStr   = "case-sensitive"
	caseInsensitiveStr = "case-insensitive"
	caseAutoStr        = "auto"
)

// Gauge encoding for the filewatcher_case_sensitivity Prometheus metric.
// Named constants satisfy mnd and document the wire format consumed by dashboards.
// These live with the enum (not in metrics.go) so all representations of
// FilesystemCaseSensitivity are defined in one place.
const (
	gaugeCaseSensitive   = 0.0
	gaugeCaseInsensitive = 1.0
	gaugeCaseAuto        = 2.0
)

// OS name constants used for runtime.GOOS comparisons. Satisfy goconst.
const (
	osWindows = "windows"
	osDarwin  = "darwin"
)

// String returns a human-readable representation of the case-sensitivity mode.
func (c FilesystemCaseSensitivity) String() string {
	switch c {
	case CaseSensitive:
		return caseSensitiveStr
	case CaseInsensitive:
		return caseInsensitiveStr
	case CaseSensitivityAuto:
		return caseAutoStr
	default:
		return categoryStringUnknown
	}
}

// GaugeValue returns the numeric encoding for the Prometheus
// filewatcher_case_sensitivity gauge (0=case-sensitive, 1=case-insensitive,
// 2=auto). This is the single source of truth for the gauge encoding.
func (c FilesystemCaseSensitivity) GaugeValue() float64 {
	switch c {
	case CaseSensitive:
		return gaugeCaseSensitive
	case CaseInsensitive:
		return gaugeCaseInsensitive
	case CaseSensitivityAuto:
		return gaugeCaseAuto
	default:
		return -1
	}
}

// resolveCaseSensitivity returns the effective case-sensitivity mode.
// Auto resolves to case-insensitive on Windows and macOS, case-sensitive elsewhere.
func resolveCaseSensitivity(mode FilesystemCaseSensitivity) FilesystemCaseSensitivity {
	if mode != CaseSensitivityAuto {
		return mode
	}

	if runtime.GOOS == osWindows || runtime.GOOS == osDarwin {
		return CaseInsensitive
	}

	return CaseSensitive
}

// normalizePath resolves a path to an absolute, cleaned form.
// It calls filepath.Abs to make the path absolute, then filepath.Clean to
// eliminate trailing slashes, redundant separators, and ".." / "." components.
// This ensures that Add(), Remove(), event paths, and exclude paths all use
// the same canonical representation, preventing subtle mismatches.
func normalizePath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		// Best-effort: clean the path even when absolute resolution fails (e.g. the
		// working directory is unreadable) so trailing slashes, "..", and redundant
		// separators are still normalized. Callers that require an absolute path
		// must check the returned error.
		return filepath.Clean(path), fmt.Errorf("resolving absolute path for %q: %w", path, err)
	}

	return filepath.Clean(abs), nil
}

// cleanPath returns a cleaned, best-effort canonical path.
// It calls normalizePath but discards the error — use this when you need
// path consistency (trailing slashes, "..", redundant separators removed)
// but do not require the path to be absolute. For strict absolute resolution,
// use normalizePath and check the returned error.
func cleanPath(path string) string {
	cleaned, _ := normalizePath(path) //nolint:erraudit // documented best-effort contract of cleanPath

	return cleaned
}

// pathKey returns a canonical string key for path lookups.
// On case-insensitive filesystems the key is lowercased so that paths differing
// only in case collide in maps and equality checks.
//
// NFC normalization is always applied (even on case-sensitive filesystems) so
// that NFC and NFD representations of the same Unicode path produce identical
// keys. This is critical on macOS, where the filesystem stores filenames as NFD
// (decomposed) but user-configured paths are typically NFC (composed). Without
// normalization, exclude-path matching, debounce deduplication, and gitignore
// prefix checks silently fail on any non-ASCII filename.
// norm.NFC.String is idempotent on ASCII, so pure-ASCII paths are unaffected
// (0 allocations, ~26ns). Pre-composed NFC Unicode input is also allocation-free
// (~140ns). Only decomposed NFD input — emitted by the macOS filesystem —
// allocates (~1µs, 3 allocs, 672B). This is negligible for event-driven watching.
func (w *Watcher) pathKey(path string) string {
	normalized := norm.NFC.String(path)

	if w.effectiveCaseSensitivity == CaseInsensitive {
		return strings.ToLower(normalized)
	}

	return normalized
}

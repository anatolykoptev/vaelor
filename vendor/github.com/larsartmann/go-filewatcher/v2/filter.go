package filewatcher

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	gitignore "github.com/sabhiram/go-gitignore"
)

// Filter determines whether a file event should be processed.
// Return true to keep the event, false to discard it.
type Filter func(event Event) bool

// FilterExtensions creates a filter that only passes events for files
// matching one of the given extensions. Extensions should include the
// dot prefix (e.g., ".go", ".md").
func FilterExtensions(exts ...string) Filter {
	return makeExtFilter(exts, true)
}

// FilterIgnoreExtensions creates a filter that discards events for files
// matching one of the given extensions.
func FilterIgnoreExtensions(exts ...string) Filter {
	return makeExtFilter(exts, false)
}

// makeSetFilter builds a Filter from a set of comparable items.
// extract retrieves the comparison key from an Event.
// include=true means pass events matching the set; include=false means reject them.
func makeSetFilter[T comparable](items []T, extract func(Event) T, include bool) Filter {
	set := make(map[T]struct{}, len(items))
	for _, item := range items {
		set[item] = struct{}{}
	}

	return func(event Event) bool {
		_, found := set[extract(event)]

		return found == include
	}
}

func makeExtFilter(exts []string, include bool) Filter {
	normalized := make([]string, len(exts))
	for i, ext := range exts {
		normalized[i] = strings.ToLower(ext)
	}

	return makeSetFilter(normalized, func(event Event) string {
		return strings.ToLower(filepath.Ext(event.Path))
	}, include)
}

func makeOpFilter(ops []Op, include bool) Filter {
	return makeSetFilter(ops, func(event Event) Op {
		return event.Op
	}, include)
}

// FilterIgnoreDirs creates a filter that discards events for files
// within directories matching any of the given directory names.
// Directory names are matched against path components (e.g., "vendor"
// matches both "vendor" and "pkg/vendor").
func FilterIgnoreDirs(dirs ...string) Filter {
	dirSet := make(map[string]struct{}, len(dirs))
	for _, dir := range dirs {
		dirSet[dir] = struct{}{}
	}

	return func(event Event) bool {
		for part := range dirSet {
			sep := string(filepath.Separator)
			if strings.Contains(event.Path, sep+part+sep) ||
				strings.HasSuffix(event.Path, sep+part) ||
				filepath.Base(event.Path) == part {
				return false
			}
		}

		return true
	}
}

// FilterExcludePaths creates a filter that discards events for files
// matching any of the given exact paths. Paths are matched after
// normalization (absolute path conversion).
//
// This differs from FilterIgnoreDirs which matches directory names anywhere
// in the path. FilterExcludePaths requires exact path matches.
//
// Example:
//
//	// Exclude specific files
//	watcher, _ := filewatcher.New("./src",
//	    filewatcher.WithFilter(filewatcher.FilterExcludePaths(
//	        "/home/user/project/src/generated.go",
//	        "/home/user/project/src/vendor",
//	    )),
//	)
func FilterExcludePaths(paths ...string) Filter {
	pathSet := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		// Normalize to absolute path for consistent matching
		abs, err := filepath.Abs(path)
		if err == nil {
			pathSet[abs] = struct{}{}
		} else {
			// Fall back to original path if Abs fails
			pathSet[path] = struct{}{}
		}
	}

	return func(event Event) bool {
		_, excluded := pathSet[event.Path]

		return !excluded
	}
}

// FilterIgnoreHidden creates a filter that discards events for hidden
// files and directories (those starting with a dot).
func FilterIgnoreHidden() Filter {
	return func(event Event) bool {
		base := filepath.Base(event.Path)
		if strings.HasPrefix(base, ".") {
			return false
		}

		for part := range strings.SplitSeq(event.Path, string(filepath.Separator)) {
			if strings.HasPrefix(part, ".") && part != "." && part != ".." {
				return false
			}
		}

		return true
	}
}

// FilterOperations creates a filter that only passes events matching
// one of the given operations.
func FilterOperations(ops ...Op) Filter {
	return makeOpFilter(ops, true)
}

// FilterNotOperations creates a filter that discards events matching
// any of the given operations.
func FilterNotOperations(ops ...Op) Filter {
	return makeOpFilter(ops, false)
}

// FilterGlob creates a filter that only passes events for files
// matching the given glob pattern.
func FilterGlob(pattern string) Filter {
	return func(event Event) bool {
		matched, err := filepath.Match(pattern, filepath.Base(event.Path))
		if err != nil {
			return false
		}

		return matched
	}
}

// FilterRegex creates a filter that only passes events for paths
// matching the given regular expression pattern. The pattern is
// pre-compiled at creation time for efficiency.
// Panics if the pattern is invalid (use regexp.Compile for runtime validation).
func FilterRegex(pattern string) Filter {
	re := regexp.MustCompile(pattern)

	return func(event Event) bool {
		return re.MatchString(event.Path)
	}
}

// filterFileStat extracts common file stat logic used by size/time filters.
// Returns (info, true, true) if stat succeeded and event is a file.
// Returns (nil, true, false) if event is a directory.
// Returns (nil, false, false) if stat fails.
func filterFileStat(event Event) (os.FileInfo, bool, bool) {
	if event.IsDir {
		return nil, true, false // isFile=false, shouldFilter=false
	}

	info, err := os.Stat(event.Path)
	if err != nil {
		return nil, false, false // stat failed, shouldFilter=false
	}

	return info, true, true // stat succeeded, isFile=true, shouldFilter=true
}

// makeSizeFilter creates a filter that applies a size comparison.
// Use >= for min size, <= for max size.
func makeSizeFilter(threshold int64, isMin bool) Filter {
	return func(event Event) bool {
		info, isFile, shouldFilter := filterFileStat(event)
		if !shouldFilter {
			return isFile // directories pass through (true), stat fails filter out (false)
		}

		if isMin {
			return info.Size() >= threshold
		}

		return info.Size() <= threshold
	}
}

// FilterMinSize creates a filter that only passes events for files
// with size greater than or equal to the given minimum size in bytes.
// Directory events are not filtered by size.
func FilterMinSize(minSize int64) Filter {
	return makeSizeFilter(minSize, true)
}

// FilterMaxSize creates a filter that only passes events for files
// with size less than or equal to the given maximum size in bytes.
// Directory events are not filtered by size.
func FilterMaxSize(maxSize int64) Filter {
	return makeSizeFilter(maxSize, false)
}

// FilterModifiedSince creates a filter that only passes events for files
// modified after the given time. Directory events are not filtered by time.
// Useful for ignoring old files during initial scan.
func FilterModifiedSince(minTime time.Time) Filter {
	return func(event Event) bool {
		info, isFile, shouldFilter := filterFileStat(event)
		if !shouldFilter {
			return isFile // directories pass through (true), stat fails filter out (false)
		}

		return info.ModTime().After(minTime)
	}
}

// FilterMinAge creates a filter that only passes events for files
// that are at least the given age old. Directory events are not filtered.
// Useful for ignoring recently modified files (e.g., during save operations).
func FilterMinAge(age time.Duration) Filter {
	return func(event Event) bool {
		info, isFile, shouldFilter := filterFileStat(event)
		if !shouldFilter {
			return isFile // directories pass through (true), stat fails filter out (false)
		}

		return time.Since(info.ModTime()) >= age
	}
}

// FilterAnd combines multiple filters with AND logic.
// All filters must return true for the event to pass.
func FilterAnd(filters ...Filter) Filter {
	return func(event Event) bool {
		for _, f := range filters {
			if !f(event) {
				return false
			}
		}

		return true
	}
}

// FilterOr combines multiple filters with OR logic.
// At least one filter must return true for the event to pass.
func FilterOr(filters ...Filter) Filter {
	return func(event Event) bool {
		for _, f := range filters {
			if f(event) {
				return true
			}
		}

		return false
	}
}

// FilterNot inverts a filter.
func FilterNot(f Filter) Filter {
	return func(event Event) bool {
		return !f(event)
	}
}

// MatchResult is the result of a filter evaluation that returns metadata.
// It allows filters to communicate WHY an event matched or didn't match,
// which is useful for debugging, logging, and analytics.
type MatchResult struct {
	// Matched indicates whether the event passed the filter.
	Matched bool
	// Reason is a short, human-readable explanation of why the event
	// matched or didn't match (e.g., "extension .go", "in ignored dir").
	// May be empty.
	Reason string
	// FilterName is the name of the filter that produced this result.
	// Useful when composing multiple filters with FilterWithMetaAnd/Or.
	FilterName string
}

// FilterWithMeta is a filter that returns match metadata in addition to
// the boolean match result. This is useful when callers want to log why
// events were kept or dropped (e.g., for debugging, audit logs, or
// metrics on filter behavior).
//
// Use FilterWithMetaAnd/FilterWithMetaOr to compose multiple metadata
// filters.
type FilterWithMeta func(event Event) MatchResult

// FilterFromWithMeta converts a FilterWithMeta to a plain Filter.
// The boolean result is preserved; metadata is discarded.
func FilterFromWithMeta(f FilterWithMeta) Filter {
	return func(event Event) bool {
		return f(event).Matched
	}
}

// FilterWithMetaAnd combines multiple FilterWithMeta with AND logic.
// All filters must return Matched=true. The first filter that reports
// Matched=false short-circuits, returning its result.
func FilterWithMetaAnd(filters ...FilterWithMeta) FilterWithMeta {
	return func(event Event) MatchResult {
		for _, f := range filters {
			result := f(event)
			if !result.Matched {
				return result
			}
		}

		return MatchResult{Matched: true, Reason: "all filters matched", FilterName: "And"}
	}
}

// FilterWithMetaOr combines multiple FilterWithMeta with OR logic.
// At least one filter must return Matched=true. The first matching filter's
// result is returned.
func FilterWithMetaOr(filters ...FilterWithMeta) FilterWithMeta {
	return func(event Event) MatchResult {
		for _, f := range filters {
			result := f(event)
			if result.Matched {
				return result
			}
		}

		return MatchResult{Matched: false, Reason: "no filter matched", FilterName: "Or"}
	}
}

// FilterWithMetaNot inverts a FilterWithMeta.
func FilterWithMetaNot(f FilterWithMeta, name string) FilterWithMeta {
	return func(event Event) MatchResult {
		result := f(event)

		return MatchResult{
			Matched:    !result.Matched,
			Reason:     "NOT(" + result.Reason + ")",
			FilterName: name,
		}
	}
}

// WithMeta wraps a plain Filter with a name and reason, returning a FilterWithMeta.
// The result is always Matched=true if the inner filter passes, with the
// given reason and name attached.
func WithMeta(f Filter, name, reason string) FilterWithMeta {
	return func(event Event) MatchResult {
		return MatchResult{
			Matched:    f(event),
			Reason:     reason,
			FilterName: name,
		}
	}
}

// FilterIgnoreGlobs creates a filter that discards events for files matching
// any of the given glob patterns. This is useful for excluding files by
// pattern (e.g., "*.log", "*.tmp", ".*") at the filter level.
func FilterIgnoreGlobs(patterns ...string) Filter {
	return func(event Event) bool {
		base := filepath.Base(event.Path)

		for _, pattern := range patterns {
			matched, matchErr := filepath.Match(pattern, base)
			if matchErr == nil && matched {
				return false
			}
		}

		return true
	}
}

// FilterContentHash creates a filter that only passes events for files whose
// SHA-256 content hash matches the expected hash. This is useful for detecting
// when a file's content has actually changed, ignoring touch-only modifications.
// Directory events always pass through. Files that cannot be read are filtered out.
func FilterContentHash(expectedHex string) Filter {
	expected := strings.ToLower(strings.TrimSpace(expectedHex))

	return func(event Event) bool {
		if event.IsDir {
			return true
		}

		actual := hashFile(event.Path)
		if actual == "" {
			return false
		}

		return strings.EqualFold(actual, expected)
	}
}

// hashFile computes the hex-encoded SHA-256 hash of the file at path.
// Returns empty string on any error (file missing, permission denied, etc.).
// Files larger than maxHashFileSize are skipped to avoid reading huge files.
func hashFile(path string) string {
	const maxHashFileSize = 10 * 1024 * 1024 // 10 MiB cap

	info, err := os.Stat(path)
	if err != nil || info.IsDir() || info.Size() > maxHashFileSize {
		return ""
	}

	file, err := os.Open(path) //nolint:gosec // path comes from fsnotify event, not user input
	if err != nil {
		return ""
	}

	defer func() { _ = file.Close() }()

	hasher := sha256.New()

	_, copyErr := io.Copy(hasher, file)
	if copyErr != nil {
		return ""
	}

	return hex.EncodeToString(hasher.Sum(nil))
}

// FilterGitignore creates a filter that discards events for paths matching
// .gitignore rules from the specified repo root directory. This provides
// event-level filtering to supplement the walk-time gitignore filtering.
// The .gitignore file is loaded from repoRoot at filter creation time.
// If the .gitignore file cannot be loaded, all events pass through.
func FilterGitignore(repoRoot string) Filter {
	gitignorePath := filepath.Join(repoRoot, ".gitignore")

	ignoreMatcher, err := gitignore.CompileIgnoreFile(gitignorePath)
	if err != nil {
		return func(_ Event) bool { return true }
	}

	return func(event Event) bool {
		relPath, relErr := filepath.Rel(repoRoot, event.Path)
		if relErr != nil {
			return true
		}

		return !ignoreMatcher.MatchesPath(relPath)
	}
}

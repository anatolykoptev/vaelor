package gogenfilter

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"sort"
	"strings"
)

// FilterConfig is a functional option for configuring a Filter.
// Returns an error if the configuration is invalid.
type FilterConfig func(*Filter) error

// WithFilterOptions specifies which generated code types to filter.
// FilterAll expands to all specific detectors plus FilterGeneric.
// Returns an error if any option is not a valid FilterOption.
func WithFilterOptions(opts ...FilterOption) (FilterConfig, error) {
	for _, opt := range opts {
		if !opt.IsValid() {
			return nil, &FilterConfigError{ //nolint:exhaustruct
				Code: CodeInvalidFilterOption, Option: opt,
			}
		}
	}

	return func(filter *Filter) error {
		expanded := optionsMap(opts...)
		for opt := range expanded {
			filter.options[opt] = struct{}{}
		}

		return nil
	}, nil
}

// WithFS sets a custom filesystem for the filter.
// Defaults to os.DirFS(".") if not provided.
func WithFS(fsys fs.FS) FilterConfig {
	return func(filter *Filter) error {
		if fsys != nil {
			filter.fsys = fsys
		}

		return nil
	}
}

// WithIncludePatterns restricts filtering scope to files matching at least one
// of the given patterns. Files that do NOT match any include pattern are
// immediately filtered (excluded from analysis) with reason ReasonOutsideScope.
//
// This "restrict scope" behavior means include patterns act as a whitelist for
// which files are worth inspecting — all other files are skipped.
// Use this to focus filtering on specific directories (e.g., "**/generated/*.go").
//
// Patterns use the ** glob syntax supported by MatchPattern.
// If no include patterns are set, all files are considered.
func WithIncludePatterns(patterns ...string) FilterConfig {
	return func(filter *Filter) error {
		filter.includePatterns = append(filter.includePatterns, patterns...)

		return nil
	}
}

// WithExcludePatterns adds exclude patterns. Files matching any exclude pattern
// are filtered regardless of generated-code detection.
func WithExcludePatterns(patterns ...string) FilterConfig {
	return func(filter *Filter) error {
		filter.excludePatterns = append(filter.excludePatterns, patterns...)

		return nil
	}
}

// Filter provides smart filtering of auto-generated Go code.
// A Filter is immutable after construction — all configuration is applied via NewFilter.
type Filter struct {
	options         map[FilterOption]struct{}
	includePatterns []string
	excludePatterns []string
	fsys            fs.FS
}

// NewFilter creates a new filter configured with the given functional options.
// A filter with no options is disabled — Filter always returns false.
// A filter is enabled when it has filter options, include patterns, or exclude patterns.
//
// Returns an error if any configuration option is invalid.
// Use errors.Is to check for specific error types.
//
// Examples:
//
//	filter, err := NewFilter(WithFilterOptions(FilterAll))
//	filter, err := NewFilter(WithFilterOptions(FilterSQLC, FilterTempl), WithExcludePatterns("**/db/*.go"))
//	filter, err := NewFilter() // disabled, always returns (false, nil)
func NewFilter(configs ...FilterConfig) (*Filter, error) {
	filter := &Filter{
		options:         make(map[FilterOption]struct{}),
		includePatterns: make([]string, 0),
		excludePatterns: make([]string, 0),
		fsys:            os.DirFS("."),
	}

	var errs []error

	for _, cfg := range configs {
		if cfg == nil {
			continue
		}

		err := cfg(filter)
		if err != nil {
			errs = append(errs, err)
		}
	}

	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}

	return filter, nil
}

// IsEnabled returns whether the filter is active.
// A filter is enabled when it has filter options, include patterns, or exclude patterns.
func (f *Filter) IsEnabled() bool {
	return len(f.options) > 0 || len(f.includePatterns) > 0 || len(f.excludePatterns) > 0
}

// FilterReasons returns the FilterReason values that this filter will detect.
// Each enabled FilterOption maps to its corresponding FilterReason.
// Meta-options (FilterAll) that don't map to a specific reason are skipped.
func (f *Filter) FilterReasons() []FilterReason {
	reasons := make([]FilterReason, 0, len(f.options))

	for opt := range f.options {
		if reason, ok := opt.Reason(); ok {
			reasons = append(reasons, reason)
		}
	}

	return reasons
}

// Filter determines if a file should be filtered out (excluded from analysis).
// Returns an error if the file could not be read for content-based detection.
func (f *Filter) Filter(filePath string) (bool, error) {
	if !f.IsEnabled() {
		return false, nil
	}

	if len(f.includePatterns) > 0 {
		return f.shouldFilterWithIncludes(filePath)
	}

	return f.shouldFilterWithExcludes(filePath)
}

// FilterDetailed is like Filter but returns a FilterResult with detailed information
// about why the file was or wasn't filtered, including a human-readable trace string.
//
// Example:
//
//	filter, _ := NewFilter(WithFilterOptions(FilterSQLC))
//	result, err := filter.FilterDetailed("db/models.go")
//	if err != nil { log.Fatal(err) }
//	if result.Filtered {
//	    fmt.Printf("filtered: reason=%s trace=%s\n", result.Reason, result.Trace)
//	}
func (f *Filter) FilterDetailed(filePath string) (FilterResult, error) {
	if !f.IsEnabled() {
		return FilterResult{Filtered: false, Reason: "", Path: filePath, Trace: ""}, nil
	}

	if len(f.includePatterns) > 0 {
		return f.shouldFilterDetailedWithIncludes(filePath)
	}

	return f.shouldFilterDetailedWithExcludes(filePath)
}

// FilterPathsDetailed is like FilterPaths but returns FilterResult values with
// detailed information about each file.
func (f *Filter) FilterPathsDetailed(paths []string) ([]FilterResult, error) {
	results := make([]FilterResult, 0, len(paths))

	for _, path := range paths {
		result, err := f.FilterDetailed(path)
		if err != nil {
			return results, err
		}

		results = append(results, result)
	}

	return results, nil
}

// FilterPaths filters multiple file paths in batch, returning a slice of booleans
// indicating whether each file should be filtered.
// If an error occurs on any path, partial results collected so far and the error are returned.
//
// Example:
//
//	filter, _ := NewFilter(WithIncludePatterns("pkg/*.go"))
//	results, err := filter.FilterPaths([]string{"pkg/main.go", "vendor/util.go", "pkg/handler.go"})
//	// results: [false, true, false] — vendor/util.go filtered as outside scope
func (f *Filter) FilterPaths(paths []string) ([]bool, error) {
	results := make([]bool, 0, len(paths))

	for _, path := range paths {
		filtered, err := f.Filter(path)
		if err != nil {
			return results, err
		}

		results = append(results, filtered)
	}

	return results, nil
}

func (f *Filter) matchesAnyPattern(filePath string, patterns []string) bool {
	return anyMatch(filePath, patterns, MatchPattern)
}

func (f *Filter) shouldFilterWithIncludes(filePath string) (bool, error) {
	return f.shouldFilterByPattern(
		filePath,
		!f.matchesAnyPattern(filePath, f.includePatterns),
	)
}

func (f *Filter) shouldFilterWithExcludes(filePath string) (bool, error) {
	return f.shouldFilterByPattern(
		filePath,
		f.matchesAnyPattern(filePath, f.excludePatterns),
	)
}

func (f *Filter) shouldFilterByPattern(
	filePath string,
	patternMatched bool,
) (bool, error) {
	if patternMatched {
		return true, nil
	}

	return f.shouldFilterByDetection(filePath)
}

func (f *Filter) shouldFilterByDetection(filePath string) (bool, error) {
	reason, err := detectReasonFS(f.fsys, filePath, f.options)
	if err != nil {
		return false, err
	}

	return reason != ReasonNotFiltered, nil
}

func (f *Filter) shouldFilterDetailedWithIncludes(filePath string) (FilterResult, error) {
	return f.shouldFilterDetailedByPattern(
		filePath,
		!f.matchesAnyPattern(filePath, f.includePatterns),
		ReasonOutsideScope,
		"excluded by include pattern scope",
	)
}

func (f *Filter) shouldFilterDetailedWithExcludes(filePath string) (FilterResult, error) {
	return f.shouldFilterDetailedByPattern(
		filePath,
		f.matchesAnyPattern(filePath, f.excludePatterns),
		ReasonExcludePattern,
		"matched exclude pattern",
	)
}

func (f *Filter) shouldFilterDetailedByPattern(
	filePath string,
	patternMatched bool,
	reason FilterReason,
	trace string,
) (FilterResult, error) {
	if patternMatched {
		return FilterResult{
			Filtered: true,
			Reason:   reason,
			Path:     filePath,
			Trace:    trace,
		}, nil
	}

	return f.shouldFilterDetailedByDetection(filePath)
}

func (f *Filter) shouldFilterDetailedByDetection(filePath string) (FilterResult, error) {
	return detectReasonFSWithTrace(f.fsys, filePath, f.options)
}

func (f *Filter) appendPatternPart(parts []string, label string, patterns []string) []string {
	if len(patterns) > 0 {
		return append(parts, fmt.Sprintf("%s=[%s]", label, strings.Join(patterns, ",")))
	}

	return parts
}

// String returns a human-readable representation of the filter configuration.
func (f *Filter) String() string {
	if !f.IsEnabled() {
		return "Filter(disabled)"
	}

	opts := make([]string, 0, len(f.options))
	for opt := range f.options {
		opts = append(opts, string(opt))
	}

	sort.Strings(opts)

	var parts []string

	if len(opts) > 0 {
		parts = []string{fmt.Sprintf("options=[%s]", strings.Join(opts, ","))}
	}

	parts = f.appendPatternPart(parts, "includes", f.includePatterns)
	parts = f.appendPatternPart(parts, "excludes", f.excludePatterns)

	return fmt.Sprintf("Filter(%s)", strings.Join(parts, ", "))
}

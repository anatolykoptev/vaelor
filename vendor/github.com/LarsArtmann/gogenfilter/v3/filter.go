package gogenfilter

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"sort"
	"strings"
	"sync/atomic"
)

// FilterConfig is a functional option for configuring a Filter.
// It is a closure that mutates the Filter during construction.
// Errors are collected and joined by NewFilter after all configs are applied.
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

	// sqlcDerived lazily holds the distilled sqlc configuration (output dirs +
	// accepted filenames) for the project. It is computed once (see
	// sqlcDerivedConfigForFS) and cached for the filter's lifetime.
	// A nil pointer means "not yet computed".
	sqlcDerived atomic.Pointer[sqlcDerivedConfig]
}

// NewFilter creates a new filter configured with the given functional options.
// A filter with no options is disabled — Filter always returns (false, nil).
// A filter is enabled when it has filter options, include patterns, or exclude patterns.
//
// Returns an error if any configuration option is invalid.
// Use errors.Is to check for specific error types.
//
// Examples:
//
//	opts, err := WithFilterOptions(FilterAll)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	filter, err := NewFilter(opts)
//
//	opts, err = WithFilterOptions(FilterSQLC, FilterTempl)
//	filter, err = NewFilter(opts, WithExcludePatterns("**/db/*.go"))
//
//	filter, err = NewFilter() // disabled, always returns (false, nil)
func NewFilter(configs ...FilterConfig) (*Filter, error) {
	//nolint:exhaustruct // sqlcDerived is atomic.Pointer, zero-valued by design
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
		return nil, &FilterConfigError{ //nolint:exhaustruct
			Code: CodeInvalidFilterOption,
			Err:  errors.Join(errs...),
		}
	}

	return filter, nil
}

// sqlcDerivedConfig lazily returns the distilled sqlc config, or an empty
// derived config (never nil) if no sqlc config exists or it can't be parsed.
// The result is cached after the first computation.
func (f *Filter) sqlcDerivedConfig() *sqlcDerivedConfig {
	if cached := f.sqlcDerived.Load(); cached != nil {
		return cached
	}

	derived, err := sqlcDerivedConfigForFS(f.fsys, []string{"."})
	if err != nil {
		derived = &sqlcDerivedConfig{dirFiles: make(map[string]map[string]bool)}
	}

	// Best-effort: cache whatever we computed; a parse error just voids the
	// config signal (falls back to header-content detection).
	f.sqlcDerived.CompareAndSwap(nil, derived)

	return derived
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
//	opts, err := WithFilterOptions(FilterSQLC)
//	filter, err := NewFilter(opts)
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

// FilterWithContent is like Filter but accepts pre-read file content.
// Use this when you already have the file content (e.g., from an AST parser)
// to avoid redundant file I/O. When content is nil or empty, only filename-based
// detection is performed.
func (f *Filter) FilterWithContent(filePath string, content []byte) (bool, error) {
	if !f.IsEnabled() {
		return false, nil
	}

	if len(f.includePatterns) > 0 && !f.matchesAnyPattern(filePath, f.includePatterns) {
		return true, nil
	}

	if f.matchesAnyPattern(filePath, f.excludePatterns) {
		return true, nil
	}

	return f.shouldFilterByContent(filePath, content)
}

// FilterDetailedWithContent is like FilterDetailed but accepts pre-read file content.
// Use this when you already have the file content to avoid redundant file I/O.
func (f *Filter) FilterDetailedWithContent(filePath string, content []byte) (FilterResult, error) {
	if !f.IsEnabled() {
		return FilterResult{Filtered: false, Reason: "", Path: filePath, Trace: ""}, nil
	}

	if len(f.includePatterns) > 0 && !f.matchesAnyPattern(filePath, f.includePatterns) {
		return FilterResult{
			Filtered: true, Reason: ReasonOutsideScope,
			Path: filePath, Trace: "excluded by include pattern scope",
		}, nil
	}

	if f.matchesAnyPattern(filePath, f.excludePatterns) {
		return FilterResult{
			Filtered: true, Reason: ReasonExcludePattern,
			Path: filePath, Trace: "matched exclude pattern",
		}, nil
	}

	return f.shouldFilterDetailedByContent(filePath, content)
}

// FilterDetailedAndContent is like FilterDetailed but also returns the file
// content if it was read during phase-2 detection. The content is nil when:
//   - The filter is disabled
//   - The file is caught by an include or exclude pattern
//   - The file is caught by filename detection (phase 1)
//   - No enabled detector requires content checks
//
// When content is non-nil, it was read exactly once from the filter's filesystem
// and can be reused by the caller for additional checks without re-reading the
// file. This avoids the double-read that FilterDetailed forces on callers that
// need the content for post-detection logic (e.g., classifying a ReasonGeneric
// hit into a specific generator category).
//
// Unlike FilterDetailedWithContent, which requires the caller to supply
// pre-read content, FilterDetailedAndContent performs the lazy read itself
// and returns the content it read.
//
// Example:
//
//	opts, err := WithFilterOptions(FilterAll)
//	filter, err := NewFilter(opts)
//	result, content, err := filter.FilterDetailedAndContent("repository.go")
//	if err != nil { log.Fatal(err) }
//	if result.Filtered && result.Reason == ReasonGeneric && isSQLC(content) {
//	    // rescue this file from the generic catch-all
//	}
func (f *Filter) FilterDetailedAndContent(filePath string) (FilterResult, []byte, error) {
	if !f.IsEnabled() {
		return FilterResult{Filtered: false, Reason: "", Path: filePath, Trace: ""}, nil, nil
	}

	if len(f.includePatterns) > 0 && !f.matchesAnyPattern(filePath, f.includePatterns) {
		return FilterResult{
			Filtered: true, Reason: ReasonOutsideScope,
			Path: filePath, Trace: "excluded by include pattern scope",
		}, nil, nil
	}

	if f.matchesAnyPattern(filePath, f.excludePatterns) {
		return FilterResult{
			Filtered: true, Reason: ReasonExcludePattern,
			Path: filePath, Trace: "matched exclude pattern",
		}, nil, nil
	}

	if result, found := f.configOrFilenameResult(filePath); found {
		return result, nil, nil
	}

	if !needsContentCheck(f.options) {
		return FilterResult{
			Filtered: false,
			Reason:   ReasonNotFiltered,
			Path:     filePath,
			Trace:    "",
		}, nil, nil
	}

	content, err := readFile(f.fsys, filePath)
	if err != nil {
		return FilterResult{
			Filtered: false, Reason: ReasonNotFiltered, Path: filePath, Trace: "",
		}, nil, err
	}

	reason, trace := getContentBasedReasonWithTrace(filePath, string(content), f.options)
	if reason != ReasonNotFiltered {
		return FilterResult{
			Filtered: true,
			Reason:   reason,
			Path:     filePath,
			Trace:    trace,
		}, content, nil
	}

	return FilterResult{
		Filtered: false,
		Reason:   ReasonNotFiltered,
		Path:     filePath,
		Trace:    "",
	}, content, nil
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

// isOptionEnabled reports whether the given filter option is enabled.
func (f *Filter) isOptionEnabled(opt FilterOption) bool {
	_, enabled := f.options[opt]

	return enabled
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
	result, err := detectReasonFSWithConfig(f.fsys, filePath, f.options, f.sqlcDerivedConfig())
	if err != nil {
		return false, err
	}

	return result.Filtered, nil
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
	return detectReasonFSWithConfig(f.fsys, filePath, f.options, f.sqlcDerivedConfig())
}

// configOrFilenameResult checks config-aware SQLC detection then filename-based
// detection. Returns the result and true if the file is filtered without needing
// to read content. Shared by FilterDetailedAndContent and shouldFilterDetailedByContent.
func (f *Filter) configOrFilenameResult(filePath string) (FilterResult, bool) {
	if reason, ok := configAwareSQLCReason(
		filePath,
		f.sqlcDerivedConfig(),
	); ok &&
		f.isOptionEnabled(FilterSQLC) {
		return FilterResult{
			Filtered: true, Reason: reason, Path: filePath,
			Trace: sqlcConfigTrace,
		}, true
	}

	reason, trace := getFilenameBasedReasonWithTrace(filePath, f.options)
	if reason != ReasonNotFiltered {
		return FilterResult{Filtered: true, Reason: reason, Path: filePath, Trace: trace}, true
	}

	return FilterResult{
		Filtered: false,
		Reason:   ReasonNotFiltered,
		Path:     filePath,
		Trace:    "",
	}, false
}

func (f *Filter) shouldFilterByContent(filePath string, content []byte) (bool, error) {
	if reason, ok := configAwareSQLCReason(
		filePath,
		f.sqlcDerivedConfig(),
	); ok &&
		f.isOptionEnabled(FilterSQLC) {
		return reason != ReasonNotFiltered, nil
	}

	reason := detectReasonFromMap(filePath, string(content), f.options)

	return reason != ReasonNotFiltered, nil
}

func (f *Filter) shouldFilterDetailedByContent(
	filePath string,
	content []byte,
) (FilterResult, error) {
	if result, found := f.configOrFilenameResult(filePath); found {
		return result, nil
	}

	if len(content) > 0 {
		reason, trace := getContentBasedReasonWithTrace(filePath, string(content), f.options)
		if reason != ReasonNotFiltered {
			return FilterResult{Filtered: true, Reason: reason, Path: filePath, Trace: trace}, nil
		}
	}

	return FilterResult{Filtered: false, Reason: ReasonNotFiltered, Path: filePath, Trace: ""}, nil
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

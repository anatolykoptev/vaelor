package filewatcher

import (
	"os"

	"github.com/LarsArtmann/gogenfilter/v3"
)

// ContentCheckMode controls whether generated code detection reads file content.
// This eliminates boolean blindness at call sites.
type ContentCheckMode int

const (
	// ContentCheckDisabled uses only filename-based detection (zero I/O).
	ContentCheckDisabled ContentCheckMode = iota
	// ContentCheckEnabled uses filename + content detection (may read files).
	ContentCheckEnabled
)

// FilterGeneratedCode creates a filewatcher filter that excludes auto-generated
// Go code files detected by gogenfilter using filename-based detection only.
// This is zero-I/O and suitable for most file watching scenarios.
//
// For content-based detection, use FilterGeneratedCodeFull with ContentCheckEnabled.
//
// Example:
//
//	watcher, _ := filewatcher.New("./src",
//	    filewatcher.WithFilter(filewatcher.FilterGeneratedCode(
//	        gogenfilter.FilterSQLC,
//	        gogenfilter.FilterProtobuf,
//	    )),
//	)
//
// This filter returns false for generated files (excluding them from events),
// and true for non-generated files (allowing them through).
func FilterGeneratedCode(options ...gogenfilter.FilterOption) Filter {
	return FilterGeneratedCodeFull(ContentCheckDisabled, options...)
}

// buildGogenFilterOptions defaults to FilterAll when no options provided.
// v3's DetectReason handles FilterAll expansion natively, so no manual expansion needed.
func buildGogenFilterOptions(options []gogenfilter.FilterOption) []gogenfilter.FilterOption {
	if len(options) == 0 {
		return []gogenfilter.FilterOption{gogenfilter.FilterAll}
	}

	return options
}

// FilterGeneratedCodeFull creates a filter with configurable content checking.
//
// Use ContentCheckDisabled for only filename-based detection (zero I/O).
// Use ContentCheckEnabled for filename + content detection (may read files).
//
// Content checking is more accurate but requires file I/O. For file watching
// scenarios, filename-only detection is usually sufficient since generated
// files typically have distinctive naming patterns.
//
// The filter returns true to keep (non-generated) files, false to discard
// (generated) files.
func FilterGeneratedCodeFull(mode ContentCheckMode, options ...gogenfilter.FilterOption) Filter {
	filterOpts := buildGogenFilterOptions(options)

	return func(event Event) bool {
		if event.IsDir {
			return true
		}

		reason := gogenfilter.DetectReason(event.Path, "", filterOpts...)
		if reason != gogenfilter.ReasonNotFiltered {
			return false
		}

		if mode == ContentCheckEnabled {
			content, err := os.ReadFile(event.Path)
			if err == nil {
				reason = gogenfilter.DetectReason(event.Path, string(content), filterOpts...)
				if reason != gogenfilter.ReasonNotFiltered {
					return false
				}
			}
		}

		return true
	}
}

// FilterGeneratedCodeWithFilter creates a filter using an existing
// gogenfilter.Filter instance. This allows for more advanced configuration
// including custom filesystems and include/exclude patterns.
//
// Example:
//
//	config, _ := gogenfilter.WithFilterOptions(gogenfilter.FilterAll)
//	genFilter, _ := gogenfilter.NewFilter(config)
//
//	watcher, _ := filewatcher.New("./src",
//	    filewatcher.WithFilter(filewatcher.FilterGeneratedCodeWithFilter(genFilter)),
//	)
func FilterGeneratedCodeWithFilter(genFilter *gogenfilter.Filter) Filter {
	return func(event Event) bool {
		if event.IsDir {
			return true
		}

		shouldFilter, err := genFilter.Filter(event.Path)
		if err != nil {
			return true
		}

		return !shouldFilter
	}
}

// GeneratedCodeDetector provides a reusable detector for generated code.
// Useful when you need to check files outside of the event filter context.
type GeneratedCodeDetector struct {
	options []gogenfilter.FilterOption
}

// NewGeneratedCodeDetector creates a new detector with the specified options.
func NewGeneratedCodeDetector(options ...gogenfilter.FilterOption) *GeneratedCodeDetector {
	return &GeneratedCodeDetector{options: buildGogenFilterOptions(options)}
}

// IsGenerated checks if a file path represents generated code using
// filename-based detection only (zero I/O).
func (d *GeneratedCodeDetector) IsGenerated(filePath string) bool {
	reason := gogenfilter.DetectReason(filePath, "", d.options...)

	return reason != gogenfilter.ReasonNotFiltered
}

// IsGeneratedWithContent checks if a file is generated using both
// filename and content detection.
func (d *GeneratedCodeDetector) IsGeneratedWithContent(filePath, content string) bool {
	reason := gogenfilter.DetectReason(filePath, content, d.options...)

	return reason != gogenfilter.ReasonNotFiltered
}

// GetReason returns the specific reason why a file was detected as generated,
// or gogenfilter.ReasonNotFiltered if it's not generated.
func (d *GeneratedCodeDetector) GetReason(filePath string) gogenfilter.FilterReason {
	return gogenfilter.DetectReason(filePath, "", d.options...)
}

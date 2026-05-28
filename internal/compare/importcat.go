package compare

import (
	"sort"
	"strings"
)

// ImportCategory classifies an import path.
type ImportCategory string

const (
	// ImportStdlib marks standard library imports.
	ImportStdlib ImportCategory = "stdlib"

	// ImportInternal marks project-relative (internal) imports.
	ImportInternal ImportCategory = "internal"

	// ImportExternal marks third-party/external imports.
	ImportExternal ImportCategory = "external"
)

// CategorizeImport classifies an import path as stdlib, internal, or external
// for the given language.
func CategorizeImport(imp, language string) ImportCategory {
	switch strings.ToLower(language) {
	case "go":
		return categorizeGoImport(imp)
	case "python":
		return categorizePythonImport(imp)
	case "javascript", "typescript":
		return categorizeJSImport(imp)
	case "cpp", "c":
		return categorizeCppImport(imp)
	case "kotlin":
		return categorizeKotlinImport(imp)
	case "swift":
		return categorizeSwiftImport(imp)
	default:
		return ImportExternal
	}
}

func categorizeGoImport(imp string) ImportCategory {
	// Go stdlib packages never contain a dot in the first path element.
	if !strings.Contains(imp, ".") {
		return ImportStdlib
	}
	return ImportExternal
}

func categorizePythonImport(imp string) ImportCategory {
	if strings.HasPrefix(imp, ".") {
		return ImportInternal
	}
	if pythonStdlib[imp] {
		return ImportStdlib
	}
	top := strings.SplitN(imp, ".", 2)[0]
	if pythonStdlib[top] {
		return ImportStdlib
	}
	return ImportExternal
}

func categorizeJSImport(imp string) ImportCategory {
	if strings.HasPrefix(imp, "./") || strings.HasPrefix(imp, "../") {
		return ImportInternal
	}
	if nodeBuiltins[imp] {
		return ImportStdlib
	}
	return ImportExternal
}

// kotlinStdlibPrefixes covers Kotlin/Java standard library and Android framework
// import prefixes that should be classified as stdlib rather than external.
var kotlinStdlibPrefixes = []string{
	"kotlin.",
	"kotlinx.",
	"java.",
	"javax.",
	"android.",
	"androidx.",
}

func categorizeKotlinImport(imp string) ImportCategory {
	for _, prefix := range kotlinStdlibPrefixes {
		if strings.HasPrefix(imp, prefix) {
			return ImportStdlib
		}
	}
	return ImportExternal
}

// swiftStdlibPrefixes covers Apple SDK and Swift standard library import names
// that should be classified as stdlib rather than external.
// Swift imports are bare module names (not dot-separated paths), so we match exact
// names or known prefix families.
var swiftStdlibPrefixes = []string{
	"Swift",
	"Foundation",
	"UIKit",
	"SwiftUI",
	"AppKit",
	"Combine",
	"CoreData",
	"CoreGraphics",
	"CoreText",
	"CoreImage",
	"CoreLocation",
	"CoreMotion",
	"CoreBluetooth",
	"AVFoundation",
	"Accelerate",
	"MapKit",
	"WebKit",
	"XCTest",
	"Network",
	"OSLog",
	"Dispatch",
}

func categorizeSwiftImport(imp string) ImportCategory {
	for _, prefix := range swiftStdlibPrefixes {
		if imp == prefix || strings.HasPrefix(imp, prefix+".") {
			return ImportStdlib
		}
	}
	return ImportExternal
}

// DetectFrameworks returns a sorted list of framework names detected from the
// given import paths for the specified language.
func DetectFrameworks(imports []string, language string) []string {
	patterns, ok := frameworkPatterns[strings.ToLower(language)]
	if !ok {
		return nil
	}

	seen := make(map[string]bool)
	for _, imp := range imports {
		for _, pattern := range patterns {
			parts := strings.SplitN(pattern, ":", 2)
			prefix, name := parts[0], parts[1]
			if strings.HasPrefix(imp, prefix) && !seen[name] {
				seen[name] = true
			}
		}
	}

	if len(seen) == 0 {
		return nil
	}

	result := make([]string, 0, len(seen))
	for name := range seen {
		result = append(result, name)
	}
	sort.Strings(result)
	return result
}

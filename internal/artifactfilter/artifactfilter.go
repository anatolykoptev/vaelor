// Package artifactfilter provides a stdlib-only helper for detecting compiled
// build artifacts that should be excluded from source-level analyses such as
// coupling, co-change, and dead-code detection.
package artifactfilter

import (
	"path/filepath"
	"strings"
)

// compiledArtifactDirs are directory name components that indicate build output.
// Files under these paths are not source code and should be excluded from analysis.
var compiledArtifactDirs = map[string]bool{
	"_app":        true, // SvelteKit: web/_app/, assets/_app/
	"immutable":   true, // SvelteKit: _app/immutable/
	"dist":        true,
	".svelte-kit": true,
	".next":       true,
	".nuxt":       true,
}

// compiledExtensions are file extensions that indicate compiled/generated content.
var compiledExtensions = map[string]bool{
	".min.js":  true,
	".min.css": true,
}

// IsCompiledArtifact returns true when filePath looks like a build output
// that should be excluded from coupling and other source-level analyses.
func IsCompiledArtifact(filePath string) bool {
	// Reject paths containing known build-output directory components.
	for _, part := range strings.Split(filepath.ToSlash(filePath), "/") {
		if compiledArtifactDirs[part] {
			return true
		}
	}
	// Reject known compiled extensions.
	for ext := range compiledExtensions {
		if strings.HasSuffix(filePath, ext) {
			return true
		}
	}
	// HTML files under assets/ are generated pages, not source files.
	if strings.HasSuffix(filePath, ".html") && strings.Contains(filePath, "assets/") {
		return true
	}
	// Reject content-hashed JS/CSS filenames: name.HASH.js where HASH is 8+ hex chars.
	base := filepath.Base(filePath)
	ext := filepath.Ext(base)
	if ext == ".js" || ext == ".css" {
		stem := strings.TrimSuffix(base, ext)
		parts := strings.Split(stem, ".")
		if len(parts) >= 2 {
			last := parts[len(parts)-1]
			// Content hash: 8+ alphanumeric chars with mix of upper/lower/digit/underscore.
			if len(last) >= 8 && isContentHash(last) {
				return true
			}
		}
	}
	return false
}

// isContentHash returns true for strings that look like bundler content hashes
// (e.g. CFDNM_nG, BtfDG5yP — mix of upper, lower, digit, underscore).
func isContentHash(s string) bool {
	hasUpper, hasLower, hasDigit := false, false, false
	for _, c := range s {
		switch {
		case c >= 'A' && c <= 'Z':
			hasUpper = true
		case c >= 'a' && c <= 'z':
			hasLower = true
		case c >= '0' && c <= '9':
			hasDigit = true
		case c == '_' || c == '-':
			// allowed separator chars
		default:
			return false // unexpected char — not a hash
		}
	}
	// Content hashes typically mix at least two character classes.
	count := 0
	if hasUpper {
		count++
	}
	if hasLower {
		count++
	}
	if hasDigit {
		count++
	}
	return count >= 2
}

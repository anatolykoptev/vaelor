package main

import (
	"fmt"
	"strings"

	"github.com/anatolykoptev/go-code/internal/analyze"
	"github.com/anatolykoptev/go-code/internal/ingest"
)

// reversePathMapping converts a container-internal path back to its host-side
// equivalent using the configured PATH_MAPPINGS. If no mapping matches the
// path is returned unchanged. Idempotent.
func reversePathMapping(path string, mappings []analyze.PathMapping) string {
	for _, m := range mappings {
		if strings.HasPrefix(path, m.Internal) {
			return m.External + path[len(m.Internal):]
		}
	}
	return path
}

// reverseToHost is an alias for reversePathMapping exposed under a clearer
// name for callers outside design_helpers.go.
func reverseToHost(path string, mappings []analyze.PathMapping) string {
	return reversePathMapping(path, mappings)
}

// indexedPathsHint returns a short human-readable message listing directory
// names that the indexer skips. Appended to zero-result responses so callers
// know to try grep/filesystem search when their symbol lives in an excluded
// path.
func indexedPathsHint() string {
	dirs := ingest.IgnoredDirNames()
	return fmt.Sprintf(
		"Note: the following directories are excluded from indexing — "+
			"if your symbol is there, search outside the index:\n  %s",
		strings.Join(dirs, ", "),
	)
}

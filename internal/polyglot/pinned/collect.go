package pinned

import (
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// skipDirs lists directory names that are skipped during recursive walks.
// These match the conventions used elsewhere in the polyglot package.
var skipDirs = map[string]bool{
	".git":         true,
	"node_modules": true,
	"vendor":       true,
	"target":       true,
	"dist":         true,
	"build":        true,
}

// Collect walks repoRoot recursively, parses all Dockerfile and
// docker-compose YAML files it finds, and returns a stable (Source, Line)
// sorted slice of PinnedImage values.
//
// Parse errors on individual files are logged and skipped — the walk
// continues and partial results are returned.
func Collect(repoRoot string) ([]PinnedImage, error) {
	var result []PinnedImage

	err := filepath.Walk(repoRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			// Best-effort walk: log and skip this entry instead of aborting.
			// Common causes: permission-denied subdirs (e.g. /var/secrets,
			// /data/youtube), broken symlinks. Walk continues into siblings.
			slog.Warn("pinned.Collect: walk entry error, skipping",
				"path", path, "err", err)
			if info != nil && info.IsDir() {
				return filepath.SkipDir // skip subtree
			}
			return nil // skip file
		}
		if info.IsDir() {
			if skipDirs[info.Name()] {
				return filepath.SkipDir
			}
			return nil
		}

		base := filepath.Base(path)
		switch {
		case isDockerfile(base):
			images, parseErr := ParseDockerfile(path)
			if parseErr != nil {
				slog.Warn("pinned.Collect: ParseDockerfile failed",
					"path", path, "err", parseErr)
				return nil
			}
			result = append(result, images...)

		case isComposeFile(base):
			images, parseErr := ParseCompose(path)
			if parseErr != nil {
				slog.Warn("pinned.Collect: ParseCompose failed",
					"path", path, "err", parseErr)
				return nil
			}
			result = append(result, images...)
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	// Sort deterministically by (Source, Line).
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].Source != result[j].Source {
			return result[i].Source < result[j].Source
		}
		return result[i].Line < result[j].Line
	})

	return result, nil
}

// isDockerfile reports whether basename matches Dockerfile patterns:
//   - "Dockerfile"
//   - "Dockerfile.*" (e.g. "Dockerfile.dev", "Dockerfile.simple")
//   - "*.Dockerfile"  (e.g. "api.Dockerfile")
func isDockerfile(base string) bool {
	if base == "Dockerfile" {
		return true
	}
	if strings.HasPrefix(base, "Dockerfile.") {
		return true
	}
	if strings.HasSuffix(base, ".Dockerfile") {
		return true
	}
	return false
}

// isComposeFile reports whether basename matches compose patterns:
//   - "docker-compose.yml"
//   - "docker-compose.*.yml"
//   - "compose.yml"
//   - "compose.*.yml"
func isComposeFile(base string) bool {
	if base == "docker-compose.yml" || base == "compose.yml" {
		return true
	}
	if strings.HasPrefix(base, "docker-compose.") && strings.HasSuffix(base, ".yml") {
		return true
	}
	if strings.HasPrefix(base, "compose.") && strings.HasSuffix(base, ".yml") {
		return true
	}
	return false
}

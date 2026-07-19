package gogenfilter

import (
	"fmt"
	"os"
	"path/filepath"
)

func fileExists(path string) bool {
	_, err := os.Stat(path)

	return !os.IsNotExist(err)
}

// FindProjectRoot searches parent directories for project marker files.
// Returns empty string if no marker is found after searching up to maxProjectRootDepth levels.
func FindProjectRoot(startPath string, markers []string) (string, *ProjectRootError) {
	absPath, err := filepath.Abs(startPath)
	if err != nil {
		return "", &ProjectRootError{
			Code:      CodeProjectRootInvalidPath,
			StartPath: startPath,
			Markers:   markers,
			Err:       fmt.Errorf("getting absolute path for %q: %w", startPath, err),
		}
	}

	current := absPath

	for range maxProjectRootDepth {
		for _, marker := range markers {
			markerPath := filepath.Join(current, marker)
			if fileExists(markerPath) {
				return current, nil
			}
		}

		parent := filepath.Dir(current)
		if parent == current || parent == "" {
			break
		}

		current = parent
	}

	return "", &ProjectRootError{
		Code:      CodeProjectRootNotFound,
		StartPath: startPath,
		Markers:   markers,
		Err:       nil,
	}
}

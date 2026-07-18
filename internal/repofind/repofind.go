// Package repofind discovers git repositories under parent directories.
// Extracted from internal/embeddings/autoindex.go so both the embeddings
// auto-indexer and internal/federate can share one scanner without a
// dependency cycle (this package depends only on internal/gitutil + stdlib).
package repofind

import (
	"log/slog"
	"os"
	"path/filepath"

	"github.com/anatolykoptev/vaelor/internal/gitutil"
)

// Discover returns the absolute paths of every immediate subdirectory of
// each input dir that is a git repository. Missing/unreadable dirs are
// skipped silently. Order follows os.ReadDir (lexical per dir).
func Discover(dirs []string) []string {
	var roots []string
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			slog.Debug("repofind: skip dir", slog.String("dir", dir), slog.Any("error", err))
			continue
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			root := filepath.Join(dir, e.Name())
			if gitutil.IsGitRepo(root) {
				roots = append(roots, root)
			}
		}
	}
	return roots
}

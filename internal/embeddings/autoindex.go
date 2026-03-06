package embeddings

import (
	"log/slog"
	"os"
	"path/filepath"
)

// RepoKeyFunc generates a graph key from a repo root path.
type RepoKeyFunc func(root string) string

// AutoIndex scans directories for Git repositories and starts background indexing.
// Each immediate subdirectory containing a .git folder is treated as a repo.
// keyFn should be codegraph.GraphNameFor (passed from caller to avoid import cycle).
func AutoIndex(pipeline *Pipeline, dirs []string, keyFn RepoKeyFunc) {
	if pipeline == nil || len(dirs) == 0 {
		return
	}

	var count int
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			slog.Debug("autoindex: skip dir", slog.String("dir", dir), slog.Any("error", err))
			continue
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			root := filepath.Join(dir, e.Name())
			if !isGitRepo(root) {
				continue
			}
			key := keyFn(root)
			if pipeline.IndexRepoAsync(key, root) {
				count++
			}
		}
	}

	if count > 0 {
		slog.Info("autoindex: started background indexing", slog.Int("repos", count))
	}
}

func isGitRepo(path string) bool {
	info, err := os.Stat(filepath.Join(path, ".git"))
	return err == nil && info.IsDir()
}

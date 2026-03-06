package embeddings

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
)

// RepoKeyFunc generates a graph key from a repo root path.
type RepoKeyFunc func(root string) string

// AutoIndex scans directories for Git repositories and indexes them sequentially.
// Each immediate subdirectory containing a .git folder is treated as a repo.
// Repos are indexed one at a time to avoid overwhelming the embedding API.
// keyFn should be codegraph.GraphNameFor (passed from caller to avoid import cycle).
func AutoIndex(pipeline *Pipeline, dirs []string, keyFn RepoKeyFunc) {
	if pipeline == nil || len(dirs) == 0 {
		return
	}

	type repo struct{ key, root string }
	var repos []repo
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
			if isGitRepo(root) {
				repos = append(repos, repo{key: keyFn(root), root: root})
			}
		}
	}

	if len(repos) == 0 {
		return
	}

	slog.Info("autoindex: indexing repos sequentially", slog.Int("repos", len(repos)))
	ctx := context.Background()
	for _, r := range repos {
		result, err := pipeline.IndexRepo(ctx, r.key, r.root)
		if err != nil {
			slog.Warn("autoindex: failed", slog.String("repo", r.key), slog.Any("error", err))
			continue
		}
		slog.Info("autoindex: done", slog.String("repo", r.key),
			slog.Int("indexed", result.Indexed), slog.Int("skipped", result.Skipped))
	}
	slog.Info("autoindex: complete", slog.Int("repos", len(repos)))
}

func isGitRepo(path string) bool {
	info, err := os.Stat(filepath.Join(path, ".git"))
	return err == nil && info.IsDir()
}

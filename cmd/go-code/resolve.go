package main

import (
	"context"
	"fmt"
	"os"

	"github.com/anatolykoptev/go-code/internal/analyze"
	"github.com/anatolykoptev/go-code/internal/ingest"
)

// resolveRoot returns the local root path for the given repo input.
// For remote repos, it clones into the workspace (at the given ref) and returns
// the clone dir along with a cleanup function. For local paths, it validates the
// directory exists and returns a no-op cleanup.
func resolveRoot(ctx context.Context, repo, ref string, deps analyze.Deps) (root string, cleanup func(), err error) {
	if ingest.IsRemote(repo) {
		slug, err := ingest.NormalizeSlug(repo)
		if err != nil {
			return "", nil, fmt.Errorf("invalid repo: %w", err)
		}
		result, err := ingest.CloneRepo(ctx, ingest.CloneOpts{
			Slug:        slug,
			Ref:         ref,
			DestDir:     deps.WorkspaceDir,
			GithubToken: deps.GithubToken,
		})
		if err != nil {
			return "", nil, fmt.Errorf("clone: %w", err)
		}
		dir := result.LocalPath
		return dir, func() { _ = ingest.CleanupCloneDir(dir) }, nil
	}
	// Local path.
	fi, err := os.Stat(repo)
	if err != nil {
		return "", nil, fmt.Errorf("local path: %w", err)
	}
	if !fi.IsDir() {
		return "", nil, fmt.Errorf("local path %q: not a directory", repo)
	}
	return repo, func() {}, nil
}

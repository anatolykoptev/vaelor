package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/anatolykoptev/go-code/internal/analyze"
	"github.com/anatolykoptev/go-code/internal/forge"
	"github.com/anatolykoptev/go-code/internal/ingest"
)

// rewritePath applies path mappings, translating external prefixes to internal ones.
func rewritePath(path string, mappings []analyze.PathMapping) string {
	for _, m := range mappings {
		if strings.HasPrefix(path, m.External) {
			return m.Internal + path[len(m.External):]
		}
	}
	return path
}

// resolveRoot returns the local root path for the given repo input.
// For remote repos, it clones into the workspace (at the given ref) and returns
// the clone dir along with a cleanup function. For local paths, it validates the
// directory exists and returns a no-op cleanup.
func resolveRoot(ctx context.Context, repo, ref string, deps analyze.Deps) (root string, cleanup func(), err error) {
	// Strip common prefix mistakes from callers.
	repo = strings.TrimPrefix(repo, "path=")
	repo = strings.TrimPrefix(repo, "local:")

	// WordPress plugin — download from wordpress.org.
	if ingest.IsWordPressPlugin(repo) {
		slug, err := ingest.NormalizeWPSlug(repo)
		if err != nil {
			return "", nil, fmt.Errorf("invalid wp plugin: %w", err)
		}
		result, err := ingest.FetchWPPlugin(ctx, ingest.WPPluginOpts{
			Slug:    slug,
			DestDir: deps.WorkspaceDir,
		})
		if err != nil {
			return "", nil, fmt.Errorf("fetch wp plugin: %w", err)
		}
		dir := result.LocalPath
		return dir, func() {
			if err := ingest.CleanupCloneDir(dir); err != nil {
				slog.Warn("failed to cleanup wp plugin dir", slog.String("dir", dir), slog.Any("error", err))
			}
		}, nil
	}

	if forge.IsRemote(repo) {
		slug, ok := forge.ExtractSlug(repo)
		if !ok {
			return "", nil, fmt.Errorf("invalid repo: cannot extract slug from %q", repo)
		}
		kind := forge.DetectForge(repo)
		cloneURL := forge.CloneURL(kind, slug, "", deps.GithubToken)
		result, err := ingest.CloneRepo(ctx, ingest.CloneOpts{
			Slug:        slug,
			Ref:         ref,
			DestDir:     deps.WorkspaceDir,
			GithubToken: deps.GithubToken,
			CloneURL:    cloneURL,
		})
		if err != nil {
			return "", nil, fmt.Errorf("clone: %w", err)
		}
		dir := result.LocalPath
		return dir, func() {
			if err := ingest.CleanupCloneDir(dir); err != nil {
				slog.Warn("failed to cleanup clone dir", slog.String("dir", dir), slog.Any("error", err))
			}
		}, nil
	}
	// Local path — apply path mappings (e.g. /path/to/repos/src → /host-src).
	repo = rewritePath(repo, deps.PathMappings)
	fi, err := os.Stat(repo)
	if err != nil {
		return "", nil, fmt.Errorf("local path: %w", err)
	}
	if !fi.IsDir() {
		return "", nil, fmt.Errorf("local path %q: not a directory", repo)
	}
	return repo, func() {}, nil
}

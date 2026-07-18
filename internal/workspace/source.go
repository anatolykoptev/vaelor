// Package workspace provides a unified Source abstraction for resolving a repo
// identifier to an on-disk path. It encapsulates the three ingestion patterns
// used by go-code: local paths (with PATH_MAPPINGS translation), remote
// owner/repo slugs (clone), and WordPress.org plugin slugs (fetch).
package workspace

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/anatolykoptev/vaelor/internal/analyze"
	"github.com/anatolykoptev/vaelor/internal/forge"
	"github.com/anatolykoptev/vaelor/internal/ingest"
)

// Source resolves a repo identifier to a usable on-disk path, applying
// PATH_MAPPINGS translation, slug-cloning, or local lookup as appropriate.
type Source interface {
	// Root returns an absolute on-disk path to the repo's source tree, along
	// with a cleanup function (no-op for non-temporary sources). Callers must
	// call cleanup() when done even if err is non-nil.
	Root(ctx context.Context) (path string, cleanup func(), err error)
}

// LocalSource resolves a bare on-disk path, applying PATH_MAPPINGS translation
// (External→Internal prefix substitution) before stat-checking the directory.
type LocalSource struct {
	// Path is the raw path as supplied by the caller (may be host-side).
	Path string
	// Mappings is the PATH_MAPPINGS translation table. May be nil.
	Mappings []analyze.PathMapping
}

// Root translates Path through Mappings and verifies the result is a directory.
func (s LocalSource) Root(_ context.Context) (string, func(), error) {
	p := RewritePath(s.Path, s.Mappings)
	fi, err := os.Stat(p)
	if err != nil {
		return "", func() {}, fmt.Errorf("local path: %w", err)
	}
	if !fi.IsDir() {
		return "", func() {}, fmt.Errorf("local path %q: not a directory", p)
	}
	return p, func() {}, nil
}

// RemoteSource clones a remote repo by owner/repo slug and returns the clone
// directory. The cleanup function removes the clone.
type RemoteSource struct {
	// Slug is the "owner/repo" identifier (resolved via forge.ExtractSlug).
	Slug string
	// RepoInput is the original user-supplied identifier (URL or slug).
	// Used for forge detection so that GitLab and other forges are handled
	// correctly. If empty, the resolver assumes GitHub forge — GitLab/Bitbucket
	// callers MUST set this from the unnormalized input.
	RepoInput string
	// Ref is the optional git ref (branch, tag, commit) to check out.
	Ref string
	// DestDir is the workspace directory for temporary clones.
	DestDir string
	// TokenFunc returns an authentication token for the clone; may be nil.
	TokenFunc func(ctx context.Context) (string, error)
	// StaticToken is used when TokenFunc is nil. May be empty.
	StaticToken string
}

// Root clones the remote repo and returns the clone directory.
func (s RemoteSource) Root(ctx context.Context) (string, func(), error) {
	token := s.StaticToken
	if s.TokenFunc != nil {
		var err error
		token, err = s.TokenFunc(ctx)
		if err != nil {
			return "", func() {}, fmt.Errorf("get clone token: %w", err)
		}
	}
	// Use RepoInput for forge detection to correctly handle GitLab and other forges.
	repoInput := s.RepoInput
	if repoInput == "" {
		repoInput = s.Slug
	}
	kind := forge.DetectForge(repoInput)
	cloneURL := forge.CloneURL(kind, s.Slug, "", token)
	result, err := ingest.CloneRepo(ctx, ingest.CloneOpts{
		Slug:        s.Slug,
		Ref:         s.Ref,
		DestDir:     s.DestDir,
		GithubToken: token,
		CloneURL:    cloneURL,
		TokenFunc:   s.TokenFunc,
	})
	if err != nil {
		return "", func() {}, fmt.Errorf("clone: %w", err)
	}
	dir := result.LocalPath
	// Register this reader against the shared, slug-deterministic clone dir so
	// the dir is removed only when the LAST holder releases it — a concurrent
	// background reader (code_health) and a synchronous sibling tool can share
	// the same dir without one deleting it mid-walk.
	ingest.AcquireCloneRef(dir)
	cleanup := func() {
		if cerr := ingest.ReleaseCloneRef(dir); cerr != nil {
			slog.Warn("failed to cleanup clone dir", slog.String("dir", dir), slog.Any("error", cerr))
		}
	}
	return dir, cleanup, nil
}

// WPSource downloads a WordPress.org plugin by slug and returns the extracted
// directory. The cleanup function removes the temporary directory.
type WPSource struct {
	// Slug is the WordPress.org plugin slug (e.g. "woocommerce").
	Slug string
	// DestDir is the workspace directory for the downloaded plugin.
	DestDir string
}

// Root fetches the WordPress plugin and returns its local path.
func (s WPSource) Root(ctx context.Context) (string, func(), error) {
	normalized, err := ingest.NormalizeWPSlug(s.Slug)
	if err != nil {
		return "", func() {}, fmt.Errorf("invalid wp plugin: %w", err)
	}
	result, err := ingest.FetchWPPlugin(ctx, ingest.WPPluginOpts{
		Slug:    normalized,
		DestDir: s.DestDir,
	})
	if err != nil {
		return "", func() {}, fmt.Errorf("fetch wp plugin: %w", err)
	}
	dir := result.LocalPath
	ingest.AcquireCloneRef(dir)
	cleanup := func() {
		if cerr := ingest.ReleaseCloneRef(dir); cerr != nil {
			slog.Warn("failed to cleanup wp plugin dir", slog.String("dir", dir), slog.Any("error", cerr))
		}
	}
	return dir, cleanup, nil
}

// RewritePath applies mappings to path, substituting the first matching
// External prefix with the corresponding Internal prefix. Returns path unchanged
// when no mapping matches or mappings is empty.
func RewritePath(path string, mappings []analyze.PathMapping) string {
	for _, m := range mappings {
		if len(path) >= len(m.External) && path[:len(m.External)] == m.External {
			return m.Internal + path[len(m.External):]
		}
	}
	return path
}

// TranslateDirs applies PATH_MAPPINGS translation to each directory in dirs,
// returning a new slice with each entry rewritten through mappings. Entries
// that have no matching mapping are returned unchanged.
//
// Use this to translate AUTO_INDEX_DIRS (host-side paths from operator config)
// to container-internal paths before passing them to AutoIndex or EagerWarmRepos.
// Controlled by GO_CODE_AUTOINDEX_TRANSLATE=true; callers are responsible for
// checking that env flag before invoking this function.
func TranslateDirs(dirs []string, mappings []analyze.PathMapping) []string {
	if len(mappings) == 0 {
		return append([]string(nil), dirs...)
	}
	out := make([]string, len(dirs))
	for i, d := range dirs {
		out[i] = RewritePath(d, mappings)
	}
	return out
}

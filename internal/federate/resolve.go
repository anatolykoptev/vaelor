// Package federate provides multi-repo fan-out for go-code: resolve a repo
// pattern to a list of repos, then run per-repo primitives and correlate in
// Go. The go-code AGE graph is per-repo, so cross-repo signals are computed
// by fan-out + merge, never by a merged graph.
package federate

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/anatolykoptev/go-code/internal/gitutil"
	"github.com/anatolykoptev/go-code/internal/repofind"
)

// RepoRef identifies one repo in a federated set.
type RepoRef struct {
	Slug string // basename of the repo dir (e.g. "oxpulse-chat"); not unique across a result set if the same basename exists under two localDirs
	Root string // absolute local path
}

// ResolveRepos expands a pattern into a list of RepoRef.
//
//   - "all"        → every git repo directly under localDirs
//   - "<glob>"     → repos whose basename matches the glob (filepath.Match)
//   - "<abs path>" → that single repo (must be a git repo)
//
// A pattern that is an absolute path to an existing git repo is treated as a
// single explicit repo. localDirs are the parent directories holding
// checkouts (deps.LocalRepoDirs, e.g. /host/src).
func ResolveRepos(pattern string, localDirs []string) ([]RepoRef, error) {
	// Single explicit path (absolute, points at a git repo).
	if filepath.IsAbs(pattern) && gitutil.IsGitRepo(pattern) {
		return []RepoRef{{Slug: filepath.Base(pattern), Root: pattern}}, nil
	}

	roots := repofind.Discover(localDirs)

	if pattern == "all" {
		return toRefs(dedupeByOrigin(roots)), nil
	}

	// Glob match on basename.
	var matched []string
	for _, root := range roots {
		base := filepath.Base(root)
		ok, err := filepath.Match(pattern, base)
		if err != nil {
			return nil, fmt.Errorf("bad repo pattern %q: %w", pattern, err)
		}
		if ok {
			matched = append(matched, root)
		}
	}
	if len(matched) > 0 {
		return toRefs(dedupeByOrigin(matched)), nil
	}

	// Fallback: exact basename match (pattern is a plain repo name).
	if !strings.ContainsAny(pattern, "*?[") {
		for _, root := range roots {
			if filepath.Base(root) == pattern {
				return []RepoRef{{Slug: pattern, Root: root}}, nil
			}
		}
	}
	return nil, nil
}

func toRefs(roots []string) []RepoRef {
	refs := make([]RepoRef, 0, len(roots))
	for _, root := range roots {
		refs = append(refs, RepoRef{Slug: filepath.Base(root), Root: root})
	}
	return refs
}

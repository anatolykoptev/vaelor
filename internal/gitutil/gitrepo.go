// Package gitutil provides shared Git repository helpers used across
// multiple internal packages.
package gitutil

import (
	"os"
	"path/filepath"
)

// IsGitRepo reports whether root is the root of a git repository.
//
// It accepts both forms of the .git entry:
//   - a directory: standard single-worktree repository
//   - a regular file: linked worktree created by "git worktree add"
//     (the file contains a "gitdir: ..." pointer to the main repo)
//
// Anything other than a stat error at <root>/.git is treated as a valid
// git repo — downstream git commands will surface a more precise error if
// the entry is malformed.
func IsGitRepo(root string) bool {
	info, err := os.Stat(filepath.Join(root, ".git"))
	if err != nil {
		return false
	}
	return info.IsDir() || info.Mode().IsRegular()
}

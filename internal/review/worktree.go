package review

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
)

// PRWorktree creates an isolated git worktree at FETCH_HEAD so the call
// graph and any tree-walking analysis sees the PR code, not the warm
// clone's checked-out ref.
//
// Why isolated: BuildFromRepo and friends parse the working tree on disk.
// `git fetch origin pull/N/head` only updates FETCH_HEAD ref — the working
// tree stays on whatever the original clone checked out (usually main).
// Without a worktree, impact analysis on PR-only symbols (functions added
// by the PR) silently misses callers because the parser never sees them.
//
// Cleanup: returned cleanup func runs `git worktree remove --force` and
// rms the temp dir. Always defer-call it, even on error paths from the
// caller — the temp dir leaks otherwise.
type PRWorktree struct {
	Path    string // absolute path to the worktree (tree-walking root)
	Cleanup func()
}

// CreatePRWorktree adds a worktree at the given ref under a temp directory.
// repoRoot is the original clone (where FETCH_HEAD is set). ref is the
// reference to check out into the new worktree (typically "FETCH_HEAD").
//
// On error, returns a fully-formed cleanup that's safe to call.
func CreatePRWorktree(ctx context.Context, repoRoot, ref string) (*PRWorktree, error) {
	tmpDir, err := os.MkdirTemp("", "go-code-review-pr-")
	if err != nil {
		return nil, fmt.Errorf("mktemp: %w", err)
	}
	// We pass the worktree path under the temp dir, but `git worktree add`
	// requires the directory to NOT pre-exist. Append a subdir.
	wtPath := filepath.Join(tmpDir, "worktree")

	cleanup := func() {
		if _, err := GitExec(ctx, repoRoot, "worktree", "remove", "--force", wtPath); err != nil {
			slog.Debug("worktree remove failed (will rm anyway)",
				slog.String("path", wtPath), slog.Any("error", err))
		}
		if err := os.RemoveAll(tmpDir); err != nil {
			slog.Debug("tmpDir cleanup failed",
				slog.String("path", tmpDir), slog.Any("error", err))
		}
	}

	// --detach: don't try to create a branch, we just want a tree at the ref.
	if _, err := GitExec(ctx, repoRoot, "worktree", "add", "--detach", wtPath, ref); err != nil {
		cleanup()
		return nil, fmt.Errorf("worktree add: %w", err)
	}

	return &PRWorktree{Path: wtPath, Cleanup: cleanup}, nil
}

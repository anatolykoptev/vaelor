package embeddings

import (
	"context"
	"errors"
	"os/exec"
	"strings"
)

// repoMainBranchSHA returns the sha of the repo's main branch (main → master →
// HEAD fallback). Used as a per-repo fingerprint to skip indexing when the
// branch hasn't moved since the last successful run.
//
// Why main-branch and not HEAD: developers on feature branches should not
// trigger reindexing of every repo on every container restart; only commits
// merged to main legitimately invalidate the cached embedding set.
//
// Returns ("", nil) when the path is not a git repo (e.g. tarball checkout) —
// callers should treat this as "no fingerprint, fall through to full index".
func repoMainBranchSHA(root string) (string, error) {
	for _, ref := range []string{"main", "master", "HEAD"} {
		out, err := exec.Command("git", "-C", root, "rev-parse", ref).Output()
		if err == nil {
			sha := strings.TrimSpace(string(out))
			if sha != "" {
				return sha, nil
			}
		}
	}
	// Path is probably not a git repo — distinguish from real errors.
	if _, err := exec.Command("git", "-C", root, "rev-parse", "--git-dir").Output(); err != nil {
		return "", nil
	}
	return "", errors.New("repo: no main/master/HEAD ref found")
}

// GetRepoState returns the last-indexed sha for a repo, or "" when no row exists.
func (s *Store) GetRepoState(ctx context.Context, repoKey string) (string, error) {
	if err := s.EnsureSchema(ctx); err != nil {
		return "", err
	}
	var sha string
	err := s.pool.QueryRow(ctx,
		`SELECT head_sha FROM public.code_repo_state WHERE repo_key = $1`, repoKey).
		Scan(&sha)
	if err != nil {
		// pgx returns ErrNoRows on empty — caller treats as "first index".
		if strings.Contains(err.Error(), "no rows") {
			return "", nil
		}
		return "", err
	}
	return sha, nil
}

// SetRepoState records the sha that was successfully indexed.
func (s *Store) SetRepoState(ctx context.Context, repoKey, sha string) error {
	if err := s.EnsureSchema(ctx); err != nil {
		return err
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO public.code_repo_state (repo_key, head_sha, indexed_at)
		 VALUES ($1, $2, NOW())
		 ON CONFLICT (repo_key) DO UPDATE SET head_sha = EXCLUDED.head_sha, indexed_at = NOW()`,
		repoKey, sha)
	return err
}

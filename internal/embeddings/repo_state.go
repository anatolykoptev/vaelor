package embeddings

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"time"
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

// GetIndexedAt returns the indexed_at timestamp for a repo, or zero time when no
// row exists (repo never indexed). Used by stale-demote to determine the
// current index generation: any embedding row with updated_at < indexed_at
// was not re-touched this generation and is a candidate orphan.
//
// Returns zero time on any error (schema init failure, no rows) so callers can
// treat zero as "generation unknown → skip demote" safely.
func (s *Store) GetIndexedAt(ctx context.Context, repoKey string) time.Time {
	if err := s.EnsureSchema(ctx); err != nil {
		return time.Time{}
	}
	var ts time.Time
	err := s.pool.QueryRow(ctx,
		`SELECT indexed_at FROM public.code_repo_state WHERE repo_key = $1`, repoKey).
		Scan(&ts)
	if err != nil {
		return time.Time{}
	}
	return ts
}

// SetRepoState records the sha and the active embedding model that was successfully indexed.
// model is stored alongside head_sha so that a model switch triggers a full reindex
// on next startup even when the repo's git SHA has not changed.
func (s *Store) SetRepoState(ctx context.Context, repoKey, sha, model string) error {
	if err := s.EnsureSchema(ctx); err != nil {
		return err
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO public.code_repo_state (repo_key, head_sha, indexed_at, embed_model)
		 VALUES ($1, $2, NOW(), $3)
		 ON CONFLICT (repo_key) DO UPDATE
		     SET head_sha = EXCLUDED.head_sha,
		         indexed_at = NOW(),
		         embed_model = EXCLUDED.embed_model`,
		repoKey, sha, model)
	return err
}

// InvalidateRepoIfModelChanged purges code_embeddings for repoKey and resets
// its head_sha to "" when the stored embed_model differs from activeModel.
// This forces a full reindex on next query, producing vectors in the new
// embedding space. No-ops when the stored model matches.
//
// The purge is atomic per repo: DELETE code_embeddings + DELETE code_repo_state
// row both run inside a single transaction so semantic_search never sees a
// half-purged state (either all old vectors remain or none do).
//
// Returns (true, nil) when a purge was performed, (false, nil) when not needed.
func (s *Store) InvalidateRepoIfModelChanged(ctx context.Context, repoKey, activeModel string) (bool, error) {
	if err := s.EnsureSchema(ctx); err != nil {
		return false, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var storedModel string
	err = tx.QueryRow(ctx,
		`SELECT embed_model FROM public.code_repo_state WHERE repo_key = $1`,
		repoKey).Scan(&storedModel)
	if err != nil {
		// No row → no embeddings to purge; first index will write the correct model.
		if strings.Contains(err.Error(), "no rows") {
			return false, nil
		}
		return false, err
	}
	if storedModel == activeModel {
		_ = tx.Rollback(ctx)
		return false, nil
	}
	// Model mismatch → purge all embeddings for this repo and reset state.
	if _, err := tx.Exec(ctx,
		`DELETE FROM public.code_embeddings WHERE repo_key = $1`, repoKey); err != nil {
		return false, err
	}
	if _, err := tx.Exec(ctx,
		`DELETE FROM public.code_repo_state WHERE repo_key = $1`, repoKey); err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return true, nil
}

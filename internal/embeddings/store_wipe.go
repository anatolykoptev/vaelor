package embeddings

import "context"

// WipeRepo atomically deletes ALL data for repoKey from both code_embeddings
// and code_repo_state inside a single transaction. If either DELETE fails the
// whole transaction rolls back — no partial deletes are ever committed.
//
// This is the irreversible-data-deletion seam (ADR-8): the wipe CLI
// subcommand and InvalidateRepoIfModelChanged both route through here so the
// dual-table DELETE logic lives in exactly one place. Data deletion is
// irreversible; callers must confirm before invoking.
//
// The transaction pattern mirrors repo_state.go: pool.Begin → deferred
// Rollback (no-op after Commit) → Exec both DELETEs → Commit.
func (s *Store) WipeRepo(ctx context.Context, repoKey string) error {
	if err := s.EnsureSchema(ctx); err != nil {
		return err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx,
		`DELETE FROM public.code_embeddings WHERE repo_key = $1`, repoKey); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx,
		`DELETE FROM public.code_repo_state WHERE repo_key = $1`, repoKey); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

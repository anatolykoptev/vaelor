package codegraph

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// getMeta retrieves the stored GraphMeta for repoKey, or returns nil if none exists.
func getMeta(ctx context.Context, store *Store, repoKey string) (*GraphMeta, error) {
	conn, err := store.Pool().Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquire connection: %w", err)
	}
	defer conn.Release()

	var m GraphMeta
	err = conn.QueryRow(ctx, `
		SELECT repo_key, repo_path, graph_name,
		       file_count, symbol_count, edge_count,
		       built_at, ttl_seconds
		FROM code_graph_meta
		WHERE repo_key = $1`, repoKey,
	).Scan(
		&m.RepoKey, &m.RepoPath, &m.GraphName,
		&m.FileCount, &m.SymbolCount, &m.EdgeCount,
		&m.BuiltAt, &m.TTLSeconds,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		// 42P01 = undefined_table: schema not yet initialised; treat as cache miss.
		if strings.Contains(err.Error(), "42P01") {
			return nil, nil
		}
		return nil, fmt.Errorf("query meta: %w", err)
	}
	return &m, nil
}

// upsertMeta inserts or updates the GraphMeta row.
func upsertMeta(ctx context.Context, store *Store, meta *GraphMeta) error {
	conn, err := store.Pool().Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire connection: %w", err)
	}
	defer conn.Release()

	_, err = conn.Exec(ctx, `
		INSERT INTO code_graph_meta
		    (repo_key, repo_path, graph_name, file_count, symbol_count, edge_count, built_at, ttl_seconds)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (repo_key) DO UPDATE SET
		    repo_path    = EXCLUDED.repo_path,
		    graph_name   = EXCLUDED.graph_name,
		    file_count   = EXCLUDED.file_count,
		    symbol_count = EXCLUDED.symbol_count,
		    edge_count   = EXCLUDED.edge_count,
		    built_at     = EXCLUDED.built_at,
		    ttl_seconds  = EXCLUDED.ttl_seconds`,
		meta.RepoKey, meta.RepoPath, meta.GraphName,
		meta.FileCount, meta.SymbolCount, meta.EdgeCount,
		meta.BuiltAt, meta.TTLSeconds,
	)
	if err != nil {
		return fmt.Errorf("upsert meta: %w", err)
	}
	return nil
}

// isFresh reports whether builtAt is within ttlSeconds of the current time.
func isFresh(builtAt time.Time, ttlSeconds int) bool {
	if ttlSeconds <= 0 {
		return false
	}
	return time.Since(builtAt) < time.Duration(ttlSeconds)*time.Second
}

// CacheStatus checks if a valid cached graph exists for root.
// Returns (true, nil) if cached and fresh, (false, nil) if not present or stale.
func CacheStatus(ctx context.Context, store *Store, root string) (bool, error) {
	key := GraphNameFor(root)
	meta, err := getMeta(ctx, store, key)
	if err != nil {
		return false, err
	}
	return meta != nil && isFresh(meta.BuiltAt, meta.TTLSeconds), nil
}

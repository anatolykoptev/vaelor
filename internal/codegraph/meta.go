package codegraph

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/anatolykoptev/vaelor/internal/ingest"
	"github.com/jackc/pgx/v5"
)

// getMeta retrieves the stored GraphMeta for repoKey, or returns nil if none exists.
func getMeta(ctx context.Context, store *Store, repoKey string) (*GraphMeta, error) {
	conn, err := store.acquireAGE(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquire connection: %w", err)
	}
	defer conn.Release()

	var m GraphMeta
	err = conn.QueryRow(ctx, `
		SELECT repo_key, repo_path, graph_name,
		       file_count, symbol_count, edge_count,
		       built_at, ttl_seconds, content_hash
		FROM code_graph_meta
		WHERE repo_key = $1`, repoKey,
	).Scan(
		&m.RepoKey, &m.RepoPath, &m.GraphName,
		&m.FileCount, &m.SymbolCount, &m.EdgeCount,
		&m.BuiltAt, &m.TTLSeconds, &m.ContentHash,
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

// ListMeta returns every stored GraphMeta row — one per repo that has ever
// completed a successful build. Used at boot to seed
// gocode_code_graph_age_seconds with the REAL age of each repo's last build
// (see cmd/go-code's publishCodeGraphAgeGauge) instead of leaving the series
// absent — and therefore invisible to GocodeCodeGraphStale — until the next
// build cycle completes.
//
// Returns (nil, nil) when code_graph_meta does not exist yet, matching
// getMeta's cold-path treatment: an uninitialised schema is "no repos known
// yet", not an error.
func ListMeta(ctx context.Context, store *Store) ([]GraphMeta, error) {
	conn, err := store.acquireAGE(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquire connection: %w", err)
	}
	defer conn.Release()

	rows, err := conn.Query(ctx, `
		SELECT repo_key, repo_path, graph_name,
		       file_count, symbol_count, edge_count,
		       built_at, ttl_seconds, content_hash
		FROM code_graph_meta`)
	if err != nil {
		// 42P01 = undefined_table: schema not yet initialised; treat as no known repos.
		if strings.Contains(err.Error(), "42P01") {
			return nil, nil
		}
		return nil, fmt.Errorf("query meta: %w", err)
	}
	defer rows.Close()

	var metas []GraphMeta
	for rows.Next() {
		var m GraphMeta
		if scanErr := rows.Scan(
			&m.RepoKey, &m.RepoPath, &m.GraphName,
			&m.FileCount, &m.SymbolCount, &m.EdgeCount,
			&m.BuiltAt, &m.TTLSeconds, &m.ContentHash,
		); scanErr != nil {
			return nil, fmt.Errorf("scan meta row: %w", scanErr)
		}
		metas = append(metas, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate meta rows: %w", err)
	}
	return metas, nil
}

// upsertMeta inserts or updates the GraphMeta row.
func upsertMeta(ctx context.Context, store *Store, meta *GraphMeta) error {
	conn, err := store.acquireAGE(ctx)
	if err != nil {
		return fmt.Errorf("acquire connection: %w", err)
	}
	defer conn.Release()

	_, err = conn.Exec(ctx, `
		INSERT INTO code_graph_meta
		    (repo_key, repo_path, graph_name, file_count, symbol_count, edge_count, built_at, ttl_seconds, content_hash)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (repo_key) DO UPDATE SET
		    repo_path     = EXCLUDED.repo_path,
		    graph_name    = EXCLUDED.graph_name,
		    file_count    = EXCLUDED.file_count,
		    symbol_count  = EXCLUDED.symbol_count,
		    edge_count    = EXCLUDED.edge_count,
		    built_at      = EXCLUDED.built_at,
		    ttl_seconds   = EXCLUDED.ttl_seconds,
		    content_hash  = EXCLUDED.content_hash`,
		meta.RepoKey, meta.RepoPath, meta.GraphName,
		meta.FileCount, meta.SymbolCount, meta.EdgeCount,
		meta.BuiltAt, meta.TTLSeconds, meta.ContentHash,
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
// Freshness requires BOTH temporal TTL and content-hash match (#592).
func CacheStatus(ctx context.Context, store *Store, root string) (bool, error) {
	key := GraphNameFor(root)
	meta, err := getMeta(ctx, store, key)
	if err != nil {
		return false, err
	}
	if meta == nil || !isFresh(meta.BuiltAt, meta.TTLSeconds) {
		return false, nil
	}
	// Temporal TTL hit — validate content hash.
	if meta.ContentHash != "" {
		return ingest.RepoContentHash(root) == meta.ContentHash, nil
	}
	return true, nil
}

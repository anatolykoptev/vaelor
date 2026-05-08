package codegraph

import (
	"context"
	"encoding/binary"
	"fmt"
	"hash/fnv"
	"log/slog"
	"math"
	"math/rand/v2"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// truncate returns the first n runes of s, or s if shorter.
func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "..."
}

// GraphNameFor returns the deterministic AGE graph name for the given repo path.
// Exported for callers that need the name without a Store instance.
func GraphNameFor(repoPath string) string {
	return graphName(repoPath)
}

// graphName produces a stable graph name from a repo path using FNV-32a.
// Format: code_<8-char-hex>, e.g. "code_a3f2b1c0".
func graphName(repoPath string) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(repoPath))
	var buf [4]byte
	binary.BigEndian.PutUint32(buf[:], h.Sum32())
	return fmt.Sprintf("code_%08x", buf)
}

// escapeCypher escapes a string for safe use in a single-quoted Cypher literal.
// Prevents Cypher injection by escaping backslash, quotes, backtick, and control characters.
func escapeCypher(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `'`, `\'`)
	s = strings.ReplaceAll(s, "\x00", "") // strip null bytes
	s = strings.ReplaceAll(s, "\n", `\n`)
	s = strings.ReplaceAll(s, "\r", `\r`)
	s = strings.ReplaceAll(s, "\t", `\t`)
	return s
}

// reWriteOp matches Cypher write keywords — used to reject writes in ExecCypher.
var reWriteOp = regexp.MustCompile(`(?i)\b(CREATE|DELETE|SET|MERGE|REMOVE|DROP|DETACH)\b`)

// reGraphName validates graph names: only lowercase alphanumeric and underscores.
var reGraphName = regexp.MustCompile(`^[a-z0-9_]+$`)

// validateGraphName returns an error if the name contains unsafe characters.
func validateGraphName(name string) error {
	if !reGraphName.MatchString(name) {
		return fmt.Errorf("invalid graph name %q: must match [a-z0-9_]+", name)
	}
	return nil
}

// cypherDollarQuote returns a dollar-quoting tag that does not appear in the
// Cypher body. PostgreSQL dollar-quoting: $tag$...$tag$.
func cypherDollarQuote(cypher string) string {
	tag := "$cq$"
	for strings.Contains(cypher, tag) {
		tag = fmt.Sprintf("$cq%d$", rand.IntN(99999)) //nolint:mnd,gosec // random suffix, not crypto
	}
	return tag
}

// isReadOnly returns true if cypher contains no write operations.
func isReadOnly(cypher string) bool {
	return !reWriteOp.MatchString(cypher)
}

// buildColDefs returns "c0 ag_catalog.agtype, c1 ag_catalog.agtype, ..." for n columns.
func buildColDefs(n int) string {
	if n <= 0 {
		n = 1
	}
	parts := make([]string, n)
	for i := range n {
		parts[i] = fmt.Sprintf("c%d ag_catalog.agtype", i)
	}
	return strings.Join(parts, ", ")
}

// UpsertFileMtimes stores file modification times for a repo.
// It replaces all existing entries for the given repoKey.
func (s *Store) UpsertFileMtimes(ctx context.Context, repoKey string, mtimes map[string]time.Time) error {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire connection: %w", err)
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, "DELETE FROM code_file_mtimes WHERE repo_key = $1", repoKey); err != nil {
		return fmt.Errorf("delete old mtimes: %w", err)
	}
	if len(mtimes) == 0 {
		return nil
	}

	rows := make([][]any, 0, len(mtimes))
	for path, mtime := range mtimes {
		rows = append(rows, []any{repoKey, path, mtime})
	}

	_, err = conn.CopyFrom(
		ctx,
		pgx.Identifier{"code_file_mtimes"},
		[]string{"repo_key", "file_path", "mod_time"},
		pgx.CopyFromRows(rows),
	)
	if err != nil {
		return fmt.Errorf("copy file mtimes: %w", err)
	}
	return nil
}

// GetFileMtimes retrieves stored file modification times for a repo.
func (s *Store) GetFileMtimes(ctx context.Context, repoKey string) (map[string]time.Time, error) {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquire connection: %w", err)
	}
	defer conn.Release()

	rows, err := conn.Query(ctx,
		"SELECT file_path, mod_time FROM code_file_mtimes WHERE repo_key = $1", repoKey)
	if err != nil {
		return nil, fmt.Errorf("query mtimes: %w", err)
	}
	defer rows.Close()

	result := make(map[string]time.Time)
	for rows.Next() {
		var path string
		var modTime time.Time
		if err := rows.Scan(&path, &modTime); err != nil {
			return nil, fmt.Errorf("scan mtime: %w", err)
		}
		result[path] = modTime
	}
	return result, rows.Err()
}

// DeleteFileMtimes removes stored file modification times for a repo.
func (s *Store) DeleteFileMtimes(ctx context.Context, repoKey string) error {
	_, err := s.pool.Exec(ctx, "DELETE FROM code_file_mtimes WHERE repo_key = $1", repoKey)
	return err
}

// healthCacheTableSQL is the schema for storing code_health result cache.
const healthCacheTableSQL = `
CREATE TABLE IF NOT EXISTS code_health_cache (
    repo_key     TEXT PRIMARY KEY,
    repo_path    TEXT NOT NULL,
    score        INT NOT NULL,
    grade        TEXT NOT NULL,
    result_xml   TEXT NOT NULL,
    computed_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    ttl_seconds  INT NOT NULL DEFAULT 3600
)`

// UpsertHealthCache stores a code_health XML result.
func (s *Store) UpsertHealthCache(ctx context.Context, repoKey, repoPath, grade, resultXML string, score int, ttl int) error {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire: %w", err)
	}
	defer conn.Release()
	_, err = conn.Exec(ctx, `
		INSERT INTO code_health_cache (repo_key, repo_path, score, grade, result_xml, computed_at, ttl_seconds)
		VALUES ($1, $2, $3, $4, $5, now(), $6)
		ON CONFLICT (repo_key) DO UPDATE SET
			repo_path = EXCLUDED.repo_path, score = EXCLUDED.score, grade = EXCLUDED.grade,
			result_xml = EXCLUDED.result_xml, computed_at = EXCLUDED.computed_at,
			ttl_seconds = EXCLUDED.ttl_seconds`,
		repoKey, repoPath, score, grade, resultXML, ttl)
	return err
}

// HealthCacheEntry holds a cached code_health result.
type HealthCacheEntry struct {
	ResultXML  string
	Score      int
	Grade      string
	ComputedAt time.Time
	TTLSeconds int
}

// LoadHealthCache returns the cached health result if fresh.
// Returns nil if not cached or stale.
func (s *Store) LoadHealthCache(ctx context.Context, repoKey string) *HealthCacheEntry {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return nil
	}
	defer conn.Release()
	var e HealthCacheEntry
	err = conn.QueryRow(ctx, `
		SELECT result_xml, score, grade, computed_at, ttl_seconds
		FROM code_health_cache WHERE repo_key = $1`, repoKey).
		Scan(&e.ResultXML, &e.Score, &e.Grade, &e.ComputedAt, &e.TTLSeconds)
	if err != nil {
		return nil
	}
	if time.Since(e.ComputedAt) > time.Duration(e.TTLSeconds)*time.Second {
		return nil // stale
	}
	return &e
}

// EnsureHealthCacheTable creates the code_health_cache table if it doesn't exist.
func (s *Store) EnsureHealthCacheTable(ctx context.Context) error {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire: %w", err)
	}
	defer conn.Release()
	_, err = conn.Exec(ctx, healthCacheTableSQL)
	return err
}

// SymbolStructuralRank returns a human-readable PageRank percentile for a symbol.
// e.g. "Top 2% (6th of 4040 symbols by structural centrality)"
// Returns "" when graph unavailable, symbol not found, or PageRank is 0.
// The caller may pass the already-fetched pagerank value to avoid re-querying.
func (s *Store) SymbolStructuralRank(ctx context.Context, repoKey, name, file string, pagerank float64) string {
	if pagerank <= 0 {
		return ""
	}

	graphName := GraphNameFor(repoKey)

	// Query 1: total symbols with pagerank.
	totalCypher := `MATCH (s:Symbol) WHERE s.pagerank IS NOT NULL RETURN count(s)`
	totalRows, err := s.ExecCypher(ctx, graphName, totalCypher, 1)
	if err != nil {
		if IsGraphMissingError(err) {
			recordGraphMissing("understand")
			slog.Debug("codegraph: structural rank skipped — graph absent", slog.String("graph", graphName))
		}
		return ""
	}
	if len(totalRows) == 0 {
		return ""
	}
	total, err := strconv.Atoi(strings.Trim(totalRows[0][0], `"`))
	if err != nil || total <= 0 {
		return ""
	}

	// Query 2: how many symbols have pagerank >= this symbol's pagerank (its rank).
	rankCypher := fmt.Sprintf(
		`MATCH (s:Symbol) WHERE s.pagerank IS NOT NULL AND toFloat(s.pagerank) >= %f RETURN count(s)`,
		pagerank)
	rankRows, err := s.ExecCypher(ctx, graphName, rankCypher, 1)
	if err != nil || len(rankRows) == 0 {
		return ""
	}
	rank, err := strconv.Atoi(strings.Trim(rankRows[0][0], `"`))
	if err != nil || rank <= 0 {
		return ""
	}

	pct := int(math.Round(float64(rank) / float64(total) * 100))
	if pct <= 0 {
		pct = 1
	}
	return fmt.Sprintf("Top %d%% (%dth of %d symbols by structural centrality)", pct, rank, total)
}

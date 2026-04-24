package codegraph

import (
	"context"
	"encoding/binary"
	"fmt"
	"hash/fnv"
	"math/rand/v2"
	"regexp"
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

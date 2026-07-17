package embeddings

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/anatolykoptev/go-code/internal/pgutil"
)

// schemaQuerier is the minimal pool surface EnsureSchema needs.
// *pgxpool.Pool satisfies it; tests inject a fake.
type schemaQuerier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Begin(ctx context.Context) (pgx.Tx, error)
}

// schemaAction is one CREATE/ALTER statement that needs to run on the cold path.
type schemaAction struct {
	sql             string
	desc            string
	needsIdxTimeout bool // true for CREATE INDEX (needs raised statement_timeout)
}

// schemaStatements holds the individual DDL statements from schemaSQL.
// It is populated at init time so the cold path can emit each statement
// conditionally while keeping the original SQL byte-identical.
var schemaStatements = splitSchemaSQL(schemaSQL)

var (
	reCreateExtension = regexp.MustCompile(`(?i)^\s*CREATE\s+EXTENSION\s+IF\s+NOT\s+EXISTS\s+(\S+)`)
	reCreateTable     = regexp.MustCompile(`(?i)^\s*CREATE\s+TABLE\s+IF\s+NOT\s+EXISTS\s+(\S+)`)
	reCreateIndex     = regexp.MustCompile(`(?i)^\s*CREATE\s+INDEX\s+IF\s+NOT\s+EXISTS\s+(\S+)\s+ON\s+(\S+)`)
	reAlterTableCol   = regexp.MustCompile(`(?i)^\s*ALTER\s+TABLE\s+(\S+)\s+ADD\s+COLUMN\s+IF\s+NOT\s+EXISTS\s+(\S+)`)
	reSQLComment      = regexp.MustCompile(`--[^\n]*`)
)

// splitSchemaSQL splits a multi-statement DDL string on semicolons and trims
// leading/trailing whitespace from each part. Internal whitespace (including
// indentation) is preserved so each returned statement stays byte-identical to
// the corresponding statement in the source string. SQL line comments are
// stripped first so trailing comments after a semicolon do not leak into the
// next statement.
func splitSchemaSQL(sql string) []string {
	sql = reSQLComment.ReplaceAllString(sql, "")
	parts := strings.Split(sql, ";")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		out = append(out, p)
	}
	return out
}

type parsedStmt struct {
	kind  string // extension, table, index, column
	name  string // extension/index name
	table string // table name
	extra string // column name
}

// parseSchemaStmt classifies a single DDL statement from schemaSQL and extracts
// the names needed for catalog existence checks.
func parseSchemaStmt(stmt string) (parsedStmt, error) {
	if m := reCreateExtension.FindStringSubmatch(stmt); m != nil {
		return parsedStmt{kind: "extension", name: m[1]}, nil
	}
	if m := reCreateTable.FindStringSubmatch(stmt); m != nil {
		return parsedStmt{kind: "table", table: normalizeTable(m[1])}, nil
	}
	if m := reCreateIndex.FindStringSubmatch(stmt); m != nil {
		return parsedStmt{kind: "index", name: m[1], table: normalizeTable(m[2])}, nil
	}
	if m := reAlterTableCol.FindStringSubmatch(stmt); m != nil {
		return parsedStmt{kind: "column", table: normalizeTable(m[1]), extra: m[2]}, nil
	}
	return parsedStmt{}, fmt.Errorf("unrecognized schema statement: %.80s", stmt)
}

// normalizeTable strips a 'public.' schema prefix if present. All catalog
// queries use 'public' as the schema, so we only need the bare table name.
func normalizeTable(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, `"`)
	return strings.TrimPrefix(s, "public.")
}

// schemaOutcome maps an init error to a metric label.
//   - SQLSTATE 57014 (query_canceled) -> timeout
//   - SQLSTATE 55P03 (lock_not_available) -> lock_timeout
//   - everything else -> error
func schemaOutcome(err error) string {
	if err == nil {
		return "ok"
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "57014":
			return "timeout"
		case "55P03":
			return "lock_timeout"
		}
	}
	return "error"
}

type tableState struct {
	existed bool
	ownerOK bool
}

// runEnsureSchema is the actual schema-init work. It is wrapped by EnsureSchema
// with singleflight + a success-only latch.
func (s *Store) runEnsureSchema(ctx context.Context) error {
	tables := make(map[string]*tableState)
	var queue []schemaAction

	for _, stmt := range schemaStatements {
		p, err := parseSchemaStmt(stmt)
		if err != nil {
			return err
		}
		switch p.kind {
		case "extension":
			exists, err := s.extensionExists(ctx)
			if err != nil {
				return err
			}
			if !exists {
				queue = append(queue, schemaAction{sql: stmt, desc: "create extension vector"})
			}
		case "table":
			exists, err := s.tableExists(ctx, p.table)
			if err != nil {
				return err
			}
			tables[p.table] = &tableState{existed: exists}
			if !exists {
				queue = append(queue, schemaAction{sql: stmt, desc: "create table " + p.table})
			}
		case "column":
			exists, err := s.columnExists(ctx, p.table, p.extra)
			if err != nil {
				return err
			}
			if !exists {
				queue = append(queue, schemaAction{sql: stmt, desc: "add column " + p.extra})
			}
		case "index":
			exists, err := s.indexExists(ctx, p.table, p.name)
			if err != nil {
				return err
			}
			if !exists {
				queue = append(queue, schemaAction{sql: stmt, desc: "create index " + p.name, needsIdxTimeout: true})
			}
		default:
			return fmt.Errorf("unknown schema statement kind: %q", p.kind)
		}
	}

	for name, st := range tables {
		if !st.existed {
			continue
		}
		ok, err := s.tableOwnedByCurrentUser(ctx, name)
		if err != nil {
			return err
		}
		st.ownerOK = ok
	}

	if len(queue) > 0 {
		tx, err := s.schema.Begin(ctx)
		if err != nil {
			return fmt.Errorf("begin schema tx: %w", err)
		}
		defer tx.Rollback(ctx)

		if _, err := tx.Exec(ctx, "SET LOCAL lock_timeout = '3s'"); err != nil {
			return fmt.Errorf("set lock_timeout: %w", err)
		}

		idxTimeoutSet := false
		for _, a := range queue {
			if a.needsIdxTimeout && !idxTimeoutSet {
				if _, err := tx.Exec(ctx, "SET LOCAL statement_timeout = '120s'"); err != nil {
					return fmt.Errorf("set statement_timeout: %w", err)
				}
				idxTimeoutSet = true
			}
			if _, err := tx.Exec(ctx, a.sql); err != nil {
				return fmt.Errorf("%s: %w", a.desc, err)
			}
		}

		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit schema tx: %w", err)
		}
	}

	s.transferOwnership(ctx, tables)
	return nil
}

func (s *Store) transferOwnership(ctx context.Context, tables map[string]*tableState) {
	for name, st := range tables {
		if st.existed && !st.ownerOK {
			pgutil.TransferOwnership(ctx, s.schema, "embeddings", "public."+name)
		}
	}
}

func (s *Store) extensionExists(ctx context.Context) (bool, error) {
	var one int
	err := s.schema.QueryRow(ctx, "SELECT 1 FROM pg_extension WHERE extname = 'vector'").Scan(&one)
	return rowExists(err)
}

func (s *Store) tableExists(ctx context.Context, table string) (bool, error) {
	var one int
	err := s.schema.QueryRow(ctx, "SELECT 1 FROM information_schema.tables WHERE table_schema = 'public' AND table_name = $1", table).Scan(&one)
	return rowExists(err)
}

func (s *Store) columnExists(ctx context.Context, table, column string) (bool, error) {
	var one int
	err := s.schema.QueryRow(ctx, "SELECT 1 FROM information_schema.columns WHERE table_schema = 'public' AND table_name = $1 AND column_name = $2", table, column).Scan(&one)
	return rowExists(err)
}

func (s *Store) indexExists(ctx context.Context, table, index string) (bool, error) {
	var one int
	err := s.schema.QueryRow(ctx, "SELECT 1 FROM pg_indexes WHERE schemaname = 'public' AND tablename = $1 AND indexname = $2", table, index).Scan(&one)
	return rowExists(err)
}

func (s *Store) tableOwnedByCurrentUser(ctx context.Context, table string) (bool, error) {
	var one int
	err := s.schema.QueryRow(ctx, "SELECT 1 FROM pg_tables WHERE schemaname = 'public' AND tablename = $1 AND tableowner = current_user", table).Scan(&one)
	return rowExists(err)
}

func rowExists(err error) (bool, error) {
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

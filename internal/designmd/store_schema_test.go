package designmd

import (
	"context"
	"errors"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type fakeRow struct{ err error }

func (r *fakeRow) Scan(...any) error { return r.err }

type fakeRows struct{}

func (fakeRows) Close()                                       {}
func (fakeRows) Err() error                                   { return nil }
func (fakeRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (fakeRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (fakeRows) Next() bool                                   { return false }
func (fakeRows) Scan(...any) error                            { return errors.New("fakeRows.Scan called") }
func (fakeRows) Values() ([]any, error)                       { return nil, nil }
func (fakeRows) RawValues() [][]byte                          { return nil }
func (fakeRows) Conn() *pgx.Conn                              { return nil }

type fakeTx struct {
	pgx.Tx
	db *fakeDB
}

func (tx *fakeTx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	return tx.db.Exec(ctx, sql, args...)
}
func (tx *fakeTx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	return tx.db.Query(ctx, sql, args...)
}
func (tx *fakeTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return tx.db.QueryRow(ctx, sql, args...)
}
func (tx *fakeTx) Commit(context.Context) error   { return nil }
func (tx *fakeTx) Rollback(context.Context) error { return nil }

var reAlterOwner = regexp.MustCompile(`(?i)^\s*ALTER\s+TABLE\s+(\S+)\s+OWNER\s+TO`)

type fakeDB struct {
	mu    sync.Mutex
	execs []string

	ext     bool
	tables  map[string]bool
	indexes map[string]bool
	owners  map[string]string
	curUser string

	failSQL string
	execErr error

	blockDDL string
	block    chan struct{}
	started  chan struct{}
}

func newFakeDB() *fakeDB {
	return &fakeDB{
		tables:  make(map[string]bool),
		indexes: make(map[string]bool),
		owners:  make(map[string]string),
		curUser: "app",
	}
}

func (f *fakeDB) Exec(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
	f.mu.Lock()
	f.execs = append(f.execs, sql)

	if f.blockDDL != "" && strings.Contains(sql, f.blockDDL) && f.block != nil {
		if f.started != nil {
			close(f.started)
		}
		f.mu.Unlock()
		<-f.block
		f.mu.Lock()
	}

	if f.failSQL != "" && strings.Contains(sql, f.failSQL) {
		err := f.execErr
		f.mu.Unlock()
		return pgconn.CommandTag{}, err
	}
	f.mu.Unlock()

	f.applyDDL(sql)
	return pgconn.CommandTag{}, nil
}

func (f *fakeDB) Query(_ context.Context, _ string, _ ...any) (pgx.Rows, error) {
	return fakeRows{}, nil
}

func (f *fakeDB) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	f.mu.Lock()
	defer f.mu.Unlock()

	switch {
	case strings.Contains(sql, "FROM pg_extension"):
		if f.ext {
			return &fakeRow{err: nil}
		}
		return &fakeRow{err: pgx.ErrNoRows}
	case strings.Contains(sql, "information_schema.tables"):
		if len(args) > 0 {
			if tbl, ok := argString(args[0]); ok && f.tables[tbl] {
				return &fakeRow{err: nil}
			}
		}
		return &fakeRow{err: pgx.ErrNoRows}
	case strings.Contains(sql, "FROM pg_indexes"):
		if len(args) > 1 {
			tbl, tok := argString(args[0])
			idx, iok := argString(args[1])
			if tok && iok && f.indexes[tbl+"."+idx] {
				return &fakeRow{err: nil}
			}
		}
		return &fakeRow{err: pgx.ErrNoRows}
	case strings.Contains(sql, "FROM pg_tables"):
		if len(args) > 0 {
			if tbl, ok := argString(args[0]); ok && f.tables[tbl] && f.owners[tbl] == f.curUser {
				return &fakeRow{err: nil}
			}
		}
		return &fakeRow{err: pgx.ErrNoRows}
	}
	return &fakeRow{err: pgx.ErrNoRows}
}

func argString(v any) (string, bool) {
	s, ok := v.(string)
	return s, ok
}

func (f *fakeDB) Begin(context.Context) (pgx.Tx, error) {
	return &fakeTx{db: f}, nil
}

func (f *fakeDB) applyDDL(sql string) {
	f.mu.Lock()
	defer f.mu.Unlock()

	sql = strings.TrimSpace(sql)
	upper := strings.ToUpper(sql)

	switch {
	case strings.HasPrefix(upper, "SET LOCAL"):
	case strings.HasPrefix(upper, "CREATE EXTENSION"):
		f.ext = true
	case strings.HasPrefix(upper, "CREATE TABLE"):
		if m := reCreateTable.FindStringSubmatch(sql); m != nil {
			tbl := normalizeTable(m[1])
			f.tables[tbl] = true
			f.owners[tbl] = f.curUser
		}
	case strings.HasPrefix(upper, "CREATE INDEX"):
		if m := reCreateIndex.FindStringSubmatch(sql); m != nil {
			tbl := normalizeTable(m[2])
			f.indexes[tbl+"."+m[1]] = true
		}
	case strings.HasPrefix(upper, "ALTER TABLE"):
		if m := reAlterOwner.FindStringSubmatch(sql); m != nil {
			tbl := normalizeTable(m[1])
			f.owners[tbl] = f.curUser
		}
	}
}

func (f *fakeDB) ddlCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, sql := range f.execs {
		s := strings.TrimSpace(sql)
		if strings.HasPrefix(s, "CREATE ") || strings.HasPrefix(s, "ALTER TABLE") {
			n++
		}
	}
	return n
}

func (f *fakeDB) hasExec(substr string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, sql := range f.execs {
		if strings.Contains(sql, substr) {
			return true
		}
	}
	return false
}

func (f *fakeDB) clearExecs() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.execs = nil
}

func storeForTest(db *fakeDB) *Store {
	return &Store{schema: db}
}

func TestEnsureSchema_Idempotent_Warm(t *testing.T) {
	db := newFakeDB()
	db.ext = true
	db.tables["design_embeddings"] = true
	db.indexes["design_embeddings.idx_design_emb_hnsw"] = true
	db.owners["design_embeddings"] = "app"

	s := storeForTest(db)
	if err := s.EnsureSchema(context.Background()); err != nil {
		t.Fatalf("first EnsureSchema: %v", err)
	}
	if n := db.ddlCount(); n != 0 {
		t.Fatalf("warm first run emitted %d DDL statements; want 0", n)
	}

	db.clearExecs()
	if err := s.EnsureSchema(context.Background()); err != nil {
		t.Fatalf("second EnsureSchema: %v", err)
	}
	if n := db.ddlCount(); n != 0 {
		t.Fatalf("warm second run emitted %d DDL statements; want 0", n)
	}
	if !s.schemaDone.Load() {
		t.Fatal("schemaDone not latched after success")
	}
}

func TestEnsureSchema_Retry_TransientFailure(t *testing.T) {
	db := newFakeDB()
	db.ext = false
	db.failSQL = "CREATE EXTENSION"
	db.execErr = &pgconn.PgError{Code: "57014"}

	s := storeForTest(db)
	if err := s.EnsureSchema(context.Background()); err == nil {
		t.Fatal("expected first EnsureSchema to fail")
	}
	if s.schemaDone.Load() {
		t.Fatal("schemaDone must not latch after failure")
	}

	db.failSQL = ""
	if err := s.EnsureSchema(context.Background()); err != nil {
		t.Fatalf("second EnsureSchema: %v", err)
	}
	if !s.schemaDone.Load() {
		t.Fatal("schemaDone must latch after retry success")
	}
	if !db.ext {
		t.Fatal("extension was not created on retry")
	}
}

func TestEnsureSchema_Concurrent_Dedup(t *testing.T) {
	db := newFakeDB()
	db.ext = false
	db.blockDDL = "CREATE EXTENSION"
	db.block = make(chan struct{})
	db.started = make(chan struct{})

	s := storeForTest(db)
	ctx := context.Background()

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		_ = s.EnsureSchema(ctx)
	}()

	<-db.started

	go func() {
		defer wg.Done()
		_ = s.EnsureSchema(ctx)
	}()

	time.Sleep(20 * time.Millisecond)
	close(db.block)
	wg.Wait()

	count := 0
	for _, sql := range db.execs {
		if strings.Contains(sql, "CREATE EXTENSION") {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("CREATE EXTENSION executed %d times; want 1 (singleflight dedup)", count)
	}
}

func TestEnsureSchema_LockTimeout_FastFail(t *testing.T) {
	db := newFakeDB()
	db.ext = false
	db.failSQL = "SET LOCAL lock_timeout"
	db.execErr = &pgconn.PgError{Code: "55P03"}

	s := storeForTest(db)
	err := s.EnsureSchema(context.Background())
	if err == nil {
		t.Fatal("expected lock_timeout error to propagate")
	}
	if !strings.Contains(err.Error(), "lock_timeout") {
		t.Fatalf("expected lock_timeout in error, got %v", err)
	}
}

func TestEnsureSchema_LogsFailure(t *testing.T) {
	db := newFakeDB()
	db.ext = false
	db.failSQL = "CREATE EXTENSION"
	db.execErr = errors.New("injected schema failure")

	var buf strings.Builder
	handler := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelError})
	old := slog.Default()
	slog.SetDefault(slog.New(handler))
	defer slog.SetDefault(old)

	s := storeForTest(db)
	_ = s.EnsureSchema(context.Background())

	if !strings.Contains(buf.String(), "designmd: schema init failed") {
		t.Fatalf("expected error log, got: %s", buf.String())
	}
}

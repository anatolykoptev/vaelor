package pgutil

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

// fakeQueryExecer is a DB-free QueryExecer for unit-testing the guarded
// TransferOwnership without a live Postgres. The pre-check (QueryRow) and the
// ALTER (Exec) are independently controllable so each guard branch can be
// exercised and falsified.
type fakeQueryExecer struct {
	// ownedByCurrent is what the pg_tables pre-check Scan returns.
	ownedByCurrent bool
	// preCheckErr, when non-nil, is returned by QueryRow().Scan instead of
	// ownedByCurrent (simulates a connection error / no-row / etc.).
	preCheckErr error

	// execErr is returned by Exec (the ALTER). nil = success.
	execErr error
	// execCalled records whether Exec was invoked at all — the idempotency
	// gate asserts this stays false when the pre-check says "already owner".
	execCalled bool
	// lastSQL is the ALTER SQL Exec was called with (empty if not called).
	lastSQL string
}

func (f *fakeQueryExecer) Exec(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
	f.execCalled = true
	f.lastSQL = sql
	return pgconn.CommandTag{}, f.execErr
}

func (f *fakeQueryExecer) QueryRow(_ context.Context, _ string, _ ...any) pgx.Row {
	return fakeRow{owned: f.ownedByCurrent, err: f.preCheckErr}
}

// fakeRow implements pgx.Row for the single Scan TransferOwnership performs.
type fakeRow struct {
	owned bool
	err   error
}

func (r fakeRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(dest) > 0 {
		bp, _ := dest[0].(*bool)
		if bp != nil {
			*bp = r.owned
		}
	}
	return nil
}

func TestTransferOwnership(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name            string
		ownedByCurrent  bool
		preCheckErr     error
		execErr         error
		wantExecCalled  bool // whether the ALTER should be issued
		wantMetricDelta int  // expected change in ownershipTransferFailedTotal
	}{
		{
			name:            "already owner: skip ALTER entirely (idempotent no-op)",
			ownedByCurrent:  true,
			wantExecCalled:  false,
			wantMetricDelta: 0,
		},
		{
			name:            "not owner, ALTER succeeds (superuser re-owns): no metric, no panic",
			ownedByCurrent:  false,
			execErr:         nil,
			wantExecCalled:  true,
			wantMetricDelta: 0,
		},
		{
			name:            "not owner, 42501 insufficient_privilege: fail-soft, metric +1, no panic",
			ownedByCurrent:  false,
			execErr:         &pgconn.PgError{Code: "42501"},
			wantExecCalled:  true,
			wantMetricDelta: 1,
		},
		{
			name:            "not owner, 42P01 relation gone: swallowed as warning, no panic, no metric",
			ownedByCurrent:  false,
			execErr:         &pgconn.PgError{Code: "42P01"},
			wantExecCalled:  true,
			wantMetricDelta: 0,
		},
		{
			name:            "not owner, non-pg error: swallowed as warning, no panic, no metric",
			ownedByCurrent:  false,
			execErr:         errors.New("connection reset by peer"),
			wantExecCalled:  true,
			wantMetricDelta: 0,
		},
		{
			name:            "pre-check query fails: skip ALTER, no panic, no metric",
			preCheckErr:     errors.New("connection refused"),
			wantExecCalled:  false,
			wantMetricDelta: 0,
		},
		{
			name:            "wrapped 42501: fail-soft via errors.As, metric +1",
			ownedByCurrent:  false,
			execErr:         fmt.Errorf("outer: %w", &pgconn.PgError{Code: "42501"}),
			wantExecCalled:  true,
			wantMetricDelta: 1,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			table := "public.test_tbl_" + strings.ReplaceAll(strings.ReplaceAll(tc.name, " ", "_"), ",", "")
			before := metricVal(t, table)

			ex := &fakeQueryExecer{
				ownedByCurrent: tc.ownedByCurrent,
				preCheckErr:    tc.preCheckErr,
				execErr:        tc.execErr,
			}
			// Must never panic regardless of error type.
			TransferOwnership(context.Background(), ex, "testpkg", table)

			if ex.execCalled != tc.wantExecCalled {
				t.Errorf("ALTER issued = %v, want %v (ownedByCurrent=%v preCheckErr=%v)",
					ex.execCalled, tc.wantExecCalled, tc.ownedByCurrent, tc.preCheckErr)
			}
			// When the ALTER runs, CURRENT_USER keyword + table name must be inlined.
			if ex.execCalled {
				if !strings.Contains(ex.lastSQL, "CURRENT_USER") {
					t.Errorf("SQL %q does not contain CURRENT_USER keyword", ex.lastSQL)
				}
				if !strings.Contains(ex.lastSQL, table) {
					t.Errorf("SQL %q does not contain table name %q", ex.lastSQL, table)
				}
			}

			got := metricVal(t, table) - before
			if got != float64(tc.wantMetricDelta) {
				t.Errorf("failure counter delta = %v, want %d", got, tc.wantMetricDelta)
			}
		})
	}
}

// metricVal reads the current value of ownershipTransferFailedTotal{table}.
func metricVal(t *testing.T, table string) float64 {
	t.Helper()
	return testutil.ToFloat64(ownershipTransferFailedTotal.WithLabelValues(table))
}

// levelCapture is a slog.Handler that records every emitted record so a test
// can assert the LEVEL of a log line (not just its presence).
type levelCapture struct {
	mu      sync.Mutex
	records []slog.Record
}

func (h *levelCapture) Enabled(context.Context, slog.Level) bool { return true }
func (h *levelCapture) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, r.Clone())
	return nil
}
func (h *levelCapture) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *levelCapture) WithGroup(string) slog.Handler      { return h }

// TestTransferOwnership_LogLevels pins the anti-spam property (issue #520): the
// 42501 "not owner" path recurs on every rebuild until an operator normalizes
// ownership, so it MUST log at DEBUG, not WARN. Without this, reverting
// slog.Debug -> slog.Warn on that path leaves every other test green (they only
// check exec/metric, never the level) — the spam guarantee would be
// unfalsifiable. Other (non-42501) transfer errors stay at WARN.
//
// Not parallel: swaps slog.Default().
func TestTransferOwnership_LogLevels(t *testing.T) {
	cases := []struct {
		name      string
		execErr   error
		wantLevel slog.Level
		wantMsg   string
	}{
		{
			name:      "42501 not-owner recurring path logs at DEBUG (a WARN would spam every rebuild)",
			execErr:   &pgconn.PgError{Code: "42501"},
			wantLevel: slog.LevelDebug,
			wantMsg:   "cannot transfer table ownership",
		},
		{
			name:      "other transfer error logs at WARN",
			execErr:   errors.New("connection reset by peer"),
			wantLevel: slog.LevelWarn,
			wantMsg:   "transfer table owner",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cap := &levelCapture{}
			orig := slog.Default()
			slog.SetDefault(slog.New(cap))
			defer slog.SetDefault(orig)

			ex := &fakeQueryExecer{ownedByCurrent: false, execErr: tc.execErr}
			TransferOwnership(context.Background(), ex, "testpkg", "public.tbl_loglevel")

			var gotLevel slog.Level
			found := false
			for _, r := range cap.records {
				if strings.Contains(r.Message, tc.wantMsg) {
					gotLevel, found = r.Level, true
				}
			}
			if !found {
				t.Fatalf("no log record containing %q was emitted (records=%d)", tc.wantMsg, len(cap.records))
			}
			if gotLevel != tc.wantLevel {
				t.Errorf("transfer-failure log level = %v, want %v (the anti-spam property)", gotLevel, tc.wantLevel)
			}
		})
	}
}

func TestIsInsufficientPrivilege(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"42501", &pgconn.PgError{Code: "42501"}, true},
		{"42P01", &pgconn.PgError{Code: "42P01"}, false},
		{"non-pg error", errors.New("boom"), false},
		{"wrapped 42501", fmt.Errorf("wrap: %w", &pgconn.PgError{Code: "42501"}), true},
		{"wrapped non-42501", fmt.Errorf("wrap: %w", &pgconn.PgError{Code: "42P01"}), false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := isInsufficientPrivilege(tc.err); got != tc.want {
				t.Errorf("isInsufficientPrivilege(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestSplitTable(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in        string
		wantSch   string
		wantTable string
	}{
		{"public.code_graph_meta", "public", "code_graph_meta"},
		{"code_graph_meta", "public", "code_graph_meta"},
		{"ag_catalog.code_repo_state", "ag_catalog", "code_repo_state"},
	}
	for _, tc := range cases {
		gotSch, gotTbl := splitTable(tc.in)
		if gotSch != tc.wantSch || gotTbl != tc.wantTable {
			t.Errorf("splitTable(%q) = (%q, %q), want (%q, %q)", tc.in, gotSch, gotTbl, tc.wantSch, tc.wantTable)
		}
	}
}

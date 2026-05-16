package pgutil

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

// fakeExec implements Execer for tests without a live Postgres instance.
type fakeExec struct {
	lastSQL string
	err     error
}

func (f *fakeExec) Exec(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
	f.lastSQL = sql
	return pgconn.CommandTag{}, f.err
}

func TestTransferOwnership(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		execErr error
	}{
		{
			name:    "success: nil error returns cleanly",
			execErr: nil,
		},
		{
			name:    "42501 insufficient_privilege: fail-soft, no panic",
			execErr: &pgconn.PgError{Code: "42501"},
		},
		{
			name:    "42P01 relation does not exist: swallowed as warning, no panic",
			execErr: &pgconn.PgError{Code: "42P01"},
		},
		{
			name:    "non-pg error: swallowed as warning, no panic",
			execErr: errors.New("connection reset by peer"),
		},
		{
			name:    "wrapped 42501: fail-soft via errors.As",
			execErr: fmt.Errorf("outer: %w", &pgconn.PgError{Code: "42501"}),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ex := &fakeExec{err: tc.execErr}
			// Must never panic regardless of error type.
			TransferOwnership(context.Background(), ex, "testpkg", "some_table")

			// CURRENT_USER keyword must appear in generated SQL.
			if !strings.Contains(ex.lastSQL, "CURRENT_USER") {
				t.Errorf("SQL %q does not contain CURRENT_USER keyword", ex.lastSQL)
			}
			// Table name must be inlined.
			if !strings.Contains(ex.lastSQL, "some_table") {
				t.Errorf("SQL %q does not contain table name", ex.lastSQL)
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

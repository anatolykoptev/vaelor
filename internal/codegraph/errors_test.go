package codegraph

import (
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

// TestIsGraphMissingError verifies SQLSTATE detection for graph-absent conditions.
func TestIsGraphMissingError(t *testing.T) {
	t.Run("nil returns false", func(t *testing.T) {
		if IsGraphMissingError(nil) {
			t.Error("expected false for nil error")
		}
	})

	t.Run("non-pg error returns false", func(t *testing.T) {
		if IsGraphMissingError(errors.New("some random error")) {
			t.Error("expected false for non-pg error")
		}
	})

	t.Run("42P01 undefined_table returns true", func(t *testing.T) {
		pgErr := &pgconn.PgError{Code: "42P01", Message: "relation does not exist"}
		if !IsGraphMissingError(pgErr) {
			t.Error("expected true for SQLSTATE 42P01")
		}
	})

	t.Run("42704 undefined_object returns true", func(t *testing.T) {
		pgErr := &pgconn.PgError{Code: "42704", Message: "type does not exist"}
		if !IsGraphMissingError(pgErr) {
			t.Error("expected true for SQLSTATE 42704")
		}
	})

	t.Run("3F000 invalid_schema_name returns true", func(t *testing.T) {
		pgErr := &pgconn.PgError{Code: "3F000", Message: "invalid schema name"}
		if !IsGraphMissingError(pgErr) {
			t.Error("expected true for SQLSTATE 3F000")
		}
	})

	t.Run("other pg error returns false", func(t *testing.T) {
		pgErr := &pgconn.PgError{Code: "42601", Message: "syntax error"}
		if IsGraphMissingError(pgErr) {
			t.Error("expected false for unrelated SQLSTATE 42601")
		}
	})

	t.Run("wrapped pg error returns true", func(t *testing.T) {
		pgErr := &pgconn.PgError{Code: "42P01", Message: "graph does not exist"}
		wrapped := fmt.Errorf("exec cypher: %w", pgErr)
		if !IsGraphMissingError(wrapped) {
			t.Error("expected true for wrapped pg 42P01 error")
		}
	})
}

// TestErrGraphNotIndexed verifies the sentinel error value.
func TestErrGraphNotIndexed(t *testing.T) {
	if ErrGraphNotIndexed == nil {
		t.Fatal("ErrGraphNotIndexed must not be nil")
	}
	if ErrGraphNotIndexed.Error() == "" {
		t.Error("ErrGraphNotIndexed must have a non-empty message")
	}
	// Sentinel must be identifiable via errors.Is.
	wrapped := fmt.Errorf("query: %w", ErrGraphNotIndexed)
	if !errors.Is(wrapped, ErrGraphNotIndexed) {
		t.Error("errors.Is must match wrapped ErrGraphNotIndexed")
	}
}

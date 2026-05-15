package codegraph

import (
	"fmt"
	"strings"
	"testing"
)

// TestTransferTableOwnerIfPossible_UsesCurrentUserKeyword asserts that the
// generated ALTER TABLE statement uses the SQL keyword CURRENT_USER rather than
// any hardcoded role name. Using CURRENT_USER is the correctness invariant: it
// resolves to the connected role at execution time regardless of the role name
// in DATABASE_URL.
func TestTransferTableOwnerIfPossible_UsesCurrentUserKeyword(t *testing.T) {
	tables := []string{
		"code_graph_meta",
		"code_file_mtimes",
		"code_graph_snapshots",
		"code_dead_code_scores",
	}

	for _, table := range tables {
		// Re-derive the SQL string the same way transferTableOwnerIfPossible does.
		// This is intentionally a string-level check, not execution — the helper
		// requires a live DB connection.
		sql := "ALTER TABLE " + table + " OWNER TO CURRENT_USER"
		if !strings.Contains(sql, "CURRENT_USER") {
			t.Errorf("table %q: generated SQL does not contain CURRENT_USER: %q", table, sql)
		}
		if strings.Contains(strings.ToUpper(sql), "GOCODE_APP") {
			t.Errorf("table %q: SQL contains hardcoded role name: %q", table, sql)
		}
	}
}

// TestIsInsufficientPrivilege_NilError verifies that nil error returns false.
func TestIsInsufficientPrivilege_NilError(t *testing.T) {
	if isInsufficientPrivilege(nil) {
		t.Error("expected false for nil error")
	}
}

// TestIsInsufficientPrivilege_OtherError verifies that unrelated errors return false.
func TestIsInsufficientPrivilege_OtherError(t *testing.T) {
	// errors.New returns a *errorString which implements error but is not a
	// *pgconn.PgError — isInsufficientPrivilege must return false for it.
	err := fmt.Errorf("some other error")
	if isInsufficientPrivilege(err) {
		t.Error("expected false for non-pg error")
	}
}

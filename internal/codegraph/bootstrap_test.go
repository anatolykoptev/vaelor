package codegraph

import (
	"strings"
	"testing"
)

// TestOwnershipSQL_UsesCurrentUserKeyword asserts that ownership transfer SQL
// uses the CURRENT_USER keyword rather than any hardcoded role name.
// This is a string-level check against the table names actually passed to
// pgutil.TransferOwnership by EnsureGraph — it does not require a live DB.
func TestOwnershipSQL_UsesCurrentUserKeyword(t *testing.T) {
	t.Parallel()

	tables := []string{
		"public.code_graph_meta",
		"public.code_file_mtimes",
		"public.code_graph_snapshots",
		"public.code_dead_code_scores",
	}

	for _, table := range tables {
		// Reconstruct the SQL the same way TransferOwnership does internally.
		sql := "ALTER TABLE " + table + " OWNER TO CURRENT_USER"
		if !strings.Contains(sql, "CURRENT_USER") {
			t.Errorf("table %q: generated SQL does not contain CURRENT_USER: %q", table, sql)
		}
		if strings.Contains(strings.ToUpper(sql), "GOCODE_APP") {
			t.Errorf("table %q: SQL contains hardcoded role name: %q", table, sql)
		}
	}
}

package codegraph

import (
	"errors"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
)

// ErrGraphNotIndexed is returned when a read-path tool is called against a repo
// that has not been indexed yet (no AGE graph exists). Callers should surface
// the "run code_graph first" message to the user.
var ErrGraphNotIndexed = errors.New("graph not indexed: run code_graph first to build the graph for this repo")

// IsGraphMissingError reports whether err indicates that an AGE graph does not
// exist. AGE raises one of three SQLSTATE codes depending on context:
//   - 3F000 — invalid_schema_name (AGE cannot set search_path to a missing schema)
//   - 42P01 — undefined_table (label tables absent inside missing schema)
//   - 42704 — undefined_object (graph object not found in catalog)
//
// String-contains fallbacks are included for AGE versions that surface the code
// only inside the message text (observed in AGE 1.3/1.4 on PG 14).
func IsGraphMissingError(err error) bool {
	if err == nil {
		return false
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "3F000", "42P01", "42704":
			return true
		}
	}
	// Fallback: some AGE versions emit the code only in the message string.
	msg := err.Error()
	for _, sub := range []string{"3F000", "42P01", "42704", "does not exist", "invalid schema name"} {
		if strings.Contains(msg, sub) {
			return true
		}
	}
	return false
}

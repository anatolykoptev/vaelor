package strutil

import (
	"fmt"
	"math/rand/v2"
	"strings"
)

// EscapeCypher escapes a string for safe use in a single-quoted Cypher literal.
// Order matters: backslashes first, so the \' added for quotes is not itself
// re-escaped. Null bytes are stripped (Postgres rejects them in text); the
// whitespace controls are encoded so the literal cannot break the statement.
//
// Single escaper for every package that inlines values into Cypher —
// codegraph.escapeCypher, codegraph.escapeCypherString and
// embeddings.escapeCypherName used to diverge (#802, #808).
func EscapeCypher(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `'`, `\'`)
	s = strings.ReplaceAll(s, "\x00", "")
	s = strings.ReplaceAll(s, "\n", `\n`)
	s = strings.ReplaceAll(s, "\r", `\r`)
	s = strings.ReplaceAll(s, "\t", `\t`)
	return s
}

// SQLLiteral escapes s for use inside a single-quoted SQL literal by doubling
// single quotes — the only escape SQL string literals recognize.
func SQLLiteral(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}

// CypherDollarQuote returns a PostgreSQL dollar-quoting tag that does not
// appear in the Cypher body, and ok=false when none is found within the bound.
// The tag is checked against the FULLY ASSEMBLED body — no stripping of
// candidate tags from interpolated values (a single-pass strip is not
// idempotent and can reassemble the delimiter).
//
// Bounded: an unbounded re-roll lets a crafted body containing every
// candidate tag spin forever while holding a pooled connection (#808).
// With rand.Int64 suffixes a 64-miss run needs the body to name 2^64-ary
// candidates — beyond brute force; failure is fail-closed.
func CypherDollarQuote(cypher string) (string, bool) {
	tag := "$cq$"
	for i := 0; i < 64; i++ {
		if !strings.Contains(cypher, tag) {
			return tag, true
		}
		tag = fmt.Sprintf("$cq%d$", rand.Int64()) //nolint:gosec // tag uniqueness, not crypto
	}
	return "", false
}

// WrapCypherSQL renders the SQL statement carrying a Cypher body to
// ag_catalog.cypher: graphName as a single-quoted SQL literal (quote-doubled —
// safe for any input; callers that regex-validate the name keep that as a
// precondition), the Cypher body inside a dollar quote whose tag is verified
// absent from the fully assembled body. Returns ok=false when no safe tag
// exists — the caller must not run the statement (#808).
func WrapCypherSQL(graphName, cypher, colDefs string) (string, bool) {
	tag, ok := CypherDollarQuote(cypher)
	if !ok {
		return "", false
	}
	return fmt.Sprintf(
		`SELECT * FROM ag_catalog.cypher('%s', %s %s %s) AS (%s)`,
		SQLLiteral(graphName), tag, cypher, tag, colDefs,
	), true
}

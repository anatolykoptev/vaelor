package codegraph

import (
	"strings"
	"testing"
)

// TestEscapeCypher verifies that escapeCypher correctly handles all special characters.
func TestEscapeCypher(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "plain string unchanged",
			input: "hello world",
			want:  "hello world",
		},
		{
			name:  "single quote escaped",
			input: "it's a test",
			want:  `it\'s a test`,
		},
		{
			name:  "double quote preserved",
			input: `say "hello"`,
			want:  `say "hello"`,
		},
		{
			name:  "backslash escaped",
			input: `path\to\file`,
			want:  `path\\to\\file`,
		},
		{
			name:  "null byte stripped",
			input: "before\x00after",
			want:  "beforeafter",
		},
		{
			name:  "newline escaped",
			input: "line1\nline2",
			want:  `line1\nline2`,
		},
		{
			name:  "carriage return escaped",
			input: "line1\rline2",
			want:  `line1\rline2`,
		},
		{
			name:  "tab escaped",
			input: "col1\tcol2",
			want:  `col1\tcol2`,
		},
		{
			name:  "backtick preserved",
			input: "use `backtick`",
			want:  "use `backtick`",
		},
		{
			name:  "mixed special characters",
			input: "it's\n\"quoted\"\t\\path",
			want:  `it\'s\n"quoted"\t\\path`,
		},
		{
			name:  "empty string unchanged",
			input: "",
			want:  "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := escapeCypher(tc.input)
			if got != tc.want {
				t.Errorf("escapeCypher(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

// TestGraphName verifies determinism, prefix, and uniqueness of graphName.
func TestGraphName(t *testing.T) {
	t.Run("has code_ prefix", func(t *testing.T) {
		name := graphName("/some/repo/path")
		if !strings.HasPrefix(name, "code_") {
			t.Errorf("graphName() = %q, want prefix %q", name, "code_")
		}
	})

	t.Run("deterministic for same input", func(t *testing.T) {
		a := graphName("/home/user/project")
		b := graphName("/home/user/project")
		if a != b {
			t.Errorf("graphName not deterministic: %q != %q", a, b)
		}
	})

	t.Run("different paths produce different names", func(t *testing.T) {
		a := graphName("/home/user/project-a")
		b := graphName("/home/user/project-b")
		if a == b {
			t.Errorf("different paths produced same graph name %q", a)
		}
	})

	t.Run("exported GraphNameFor matches graphName", func(t *testing.T) {
		path := "/srv/src/repos/go-code"
		if GraphNameFor(path) != graphName(path) {
			t.Errorf("GraphNameFor and graphName diverge for %q", path)
		}
	})

	t.Run("name has expected length (code_ + 8 hex chars = 13)", func(t *testing.T) {
		name := graphName("/any/path")
		const wantLen = 13
		if len(name) != wantLen {
			t.Errorf("graphName() = %q, len=%d, want %d", name, len(name), wantLen)
		}
	})

	t.Run("empty path is stable", func(t *testing.T) {
		a := graphName("")
		b := graphName("")
		if a != b {
			t.Errorf("empty path not stable: %q != %q", a, b)
		}
	})
}

// TestIsReadOnly verifies that isReadOnly correctly classifies Cypher statements.
func TestIsReadOnly(t *testing.T) {
	tests := []struct {
		name   string
		cypher string
		want   bool
	}{
		// Read-only: should be allowed
		{
			name:   "MATCH and RETURN",
			cypher: "MATCH (n:Symbol) RETURN n",
			want:   true,
		},
		{
			name:   "MATCH with WHERE",
			cypher: "MATCH (n) WHERE n.name = 'foo' RETURN n",
			want:   true,
		},
		{
			name:   "MATCH with ORDER BY and LIMIT",
			cypher: "MATCH (a)-[r]->(b) RETURN a, r, b ORDER BY a.name LIMIT 10",
			want:   true,
		},
		// Write operations: should be blocked
		{
			name:   "CREATE node",
			cypher: "CREATE (n:Symbol {name: 'foo'})",
			want:   false,
		},
		{
			name:   "DELETE node",
			cypher: "MATCH (n) WHERE n.id = 1 DELETE n",
			want:   false,
		},
		{
			name:   "SET property",
			cypher: "MATCH (n) WHERE n.id = 1 SET n.name = 'bar'",
			want:   false,
		},
		{
			name:   "MERGE node",
			cypher: "MERGE (n:Symbol {name: 'baz'})",
			want:   false,
		},
		{
			name:   "REMOVE property",
			cypher: "MATCH (n) REMOVE n.obsolete",
			want:   false,
		},
		{
			name:   "DROP graph",
			cypher: "DROP GRAPH myrepo CASCADE",
			want:   false,
		},
		{
			name:   "DETACH DELETE",
			cypher: "MATCH (n) DETACH DELETE n",
			want:   false,
		},
		{
			name:   "CREATE case-insensitive lowercase",
			cypher: "create (n:Symbol {name: 'foo'})",
			want:   false,
		},
		{
			name:   "MERGE case-insensitive mixed",
			cypher: "Merge (n:Symbol {name: 'foo'})",
			want:   false,
		},
		{
			name:   "empty string is read-only",
			cypher: "",
			want:   true,
		},
		// #789: write keywords inside string literals are parameter values,
		// not operations — common Go method names like Set/Delete/Create.
		{
			name:   "who_calls rendered with name=Set",
			cypher: "MATCH (caller:Symbol)-[:CALLS]->(s:Symbol) WHERE s.name = 'Set' RETURN caller.name",
			want:   true,
		},
		{
			name:   "all write keywords as literal values",
			cypher: "MATCH (s:Symbol) WHERE s.name IN ['Set', 'Delete', 'Create', 'Merge', 'Remove', 'Drop', 'Detach'] RETURN s.name",
			want:   true,
		},
		{
			name:   "symbols_in rendered with a create/ path",
			cypher: "MATCH (s:Symbol) WHERE s.file STARTS WITH 'internal/create/' RETURN s.name",
			want:   true,
		},
		{
			name:   "double-quoted literal containing a write keyword",
			cypher: `MATCH (s:Symbol) WHERE s.name = "Remove" RETURN s`,
			want:   true,
		},
		{
			name:   "backtick-quoted identifier containing a write keyword",
			cypher: "MATCH (s:`Set`) RETURN s",
			want:   true,
		},
		{
			name:   "escaped quote keeps a write keyword inside the literal",
			cypher: `MATCH (s:Symbol) WHERE s.name = 'a\' ; DELETE x' RETURN s`,
			want:   true,
		},
		{
			name:   "write keyword after a closed literal is still caught",
			cypher: "MATCH (s:Symbol) WHERE s.name = 'x' DETACH DELETE s",
			want:   false,
		},
		{
			name:   "write keyword between literals is still caught",
			cypher: "RETURN 'a' + 'b' DELETE n",
			want:   false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := isReadOnly(tc.cypher)
			if got != tc.want {
				t.Errorf("isReadOnly(%q) = %v, want %v", tc.cypher, got, tc.want)
			}
		})
	}
}

// TestAgeSetupNoLOAD verifies that ageSetup no longer contains a LOAD directive.
// Regression guard: per-connection LOAD was removed in favour of shared_preload_libraries.
// If this test fails, someone re-introduced LOAD — verify postgresql.conf instead.
func TestAgeSetupNoLOAD(t *testing.T) {
	if strings.Contains(strings.ToUpper(ageSetup), "LOAD") {
		t.Errorf("ageSetup must not contain LOAD directive (rely on shared_preload_libraries): %q", ageSetup)
	}
}

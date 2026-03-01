package codegraph

import "testing"

func TestCountReturnCols(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		cypher string
		want   int
	}{
		{
			name:   "single_column",
			cypher: "MATCH (s:Symbol) RETURN s",
			want:   1,
		},
		{
			name:   "four_columns",
			cypher: "MATCH (s:Symbol) RETURN s.name, s.file, s.complexity, s.lines ORDER BY s.complexity DESC LIMIT 10",
			want:   4,
		},
		{
			name:   "with_count_aggregation",
			cypher: "MATCH (s:Symbol)<-[:CALLS]-(c:Symbol) RETURN s.name, s.kind, s.file, count(c) AS call_count ORDER BY call_count DESC LIMIT 20",
			want:   4,
		},
		{
			name:   "with_type_function",
			cypher: "MATCH (a:Symbol)-[r:INHERITS|IMPLEMENTS]->(b:Symbol) RETURN a.name, b.name, type(r) AS relation",
			want:   3,
		},
		{
			name:   "return_path",
			cypher: "MATCH path = shortestPath((a:Symbol)-[:CALLS*..10]->(b:Symbol)) RETURN path",
			want:   1,
		},
		{
			name:   "six_columns",
			cypher: "MATCH (s:Symbol)-[:HANDLES]->(r:Route)<-[:FETCHES]-(c:Symbol) RETURN s.name, s.file, r.method, r.path, c.name, c.file",
			want:   6,
		},
		{
			name:   "multiline_cypher",
			cypher: "MATCH (s:Symbol)\nWHERE s.kind = 'function'\nRETURN s.name, s.complexity\nORDER BY s.complexity DESC\nLIMIT 10",
			want:   2,
		},
		{
			name:   "multiline_four_cols",
			cypher: "MATCH (s:Symbol)\nWHERE s.kind IN ['function', 'method']\nRETURN s.name, s.file, s.complexity, s.lines\nORDER BY s.complexity DESC\nLIMIT 10",
			want:   4,
		},
		{
			name:   "no_return",
			cypher: "MATCH (s:Symbol)",
			want:   1,
		},
		{
			name:   "distinct",
			cypher: "MATCH (f:File)-[:IMPORTS]->(p:Package) RETURN DISTINCT p",
			want:   1,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := countReturnCols(tc.cypher)
			if got != tc.want {
				t.Errorf("countReturnCols(%q) = %d, want %d", tc.cypher, got, tc.want)
			}
		})
	}
}

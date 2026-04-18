package codegraph

import (
	"context"
	"strings"
	"testing"

	"github.com/anatolykoptev/go-kit/llm"
)

type captureCompleter struct {
	userPrompt string
	reply      string
}

func (c *captureCompleter) Complete(_ context.Context, _, userPrompt string, _ ...llm.ChatOption) (string, error) {
	c.userPrompt = userPrompt
	return c.reply, nil
}

func TestExtractCypher(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "plain Cypher unchanged",
			input: "MATCH (s:Symbol) WHERE s.name = 'main' RETURN s",
			want:  "MATCH (s:Symbol) WHERE s.name = 'main' RETURN s",
		},
		{
			name:  "wrapped in cypher code block",
			input: "```cypher\nMATCH (p:Package) RETURN p.name\n```",
			want:  "MATCH (p:Package) RETURN p.name",
		},
		{
			name:  "wrapped in plain code block",
			input: "```\nMATCH (f:File) RETURN f.path LIMIT 10\n```",
			want:  "MATCH (f:File) RETURN f.path LIMIT 10",
		},
		{
			name: "explanation text around code block",
			input: `Here is the query you requested:

` + "```cypher\nMATCH (s:Symbol)-[:CALLS]->(t:Symbol) WHERE s.name = 'Serve' RETURN t\n```" + `

This will return all symbols called by Serve.`,
			want: "MATCH (s:Symbol)-[:CALLS]->(t:Symbol) WHERE s.name = 'Serve' RETURN t",
		},
		{
			name:  "trimmed whitespace on plain input",
			input: "  MATCH (n) RETURN n  ",
			want:  "MATCH (n) RETURN n",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := extractCypher(tc.input)
			if got != tc.want {
				t.Errorf("extractCypher(%q)\n got: %q\nwant: %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestCypherSystemPrompt_ContainsExamples(t *testing.T) {
	prompt := cypherSystemPrompt()
	if !strings.Contains(prompt, "type(r)") {
		t.Error("freeform prompt should contain type(r) AGE-compatible example")
	}
	if !strings.Contains(prompt, "INHERITS") {
		t.Error("freeform prompt should mention INHERITS edge")
	}
	if !strings.Contains(prompt, "pagerank") {
		t.Error("freeform prompt should mention pagerank property")
	}
	if !strings.Contains(prompt, "match_vle_terminal_edge") {
		t.Error("freeform prompt must warn about the AGE NULL-start VLE pitfall")
	}
	if !strings.Contains(prompt, "WITH startNode WHERE startNode IS NOT NULL") {
		t.Error("freeform prompt must show the WITH-guard pattern for VLE")
	}
}

func TestGenerateCypherWithRetry_VLEHintOnNullEdgeError(t *testing.T) {
	t.Parallel()

	cap := &captureCompleter{reply: "MATCH (n) RETURN n"}
	firstErr := `row iteration: ERROR: match_vle_terminal_edge() arguments cannot be NULL (SQLSTATE 22023)`

	_, err := GenerateCypherWithRetry(context.Background(), cap, "show me stuff", firstErr)
	if err != nil {
		t.Fatalf("retry returned unexpected error: %v", err)
	}
	if !strings.Contains(cap.userPrompt, "variable-length path") {
		t.Errorf("retry prompt missing VLE hint, got:\n%s", cap.userPrompt)
	}
	if !strings.Contains(cap.userPrompt, "IS NOT NULL") {
		t.Errorf("retry prompt missing NULL-guard remedy, got:\n%s", cap.userPrompt)
	}
}

func TestGenerateCypherWithRetry_NoHintOnUnrelatedError(t *testing.T) {
	t.Parallel()

	cap := &captureCompleter{reply: "MATCH (n) RETURN n"}
	_, err := GenerateCypherWithRetry(context.Background(), cap, "show me stuff", "syntax error at position 42")
	if err != nil {
		t.Fatalf("retry returned unexpected error: %v", err)
	}
	if strings.Contains(cap.userPrompt, "variable-length path") {
		t.Errorf("retry prompt injected VLE hint for unrelated error:\n%s", cap.userPrompt)
	}
}

func TestIsReadOnlyGuard(t *testing.T) {
	t.Parallel()

	readCases := []struct {
		name   string
		cypher string
	}{
		{"simple match", "MATCH (n) RETURN n"},
		{"match with where", "MATCH (s:Symbol) WHERE s.kind = 'function' RETURN s.name"},
		{"match relationship", "MATCH (a)-[:CALLS]->(b) RETURN a.name, b.name"},
		{"match with limit", "MATCH (f:File) RETURN f.path ORDER BY f.lines DESC LIMIT 20"},
		{"optional match", "OPTIONAL MATCH (p:Package)-[:CONTAINS]->(f:File) RETURN p, f"},
		{"with clause", "MATCH (s:Symbol) WITH s ORDER BY s.name RETURN s.name"},
	}

	for _, tc := range readCases {
		t.Run("read/"+tc.name, func(t *testing.T) {
			t.Parallel()
			if !isReadOnly(tc.cypher) {
				t.Errorf("isReadOnly(%q) = false, want true", tc.cypher)
			}
		})
	}

	writeCases := []struct {
		name   string
		cypher string
	}{
		{"create node", "CREATE (n:Symbol {name: 'foo'})"},
		{"delete node", "MATCH (n) DELETE n"},
		{"detach delete", "MATCH (n) DETACH DELETE n"},
		{"set property", "MATCH (n) SET n.name = 'bar'"},
		{"merge", "MERGE (n:Symbol {name: 'foo'})"},
		{"remove", "MATCH (n) REMOVE n.name"},
		{"drop", "DROP GRAPH myGraph"},
		{"create lowercase", "create (n:Pkg {name: 'test'})"},
		{"mixed case set", "MATCH (n) Set n.x = 1"},
	}

	for _, tc := range writeCases {
		t.Run("write/"+tc.name, func(t *testing.T) {
			t.Parallel()
			if isReadOnly(tc.cypher) {
				t.Errorf("isReadOnly(%q) = true, want false", tc.cypher)
			}
		})
	}
}

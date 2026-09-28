package strutil

import (
	"strings"
	"testing"
)

func TestEscapeCypher(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"plain", "simple", "simple"},
		{"single quote", "O'Brien", `O\'Brien`},
		{"backslash first", `foo\`, `foo\\`},
		{"backslash-quote combo", `x\'`, `x\\\'`},
		{"null byte stripped", "a\x00b", "ab"},
		{"newline", "a\nb", `a\nb`},
		{"carriage return", "a\rb", `a\rb`},
		{"tab", "a\tb", `a\tb`},
		// A lone backslash before the closing quote must NOT escape it:
		// 'x\' keeps the literal open — the write clause that follows executes.
		{"trailing backslash", `Set\`, `Set\\`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := EscapeCypher(tc.input); got != tc.want {
				t.Errorf("EscapeCypher(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestSQLLiteral(t *testing.T) {
	t.Parallel()
	if got := SQLLiteral("g'r$a/ph"); got != "g''r$a/ph" {
		t.Errorf("SQLLiteral = %q, want doubled quote", got)
	}
	if got := SQLLiteral("plain"); got != "plain" {
		t.Errorf("SQLLiteral = %q, want unchanged", got)
	}
}

func TestCypherDollarQuote_DefaultAndCollision(t *testing.T) {
	t.Parallel()

	tag, ok := CypherDollarQuote("MATCH (n) RETURN n")
	if !ok || tag != "$cq$" {
		t.Fatalf("default tag: got %q ok=%v", tag, ok)
	}

	// Body containing $cq$ must get a different tag that is absent from the body.
	body := `MATCH (n) WHERE n.x = '$cq$' RETURN n`
	tag2, ok := CypherDollarQuote(body)
	if !ok {
		t.Fatal("no tag found for colliding body")
	}
	if tag2 == "$cq$" || strings.Contains(body, tag2) {
		t.Fatalf("tag %q still collides with body", tag2)
	}
}

func TestUnquoteAgtype(t *testing.T) {
	t.Parallel()
	cases := []struct{ in, want string }{
		{`"foo"`, "foo"},
		{`"a b"`, "a b"},
		{`  "padded"  `, "padded"},
		{`" inner space "`, " inner space "},
		{`42`, "42"},
		{`true`, "true"},
		{``, ""},
		{`"`, `"`},
		{`""`, ""},
		// Matched-pair semantics, not run-strip: an interior quote survives.
		{`""x""`, `"x"`},
		{`"say \"hi\""`, `say \"hi\"`},
	}
	for _, c := range cases {
		if got := UnquoteAgtype(c.in); got != c.want {
			t.Errorf("UnquoteAgtype(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
